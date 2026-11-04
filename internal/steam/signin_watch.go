package steam

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"steamswitch/internal/crashlog"
	"steamswitch/internal/security"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Did Steam actually sign in?
//
// A switch writes the login files and starts Steam. Whether the account ends up signed in is
// Steam's decision and comes later: a rejected token (measured: "27 (License expired)") or an
// account with no stored login both end at a password prompt, while everything this app wrote
// still reads as a success. So the claim "signed in" is held back until Steam itself says so, and
// the watch here is what collects Steam's answer.
//
// The evidence, strongest first:
//
//   - connection_log.txt: logon results, with the account. The last result since launch decides,
//     so a rejection followed by the user typing their password ends up confirmed.
//   - ActiveUser: the client's own record of who is signed in.
//   - the loginusers.vdf Timestamp moving forward. Weakest, because it was not measured whether
//     Steam also rewrites it on a failed attempt, so it counts only after a settling delay.
//   - the account's ConnectCache entry disappearing, which Steam does on rejection (measured).
//
// None of the positive signals could be measured on real Steam from where this was written. The
// failure mode of a log format or registry key that differs is deliberate: the result is
// "unconfirmed", never a false "signed in".

// SignInState is what the switcher may claim. The strings cross the bindings.
type SignInState string

const (
	// SignInNone: no launch is being watched.
	SignInNone SignInState = "none"
	// SignInWaiting: Steam was started, no answer yet.
	SignInWaiting SignInState = "waiting"
	// SignInConfirmed: Steam reports this account as signed in.
	SignInConfirmed SignInState = "confirmed"
	// SignInNeedsPassword: Steam rejected the login, or there was no stored login to use.
	SignInNeedsPassword SignInState = "needsSignIn"
	// SignInUnconfirmed: Steam is up but gave no answer in time. Says nothing either way.
	SignInUnconfirmed SignInState = "unconfirmed"
	// SignInSteamExited: Steam closed before signing in.
	SignInSteamExited SignInState = "steamExited"
)

// SignInStatus is the current verdict. Reason is Steam's own short text ("License expired") or
// "noStoredLogin". It carries no token or log content beyond that.
type SignInStatus struct {
	SteamID64 string      `json:"steamId64"`
	State     SignInState `json:"state"`
	Reason    string      `json:"reason,omitempty"`
}

// SignInStatusEvent carries a SignInStatus to the frontend whenever it changes.
const SignInStatusEvent = "steam-signin-status"

const (
	signInAnswerTimeout   = 90 * time.Second
	signInStartupGrace    = 45 * time.Second
	signInWeakSignalDelay = 5 * time.Second
	signInWatchLimit      = 10 * time.Minute
	signInFastPoll        = time.Second
	signInSlowPoll        = 3 * time.Second
	connectionLogTail     = 512 * 1024
)

var (
	connLogTimeRe = regexp.MustCompile(`^\[(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})\]`)
	// "... 27 (License expired): LogonFailureReceived(2)" and "not auto reconnecting due to X".
	connLogCodeRe   = regexp.MustCompile(`\d+ \(([A-Za-z][A-Za-z ]{1,40})\)`)
	connLogDueToRe  = regexp.MustCompile(`not auto reconnecting due to ([A-Za-z][A-Za-z ]{1,40})`)
	connLogAnswerRe = regexp.MustCompile(`LogOnResponse[^']*'([^']+)'`)
)

// transientLogonFailures are network conditions Steam retries by itself. A launch often starts
// before the connection is up, and none of these says anything about the account.
var transientLogonFailures = []string{
	"no connection", "timeout", "service unavailable", "try another cm", "rate limit",
	"busy", "connect failed", "encryption", "pending",
}

func isTransientLogonFailure(reason string) bool {
	r := strings.ToLower(reason)
	for _, t := range transientLogonFailures {
		if strings.Contains(r, t) {
			return true
		}
	}
	return false
}

// logonEvent is one logon result read from connection_log.txt.
type logonEvent struct {
	success bool
	reason  string
}

// lastLogonEvent returns the latest logon result for this account at or after since. Lines that
// carry no timestamp are skipped, because they cannot be told apart from the previous session.
func lastLogonEvent(logText, id32 string, since time.Time) (logonEvent, bool) {
	var last logonEvent
	found := false
	tag := "[U:1:" + id32 + "]"
	for _, line := range strings.Split(logText, "\n") {
		m := connLogTimeRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		at, err := time.ParseInLocation("2006-01-02 15:04:05", m[1], time.Local)
		if err != nil || at.Before(since.Truncate(time.Second)) {
			continue
		}
		if ev, ok := parseLogonLine(line, tag); ok {
			last, found = ev, true
		}
	}
	return last, found
}

func parseLogonLine(line, accountTag string) (logonEvent, bool) {
	if a := connLogAnswerRe.FindStringSubmatch(line); a != nil {
		// A response about another account says nothing about this one.
		if strings.Contains(line, "[U:1:") && !strings.Contains(line, accountTag) {
			return logonEvent{}, false
		}
		if a[1] == "OK" {
			return logonEvent{success: true}, true
		}
		if isTransientLogonFailure(a[1]) {
			return logonEvent{}, false
		}
		return logonEvent{reason: a[1]}, true
	}
	if strings.Contains(line, "[Logged On") && strings.Contains(line, accountTag) {
		return logonEvent{success: true}, true
	}
	// These two lines carry no account, but only one account is being signed in at this point.
	var reason string
	if strings.Contains(line, "LogonFailureReceived") {
		if m := connLogCodeRe.FindStringSubmatch(line); m != nil {
			reason = m[1]
		}
	} else if m := connLogDueToRe.FindStringSubmatch(line); m != nil {
		reason = strings.TrimSpace(m[1])
	} else {
		return logonEvent{}, false
	}
	if isTransientLogonFailure(reason) {
		return logonEvent{}, false
	}
	return logonEvent{reason: reason}, true
}

// readConnectionLogTail reads the end of Steam's connection log. Missing reads as empty.
func readConnectionLogTail(steamRoot string) string {
	f, err := os.Open(filepath.Join(steamRoot, "logs", "connection_log.txt"))
	if err != nil {
		return ""
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > connectionLogTail {
		if _, err := f.Seek(-connectionLogTail, io.SeekEnd); err != nil {
			return ""
		}
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return ""
	}
	return string(data)
}

// signInWatch is one launch being watched. The fields set at the start are read-only afterwards.
type signInWatch struct {
	steamRoot     string
	steamID64     string
	id32          string
	accountName   string
	startedAt     time.Time
	noStoredLogin bool
	hadToken      bool

	sawClient bool
	weakSince time.Time
}

// newSignInWatch must run before Steam starts: it records the launch time, and whether a token
// existed so that it disappearing later can be noticed.
func newSignInWatch(steamRoot string, now time.Time) *signInWatch {
	users, err := ParseLoginUsers(LoginUsersPath(steamRoot))
	if err != nil {
		return nil
	}
	id := strings.TrimSpace(ActiveSessionSteamID64(users))
	if id == "" {
		return nil
	}
	f, err := FormatsFromID64(id)
	if err != nil {
		return nil
	}
	w := &signInWatch{steamRoot: steamRoot, steamID64: id, id32: f.ID32, startedAt: now}
	for _, u := range users {
		if u.SteamID64 == id {
			w.accountName = u.AccountName
		}
	}
	// Through the same check as the badge: if the key scheme is not what is expected, nobody gets
	// reported as having no login.
	names := make([]string, 0, len(users))
	for _, u := range users {
		names = append(names, u.AccountName)
	}
	localVDF := backend.LocalVDFPath(steamRoot)
	w.noStoredLogin = accountsWithoutStoredLogin(localVDF, names)[strings.ToLower(strings.TrimSpace(w.accountName))]
	if keys, ok := connectCacheKeys(localVDF); ok {
		w.hadToken = hasConnectCacheEntry(keys, w.accountName)
	}
	return w
}

// check turns the current evidence into a verdict. It is the only place that decides.
func (w *signInWatch) check(now time.Time) SignInStatus {
	status := SignInStatus{SteamID64: w.steamID64, State: SignInWaiting}
	running := steamClientRunning()
	if running {
		w.sawClient = true
	}

	if ev, ok := lastLogonEvent(readConnectionLogTail(w.steamRoot), w.id32, w.startedAt); ok {
		if ev.success {
			status.State = SignInConfirmed
		} else {
			status.State, status.Reason = SignInNeedsPassword, ev.reason
		}
		return status
	}
	if running && backend.ActiveUserID32(w.steamRoot) == w.id32 {
		status.State = SignInConfirmed
		return status
	}
	if !running && (w.sawClient || now.Sub(w.startedAt) >= signInStartupGrace) {
		status.State = SignInSteamExited
		return status
	}
	if running && w.loginTimestampMoved() {
		if w.weakSince.IsZero() {
			w.weakSince = now
		} else if now.Sub(w.weakSince) >= signInWeakSignalDelay {
			status.State = SignInConfirmed
			return status
		}
	}
	if w.noStoredLogin {
		status.State, status.Reason = SignInNeedsPassword, "noStoredLogin"
		return status
	}
	if w.hadToken {
		if keys, ok := connectCacheKeys(backend.LocalVDFPath(w.steamRoot)); ok && !hasConnectCacheEntry(keys, w.accountName) {
			status.State, status.Reason = SignInNeedsPassword, "tokenRejected"
			return status
		}
	}
	if now.Sub(w.startedAt) >= signInAnswerTimeout {
		status.State = SignInUnconfirmed
	}
	return status
}

func (w *signInWatch) loginTimestampMoved() bool {
	users, err := ParseLoginUsers(LoginUsersPath(w.steamRoot))
	if err != nil {
		return false
	}
	for _, u := range users {
		if u.SteamID64 == w.steamID64 {
			ts, err := strconv.ParseInt(strings.TrimSpace(u.Timestamp), 10, 64)
			return err == nil && ts >= w.startedAt.Unix()
		}
	}
	return false
}

// The single current watch. A new launch replaces it, and the generation stops the old goroutine.
var signInTracker struct {
	sync.Mutex
	generation int
	status     SignInStatus
}

func setSignInStatus(generation int, status SignInStatus) {
	signInTracker.Lock()
	if generation != signInTracker.generation || signInTracker.status == status {
		signInTracker.Unlock()
		return
	}
	signInTracker.status = status
	signInTracker.Unlock()
	if status.State != SignInWaiting && status.State != SignInNone {
		steamLog.Info("sign-in result", slog.String("steamId", tailSteamID(status.SteamID64)),
			slog.String("state", string(status.State)), slog.String("reason", status.Reason))
	}
	if app := application.Get(); app != nil {
		app.Event.Emit(SignInStatusEvent, status)
	}
}

func nextSignInGeneration() int {
	signInTracker.Lock()
	defer signInTracker.Unlock()
	signInTracker.generation++
	return signInTracker.generation
}

// clearSignInWatch ends any watch and reports that nothing is being watched.
func clearSignInWatch() {
	setSignInStatus(nextSignInGeneration(), SignInStatus{State: SignInNone})
}

// launchAndWatchSignIn is how Steam gets started anywhere in this package.
func launchAndWatchSignIn(steamRoot string, args []string, opts LaunchOpts) error {
	now := time.Now()
	w := newSignInWatch(steamRoot, now)
	generation := nextSignInGeneration()
	if w == nil {
		setSignInStatus(generation, SignInStatus{State: SignInNone})
	} else {
		setSignInStatus(generation, SignInStatus{SteamID64: w.steamID64, State: SignInWaiting})
	}
	if err := backend.Launch(steamRoot, args, opts); err != nil {
		setSignInStatus(generation, SignInStatus{State: SignInNone})
		return err
	}
	if w != nil {
		go w.run(generation)
	}
	return nil
}

// run polls until Steam gives a final answer. needsSignIn and unconfirmed are not final: the
// person may type their password, and then the account really is signed in.
func (w *signInWatch) run(generation int) {
	defer crashlog.Capture()
	for {
		now := time.Now()
		status := w.check(now)
		setSignInStatus(generation, status)
		if status.State == SignInConfirmed || status.State == SignInSteamExited {
			return
		}
		elapsed := now.Sub(w.startedAt)
		if elapsed >= signInWatchLimit {
			return
		}
		signInTracker.Lock()
		stale := generation != signInTracker.generation
		signInTracker.Unlock()
		if stale {
			return
		}
		if elapsed < signInAnswerTimeout {
			time.Sleep(signInFastPoll)
		} else {
			time.Sleep(signInSlowPoll)
		}
	}
}

// GetSignInStatus lets the UI read the verdict it may have missed an event for.
func (s *SteamService) GetSignInStatus() SignInStatus {
	if security.RequireUnlocked() != nil {
		return SignInStatus{State: SignInNone}
	}
	signInTracker.Lock()
	defer signInTracker.Unlock()
	if signInTracker.status.State == "" {
		return SignInStatus{State: SignInNone}
	}
	return signInTracker.status
}
