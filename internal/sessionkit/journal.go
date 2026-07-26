package sessionkit

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"steamswitch/internal/fsutil"
)

// The transaction journal (REDESIGN.md §2, "Safety").
//
// Every mutation is preceded by a durable journal write, so that whatever the app was doing
// when it died can be reconstructed on the next launch. The journal is the authority — not
// an assumption that a shutdown handler got to finish.

// JournalSchemaVersion guards against a future format being read by an older build. An
// unrecognised version makes recovery refuse to act rather than guess.
const JournalSchemaVersion = 1

// Phase is the top-level transaction state.
//
// `PhaseKitActive`, `PhaseKeptActive` and `PhaseExternalChangeBlocked` are *stable* resting
// states, not incomplete work: seeing one on startup is normal. Every other non-terminal
// phase means the process died mid-flight and recovery must run.
type Phase string

const (
	PhasePlanned       Phase = "planned"
	PhaseClosingSteam  Phase = "closing-steam"
	PhaseSteamClosed   Phase = "steam-closed"
	PhaseSnapshotting  Phase = "snapshotting"
	PhaseSnapshotSaved Phase = "snapshot-saved"
	PhaseApplying      Phase = "applying"
	PhaseKitApplied    Phase = "kit-applied"
	PhaseSwappingLogin Phase = "swapping-login"
	PhaseLoginSwapped  Phase = "login-swapped"
	PhaseLaunching     Phase = "launching"

	// Resting states.
	PhaseKitActive             Phase = "kit-active"
	PhaseKeptActive            Phase = "kept-active"
	PhaseExternalChangeBlocked Phase = "external-change-blocked"

	// Leaving.
	PhaseLeavePlanned              Phase = "leave-planned"
	PhaseClosingForRestore         Phase = "closing-steam-for-restore"
	PhaseRestoreChecking           Phase = "restore-checking"
	PhaseRestoring                 Phase = "restoring"
	PhaseSetupRestored             Phase = "setup-restored"
	PhaseSwappingLoginAfterRestore Phase = "swapping-login-after-restore"
	PhaseLaunchingAfterRestore     Phase = "launching-after-restore"

	PhaseComplete Phase = "complete"
)

// restingPhases are the states in which no work is outstanding.
var restingPhases = map[Phase]bool{
	PhaseKitActive:             true,
	PhaseKeptActive:            true,
	PhaseExternalChangeBlocked: true,
	PhaseComplete:              true,
}

// IsResting reports whether a phase represents a settled transaction rather than one
// interrupted mid-flight.
func (p Phase) IsResting() bool { return restingPhases[p] }

// NeedsRecovery reports whether finding this phase on startup means work was interrupted.
func (p Phase) NeedsRecovery() bool { return !p.IsResting() && p != "" }

// PartState within a replacement — tracked per part so a crash between the two renames of a
// staged replace is recoverable.
type ReplaceState string

const (
	ReplacePending   ReplaceState = "pending"
	ReplaceStaged    ReplaceState = "staged"
	ReplaceOldMoved  ReplaceState = "old-moved"
	ReplaceInstalled ReplaceState = "installed"
	ReplaceVerified  ReplaceState = "verified"
)

// Direction distinguishes an entering transaction from a leaving one.
type Direction string

const (
	DirectionEnter Direction = "enter"
	DirectionLeave Direction = "leave"
)

// ModuleSnapshots records the two snapshot ids a module produced for this transaction.
type ModuleSnapshots struct {
	TheirSetup string `json:"theirSetup,omitempty"`
	KitSource  string `json:"kitSource,omitempty"`
}

// ModuleHashes are the compact per-part digests at each checkpoint.
type ModuleHashes struct {
	TheirSetup map[string]string `json:"theirSetup,omitempty"`
	KitSource  map[string]string `json:"kitSource,omitempty"`
	KitApplied map[string]string `json:"kitApplied,omitempty"`
	Restored   map[string]string `json:"restored,omitempty"`
}

// Journal is the on-disk record of one transaction.
type Journal struct {
	SchemaVersion int       `json:"schemaVersion"`
	TransactionID string    `json:"transactionId"`
	Phase         Phase     `json:"phase"`
	Direction     Direction `json:"direction"`

	From AccountRef `json:"from"`
	To   AccountRef `json:"to"`
	// LeaveTarget is where the user wants to end up when leaving a kit-active account.
	LeaveTarget *AccountRef `json:"leaveTarget,omitempty"`

	// KitSource is the Home account the kit was taken from.
	KitSource AccountRef   `json:"kitSource"`
	Plans     []ModulePlan `json:"plans"`

	// SteamRoot is the install directory as it was when the transaction started.
	//
	// Recovery must not re-resolve it: if the user moves Steam, points the app at a different
	// install, or a library drive is offline, a freshly resolved root would name a *different*
	// tree, and the rollback directories holding the original files would be silently
	// unreachable — reported as "nothing to roll back" rather than as the failure it is.
	SteamRoot string `json:"steamRoot,omitempty"`
	// ScratchAnchors is moduleID -> the directory that hosted staging and rollback. Recorded
	// for the same reason, and validated before use.
	ScratchAnchors map[string]string `json:"scratchAnchors,omitempty"`

	Snapshots map[string]ModuleSnapshots `json:"snapshots"`
	Hashes    map[string]ModuleHashes    `json:"hashes"`
	// ModuleState is moduleID -> partID -> replacement substate.
	ModuleState map[string]map[string]ReplaceState `json:"moduleState"`

	PersonaState int    `json:"personaState"`
	StartedAt    string `json:"startedAt"`
	UpdatedAt    string `json:"updatedAt"`
	LastError    string `json:"lastError,omitempty"`
}

var (
	// ErrJournalUnknownVersion refuses a journal written by a newer build.
	ErrJournalUnknownVersion = errors.New("Toast_Kit_JournalVersion")
	// ErrJournalCorrupt covers unparseable or internally inconsistent journals.
	ErrJournalCorrupt = errors.New("Toast_Kit_JournalCorrupt")
)

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// NewJournal starts a transaction record. It is not durable until Write is called.
func NewJournal(txID string, dir Direction, from, to, kitSource AccountRef, plans []ModulePlan, personaState int) *Journal {
	ts := nowRFC3339()
	return &Journal{
		SchemaVersion: JournalSchemaVersion,
		TransactionID: txID,
		Phase:         PhasePlanned,
		Direction:     dir,
		From:          from,
		To:            to,
		KitSource:     kitSource,
		Plans:         plans,
		Snapshots:     map[string]ModuleSnapshots{},
		Hashes:        map[string]ModuleHashes{},
		ModuleState:   map[string]map[string]ReplaceState{},
		PersonaState:  personaState,
		StartedAt:     ts,
		UpdatedAt:     ts,
	}
}

// PlanFor returns the plan for a module id.
func (j *Journal) PlanFor(moduleID string) (ModulePlan, bool) {
	for _, p := range j.Plans {
		if p.ModuleID == moduleID {
			return p, true
		}
	}
	return ModulePlan{}, false
}

// SetPartState records a replacement substate, creating the module map on demand.
func (j *Journal) SetPartState(moduleID, partID string, state ReplaceState) {
	if j.ModuleState == nil {
		j.ModuleState = map[string]map[string]ReplaceState{}
	}
	if j.ModuleState[moduleID] == nil {
		j.ModuleState[moduleID] = map[string]ReplaceState{}
	}
	j.ModuleState[moduleID][partID] = state
}

// ResetPartStates puts every planned part of every module back to pending, ready for a new
// replacement pass (an apply, or a later restore).
func (j *Journal) ResetPartStates() {
	j.ModuleState = map[string]map[string]ReplaceState{}
	for _, plan := range j.Plans {
		for _, part := range plan.Parts {
			j.SetPartState(plan.ModuleID, part.ID, ReplacePending)
		}
	}
}

func (j *Journal) setHashes(moduleID string, mutate func(*ModuleHashes)) {
	if j.Hashes == nil {
		j.Hashes = map[string]ModuleHashes{}
	}
	h := j.Hashes[moduleID]
	mutate(&h)
	j.Hashes[moduleID] = h
}

func (j *Journal) RecordTheirSetup(moduleID, snapshotID string, digests map[string]string) {
	if j.Snapshots == nil {
		j.Snapshots = map[string]ModuleSnapshots{}
	}
	s := j.Snapshots[moduleID]
	s.TheirSetup = snapshotID
	j.Snapshots[moduleID] = s
	j.setHashes(moduleID, func(h *ModuleHashes) { h.TheirSetup = digests })
}

func (j *Journal) RecordKitSource(moduleID, snapshotID string, digests map[string]string) {
	if j.Snapshots == nil {
		j.Snapshots = map[string]ModuleSnapshots{}
	}
	s := j.Snapshots[moduleID]
	s.KitSource = snapshotID
	j.Snapshots[moduleID] = s
	j.setHashes(moduleID, func(h *ModuleHashes) { h.KitSource = digests })
}

func (j *Journal) RecordKitApplied(moduleID string, digests map[string]string) {
	j.setHashes(moduleID, func(h *ModuleHashes) { h.KitApplied = digests })
}

func (j *Journal) RecordRestored(moduleID string, digests map[string]string) {
	j.setHashes(moduleID, func(h *ModuleHashes) { h.Restored = digests })
}

// Validate rejects a journal that cannot be acted on safely.
func (j *Journal) Validate() error {
	if j == nil {
		return ErrJournalCorrupt
	}
	if j.SchemaVersion != JournalSchemaVersion {
		return fmt.Errorf("%w: schema %d", ErrJournalUnknownVersion, j.SchemaVersion)
	}
	if !isSafeID(j.TransactionID) {
		return fmt.Errorf("%w: transaction id %q", ErrJournalCorrupt, j.TransactionID)
	}
	if j.Phase == "" {
		return fmt.Errorf("%w: empty phase", ErrJournalCorrupt)
	}
	for _, plan := range j.Plans {
		if strings.TrimSpace(plan.ModuleID) == "" {
			return fmt.Errorf("%w: plan with no module id", ErrJournalCorrupt)
		}
	}
	for _, snaps := range j.Snapshots {
		for _, id := range []string{snaps.TheirSetup, snaps.KitSource} {
			if id != "" && !isSafeID(id) {
				return fmt.Errorf("%w: snapshot id %q", ErrJournalCorrupt, id)
			}
		}
	}
	return nil
}

// isSafeID rejects anything that could escape the data directory when used as a path
// segment. Journal and snapshot ids reach the filesystem, and a journal on disk is not a
// trusted input — a hand-edited or corrupted one must not be able to point at `../..`.
func isSafeID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			continue
		default:
			return false
		}
	}
	return true
}

// activePointer is the tiny file naming the in-flight transaction.
type activePointer struct {
	SchemaVersion int    `json:"schemaVersion"`
	TransactionID string `json:"transactionId"`
}

// journalStore persists journals under the session-kit root.
type journalStore struct{ root string }

func (s journalStore) activePath() string { return filepath.Join(s.root, "active.json") }

func (s journalStore) txDir(txID string) (string, error) {
	if !isSafeID(txID) {
		return "", fmt.Errorf("%w: transaction id %q", ErrJournalCorrupt, txID)
	}
	return filepath.Join(s.root, "transactions", txID), nil
}

func (s journalStore) journalPath(txID string) (string, error) {
	dir, err := s.txDir(txID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "journal.json"), nil
}

// Write persists the journal and stamps UpdatedAt. Both this and the active pointer go
// through WriteFileAtomic, which fsyncs a same-directory temp file before renaming.
func (s journalStore) Write(j *Journal) error {
	j.UpdatedAt = nowRFC3339()
	path, err := s.journalPath(j.TransactionID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, data, 0o644)
}

// Advance moves to a new phase and persists in one step, so no caller can change the phase
// in memory and forget to make it durable.
func (s journalStore) Advance(j *Journal, phase Phase) error {
	j.Phase = phase
	return s.Write(j)
}

func (s journalStore) SetActive(txID string) error {
	if !isSafeID(txID) {
		return fmt.Errorf("%w: transaction id %q", ErrJournalCorrupt, txID)
	}
	data, err := json.MarshalIndent(activePointer{
		SchemaVersion: JournalSchemaVersion,
		TransactionID: txID,
	}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(s.activePath(), data, 0o644)
}

func (s journalStore) ClearActive() error {
	err := os.Remove(s.activePath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// LoadActive returns the in-flight journal, or (nil, nil) when there is none.
func (s journalStore) LoadActive() (*Journal, error) {
	data, err := os.ReadFile(s.activePath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var ptr activePointer
	if err := json.Unmarshal(data, &ptr); err != nil {
		return nil, fmt.Errorf("%w: active pointer: %v", ErrJournalCorrupt, err)
	}
	if ptr.SchemaVersion != JournalSchemaVersion {
		return nil, fmt.Errorf("%w: active pointer schema %d", ErrJournalUnknownVersion, ptr.SchemaVersion)
	}
	return s.Load(ptr.TransactionID)
}

func (s journalStore) Load(txID string) (*Journal, error) {
	path, err := s.journalPath(txID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// The pointer outlived its journal: treat as nothing in flight rather than
			// blocking the app forever on a transaction whose record is gone.
			return nil, nil
		}
		return nil, err
	}
	var j Journal
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrJournalCorrupt, err)
	}
	if err := j.Validate(); err != nil {
		return nil, err
	}
	return &j, nil
}

// Archive moves a finished journal out of the active set, keeping it for diagnostics.
func (s journalStore) Archive(j *Journal) error {
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Join(s.root, "archive")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(filepath.Join(dir, j.TransactionID+".json"), data, 0o644)
}
