package sessionkit

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// Scratch-space placement and per-part journalling.
//
// Both exist for the same reason: the engine has to be able to find, on a later launch, the
// directory holding a displaced live tree. That means the location must be derivable rather
// than remembered, and every rename must be recorded before the next one starts.

func TestScratchDirs_UsesTheModuleAnchorSoRenamesStayOnOneVolume(t *testing.T) {
	// ReplacePart installs with os.Rename, which cannot cross filesystems. Steam is commonly
	// on a second drive while the data root follows %AppData% on the system drive, so
	// anchoring scratch space under the data root would break every switch on such a machine.
	h := newHarness(t)
	anchor := t.TempDir()
	h.mod.scratchAnchor = anchor

	stage, rollback, err := h.engine.scratchDirs("tx1", fakeModuleID, "", AccountRef{SteamID64: sharedID})
	if err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(anchor, sharedID, scratchDirName, "tx1")
	if stage != filepath.Join(want, "staging") {
		t.Fatalf("stage = %q, want it under the module anchor %q", stage, want)
	}
	if rollback != filepath.Join(want, "rollback") {
		t.Fatalf("rollback = %q, want it under the module anchor %q", rollback, want)
	}
}

func TestScratchDirs_IsDerivedNotStored(t *testing.T) {
	// Recovery recomputes these on the next launch instead of reading them out of the
	// journal, so a corrupted journal cannot redirect a rollback at some other tree. That
	// only holds if the same inputs always produce the same answer.
	h := newHarness(t)
	h.mod.scratchAnchor = t.TempDir()

	a1, b1, err := h.engine.scratchDirs("tx1", fakeModuleID, "", AccountRef{SteamID64: sharedID})
	if err != nil {
		t.Fatal(err)
	}
	a2, b2, err := h.engine.scratchDirs("tx1", fakeModuleID, "", AccountRef{SteamID64: sharedID})
	if err != nil {
		t.Fatal(err)
	}
	if a1 != a2 || b1 != b2 {
		t.Fatalf("not deterministic: (%q,%q) then (%q,%q)", a1, b1, a2, b2)
	}
}

// unanchoredModule embeds the fake but hides ScratchAnchor, standing in for a module that
// never claimed to know where its volume is.
type unanchoredModule struct{ inner *fakeModule }

func (u unanchoredModule) ID() string          { return u.inner.ID() }
func (u unanchoredModule) DisplayName() string { return u.inner.DisplayName() }
func (u unanchoredModule) Detect(ctx context.Context, r DetectRequest) (Detection, error) {
	return u.inner.Detect(ctx, r)
}
func (u unanchoredModule) Preflight(ctx context.Context, r PreflightRequest) (ModulePlan, error) {
	return u.inner.Preflight(ctx, r)
}
func (u unanchoredModule) Snapshot(ctx context.Context, r SnapshotRequest) (SnapshotResult, error) {
	return u.inner.Snapshot(ctx, r)
}
func (u unanchoredModule) Apply(ctx context.Context, r ApplyRequest) (ApplyResult, error) {
	return u.inner.Apply(ctx, r)
}
func (u unanchoredModule) Restore(ctx context.Context, r RestoreRequest) (RestoreResult, error) {
	return u.inner.Restore(ctx, r)
}
func (u unanchoredModule) Verify(ctx context.Context, r VerifyRequest) (VerifyResult, error) {
	return u.inner.Verify(ctx, r)
}

func TestScratchDirs_FallsBackToTheDataRootOnlyForModulesThatNeverClaimedAnAnchor(t *testing.T) {
	h := newHarness(t)
	h.engine.modules = []Module{unanchoredModule{inner: h.mod}}

	stage, rollback, err := h.engine.scratchDirs("tx1", fakeModuleID, "", AccountRef{SteamID64: sharedID})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stage, filepath.Join("transactions", "tx1", "staging")) {
		t.Fatalf("stage = %q, want the data-root fallback", stage)
	}
	if !strings.Contains(rollback, filepath.Join("transactions", "tx1", "rollback")) {
		t.Fatalf("rollback = %q, want the data-root fallback", rollback)
	}
}

func TestScratchDirs_RefusesWhenAnAnchoredModuleCannotResolveOne(t *testing.T) {
	// Silently falling back would either rename across volumes mid-transaction or strand the
	// rollback copies somewhere recovery does not look. Refusing up front is the safe answer.
	h := newHarness(t)
	h.mod.scratchAnchor = ""

	if _, _, err := h.engine.scratchDirs("tx1", fakeModuleID, "", AccountRef{SteamID64: sharedID}); !errors.Is(err, ErrNoScratchAnchor) {
		t.Fatalf("err = %v, want ErrNoScratchAnchor", err)
	}
}

func TestScratchDirs_RejectsARelativeAnchor(t *testing.T) {
	// A relative anchor would plant scratch space in the process working directory, which on
	// Windows is wherever the shortcut happened to point.
	h := newHarness(t)
	h.mod.scratchAnchor = "not/absolute"

	if _, _, err := h.engine.scratchDirs("tx1", fakeModuleID, "", AccountRef{SteamID64: sharedID}); !errors.Is(err, ErrNoScratchAnchor) {
		t.Fatalf("err = %v, want ErrNoScratchAnchor for a relative anchor", err)
	}
}

func TestEnter_RefusesBeforeWritingAnythingWhenNoAnchorIsAvailable(t *testing.T) {
	// The check has to run before the journal is activated, so a machine that cannot stage
	// safely never gets a half-applied kit.
	h := newHarness(t)
	h.mod.scratchAnchor = ""

	err := h.engine.Enter(context.Background(), AccountRef{SteamID64: sharedID}, -1)
	if !errors.Is(err, ErrNoScratchAnchor) {
		t.Fatalf("err = %v, want ErrNoScratchAnchor", err)
	}
	st, sErr := h.engine.Status()
	if sErr != nil {
		t.Fatal(sErr)
	}
	if st.Kind != RecoveryNone {
		t.Fatalf("status = %q, want nothing outstanding — the journal should never have opened", st.Kind)
	}
}

func TestRecoveryUsesTheRecordedSteamRootNotAFreshOne(t *testing.T) {
	// If Steam is moved between the crash and the next launch, re-resolving the root would
	// name a different tree and the rollback copies would silently look absent.
	h := newHarness(t)
	h.mod.failApplyOn = partRemote

	if err := h.engine.Enter(context.Background(), AccountRef{SteamID64: sharedID}, -1); err == nil {
		t.Fatal("expected the simulated mid-apply failure")
	}

	j, err := h.engine.activeJournal()
	if err != nil || j == nil {
		t.Fatalf("no active journal: %v", err)
	}
	if j.SteamRoot == "" {
		t.Fatal("the journal did not record the Steam root")
	}
	if j.ScratchAnchors[fakeModuleID] == "" {
		t.Fatal("the journal did not record the module's scratch anchor")
	}

	// Move Steam. Recovery must still resolve the original anchor.
	original := j.SteamRoot
	h.life.steamRoot = t.TempDir()

	root, err := h.engine.journalSteamRoot(j)
	if err != nil {
		t.Fatal(err)
	}
	if root != original {
		t.Fatalf("root = %q, want the recorded %q", root, original)
	}
}

func TestResolve_AbandonRefusesOnceAKitHasBeenWritten(t *testing.T) {
	// "Abandon" claims the transaction never happened. Once a part is verified, the kit is
	// sitting on someone else's account, and discarding the journal would lose track of it.
	h := newHarness(t)
	if err := h.engine.Enter(context.Background(), AccountRef{SteamID64: sharedID}, -1); err != nil {
		t.Fatalf("enter: %v", err)
	}
	if err := h.engine.Resolve(context.Background(), ActionAbandon); !errors.Is(err, ErrAbandonAfterWrite) {
		t.Fatalf("err = %v, want ErrAbandonAfterWrite", err)
	}
}

func TestEnter_JournalsEveryPartTransitionAsItHappens(t *testing.T) {
	// Without a per-part callback the engine could only record states after Apply returned,
	// so a crash halfway through a module's parts would leave every part reading `pending`
	// while some had already been moved aside.
	h := newHarness(t)
	if err := h.engine.Enter(context.Background(), AccountRef{SteamID64: sharedID}, -1); err != nil {
		t.Fatalf("enter: %v", err)
	}

	for _, want := range []string{
		partLocal + ":" + string(ReplaceStaged),
		partLocal + ":" + string(ReplaceOldMoved),
		partLocal + ":" + string(ReplaceInstalled),
		partRemote + ":" + string(ReplaceStaged),
		partRemote + ":" + string(ReplaceOldMoved),
		partRemote + ":" + string(ReplaceInstalled),
	} {
		if !containsString(h.mod.journalled, want) {
			t.Fatalf("transition %q was never journalled; got %v", want, h.mod.journalled)
		}
	}
}

func TestEnter_ScratchSpaceIsRemovedWhenTheTransactionFinishes(t *testing.T) {
	// Scratch lives inside the Steam tree when an anchor is available, so leaving it behind
	// litters the user's userdata folder.
	h := newHarness(t)
	anchor := t.TempDir()
	h.mod.scratchAnchor = anchor

	ctx := context.Background()
	if err := h.engine.Enter(ctx, AccountRef{SteamID64: sharedID}, -1); err != nil {
		t.Fatalf("enter: %v", err)
	}
	if err := h.engine.Leave(ctx, AccountRef{SteamID64: homeID}, LeaveRestoreTheirs, -1); err != nil {
		t.Fatalf("leave: %v", err)
	}

	leftover := filepath.Join(anchor, sharedID, scratchDirName)
	if exists, _ := pathExists(leftover); exists {
		entries, _ := filepath.Glob(filepath.Join(leftover, "*"))
		if len(entries) > 0 {
			t.Fatalf("scratch space survived the transaction: %v", entries)
		}
	}
}

func containsString(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}
