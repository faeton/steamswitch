package vault

import (
	"context"
	"errors"
	"strconv"
	"time"

	"steamswitch/internal/vault/probe"
	"steamswitch/internal/vault/totp"
)

// QuickCheckTimeout bounds one account's cheap check. Two HTTP GETs; anything slower than
// this is a network problem, not a slow answer.
const QuickCheckTimeout = 20 * time.Second

// worseThan orders verdicts so a report can be summarised worst-wins. An account with one
// failing signal is a failing account regardless of how many pass.
var verdictRank = map[string]int{
	VerdictUnknown: 0,
	VerdictOK:      1,
	VerdictWarn:    2,
	VerdictFail:    3,
}

func worst(a, b string) string {
	if verdictRank[b] > verdictRank[a] {
		return b
	}
	return a
}

// QuickCheckInput carries the things the vault does not own itself.
//
// LastUsed comes from the Steam engine's ids.json, which this package deliberately does not
// import: internal/vault must stay free of a dependency on the switching engine so the
// dependency arrow keeps pointing one way.
type QuickCheckInput struct {
	APIKey   string
	LastUsed time.Time
}

// QuickCheck runs every signal that costs nothing but an HTTP GET. It never logs in, never
// consumes a Guard code, and has no side effect on the account — which is what makes it
// safe to run across every account at once.
func QuickCheck(ctx context.Context, steamID64 string, in QuickCheckInput) (HealthReport, error) {
	rep, _, err := quickReport(ctx, steamID64, in)
	if err != nil {
		return HealthReport{}, err
	}
	// recordHealthOnly, not recordHealth: the cheap tier does not test the password, so it must
	// not write CheckFailures at all — it runs without deepMu, and a stale snapshot of the count
	// would clobber a failure a concurrent deep check recorded.
	if err := recordHealthOnly(steamID64, rep); err != nil {
		return rep, err
	}
	return rep, nil
}

// quickReport builds the cheap-signal report without persisting it, and returns the entry it read
// so a caller can record with the entry's own CheckFailures. QuickCheck records it as-is; the
// session-liveness paths (manual and scheduled) merge a session row onto it and record once, so a
// session check leaves stored health as complete as a full check rather than a token-only stub.
func quickReport(ctx context.Context, steamID64 string, in QuickCheckInput) (HealthReport, Entry, error) {
	e, err := entry(steamID64)
	if err != nil {
		return HealthReport{}, Entry{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, QuickCheckTimeout)
	defer cancel()

	rep := HealthReport{ProbedAt: time.Now().UTC().Format(time.RFC3339), Verdict: VerdictUnknown}
	add := func(s Signal) {
		rep.Signals = append(rep.Signals, s)
		rep.Verdict = worst(rep.Verdict, s.Status)
	}

	add(bansSignal(ctx, in.APIKey, steamID64))
	add(profileSignal(ctx, in.APIKey, steamID64))
	add(tokenSignal(e))
	add(totpSignal(e))
	add(lastUsedSignal(in.LastUsed))

	return rep, e, nil
}

func bansSignal(ctx context.Context, apiKey, steamID64 string) Signal {
	s := Signal{Name: SignalBans, Status: VerdictUnknown, Detail: "Vault_Signal_BansUnknown"}
	bans, err := probe.FetchBans(ctx, apiKey, steamID64)
	if err != nil {
		if errors.Is(err, probe.ErrNoAPIKey) {
			s.Detail = "Vault_Signal_NeedsAPIKey"
		} else if errors.Is(err, probe.ErrRateLimited) {
			s.Detail = "Vault_Signal_RateLimited"
		}
		return s
	}
	switch {
	case bans.VACBanned || bans.NumberOfGameBans > 0:
		// A ban is the one signal that makes an account useless rather than merely
		// suspect, so it is the only one that blocks.
		s.Status, s.Blocker, s.Detail = VerdictFail, true, "Vault_Signal_Banned"
		s.Params = map[string]string{
			"vac":  strconv.Itoa(bans.NumberOfVACBans),
			"game": strconv.Itoa(bans.NumberOfGameBans),
			"days": strconv.Itoa(bans.DaysSinceLastBan),
		}
	case bans.CommunityBanned:
		s.Status, s.Detail = VerdictWarn, "Vault_Signal_CommunityBanned"
	case bans.EconomyBan != "" && bans.EconomyBan != "none":
		s.Status, s.Detail = VerdictWarn, "Vault_Signal_EconomyBanned"
		s.Params = map[string]string{"state": bans.EconomyBan}
	default:
		s.Status, s.Detail = VerdictOK, "Vault_Signal_NoBans"
	}
	return s
}

func profileSignal(ctx context.Context, apiKey, steamID64 string) Signal {
	s := Signal{Name: SignalProfile, Status: VerdictUnknown, Detail: "Vault_Signal_ProfileUnknown"}
	sum, err := probe.FetchSummary(ctx, apiKey, steamID64)
	if err != nil {
		if errors.Is(err, probe.ErrNoAPIKey) {
			s.Detail = "Vault_Signal_NeedsAPIKey"
		} else if errors.Is(err, probe.ErrRateLimited) {
			s.Detail = "Vault_Signal_RateLimited"
		}
		return s
	}
	if sum.TimeCreated > 0 {
		age := time.Since(time.Unix(sum.TimeCreated, 0))
		days := int(age.Hours() / 24)
		if days < probe.LimitedAccountAgeDays {
			// Steam exposes no "limited" flag, so this is inference — and it is labelled
			// as inference in the message rather than asserted as fact.
			s.Status = VerdictWarn
			s.Detail = "Vault_Signal_LikelyLimited"
			s.Params = map[string]string{"days": strconv.Itoa(days)}
			return s
		}
		s.Status, s.Detail = VerdictOK, "Vault_Signal_ProfileOK"
		s.Params = map[string]string{"days": strconv.Itoa(days)}
		return s
	}
	s.Status, s.Detail = VerdictOK, "Vault_Signal_ProfileOK"
	return s
}

// TokenWarnWindow is how close to expiry a stored token has to be before it is worth
// mentioning. Observed validity is months, so a week's warning is ample.
const TokenWarnWindow = 7 * 24 * time.Hour

func tokenSignal(e Entry) Signal {
	s := Signal{Name: SignalToken, Status: VerdictUnknown, Detail: "Vault_Signal_NoToken"}
	if e.RefreshToken == "" {
		return s
	}
	claims, err := probe.DecodeToken(e.RefreshToken)
	if err != nil {
		s.Status, s.Detail = VerdictWarn, "Vault_Signal_TokenUnreadable"
		return s
	}
	now := time.Now()
	switch {
	case claims.Expired(now):
		s.Status, s.Detail = VerdictWarn, "Vault_Signal_TokenExpired"
	case !claims.ExpiresAt.IsZero() && claims.ExpiresAt.Sub(now) < TokenWarnWindow:
		s.Status, s.Detail = VerdictWarn, "Vault_Signal_TokenExpiringSoon"
		s.Params = map[string]string{"days": strconv.Itoa(int(claims.ExpiresAt.Sub(now).Hours() / 24))}
	default:
		s.Status, s.Detail = VerdictOK, "Vault_Signal_TokenValid"
		if !claims.ExpiresAt.IsZero() {
			s.Params = map[string]string{"days": strconv.Itoa(int(claims.ExpiresAt.Sub(now).Hours() / 24))}
		}
	}
	return s
}

func totpSignal(e Entry) Signal {
	s := Signal{Name: SignalTOTP, Status: VerdictUnknown}
	switch {
	case e.SharedSecret == "":
		// Not having an authenticator seed is a missing convenience, not a problem with
		// the account. Reporting it as a warning would make healthy accounts look sick.
		s.Status, s.Detail = VerdictUnknown, "Vault_Signal_NoAuthenticator"
	case !totp.Valid(e.SharedSecret):
		s.Status, s.Detail = VerdictWarn, "Vault_Signal_BadAuthenticatorSeed"
	default:
		s.Status, s.Detail = VerdictOK, "Vault_Signal_AuthenticatorReady"
	}
	return s
}

// IdleWarnWindow is how long an account can go untouched before it is worth flagging. Idle
// smurfs are the ones that surprise you.
const IdleWarnWindow = 90 * 24 * time.Hour

func lastUsedSignal(lastUsed time.Time) Signal {
	s := Signal{Name: SignalLastUsed, Status: VerdictUnknown, Detail: "Vault_Signal_NeverUsed"}
	if lastUsed.IsZero() {
		return s
	}
	idle := time.Since(lastUsed)
	s.Params = map[string]string{"days": strconv.Itoa(int(idle.Hours() / 24))}
	if idle > IdleWarnWindow {
		s.Status, s.Detail = VerdictWarn, "Vault_Signal_Idle"
		return s
	}
	s.Status, s.Detail = VerdictOK, "Vault_Signal_RecentlyUsed"
	return s
}

// --- deep check scheduling --------------------------------------------------------------

// DeepCheckInterval is the default gap between deep checks of the same account. Measured in
// days because a deep check emails the user; running one hourly would be indistinguishable
// from an attack on their own account.
const DeepCheckInterval = 14 * 24 * time.Hour

// BackoffBase is the first retry delay after a failed deep check; it doubles per consecutive
// failure up to BackoffMax.
//
// It is deliberately equal to MinDeepCheckInterval rather than shorter. It was six hours,
// which meant the floor the user cannot configure their way below — one check per account per
// day — did not apply to the failure path, and a wrong stored password produced four real
// logins a day indefinitely. A failure is not a reason to check *more* often than the
// configured minimum allows.
const (
	BackoffBase = MinDeepCheckInterval
	BackoffMax  = 30 * 24 * time.Hour
)

// nextEligible computes when an account may next be deep-checked. Persisted, so quitting
// the app is not a way to reset the clock.
//
// The gap follows whatever the scheduler is configured with, falling back to
// DeepCheckInterval when it is off — a manual check should record a sane schedule either
// way. A failure ignores the configured interval entirely and uses the backoff instead:
// after something has gone wrong, retrying on the user's chosen cadence is the wrong
// answer regardless of what that cadence is.
func nextEligible(now time.Time, failures int) time.Time {
	return nextEligibleWithin(now, failures, configuredInterval())
}

func nextEligibleWithin(now time.Time, failures int, interval time.Duration) time.Time {
	if failures <= 0 {
		return now.Add(interval)
	}
	d := BackoffBase
	for i := 1; i < failures && d < BackoffMax; i++ {
		d *= 2
	}
	if d > BackoffMax {
		d = BackoffMax
	}
	return now.Add(d)
}

// DueForDeepCheck reports whether an account's persisted schedule allows a deep check now.
func DueForDeepCheck(steamID64 string, now time.Time) bool {
	e, err := entry(steamID64)
	if err != nil {
		return false
	}
	at, ok := eligibleAt(e)
	if !ok {
		return false
	}
	return !now.Before(at)
}
