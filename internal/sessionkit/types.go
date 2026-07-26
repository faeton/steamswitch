// Package sessionkit implements the Session Kit: a journalled, crash-recoverable
// transaction that carries the Home account's game configuration onto a shared account and
// puts the other person's setup back afterwards (REDESIGN.md §2).
//
// The package deliberately knows nothing about Steam or about any particular game. It
// depends only on the standard library plus `internal/fsutil`, `internal/paths`,
// `internal/security` and `internal/actionlog`. Everything platform-shaped arrives through
// two injected interfaces:
//
//	Module    — one game (Dota 2 today, CS2 next); implemented in the package that owns
//	            that game's file layout.
//	Lifecycle — closing Steam, swapping the login, relaunching; implemented in
//	            `internal/steam`.
//
// That direction is deliberate: `internal/platform` is imported by nearly everything and
// must not import `steam`/`basic`, so behaviour is pushed *into* the engine from `main.go`
// rather than reached out for. See CLAUDE.md, "Startup and wiring".
package sessionkit

import (
	"context"
	"time"
)

// PartRisk classifies whether a part is safe to write locally or is cloud-synced and can be
// silently reverted by Steam Cloud after the fact.
type PartRisk string

const (
	PartLocalSafe PartRisk = "local-safe"
	PartCloudRisk PartRisk = "cloud-risk"
)

// Part is one addressable unit of a game's configuration, e.g. Dota's `local` or `remote`.
type Part struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Risk  PartRisk `json:"risk"`
	// KitEligible is false for machine-wide parts. Dota's `globalcfg` is shared by every
	// account on the box, so copying it between accounts is a no-op at best and a
	// cross-account clobber at worst; it is captured in snapshots but never travels.
	KitEligible bool `json:"kitEligible"`
}

// AccountRef identifies an account to a module. Steam is the only namespace today.
type AccountRef struct {
	SteamID64 string `json:"steamId64"`
}

func (a AccountRef) IsZero() bool { return a.SteamID64 == "" }

// Operation is why a preflight is being run; modules may refuse some and allow others.
type Operation string

const (
	OperationEnter   Operation = "enter"
	OperationRestore Operation = "restore"
	OperationVerify  Operation = "verify"
)

// ---------------------------------------------------------------------------
// Module interface
// ---------------------------------------------------------------------------

// Module is one pluggable game (REDESIGN.md §5).
//
// Implementations must be safe to call concurrently only in their read-only methods
// (Detect, Verify); the engine serialises everything that mutates.
type Module interface {
	// ID is a stable identifier used in journals and on disk. Never localise it.
	ID() string

	// DisplayName is shown in the status strip, e.g. "Dota 2".
	DisplayName() string

	// Detect reports whether the game is installed and the module is ready to run.
	// Must not mutate anything.
	Detect(ctx context.Context, req DetectRequest) (Detection, error)

	// Preflight resolves a concrete plan without touching disk: which parts apply, where
	// they live, and whether anything blocks the operation (a running game, a missing
	// path). Returning an error aborts before the journal is written.
	Preflight(ctx context.Context, req PreflightRequest) (ModulePlan, error)

	// Snapshot copies an account's live parts into `req.Destination` and returns a
	// manifest of what was captured. The destination is an engine-owned directory that
	// does not yet exist.
	Snapshot(ctx context.Context, req SnapshotRequest) (SnapshotResult, error)

	// Apply writes a previously captured snapshot onto the target account.
	// Implementations must stage and verify rather than delete-then-copy; the engine's
	// ReplaceTree helper does this correctly.
	Apply(ctx context.Context, req ApplyRequest) (ApplyResult, error)

	// Restore is Apply in the other direction: it puts a captured "their setup" back.
	// Unlike Apply it must honour *absence* — a part recorded as absent is removed.
	Restore(ctx context.Context, req RestoreRequest) (RestoreResult, error)

	// Verify hashes an account's live parts and compares against an expected manifest.
	// Read-only, so it is the one method that may run while Steam is up.
	Verify(ctx context.Context, req VerifyRequest) (VerifyResult, error)
}

type DetectRequest struct {
	SteamRoot string
}

type Detection struct {
	Installed bool `json:"installed"`
	Ready     bool `json:"ready"`
	Paused    bool `json:"paused"`
	// Reason is an i18n key explaining a non-ready state, following the `dota.go`
	// convention of using translation keys as sentinel strings.
	Reason string `json:"reason,omitempty"`
	Parts  []Part `json:"parts"`
	// Fingerprint changes when the game's on-disk layout changes. A mismatch against the
	// fingerprint recorded at plan time auto-pauses the module until a self-test passes
	// (REDESIGN.md §5).
	Fingerprint string    `json:"fingerprint"`
	CheckedAt   time.Time `json:"checkedAt"`
}

type PreflightRequest struct {
	Operation Operation
	Source    AccountRef
	Target    AccountRef
	SteamRoot string
	// Parts optionally narrows the selection; empty means "every kit-eligible part".
	Parts []string
}

// ModulePlan is the frozen decision about what a module will touch in this transaction.
type ModulePlan struct {
	ModuleID             string     `json:"moduleId"`
	Source               AccountRef `json:"source"`
	Target               AccountRef `json:"target"`
	Parts                []Part     `json:"parts"`
	DetectionFingerprint string     `json:"detectionFingerprint"`
}

// PartIDs is the plan's part list flattened for logging and journal keys.
func (p ModulePlan) PartIDs() []string {
	out := make([]string, 0, len(p.Parts))
	for _, part := range p.Parts {
		out = append(out, part.ID)
	}
	return out
}

// HasCloudRisk reports whether any planned part is cloud-synced, which downgrades the
// post-apply status from "done" to "applied · cloud may override".
func (p ModulePlan) HasCloudRisk() bool {
	for _, part := range p.Parts {
		if part.Risk == PartCloudRisk {
			return true
		}
	}
	return false
}

// SnapshotPurpose distinguishes the two snapshots a switch takes.
type SnapshotPurpose string

const (
	// PurposeTheirSetup is the shared account's own configuration, saved before overlay.
	PurposeTheirSetup SnapshotPurpose = "their-setup"
	// PurposeKitSource is the Home account's configuration, staged so that edits to Home
	// between planning and applying cannot change what gets written.
	PurposeKitSource SnapshotPurpose = "kit-source"
)

type SnapshotRequest struct {
	TransactionID string
	Account       AccountRef
	Plan          ModulePlan
	Purpose       SnapshotPurpose
	SteamRoot     string
	// Destination is an absolute path the module must create and fill, one subdirectory
	// per part id.
	Destination string
}

type SnapshotResult struct {
	SnapshotID    string   `json:"snapshotId"`
	PayloadPath   string   `json:"payloadPath"`
	CapturedParts []string `json:"capturedParts"`
	Manifest      Manifest `json:"manifest"`
}

// PartJournal makes a per-part replacement substate durable.
//
// The engine cannot journal around `Apply` as a whole: a crash halfway through a module's
// parts would leave the journal saying only "applying", with nothing to say which parts had
// already been moved aside. Modules therefore call this after every atomic rename, and the
// engine turns it into a journal write. An error means the write failed — the module must
// abort rather than continue past a checkpoint it could not record.
type PartJournal func(partID string, state ReplaceState) error

type ApplyRequest struct {
	TransactionID string
	Plan          ModulePlan
	SteamRoot     string
	// PayloadPath holds the verified source tree, one subdirectory per part id.
	PayloadPath string
	// Expected is the manifest recorded when PayloadPath was captured.
	Expected Manifest
	// StageRoot is a scratch directory on the same volume as the live tree.
	StageRoot string
	// RollbackRoot receives the displaced live tree, one subdirectory per part id. Recovery
	// derives the same path independently, so a module must not invent its own.
	RollbackRoot string
	// Journal records each part's replacement substate durably. Never nil.
	Journal PartJournal
}

type ApplyResult struct {
	AppliedParts []string `json:"appliedParts"`
	Manifest     Manifest `json:"manifest"`
	CloudRisk    bool     `json:"cloudRisk"`
	// SideEffects records adapter-specific extras (Dota's `remotecache.vdf` removal) and
	// whether they succeeded, so a partial success is not reported as a clean one.
	SideEffects map[string]bool `json:"sideEffects,omitempty"`
}

type RestoreRequest struct {
	TransactionID string
	Plan          ModulePlan
	SteamRoot     string
	PayloadPath   string
	Expected      Manifest
	StageRoot     string
	RollbackRoot  string
	Journal       PartJournal
}

type RestoreResult struct {
	RestoredParts []string `json:"restoredParts"`
	Manifest      Manifest `json:"manifest"`
}

type VerifyRequest struct {
	Account   AccountRef
	Plan      ModulePlan
	SteamRoot string
	Expected  Manifest
}

type VerifyResult struct {
	Match       bool       `json:"match"`
	Current     Manifest   `json:"current"`
	Differences []FileDiff `json:"differences,omitempty"`
}

// ---------------------------------------------------------------------------
// Steam lifecycle
// ---------------------------------------------------------------------------

// Lifecycle is the Steam-side half of a switch, split out of the monolithic
// `steam.SwapToAccount` so the engine can journal between each step.
//
// The split matters: `SwapToAccount` closes Steam, writes the login, bumps statistics and
// launches in one call, and short-circuits when the target is already current. A kit apply
// has to happen *between* "Steam is closed" and "login is written", and the short circuit
// would skip the close entirely.
type Lifecycle interface {
	// SteamRoot resolves the Steam install directory.
	SteamRoot() (string, error)

	// CurrentAccount reports which account Steam is currently set to log in as.
	CurrentAccount() (AccountRef, error)

	// CloseSteam terminates Steam and its helpers, returning only once they are gone.
	CloseSteam(ctx context.Context) error

	// RunningProcesses lists any Steam or game processes still holding files, for the
	// "Waiting for Steam to close" status.
	RunningProcesses() []string

	// WriteLogin points Steam at the given account without launching it.
	WriteLogin(ctx context.Context, account AccountRef, personaState int) error

	// LaunchSteam starts Steam.
	LaunchSteam(ctx context.Context) error

	// OnSwitchSucceeded runs the bookkeeping that used to live at the tail of
	// SwapToAccount: tray recents, stability, statistics, presence. Called once, only
	// after the whole transaction has committed, so a failed switch is not counted.
	OnSwitchSucceeded(account AccountRef)
}

// ProgressSink receives narration for the status strip. Injected rather than calling
// `platform.EmitActionBarStatus*` directly, so this package need not import platform.
type ProgressSink interface {
	// Phase reports an i18n key plus substitution variables.
	Phase(key string, vars map[string]string)
}

// noopSink is used when the caller supplies none, so the engine never nil-checks.
type noopSink struct{}

func (noopSink) Phase(string, map[string]string) {}

// scratchDirName is the per-transaction scratch folder a module's ScratchAnchor hosts.
//
// Leading dot so Steam's own directory listings ignore it, and a fixed name so a leftover
// from a killed process is recognisable — and removable — by a later run.
const scratchDirName = ".steamswitch-tx"

// ScratchAnchor lets a module put the engine's staging and rollback directories on the same
// filesystem as the files being replaced.
//
// `ReplacePart` installs by renaming, and a rename cannot cross a filesystem boundary. The
// data root is not a safe anchor: Steam is very often on a second drive while the data root
// follows %AppData% on the system drive. A module that replaces files therefore has to say
// where its own volume is.
//
// The returned directory must already be on the target volume and must be derivable purely
// from its arguments — recovery recomputes it on the next launch instead of reading it back
// out of the journal, so a corrupted journal cannot redirect a rollback at another tree.
type ScratchAnchor interface {
	ScratchAnchor(steamRoot string, account AccountRef) (string, bool)
}
