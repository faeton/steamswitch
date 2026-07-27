package vault

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

	"steamswitch/internal/appclient"
	"steamswitch/internal/crashlog"
	"steamswitch/internal/security"
)

// The scheduler is the only thing in this package that acts without the user asking for it,
// and it acts by logging in to somebody's Steam account. Every constraint below comes from
// VAULT.md §5.3, and each one is the difference between a health feature and something Valve
// cannot distinguish from an attack:
//
//   - opt-in, off by default;
//   - one account in flight, process-wide — never a fan-out;
//   - eligibility persisted per entry, so quitting is not a way to reset the clock;
//   - a hard stop on rate limiting, with no automatic recovery;
//   - never at launch.
const (
	// SchedulerStartDelay keeps the first tick well clear of startup. Launch runs quick
	// checks only; a deep check at launch would make this an app that emails you a Steam
	// Guard code every time you open it.
	SchedulerStartDelay = 10 * time.Minute

	// SchedulerTick is how often the scheduler looks for work, not how often an account is
	// checked — eligibility is per-account and measured in days. A short tick costs nothing
	// when nothing is due, and it means an account that becomes due while the app is open
	// does not wait until the next launch.
	SchedulerTick = 30 * time.Minute

	// MinDeepCheckInterval floors whatever the user configures. The first check of an
	// account emails them a Guard code (later ones reuse the stored guard_data), so a
	// configuration of "every hour" would be a way to mail-bomb yourself.
	MinDeepCheckInterval = 24 * time.Hour
)

// deepInterval is the gap the scheduler enforces between deep checks of one account. Zero
// means the scheduler is off — nextEligible still falls back to DeepCheckInterval so a
// manual check records a sane schedule whether or not the scheduler is running.
var (
	intervalMu   sync.RWMutex
	deepInterval time.Duration
)

func schedulerInterval() time.Duration {
	intervalMu.RLock()
	defer intervalMu.RUnlock()
	return deepInterval
}

// configuredInterval is what nextEligible spaces checks by.
func configuredInterval() time.Duration {
	if d := schedulerInterval(); d > 0 {
		return d
	}
	return DeepCheckInterval
}

var (
	schedMu     sync.Mutex
	schedCancel context.CancelFunc
)

// ConfigureScheduler applies the user's setting. An interval of zero or less turns the
// scheduler off and stops it; anything shorter than MinDeepCheckInterval is raised to it.
//
// Safe to call repeatedly. A running loop picks up a changed interval on its next tick
// rather than being torn down, so changing the setting cannot be used to skip the start
// delay.
func ConfigureScheduler(interval time.Duration) {
	switch {
	case interval <= 0:
		// Normalised rather than stored as-is, so "off" has exactly one representation and
		// a stray negative cannot read as an interval anywhere downstream.
		interval = 0
	case interval < MinDeepCheckInterval:
		interval = MinDeepCheckInterval
	}
	intervalMu.Lock()
	deepInterval = interval
	intervalMu.Unlock()

	schedMu.Lock()
	defer schedMu.Unlock()
	if interval <= 0 {
		stopSchedulerLocked()
		return
	}
	if schedCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	schedCancel = cancel
	go runScheduler(ctx)
	slog.Info("vault: scheduled deep checks enabled", "interval", interval)
}

// StopScheduler halts the loop. Used on shutdown and by tests.
func StopScheduler() {
	schedMu.Lock()
	defer schedMu.Unlock()
	stopSchedulerLocked()
}

func stopSchedulerLocked() {
	if schedCancel != nil {
		schedCancel()
		schedCancel = nil
	}
}

func runScheduler(ctx context.Context) {
	defer crashlog.Capture()

	select {
	case <-ctx.Done():
		return
	case <-time.After(SchedulerStartDelay):
	}

	t := time.NewTicker(SchedulerTick)
	defer t.Stop()
	for {
		if !schedulerStep(ctx) {
			// The step asked to stop for good. Clear the handle so a later
			// ConfigureScheduler can start a fresh loop if the setting changes — the
			// rate-limit latch itself still refuses, which is what makes the stop hard.
			schedMu.Lock()
			stopSchedulerLocked()
			schedMu.Unlock()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// schedulerStep runs at most one deep check and reports whether the loop should continue.
// A false is the hard stop after a rate limit: nothing but a restart clears it.
func schedulerStep(ctx context.Context) bool {
	if RateLimited() {
		slog.Warn("vault: scheduler stopping, Steam has rate-limited this client")
		return false
	}
	// Each of these is "not now", not "not ever": the setting can be turned back on, the app
	// unlocked, the network reconnected, and the next tick picks it up.
	if schedulerInterval() <= 0 {
		return true
	}
	// The same gate every other flow that touches account data takes. A locked app must not
	// log in to a Steam account on its own; nextDue's load() also fails closed when there is
	// no master key at all, which is the case this call does not cover.
	if err := security.RequireUnlocked(); err != nil {
		return true
	}
	if appclient.IsOfflineMode() {
		return true
	}

	id, ok := nextDue(time.Now())
	if !ok {
		return true
	}

	// Cheap before expensive: an account due for a deep check that still has a stored session
	// token gets a brief CM sign-in first. If Steam still honours the token the account is
	// reachable, we advance the schedule and skip the credential login — no Guard email spent.
	// Only a token that is gone, revoked, or unconfirmable falls through to the deep check.
	if sessionCheckSatisfiesSchedule(ctx, id) {
		return !RateLimited()
	}
	// The session probe shares Steam's login rate limit and may have tripped the latch. Stop
	// here rather than walking into a deep check that would only draw on the same limit.
	if RateLimited() {
		slog.Warn("vault: scheduler stopping, Steam rate-limited the session probe")
		return false
	}

	rep, err := DeepCheck(ctx, id, checkInput(id))
	switch {
	case err == nil:
		slog.Info("vault: scheduled deep check finished", "steamId64", id, "verdict", rep.Verdict)
	case errors.Is(err, ErrDeepCheckBusy):
		// The user started one by hand. Theirs wins; this account is still due next tick.
		return true
	default:
		// Deliberately not the error text: it can quote Steam's own message, which may
		// contain the account name.
		slog.Warn("vault: scheduled deep check could not run", "steamId64", id)
		// A check that did not advance the account's schedule leaves it due again in thirty
		// minutes — and again, and again, each one a real login. Rather than trust that every
		// failure path persisted a backoff, confirm it did, and stop the scheduler if not.
		// An unwritable vault is not a condition to keep logging in through.
		if !DueForDeepCheck(id, time.Now()) {
			return !RateLimited()
		}
		slog.Warn("vault: scheduler stopping, the schedule could not be advanced", "steamId64", id)
		return false
	}
	return !RateLimited()
}

// MaxSessionSkips bounds how many scheduled deep checks in a row a live session token may stand
// in for before a real credential login is forced. A live token means the account is reachable,
// so re-verifying the password every cycle would spend a Guard email to learn something the
// switcher does not need — but a wrong stored password hidden behind a perpetually-live token
// must still be caught eventually, which is what this cap guarantees (at most this many intervals
// of deferral).
const MaxSessionSkips = 3

// sessionCheckSatisfiesSchedule runs the cheap session probe for a due account and, when Steam
// still honours the stored token, advances the deep-check schedule so the credential login is
// skipped this cycle. It returns true only when it ran the probe, confirmed a live session, and
// recorded that progress.
//
// It declines (returns false, so the caller falls through to the deep check) in every other case:
//   - no stored token — nothing to probe;
//   - the account is backing off a failed deep check (CheckFailures > 0) — a suspect password
//     must be re-verified, never deferred behind a live token, and its backoff must not be erased;
//   - the deferral cap is reached (SessionSkips >= MaxSessionSkips) — force a real check so a
//     wrong password behind a perpetually-live token is still caught;
//   - the session is revoked (a fresh token is needed and only the deep path can mint one),
//     unconfirmable (rate limit, CM down, timeout — we learned nothing), or the write failed.
//
// The whole transaction runs under deepMu, held from the eligibility read through the single
// write. That is what makes the skip atomic: nothing else can start a login (a concurrent deep
// check would be the only writer of CheckFailures) or clobber the schedule in the window between
// the probe and the write. It also means exactly one health write happens — the merged report —
// so no thin intermediate report is ever persisted, even on a crash. recordSessionSkip re-checks
// the eligibility under the write lock as a second line of defence.
func sessionCheckSatisfiesSchedule(ctx context.Context, steamID64 string) bool {
	// Fail closed on the rate-limit latch, before and after taking the login lock, as the manual
	// checks do — the probe below is a login and must honour the hard stop.
	if RateLimited() {
		return false
	}
	if !deepMu.TryLock() {
		// A manual check holds the login slot; let the caller fall through — its DeepCheck will
		// find the slot busy too and report it, and this account is still due next tick.
		return false
	}
	defer deepMu.Unlock()
	if RateLimited() {
		return false
	}

	e, err := entry(steamID64)
	if err != nil || e.RefreshToken == "" {
		return false
	}
	if e.CheckFailures > 0 || e.SessionSkips >= MaxSessionSkips {
		return false
	}

	// One report, cheap signals plus the session probe, built without persisting anything.
	// buildSessionHealth latches (and skips the CM login) on a cheap-tier 429, so a rate-limited
	// tick lands here as a non-live session and is caught by the confirmed-live gate below; the
	// outer schedulerStep's RateLimited() check then makes it a hard stop rather than a deep login.
	rep := buildSessionHealth(ctx, steamID64, e)
	if !sessionConfirmedLive(rep) {
		// Revoked, or unconfirmable (rate limit, CM down, timeout). Record nothing here — let the
		// deep path run and record a full result — so a non-live tick never overwrites stored health
		// with a report that lacks a credential verdict.
		return false
	}

	if err := recordSessionSkip(steamID64, rep, nextEligible(time.Now(), 0)); err != nil {
		if !errors.Is(err, errSkipSuperseded) {
			slog.Warn("vault: could not record the session-check schedule", "steamId64", steamID64)
		}
		return false
	}
	slog.Info("vault: scheduled session check confirmed a live token; deferring the credential login", "steamId64", steamID64, "skips", e.SessionSkips+1)
	return true
}

// sessionConfirmedLive reports whether the report's session signal came back explicitly live —
// not merely non-failing. An absent or unknown session signal is not a confirmation, so the
// caller must not treat it as one and skip the deep check on it.
func sessionConfirmedLive(rep HealthReport) bool {
	for _, s := range rep.Signals {
		if s.Name == SignalSession {
			return s.Status == VerdictOK
		}
	}
	return false
}

// nextDue picks the single account most overdue for a deep check.
//
// One per tick, never a batch. Entries with no stored credentials are skipped rather than
// attempted: DeepCheck would refuse them, and an account that is refused every tick forever
// is not a schedule, it is a loop that never advances.
func nextDue(now time.Time) (string, bool) {
	doc, err := load()
	if err != nil {
		return "", false
	}

	type candidate struct {
		id string
		at time.Time
	}
	var due []candidate
	for id, e := range doc.Entries {
		if e.Password == "" || e.AccountName == "" {
			continue
		}
		at, ok := effectiveEligibility(e, schedulerInterval())
		if !ok || now.Before(at) {
			continue
		}
		due = append(due, candidate{id: id, at: at})
	}
	if len(due) == 0 {
		return "", false
	}
	// Oldest eligibility first, then by id. The tie-break is not cosmetic: entries that have
	// never been checked all share the zero time, and Go's map order would otherwise make
	// which one goes first differ from tick to tick.
	sort.Slice(due, func(i, j int) bool {
		if due[i].at.Equal(due[j].at) {
			return due[i].id < due[j].id
		}
		return due[i].at.Before(due[j].at)
	})
	return due[0].id, true
}

// effectiveEligibility applies the *current* cadence to a timestamp that was computed under
// whatever the cadence was at the time of the last check.
//
// Without this, lengthening the interval did nothing to accounts already scheduled: set one
// day, let an account be checked, then change to thirty days, and it would still be logged
// into a day later — while the setting said "how many days apart" and the code claimed the
// change took effect immediately.
//
// The correction only ever pushes a check *later*. Shortening the interval does not pull one
// forward, and a backing-off entry is left alone entirely — in both of those directions the
// safe answer is to wait, and this is a feature whose unit of error is a real login.
func effectiveEligibility(e Entry, interval time.Duration) (time.Time, bool) {
	at, ok := eligibleAt(e)
	if !ok || interval <= 0 || e.CheckFailures > 0 || e.Health == nil {
		return at, ok
	}
	probed, err := time.Parse(time.RFC3339, e.Health.ProbedAt)
	if err != nil {
		return at, ok
	}
	if next := probed.Add(interval); next.After(at) {
		return next, true
	}
	return at, ok
}

// eligibleAt reads an entry's persisted schedule. The second return is whether the entry may
// be considered at all: an unparseable timestamp means no, because failing open there would
// turn one corrupt field into a login attempt every tick.
func eligibleAt(e Entry) (time.Time, bool) {
	if e.NextEligibleAt == "" {
		return time.Time{}, true
	}
	t, err := time.Parse(time.RFC3339, e.NextEligibleAt)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
