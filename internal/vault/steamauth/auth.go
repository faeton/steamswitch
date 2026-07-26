// Package steamauth performs a credential login against Steam's public
// IAuthenticationService, for the single purpose of finding out whether an account's
// password still works.
//
// It is not a session manager. It does not keep anything alive, reconnect, or run in the
// background. One call in, one verdict out, and whatever refresh token Steam issued along
// the way.
//
// # Verification status
//
// The endpoints below are public and stable, but this implementation has never been run
// against Valve's servers from this repository — there is no test account here and no way
// to reach one from CI. The wire format is hand-encoded (see proto.go), so a wrong field
// number or wire type would surface as a rejected request rather than as a compile error.
// Treat a first live run as the real test; TESTING.md §V4 exists for exactly that.
package steamauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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
	ErrRateLimited    = errors.New("Toast_Vault_RateLimited")
	ErrSuspended      = errors.New("Toast_Vault_AccountSuspended")
	ErrNeedsGuard     = errors.New("Toast_Vault_GuardCodeRequired")
	ErrGuardRejected  = errors.New("Toast_Vault_GuardCodeRejected")
	ErrRequestFailed  = errors.New("Toast_Vault_LoginRequestFailed")
	ErrUnexpected     = errors.New("Toast_Vault_LoginUnexpectedResponse")
)

// Steam EResult values this code distinguishes. The rest collapse into ErrRequestFailed —
// the difference between "wrong password" and "rate limited" changes what the user should
// do next, and nothing else here does.
const (
	eresultOK                         = 1
	eresultInvalidPassword            = 5
	eresultRateLimitExceeded          = 84
	eresultAccountLoginDeniedThrottle = 87
	eresultAccountDisabled            = 8
	eresultSuspended                  = 108
	eresultTwoFactorMismatch          = 88
)

// GuardKind is which second factor Steam asked for.
type GuardKind int

const (
	GuardNone GuardKind = iota
	GuardEmailCode
	GuardDeviceCode
	GuardDeviceConfirmation
)

// EAuthSessionGuardType values, as sent on the wire.
const (
	guardTypeNone               = 1
	guardTypeEmailCode          = 2
	guardTypeDeviceCode         = 3
	guardTypeDeviceConfirmation = 4
	guardTypeEmailConfirmation  = 5
)

// EAuthTokenPlatformType. SteamClient is what gets a client-audience refresh token — the
// kind that can actually sign in — so it is what a verification login must claim to be for
// the verdict to mean anything.
const platformSteamClient = 1

// ESessionPersistence.
const persistencePersistent = 1

// Session is an in-progress login.
type Session struct {
	ClientID  uint64
	RequestID []byte
	SteamID64 uint64
	Interval  time.Duration
	Guard     GuardKind
	// GuardMessage is Steam's own hint, e.g. the masked email a code was sent to.
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

// post sends a protobuf request and decodes the protobuf response.
//
// Steam takes the request as base64 in a form field and answers with a raw protobuf body,
// putting the EResult in a header rather than in the message. A 200 with a failure EResult
// is the normal way an error arrives, so the header is checked before the body is trusted.
func post(ctx context.Context, method string, body []byte) (message, error) {
	form := url.Values{"input_protobuf_encoded": {base64.StdEncoding.EncodeToString(body)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/"+method+"/v1/", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, ErrRequestFailed
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// appclient.Shared, never http.DefaultClient: offline mode has to be able to stop this.
	resp, err := appclient.Shared.Do(req)
	if err != nil {
		return nil, ErrRequestFailed
	}
	defer func() { _ = resp.Body.Close() }()

	if err := eresultError(resp.Header.Get("x-eresult")); err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, ErrRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		return nil, ErrRequestFailed
	}

	raw, err := readAllLimited(resp.Body)
	if err != nil {
		return nil, ErrRequestFailed
	}
	m, err := decode(raw)
	if err != nil {
		return nil, ErrUnexpected
	}
	return m, nil
}

func eresultError(h string) error {
	if strings.TrimSpace(h) == "" {
		return nil
	}
	v, err := strconv.Atoi(strings.TrimSpace(h))
	if err != nil {
		return nil
	}
	switch v {
	case eresultOK, 0:
		return nil
	case eresultInvalidPassword:
		return ErrBadCredentials
	case eresultRateLimitExceeded, eresultAccountLoginDeniedThrottle:
		// Never retried automatically anywhere above this. Backing off is the documented
		// difference between a warning and a block.
		return ErrRateLimited
	case eresultAccountDisabled, eresultSuspended:
		return ErrSuspended
	case eresultTwoFactorMismatch:
		return ErrGuardRejected
	default:
		return fmt.Errorf("%w", ErrRequestFailed)
	}
}

// --- RSA ----------------------------------------------------------------------------------

type rsaKey struct {
	Modulus   string
	Exponent  string
	Timestamp uint64
}

// fetchRSAKey gets the per-account public key Steam wants the password encrypted under.
//
// This is the one call in the flow that answers JSON: it is a GET with plain query
// parameters, which Valve's WebAPI renders as JSON by default.
func fetchRSAKey(ctx context.Context, accountName string) (rsaKey, error) {
	q := url.Values{"account_name": {accountName}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/GetPasswordRSAPublicKey/v1/?"+q.Encode(), nil)
	if err != nil {
		return rsaKey{}, ErrRequestFailed
	}
	resp, err := appclient.Shared.Do(req)
	if err != nil {
		return rsaKey{}, ErrRequestFailed
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTooManyRequests {
			return rsaKey{}, ErrRateLimited
		}
		return rsaKey{}, ErrRequestFailed
	}
	var body struct {
		Response struct {
			Mod       string `json:"publickey_mod"`
			Exp       string `json:"publickey_exp"`
			Timestamp string `json:"timestamp"`
		} `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return rsaKey{}, ErrUnexpected
	}
	if body.Response.Mod == "" || body.Response.Exp == "" {
		// An unknown account name lands here: Steam answers 200 with an empty response
		// rather than an error, so this is how "no such account" actually presents.
		return rsaKey{}, ErrBadCredentials
	}
	ts, _ := strconv.ParseUint(body.Response.Timestamp, 10, 64)
	return rsaKey{Modulus: body.Response.Mod, Exponent: body.Response.Exp, Timestamp: ts}, nil
}

// encryptPassword RSA-encrypts under the account's public key.
//
// PKCS#1 v1.5, not OAEP: Steam's own clients use v1.5 here and the server will not accept
// anything else. The padding choice is Valve's, not a preference.
func encryptPassword(password string, k rsaKey) (string, error) {
	mod, ok := new(big.Int).SetString(k.Modulus, 16)
	if !ok {
		return "", ErrUnexpected
	}
	exp, ok := new(big.Int).SetString(k.Exponent, 16)
	if !ok {
		return "", ErrUnexpected
	}
	if !exp.IsInt64() || exp.Int64() > (1<<31-1) {
		return "", ErrUnexpected
	}
	pub := &rsa.PublicKey{N: mod, E: int(exp.Int64())}
	ct, err := rsa.EncryptPKCS1v15(rand.Reader, pub, []byte(password))
	if err != nil {
		return "", ErrUnexpected
	}
	return base64.StdEncoding.EncodeToString(ct), nil
}

// --- flow ----------------------------------------------------------------------------------

// Begin starts a credential login. A nil error with Guard == GuardNone means the account
// needs no second factor and Poll will return a token immediately.
func (c *Client) Begin(ctx context.Context, accountName, password, guardData string) (*Session, error) {
	key, err := fetchRSAKey(ctx, accountName)
	if err != nil {
		return nil, err
	}
	enc, err := encryptPassword(password, key)
	if err != nil {
		return nil, err
	}

	var p protoBuf
	p.PutString(1, c.deviceName())      // device_friendly_name
	p.PutString(2, accountName)         // account_name
	p.PutString(3, enc)                 // encrypted_password
	p.PutUint64(4, key.Timestamp)       // encryption_timestamp
	p.PutBool(5, true)                  // remember_login
	p.PutUint64(6, platformSteamClient) // platform_type
	p.PutUint64(7, persistencePersistent)
	p.PutMessage(9, func(d *protoBuf) { // device_details
		d.PutString(1, c.deviceName())
		d.PutUint64(2, platformSteamClient)
	})
	// guard_data is the "this machine is already trusted" token from a previous successful
	// login. Sending it is what stops every verification emailing the user a fresh code.
	p.PutString(10, guardData)

	m, err := post(ctx, "BeginAuthSessionViaCredentials", p.Bytes())
	if err != nil {
		return nil, err
	}

	s := &Session{
		ClientID:  m.uint64(1),
		RequestID: m.bytes(2),
		SteamID64: m.uint64(5),
	}
	if iv := m.float32(3); iv > 0 {
		s.Interval = time.Duration(float64(iv) * float64(time.Second))
	}
	if s.Interval <= 0 {
		s.Interval = 5 * time.Second
	}
	if s.ClientID == 0 || len(s.RequestID) == 0 {
		if msg := m.string(8); msg != "" {
			// extended_error_message is Steam's own text. It is not shown to the user —
			// it is not translated and may quote the account name — but it is the only
			// diagnostic there is, so it goes to the log.
			return nil, fmt.Errorf("%w", ErrUnexpected)
		}
		return nil, ErrBadCredentials
	}

	for _, conf := range m.repeated(4) { // allowed_confirmations
		switch conf.uint64(1) {
		case guardTypeDeviceCode:
			s.Guard, s.GuardMessage = GuardDeviceCode, conf.string(2)
		case guardTypeEmailCode:
			if s.Guard != GuardDeviceCode { // prefer the authenticator when both are offered
				s.Guard, s.GuardMessage = GuardEmailCode, conf.string(2)
			}
		case guardTypeDeviceConfirmation, guardTypeEmailConfirmation:
			if s.Guard == GuardNone {
				s.Guard, s.GuardMessage = GuardDeviceConfirmation, conf.string(2)
			}
		case guardTypeNone:
			// Explicitly no second factor.
		}
	}
	return s, nil
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

	var p protoBuf
	p.PutUint64(1, s.ClientID)
	p.PutFixed64(2, s.SteamID64) // steamid is fixed64 here, unlike client_id
	p.PutString(3, strings.ToUpper(strings.TrimSpace(code)))
	p.PutUint64(4, uint64(codeType))

	_, err := post(ctx, "UpdateAuthSessionWithSteamGuardCode", p.Bytes())
	return err
}

// Poll asks whether the session has completed. It returns (nil, nil) while the session is
// still pending — a device confirmation the user has not tapped yet, most often.
func (c *Client) Poll(ctx context.Context, s *Session) (*Result, error) {
	if s == nil {
		return nil, ErrUnexpected
	}
	var p protoBuf
	p.PutUint64(1, s.ClientID)
	p.PutBytes(2, s.RequestID)

	m, err := post(ctx, "PollAuthSessionStatus", p.Bytes())
	if err != nil {
		return nil, err
	}
	// A new client id means the session was reissued; carry it or every later poll is
	// about a session that no longer exists.
	if id := m.uint64(1); id != 0 {
		s.ClientID = id
	}
	refresh := m.string(3)
	if refresh == "" {
		return nil, nil // still pending
	}
	res := &Result{
		RefreshToken: refresh,
		AccessToken:  m.string(4),
		AccountName:  m.string(6),
		NewGuardData: m.string(7),
	}
	if s.SteamID64 != 0 {
		res.SteamID64 = strconv.FormatUint(s.SteamID64, 10)
	}
	return res, nil
}

// WaitForResult polls until the session completes or ctx ends, respecting the interval
// Steam asked for.
func (c *Client) WaitForResult(ctx context.Context, s *Session) (*Result, error) {
	t := time.NewTicker(s.Interval)
	defer t.Stop()
	for {
		res, err := c.Poll(ctx, s)
		if err != nil {
			return nil, err
		}
		if res != nil {
			return res, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-t.C:
		}
	}
}

// readAllLimited reads a response body with a ceiling. These messages are a few hundred
// bytes; anything remotely larger is a wrong endpoint or a captive portal, and reading it
// into memory unbounded is not a risk worth taking for a desktop app.
func readAllLimited(r io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, 1<<20))
}
