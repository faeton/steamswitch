// Package steamauth performs a credential login against Steam's public
// IAuthenticationService, for the single purpose of finding out whether an account's
// password still works.
//
// It is not a session manager. It does not keep anything alive, reconnect, or run in the
// background. One call in, one verdict out, and whatever refresh token Steam issued along
// the way.
//
// # Wire format
//
// These endpoints accept ordinary form-encoded parameters and answer with JSON. They are
// protobuf methods underneath, and Valve's WebAPI translates both directions — so no
// protobuf encoding is needed here, and none is done.
//
// An earlier version of this file hand-encoded the protobuf messages, on the assumption
// that `input_protobuf_encoded` was required. It is not, and that assumption cost ~400 lines
// of wire-format code whose field numbers could only have been validated against a live
// Steam account. The shape below is the one that has actually been run against Valve's
// servers, in ggcr-bot-fleet, over a long period and many accounts.
package steamauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"steamswitch/internal/appclient"
)

const apiBase = "https://api.steampowered.com/IAuthenticationService"

// Errors are i18n keys.
var (
	ErrBadCredentials = errors.New("Toast_Vault_WrongPassword")
	ErrNoSuchAccount  = errors.New("Toast_Vault_NoSuchAccount")
	ErrRateLimited    = errors.New("Toast_Vault_RateLimited")
	ErrSuspended      = errors.New("Toast_Vault_AccountSuspended")
	ErrAccessDenied   = errors.New("Toast_Vault_AccessDenied")
	ErrGuardRequired  = errors.New("Toast_Vault_GuardCodeRequired")
	ErrGuardRejected  = errors.New("Toast_Vault_GuardCodeRejected")
	ErrServiceDown    = errors.New("Toast_Vault_SteamUnavailable")
	ErrRequestFailed  = errors.New("Toast_Vault_LoginRequestFailed")
	ErrUnexpected     = errors.New("Toast_Vault_LoginUnexpectedResponse")
)

// Steam EResult values that reach this flow in practice.
//
// The list and its meanings are taken from a deployment that has seen all of them. Two are
// worth calling out because guessing them wrongly sends the user the wrong way: 8 is "no
// such account", not "account disabled", and 84 is a rate limit that must never be retried
// automatically.
const (
	eresultInvalidPassword    = "5"
	eresultNoMatch            = "8"
	eresultAccessDenied       = "15"
	eresultServiceUnavailable = "20"
	eresultLimitExceeded      = "25"
	eresultTwoFactorRequired  = "65"
	eresultRateLimitExceeded  = "84"
	eresultTwoFactorRequired2 = "85"
	eresultTwoFactorMismatch  = "88"
	eresultSuspended          = "108"
)

// GuardKind is which second factor Steam asked for.
type GuardKind int

const (
	GuardNone GuardKind = iota
	GuardEmailCode
	GuardDeviceCode
	GuardDeviceConfirmation
)

// EAuthSessionGuardType values, as they appear in allowed_confirmations.
//
// `machineToken` means Steam already trusts this machine from a previous login and wants
// nothing further; it is treated as "no interactive step", not as an unknown type to fall
// through on.
const (
	guardTypeUnknown            = 0
	guardTypeNone               = 1
	guardTypeEmailCode          = 2
	guardTypeDeviceCode         = 3
	guardTypeDeviceConfirmation = 4
	guardTypeEmailConfirmation  = 5
	guardTypeMachineToken       = 6
)

// platformSteamClient is EAuthTokenPlatformType_SteamClient, and websiteClient is the
// matching website id. Together they are what gets a client-audience refresh token — the
// kind that can actually sign in — so they are what a verification login must claim to be
// for its verdict to mean anything.
const (
	platformSteamClient = "1"
	websiteClient       = "Client"
	persistencePersist  = "1"
)

// Session is an in-progress login.
type Session struct {
	ClientID  uint64
	RequestID []byte
	SteamID64 uint64
	Interval  time.Duration
	Guard     GuardKind
	// GuardMessage is Steam's own hint, e.g. the masked address a code was sent to.
	GuardMessage string
}

// Result is what a completed login yielded.
type Result struct {
	RefreshToken string
	AccessToken  string
	AccountName  string
	NewGuardData string
	SteamID64    string
	ExpiresAt    time.Time
}

// Client performs logins. DeviceName is what shows up in the account's authorised-devices
// list, so it is honest about which application logged in rather than impersonating Steam.
type Client struct {
	DeviceName string
}

func (c *Client) deviceName() string {
	if strings.TrimSpace(c.DeviceName) == "" {
		return "SteamSwitch"
	}
	return c.DeviceName
}

// --- transport ---------------------------------------------------------------------------

// maxBody caps a response read. These are a few hundred bytes; anything larger is a captive
// portal or a wrong endpoint, and reading it unbounded is not a risk worth taking.
const maxBody = 1 << 20

// post sends form parameters and decodes the JSON reply.
//
// Steam reports failures two ways at once: an HTTP status, and an `x-eresult` header on an
// otherwise-200 response with an empty body. The header is the more specific of the two, so
// it is preferred wherever both are present.
func post(ctx context.Context, method string, params url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/"+method+"/v1/",
		strings.NewReader(params.Encode()))
	if err != nil {
		return ErrRequestFailed
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	// appclient.Shared, never http.DefaultClient: offline mode has to be able to stop this.
	resp, err := appclient.Shared.Do(req)
	if err != nil {
		return ErrRequestFailed
	}
	defer func() { _ = resp.Body.Close() }()

	eresult := resp.Header.Get("x-eresult")
	if resp.StatusCode == http.StatusTooManyRequests {
		return ErrRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		if err := eresultError(eresult); err != nil {
			return err
		}
		return ErrRequestFailed
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return ErrRequestFailed
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			// A non-JSON 200 is Steam refusing in a way this build does not model. The
			// eresult header is usually the real answer in that case.
			if e := eresultError(eresult); e != nil {
				return e
			}
			return ErrUnexpected
		}
	}
	return nil
}

// eresultError maps Steam's EResult to a specific condition, or nil when it says nothing.
//
// Only codes that change what the user should do next are distinguished. "Wrong password"
// and "rate limited" lead to opposite actions; everything else collapses.
func eresultError(code string) error {
	switch strings.TrimSpace(code) {
	case "", "0", "1": // absent, invalid, or OK
		return nil
	case eresultInvalidPassword:
		return ErrBadCredentials
	case eresultNoMatch:
		return ErrNoSuchAccount
	case eresultAccessDenied:
		return ErrAccessDenied
	case eresultServiceUnavailable:
		return ErrServiceDown
	case eresultLimitExceeded, eresultRateLimitExceeded:
		// Never retried automatically anywhere above this. Backing off is the documented
		// difference between a warning and a block.
		return ErrRateLimited
	case eresultTwoFactorRequired, eresultTwoFactorRequired2:
		return ErrGuardRequired
	case eresultTwoFactorMismatch:
		return ErrGuardRejected
	case eresultSuspended:
		return ErrSuspended
	default:
		return ErrRequestFailed
	}
}

// --- RSA ----------------------------------------------------------------------------------

type rsaKey struct {
	Modulus   string
	Exponent  string
	Timestamp string
}

// fetchRSAKey gets the per-account public key Steam wants the password encrypted under.
func fetchRSAKey(ctx context.Context, accountName string) (rsaKey, error) {
	q := url.Values{"account_name": {accountName}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		apiBase+"/GetPasswordRSAPublicKey/v1/?"+q.Encode(), nil)
	if err != nil {
		return rsaKey{}, ErrRequestFailed
	}
	resp, err := appclient.Shared.Do(req)
	if err != nil {
		return rsaKey{}, ErrRequestFailed
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests {
		return rsaKey{}, ErrRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		return rsaKey{}, ErrRequestFailed
	}
	var body struct {
		Response struct {
			Mod       string `json:"publickey_mod"`
			Exp       string `json:"publickey_exp"`
			Timestamp string `json:"timestamp"`
		} `json:"response"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&body); err != nil {
		return rsaKey{}, ErrUnexpected
	}
	if body.Response.Mod == "" || body.Response.Exp == "" {
		// An unknown account name lands here: Steam answers 200 with an empty response
		// rather than an error, so this is how "no such account" actually presents.
		return rsaKey{}, ErrNoSuchAccount
	}
	return rsaKey{Modulus: body.Response.Mod, Exponent: body.Response.Exp, Timestamp: body.Response.Timestamp}, nil
}

// EncryptPassword RSA-encrypts under an account's public key, given hex modulus and
// exponent as Steam publishes them.
//
// PKCS#1 v1.5, not OAEP: Steam's own clients use v1.5 here and the server accepts nothing
// else. The padding choice is Valve's, not a preference.
func EncryptPassword(password, modHex, expHex string) (string, error) {
	n, ok := new(big.Int).SetString(strings.TrimSpace(modHex), 16)
	if !ok {
		return "", ErrUnexpected
	}
	exp, err := strconv.ParseInt(strings.TrimSpace(expHex), 16, 32)
	if err != nil || exp <= 0 {
		return "", ErrUnexpected
	}
	pub := &rsa.PublicKey{N: n, E: int(exp)}
	ct, err := rsa.EncryptPKCS1v15(rand.Reader, pub, []byte(password))
	if err != nil {
		return "", ErrUnexpected
	}
	return base64.StdEncoding.EncodeToString(ct), nil
}

// --- flow ----------------------------------------------------------------------------------

type beginResponse struct {
	Response struct {
		// Steam renders 64-bit ids as JSON strings; without `,string` these decode as zero
		// and every later call is about a session that does not exist.
		ClientID             uint64          `json:"client_id,string"`
		RequestID            json.RawMessage `json:"request_id"`
		Interval             float64         `json:"interval"`
		AllowedConfirmations []struct {
			ConfirmationType  int    `json:"confirmation_type"`
			AssociatedMessage string `json:"associated_message"`
		} `json:"allowed_confirmations"`
		SteamID              string `json:"steamid"`
		ExtendedErrorMessage string `json:"extended_error_message"`
	} `json:"response"`
}

// Begin starts a credential login. A nil error with Guard == GuardNone means no interactive
// second factor is needed and Poll will return a token shortly.
func (c *Client) Begin(ctx context.Context, accountName, password, guardData string) (*Session, error) {
	key, err := fetchRSAKey(ctx, accountName)
	if err != nil {
		return nil, err
	}
	enc, err := EncryptPassword(password, key.Modulus, key.Exponent)
	if err != nil {
		return nil, err
	}

	params := url.Values{
		"account_name":         {accountName},
		"encrypted_password":   {enc},
		"encryption_timestamp": {key.Timestamp},
		"persistence":          {persistencePersist},
		"website_id":           {websiteClient},
		"device_friendly_name": {c.deviceName()},
		"platform_type":        {platformSteamClient},
	}
	// guard_data is the "this machine is already trusted" token from a previous successful
	// login. Sending it is what stops every verification emailing the user a fresh code.
	if strings.TrimSpace(guardData) != "" {
		params.Set("guard_data", guardData)
	}

	var body beginResponse
	if err := post(ctx, "BeginAuthSessionViaCredentials", params, &body); err != nil {
		return nil, err
	}
	r := body.Response
	if r.ClientID == 0 {
		// Steam answers 200 with an empty body and puts the reason in the header, which
		// post() has already classified if it said anything. Reaching here means it did
		// not, and the only remaining reading is that the credentials were refused.
		return nil, ErrBadCredentials
	}

	requestID, err := decodeRequestID(r.RequestID)
	if err != nil {
		return nil, ErrUnexpected
	}
	steamID, _ := strconv.ParseUint(strings.TrimSpace(r.SteamID), 10, 64)

	s := &Session{
		ClientID:  r.ClientID,
		RequestID: requestID,
		SteamID64: steamID,
		Interval:  time.Duration(r.Interval * float64(time.Second)),
	}
	if s.Interval < time.Second {
		s.Interval = 5 * time.Second
	}

	for _, conf := range r.AllowedConfirmations {
		switch conf.ConfirmationType {
		case guardTypeDeviceCode:
			// Preferred when offered alongside email: it is instant and needs no inbox.
			s.Guard, s.GuardMessage = GuardDeviceCode, conf.AssociatedMessage
		case guardTypeEmailCode:
			if s.Guard != GuardDeviceCode {
				s.Guard, s.GuardMessage = GuardEmailCode, conf.AssociatedMessage
			}
		case guardTypeDeviceConfirmation, guardTypeEmailConfirmation:
			if s.Guard == GuardNone {
				s.Guard, s.GuardMessage = GuardDeviceConfirmation, conf.AssociatedMessage
			}
		case guardTypeNone, guardTypeMachineToken, guardTypeUnknown:
			// Nothing interactive to do. A machine token means Steam already trusts this
			// machine from a previous login, which is the normal state after the first
			// successful verification.
		}
	}
	return s, nil
}

// decodeRequestID copes with Steam rendering the same field as a base64 string or as a JSON
// byte array depending on the endpoint and the day.
func decodeRequestID(raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 {
		return nil, ErrUnexpected
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if decoded, err := base64.StdEncoding.DecodeString(s); err == nil {
			return decoded, nil
		}
		return []byte(s), nil
	}
	var b []byte
	if err := json.Unmarshal(raw, &b); err == nil {
		return b, nil
	}
	return nil, ErrUnexpected
}

// SubmitGuardCode answers Steam's second-factor challenge.
func (c *Client) SubmitGuardCode(ctx context.Context, s *Session, code string) error {
	if s == nil {
		return ErrUnexpected
	}
	codeType := guardTypeEmailCode
	if s.Guard == GuardDeviceCode {
		codeType = guardTypeDeviceCode
	}
	return post(ctx, "UpdateAuthSessionWithSteamGuardCode", url.Values{
		"client_id": {strconv.FormatUint(s.ClientID, 10)},
		"steamid":   {strconv.FormatUint(s.SteamID64, 10)},
		"code":      {strings.ToUpper(strings.TrimSpace(code))},
		"code_type": {strconv.Itoa(codeType)},
	}, nil)
}

// Poll asks whether the session has completed. It returns (nil, nil) while the session is
// still pending — a device confirmation the user has not tapped yet, most often.
func (c *Client) Poll(ctx context.Context, s *Session) (*Result, error) {
	if s == nil {
		return nil, ErrUnexpected
	}
	var body struct {
		Response struct {
			NewClientID  uint64 `json:"new_client_id,string"`
			RefreshToken string `json:"refresh_token"`
			AccessToken  string `json:"access_token"`
			AccountName  string `json:"account_name"`
			NewGuardData string `json:"new_guard_data"`
		} `json:"response"`
	}
	err := post(ctx, "PollAuthSessionStatus", url.Values{
		"client_id":  {strconv.FormatUint(s.ClientID, 10)},
		"request_id": {base64.StdEncoding.EncodeToString(s.RequestID)},
	}, &body)
	if err != nil {
		return nil, err
	}

	// A reissued client id must be carried, or every later poll is about a session that no
	// longer exists.
	if body.Response.NewClientID != 0 {
		s.ClientID = body.Response.NewClientID
	}
	if body.Response.RefreshToken == "" {
		return nil, nil // still pending
	}

	res := &Result{
		RefreshToken: body.Response.RefreshToken,
		AccessToken:  body.Response.AccessToken,
		AccountName:  body.Response.AccountName,
		NewGuardData: body.Response.NewGuardData,
	}
	if s.SteamID64 != 0 {
		res.SteamID64 = strconv.FormatUint(s.SteamID64, 10)
	}
	if exp, ok := TokenExpiry(res.RefreshToken); ok {
		res.ExpiresAt = exp
	}
	return res, nil
}

// maxPollAttempts bounds the wait for a device confirmation the user may never tap.
const maxPollAttempts = 30

// WaitForResult polls until the session completes or ctx ends.
//
// Transient poll failures are tolerated rather than fatal: Steam returns an empty or
// unparseable body mid-session often enough that treating the first one as a failed login
// would report "could not be confirmed" for logins that were about to succeed. A terminal
// condition — a rate limit, a rejected code — still stops immediately.
func (c *Client) WaitForResult(ctx context.Context, s *Session) (*Result, error) {
	t := time.NewTicker(s.Interval)
	defer t.Stop()

	var lastErr error
	for range maxPollAttempts {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-t.C:
		}

		res, err := c.Poll(ctx, s)
		if err != nil {
			if terminal(err) {
				return nil, err
			}
			lastErr = err
			continue
		}
		if res != nil {
			return res, nil
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrGuardRequired
}

// terminal reports whether an error means the login cannot succeed, as opposed to "not
// yet". Retrying a terminal condition is how a rate limit becomes a block.
func terminal(err error) bool {
	for _, e := range []error{
		ErrRateLimited, ErrBadCredentials, ErrNoSuchAccount,
		ErrSuspended, ErrAccessDenied, ErrGuardRejected,
	} {
		if errors.Is(err, e) {
			return true
		}
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// TokenExpiry reads the `exp` claim from a refresh token.
//
// The signature is deliberately not verified: there is no public key to verify it against,
// and the purpose is to report what the token says about itself.
func TokenExpiry(token string) (time.Time, bool) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, false
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(data, &claims); err != nil || claims.Exp == 0 {
		return time.Time{}, false
	}
	return time.Unix(claims.Exp, 0).UTC(), true
}
