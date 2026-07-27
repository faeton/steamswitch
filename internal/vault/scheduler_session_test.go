package vault

import (
	"context"
	"errors"
	"testing"
	"time"
)

// sessionConfirmedLive must accept only an explicitly-live session signal. Anything else —
// a failing signal, an unknown one, or no session signal at all — is not a confirmation, and
// treating it as one is what would let the scheduler skip a deep check it should have run.
func TestSessionConfirmedLive(t *testing.T) {
	cases := []struct {
		name string
		rep  HealthReport
		want bool
	}{
		{
			name: "live session",
			rep:  HealthReport{Signals: []Signal{{Name: SignalSession, Status: VerdictOK}}},
			want: true,
		},
		{
			name: "revoked session is not live",
			rep:  HealthReport{Signals: []Signal{{Name: SignalSession, Status: VerdictFail, Blocker: true}}},
			want: false,
		},
		{
			name: "unknown session is not live",
			rep:  HealthReport{Signals: []Signal{{Name: SignalSession, Status: VerdictUnknown}}},
			want: false,
		},
		{
			name: "a passing token signal is not a session confirmation",
			rep:  HealthReport{Signals: []Signal{{Name: SignalToken, Status: VerdictOK}}},
			want: false,
		},
		{
			name: "no signals",
			rep:  HealthReport{},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sessionConfirmedLive(tc.rep); got != tc.want {
				t.Fatalf("sessionConfirmedLive = %v, want %v", got, tc.want)
			}
		})
	}
}

// An account with no stored token has nothing to probe, so the session-first path must decline
// without reaching the network and leave the account to fall through to the deep check. This is
// the one branch that is safe to exercise without a live Steam CM — a token-bearing account
// would attempt a real sign-in.
func TestSessionCheckSatisfiesSchedule_NoTokenDeclines(t *testing.T) {
	newVault(t)
	resetScheduler(t)

	const id = "76561198000000601"
	if err := Put(Draft{SteamID64: id, AccountName: ptr("acct"), Password: ptr("pw")}); err != nil {
		t.Fatal(err)
	}

	if sessionCheckSatisfiesSchedule(context.Background(), id) {
		t.Fatal("an account with no stored token reported its schedule satisfied by a session check")
	}
	// It must not have written a schedule either: a decline is "fall through to the deep check",
	// not "silently mark this account handled".
	e, err := entry(id)
	if err != nil {
		t.Fatal(err)
	}
	if e.NextEligibleAt != "" {
		t.Fatalf("a declined session check advanced the schedule to %q; it must leave it untouched", e.NextEligibleAt)
	}
}

// An account backing off a failed deep check has a suspect password. The session-first path must
// decline it — a live token must never defer (or erase the backoff of) a password that Steam has
// already rejected. It declines before any network call, so this is safe to run offline.
func TestSessionCheckSatisfiesSchedule_BackingOffDeclines(t *testing.T) {
	newVault(t)
	resetScheduler(t)

	const id = "76561198000000602"
	if err := Put(Draft{SteamID64: id, AccountName: ptr("acct"), Password: ptr("pw")}); err != nil {
		t.Fatal(err)
	}
	// A stored token (so the token check passes) but a live failure count (so backoff applies).
	if err := mutate(func(doc *Doc) error {
		e := doc.Entries[id]
		e.RefreshToken = "tok"
		e.CheckFailures = 2
		doc.Entries[id] = e
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if sessionCheckSatisfiesSchedule(context.Background(), id) {
		t.Fatal("a backing-off account was session-skipped; its failed password must be re-verified, not deferred")
	}
}

// Once a live token has stood in for MaxSessionSkips deep checks in a row, the next one must be a
// real credential login, so a wrong password behind a perpetually-live token is still caught.
// Also declines before any network call.
func TestSessionCheckSatisfiesSchedule_SkipCapDeclines(t *testing.T) {
	newVault(t)
	resetScheduler(t)

	const id = "76561198000000603"
	if err := Put(Draft{SteamID64: id, AccountName: ptr("acct"), Password: ptr("pw")}); err != nil {
		t.Fatal(err)
	}
	if err := mutate(func(doc *Doc) error {
		e := doc.Entries[id]
		e.RefreshToken = "tok"
		e.SessionSkips = MaxSessionSkips
		doc.Entries[id] = e
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if sessionCheckSatisfiesSchedule(context.Background(), id) {
		t.Fatal("the deferral cap did not force a real deep check; a live token deferred verification forever")
	}
}

// recordSessionSkip is the write a live-session skip makes: it advances the schedule like a passed
// check, keeps CheckFailures at zero, and increments the deferral counter. resetSessionSkips (run
// by a real deep check) clears that counter so skipping can resume.
func TestRecordSessionSkip_AdvancesAndCounts(t *testing.T) {
	newVault(t)
	resetScheduler(t)

	const id = "76561198000000604"
	if err := Put(Draft{SteamID64: id, AccountName: ptr("acct"), Password: ptr("pw")}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	next := now.Add(DeepCheckInterval)
	rep := HealthReport{ProbedAt: now.Format(time.RFC3339), Verdict: VerdictOK}

	for i := 1; i <= 2; i++ {
		if err := recordSessionSkip(id, rep, next); err != nil {
			t.Fatal(err)
		}
		e, err := entry(id)
		if err != nil {
			t.Fatal(err)
		}
		if e.SessionSkips != i {
			t.Fatalf("after %d skip(s) SessionSkips = %d, want %d", i, e.SessionSkips, i)
		}
		if e.CheckFailures != 0 {
			t.Fatalf("a session skip left CheckFailures = %d, want 0", e.CheckFailures)
		}
		if e.NextEligibleAt == "" {
			t.Fatal("a session skip did not advance the schedule")
		}
	}

	if err := resetSessionSkips(id); err != nil {
		t.Fatal(err)
	}
	e, err := entry(id)
	if err != nil {
		t.Fatal(err)
	}
	if e.SessionSkips != 0 {
		t.Fatalf("resetSessionSkips left SessionSkips = %d, want 0", e.SessionSkips)
	}
}

// recordSessionSkip re-checks eligibility under the write lock, so a failure or a hit cap that
// appeared after the scheduler read the entry is never overwritten. This is the second line of
// defence behind holding deepMu across the whole transaction.
func TestRecordSessionSkip_RefusesSupersededState(t *testing.T) {
	newVault(t)
	resetScheduler(t)

	const id = "76561198000000605"
	if err := Put(Draft{SteamID64: id, AccountName: ptr("acct"), Password: ptr("pw")}); err != nil {
		t.Fatal(err)
	}
	// A deep check recorded a failure since the probe started.
	if err := recordHealth(id, HealthReport{Verdict: VerdictFail}, time.Now().Add(BackoffBase), 1); err != nil {
		t.Fatal(err)
	}

	err := recordSessionSkip(id, HealthReport{Verdict: VerdictOK}, time.Now().Add(DeepCheckInterval))
	if !errors.Is(err, errSkipSuperseded) {
		t.Fatalf("recordSessionSkip overwrote a fresh failure; got err = %v, want errSkipSuperseded", err)
	}
	// The failure and its backoff must be intact.
	e, err := entry(id)
	if err != nil {
		t.Fatal(err)
	}
	if e.CheckFailures != 1 {
		t.Fatalf("CheckFailures = %d after a refused skip, want the deep check's 1", e.CheckFailures)
	}
	if e.SessionSkips != 0 {
		t.Fatalf("a refused skip still bumped SessionSkips to %d", e.SessionSkips)
	}
}

// The cheap and session checks record with recordHealthOnly, which must leave the deep check's
// failure/backoff state alone — they run without deepMu, so writing a stale CheckFailures is how a
// concurrent quick check could erase a deep check's failure and re-open password deferral.
func TestRecordHealthOnly_PreservesFailureState(t *testing.T) {
	newVault(t)
	resetScheduler(t)

	const id = "76561198000000606"
	if err := Put(Draft{SteamID64: id, AccountName: ptr("acct"), Password: ptr("pw")}); err != nil {
		t.Fatal(err)
	}
	// A deep check recorded a failure + backoff; a session skip bumped the counter earlier.
	backoff := time.Now().Add(BackoffBase).UTC().Format(time.RFC3339)
	if err := mutate(func(doc *Doc) error {
		e := doc.Entries[id]
		e.CheckFailures = 2
		e.SessionSkips = 1
		e.NextEligibleAt = backoff
		doc.Entries[id] = e
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := recordHealthOnly(id, HealthReport{Verdict: VerdictOK, ProbedAt: "now"}); err != nil {
		t.Fatal(err)
	}
	e, err := entry(id)
	if err != nil {
		t.Fatal(err)
	}
	if e.CheckFailures != 2 {
		t.Fatalf("recordHealthOnly changed CheckFailures to %d; a cheap/session check must not touch it", e.CheckFailures)
	}
	if e.SessionSkips != 1 {
		t.Fatalf("recordHealthOnly changed SessionSkips to %d; it must leave it alone", e.SessionSkips)
	}
	if e.NextEligibleAt != backoff {
		t.Fatalf("recordHealthOnly moved the schedule to %q; it must not advance it", e.NextEligibleAt)
	}
	if e.Health == nil || e.Health.Verdict != VerdictOK {
		t.Fatal("recordHealthOnly did not store the new report")
	}
}
