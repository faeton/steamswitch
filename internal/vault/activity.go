package vault

import (
	"log/slog"
	"sync"
	"time"
)

// ActivityEvent tells the frontend a check has done something.
const ActivityEvent = "vault-check-activity"

// ActivityStep is one thing a check did. Key and Detail are i18n keys and Params are counts and
// durations, so a step is safe to log, show and copy into a bug report: it can never carry a
// password, a code, a token or Steam's own error text.
type ActivityStep struct {
	// Seq orders steps. At has one-second resolution and events can arrive out of order.
	Seq       uint64            `json:"seq"`
	SteamID64 string            `json:"steamId64"`
	At        string            `json:"at"`
	Key       string            `json:"key"`
	Status    string            `json:"status"`
	Params    map[string]string `json:"params,omitempty"`
}

// maxActivityPerAccount keeps a session's history readable without letting it grow for ever.
const maxActivityPerAccount = 100

var (
	activityMu   sync.Mutex
	activity     = map[string][]ActivityStep{}
	activitySeq  uint64
	activityHook func(ActivityStep)
)

// SetActivityHook connects steps to the UI. Injected from the composition root, like the Guard
// code hook, so this package does not depend on the app framework.
func SetActivityHook(fn func(ActivityStep)) {
	activityMu.Lock()
	defer activityMu.Unlock()
	activityHook = fn
}

// logStep records one step, in memory for the Account health window and in the app log.
func logStep(steamID64, key, status string, params map[string]string) {
	step := ActivityStep{
		SteamID64: normID(steamID64),
		At:        time.Now().UTC().Format(time.RFC3339),
		Key:       key,
		Status:    status,
		Params:    params,
	}
	activityMu.Lock()
	activitySeq++
	step.Seq = activitySeq
	steps := append(activity[step.SteamID64], step)
	if len(steps) > maxActivityPerAccount {
		steps = steps[len(steps)-maxActivityPerAccount:]
	}
	activity[step.SteamID64] = steps
	hook := activityHook
	activityMu.Unlock()

	slog.Info("vault check", "steamId64", step.SteamID64, "step", key, "status", status, "params", params)
	if hook != nil {
		hook(step)
	}
}

// logSignal records what a check concluded, using the same words the report shows.
func logSignal(steamID64 string, s Signal) {
	logStep(steamID64, s.Detail, s.Status, s.Params)
}

// Activity returns what has been done for an account since the app started, oldest first.
func Activity(steamID64 string) []ActivityStep {
	activityMu.Lock()
	defer activityMu.Unlock()
	return append([]ActivityStep{}, activity[normID(steamID64)]...)
}

// moveActivity keeps the history with an entry when it gets its real SteamID.
func moveActivity(from, to string) {
	activityMu.Lock()
	defer activityMu.Unlock()
	activity[to] = append(activity[from], activity[to]...)
	delete(activity, from)
}

// forgetActivity clears everything when the vault locks. Which accounts were checked, and when,
// is part of what a lock is meant to close.
func forgetActivity() {
	activityMu.Lock()
	defer activityMu.Unlock()
	activity = map[string][]ActivityStep{}
}

func (r *HealthReport) hasSignal(name string) bool {
	for _, s := range r.Signals {
		if s.Name == name {
			return true
		}
	}
	return false
}

// setSignal puts a signal in a report, replacing any older one of the same name, and works the
// verdict out again. Recomputing matters: a stale "password rejected" being replaced by a pass
// has to stop counting against the account.
func (r *HealthReport) setSignal(s Signal) {
	kept := r.Signals[:0:0]
	for _, old := range r.Signals {
		if old.Name != s.Name {
			kept = append(kept, old)
		}
	}
	r.Signals = append(kept, s)
	r.Verdict = VerdictUnknown
	for _, sig := range r.Signals {
		r.Verdict = worst(r.Verdict, sig.Status)
	}
}

// carrySignInResults keeps what real sign-ins established across a check that does not sign in.
// A free lookup says nothing about the password, so it must neither erase "password accepted"
// nor make "password rejected" go away. Each keeps the time it was actually learned.
//
// It runs as the report is written, against what is stored at that moment, and never replaces a
// result the report already has. Doing it when a check starts would let a slow lookup write back
// an older sign-in result over one that landed while it ran.
func carrySignInResults(rep *HealthReport, prior *HealthReport) {
	if prior == nil {
		return
	}
	for _, s := range prior.Signals {
		if (s.Name != SignalPassword && s.Name != SignalSession) || s.Status == VerdictUnknown {
			continue
		}
		if rep.hasSignal(s.Name) {
			continue
		}
		if s.At == "" {
			s.At = prior.ProbedAt
		}
		rep.setSignal(s)
	}
}

// dropSignal removes a signal from a report and works the verdict out again.
func (r *HealthReport) dropSignal(name string) {
	kept := r.Signals[:0:0]
	r.Verdict = VerdictUnknown
	for _, s := range r.Signals {
		if s.Name != name {
			kept = append(kept, s)
			r.Verdict = worst(r.Verdict, s.Status)
		}
	}
	r.Signals = kept
}

// forgetSignIn removes a sign-in result that no longer describes the entry, such as "password
// accepted" after the password was changed.
func forgetSignIn(e *Entry, name string) {
	if e.Health == nil {
		return
	}
	kept := e.Health.Signals[:0:0]
	verdict := VerdictUnknown
	for _, s := range e.Health.Signals {
		if s.Name != name {
			kept = append(kept, s)
			verdict = worst(verdict, s.Status)
		}
	}
	e.Health.Signals, e.Health.Verdict = kept, verdict
}
