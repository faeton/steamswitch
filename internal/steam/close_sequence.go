package steam

import (
	"errors"
	"time"
)

// steamExitURL asks a running client to quit by itself.
const steamExitURL = "steam://exit"

// closeOutcome names what actually closed Steam, for the log.
type closeOutcome string

const (
	closeNothingRunning closeOutcome = "nothing-running"
	closeByRequest      closeOutcome = "steam-exit"
	closeBySignal       closeOutcome = "sigterm"
	closeByForce        closeOutcome = "sigkill"
)

// closeSequence is the order of a polite close, kept free of OS calls so it can be tested
// anywhere. Only macOS uses it today; Windows has its own path through winutil.KillByName.
type closeSequence struct {
	clientRunning func() bool
	anyRunning    func() bool
	askToExit     func() error
	terminate     func()
	kill          func()

	// exitGrace is how long the client gets to act on askToExit.
	exitGrace time.Duration
	// helperGrace is how long helpers get once the client itself has gone. They normally
	// follow within a second, so one that lingers should not cost the rest of exitGrace.
	helperGrace time.Duration
	termGrace   time.Duration
	poll        time.Duration
}

func (c closeSequence) run() (closeOutcome, error) {
	if !c.anyRunning() {
		return closeNothingRunning, nil
	}
	// Only a running client may be asked: with Steam closed, opening the URL would start it.
	// Leftover helpers with no client go straight to the signals.
	if c.clientRunning() && c.waitForRequestedExit() {
		return closeByRequest, nil
	}
	c.terminate()
	if c.waitUntilClosed(c.termGrace) {
		return closeBySignal, nil
	}
	c.kill()
	// Give the kernel a moment to reap, then report honestly. The caller treats a non-nil error
	// as advisory and re-checks with IsRunning, so a stubborn process surfaces either way, but
	// saying so here makes the log useful.
	time.Sleep(c.poll)
	if c.anyRunning() {
		return closeByForce, errSteamSurvivedKill
	}
	return closeByForce, nil
}

// waitForRequestedExit returns true when everything closed. It gives up early when the request
// could not even be delivered, or when only helpers are left and they are overstaying.
//
// askToExit runs on its own goroutine: a stuck `open` must not use up the grace period or hang
// the switch, and the process table is the authority on whether it worked.
func (c closeSequence) waitForRequestedExit() bool {
	asked := make(chan error, 1)
	go func() { asked <- c.askToExit() }()

	deadline := time.Now().Add(c.exitGrace)
	var clientGoneAt time.Time
	for time.Now().Before(deadline) {
		select {
		case err := <-asked:
			if err != nil {
				steamLog.Warn("steam exit request failed, using signals", "err", err)
				return !c.anyRunning()
			}
		default:
		}
		if !c.anyRunning() {
			return true
		}
		if !c.clientRunning() {
			if clientGoneAt.IsZero() {
				clientGoneAt = time.Now()
			} else if time.Since(clientGoneAt) >= c.helperGrace {
				return false
			}
		}
		time.Sleep(c.poll)
	}
	return !c.anyRunning()
}

func (c closeSequence) waitUntilClosed(grace time.Duration) bool {
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if !c.anyRunning() {
			return true
		}
		time.Sleep(c.poll)
	}
	return !c.anyRunning()
}

var errSteamSurvivedKill = errors.New("steam did not exit after SIGKILL")
