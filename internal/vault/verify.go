package vault

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"steamswitch/internal/actionlog"
	"steamswitch/internal/vault/probe"
	"steamswitch/internal/vault/steamauth"
	"steamswitch/internal/vault/totp"
)

// DeepCheckTimeout bounds one credential verification end to end, including waiting for a
// Guard code to arrive by mail.
const DeepCheckTimeout = 3 * time.Minute

// Only one deep check may be in flight process-wide. Fanning them out is what makes a
// health feature indistinguishable from credential stuffing, and it is also how an account
// gets rate-limited out of its own logins.
var deepMu sync.Mutex

// rateLimited latches for the process once Steam says it has had enough. Nothing clears it
// but a restart: auto-recovering from a rate limit is how a warning turns into a block.
var (
	rateLimitMu sync.Mutex
	rateLimited bool
)

// RateLimited reports whether Steam has rate-limited this process. The scheduler and the UI
// both consult it before starting anything.
func RateLimited() bool {
	rateLimitMu.Lock()
	defer rateLimitMu.Unlock()
	return rateLimited
}

func markRateLimited() {
	rateLimitMu.Lock()
	rateLimited = true
	rateLimitMu.Unlock()
	slog.Warn("vault: Steam rate-limited this client; deep checks are stopped for this session")
}

// ResetRateLimitForTest clears the latch. Tests only.
func ResetRateLimitForTest() {
	rateLimitMu.Lock()
	rateLimited = false
	rateLimitMu.Unlock()
}

var ErrDeepCheckBusy = errors.New("Toast_Vault_CheckAlreadyRunning")
var ErrNoPassword = errors.New("Toast_Vault_NoPasswordStored")

// DeepCheck verifies that an account's stored password still works.
//
// This is the expensive tier: it performs a real login, which for most accounts sends a
// Steam Guard email. It is never batched and never runs implicitly — see VAULT.md §5.1.
//
// It is not a way to sign the Steam client in. The only thing it does with the session it
// obtains is record the refresh token and throw the rest away.
func DeepCheck(ctx context.Context, steamID64 string, in QuickCheckInput) (HealthReport, error) {
	if RateLimited() {
		return HealthReport{}, steamauth.ErrRateLimited
	}
	if !deepMu.TryLock() {
		return HealthReport{}, ErrDeepCheckBusy
	}
	defer deepMu.Unlock()

	e, err := entry(steamID64)
	if err != nil {
		return HealthReport{}, err
	}
	// Both are needed to log in at all. The account name is not derivable from the SteamID
	// without asking Steam, so an entry saved with only a password cannot be deep-checked.
	if e.Password == "" || e.AccountName == "" {
		return HealthReport{}, ErrNoPassword
	}

	// The cheap signals run first and unconditionally: if the account is VAC-banned there
	// is no point spending a Guard code to learn the password still works.
	rep, _ := QuickCheck(ctx, steamID64, in)
	rep.Deep = true

	ctx, cancel := context.WithTimeout(ctx, DeepCheckTimeout)
	defer cancel()

	sig, result := runCredentialLogin(ctx, e)
	// The action log is attached to crash reports, so it carries the signal's outcome and
	// the account id (which logsanitize aliases) and nothing else. No password, no code, no
	// token, and no Steam error text that might quote one.
	actionlog.Record("vault.deepcheck", steamID64, sig.Status, nil)

	rep.Signals = append(rep.Signals, sig)
	rep.Verdict = worst(rep.Verdict, sig.Status)

	failures := e.CheckFailures
	if sig.Status == VerdictOK {
		failures = 0
	} else {
		failures++
	}

	if result != nil && result.RefreshToken != "" {
		expiry := ""
		if claims, err := probe.DecodeToken(result.RefreshToken); err == nil && !claims.ExpiresAt.IsZero() {
			expiry = claims.ExpiresAt.Format(time.RFC3339)
		}
		// guard_data is what stops the *next* verification emailing the user another code.
		if err := recordSession(steamID64, result.RefreshToken, result.NewGuardData, expiry); err != nil {
			slog.Warn("vault: could not store the session material", "steamId64", steamID64)
		}
	}

	if err := recordHealth(steamID64, rep, nextEligible(time.Now(), failures), failures); err != nil {
		return rep, err
	}
	return rep, nil
}

// runCredentialLogin performs the login and turns the outcome into a signal. It never
// returns Steam's own error text to the caller: it is untranslated and may quote the
// account name.
func runCredentialLogin(ctx context.Context, e Entry) (Signal, *steamauth.Result) {
	s := Signal{Name: SignalPassword, Status: VerdictUnknown, Detail: "Vault_Signal_PasswordUnknown"}
	client := &steamauth.Client{}

	sess, err := client.Begin(ctx, e.AccountName, e.Password, e.GuardData)
	if err != nil {
		return classifyLoginError(s, err), nil
	}

	if sess.Guard != steamauth.GuardNone {
		code, cerr := guardCodeForLogin(ctx, e, sess)
		if cerr != nil {
			// Reaching the Guard step proves the password was accepted — Steam does not
			// ask for a second factor otherwise. That is the question this check exists to
			// answer, so it is a pass with a caveat, not a failure.
			s.Status, s.Detail = VerdictOK, "Vault_Signal_PasswordOKGuardUnavailable"
			return s, nil
		}
		if err := client.SubmitGuardCode(ctx, sess, code); err != nil {
			return classifyLoginError(s, err), nil
		}
	}

	res, err := client.WaitForResult(ctx, sess)
	if err != nil {
		return classifyLoginError(s, err), nil
	}
	s.Status, s.Detail = VerdictOK, "Vault_Signal_PasswordOK"
	return s, res
}

// guardCodeForLogin answers Steam's challenge from the authenticator seed when the account
// has one, and from the bound mailbox otherwise.
func guardCodeForLogin(ctx context.Context, e Entry, sess *steamauth.Session) (string, error) {
	if sess.Guard == steamauth.GuardDeviceCode || e.SharedSecret != "" {
		if e.SharedSecret == "" {
			return "", totp.ErrEmptySecret
		}
		code, err := totp.Generate(e.SharedSecret, time.Now())
		if err != nil {
			return "", err
		}
		return code, nil
	}
	src, err := sourceFor(e)
	if err != nil {
		return "", err
	}
	return src.FetchCode(ctx, time.Now())
}

func classifyLoginError(s Signal, err error) Signal {
	switch {
	case errors.Is(err, steamauth.ErrBadCredentials):
		s.Status, s.Blocker, s.Detail = VerdictFail, true, "Vault_Signal_PasswordWrong"
	case errors.Is(err, steamauth.ErrSuspended):
		s.Status, s.Blocker, s.Detail = VerdictFail, true, "Vault_Signal_AccountSuspended"
	case errors.Is(err, steamauth.ErrGuardRejected):
		s.Status, s.Detail = VerdictWarn, "Vault_Signal_GuardRejected"
	case errors.Is(err, steamauth.ErrRateLimited):
		markRateLimited()
		s.Status, s.Detail = VerdictUnknown, "Vault_Signal_RateLimited"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		s.Status, s.Detail = VerdictUnknown, "Vault_Signal_CheckTimedOut"
	default:
		s.Status, s.Detail = VerdictUnknown, "Vault_Signal_PasswordUnknown"
	}
	return s
}

// --- login details --------------------------------------------------------------------

// TokenDetails is the Tier 2 half of the login debug panel: what the stored refresh token
// says about itself. The token itself is not here — it is behind Reveal like every other
// secret.
type TokenDetails struct {
	Present         bool   `json:"present"`
	Issuer          string `json:"issuer,omitempty"`
	Subject         string `json:"subject,omitempty"`
	Audience        string `json:"audience,omitempty"`
	IsClientToken   bool   `json:"isClientToken"`
	IssuedAt        string `json:"issuedAt,omitempty"`
	ExpiresAt       string `json:"expiresAt,omitempty"`
	Expired         bool   `json:"expired"`
	JTI             string `json:"jti,omitempty"`
	IPSubject       string `json:"ipSubject,omitempty"`
	IPConfirmer     string `json:"ipConfirmer,omitempty"`
	Unreadable      bool   `json:"unreadable"`
	HasGuardData    bool   `json:"hasGuardData"`
	DaysUntilExpiry int    `json:"daysUntilExpiry,omitempty"`
}

// TokenInfo decodes the stored token for the debug panel.
func TokenInfo(steamID64 string) (TokenDetails, error) {
	e, err := entry(steamID64)
	if err != nil {
		return TokenDetails{}, err
	}
	d := TokenDetails{HasGuardData: e.GuardData != ""}
	if e.RefreshToken == "" {
		return d, nil
	}
	d.Present = true
	claims, err := probe.DecodeToken(e.RefreshToken)
	if err != nil {
		d.Unreadable = true
		return d, nil
	}
	now := time.Now()
	d.Issuer = claims.Issuer
	d.Subject = claims.Subject
	d.Audience = claims.DescribeAudience()
	d.IsClientToken = claims.IsClientAudience()
	d.JTI = claims.JTI
	d.IPSubject = claims.IPSubject
	d.IPConfirmer = claims.IPConfirmer
	d.Expired = claims.Expired(now)
	if !claims.IssuedAt.IsZero() {
		d.IssuedAt = claims.IssuedAt.Format(time.RFC3339)
	}
	if !claims.ExpiresAt.IsZero() {
		d.ExpiresAt = claims.ExpiresAt.Format(time.RFC3339)
		if !d.Expired {
			d.DaysUntilExpiry = int(claims.ExpiresAt.Sub(now).Hours() / 24)
		}
	}
	return d, nil
}
