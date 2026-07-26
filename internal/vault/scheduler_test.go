package vault

import (
	"context"
	"testing"
	"time"

	"steamswitch/internal/appclient"
	"steamswitch/internal/security"
)

// resetScheduler puts the package-level scheduler state back to "off" so one test's interval
// cannot change what another test's nextEligible computes.
func resetScheduler(t *testing.T) {
	t.Helper()
	StopScheduler()
	intervalMu.Lock()
	deepInterval = 0
	intervalMu.Unlock()
	t.Cleanup(func() {
		StopScheduler()
		intervalMu.Lock()
		deepInterval = 0
		intervalMu.Unlock()
	})
}

func TestConfigureScheduler_ClampsAndDisables(t *testing.T) {
	resetScheduler(t)

	// Off is the default and the only value that stops the loop.
	ConfigureScheduler(0)
	if got := schedulerInterval(); got != 0 {
		t.Fatalf("interval after ConfigureScheduler(0) = %v, want 0", got)
	}
	ConfigureScheduler(-time.Hour)
	if got := schedulerInterval(); got != 0 {
		t.Fatalf("a negative interval did not read as off: %v", got)
	}

	// Anything the user can express that is shorter than the floor is raised to it. An
	// hourly credential login is a way to mail-bomb yourself with Guard codes.
	ConfigureScheduler(time.Hour)
	if got := schedulerInterval(); got != MinDeepCheckInterval {
		t.Fatalf("interval = %v, want it clamped to %v", got, MinDeepCheckInterval)
	}

	ConfigureScheduler(21 * 24 * time.Hour)
	if got := schedulerInterval(); got != 21*24*time.Hour {
		t.Fatalf("interval = %v, want the configured 21 days", got)
	}
}

// Turning the scheduler off has to stop the loop, not merely make its steps no-ops. The
// handle is the observable part of that.
func TestConfigureScheduler_StopClearsTheHandle(t *testing.T) {
	resetScheduler(t)

	ConfigureScheduler(7 * 24 * time.Hour)
	schedMu.Lock()
	running := schedCancel != nil
	schedMu.Unlock()
	if !running {
		t.Fatal("enabling the scheduler did not start a loop")
	}

	ConfigureScheduler(0)
	schedMu.Lock()
	running = schedCancel != nil
	schedMu.Unlock()
	if running {
		t.Fatal("disabling the scheduler left the loop running")
	}
}

// The configured cadence is what a successful check schedules by; a *failed* one ignores it
// and uses the backoff instead. Retrying on the user's chosen cadence after something went
// wrong is the behaviour the backoff exists to prevent.
func TestNextEligible_FollowsTheConfiguredInterval(t *testing.T) {
	resetScheduler(t)
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)

	if got := nextEligible(now, 0); !got.Equal(now.Add(DeepCheckInterval)) {
		t.Fatalf("with the scheduler off = %v, want the default %v", got.Sub(now), DeepCheckInterval)
	}

	ConfigureScheduler(3 * 24 * time.Hour)
	if got := nextEligible(now, 0); !got.Equal(now.Add(3 * 24 * time.Hour)) {
		t.Fatalf("with a 3-day cadence = %v, want 72h", got.Sub(now))
	}
	if got := nextEligible(now, 1); !got.Equal(now.Add(BackoffBase)) {
		t.Fatalf("after a failure = %v, want the backoff %v", got.Sub(now), BackoffBase)
	}
}

func TestNextDue(t *testing.T) {
	newVault(t)
	resetScheduler(t)
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)

	const (
		overdue     = "76561198000000101"
		mostOverdue = "76561198000000102"
		future      = "76561198000000103"
		noPassword  = "76561198000000104"
		noName      = "76561198000000105"
	)
	for _, id := range []string{overdue, mostOverdue, future} {
		if err := Put(Draft{SteamID64: id, AccountName: ptr("acct" + id[len(id)-3:]), Password: ptr("pw")}); err != nil {
			t.Fatal(err)
		}
	}
	// An entry with no credentials cannot be logged in with, so it must never be selected:
	// DeepCheck would refuse it, and being refused every tick forever is not a schedule.
	if err := Put(Draft{SteamID64: noPassword, AccountName: ptr("no_password")}); err != nil {
		t.Fatal(err)
	}
	if err := Put(Draft{SteamID64: noName, Password: ptr("pw")}); err != nil {
		t.Fatal(err)
	}

	schedule := func(id string, at time.Time) {
		t.Helper()
		if err := recordHealth(id, HealthReport{ProbedAt: now.Format(time.RFC3339), Verdict: VerdictOK}, at, 0); err != nil {
			t.Fatal(err)
		}
	}
	schedule(overdue, now.Add(-time.Hour))
	schedule(mostOverdue, now.Add(-48*time.Hour))
	schedule(future, now.Add(24*time.Hour))
	schedule(noPassword, now.Add(-72*time.Hour))
	schedule(noName, now.Add(-72*time.Hour))

	got, ok := nextDue(now)
	if !ok {
		t.Fatal("nothing was due when two accounts were overdue")
	}
	if got != mostOverdue {
		t.Fatalf("nextDue = %s, want the most overdue account %s", got, mostOverdue)
	}

	// Once the overdue ones are pushed out, nothing is due — the future-scheduled account
	// must not be pulled forward.
	schedule(overdue, now.Add(24*time.Hour))
	schedule(mostOverdue, now.Add(24*time.Hour))
	if id, ok := nextDue(now); ok {
		t.Fatalf("nextDue = %s with everything scheduled ahead; want nothing due", id)
	}
}

// Entries that have never been checked share the zero time, so without an explicit tie-break
// Go's map order decides which one is picked and it differs from tick to tick.
func TestNextDue_IsDeterministic(t *testing.T) {
	newVault(t)
	resetScheduler(t)
	now := time.Now()

	for _, id := range []string{"76561198000000203", "76561198000000201", "76561198000000202"} {
		if err := Put(Draft{SteamID64: id, AccountName: ptr("a" + id), Password: ptr("pw")}); err != nil {
			t.Fatal(err)
		}
	}
	for range 8 {
		got, ok := nextDue(now)
		if !ok {
			t.Fatal("nothing due when three fresh entries exist")
		}
		if got != "76561198000000201" {
			t.Fatalf("nextDue = %s, want the lowest id; the tie-break is not holding", got)
		}
	}
}

// A corrupt schedule must read as "not due", matching DueForDeepCheck. Failing open here
// would turn one unparseable field into a login attempt every tick.
func TestNextDue_SkipsACorruptSchedule(t *testing.T) {
	newVault(t)
	resetScheduler(t)

	const id = "76561198000000301"
	if err := Put(Draft{SteamID64: id, AccountName: ptr("acct"), Password: ptr("pw")}); err != nil {
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
	if got, ok := nextDue(time.Now()); ok {
		t.Fatalf("nextDue = %s for a corrupt schedule; want nothing", got)
	}
}

// Every one of these guards has to hold before nextDue is even consulted, because past that
// point the next thing that happens is a real login.
func TestSchedulerStep_Guards(t *testing.T) {
	newVault(t)
	resetScheduler(t)
	ResetRateLimitForTest()
	t.Cleanup(ResetRateLimitForTest)

	// A due account with full credentials, so any guard that fails open would immediately
	// try to reach Steam.
	if err := Put(Draft{SteamID64: "76561198000000401", AccountName: ptr("acct"), Password: ptr("pw")}); err != nil {
		t.Fatal(err)
	}

	// Off: the setting can be turned off between ticks, and the running loop has to notice.
	ConfigureScheduler(0)
	if !schedulerStep(context.Background()) {
		t.Fatal("a disabled scheduler asked to stop for good; it should idle instead")
	}

	ConfigureScheduler(7 * 24 * time.Hour)

	// Offline mode is a transport-level block, so a check would fail rather than hang — but
	// it must be skipped explicitly, not attempted and reported as a network fault.
	appclient.SetOfflineMode(true)
	if !schedulerStep(context.Background()) {
		t.Fatal("offline mode stopped the scheduler for good; it should idle")
	}
	appclient.SetOfflineMode(false)
	t.Cleanup(func() { appclient.SetOfflineMode(false) })

	// No master key: the vault cannot be opened, so there is nothing to log in with.
	//
	// This stands in for the locked case, which cannot be constructed in-process — there is
	// no "lock now" entry point, the app relocks by relaunching. Both reach the scheduler as
	// the same thing: a vault whose load() fails, which must idle rather than crash or fall
	// through to a login.
	if err := security.RemoveAppPassword("correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	DropCache()
	if _, err := List(); err == nil {
		t.Fatal("the vault is still readable; the rest of this test proves nothing")
	}
	if _, ok := nextDue(time.Now()); ok {
		t.Fatal("an unreadable vault produced a candidate for a credential login")
	}
	if !schedulerStep(context.Background()) {
		t.Fatal("an unreadable vault stopped the scheduler for good; it should idle")
	}
}

// Rate limiting is the one condition that stops the loop rather than idling it. Steam has
// said it has had enough, and the documented way to turn that into a ban is to keep going.
func TestSchedulerStep_RateLimitIsAHardStop(t *testing.T) {
	newVault(t)
	resetScheduler(t)
	ResetRateLimitForTest()
	t.Cleanup(ResetRateLimitForTest)

	ConfigureScheduler(7 * 24 * time.Hour)
	markRateLimited()
	if schedulerStep(context.Background()) {
		t.Fatal("the scheduler carried on after Steam rate-limited it")
	}
}
