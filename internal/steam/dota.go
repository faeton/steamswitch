package steam

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// DotaAppID is Dota 2's Steam application ID. Its per-account configuration lives
// under <steam>/userdata/<id32>/570/.
const DotaAppID = "570"

// dotaGlobalCfgRelPath is the machine-wide cfg folder relative to a steamapps dir. It is
// shared by every account on the machine, so copying it between accounts is a no-op —
// it is included only so snapshots can capture and restore autoexec.cfg and friends.
var dotaGlobalCfgRelPath = filepath.Join("common", "dota 2 beta", "game", "dota", "cfg")

// resolveDotaGlobalCfg finds the Dota install's cfg folder. Dota is frequently installed
// on a secondary library drive, so the default steamapps dir is not enough: every library
// in libraryfolders.vdf is checked. Returns "" when Dota is not installed.
func resolveDotaGlobalCfg(steamRoot string) string {
	dirs, err := steamAppsDirs(steamRoot)
	if err != nil {
		dirs = []string{filepath.Join(filepath.Clean(steamRoot), "steamapps")}
	}
	for _, d := range dirs {
		p := filepath.Join(d, dotaGlobalCfgRelPath)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
	}
	return ""
}

// Dota config part identifiers. These are the strings the frontend passes.
const (
	DotaPartLocal     = "local"     // userdata/<id32>/570/local  — not cloud-synced
	DotaPartRemote    = "remote"    // userdata/<id32>/570/remote — cloud-synced (hero grids etc.)
	DotaPartGlobalCfg = "globalcfg" // steamapps/common/dota 2 beta/game/dota/cfg — machine-wide
)

// DotaPartsAll is every selectable part.
var DotaPartsAll = []string{DotaPartLocal, DotaPartRemote, DotaPartGlobalCfg}

// dotaPartsDefault is what an empty selection means. It deliberately EXCLUDES
// DotaPartGlobalCfg: that folder is shared by every account on the machine, so a caller
// that forgot to pass parts must never end up overwriting it.
var dotaPartsDefault = []string{DotaPartLocal, DotaPartRemote}

var (
	errDotaNoParts       = errors.New("Toast_Dota_NoPartsSelected")
	errDotaSameAccount   = errors.New("Toast_SameAccount")
	errDotaNoSnapshot    = errors.New("Toast_Dota_SnapshotMissing")
	errDotaInvalidLabel  = errors.New("Toast_Dota_InvalidLabel")
	errDotaNothingCopied = errors.New("Toast_Dota_NothingToCopy")
)

// ErrDotaSteamRunning is returned when a copy that touches the cloud-synced remote/
// folder is attempted while Steam is running. Steam Cloud would re-sync the old files
// over the copy on next launch, silently reverting it.
var ErrDotaSteamRunning = errors.New("Toast_Dota_SteamRunning")

// DotaSnapshot is one saved, labelled game configuration in the snapshot library. It keeps its
// name for the sake of the Dota snapshots already on disk; see gameconfig.go for the general path.
type DotaSnapshot struct {
	ID string `json:"id"`
	// Label is the user-facing name, e.g. "Ceb's config" or "my 2026 setup".
	Label string `json:"label"`
	// Note is free-form text, e.g. where the config came from.
	Note string `json:"note,omitempty"`
	// SourceSteamID64 is the account the snapshot was taken from ("" for imported).
	SourceSteamID64 string `json:"sourceSteamId64,omitempty"`
	// SourceAccountName is a display name captured at snapshot time.
	SourceAccountName string `json:"sourceAccountName,omitempty"`
	// Parts lists which folders this snapshot actually contains.
	Parts []string `json:"parts"`
	// CreatedAt is RFC3339 UTC.
	CreatedAt string `json:"createdAt"`
	// Auto marks snapshots taken automatically before an overwrite, so the UI can
	// group them as "revert points" rather than curated configs.
	Auto bool `json:"auto"`
	// EmptyParts lists parts the destination did not have when an automatic revert point was
	// taken. Undoing has to remove what the copy created there, or the undo is only partial.
	EmptyParts []string `json:"emptyParts,omitempty"`
	// AppID is the game this snapshot belongs to. Snapshots from before other games were
	// supported have none, and are Dota's.
	AppID string `json:"appId,omitempty"`
	// SizeBytes is the on-disk size of the snapshot payload.
	SizeBytes int64 `json:"sizeBytes"`
}

// DotaCopyResult reports what a copy or apply actually moved.
type DotaCopyResult struct {
	CopiedParts  []string `json:"copiedParts"`
	SkippedParts []string `json:"skippedParts"`
	// RemovedParts were absent from the snapshot being applied, so applying it removed them.
	RemovedParts []string `json:"removedParts,omitempty"`
	// RevertSnapshotID is the auto-snapshot of the destination taken before the write.
	RevertSnapshotID string `json:"revertSnapshotId,omitempty"`
	// Failed is true when a part write aborted partway, so the destination is in a
	// mixed state and RevertSnapshotID should be offered prominently.
	Failed bool `json:"failed,omitempty"`
	// CloudFollowUp is true when a cloud-synced part was written. Steam may still show a
	// cloud conflict on the next launch of that account; the UI must say so rather than
	// claiming the copy is durable.
	CloudFollowUp bool `json:"cloudFollowUp,omitempty"`
}

// dotaAccountPartPath returns the live path for one part of an account's Dota config. The global
// cfg folder is machine-wide and independent of id32; it resolves to "" (and reports false) when
// Dota is not installed in any Steam library.
func dotaAccountPartPath(steamRoot, id32, part string) (string, bool) {
	return gamePartPath(steamRoot, id32, DotaAppID, part)
}

func dotaSnapshotDir(id string) (string, error) { return gameSnapshotDir(DotaAppID, id) }

func normalizeDotaParts(parts []string) ([]string, error) {
	return normalizeGameParts(DotaAppID, parts)
}

// dotaSteamRunningGuard is gameCloudGuard for Dota, where only remote/ is cloud-synced.
func dotaSteamRunningGuard(parts []string, allowCloudRisk bool) error {
	return gameCloudGuard(DotaAppID, parts, allowCloudRisk)
}

func (s *SteamService) dotaAccountID32(steamID64 string) (string, error) {
	f, err := FormatsFromID64(strings.TrimSpace(steamID64))
	if err != nil {
		return "", errSteamDataInvalidID
	}
	return f.ID32, nil
}

func (s *SteamService) captureDotaSnapshot(steamRoot, id32, steamID64, accountName, label, note string, parts []string, auto bool) (DotaSnapshot, error) {
	return s.captureGameSnapshot(DotaAppID, steamRoot, id32, steamID64, accountName, label, note, parts, auto)
}

func (s *SteamService) applyDotaParts(
	steamRoot, destID64 string,
	parts []string,
	srcFor func(part string) (string, bool),
) (DotaCopyResult, error) {
	return s.applyGameParts(DotaAppID, steamRoot, destID64, parts, nil, srcFor)
}

// The Dota-named service methods are the general ones with the app ID filled in.

// SnapshotDotaConfig saves an account's Dota configuration under a label, e.g. "Ceb's config".
func (s *SteamService) SnapshotDotaConfig(steamID64, label, note string, parts []string) (DotaSnapshot, error) {
	return s.SnapshotGameConfig(DotaAppID, steamID64, label, note, parts)
}

// ListDotaSnapshots returns the Dota snapshot library, newest first.
func (s *SteamService) ListDotaSnapshots() ([]DotaSnapshot, error) {
	return s.ListGameConfigSnapshots(DotaAppID)
}

// RenameDotaSnapshot updates a snapshot's label and note.
func (s *SteamService) RenameDotaSnapshot(snapshotID, label, note string) (DotaSnapshot, error) {
	return s.RenameGameConfigSnapshot(DotaAppID, snapshotID, label, note)
}

// DeleteDotaSnapshot removes a snapshot from the library.
func (s *SteamService) DeleteDotaSnapshot(snapshotID string) error {
	return s.DeleteGameConfigSnapshot(DotaAppID, snapshotID)
}

// OpenDotaSnapshotFolder reveals a snapshot on disk.
func (s *SteamService) OpenDotaSnapshotFolder(snapshotID string) error {
	return s.OpenGameConfigSnapshotFolder(DotaAppID, snapshotID)
}

// CopyDotaConfigBetween copies Dota 2 configuration directly from one account to another.
func (s *SteamService) CopyDotaConfigBetween(sourceSteamID64, destSteamID64 string, parts []string, allowCloudRisk bool) (DotaCopyResult, error) {
	return s.CopyGameConfigBetween(DotaAppID, sourceSteamID64, destSteamID64, parts, allowCloudRisk)
}

// ApplyDotaSnapshot writes a labelled snapshot onto a destination account.
func (s *SteamService) ApplyDotaSnapshot(snapshotID, destSteamID64 string, parts []string, allowCloudRisk bool) (DotaCopyResult, error) {
	return s.ApplyGameConfigSnapshot(DotaAppID, snapshotID, destSteamID64, parts, allowCloudRisk)
}

// IsSteamRunning lets the UI warn before a copy that Steam Cloud could revert.
//
// False here means either "closed" or "this build cannot tell", so it is only ever used to
// *add* a warning. Anything that must not run while Steam is up goes through
// requireProcessInspection first, which distinguishes the two.
func (s *SteamService) IsSteamRunning() bool { return steamClientRunning() }
