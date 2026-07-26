package steam

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"steamswitch/internal/paths"
	"steamswitch/internal/sessionkit"
)

func writeFileT(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// withNoEngine points the guard at a state where the engine was never built, and at a fresh
// data root so the on-disk fallback has something real to stat.
func withNoEngine(t *testing.T) {
	t.Helper()
	kitEngineMu.Lock()
	prev := kitEngine
	kitEngine = nil
	kitEngineMu.Unlock()
	paths.ResetForTest(t.TempDir())
	t.Cleanup(func() {
		kitEngineMu.Lock()
		kitEngine = prev
		kitEngineMu.Unlock()
	})
}

// A machine that has never used a shared account must still be able to switch when the engine
// failed to construct.
//
// The first fix for this made every switch fail, on the stated grounds that a construction
// failure meant the journal could not be read. That is not what happens: `sessionkit.New` only
// resolves the data root and makes a directory, and never touches the journal. Refusing on
// that basis would disable the entire product for the large majority of users, to protect a
// feature they have not turned on.
func TestGuardedSwap_AllowsWhenNoEngineAndNoTransactionOnDisk(t *testing.T) {
	withNoEngine(t)

	ran := false
	if err := guardedSwap("76561198000000002", func() error {
		ran = true
		return nil
	}); err != nil {
		t.Fatalf("guardedSwap = %v, want nil — nothing is outstanding", err)
	}
	if !ran {
		t.Fatal("the swap did not run; a user who never touched the Session Kit lost switching")
	}
}

// But a transaction pointer left on disk is exactly the case a bare swap could strand, and
// without an engine there is nothing that can say what it holds.
func TestGuardedSwap_RefusesWhenNoEngineButATransactionExists(t *testing.T) {
	withNoEngine(t)

	root, err := sessionkit.SessionKitRoot()
	if err != nil {
		t.Fatal(err)
	}
	mkdirAllT(t, root)
	writeFileT(t, filepath.Join(root, "active.json"), `{"schemaVersion":1,"transactionId":"abc"}`)

	ran := false
	err = guardedSwap("76561198000000002", func() error {
		ran = true
		return nil
	})
	if !errors.Is(err, ErrKitEngineUnavailable) {
		t.Fatalf("guardedSwap = %v, want ErrKitEngineUnavailable", err)
	}
	if ran {
		t.Fatal("the swap ran with an unreadable transaction outstanding")
	}
}

func TestRestorePermitted_RunsUnderTheSameFallback(t *testing.T) {
	withNoEngine(t)

	ran := false
	if err := RestorePermitted(func() error {
		ran = true
		return nil
	}); err != nil {
		t.Fatalf("RestorePermitted = %v, want nil", err)
	}
	if !ran {
		t.Fatal("restore was refused with nothing outstanding")
	}
}

// The outside-window errors must keep identifying as the engine condition they stand for,
// while carrying their own message. Losing either half breaks something: `errors.Is` is how
// callers classify, and the key is what the tray and CLI put in front of the user.
func TestOutsideWindowErrors_KeepBothIdentities(t *testing.T) {
	cases := []struct {
		err     error
		engine  error
		wantKey string
	}{
		{ErrKitLeaveRequired, sessionkit.ErrLeaveRequired, "Toast_Kit_LeaveRequiredOutsideWindow"},
		{ErrKitRecoveryRequired, sessionkit.ErrRecoveryRequired, "Toast_Kit_RecoveryRequiredOutsideWindow"},
		{ErrKitRestoreBlocked, sessionkit.ErrNotSettled, "Toast_Kit_RestoreBlockedByKit"},
	}
	for _, tc := range cases {
		t.Run(tc.wantKey, func(t *testing.T) {
			if !errors.Is(tc.err, tc.engine) {
				t.Fatalf("%v no longer matches its engine sentinel", tc.err)
			}
			if tc.err.Error() != tc.wantKey {
				t.Fatalf("Error() = %q, want the i18n key %q", tc.err.Error(), tc.wantKey)
			}
		})
	}
}

// asOutsideWindow must re-label the engine's refusals and leave everything else alone —
// including the wrapped operation's own error, which is not a kit condition at all.
func TestAsOutsideWindow(t *testing.T) {
	if got := asOutsideWindow(nil); got != nil {
		t.Fatalf("nil became %v", got)
	}
	if got := asOutsideWindow(sessionkit.ErrLeaveRequired); !errors.Is(got, ErrKitLeaveRequired) {
		t.Fatalf("got %v, want the outside-window leave error", got)
	}
	if got := asOutsideWindow(sessionkit.ErrExternalChange); !errors.Is(got, ErrKitRecoveryRequired) {
		t.Fatalf("external change = %v, want the recovery wording", got)
	}
	own := errors.New("steam is still running")
	if got := asOutsideWindow(own); !errors.Is(got, own) {
		t.Fatalf("the operation's own error was swallowed: %v", got)
	}
}
