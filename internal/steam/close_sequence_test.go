package steam

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// fakeSteam models the process table: the client and its helpers, and which request gets a reaction.
type fakeSteam struct {
	client, helpers atomic.Bool
	asked, termed   atomic.Int32
	killed          atomic.Int32
	exitsOnRequest  bool
	exitsOnTerm     bool
	helpersStay     bool
	askErr          error
	survivesKill    bool
}

func (f *fakeSteam) seq() closeSequence {
	return closeSequence{
		clientRunning: f.client.Load,
		anyRunning:    func() bool { return f.client.Load() || f.helpers.Load() },
		askToExit: func() error {
			f.asked.Add(1)
			if f.askErr != nil {
				return f.askErr
			}
			if f.exitsOnRequest {
				f.client.Store(false)
				if !f.helpersStay {
					f.helpers.Store(false)
				}
			}
			return nil
		},
		terminate: func() {
			f.termed.Add(1)
			if f.exitsOnTerm {
				f.client.Store(false)
				f.helpers.Store(false)
			}
		},
		kill: func() {
			f.killed.Add(1)
			if !f.survivesKill {
				f.client.Store(false)
				f.helpers.Store(false)
			}
		},
		exitGrace:   150 * time.Millisecond,
		askSettle:   time.Second,
		helperGrace: 30 * time.Millisecond,
		termGrace:   50 * time.Millisecond,
		poll:        5 * time.Millisecond,
	}
}

func running() *fakeSteam {
	f := &fakeSteam{}
	f.client.Store(true)
	f.helpers.Store(true)
	return f
}

func TestCloseSequence_SteamExitIsAskedFirstAndSignalsAreNotSent(t *testing.T) {
	f := running()
	f.exitsOnRequest = true
	outcome, err := f.seq().run()
	if err != nil || outcome != closeByRequest {
		t.Fatalf("outcome %q err %v, want %q", outcome, err, closeByRequest)
	}
	if f.termed.Load() != 0 || f.killed.Load() != 0 {
		t.Fatalf("signals sent to a Steam that quit by request: term %d kill %d", f.termed.Load(), f.killed.Load())
	}
}

// With Steam closed, opening steam://exit would launch Steam just to quit it.
func TestCloseSequence_NothingRunningAsksNothing(t *testing.T) {
	f := &fakeSteam{}
	outcome, err := f.seq().run()
	if err != nil || outcome != closeNothingRunning || f.asked.Load() != 0 {
		t.Fatalf("outcome %q err %v asked %d", outcome, err, f.asked.Load())
	}
}

func TestCloseSequence_LeftoverHelpersNeverOpenTheURL(t *testing.T) {
	f := &fakeSteam{exitsOnTerm: true}
	f.helpers.Store(true)
	outcome, err := f.seq().run()
	if err != nil || outcome != closeBySignal {
		t.Fatalf("outcome %q err %v", outcome, err)
	}
	if f.asked.Load() != 0 {
		t.Fatal("asked a client that is not running; that starts Steam")
	}
}

// A client at a login prompt, or a hung one: the request does nothing, SIGTERM works.
func TestCloseSequence_UnresponsiveToRequestFallsBackToSignal(t *testing.T) {
	f := running()
	f.exitsOnTerm = true
	outcome, err := f.seq().run()
	if err != nil || outcome != closeBySignal {
		t.Fatalf("outcome %q err %v", outcome, err)
	}
	if f.asked.Load() != 1 || f.killed.Load() != 0 {
		t.Fatalf("asked %d killed %d", f.asked.Load(), f.killed.Load())
	}
}

func TestCloseSequence_IgnoresEverythingThenForceKills(t *testing.T) {
	f := running()
	outcome, err := f.seq().run()
	if err != nil || outcome != closeByForce || f.killed.Load() != 1 {
		t.Fatalf("outcome %q err %v killed %d", outcome, err, f.killed.Load())
	}
}

func TestCloseSequence_SurvivingKillIsReported(t *testing.T) {
	f := running()
	f.survivesKill = true
	if _, err := f.seq().run(); !errors.Is(err, errSteamSurvivedKill) {
		t.Fatalf("err = %v", err)
	}
}

func TestCloseSequence_FailedRequestDoesNotWaitOutTheGrace(t *testing.T) {
	f := running()
	f.askErr = errors.New("no handler for steam://")
	f.exitsOnTerm = true
	seq := f.seq()
	seq.exitGrace = 5 * time.Second
	started := time.Now()
	outcome, err := seq.run()
	if err != nil || outcome != closeBySignal {
		t.Fatalf("outcome %q err %v", outcome, err)
	}
	if took := time.Since(started); took > 2*time.Second {
		t.Fatalf("took %v; a request that failed should move straight to signals", took)
	}
}

// One helper was seen parented to PID 1 while Steam ran normally. It must not hold up the switch
// for the whole request grace, and it must still be dealt with.
func TestCloseSequence_LingeringHelperAfterClientQuitGetsSignalled(t *testing.T) {
	f := running()
	f.exitsOnRequest = true
	f.helpersStay = true
	f.exitsOnTerm = true
	seq := f.seq()
	seq.exitGrace = 5 * time.Second
	started := time.Now()
	outcome, err := seq.run()
	if err != nil || outcome != closeBySignal {
		t.Fatalf("outcome %q err %v", outcome, err)
	}
	if took := time.Since(started); took > 2*time.Second {
		t.Fatalf("took %v waiting on a lingering helper", took)
	}
}

// Giving up on the request brings SIGTERM forward, never SIGKILL.
func TestCloseSequence_KillNeverComesBeforeTheFullGrace(t *testing.T) {
	for name, setup := range map[string]func(*fakeSteam){
		"request failed":  func(f *fakeSteam) { f.askErr = errors.New("no handler") },
		"leftover helper": func(f *fakeSteam) { f.client.Store(false) },
	} {
		t.Run(name, func(t *testing.T) {
			f := running()
			setup(f)
			seq := f.seq()
			started := time.Now()
			if outcome, _ := seq.run(); outcome != closeByForce {
				t.Fatalf("outcome %q", outcome)
			}
			if took, min := time.Since(started), seq.exitGrace+seq.termGrace; took < min {
				t.Fatalf("killed after %v, before the full %v", took, min)
			}
		})
	}
}

// Delivered after the client has gone, the URL would start Steam again.
func TestCloseSequence_WaitsForARequestStillInFlight(t *testing.T) {
	f := running()
	f.exitsOnRequest = true
	seq := f.seq()
	ask := seq.askToExit
	var finished atomic.Bool
	seq.askToExit = func() error {
		err := ask()
		time.Sleep(80 * time.Millisecond)
		finished.Store(true)
		return err
	}
	if outcome, err := seq.run(); err != nil || outcome != closeByRequest {
		t.Fatalf("outcome %q err %v", outcome, err)
	}
	if !finished.Load() {
		t.Fatal("reported closed while the request was still in flight")
	}
}
