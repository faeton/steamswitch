package steam

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"steamswitch/internal/fsutil"
	"steamswitch/internal/paths"
	"steamswitch/internal/platform"
	"steamswitch/internal/security"

	"github.com/google/uuid"
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

// DotaSnapshot is one saved, labelled Dota configuration in the snapshot library.
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
	// SizeBytes is the on-disk size of the snapshot payload.
	SizeBytes int64 `json:"sizeBytes"`
}

// DotaCopyResult reports what a copy or apply actually moved.
type DotaCopyResult struct {
	CopiedParts  []string `json:"copiedParts"`
	SkippedParts []string `json:"skippedParts"`
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

// dotaWriteMu serialises every operation that mutates a live Dota tree or the snapshot
// library. The frontend's `busy` flag is not a lock, and the tray/CLI can reach these
// paths too, so two overlapping applies could otherwise interleave RemoveAll/CopyDir on
// the same destination.
var dotaWriteMu sync.Mutex

func dotaSnapshotsRoot() (string, error) {
	r, err := paths.DataRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(r, "Backups", "Dota"), nil
}

func dotaSnapshotDir(id string) (string, error) {
	root, err := dotaSnapshotsRoot()
	if err != nil {
		return "", err
	}
	id = strings.TrimSpace(id)
	if id == "" || strings.ContainsAny(id, `/\.`) {
		return "", errDotaNoSnapshot
	}
	return filepath.Join(root, id), nil
}

// dotaAccountPartPath returns the live path for one part of an account's Dota config.
// The global cfg folder is machine-wide and therefore independent of id32; it resolves to
// "" (and reports false) when Dota is not installed in any Steam library.
func dotaAccountPartPath(steamRoot, id32, part string) (string, bool) {
	switch part {
	case DotaPartLocal:
		return filepath.Join(steamRoot, "userdata", id32, DotaAppID, "local"), true
	case DotaPartRemote:
		return filepath.Join(steamRoot, "userdata", id32, DotaAppID, "remote"), true
	case DotaPartGlobalCfg:
		p := resolveDotaGlobalCfg(steamRoot)
		return p, p != ""
	}
	return "", false
}

func normalizeDotaParts(parts []string) ([]string, error) {
	if len(parts) == 0 {
		return append([]string(nil), dotaPartsDefault...), nil
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		switch p {
		case DotaPartLocal, DotaPartRemote, DotaPartGlobalCfg:
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	if len(out) == 0 {
		return nil, errDotaNoParts
	}
	// Keep a stable order so logs and snapshot metadata read consistently.
	sort.Slice(out, func(i, j int) bool { return dotaPartRank(out[i]) < dotaPartRank(out[j]) })
	return out, nil
}

func dotaPartRank(p string) int {
	switch p {
	case DotaPartLocal:
		return 0
	case DotaPartRemote:
		return 1
	default:
		return 2
	}
}

func containsPart(parts []string, want string) bool {
	for _, p := range parts {
		if p == want {
			return true
		}
	}
	return false
}

// dotaSteamRunningGuard blocks writes to the cloud-synced remote/ folder while Steam
// is running, because Steam Cloud would overwrite them on next launch.
func dotaSteamRunningGuard(parts []string) error {
	if !containsPart(parts, DotaPartRemote) {
		return nil
	}
	// "Cannot tell" must not read as "not running" — see requireProcessInspection. This is the
	// guard with no backstop: nothing else stops a remote/ write from being silently reverted
	// by Steam Cloud.
	if err := requireProcessInspection(); err != nil {
		return err
	}
	if steamClientRunning() {
		return ErrDotaSteamRunning
	}
	return nil
}

func (s *SteamService) dotaAccountID32(steamID64 string) (string, error) {
	f, err := FormatsFromID64(strings.TrimSpace(steamID64))
	if err != nil {
		return "", errSteamDataInvalidID
	}
	return f.ID32, nil
}

func dirHasContent(p string) bool {
	ent, err := os.ReadDir(p)
	return err == nil && len(ent) > 0
}

func dirSizeBytes(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, e := d.Info(); e == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// replaceDir removes dst and copies src over it, leaving dst absent if src is absent.
func replaceDir(src, dst string) error {
	if !dirHasContent(src) {
		return nil
	}
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return fsutil.CopyDir(src, dst)
}

// ---------------------------------------------------------------------------
// Snapshot library
// ---------------------------------------------------------------------------

func writeSnapshotMeta(dir string, snap DotaSnapshot) error {
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(filepath.Join(dir, "snapshot.json"), data, 0o644)
}

func readSnapshotMeta(dir string) (DotaSnapshot, error) {
	data, err := os.ReadFile(filepath.Join(dir, "snapshot.json"))
	if err != nil {
		return DotaSnapshot{}, err
	}
	var snap DotaSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return DotaSnapshot{}, err
	}
	return snap, nil
}

// maxAutoRevertSnapshots caps how many automatic revert points are kept. Each one is a
// full copy of a Dota config tree, so an uncapped library would grow without bound and
// bury the user's curated configs. Curated (non-auto) snapshots are never pruned.
const maxAutoRevertSnapshots = 10

// pruneAutoSnapshots deletes the oldest automatic revert points beyond the cap.
// Best-effort: failures are ignored, since pruning must never fail a copy.
func (s *SteamService) pruneAutoSnapshots() {
	root, err := dotaSnapshotsRoot()
	if err != nil {
		return
	}
	ents, err := os.ReadDir(root)
	if err != nil {
		return
	}
	var autos []DotaSnapshot
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		snap, err := readSnapshotMeta(filepath.Join(root, e.Name()))
		if err != nil || !snap.Auto {
			continue
		}
		autos = append(autos, snap)
	}
	if len(autos) <= maxAutoRevertSnapshots {
		return
	}
	sort.Slice(autos, func(i, j int) bool { return autos[i].CreatedAt > autos[j].CreatedAt })
	for _, snap := range autos[maxAutoRevertSnapshots:] {
		if dir, err := dotaSnapshotDir(snap.ID); err == nil {
			_ = os.RemoveAll(dir)
		}
	}
}

// captureDotaSnapshot copies the requested parts of an account's live Dota config into
// a new labelled snapshot folder. Parts with no content on disk are skipped and
// omitted from the snapshot's Parts list.
func (s *SteamService) captureDotaSnapshot(steamRoot, id32, steamID64, accountName, label, note string, parts []string, auto bool) (DotaSnapshot, error) {
	id := uuid.NewString()
	dir, err := dotaSnapshotDir(id)
	if err != nil {
		return DotaSnapshot{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return DotaSnapshot{}, err
	}

	var captured []string
	for _, part := range parts {
		src, ok := dotaAccountPartPath(steamRoot, id32, part)
		if !ok || !dirHasContent(src) {
			continue
		}
		if err := fsutil.CopyDir(src, filepath.Join(dir, part)); err != nil {
			_ = os.RemoveAll(dir)
			return DotaSnapshot{}, err
		}
		captured = append(captured, part)
	}
	if len(captured) == 0 {
		_ = os.RemoveAll(dir)
		return DotaSnapshot{}, errDotaNothingCopied
	}

	snap := DotaSnapshot{
		ID:                id,
		Label:             label,
		Note:              note,
		SourceSteamID64:   steamID64,
		SourceAccountName: accountName,
		Parts:             captured,
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
		Auto:              auto,
		SizeBytes:         dirSizeBytes(dir),
	}
	if err := writeSnapshotMeta(dir, snap); err != nil {
		_ = os.RemoveAll(dir)
		return DotaSnapshot{}, err
	}
	return snap, nil
}

// SnapshotDotaConfig saves the given account's Dota configuration into the snapshot
// library under a user-supplied label, e.g. "Ceb's config".
func (s *SteamService) SnapshotDotaConfig(steamID64, label, note string, parts []string) (DotaSnapshot, error) {
	dotaWriteMu.Lock()
	defer dotaWriteMu.Unlock()
	if err := security.RequireUnlocked(); err != nil {
		return DotaSnapshot{}, err
	}
	label = strings.TrimSpace(label)
	if label == "" {
		return DotaSnapshot{}, errDotaInvalidLabel
	}
	sel, err := normalizeDotaParts(parts)
	if err != nil {
		return DotaSnapshot{}, err
	}
	root, err := s.steamInstallRoot()
	if err != nil {
		return DotaSnapshot{}, err
	}
	if strings.TrimSpace(root) == "" {
		return DotaSnapshot{}, errSteamDataInvalidID
	}
	id32, err := s.dotaAccountID32(steamID64)
	if err != nil {
		return DotaSnapshot{}, err
	}
	return s.captureDotaSnapshot(root, id32, steamID64, s.dotaAccountDisplayName(steamID64), label, strings.TrimSpace(note), sel, false)
}

// ListDotaSnapshots returns the snapshot library, newest first.
func (s *SteamService) ListDotaSnapshots() ([]DotaSnapshot, error) {
	if err := security.RequireUnlocked(); err != nil {
		return nil, err
	}
	root, err := dotaSnapshotsRoot()
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []DotaSnapshot{}, nil
		}
		return nil, err
	}
	out := make([]DotaSnapshot, 0, len(ents))
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		snap, err := readSnapshotMeta(filepath.Join(root, e.Name()))
		if err != nil {
			continue
		}
		out = append(out, snap)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

// RenameDotaSnapshot updates a snapshot's label and note, e.g. to tag a revert point
// as somebody else's config after importing it.
func (s *SteamService) RenameDotaSnapshot(snapshotID, label, note string) (DotaSnapshot, error) {
	if err := security.RequireUnlocked(); err != nil {
		return DotaSnapshot{}, err
	}
	label = strings.TrimSpace(label)
	if label == "" {
		return DotaSnapshot{}, errDotaInvalidLabel
	}
	dir, err := dotaSnapshotDir(snapshotID)
	if err != nil {
		return DotaSnapshot{}, err
	}
	snap, err := readSnapshotMeta(dir)
	if err != nil {
		return DotaSnapshot{}, errDotaNoSnapshot
	}
	snap.Label = label
	snap.Note = strings.TrimSpace(note)
	// A snapshot the user has deliberately named is a curated config, not a revert point.
	snap.Auto = false
	if err := writeSnapshotMeta(dir, snap); err != nil {
		return DotaSnapshot{}, err
	}
	return snap, nil
}

// DeleteDotaSnapshot removes a snapshot from the library.
func (s *SteamService) DeleteDotaSnapshot(snapshotID string) error {
	dotaWriteMu.Lock()
	defer dotaWriteMu.Unlock()
	if err := security.RequireUnlocked(); err != nil {
		return err
	}
	dir, err := dotaSnapshotDir(snapshotID)
	if err != nil {
		return err
	}
	if _, err := readSnapshotMeta(dir); err != nil {
		return errDotaNoSnapshot
	}
	return os.RemoveAll(dir)
}

// OpenDotaSnapshotFolder reveals a snapshot on disk.
func (s *SteamService) OpenDotaSnapshotFolder(snapshotID string) error {
	if err := security.RequireUnlocked(); err != nil {
		return err
	}
	dir, err := dotaSnapshotDir(snapshotID)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err != nil {
		return errDotaNoSnapshot
	}
	return platform.OpenPathInFileManager(dir)
}

// ---------------------------------------------------------------------------
// Copy / apply
// ---------------------------------------------------------------------------

func (s *SteamService) dotaAccountDisplayName(steamID64 string) string {
	accounts, err := s.GetSteamAccounts()
	if err != nil {
		return ""
	}
	steamID64 = strings.TrimSpace(steamID64)
	for _, a := range accounts {
		if strings.TrimSpace(a.SteamID64) == steamID64 {
			if n := strings.TrimSpace(a.DisplayName); n != "" {
				return n
			}
			return strings.TrimSpace(a.AccountName)
		}
	}
	return ""
}

func autoRevertLabel(accountName, steamID64 string) string {
	who := strings.TrimSpace(accountName)
	if who == "" {
		who = strings.TrimSpace(steamID64)
	}
	return fmt.Sprintf("Before overwrite — %s", who)
}

// applyDotaParts writes each part from srcFor() into the destination account, taking an
// automatic revert snapshot of the destination first.
func (s *SteamService) applyDotaParts(
	steamRoot, destID64 string,
	parts []string,
	srcFor func(part string) (string, bool),
) (DotaCopyResult, error) {
	var res DotaCopyResult

	destID32, err := s.dotaAccountID32(destID64)
	if err != nil {
		return res, err
	}

	// Revert point: snapshot whatever the destination has today before touching it.
	destName := s.dotaAccountDisplayName(destID64)
	if snap, err := s.captureDotaSnapshot(
		steamRoot, destID32, destID64, destName,
		autoRevertLabel(destName, destID64), "Automatic revert point", parts, true,
	); err == nil {
		res.RevertSnapshotID = snap.ID
	} else if !errors.Is(err, errDotaNothingCopied) {
		return res, err
	}
	s.pruneAutoSnapshots()

	for _, part := range parts {
		src, ok := srcFor(part)
		if !ok || !dirHasContent(src) {
			res.SkippedParts = append(res.SkippedParts, part)
			continue
		}
		dst, ok := dotaAccountPartPath(steamRoot, destID32, part)
		if !ok {
			res.SkippedParts = append(res.SkippedParts, part)
			continue
		}
		if err := replaceDir(src, dst); err != nil {
			// The destination tree was already removed at this point; tell the caller
			// which revert snapshot restores it rather than failing silently.
			res.Failed = true
			return res, err
		}
		if part == DotaPartRemote {
			// remotecache.vdf is Steam's local inventory of the cloud payload (paths,
			// sizes, hashes). Leaving the pre-copy version in place invites Steam to
			// treat the new files as stale and re-download the old ones.
			_ = os.Remove(filepath.Join(steamRoot, "userdata", destID32, DotaAppID, "remotecache.vdf"))
			res.CloudFollowUp = true
		}
		res.CopiedParts = append(res.CopiedParts, part)
	}
	if len(res.CopiedParts) == 0 {
		return res, errDotaNothingCopied
	}
	return res, nil
}

// CopyDotaConfigBetween copies Dota 2 configuration directly from one account to
// another. Unlike CopySteamGameSettingsFrom, neither account has to be the one
// currently signed in to Steam.
func (s *SteamService) CopyDotaConfigBetween(sourceSteamID64, destSteamID64 string, parts []string) (DotaCopyResult, error) {
	// Before dotaWriteMu, never after. The guard reads the engine's state, which takes
	// Engine.mu; taking dotaWriteMu first would invert the documented lock order and deadlock
	// against a transaction that already holds Engine.mu and is waiting for dotaWriteMu.
	if err := guardManualConfigWrite(); err != nil {
		return DotaCopyResult{}, err
	}
	dotaWriteMu.Lock()
	defer dotaWriteMu.Unlock()
	var res DotaCopyResult
	if err := security.RequireUnlocked(); err != nil {
		return res, err
	}
	sel, err := normalizeDotaParts(parts)
	if err != nil {
		return res, err
	}
	sourceSteamID64 = strings.TrimSpace(sourceSteamID64)
	destSteamID64 = strings.TrimSpace(destSteamID64)
	if sourceSteamID64 == destSteamID64 {
		return res, errDotaSameAccount
	}
	if err := dotaSteamRunningGuard(sel); err != nil {
		return res, err
	}
	root, err := s.steamInstallRoot()
	if err != nil {
		return res, err
	}
	if strings.TrimSpace(root) == "" {
		return res, errSteamDataInvalidID
	}
	srcID32, err := s.dotaAccountID32(sourceSteamID64)
	if err != nil {
		return res, err
	}

	return s.applyDotaParts(root, destSteamID64, sel, func(part string) (string, bool) {
		// The global cfg folder is machine-wide: copying it from A to B changes nothing,
		// so it is skipped for account-to-account copies and only used in snapshots.
		if part == DotaPartGlobalCfg {
			return "", false
		}
		return dotaAccountPartPath(root, srcID32, part)
	})
}

// ApplyDotaSnapshot writes a labelled snapshot onto a destination account, taking a
// revert point of that account first.
func (s *SteamService) ApplyDotaSnapshot(snapshotID, destSteamID64 string, parts []string) (DotaCopyResult, error) {
	// See CopyDotaConfigBetween: the engine guard must run before dotaWriteMu.
	if err := guardManualConfigWrite(); err != nil {
		return DotaCopyResult{}, err
	}
	dotaWriteMu.Lock()
	defer dotaWriteMu.Unlock()
	var res DotaCopyResult
	if err := security.RequireUnlocked(); err != nil {
		return res, err
	}
	dir, err := dotaSnapshotDir(snapshotID)
	if err != nil {
		return res, err
	}
	snap, err := readSnapshotMeta(dir)
	if err != nil {
		return res, errDotaNoSnapshot
	}
	sel, err := normalizeDotaParts(parts)
	if err != nil {
		return res, err
	}
	// Only apply what this snapshot actually holds.
	var usable []string
	for _, p := range sel {
		if containsPart(snap.Parts, p) {
			usable = append(usable, p)
		}
	}
	if len(usable) == 0 {
		return res, errDotaNothingCopied
	}
	if err := dotaSteamRunningGuard(usable); err != nil {
		return res, err
	}
	root, err := s.steamInstallRoot()
	if err != nil {
		return res, err
	}
	if strings.TrimSpace(root) == "" {
		return res, errSteamDataInvalidID
	}
	return s.applyDotaParts(root, destSteamID64, usable, func(part string) (string, bool) {
		return filepath.Join(dir, part), true
	})
}

// DotaAccountStatus describes what Dota config an account currently has on disk, so the
// UI can enable or disable copy targets.
type DotaAccountStatus struct {
	SteamID64    string   `json:"steamId64"`
	PresentParts []string `json:"presentParts"`
}

// GetDotaAccountStatuses reports which Dota config folders exist for each given account.
func (s *SteamService) GetDotaAccountStatuses(steamIDs []string) ([]DotaAccountStatus, error) {
	if err := security.RequireUnlocked(); err != nil {
		return nil, err
	}
	root, err := s.steamInstallRoot()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(root) == "" {
		return nil, errSteamDataInvalidID
	}
	out := make([]DotaAccountStatus, 0, len(steamIDs))
	globalPresent := dirHasContent(filepath.Join(root, dotaGlobalCfgRelPath))
	for _, id := range steamIDs {
		st := DotaAccountStatus{SteamID64: strings.TrimSpace(id), PresentParts: []string{}}
		id32, err := s.dotaAccountID32(id)
		if err == nil {
			for _, part := range []string{DotaPartLocal, DotaPartRemote} {
				if p, ok := dotaAccountPartPath(root, id32, part); ok && dirHasContent(p) {
					st.PresentParts = append(st.PresentParts, part)
				}
			}
		}
		if globalPresent {
			st.PresentParts = append(st.PresentParts, DotaPartGlobalCfg)
		}
		out = append(out, st)
	}
	return out, nil
}

// IsSteamRunning lets the UI warn before a copy that Steam Cloud could revert.
//
// False here means either "closed" or "this build cannot tell", so it is only ever used to
// *add* a warning. Anything that must not run while Steam is up goes through
// requireProcessInspection first, which distinguishes the two.
func (s *SteamService) IsSteamRunning() bool { return steamClientRunning() }
