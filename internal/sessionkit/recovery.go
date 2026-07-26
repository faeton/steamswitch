package sessionkit

import (
	"context"
	"errors"

	"steamswitch/internal/actionlog"
)

// Crash recovery (REDESIGN.md §2).
//
// On launch, an unfinished journal blocks switching and the user is shown what happened.
// Classification is driven by the durable phase plus the per-part replacement substates, so
// the answer does not depend on guessing what the process was doing when it died.

// RecoveryKind is what the UI should offer.
type RecoveryKind string

const (
	// RecoveryNone: nothing outstanding.
	RecoveryNone RecoveryKind = "none"
	// RecoveryKitActive: a kit is legitimately live. Not an error; the strip says so.
	RecoveryKitActive RecoveryKind = "kit-active"
	// RecoveryInterrupted: a transaction died mid-flight.
	RecoveryInterrupted RecoveryKind = "interrupted"
	// RecoveryExternalChange: restore is blocked pending a user decision.
	RecoveryExternalChange RecoveryKind = "external-change"
)

// RecoveryState is what the frontend renders.
type RecoveryState struct {
	Kind          RecoveryKind `json:"kind"`
	TransactionID string       `json:"transactionId,omitempty"`
	Phase         string       `json:"phase,omitempty"`
	Direction     string       `json:"direction,omitempty"`
	// TargetSteamID64 is the account whose files are affected.
	TargetSteamID64 string `json:"targetSteamId64,omitempty"`
	// KitApplied reports whether the overlay actually landed, which decides whether
	// "Restore their setup" is a meaningful offer.
	KitApplied bool `json:"kitApplied"`
	// CanRestore is false when no "their setup" snapshot was captured before the crash —
	// in that case nothing was overwritten either.
	CanRestore bool     `json:"canRestore"`
	Modules    []string `json:"modules,omitempty"`
	LastError  string   `json:"lastError,omitempty"`
	StartedAt  string   `json:"startedAt,omitempty"`
}

// Status classifies the current state without changing anything. Safe to call while the app
// is still locked: it exposes no account paths or hashes, only that something needs
// attention.
func (e *Engine) Status() (RecoveryState, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.statusLocked()
}

func (e *Engine) statusLocked() (RecoveryState, error) {
	j, err := e.activeJournal()
	if err != nil {
		// A journal we cannot parse is itself a reason to block: acting on a switch while
		// an unreadable transaction record exists risks compounding the damage.
		if errors.Is(err, ErrJournalCorrupt) || errors.Is(err, ErrJournalUnknownVersion) {
			return RecoveryState{Kind: RecoveryInterrupted, LastError: err.Error()}, nil
		}
		return RecoveryState{}, err
	}
	if j == nil {
		return RecoveryState{Kind: RecoveryNone}, nil
	}

	st := RecoveryState{
		TransactionID:   j.TransactionID,
		Phase:           string(j.Phase),
		Direction:       string(j.Direction),
		TargetSteamID64: j.To.SteamID64,
		LastError:       j.LastError,
		StartedAt:       j.StartedAt,
	}
	for _, p := range j.Plans {
		st.Modules = append(st.Modules, p.ModuleID)
		if snaps, ok := j.Snapshots[p.ModuleID]; ok && snaps.TheirSetup != "" {
			st.CanRestore = true
		}
		if h, ok := j.Hashes[p.ModuleID]; ok && len(h.KitApplied) > 0 {
			st.KitApplied = true
		}
	}

	switch {
	case j.Phase == PhaseExternalChangeBlocked:
		st.Kind = RecoveryExternalChange
	case j.Phase == PhaseKitActive || j.Phase == PhaseKeptActive:
		st.Kind = RecoveryKitActive
	case j.Phase == PhaseComplete:
		st.Kind = RecoveryNone
	default:
		st.Kind = RecoveryInterrupted
	}
	return st, nil
}

// RecoveryAction is the user's choice from the recovery prompt.
type RecoveryAction string

const (
	// ActionRestoreTheirs rolls the shared account back to its saved setup.
	ActionRestoreTheirs RecoveryAction = "restore-theirs"
	// ActionKeepCurrent accepts whatever is on disk and closes the transaction.
	ActionKeepCurrent RecoveryAction = "keep-current"
	// ActionAbandon discards the transaction without touching files. Used when the crash
	// happened before anything was written.
	ActionAbandon RecoveryAction = "abandon"
)

// Resolve applies the user's recovery choice and unblocks the app.
func (e *Engine) Resolve(ctx context.Context, action RecoveryAction) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	j, err := e.activeJournal()
	if err != nil {
		// An unparseable journal can only be abandoned; there is nothing to act on.
		if action == ActionAbandon && (errors.Is(err, ErrJournalCorrupt) || errors.Is(err, ErrJournalUnknownVersion)) {
			actionlog.Record("kit:abandonCorrupt", "", "", nil)
			return e.store.journal.ClearActive()
		}
		return err
	}
	if j == nil {
		return nil
	}

	switch action {
	case ActionAbandon:
		// Abandon means "this transaction never happened". That is only truthful while
		// nothing has been overlaid: once a part is `verified`, the shared account is
		// carrying the kit, and discarding the journal would strand it there with no record
		// that it needs restoring. Refuse and make the user pick a real answer instead.
		if e.hasCompletedWrites(j) {
			return ErrAbandonAfterWrite
		}
		// Undo any half-finished replacement first, so "abandon" never leaves a tree in
		// the mixed state the crash created.
		if err := e.rollbackIncomplete(j); err != nil {
			return err
		}
		actionlog.Record("kit:abandon", j.TransactionID, string(j.Phase), nil)
		e.finish(j)
		return nil

	case ActionKeepCurrent:
		actionlog.Record("kit:keepCurrent", j.TransactionID, string(j.Phase), nil)
		if j.Phase == PhaseExternalChangeBlocked {
			// Their files won, so the overlay is gone; there is nothing left to restore.
			e.finish(j)
			return nil
		}
		// The overlay stands and stays tracked, so restoring remains available later.
		return e.store.journal.Advance(j, PhaseKeptActive)

	case ActionRestoreTheirs:
		steamRoot, err := e.journalSteamRoot(j)
		if err != nil {
			return err
		}
		if err := e.ensureClosed(ctx); err != nil {
			return err
		}
		if err := e.rollbackIncomplete(j); err != nil {
			return err
		}
		// Force the restore past the external-change gate: the user has been shown the
		// difference and chosen to discard it.
		if err := e.restoreFromSnapshots(ctx, j, steamRoot); err != nil {
			return err
		}
		if err := e.store.journal.Advance(j, PhaseComplete); err != nil {
			return err
		}
		actionlog.Record("kit:restoreTheirs", j.TransactionID, j.To.SteamID64, nil)
		e.finish(j)
		return nil

	default:
		return errors.New("sessionkit: unknown recovery action")
	}
}

// ErrAbandonAfterWrite refuses to discard a transaction that already overlaid files.
var ErrAbandonAfterWrite = errors.New("Toast_Kit_AbandonAfterWrite")

// hasCompletedWrites reports whether any part finished its replacement and was verified.
//
// This is what distinguishes "the crash happened before we touched anything", where abandon
// is honest, from "the kit is on their account", where it is a way to lose track of it.
func (e *Engine) hasCompletedWrites(j *Journal) bool {
	for _, parts := range j.ModuleState {
		for _, state := range parts {
			if state == ReplaceVerified {
				return true
			}
		}
	}
	return false
}

// rollbackIncomplete undoes any part left mid-replacement.
//
// A part recorded as `verified` is left alone: the write completed and was checked, so it is
// the snapshot restore below — not a rollback — that should undo it if the user asked for
// that. Rolling it back here as well would double-apply.
func (e *Engine) rollbackIncomplete(j *Journal) error {
	// The root recorded when the transaction started, not a freshly resolved one — see
	// Journal.SteamRoot.
	steamRoot, err := e.journalSteamRoot(j)
	if err != nil {
		return err
	}
	for moduleID, parts := range j.ModuleState {
		plan, ok := j.PlanFor(moduleID)
		if !ok {
			continue
		}
		m := e.moduleByID(moduleID)
		if m == nil {
			continue
		}
		stageRoot, rollbackRoot, err := e.recoveryScratchDirs(j, moduleID, steamRoot)
		if err != nil {
			return err
		}
		for partID, state := range parts {
			// `pending` is NOT skipped. The journal is a lower bound: a crash between the
			// rename that moves the live tree aside and the write that records it leaves the
			// state at the value before the rename. `RollbackPart` treats the rollback
			// directory as the authority precisely so it can be called on any state, and
			// calling it on a part that genuinely never moved is a no-op.
			if state == ReplaceVerified {
				// A verified part completed and was hash-checked, so it is not "incomplete".
				// Undoing it is the snapshot restore's job, not a rollback's — doing both
				// would apply the old tree twice.
				continue
			}
			live, ok := e.livePathFor(m, steamRoot, j.To, plan, partID)
			if !ok {
				continue
			}
			if err := RollbackPart(
				live,
				partPath(stageRoot, partID),
				partPath(rollbackRoot, partID),
				state,
			); err != nil {
				return err
			}
			j.SetPartState(moduleID, partID, ReplacePending)
		}
	}
	return e.store.journal.Write(j)
}

// restoreFromSnapshots writes every recorded "their setup" back, skipping the
// external-change gate that Leave applies.
func (e *Engine) restoreFromSnapshots(ctx context.Context, j *Journal, steamRoot string) error {
	j.ResetPartStates()
	if err := e.store.journal.Advance(j, PhaseRestoring); err != nil {
		return err
	}
	for _, m := range e.modulesFor(j.Plans) {
		plan, _ := planFor(j.Plans, m.ID())
		snapID := j.Snapshots[m.ID()].TheirSetup
		if snapID == "" {
			continue
		}
		_, manifest, payload, err := e.store.readSnapshot(m.ID(), j.To, snapID)
		if err != nil {
			return err
		}
		stageRoot, rollbackRoot, err := e.recoveryScratchDirs(j, m.ID(), steamRoot)
		if err != nil {
			return err
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
			return err
		}
		j.RecordRestored(m.ID(), res.Manifest.Digests())
		for _, part := range res.RestoredParts {
			j.SetPartState(m.ID(), part, ReplaceVerified)
		}
		if err := e.store.journal.Write(j); err != nil {
			return err
		}
	}
	return e.store.journal.Advance(j, PhaseSetupRestored)
}

func (e *Engine) moduleByID(id string) Module {
	for _, m := range e.modules {
		if m.ID() == id {
			return m
		}
	}
	return nil
}

// LivePathResolver lets the engine find a part's live path for rollback without knowing the
// game's layout. Modules that do not implement it cannot be rolled back automatically; they
// are skipped and left to their own Restore.
type LivePathResolver interface {
	LivePath(steamRoot string, account AccountRef, partID string) (string, bool)
}

func (e *Engine) livePathFor(m Module, steamRoot string, account AccountRef, plan ModulePlan, partID string) (string, bool) {
	r, ok := m.(LivePathResolver)
	if !ok {
		return "", false
	}
	return r.LivePath(steamRoot, account, partID)
}
