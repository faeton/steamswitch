package vault

// Manual Guard-code entry, for an account whose inbox cannot be IMAP-checked.
//
// The login flow (guardCodeForLogin, inside a DeepCheck) reaches a Guard challenge it cannot
// answer automatically, asks the UI for a code, and blocks until the user submits one or the check
// times out. The UI is told through an injected hook (a Wails event, wired in main so this package
// keeps no dependency on the app layer) and answers with SubmitManualGuardCode.
//
// Each request carries a one-shot id. A submit must quote it, so a prompt left open by an earlier
// (timed-out) request can never deliver its now-stale code into a later request for the same
// account. Deep checks are serialised process-wide (deepMu), so at most one request is live at a
// time, but the id makes stale answers safe regardless.

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
)

// GuardCodeNeededEvent is the Wails event asking the frontend to prompt for a manual Guard code.
const GuardCodeNeededEvent = "vault-guard-code-needed"

// GuardCodeNeededPayload carries which account needs a code, the request id the submit must quote,
// and Steam's own masked-address hint.
type GuardCodeNeededPayload struct {
	SteamID64 string `json:"steamId64"`
	RequestID string `json:"requestId"`
	Hint      string `json:"hint"`
}

// ErrNoManualRequest is returned when a code is submitted for a request that is not waiting — no
// login in flight, a stale prompt from an earlier request, an empty code, or one already answered.
var ErrNoManualRequest = errors.New("Toast_Vault_NoManualCodeRequest")

type pendingReq struct {
	id string
	ch chan string
}

var (
	manualMu      sync.Mutex
	manualPending = map[string]*pendingReq{}
	manualSeq     uint64
	guardCodeHook func(steamID64, requestID, hint string)
)

// SetGuardCodeNeededHook injects the notifier the login flow uses to ask the UI for a code. main
// wires it to a Wails event; with no hook (headless, tests) a request simply waits out its
// context, which is the correct "no one can answer" behaviour.
func SetGuardCodeNeededHook(fn func(steamID64, requestID, hint string)) {
	manualMu.Lock()
	guardCodeHook = fn
	manualMu.Unlock()
}

// requestManualCode asks the user for a Guard code and blocks until one is submitted for this
// request or ctx ends. hint is Steam's masked-address hint, shown to orient the user.
func requestManualCode(ctx context.Context, steamID64, hint string) (string, error) {
	id := normID(steamID64)
	if id == "" {
		return "", ErrNoSteamID
	}
	req := &pendingReq{ch: make(chan string, 1)}

	manualMu.Lock()
	manualSeq++
	req.id = strconv.FormatUint(manualSeq, 10)
	// One outstanding request per account. Any previous one (there should be none under deepMu) is
	// replaced; its own waiter falls through to its context, and its stale id no longer matches.
	manualPending[id] = req
	hook := guardCodeHook
	manualMu.Unlock()

	defer func() {
		manualMu.Lock()
		if manualPending[id] == req {
			delete(manualPending, id)
		}
		manualMu.Unlock()
	}()

	if hook == nil {
		// No UI attached — no one can answer. Wait out the context so the caller times out cleanly
		// rather than blocking forever.
		<-ctx.Done()
		return "", ctx.Err()
	}
	hook(id, req.id, hint)

	select {
	case code := <-req.ch:
		return strings.TrimSpace(code), nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// SubmitManualGuardCode delivers a user-entered Guard code to the request it was raised for. It
// never blocks: the send is non-blocking, and a missing/mismatched request (nothing waiting, or a
// stale prompt), an empty code, or an already-answered request are reported rather than waited on.
func SubmitManualGuardCode(steamID64, requestID, code string) error {
	if strings.TrimSpace(code) == "" {
		// An empty submission is not an answer; do not consume the pending request over it.
		return ErrNoManualRequest
	}
	id := normID(steamID64)
	manualMu.Lock()
	defer manualMu.Unlock()
	req := manualPending[id]
	if req == nil || req.id != requestID {
		return ErrNoManualRequest
	}
	select {
	case req.ch <- code:
		// Consume the request so a duplicate submit finds nothing waiting rather than filling the
		// just-emptied buffer and falsely reporting success.
		delete(manualPending, id)
		return nil
	default:
		return ErrNoManualRequest
	}
}
