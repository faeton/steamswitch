package sessionkit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"steamswitch/internal/actionlog"
)

// Engine orchestrates the switch (REDESIGN.md §2).
//
// Ordering is the whole point of this type. `steam.SwapToAccount` closes Steam, writes the
// login, counts a switch and launches in one call, and short-circuits when the target is
// already current — none of which leaves room to snapshot and apply a kit in between. The
// engine drives the Lifecycle steps individually and journals between each one.

var (
	// ErrBusy is returned when a transaction is already running.
	ErrBusy = errors.New("Toast_Kit_Busy")
	// ErrRecoveryRequired blocks every switch entry point until an interrupted
	// transaction has been resolved.
	ErrRecoveryRequired = errors.New("Toast_Kit_RecoveryRequired")
	// ErrExternalChange means live files no longer match what was applied, so restoring
	// would silently discard someone else's work.
	ErrExternalChange = errors.New("Toast_Kit_ExternalChange")
	// ErrProcessesRunning means Steam or a game still holds the files.
	ErrProcessesRunning = errors.New("Toast_Kit_ProcessesRunning")
	// ErrNoHomeAccount means no kit source has been nominated.
	ErrNoHomeAccount = errors.New("Toast_Kit_NoHomeAccount")
	// ErrNoActiveKit is returned by Leave when nothing is overlaid.
	ErrNoActiveKit = errors.New("Toast_Kit_NoActiveKit")
	// ErrLeaveRequired means a kit is live on a different account, so switching away is a
	// decision about somebody else's files that only Leave is allowed to make.
	ErrLeaveRequired = errors.New("Toast_Kit_LeaveRequiredOutsideWindow")
)

var engineLog = slog.Default().With("pkg", "sessionkit")

// LeaveChoice is the answer to "Restore X's setup?" (REDESIGN.md §2).
type LeaveChoice string

const (
	// LeaveRestoreTheirs puts the shared account's own configuration back. The default.
	LeaveRestoreTheirs LeaveChoice = "restore-theirs"
	// LeaveKeepMine leaves the overlay in place, still flagged as active.
	LeaveKeepMine LeaveChoice = "keep-mine"
)

// Engine is the single owner of session-kit transactions.
type Engine struct {
	// mu covers a whole transaction, not one call. Neither SteamService.mu nor
	// dota.go's dotaWriteMu is wide enough: the tray, the CLI and the manual snapshot
	// tools all reach the same trees, and any of them interleaving with a close →
	// snapshot → apply → login sequence would corrupt it.
	mu sync.Mutex

	store    store
	life     Lifecycle
	modules  []Module
	progress ProgressSink

	// kitSource is the Home account, refreshed per call by the resolver.
	homeResolver func() (AccountRef, error)
	// sharedResolver reports whether an account is marked shared.
	sharedResolver func(AccountRef) bool
}

// Options configures a new Engine.
type Options struct {
	Lifecycle Lifecycle
	Modules   []Module
	Progress  ProgressSink
	// Home returns the current Home account, or a zero ref when none is nominated.
	Home func() (AccountRef, error)
	// IsShared reports whether switching to this account should carry the kit.
	IsShared func(AccountRef) bool
}

// New constructs an Engine. It touches the data root, so it must run after
// platform.InitDataPaths.
func New(opts Options) (*Engine, error) {
	if opts.Lifecycle == nil {
		return nil, errors.New("sessionkit: Lifecycle is required")
	}
	st, err := newStore()
	if err != nil {
		return nil, err
	}
	progress := opts.Progress
	if progress == nil {
		progress = noopSink{}
	}
	home := opts.Home
	if home == nil {
		home = func() (AccountRef, error) { return AccountRef{}, nil }
	}
	shared := opts.IsShared
	if shared == nil {
		shared = func(AccountRef) bool { return false }
	}
	return &Engine{
		store:          st,
		life:           opts.Lifecycle,
		modules:        opts.Modules,
		progress:       progress,
		homeResolver:   home,
		sharedResolver: shared,
	}, nil
}

func (e *Engine) phase(key string, vars map[string]string) {
	e.progress.Phase(key, vars)
}

// activeJournal returns the in-flight journal, if any.
func (e *Engine) activeJournal() (*Journal, error) {
	return e.store.journal.LoadActive()
}

// guardReady refuses to start new work while an interrupted or blocked transaction stands.
func (e *Engine) guardReady() (*Journal, error) {
	j, err := e.activeJournal()
	if err != nil {
		return nil, err
	}
	if j == nil {
		return nil, nil
	}
	if j.Phase.NeedsRecovery() {
		return j, ErrRecoveryRequired
	}
	if j.Phase == PhaseExternalChangeBlocked {
		return j, ErrExternalChange
	}
	return j, nil
}

// RunUnjournaledSwap runs a bare swap that bypasses the engine, but only if the current state
// permits one, and holds the transaction lock for the whole thing.
//
// The tray, desktop shortcuts, `steamswitch://` URLs and `--run-appid=` all switch accounts
// without going through the engine. Checking the status and *then* letting them run is not
// enough: `Status()` releases the lock before returning, so a bare swap could read "nothing
// outstanding", and a tile switch could enter a full transaction, and the two would then be
// closing Steam and rewriting `loginusers.vdf` at the same time — an unjournaled login change
// interleaved with a journaled one.
//
// Holding `mu` across `swap` gives those callers the same mutual exclusion a transaction has.
// That is what the comment on the process-wide engine has always claimed and what, until now,
// nothing implemented.
//
// `swap` must not call back into the engine.
func (e *Engine) RunUnjournaledSwap(target AccountRef, swap func() error) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	st, err := e.statusLocked()
	if err != nil {
		// Fail closed. An active journal may well exist and simply be unreadable right now
		// (permissions, I/O), and proceeding is how a live transaction gets compounded. This
		// matches guardManualConfigWrite, which already treats an unreadable status as unsafe.
		return fmt.Errorf("%w: %v", ErrRecoveryRequired, err)
	}
	switch st.Kind {
	case RecoveryInterrupted, RecoveryExternalChange:
		return ErrRecoveryRequired
	case RecoveryKitActive:
		if !strings.EqualFold(strings.TrimSpace(st.TargetSteamID64), strings.TrimSpace(target.SteamID64)) {
			return ErrLeaveRequired
		}
	}
	return swap()
}

// kitBlocksEntry reports whether an applied kit forbids switching to `target`.
//
// Both resting "a kit is on someone" phases count. Re-entering the account the kit is already
// on is allowed: that is not leaving it, and shortcuts do it routinely.
func kitBlocksEntry(j *Journal, target AccountRef) error {
	if j == nil {
		return nil
	}
	if j.Phase != PhaseKitActive && j.Phase != PhaseKeptActive {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(j.To.SteamID64), strings.TrimSpace(target.SteamID64)) {
		return nil
	}
	return ErrLeaveRequired
}

// ensureClosed closes Steam and confirms nothing is left holding the files.
//
// The engine enforces this rather than leaving it to modules: Dota's inherited guard only
// checks `steam.exe`, and only when the cloud-synced part is selected, but the redesign
// forbids mutating *any* part while Steam or a game runs.
func (e *Engine) ensureClosed(ctx context.Context) error {
	e.phase("Status_ClosingPlatform", map[string]string{"platform": "Steam"})
	if err := e.life.CloseSteam(ctx); err != nil {
		return err
	}
	if running := e.life.RunningProcesses(); len(running) > 0 {
		return fmt.Errorf("%w: %v", ErrProcessesRunning, running)
	}
	return nil
}

// plan runs Detect + Preflight for every module without mutating anything.
func (e *Engine) plan(ctx context.Context, op Operation, source, target AccountRef, steamRoot string) ([]ModulePlan, error) {
	var plans []ModulePlan
	for _, m := range e.modules {
		det, err := m.Detect(ctx, DetectRequest{SteamRoot: steamRoot})
		if err != nil {
			return nil, err
		}
		if !det.Installed || !det.Ready || det.Paused {
			// A module that is not ready is skipped, not fatal: the user may have Dota
			// enabled on one machine and not another.
			engineLog.Info("module skipped", "module", m.ID(), "ready", det.Ready, "paused", det.Paused, "reason", det.Reason)
			continue
		}
		p, err := m.Preflight(ctx, PreflightRequest{
			Operation: op,
			Source:    source,
			Target:    target,
			SteamRoot: steamRoot,
		})
		if err != nil {
			return nil, err
		}
		if len(p.Parts) == 0 {
			continue
		}
		plans = append(plans, p)
	}
	return plans, nil
}

// snapshot captures one account's parts into a new immutable snapshot.
func (e *Engine) snapshot(ctx context.Context, m Module, txID string, account AccountRef, plan ModulePlan, purpose SnapshotPurpose, steamRoot string) (SnapshotResult, error) {
	tmpDir, payloadDir, id, err := e.store.beginSnapshot(m.ID(), account)
	if err != nil {
		return SnapshotResult{}, err
	}

	res, err := m.Snapshot(ctx, SnapshotRequest{
		TransactionID: txID,
		Account:       account,
		Plan:          plan,
		Purpose:       purpose,
		SteamRoot:     steamRoot,
		Destination:   payloadDir,
	})
	if err != nil {
		e.store.abortSnapshot(tmpDir)
		return SnapshotResult{}, err
	}

	meta := SnapshotMeta{
		ID:            id,
		ModuleID:      m.ID(),
		SteamID64:     account.SteamID64,
		Purpose:       string(purpose),
		TransactionID: txID,
		Parts:         res.CapturedParts,
		CreatedAt:     nowRFC3339(),
	}
	final, err := e.store.commitSnapshot(tmpDir, m.ID(), account, id, meta, res.Manifest)
	if err != nil {
		e.store.abortSnapshot(tmpDir)
		return SnapshotResult{}, err
	}

	res.SnapshotID = id
	res.PayloadPath = filepath.Join(final, "payload")
	return res, nil
}

// Enter switches to `target`, carrying the Home account's kit if the target is shared.
//
// Every mutation is preceded by a durable journal write, so any crash point is classifiable
// on the next launch.
func (e *Engine) Enter(ctx context.Context, target AccountRef, personaState int) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	active, err := e.guardReady()
	if err != nil {
		return err
	}
	// A live kit may only be left through Leave, which asks whose setup to keep. Enter used
	// to walk straight past a resting PhaseKitActive into plainSwitch — closing Steam,
	// rewriting the login and launching — with no restore and no journal update, stranding
	// the overlay on the other person's account.
	//
	// The frontend does raise the prompt before calling in, so the tiles were safe in
	// practice. That is the problem: it made the rule a property of one Svelte component
	// rather than of the engine, and every other caller of SwitchTo inherited the hole.
	if err := kitBlocksEntry(active, target); err != nil {
		return err
	}

	steamRoot, err := e.life.SteamRoot()
	if err != nil {
		return err
	}
	from, err := e.life.CurrentAccount()
	if err != nil {
		return err
	}

	// A plain switch to a non-shared account needs no kit and no journal: nothing is
	// overlaid, so there is nothing to recover.
	if !e.sharedResolver(target) {
		return e.plainSwitch(ctx, target, personaState)
	}

	home, err := e.homeResolver()
	if err != nil {
		return err
	}
	if home.IsZero() {
		return ErrNoHomeAccount
	}
	if home.SteamID64 == target.SteamID64 {
		// Defensive: the role model already forbids Home being shared.
		return e.plainSwitch(ctx, target, personaState)
	}

	plans, err := e.plan(ctx, OperationEnter, home, target, steamRoot)
	if err != nil {
		return err
	}
	if len(plans) == 0 {
		// No ready module has anything to carry; this degenerates to a plain switch.
		return e.plainSwitch(ctx, target, personaState)
	}

	txID, err := newID()
	if err != nil {
		return err
	}
	j := NewJournal(txID, DirectionEnter, from, target, home, plans, personaState)
	e.recordScratchAnchors(j, steamRoot)

	// Fail before the journal exists, not halfway through writing files: a module that
	// cannot name a same-volume scratch directory would otherwise abort mid-apply.
	for _, p := range plans {
		if _, _, err := e.scratchDirs(txID, p.ModuleID, steamRoot, target); err != nil {
			return err
		}
	}

	// Steam must be closed before *any* snapshot, not just before the writes: a snapshot
	// taken while Steam is running can capture a half-flushed config.
	if err := e.ensureClosed(ctx); err != nil {
		return err
	}

	// Stage the Home kit first. Doing it before the journal exists keeps the target
	// untouched if the source turns out to be unreadable, and freezing the payload here
	// means later edits to Home cannot change what gets written.
	kitSnaps := map[string]SnapshotResult{}
	for _, m := range e.modulesFor(plans) {
		plan, _ := planFor(plans, m.ID())
		res, err := e.snapshot(ctx, m, txID, home, plan, PurposeKitSource, steamRoot)
		if err != nil {
			return err
		}
		kitSnaps[m.ID()] = res
		j.RecordKitSource(m.ID(), res.SnapshotID, res.Manifest.Digests())
	}

	// First durable write, before the first mutation of the target.
	if err := e.store.journal.Write(j); err != nil {
		return err
	}
	if err := e.store.journal.SetActive(txID); err != nil {
		return err
	}
	actionlog.Record("kit:enter", target.SteamID64, txID, nil)

	if err := e.store.journal.Advance(j, PhaseSteamClosed); err != nil {
		return err
	}

	// Save their setup.
	e.phase("Status_Kit_SavingTheirSetup", nil)
	if err := e.store.journal.Advance(j, PhaseSnapshotting); err != nil {
		return err
	}
	for _, m := range e.modulesFor(plans) {
		plan, _ := planFor(plans, m.ID())
		res, err := e.snapshot(ctx, m, txID, target, plan, PurposeTheirSetup, steamRoot)
		if err != nil {
			return e.failTransaction(j, err)
		}
		j.RecordTheirSetup(m.ID(), res.SnapshotID, res.Manifest.Digests())
		if err := e.store.journal.Write(j); err != nil {
			return e.failTransaction(j, err)
		}
		// Only now, with payload and manifest both committed and the journal referencing
		// them, does the last-known-good pointer advance. The snapshot it previously
		// named is left on disk.
		if err := e.store.setLastKnownGood(m.ID(), target, res.SnapshotID); err != nil {
			return e.failTransaction(j, err)
		}
		e.store.prune(m.ID(), target, e.store.protectedSnapshots(j, m.ID(), target))

		// The Home account needs pruning too, and it is the one that grows fastest: a
		// kit-source snapshot is taken on *every* Enter, whereas a "their setup" snapshot is
		// taken once per shared account. Left alone this is an unbounded pile of full copies
		// of the same config. Safe here because the journal now references the current
		// kit-source snapshot, so protectedSnapshots covers it.
		if home.SteamID64 != target.SteamID64 {
			e.store.prune(m.ID(), home, e.store.protectedSnapshots(j, m.ID(), home))
		}
	}
	if err := e.store.journal.Advance(j, PhaseSnapshotSaved); err != nil {
		return e.failTransaction(j, err)
	}

	// Apply the kit.
	e.phase("Status_Kit_ApplyingKit", nil)
	j.ResetPartStates()
	if err := e.store.journal.Advance(j, PhaseApplying); err != nil {
		return e.failTransaction(j, err)
	}
	for _, m := range e.modulesFor(plans) {
		plan, _ := planFor(plans, m.ID())
		src := kitSnaps[m.ID()]
		stageRoot, rollbackRoot, err := e.scratchDirs(txID, m.ID(), steamRoot, target)
		if err != nil {
			return e.failTransaction(j, err)
		}
		res, err := m.Apply(ctx, ApplyRequest{
			TransactionID: txID,
			Plan:          plan,
			SteamRoot:     steamRoot,
			PayloadPath:   src.PayloadPath,
			Expected:      src.Manifest,
			StageRoot:     stageRoot,
			RollbackRoot:  rollbackRoot,
			Journal:       e.partJournal(j, m.ID()),
		})
		if err != nil {
			return e.failTransaction(j, err)
		}
		j.RecordKitApplied(m.ID(), res.Manifest.Digests())
		for _, part := range res.AppliedParts {
			j.SetPartState(m.ID(), part, ReplaceVerified)
		}
		if err := e.store.journal.Write(j); err != nil {
			return e.failTransaction(j, err)
		}
	}
	if err := e.store.journal.Advance(j, PhaseKitApplied); err != nil {
		return e.failTransaction(j, err)
	}

	// Swap the login and launch.
	if err := e.swapAndLaunch(ctx, j, target, personaState,
		PhaseSwappingLogin, PhaseLoginSwapped, PhaseLaunching); err != nil {
		return err
	}

	if err := e.store.journal.Advance(j, PhaseKitActive); err != nil {
		return err
	}
	e.life.OnSwitchSucceeded(target)
	return nil
}

// swapAndLaunch performs the Steam-side half with a journal write around each step.
func (e *Engine) swapAndLaunch(ctx context.Context, j *Journal, target AccountRef, personaState int, swapping, swapped, launching Phase) error {
	e.phase("Status_ActionBar_UpdatingSteamLogin", nil)
	if err := e.store.journal.Advance(j, swapping); err != nil {
		return e.failTransaction(j, err)
	}
	if err := e.life.WriteLogin(ctx, target, personaState); err != nil {
		return e.failTransaction(j, err)
	}
	if err := e.store.journal.Advance(j, swapped); err != nil {
		return e.failTransaction(j, err)
	}

	e.phase("Status_StartingPlatform", map[string]string{"platform": "Steam"})
	if err := e.store.journal.Advance(j, launching); err != nil {
		return e.failTransaction(j, err)
	}
	if err := e.life.LaunchSteam(ctx); err != nil {
		return e.failTransaction(j, err)
	}
	return nil
}

// plainSwitch is the no-kit path: close, write login, launch. No journal, because nothing
// is overlaid and so nothing needs recovering.
func (e *Engine) plainSwitch(ctx context.Context, target AccountRef, personaState int) error {
	if err := e.ensureClosed(ctx); err != nil {
		return err
	}
	e.phase("Status_ActionBar_UpdatingSteamLogin", nil)
	if err := e.life.WriteLogin(ctx, target, personaState); err != nil {
		return err
	}
	e.phase("Status_StartingPlatform", map[string]string{"platform": "Steam"})
	if err := e.life.LaunchSteam(ctx); err != nil {
		return err
	}
	e.life.OnSwitchSucceeded(target)
	return nil
}

// Leave resolves an active kit and switches to `target`.
func (e *Engine) Leave(ctx context.Context, target AccountRef, choice LeaveChoice, personaState int) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	j, err := e.activeJournal()
	if err != nil {
		return err
	}
	if j == nil || (j.Phase != PhaseKitActive && j.Phase != PhaseKeptActive) {
		return ErrNoActiveKit
	}

	// The root this transaction was started against, so a Steam move between Enter and
	// Leave cannot point the restore at a different tree.
	steamRoot, err := e.journalSteamRoot(j)
	if err != nil {
		return err
	}
	j.LeaveTarget = &target

	if choice == LeaveKeepMine {
		// The overlay stays. The journal, snapshots and hashes are all retained so that
		// restoring remains possible later.
		if err := e.store.journal.Advance(j, PhaseLeavePlanned); err != nil {
			return err
		}
		if err := e.ensureClosed(ctx); err != nil {
			return err
		}
		if err := e.swapAndLaunch(ctx, j, target, personaState,
			PhaseSwappingLoginAfterRestore, PhaseLoginSwapped, PhaseLaunchingAfterRestore); err != nil {
			return err
		}
		if err := e.store.journal.Advance(j, PhaseKeptActive); err != nil {
			return err
		}
		e.life.OnSwitchSucceeded(target)
		return nil
	}

	if err := e.store.journal.Advance(j, PhaseLeavePlanned); err != nil {
		return err
	}
	if err := e.ensureClosed(ctx); err != nil {
		return err
	}
	if err := e.store.journal.Advance(j, PhaseClosingForRestore); err != nil {
		return err
	}

	if err := e.restoreTheirSetup(ctx, j, steamRoot); err != nil {
		return err
	}

	if err := e.swapAndLaunch(ctx, j, target, personaState,
		PhaseSwappingLoginAfterRestore, PhaseLoginSwapped, PhaseLaunchingAfterRestore); err != nil {
		return err
	}

	if err := e.store.journal.Advance(j, PhaseComplete); err != nil {
		return err
	}
	e.finish(j)
	e.life.OnSwitchSucceeded(target)
	return nil
}

// restoreTheirSetup checks for outside interference, then puts the saved setup back.
func (e *Engine) restoreTheirSetup(ctx context.Context, j *Journal, steamRoot string) error {
	e.phase("Status_Kit_CheckingChanges", nil)
	if err := e.store.journal.Advance(j, PhaseRestoreChecking); err != nil {
		return e.failTransaction(j, err)
	}

	// External-change detection. If the live tree no longer matches what we applied, the
	// other person has played (or Steam Cloud pulled), and writing over it would destroy
	// their work. Default is no write.
	for _, m := range e.modulesFor(j.Plans) {
		plan, _ := planFor(j.Plans, m.ID())
		expectedDigests := j.Hashes[m.ID()].KitApplied
		if len(expectedDigests) == 0 {
			continue
		}
		res, err := m.Verify(ctx, VerifyRequest{
			Account:   j.To,
			Plan:      plan,
			SteamRoot: steamRoot,
			Expected:  DigestOnlyManifest(m.ID(), expectedDigests),
		})
		if err != nil {
			return e.failTransaction(j, err)
		}
		if !res.Match {
			j.LastError = ErrExternalChange.Error()
			if err := e.store.journal.Advance(j, PhaseExternalChangeBlocked); err != nil {
				return err
			}
			actionlog.Record("kit:externalChange", j.To.SteamID64, m.ID(), nil)
			return ErrExternalChange
		}
	}

	e.phase("Status_Kit_RestoringTheirSetup", nil)
	j.ResetPartStates()
	if err := e.store.journal.Advance(j, PhaseRestoring); err != nil {
		return e.failTransaction(j, err)
	}

	for _, m := range e.modulesFor(j.Plans) {
		plan, _ := planFor(j.Plans, m.ID())
		snapID := j.Snapshots[m.ID()].TheirSetup
		if snapID == "" {
			continue
		}
		_, manifest, payload, err := e.store.readSnapshot(m.ID(), j.To, snapID)
		if err != nil {
			return e.failTransaction(j, err)
		}
		stageRoot, rollbackRoot, err := e.recoveryScratchDirs(j, m.ID(), steamRoot)
		if err != nil {
			return e.failTransaction(j, err)
		}
		res, err := m.Restore(ctx, RestoreRequest{
			TransactionID: j.TransactionID,
			Plan:          plan,
			SteamRoot:     steamRoot,
			PayloadPath:   payload,
			Expected:      manifest,
			StageRoot:     stageRoot,
			RollbackRoot:  rollbackRoot,
			Journal:       e.partJournal(j, m.ID()),
		})
		if err != nil {
			return e.failTransaction(j, err)
		}
		j.RecordRestored(m.ID(), res.Manifest.Digests())
		for _, part := range res.RestoredParts {
			j.SetPartState(m.ID(), part, ReplaceVerified)
		}
		if err := e.store.journal.Write(j); err != nil {
			return e.failTransaction(j, err)
		}
	}

	return e.store.journal.Advance(j, PhaseSetupRestored)
}

// failTransaction records the error on the journal so recovery has something to show, then
// returns the original error. The journal is deliberately *not* cleared: an interrupted
// transaction must keep blocking until the user resolves it.
func (e *Engine) failTransaction(j *Journal, cause error) error {
	if j != nil && cause != nil {
		j.LastError = cause.Error()
		if err := e.store.journal.Write(j); err != nil {
			engineLog.Error("could not record transaction failure", "err", err)
		}
	}
	actionlog.Record("kit:failed", j.TransactionID, "", cause)
	return cause
}

// finish archives a completed transaction and releases its scratch space.
func (e *Engine) finish(j *Journal) {
	if err := e.store.journal.Archive(j); err != nil {
		engineLog.Warn("archive journal", "err", err)
	}
	if err := e.store.journal.ClearActive(); err != nil {
		engineLog.Warn("clear active journal", "err", err)
	}
	if dir, err := e.store.txDir(j.TransactionID); err == nil {
		_ = os.RemoveAll(dir)
	}
	e.releaseScratch(j)
}

// partJournal returns the callback a module uses to make each atomic rename durable.
//
// Without it the engine could only record part states after `Apply` returned, so a crash
// halfway through a module's parts would leave every part reading `pending` while some were
// already moved aside — the exact ambiguity the substates exist to remove.
func (e *Engine) partJournal(j *Journal, moduleID string) PartJournal {
	return func(partID string, state ReplaceState) error {
		j.SetPartState(moduleID, partID, state)
		return e.store.journal.Write(j)
	}
}

// scratchDirs resolves the staging and rollback roots for one module's replacement pass.
//
// They must sit on the same filesystem as the live tree, because `ReplacePart` installs by
// renaming and a rename cannot cross volumes. The data root is the wrong home for them:
// Steam is routinely installed on a second drive while the data root follows %AppData% on
// the system drive, so anchoring scratch space there breaks every switch on such a machine.
//
// The paths are *derived*, never stored, so recovery on the next launch recomputes exactly
// the same locations from the transaction id and the module's own live paths. A journal that
// has been truncated or hand-edited therefore cannot point recovery at the wrong tree.
// ErrNoScratchAnchor means a module that writes files could not name a same-volume scratch
// location, so the transaction is refused before anything is touched.
var ErrNoScratchAnchor = errors.New("Toast_Kit_NoScratchAnchor")

func (e *Engine) scratchDirs(txID, moduleID, steamRoot string, account AccountRef) (string, string, error) {
	if m := e.moduleByID(moduleID); m != nil {
		if _, isAnchored := m.(ScratchAnchor); isAnchored {
			anchor, ok := e.scratchAnchor(m, steamRoot, account)
			if !ok {
				// A module that advertises an anchor but cannot produce one is not a
				// candidate for the data-root fallback: it writes into a tree we have just
				// failed to locate, so proceeding would either rename across volumes
				// mid-transaction or strand the rollback copies somewhere recovery will not
				// look. Refusing is the safe answer.
				return "", "", fmt.Errorf("%w: module %q", ErrNoScratchAnchor, moduleID)
			}
			base := filepath.Join(anchor, scratchDirName, txID)
			return filepath.Join(base, "staging"), filepath.Join(base, "rollback"), nil
		}
	}
	// Modules that never claimed an anchor use the data root. Reaching this branch means the
	// module does not implement ScratchAnchor at all, which today is only the test fakes.
	stage, err := e.store.stagingDir(txID, moduleID)
	if err != nil {
		return "", "", err
	}
	rollback, err := e.store.rollbackDir(txID, moduleID)
	if err != nil {
		return "", "", err
	}
	return stage, rollback, nil
}

// recordScratchAnchors freezes the Steam root and each module's anchor onto the journal.
//
// Called once, before the first mutation. Recovery reads these back instead of re-resolving,
// so moving the Steam install between the crash and the next launch cannot make the rollback
// copies invisible.
func (e *Engine) recordScratchAnchors(j *Journal, steamRoot string) {
	j.SteamRoot = steamRoot
	j.ScratchAnchors = map[string]string{}
	for _, m := range e.modulesFor(j.Plans) {
		if anchor, ok := e.scratchAnchor(m, steamRoot, j.To); ok {
			j.ScratchAnchors[m.ID()] = anchor
		}
	}
}

// journalSteamRoot prefers the root recorded at transaction time over a freshly resolved one.
func (e *Engine) journalSteamRoot(j *Journal) (string, error) {
	if j != nil && strings.TrimSpace(j.SteamRoot) != "" {
		return j.SteamRoot, nil
	}
	return e.life.SteamRoot()
}

// recoveryScratchDirs resolves scratch paths for a transaction being recovered, using the
// anchor the journal recorded. A recorded anchor that no longer exists is an error rather
// than a reason to fall through to a different location.
func (e *Engine) recoveryScratchDirs(j *Journal, moduleID, steamRoot string) (string, string, error) {
	anchor, ok := j.ScratchAnchors[moduleID]
	if !ok {
		return e.scratchDirs(j.TransactionID, moduleID, steamRoot, j.To)
	}
	if !filepath.IsAbs(anchor) {
		return "", "", fmt.Errorf("%w: recorded anchor %q for module %q",
			ErrJournalCorrupt, anchor, moduleID)
	}
	base := filepath.Join(anchor, scratchDirName, j.TransactionID)
	return filepath.Join(base, "staging"), filepath.Join(base, "rollback"), nil
}

// scratchAnchor asks a module where its volume is, rejecting anything that is not an
// absolute path so a bad implementation cannot plant scratch space in the working directory.
func (e *Engine) scratchAnchor(m Module, steamRoot string, account AccountRef) (string, bool) {
	a, ok := m.(ScratchAnchor)
	if !ok {
		return "", false
	}
	dir, ok := a.ScratchAnchor(steamRoot, account)
	if !ok || !filepath.IsAbs(dir) {
		return "", false
	}
	return dir, true
}

// releaseScratch removes a transaction's scratch space from every module's anchor.
//
// It runs only from `finish`, i.e. once the transaction is archived — the rollback copies
// under it are the last line of defence and must outlive every earlier cleanup point.
func (e *Engine) releaseScratch(j *Journal) {
	steamRoot, err := e.journalSteamRoot(j)
	if err != nil {
		return
	}
	for _, m := range e.modulesFor(j.Plans) {
		for _, account := range []AccountRef{j.To, j.KitSource} {
			if account.IsZero() {
				continue
			}
			if anchor, ok := e.scratchAnchor(m, steamRoot, account); ok {
				_ = os.RemoveAll(filepath.Join(anchor, scratchDirName, j.TransactionID))
			}
		}
	}
}

func (e *Engine) modulesFor(plans []ModulePlan) []Module {
	var out []Module
	for _, p := range plans {
		for _, m := range e.modules {
			if m.ID() == p.ModuleID {
				out = append(out, m)
				break
			}
		}
	}
	return out
}

func planFor(plans []ModulePlan, moduleID string) (ModulePlan, bool) {
	for _, p := range plans {
		if p.ModuleID == moduleID {
			return p, true
		}
	}
	return ModulePlan{}, false
}

// VerifyCloudRisk re-reads the cloud-synced parts after Steam has been up for a while, to
// catch Steam Cloud reverting the kit (REDESIGN.md §2, "Steam Cloud honesty").
//
// Read-only, so it is safe while Steam runs — and it must stay that way: re-applying with
// Steam up would just be clobbered again. A mismatch is recorded and surfaced; the re-apply
// is offered only once Steam is closed.
func (e *Engine) VerifyCloudRisk(ctx context.Context) (bool, error) {
	j, err := e.activeJournal()
	if err != nil || j == nil {
		return true, err
	}
	if j.Phase != PhaseKitActive && j.Phase != PhaseKeptActive {
		return true, nil
	}
	steamRoot, err := e.journalSteamRoot(j)
	if err != nil {
		return true, err
	}

	for _, m := range e.modulesFor(j.Plans) {
		plan, _ := planFor(j.Plans, m.ID())
		if !plan.HasCloudRisk() {
			continue
		}
		expected := j.Hashes[m.ID()].KitApplied
		if len(expected) == 0 {
			continue
		}
		res, err := m.Verify(ctx, VerifyRequest{
			Account:   j.To,
			Plan:      plan,
			SteamRoot: steamRoot,
			Expected:  DigestOnlyManifest(m.ID(), expected),
		})
		if err != nil {
			return true, err
		}
		if !res.Match {
			actionlog.Record("kit:cloudClobber", j.To.SteamID64, m.ID(), nil)
			return false, nil
		}
	}
	return true, nil
}

// cloudSettleDelay is how long to wait after Steam appears before sampling cloud-risk parts.
// Steam pulls cloud state shortly after login; sampling immediately would report a false
// "still applied".
const cloudSettleDelay = 20 * time.Second
