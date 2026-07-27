// Package session probes whether a stored Steam refresh token is still live.
//
// It exists because Steam's HTTP token endpoints refuse a client-audience refresh token: a live
// account on 2026-07-27 got EResult AccessDenied from GenerateAccessTokenForApp for a token minted
// seconds earlier, and again after a cooldown, so there is no cheap HTTP "is this token alive"
// answer. The only mechanism that reliably validates a client token is what the real Steam client
// does — connect to a CM and log on with it. This package does exactly that, briefly.
package session

import (
	"context"
	"strconv"
	"sync"
	"time"

	steam "github.com/gg-cr/go-steam"
	"github.com/gg-cr/go-steam/protocol/steamlang"

	"steamswitch/internal/appclient"
	"steamswitch/internal/vault/steamauth"
)

// dialTimeout bounds the CM TCP dial. go-steam's dial is otherwise the OS default, which can hang
// a probe for over a minute on a black-holed CM; ConnectionTimeout makes it fail in seconds.
const dialTimeout = 15 * time.Second

// probeTimeout is a whole-probe backstop for a caller that passes a context with no deadline.
// Callers that set their own (CheckSession does) keep the tighter of the two.
const probeTimeout = 45 * time.Second

// Result is the outcome of a successful probe. A failed probe is an error, classified with the
// same steamauth sentinels a login uses so the vault maps it identically.
type Result struct {
	Live      bool
	SteamID64 string
}

var (
	dirMu   sync.Mutex
	dirDone bool
)

// ensureDirectory populates go-steam's CM list from Steam once. It retries on a later call if the
// fetch failed (so a transient network problem at first use is not permanent), and sticks once it
// has succeeded.
func ensureDirectory() {
	dirMu.Lock()
	defer dirMu.Unlock()
	if dirDone {
		return
	}
	if err := steam.InitializeSteamDirectory(); err == nil {
		dirDone = true
	}
}

// Check connects to a Steam CM, logs on with the refresh token, and reports whether Steam still
// honours it.
//
// This is a real, brief authenticated sign-in — not a side-effect-free query. It is the only
// mechanism that validates a client-audience refresh token, since Steam's HTTP token endpoints
// refuse it. The account is signed in for the ~second the probe runs; persona state is left
// Offline (go-steam does not set it Online), so it is not shown online to friends, but it does
// create a real Steam session that will appear in the account's sign-in history and can coincide
// with an already-open client — the caller decides whether that is acceptable.
//
// A token Steam accepts yields Live (with the account it authenticated, verified against
// steamID64); one it rejects (revoked, expired, owner password change / sign-out-everywhere)
// returns steamauth.ErrAccessDenied; anything that means "could not tell" (rate limit, CM
// unavailable, timeout) returns the matching transient error, so a network blip never reads as a
// dead account.
func Check(ctx context.Context, accountName, refreshToken, steamID64 string) (*Result, error) {
	if accountName == "" || refreshToken == "" {
		return nil, steamauth.ErrRequestFailed
	}
	// The CM connection and the directory fetch both bypass appclient's transport, so offline mode
	// — which the rest of the vault gets for free through appclient — has to be enforced here
	// explicitly, or the probe would reach the network with outbound HTTP nominally disabled.
	if appclient.IsOfflineMode() {
		return nil, steamauth.ErrServiceDown
	}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	// Fetch a live CM list once. go-steam's built-in fallback list is hardcoded and goes stale;
	// without this, Connect() regularly picks a dead server and the dial burns the whole
	// dialTimeout before failing. This costs one HTTPS call the first time it succeeds.
	ensureDirectory()

	client := steam.NewClient()
	client.ConnectionTimeout = dialTimeout

	var res *Result
	var rerr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Connect dials in this goroutine, so the caller's ctx is honoured while it dials (the
		// dial itself is bounded by ConnectionTimeout). The loop reads Events() continuously — the
		// channel is never closed by go-steam — and returns on the first terminal event, which
		// keeps go-steam's fixed event buffer drained so nothing it emits can block.
		client.Connect()
		// If the caller gave up while we were dialing, tear down and stop here. Disconnect() cannot
		// interrupt an in-flight dial (conn is not set yet), so the ctx.Done arm's Disconnect was a
		// no-op; now that Connect has returned, conn is set and this Disconnect actually tears down.
		// Without this, a dial that finished after cancellation would drop into the event loop with
		// nothing left to disconnect it, and the waiting `<-done` would block forever.
		if ctx.Err() != nil {
			client.Disconnect()
			return
		}
		for ev := range client.Events() {
			switch e := ev.(type) {
			case *steam.ConnectedEvent:
				client.Auth.LogOn(&steam.LogOnDetails{Username: accountName, RefreshToken: refreshToken})
			case *steam.LoggedOnEvent:
				// Verify against the authoritative header SteamID (client.SteamId()), not the
				// event's ClientSteamId — the latter is the client-supplied field and can be 0.
				res, rerr = verdictForLogon(e, uint64(client.SteamId()), steamID64)
				return
			case *steam.LogOnFailedEvent:
				rerr = mapEResult(e.Result)
				return
			case *steam.SteamFailureEvent:
				// Fail / ServiceUnavailable / TryAnotherCM arrive here, not as a LogOnFailedEvent.
				rerr = mapEResult(e.Result)
				return
			case *steam.DisconnectedEvent:
				rerr = steamauth.ErrServiceDown
				return
			case steam.FatalErrorEvent:
				// FatalErrorEvent is a named error type (a read/handshake failure). Plain errors
				// from Errorf are non-fatal and correctly fall through to keep waiting.
				rerr = steamauth.ErrServiceDown
				return
			}
		}
	}()

	select {
	case <-ctx.Done():
		client.Disconnect() // no-op if still dialing; the goroutine's post-dial guard tears down then
		<-done              // bounded by the dial timeout; then the goroutine has fully returned
		// The cancellation is the real reason we stopped, so report it over whatever transient the
		// teardown produced in the goroutine — unless a genuine verdict landed exactly as we cancelled.
		if res != nil {
			return res, nil
		}
		return nil, ctx.Err()
	case <-done:
		client.Disconnect()
	}

	switch {
	case res != nil:
		return res, nil
	case rerr != nil:
		return nil, rerr
	default:
		return nil, ctx.Err()
	}
}

// verdictForLogon turns a successful logon into a verdict, verifying that the account Steam
// authenticated (authenticatedID, the authoritative header SteamID) is the one this entry claims —
// a valid token under the wrong stored SteamID must not attach a live verdict to the wrong entry.
func verdictForLogon(e *steam.LoggedOnEvent, authenticatedID uint64, steamID64 string) (*Result, error) {
	if e.Result != steamlang.EResult_OK {
		return nil, mapEResult(e.Result)
	}
	if authenticatedID == 0 {
		// Steam accepted the logon but the client recorded no authoritative SteamID, so we cannot
		// confirm which account this is — better to report "could not tell" than a live verdict we
		// cannot attribute.
		return nil, steamauth.ErrRequestFailed
	}
	got := strconv.FormatUint(authenticatedID, 10)
	if steamID64 != "" && got != steamID64 {
		return nil, steamauth.ErrRequestFailed
	}
	return &Result{Live: true, SteamID64: got}, nil
}

// mapEResult turns Steam's logon EResult into the sentinel classifySessionError expects. Only the
// distinctions that change what the user should do are kept: a dead token (blocks) versus a
// transient (does not).
//
// AccountLogonDenied is deliberately absent from the "dead" set — it means "a Guard code is
// required", which a valid token can still hit, so it falls through to the transient default
// rather than falsely reporting the session revoked.
func mapEResult(r steamlang.EResult) error {
	switch r {
	case steamlang.EResult_InvalidPassword, steamlang.EResult_AccessDenied, steamlang.EResult_Expired:
		return steamauth.ErrAccessDenied
	case steamlang.EResult_RateLimitExceeded, steamlang.EResult_LimitExceeded,
		steamlang.EResult_AccountLoginDeniedThrottle:
		return steamauth.ErrRateLimited
	case steamlang.EResult_ServiceUnavailable, steamlang.EResult_TryAnotherCM, steamlang.EResult_Fail:
		return steamauth.ErrServiceDown
	default:
		return steamauth.ErrRequestFailed
	}
}
