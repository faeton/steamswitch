package steam

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"steamswitch/internal/paths"
)

const (
	signInID64 = "76561198000000002"
	signInID32 = "39734274"
)

// signInBackend answers the questions the watch asks; the rest fail closed.
type signInBackend struct {
	unsupportedBackend
	root       string
	running    bool
	activeUser string
}

func (b *signInBackend) ProcessInspectionSupported() bool { return true }
func (b *signInBackend) ProcessNames() []string           { return []string{"steam"} }
func (b *signInBackend) IsRunning(string) bool            { return b.running }
func (b *signInBackend) ActiveUserID32(string) string     { return b.activeUser }
func (b *signInBackend) LocalVDFPath(root string) string  { return filepath.Join(root, "local.vdf") }

func signInFixture(t *testing.T) (*signInBackend, string) {
	t.Helper()
	base := t.TempDir()
	paths.ResetForTest(filepath.Join(base, "data"))
	root := filepath.Join(base, "steam")
	writeFile(t, LoginUsersPath(root), fmt.Sprintf(`"users"
{
	"%s"
	{
		"AccountName"		"target"
		"MostRecent"		"1"
		"Timestamp"		"1000"
	}
	"76561198000000009"
	{
		"AccountName"		"other"
	}
}
`, signInID64))
	b := &signInBackend{root: root, running: true}
	prev := backend
	backend = b
	t.Cleanup(func() { backend = prev })
	return b, root
}

func writeLocalVDF(t *testing.T, root string, accounts ...string) {
	t.Helper()
	var sb strings.Builder
	sb.WriteString("\"MachineUserConfigStore\"\n{\n\t\"Software\"\n\t{\n\t\t\"Valve\"\n\t\t{\n\t\t\t\"Steam\"\n\t\t\t{\n\t\t\t\t\"ConnectCache\"\n\t\t\t\t{\n")
	for _, a := range accounts {
		fmt.Fprintf(&sb, "\t\t\t\t\t\"%s\"\t\t\"SECRETTOKENVALUE\"\n", connectCacheKeyCandidates(a)[0])
	}
	sb.WriteString("\t\t\t\t}\n\t\t\t}\n\t\t}\n\t}\n}\n")
	writeFile(t, filepath.Join(root, "local.vdf"), sb.String())
}

func writeConnLog(t *testing.T, root string, at time.Time, lines ...string) {
	t.Helper()
	var sb strings.Builder
	for _, l := range lines {
		fmt.Fprintf(&sb, "[%s] %s\n", at.Format("2006-01-02 15:04:05"), l)
	}
	writeFile(t, filepath.Join(root, "logs", "connection_log.txt"), sb.String())
}

func newTestWatch(t *testing.T, root string, at time.Time) *signInWatch {
	t.Helper()
	w := newSignInWatch(root, at)
	if w == nil {
		t.Fatal("no watch")
	}
	return w
}

// The reported bug, with the line Steam really wrote.
func TestSignIn_ExpiredTokenIsNotASuccess(t *testing.T) {
	_, root := signInFixture(t)
	writeLocalVDF(t, root, "target")
	start := time.Now()
	w := newTestWatch(t, root, start)
	writeConnLog(t, root, start.Add(2*time.Second),
		"Clearing in-memory token - 27 (License expired): LogonFailureReceived(2)",
		"not auto reconnecting due to License expired")

	st := w.check(start.Add(3 * time.Second))
	if st.State != SignInNeedsPassword || st.Reason != "License expired" {
		t.Fatalf("status = %+v", st)
	}
}

func TestSignIn_NoStoredLoginEndsAtAPasswordPromptThenConfirmsWhenTyped(t *testing.T) {
	b, root := signInFixture(t)
	writeLocalVDF(t, root, "other")
	start := time.Now()
	w := newTestWatch(t, root, start)
	if st := w.check(start.Add(time.Second)); st.State != SignInNeedsPassword || st.Reason != "noStoredLogin" {
		t.Fatalf("status = %+v", st)
	}
	b.activeUser = signInID32
	if st := w.check(start.Add(30 * time.Second)); st.State != SignInConfirmed {
		t.Fatalf("after signing in by hand: %+v", st)
	}
}

func TestSignIn_RejectionFollowedByPasswordIsConfirmed(t *testing.T) {
	_, root := signInFixture(t)
	writeLocalVDF(t, root, "target")
	start := time.Now()
	w := newTestWatch(t, root, start)
	writeConnLog(t, root, start.Add(time.Second),
		"Clearing in-memory token - 27 (License expired): LogonFailureReceived(2)",
		"[1,5] RecvMsgClientLogOnResponse() : [U:1:"+signInID32+"] 'OK' (Cell 1)")
	if st := w.check(start.Add(2 * time.Second)); st.State != SignInConfirmed {
		t.Fatalf("status = %+v", st)
	}
}

func TestSignIn_ActiveUserConfirmsAndAnotherAccountDoesNot(t *testing.T) {
	b, root := signInFixture(t)
	writeLocalVDF(t, root, "target")
	start := time.Now()
	w := newTestWatch(t, root, start)
	b.activeUser = "111"
	if st := w.check(start.Add(time.Second)); st.State != SignInWaiting {
		t.Fatalf("another account's ActiveUser: %+v", st)
	}
	b.activeUser = signInID32
	if st := w.check(start.Add(2 * time.Second)); st.State != SignInConfirmed {
		t.Fatalf("status = %+v", st)
	}
}

// The previous session's lines are in the same file. A success there is not this launch's.
func TestSignIn_LinesFromBeforeTheLaunchDoNotCount(t *testing.T) {
	_, root := signInFixture(t)
	writeLocalVDF(t, root, "target")
	start := time.Now()
	writeConnLog(t, root, start.Add(-time.Minute),
		"RecvMsgClientLogOnResponse() : [U:1:"+signInID32+"] 'OK' (Cell 1)")
	w := newTestWatch(t, root, start)
	if st := w.check(start.Add(time.Second)); st.State != SignInWaiting {
		t.Fatalf("status = %+v", st)
	}
}

func TestSignIn_TransientNetworkFailureKeepsWaiting(t *testing.T) {
	_, root := signInFixture(t)
	writeLocalVDF(t, root, "target")
	start := time.Now()
	w := newTestWatch(t, root, start)
	writeConnLog(t, root, start.Add(time.Second),
		"Clearing in-memory token - 3 (No Connection): LogonFailureReceived(2)")
	if st := w.check(start.Add(2 * time.Second)); st.State != SignInWaiting {
		t.Fatalf("status = %+v", st)
	}
}

func TestSignIn_NoAnswerBecomesUnconfirmedNeverSignedIn(t *testing.T) {
	_, root := signInFixture(t)
	writeLocalVDF(t, root, "target")
	start := time.Now()
	w := newTestWatch(t, root, start)
	if st := w.check(start.Add(signInAnswerTimeout + time.Second)); st.State != SignInUnconfirmed {
		t.Fatalf("status = %+v", st)
	}
}

func TestSignIn_SteamClosingIsReported(t *testing.T) {
	b, root := signInFixture(t)
	writeLocalVDF(t, root, "target")
	start := time.Now()
	w := newTestWatch(t, root, start)
	w.check(start.Add(time.Second))
	b.running = false
	w.check(start.Add(2 * time.Second))
	if st := w.check(start.Add(2*time.Second + signInExitSettle)); st.State != SignInSteamExited {
		t.Fatalf("status = %+v", st)
	}
}

// Steam can take a while to appear after `open` returns.
func TestSignIn_NotStartedYetIsStillWaiting(t *testing.T) {
	b, root := signInFixture(t)
	writeLocalVDF(t, root, "target")
	b.running = false
	start := time.Now()
	w := newTestWatch(t, root, start)
	if st := w.check(start.Add(10 * time.Second)); st.State != SignInWaiting {
		t.Fatalf("status = %+v", st)
	}
}

func TestSignIn_TokenVanishingCountsAsRejected(t *testing.T) {
	_, root := signInFixture(t)
	writeLocalVDF(t, root, "target")
	start := time.Now()
	w := newTestWatch(t, root, start)
	writeLocalVDF(t, root, "other")
	if st := w.check(start.Add(2 * time.Second)); st.State != SignInNeedsPassword {
		t.Fatalf("status = %+v", st)
	}
}

// Nobody knows whether Steam rewrites Timestamp on a failed attempt, so it proves nothing.
func TestSignIn_TimestampMovingNeverConfirms(t *testing.T) {
	_, root := signInFixture(t)
	writeLocalVDF(t, root, "other")
	start := time.Now()
	w := newTestWatch(t, root, start)
	writeFile(t, LoginUsersPath(root), fmt.Sprintf(`"users"
{
	"%s"
	{
		"AccountName"		"target"
		"MostRecent"		"1"
		"Timestamp"		"%d"
	}
}
`, signInID64, start.Unix()+1))
	for _, after := range []time.Duration{2 * time.Second, 20 * time.Second, signInAnswerTimeout} {
		if st := w.check(start.Add(after)); st.State == SignInConfirmed {
			t.Fatalf("after %v: %+v", after, st)
		}
	}
}

// A killed client leaves ActiveUser behind. Relaunching that account must not read as signed in
// before Steam has answered, or a rejection right after it would never be seen.
func TestSignIn_ActiveUserLeftBehindDoesNotConfirm(t *testing.T) {
	b, root := signInFixture(t)
	writeLocalVDF(t, root, "target")
	b.activeUser = signInID32
	start := time.Now()
	w := newTestWatch(t, root, start)
	if st := w.check(start.Add(time.Second)); st.State != SignInWaiting {
		t.Fatalf("stale value: %+v", st)
	}
	b.activeUser = "0"
	if st := w.check(start.Add(2 * time.Second)); st.State != SignInWaiting {
		t.Fatalf("cleared: %+v", st)
	}
	b.activeUser = signInID32
	if st := w.check(start.Add(3 * time.Second)); st.State != SignInConfirmed {
		t.Fatalf("really signed in: %+v", st)
	}
}

func TestSignIn_RejectionThenActiveUserComingBackConfirms(t *testing.T) {
	b, root := signInFixture(t)
	writeLocalVDF(t, root, "target")
	start := time.Now()
	w := newTestWatch(t, root, start)
	writeConnLog(t, root, start.Add(time.Second), "Clearing in-memory token - 27 (License expired): LogonFailureReceived(2)")
	// Still naming this account during the failed attempt says nothing.
	b.activeUser = signInID32
	if st := w.check(start.Add(2 * time.Second)); st.State != SignInNeedsPassword {
		t.Fatalf("during the rejection: %+v", st)
	}
	b.activeUser = "0"
	if st := w.check(start.Add(3 * time.Second)); st.State != SignInNeedsPassword {
		t.Fatalf("at the prompt: %+v", st)
	}
	// The success line is not one this code knows, but the client says so.
	b.activeUser = signInID32
	if st := w.check(start.Add(4 * time.Second)); st.State != SignInConfirmed {
		t.Fatalf("after the password: %+v", st)
	}
}

func TestSignIn_RestartIsNotAnExit(t *testing.T) {
	b, root := signInFixture(t)
	writeLocalVDF(t, root, "target")
	start := time.Now()
	w := newTestWatch(t, root, start)
	w.check(start.Add(time.Second))
	b.running = false
	if st := w.check(start.Add(2 * time.Second)); st.State == SignInSteamExited {
		t.Fatalf("one look at an empty process table: %+v", st)
	}
	b.running = true
	w.check(start.Add(5 * time.Second))
	b.running = false
	if st := w.check(start.Add(6 * time.Second)); st.State == SignInSteamExited {
		t.Fatalf("settling restarted after the client came back: %+v", st)
	}
}

func TestSignIn_ClosingAtThePasswordPromptIsAnExit(t *testing.T) {
	b, root := signInFixture(t)
	writeLocalVDF(t, root, "target")
	start := time.Now()
	w := newTestWatch(t, root, start)
	writeConnLog(t, root, start.Add(time.Second), "Clearing in-memory token - 27 (License expired): LogonFailureReceived(2)")
	w.check(start.Add(2 * time.Second))
	b.running = false
	w.check(start.Add(3 * time.Second))
	if st := w.check(start.Add(3*time.Second + signInExitSettle)); st.State != SignInSteamExited {
		t.Fatalf("status = %+v", st)
	}
}

// The log has one-second resolution, so the launch's own second can only be the previous session.
func TestSignIn_LineFromTheLaunchSecondDoesNotCount(t *testing.T) {
	_, root := signInFixture(t)
	writeLocalVDF(t, root, "target")
	start := time.Now()
	writeConnLog(t, root, start, "RecvMsgClientLogOnResponse() : [U:1:"+signInID32+"] 'OK' (Cell 1)")
	w := newTestWatch(t, root, start)
	if st := w.check(start.Add(time.Second)); st.State != SignInWaiting {
		t.Fatalf("status = %+v", st)
	}
}

// If no account matches, the key scheme changed. That must not read as "needs a password".
func TestSignIn_UnrecognisedKeysDoNotClaimAPasswordIsNeeded(t *testing.T) {
	_, root := signInFixture(t)
	writeLocalVDF(t, root, "stranger")
	start := time.Now()
	w := newTestWatch(t, root, start)
	if st := w.check(start.Add(time.Second)); st.State != SignInWaiting {
		t.Fatalf("status = %+v", st)
	}
}

func TestSignInStatus_DefaultIsNone(t *testing.T) {
	clearSignInWatch()
	var s SteamService
	if got := s.GetSignInStatus(); got.State != SignInNone {
		t.Fatalf("status = %+v", got)
	}
}

func TestStoredLogin_FlagsOnlyAccountsWithNoEntry(t *testing.T) {
	root := t.TempDir()
	writeLocalVDF(t, root, "alice", "bob")
	got := accountsWithoutStoredLogin(filepath.Join(root, "local.vdf"), []string{"alice", "Bob", "carol"})
	if len(got) != 1 || !got["carol"] {
		t.Fatalf("got %v, want only carol", got)
	}
}

func TestStoredLogin_UnreadableMeansUnknownNotNeedsPassword(t *testing.T) {
	if got := accountsWithoutStoredLogin(filepath.Join(t.TempDir(), "absent.vdf"), []string{"alice"}); len(got) != 0 {
		t.Fatalf("missing file flagged %v", got)
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "local.vdf"), "\"MachineUserConfigStore\"\n{\n}\n")
	if got := accountsWithoutStoredLogin(filepath.Join(root, "local.vdf"), []string{"alice"}); len(got) != 0 {
		t.Fatalf("no ConnectCache block flagged %v", got)
	}
}

// If nothing matches at all, the key scheme has changed and flagging everybody would be wrong.
func TestStoredLogin_NoMatchesAtAllMeansUnknown(t *testing.T) {
	root := t.TempDir()
	writeLocalVDF(t, root, "stranger")
	if got := accountsWithoutStoredLogin(filepath.Join(root, "local.vdf"), []string{"alice", "bob"}); len(got) != 0 {
		t.Fatalf("flagged %v", got)
	}
}

func TestStoredLogin_TokenValuesAreNeverReturned(t *testing.T) {
	root := t.TempDir()
	writeLocalVDF(t, root, "alice")
	keys, ok := connectCacheKeys(filepath.Join(root, "local.vdf"))
	if !ok {
		t.Fatal("not ok")
	}
	for k := range keys {
		if strings.Contains(strings.ToLower(k), "secret") {
			t.Fatalf("token value in keys: %q", k)
		}
	}
}

func TestConnectionLogTail_ReadsOnlyTheEnd(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "logs", "connection_log.txt")
	writeFile(t, path, strings.Repeat("x", connectionLogTail+100)+"END")
	got := readConnectionLogTail(root)
	if len(got) != connectionLogTail || !strings.HasSuffix(got, "END") {
		t.Fatalf("len %d", len(got))
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got := readConnectionLogTail(root); got != "" {
		t.Fatalf("missing log read as %q", got)
	}
}
