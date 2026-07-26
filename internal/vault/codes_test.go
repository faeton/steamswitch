package vault

import (
	"testing"
	"time"
)

// A pre-warmed code is consumed, not read. Steam codes are single use, so a second caller
// must go and fetch a fresh one rather than be handed the same dead value — which fails in a
// way the user cannot diagnose, because a dead code looks exactly like a live one.
func TestTakePrewarmed_ConsumesOnce(t *testing.T) {
	newVault(t)
	t.Cleanup(resetPrewarmForTest)

	const id = "76561198000000020"
	prewarmMu.Lock()
	prewarms[id] = &prewarm{done: true, code: "4K7BX", startAt: time.Now()}
	prewarmMu.Unlock()

	if code, ok := takePrewarmed(id); !ok || code != "4K7BX" {
		t.Fatalf("first take = %q, %v", code, ok)
	}
	if code, ok := takePrewarmed(id); ok {
		t.Fatalf("second take returned %q; the code was handed out twice", code)
	}
}

// A code fetched long before it is wanted is worse than no code: it will have expired, and
// Steam's rejection gives the user nothing to act on.
func TestTakePrewarmed_RejectsStale(t *testing.T) {
	newVault(t)
	t.Cleanup(resetPrewarmForTest)

	const id = "76561198000000021"
	prewarmMu.Lock()
	prewarms[id] = &prewarm{done: true, code: "5MWGC", startAt: time.Now().Add(-PrewarmLifetime - time.Minute)}
	prewarmMu.Unlock()

	if code, ok := takePrewarmed(id); ok {
		t.Fatalf("a stale pre-warm was handed over: %q", code)
	}
}

// An in-flight fetch is not a result. Returning one would make the status strip claim a code
// is ready when nothing has arrived.
func TestTakePrewarmed_IgnoresInFlight(t *testing.T) {
	newVault(t)
	t.Cleanup(resetPrewarmForTest)

	const id = "76561198000000022"
	prewarmMu.Lock()
	prewarms[id] = &prewarm{startAt: time.Now()}
	prewarmMu.Unlock()

	if _, ok := takePrewarmed(id); ok {
		t.Fatal("an unfinished pre-warm was treated as a result")
	}
	if !PrewarmPending(id) {
		t.Fatal("PrewarmPending did not report the in-flight fetch")
	}
}

// Prewarm must be a no-op for an account with an authenticator seed: the code is computed in
// microseconds on demand, and pre-computing one only risks handing over an expired window.
func TestPrewarm_SkipsAccountsWithASeed(t *testing.T) {
	newVault(t)
	t.Cleanup(resetPrewarmForTest)

	const id = "76561198000000023"
	if err := Put(Draft{
		SteamID64:    id,
		SharedSecret: ptr("MTIzNDU2Nzg5MDEyMzQ1Njc4OTA="),
		EmailSource:  ptr(EmailSourceIMAP),
		IMAPHost:     ptr("imap.invalid.test"),
		IMAPUser:     ptr("u"),
		IMAPPassword: ptr("p"),
	}); err != nil {
		t.Fatal(err)
	}

	Prewarm(id)
	if PrewarmPending(id) {
		t.Fatal("a pre-warm was started for an account that generates its own codes")
	}
}

// Prewarm must not start anything for an account with no code source at all — including the
// common case of an account with no vault entry, which every switch calls it for.
func TestPrewarm_NoSourceIsANoOp(t *testing.T) {
	newVault(t)
	t.Cleanup(resetPrewarmForTest)

	Prewarm("76561198000000024") // no entry at all
	if PrewarmPending("76561198000000024") {
		t.Fatal("a pre-warm was started for an account with no vault entry")
	}

	const id = "76561198000000025"
	if err := Put(Draft{SteamID64: id, Password: ptr("x")}); err != nil {
		t.Fatal(err)
	}
	Prewarm(id)
	if PrewarmPending(id) {
		t.Fatal("a pre-warm was started for an account with no code source")
	}
}

func TestCancelPrewarm(t *testing.T) {
	newVault(t)
	t.Cleanup(resetPrewarmForTest)

	const id = "76561198000000026"
	prewarmMu.Lock()
	prewarms[id] = &prewarm{startAt: time.Now()}
	prewarmMu.Unlock()

	CancelPrewarm(id)
	if PrewarmPending(id) {
		t.Fatal("the fetch survived a cancel")
	}
}

// The deep-check schedule is persisted so quitting the app cannot reset the clock, and it
// backs off after failures because retrying a credential login on a timer is what gets an
// account rate-limited out of its own logins.
func TestNextEligible_BacksOff(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)

	if got := nextEligible(now, 0); !got.Equal(now.Add(DeepCheckInterval)) {
		t.Fatalf("after success = %v, want +%v", got, DeepCheckInterval)
	}

	first := nextEligible(now, 1).Sub(now)
	second := nextEligible(now, 2).Sub(now)
	third := nextEligible(now, 3).Sub(now)
	if !(first < second && second < third) {
		t.Fatalf("backoff is not increasing: %v, %v, %v", first, second, third)
	}
	if got := nextEligible(now, 50).Sub(now); got != BackoffMax {
		t.Fatalf("backoff after many failures = %v, want it capped at %v", got, BackoffMax)
	}
}

func TestDueForDeepCheck(t *testing.T) {
	newVault(t)
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)

	const id = "76561198000000027"
	if err := Put(Draft{SteamID64: id, Password: ptr("x")}); err != nil {
		t.Fatal(err)
	}
	if !DueForDeepCheck(id, now) {
		t.Fatal("an account never checked is not due")
	}

	rep := HealthReport{ProbedAt: now.Format(time.RFC3339), Verdict: VerdictOK}
	if err := recordHealth(id, rep, now.Add(time.Hour), 0); err != nil {
		t.Fatal(err)
	}
	if DueForDeepCheck(id, now) {
		t.Fatal("an account scheduled for later reported as due")
	}
	if !DueForDeepCheck(id, now.Add(2*time.Hour)) {
		t.Fatal("an account past its schedule is not due")
	}

	// An unparseable timestamp must read as "not due". Failing open would let one corrupt
	// field turn into a credential login on every launch.
	if err := Put(Draft{SteamID64: id}); err != nil {
		t.Fatal(err)
	}
	if err := mutate(func(doc *Doc) error {
		e := doc.Entries[id]
		e.NextEligibleAt = "not a timestamp"
		doc.Entries[id] = e
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if DueForDeepCheck(id, now) {
		t.Fatal("a corrupt schedule read as due; that is one login attempt per launch")
	}

	if DueForDeepCheck("76561198999999999", now) {
		t.Fatal("an account with no vault entry reported as due for a credential login")
	}
}

// The rate-limit latch must survive until a restart. Clearing it automatically is how a
// warning from Steam becomes a block.
func TestRateLimitLatch(t *testing.T) {
	ResetRateLimitForTest()
	t.Cleanup(ResetRateLimitForTest)

	if RateLimited() {
		t.Fatal("rate-limited before anything happened")
	}
	markRateLimited()
	if !RateLimited() {
		t.Fatal("the latch did not hold")
	}
}
