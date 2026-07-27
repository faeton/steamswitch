package vault

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"steamswitch/internal/actionlog"
	"steamswitch/internal/vault/mail"
	"steamswitch/internal/vault/probe"
	"steamswitch/internal/vault/session"
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

// quickRateLimited reports whether any cheap signal came back rate-limited. The signals
// carry an i18n key rather than the error, so this matches on the key the probe helpers set.
func quickRateLimited(rep HealthReport) bool {
	for _, s := range rep.Signals {
		if s.Detail == "Vault_Signal_RateLimited" {
			return true
		}
	}
	return false
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
// Steam Guard email. It is never batched — one account at a time, process-wide.
//
// It runs when the user asks for it, and on the opt-in schedule in scheduler.go, which is
// off by default. (This comment used to say "never runs implicitly"; the scheduler made that
// false, and a stale safety claim on the most dangerous path in the package is worse than no
// comment at all.)
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
	rep, quickErr := QuickCheck(ctx, steamID64, in)
	rep.Deep = true

	// A 429 from the cheap Web API calls is Steam saying it has had enough of this client.
	// Walking past it into a credential login is exactly the escalation VAULT.md §5.3 forbids
	// — the quick tier's rate limit used to be recorded as an "unknown" signal and otherwise
	// ignored, so the latch only ever tripped if the login *itself* was refused.
	if quickRateLimited(rep) {
		markRateLimited()
		return rep, steamauth.ErrRateLimited
	}
	// A quick check that could not be persisted means the schedule cannot be advanced either.
	// Logging in anyway leaves the account due on the next tick and every tick after it, which
	// turns one unwritable disk into a credential login every thirty minutes.
	if quickErr != nil {
		return rep, quickErr
	}

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
		if !result.ExpiresAt.IsZero() {
			expiry = result.ExpiresAt.Format(time.RFC3339)
		} else if claims, err := probe.DecodeToken(result.RefreshToken); err == nil && !claims.ExpiresAt.IsZero() {
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

// SessionCheckTimeout bounds a session liveness probe: one brief CM sign-in, no Guard wait.
const SessionCheckTimeout = 30 * time.Second

// CheckSession reports whether an account's stored session is still live, without a password or a
// Guard email.
//
// It sits between QuickCheck (local signals only) and DeepCheck (a full credential login that
// spends a Guard code): a brief CM sign-in with the saved refresh token (see session.Check)
// confirms the session when Steam still honours it, and reports it revoked — owner password
// change, "sign out of all devices", or a Steam-side invalidation — when it does not. Because it
// needs no Guard email it is the check to prefer whenever an account has a stored token, at the
// cost of briefly signing the account in.
//
// It records only the session report; it deliberately leaves CheckFailures and the deep-check
// schedule alone, because those are the credential login's backoff state, not this check's. Like
// DeepCheck it serialises through deepMu and honours the rate-limit latch: the CM logon shares
// Steam's login rate limit, so a scan of many accounts must run one at a time and stop the moment
// Steam pushes back.
func CheckSession(ctx context.Context, steamID64 string) (HealthReport, error) {
	if RateLimited() {
		return HealthReport{}, steamauth.ErrRateLimited
	}
	// One CM logon at a time, process-wide, shared with DeepCheck: both draw on the same login
	// rate limit, and fanning them out is what gets an account throttled out of its own logins.
	if !deepMu.TryLock() {
		return HealthReport{}, ErrDeepCheckBusy
	}
	defer deepMu.Unlock()

	e, err := entry(steamID64)
	if err != nil {
		return HealthReport{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, SessionCheckTimeout)
	defer cancel()

	rep := HealthReport{ProbedAt: time.Now().UTC().Format(time.RFC3339), Verdict: VerdictUnknown}
	add := func(s Signal) {
		rep.Signals = append(rep.Signals, s)
		rep.Verdict = worst(rep.Verdict, s.Status)
	}
	add(tokenSignal(e)) // local context: what the stored token says about its own expiry
	add(sessionSignal(ctx, steamID64, e))
	// The action log rides on crash reports, so only the outcome and the (aliased) account id.
	actionlog.Record("vault.sessioncheck", steamID64, rep.Verdict, nil)

	// Store the report only: the existing failure count is passed back unchanged and nextEligible
	// is zero, so recordHealth leaves CheckFailures and the deep-check schedule untouched.
	if err := recordHealth(steamID64, rep, time.Time{}, e.CheckFailures); err != nil {
		return rep, err
	}
	return rep, nil
}

// sessionSignal probes the stored refresh token's liveness. It briefly logs the account on to a
// Steam CM with the token (the only mechanism that validates a client-audience token — Steam's
// HTTP token endpoints refuse it) and turns the outcome into a signal.
func sessionSignal(ctx context.Context, steamID64 string, e Entry) Signal {
	s := Signal{Name: SignalSession, Status: VerdictUnknown, Detail: "Vault_Signal_SessionUnknown"}
	if e.RefreshToken == "" {
		s.Detail = "Vault_Signal_NoToken"
		return s
	}
	if _, err := session.Check(ctx, e.AccountName, e.RefreshToken, steamID64); err != nil {
		return classifySessionError(s, err)
	}
	s.Status, s.Detail = VerdictOK, "Vault_Signal_SessionLive"
	return s
}

// classifySessionError turns a session-probe failure into a signal, on the same fail-versus-
// unknown principle as classifyLoginError: "Steam refused the token" is a fact about the account
// and blocks; "Steam would not answer" is a fact about the network and must not read as a dead
// account.
func classifySessionError(s Signal, err error) Signal {
	switch {
	case errors.Is(err, steamauth.ErrAccessDenied),
		errors.Is(err, steamauth.ErrBadCredentials),
		errors.Is(err, steamauth.ErrNoSuchAccount):
		// Steam no longer honours the token: revoked, or the owner changed the password /
		// signed out everywhere. The fix is a fresh credential login, so this blocks.
		s.Status, s.Blocker, s.Detail = VerdictFail, true, "Vault_Signal_SessionRevoked"
	case errors.Is(err, steamauth.ErrRateLimited):
		markRateLimited()
		s.Status, s.Detail = VerdictUnknown, "Vault_Signal_RateLimited"
	case errors.Is(err, steamauth.ErrServiceDown):
		s.Status, s.Detail = VerdictUnknown, "Vault_Signal_SteamUnavailable"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		s.Status, s.Detail = VerdictUnknown, "Vault_Signal_CheckTimedOut"
	default:
		s.Status, s.Detail = VerdictUnknown, "Vault_Signal_SessionUnknown"
	}
	return s
}

// runCredentialLogin performs the login and turns the outcome into a signal. It never
// returns Steam's own error text to the caller: it is untranslated and may quote the
// account name.
func runCredentialLogin(ctx context.Context, e Entry) (Signal, *steamauth.Result) {
	s := Signal{Name: SignalPassword, Status: VerdictUnknown, Detail: "Vault_Signal_PasswordUnknown"}
	client := &steamauth.Client{}

	// Capture the login-start instant before Begin, which is what triggers the Guard email. The
	// mailbox fetch judges freshness against this, so it must precede the mail, not follow it.
	loginStart := time.Now()
	sess, err := client.Begin(ctx, e.AccountName, e.Password, e.GuardData)
	if err != nil {
		return classifyLoginError(s, err), nil
	}

	if sess.Guard != steamauth.GuardNone {
		code, cerr := guardCodeForLogin(ctx, e, sess, loginStart)
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
func guardCodeForLogin(ctx context.Context, e Entry, sess *steamauth.Session, loginStart time.Time) (string, error) {
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
	// Everything below answers an EMAILED code. A device-confirmation challenge (approve on the
	// mobile app) has no code to type, so any non-email kind fails closed here — the caller reports
	// "password OK, Guard step unavailable" rather than prompting for a code Steam never sent.
	if sess.Guard != steamauth.GuardEmailCode {
		return "", steamauth.ErrGuardRequired
	}
	// An inbox we cannot IMAP-check (manual source), or an account with no configured source at
	// all, asks the user to type the code rather than failing the Guard step.
	if emailSourceOf(e) == EmailSourceManual {
		return requestManualCode(ctx, e.SteamID64, sess.GuardMessage)
	}
	src, err := sourceFor(e)
	if err != nil {
		if errors.Is(err, mail.ErrNoSource) {
			return requestManualCode(ctx, e.SteamID64, sess.GuardMessage)
		}
		return "", err
	}
	// loginStart, not time.Now(): the email was triggered by Begin above, and freshness is judged
	// against the instant before it was sent.
	return src.FetchCode(ctx, loginStart)
}

// classifyLoginError turns a login failure into a signal.
//
// The distinction that matters is fail versus unknown. "Steam rejected the password" is a
// fact about the account and blocks; "Steam would not answer" is a fact about the network
// and must not be reported as though the account were broken — a user who deletes a working
// account because the app said it was dead has been failed by this function.
func classifyLoginError(s Signal, err error) Signal {
	switch {
	case errors.Is(err, steamauth.ErrBadCredentials):
		s.Status, s.Blocker, s.Detail = VerdictFail, true, "Vault_Signal_PasswordWrong"
	case errors.Is(err, steamauth.ErrNoSuchAccount):
		// Steam has no such login name. Distinct from a wrong password: the thing to fix is
		// the stored account name, not the password.
		s.Status, s.Blocker, s.Detail = VerdictFail, true, "Vault_Signal_NoSuchAccount"
	case errors.Is(err, steamauth.ErrSuspended):
		s.Status, s.Blocker, s.Detail = VerdictFail, true, "Vault_Signal_AccountSuspended"
	case errors.Is(err, steamauth.ErrAccessDenied):
		s.Status, s.Blocker, s.Detail = VerdictFail, true, "Vault_Signal_AccessDenied"
	case errors.Is(err, steamauth.ErrGuardRejected):
		s.Status, s.Detail = VerdictWarn, "Vault_Signal_GuardRejected"
	case errors.Is(err, steamauth.ErrGuardRequired):
		// Reaching the Guard step at all proves the password was accepted; Steam does not
		// ask for a second factor otherwise.
		s.Status, s.Detail = VerdictOK, "Vault_Signal_PasswordOKGuardUnavailable"
	case errors.Is(err, steamauth.ErrRateLimited):
		markRateLimited()
		s.Status, s.Detail = VerdictUnknown, "Vault_Signal_RateLimited"
	case errors.Is(err, steamauth.ErrServiceDown):
		s.Status, s.Detail = VerdictUnknown, "Vault_Signal_SteamUnavailable"
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
