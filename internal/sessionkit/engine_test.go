package sessionkit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"steamswitch/internal/paths"
)

// A fake game module backed by two directories per account, so the engine's ordering and
// recovery can be exercised without Steam or Dota being installed.

const (
	fakeModuleID = "fakegame"
	partLocal    = "local"
	partRemote   = "remote"
)

type fakeModule struct {
	// root/<steamId64>/<part>/ is the live tree.
	root string
	// failApplyOn aborts Apply once the named part is reached, simulating a mid-write crash.
	failApplyOn string
	applyCalls  int
	// scratchAnchor, when set, makes the fake report a same-volume scratch location the way
	// a real module does. Left empty by default so the data-root fallback stays covered.
	scratchAnchor string
	// journalled records every (part, state) the engine was asked to make durable.
	journalled []string
}

// ScratchAnchor is only honoured when the test sets one; `scratchDirs` treats an empty or
// relative answer as "no anchor" and falls back to the data root.
func (f *fakeModule) ScratchAnchor(_ string, account AccountRef) (string, bool) {
	if f.scratchAnchor == "" {
		return "", false
	}
	return filepath.Join(f.scratchAnchor, account.SteamID64), true
}

func (f *fakeModule) ID() string          { return fakeModuleID }
func (f *fakeModule) DisplayName() string { return "Fake Game" }

func (f *fakeModule) parts() []Part {
	return []Part{
		{ID: partLocal, Label: "Local", Risk: PartLocalSafe, KitEligible: true},
		{ID: partRemote, Label: "Remote", Risk: PartCloudRisk, KitEligible: true},
	}
}

func (f *fakeModule) LivePath(_ string, account AccountRef, partID string) (string, bool) {
	return filepath.Join(f.root, account.SteamID64, partID), true
}

func (f *fakeModule) Detect(context.Context, DetectRequest) (Detection, error) {
	return Detection{Installed: true, Ready: true, Parts: f.parts(), Fingerprint: "v1"}, nil
}

func (f *fakeModule) Preflight(_ context.Context, req PreflightRequest) (ModulePlan, error) {
	return ModulePlan{
		ModuleID:             fakeModuleID,
		Source:               req.Source,
		Target:               req.Target,
		Parts:                f.parts(),
		DetectionFingerprint: "v1",
	}, nil
}

func (f *fakeModule) Snapshot(_ context.Context, req SnapshotRequest) (SnapshotResult, error) {
	var captured []string
	for _, p := range req.Plan.Parts {
		src, _ := f.LivePath(req.SteamRoot, req.Account, p.ID)
		if exists, _ := pathExists(src); !exists {
			continue
		}
		if err := copyTree(src, filepath.Join(req.Destination, p.ID)); err != nil {
			return SnapshotResult{}, err
		}
		captured = append(captured, p.ID)
	}
	man, err := HashParts(fakeModuleID, req.Plan.Parts, func(id string) (string, bool) {
		p, _ := f.LivePath(req.SteamRoot, req.Account, id)
		return p, true
	})
	if err != nil {
		return SnapshotResult{}, err
	}
	return SnapshotResult{CapturedParts: captured, Manifest: man}, nil
}

func (f *fakeModule) write(req ApplyRequest, honourAbsence bool) ([]string, Manifest, error) {
	var done []string
	for _, p := range req.Plan.Parts {
		f.applyCalls++
		if f.failApplyOn == p.ID {
			return done, Manifest{}, errors.New("simulated crash mid-apply")
		}
		src := filepath.Join(req.PayloadPath, p.ID)
		live, _ := f.LivePath(req.SteamRoot, req.Plan.Target, p.ID)
		srcExists, _ := pathExists(src)
		if !srcExists && !honourAbsence {
			continue
		}
		// Use the roots the engine handed over, and journal every transition through its
		// callback. Deriving either locally would let the module and recovery disagree about
		// where the displaced tree went.
		err := ReplacePart(src, live, partPath(req.StageRoot, p.ID), partPath(req.RollbackRoot, p.ID),
			!srcExists, func(s ReplaceState) error {
				f.journalled = append(f.journalled, p.ID+":"+string(s))
				if req.Journal == nil {
					return nil
				}
				return req.Journal(p.ID, s)
			})
		if err != nil {
			return done, Manifest{}, err
		}
		done = append(done, p.ID)
	}
	man, err := HashParts(fakeModuleID, req.Plan.Parts, func(id string) (string, bool) {
		p, _ := f.LivePath(req.SteamRoot, req.Plan.Target, id)
		return p, true
	})
	return done, man, err
}

func (f *fakeModule) Apply(_ context.Context, req ApplyRequest) (ApplyResult, error) {
	done, man, err := f.write(req, false)
	if err != nil {
		return ApplyResult{AppliedParts: done}, err
	}
	return ApplyResult{AppliedParts: done, Manifest: man, CloudRisk: req.Plan.HasCloudRisk()}, nil
}

func (f *fakeModule) Restore(_ context.Context, req RestoreRequest) (RestoreResult, error) {
	done, man, err := f.write(ApplyRequest{
		TransactionID: req.TransactionID,
		Plan:          req.Plan,
		SteamRoot:     req.SteamRoot,
		PayloadPath:   req.PayloadPath,
		Expected:      req.Expected,
		StageRoot:     req.StageRoot,
		RollbackRoot:  req.RollbackRoot,
		Journal:       req.Journal,
	}, true)
	if err != nil {
		return RestoreResult{RestoredParts: done}, err
	}
	return RestoreResult{RestoredParts: done, Manifest: man}, nil
}

func (f *fakeModule) Verify(_ context.Context, req VerifyRequest) (VerifyResult, error) {
	cur, err := HashParts(fakeModuleID, req.Plan.Parts, func(id string) (string, bool) {
		p, _ := f.LivePath(req.SteamRoot, req.Account, id)
		return p, true
	})
	if err != nil {
		return VerifyResult{}, err
	}
	match := true
	for id, want := range req.Expected.Parts {
		if got, ok := cur.Parts[id]; !ok || got.Digest != want.Digest {
			match = false
			break
		}
	}
	return VerifyResult{Match: match, Current: cur}, nil
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// --- fake lifecycle --------------------------------------------------------

type fakeLifecycle struct {
	steamRoot string
	current   AccountRef
	calls     []string
	stillOpen []string
	succeeded []string
}

func (l *fakeLifecycle) SteamRoot() (string, error)          { return l.steamRoot, nil }
func (l *fakeLifecycle) CurrentAccount() (AccountRef, error) { return l.current, nil }
func (l *fakeLifecycle) RunningProcesses() []string          { return l.stillOpen }
func (l *fakeLifecycle) CloseSteam(context.Context) error {
	l.calls = append(l.calls, "close")
	return nil
}
func (l *fakeLifecycle) LaunchSteam(context.Context) error {
	l.calls = append(l.calls, "launch")
	return nil
}
func (l *fakeLifecycle) OnSwitchSucceeded(a AccountRef) {
	l.succeeded = append(l.succeeded, a.SteamID64)
}
func (l *fakeLifecycle) WriteLogin(_ context.Context, a AccountRef, _ int) error {
	l.calls = append(l.calls, "login:"+a.SteamID64)
	l.current = a
	return nil
}

// --- harness ---------------------------------------------------------------

const (
	homeID   = "76561190000000001"
	sharedID = "76561190000000002"
	otherID  = "76561190000000003"
)

type harness struct {
	engine *Engine
	mod    *fakeModule
	life   *fakeLifecycle
	store  store
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dataDir := t.TempDir()
	paths.ResetForTest(dataDir)
	t.Cleanup(func() { paths.ResetForTest(t.TempDir()) })

	gameRoot := t.TempDir()
	// Anchored by default, matching every real module: an unanchored writer is now refused
	// outright rather than quietly staged under the data root.
	mod := &fakeModule{root: gameRoot, scratchAnchor: filepath.Join(gameRoot, "_scratch")}
	life := &fakeLifecycle{steamRoot: t.TempDir(), current: AccountRef{SteamID64: homeID}}

	// Home has a kit; the shared account has its own setup.
	writeFile(t, filepath.Join(gameRoot, homeID, partLocal, "my.cfg"), "mine")
	writeFile(t, filepath.Join(gameRoot, homeID, partRemote, "grid.json"), "my-grid")
	writeFile(t, filepath.Join(gameRoot, sharedID, partLocal, "their.cfg"), "theirs")
	writeFile(t, filepath.Join(gameRoot, sharedID, partRemote, "grid.json"), "their-grid")

	eng, err := New(Options{
		Lifecycle: life,
		Modules:   []Module{mod},
		Home:      func() (AccountRef, error) { return AccountRef{SteamID64: homeID}, nil },
		IsShared:  func(a AccountRef) bool { return a.SteamID64 == sharedID },
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	st, err := newStore()
	if err != nil {
		t.Fatal(err)
	}
	return &harness{engine: eng, mod: mod, life: life, store: st}
}

func (h *harness) liveTree(t *testing.T, account, part string) map[string]string {
	t.Helper()
	return readTree(t, filepath.Join(h.mod.root, account, part))
}

// --- tests -----------------------------------------------------------------

func TestEnter_SharedAccountCarriesTheKit(t *testing.T) {
	h := newHarness(t)

	if err := h.engine.Enter(context.Background(), AccountRef{SteamID64: sharedID}, -1); err != nil {
		t.Fatalf("enter: %v", err)
	}

	// The shared account now holds the Home kit...
	local := h.liveTree(t, sharedID, partLocal)
	if local["my.cfg"] != "mine" {
		t.Fatalf("shared local = %v, want the Home kit", local)
	}
	if _, ok := local["their.cfg"]; ok {
		t.Fatalf("shared local merged with stale files: %v", local)
	}

	// ...and Steam was closed before the write and launched after it.
	want := []string{"close", "login:" + sharedID, "launch"}
	if len(h.life.calls) != len(want) {
		t.Fatalf("lifecycle calls = %v, want %v", h.life.calls, want)
	}
	for i := range want {
		if h.life.calls[i] != want[i] {
			t.Fatalf("lifecycle calls = %v, want %v", h.life.calls, want)
		}
	}

	st, err := h.engine.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.Kind != RecoveryKitActive {
		t.Fatalf("status = %+v, want kit-active", st)
	}
}

func TestEnter_PlainAccountSkipsTheJournalEntirely(t *testing.T) {
	h := newHarness(t)

	if err := h.engine.Enter(context.Background(), AccountRef{SteamID64: otherID}, -1); err != nil {
		t.Fatalf("enter: %v", err)
	}
	st, err := h.engine.Status()
	if err != nil {
		t.Fatal(err)
	}
	// Nothing was overlaid, so there is nothing to recover and nothing to block on.
	if st.Kind != RecoveryNone {
		t.Fatalf("status = %+v, want none", st)
	}
}

func TestLeave_RestoreTheirsPutsTheirSetupBack(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	if err := h.engine.Enter(ctx, AccountRef{SteamID64: sharedID}, -1); err != nil {
		t.Fatal(err)
	}
	if err := h.engine.Leave(ctx, AccountRef{SteamID64: homeID}, LeaveRestoreTheirs, -1); err != nil {
		t.Fatalf("leave: %v", err)
	}

	local := h.liveTree(t, sharedID, partLocal)
	if local["their.cfg"] != "theirs" {
		t.Fatalf("shared local = %v, want their setup restored", local)
	}
	if _, ok := local["my.cfg"]; ok {
		t.Fatalf("the kit was left behind: %v", local)
	}

	st, _ := h.engine.Status()
	if st.Kind != RecoveryNone {
		t.Fatalf("status = %+v, want none after a completed leave", st)
	}
}

func TestLeave_KeepMineLeavesTheOverlayTracked(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	if err := h.engine.Enter(ctx, AccountRef{SteamID64: sharedID}, -1); err != nil {
		t.Fatal(err)
	}
	if err := h.engine.Leave(ctx, AccountRef{SteamID64: homeID}, LeaveKeepMine, -1); err != nil {
		t.Fatalf("leave: %v", err)
	}

	if got := h.liveTree(t, sharedID, partLocal); got["my.cfg"] != "mine" {
		t.Fatalf("shared local = %v, want the kit kept", got)
	}
	st, _ := h.engine.Status()
	// Still tracked, so a later restore is still possible.
	if st.Kind != RecoveryKitActive || !st.CanRestore {
		t.Fatalf("status = %+v, want a restorable kit-active state", st)
	}
}

func TestLeave_BlocksWhenFilesChangedOutside(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	if err := h.engine.Enter(ctx, AccountRef{SteamID64: sharedID}, -1); err != nil {
		t.Fatal(err)
	}

	// The other person played and their game rewrote a config.
	writeFile(t, filepath.Join(h.mod.root, sharedID, partLocal, "my.cfg"), "edited by them")

	err := h.engine.Leave(ctx, AccountRef{SteamID64: homeID}, LeaveRestoreTheirs, -1)
	if !errors.Is(err, ErrExternalChange) {
		t.Fatalf("err = %v, want ErrExternalChange", err)
	}
	// Default is no write: their edit must still be there.
	if got := h.liveTree(t, sharedID, partLocal); got["my.cfg"] != "edited by them" {
		t.Fatalf("live = %v, want the outside edit preserved", got)
	}

	st, _ := h.engine.Status()
	if st.Kind != RecoveryExternalChange {
		t.Fatalf("status = %+v, want external-change", st)
	}
	// And the app stays blocked until the user decides.
	if err := h.engine.Enter(ctx, AccountRef{SteamID64: otherID}, -1); !errors.Is(err, ErrExternalChange) {
		t.Fatalf("switching while blocked returned %v", err)
	}
}

func TestResolve_RestoreTheirsAfterExternalChange(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	if err := h.engine.Enter(ctx, AccountRef{SteamID64: sharedID}, -1); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(h.mod.root, sharedID, partLocal, "my.cfg"), "edited by them")
	_ = h.engine.Leave(ctx, AccountRef{SteamID64: homeID}, LeaveRestoreTheirs, -1)

	// The user has seen the difference and chosen to discard it.
	if err := h.engine.Resolve(ctx, ActionRestoreTheirs); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got := h.liveTree(t, sharedID, partLocal); got["their.cfg"] != "theirs" {
		t.Fatalf("live = %v, want their setup", got)
	}
	st, _ := h.engine.Status()
	if st.Kind != RecoveryNone {
		t.Fatalf("status = %+v, want none", st)
	}
}

func TestResolve_KeepCurrentClosesTheExternalChangeBlock(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	if err := h.engine.Enter(ctx, AccountRef{SteamID64: sharedID}, -1); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(h.mod.root, sharedID, partLocal, "my.cfg"), "edited by them")
	_ = h.engine.Leave(ctx, AccountRef{SteamID64: homeID}, LeaveRestoreTheirs, -1)

	if err := h.engine.Resolve(ctx, ActionKeepCurrent); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got := h.liveTree(t, sharedID, partLocal); got["my.cfg"] != "edited by them" {
		t.Fatalf("live = %v, want the outside edit kept", got)
	}
	if err := h.engine.Enter(ctx, AccountRef{SteamID64: otherID}, -1); err != nil {
		t.Fatalf("switching after keep-current: %v", err)
	}
}

func TestEnter_MidApplyFailureBlocksAndIsRecoverable(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	// Die after `local` has landed but before `remote`.
	h.mod.failApplyOn = partRemote

	if err := h.engine.Enter(ctx, AccountRef{SteamID64: sharedID}, -1); err == nil {
		t.Fatal("expected the simulated crash to surface")
	}

	st, _ := h.engine.Status()
	if st.Kind != RecoveryInterrupted {
		t.Fatalf("status = %+v, want interrupted", st)
	}
	if !st.CanRestore {
		t.Fatal("their setup was snapshotted before the write, so restore must be offered")
	}
	// No further switching until it is resolved.
	if err := h.engine.Enter(ctx, AccountRef{SteamID64: otherID}, -1); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("err = %v, want ErrRecoveryRequired", err)
	}

	h.mod.failApplyOn = ""
	if err := h.engine.Resolve(ctx, ActionRestoreTheirs); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// The mixed state is gone: both parts are back to what the other person had.
	if got := h.liveTree(t, sharedID, partLocal); got["their.cfg"] != "theirs" {
		t.Fatalf("local = %v, want their setup", got)
	}
	if got := h.liveTree(t, sharedID, partRemote); got["grid.json"] != "their-grid" {
		t.Fatalf("remote = %v, want their grid", got)
	}
}

func TestEnter_LastKnownGoodSurvivesASecondSwitch(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	if err := h.engine.Enter(ctx, AccountRef{SteamID64: sharedID}, -1); err != nil {
		t.Fatal(err)
	}
	first, ok := h.store.lastKnownGood(fakeModuleID, AccountRef{SteamID64: sharedID})
	if !ok {
		t.Fatal("no last-known-good pointer after the first switch")
	}
	if err := h.engine.Leave(ctx, AccountRef{SteamID64: homeID}, LeaveRestoreTheirs, -1); err != nil {
		t.Fatal(err)
	}
	if err := h.engine.Enter(ctx, AccountRef{SteamID64: sharedID}, -1); err != nil {
		t.Fatal(err)
	}
	second, _ := h.store.lastKnownGood(fakeModuleID, AccountRef{SteamID64: sharedID})

	if first == second {
		t.Fatal("the pointer did not advance")
	}
	// The superseded snapshot must still exist: "never overwrite the last known-good".
	dir, err := h.store.snapshotDir(fakeModuleID, AccountRef{SteamID64: sharedID}, first)
	if err != nil {
		t.Fatal(err)
	}
	if exists, _ := pathExists(dir); !exists {
		t.Fatal("advancing the pointer deleted the previous snapshot")
	}
}

func TestEnsureClosed_RefusesWhileProcessesHoldFiles(t *testing.T) {
	h := newHarness(t)
	h.life.stillOpen = []string{"dota2.exe"}

	err := h.engine.Enter(context.Background(), AccountRef{SteamID64: sharedID}, -1)
	if !errors.Is(err, ErrProcessesRunning) {
		t.Fatalf("err = %v, want ErrProcessesRunning", err)
	}
	// Nothing was written, so the other person's setup is untouched.
	if got := h.liveTree(t, sharedID, partLocal); got["their.cfg"] != "theirs" {
		t.Fatalf("live = %v, want untouched", got)
	}
}

func TestEnter_NoHomeAccountIsRefused(t *testing.T) {
	h := newHarness(t)
	eng, err := New(Options{
		Lifecycle: h.life,
		Modules:   []Module{h.mod},
		Home:      func() (AccountRef, error) { return AccountRef{}, nil },
		IsShared:  func(a AccountRef) bool { return a.SteamID64 == sharedID },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Enter(context.Background(), AccountRef{SteamID64: sharedID}, -1); !errors.Is(err, ErrNoHomeAccount) {
		t.Fatalf("err = %v, want ErrNoHomeAccount", err)
	}
}
