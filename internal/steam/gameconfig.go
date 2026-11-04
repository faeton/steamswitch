package steam

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"steamswitch/internal/actionlog"
	"steamswitch/internal/fsutil"
	"steamswitch/internal/paths"
	"steamswitch/internal/platform"
	"steamswitch/internal/security"

	"github.com/google/uuid"
)

// Copying a game's per-account config between two accounts, for any game.
//
// Neither account has to be signed in. This is the manual tool. The Session Kit's "Moving your
// game settings" step is a different thing: it journals a kit onto a shared account *during* a
// switch, only for Dota so far, and refuses CS2 outright (cs2Enabled). The two never run at once,
// because guardManualConfigWrite refuses here while a kit is live.
//
// The same rules hold for every game:
//   - the destination is snapshotted first, including what it lacks, so any copy can be undone;
//   - each part is replaced, never merged;
//   - cloud-synced parts are refused while Steam runs unless the risk was accepted;
//   - nothing is written while the game runs;
//   - IDs from the frontend are validated.
//
// What a "part" holds is per game and is read from disk, not assumed:
//
//	Dota 2 (570)   local/ video and local options; remote/ binds, hero grids, cloud-synced.
//	CS2 (730)      local/cfg/ holds cs2_user_convars, cs2_user_keys, cs2_video.txt per public
//	               guides, which also call it cloud-synced. Unlike Dota's local/, so it is treated
//	               as cloud-synced here. Not yet checked against a real install.
//	Anything else  Unknown layout. remote/ is whatever the game keeps in Steam Cloud and may
//	               hold save games. local/ is treated as cloud-synced because nobody has shown
//	               otherwise. Games that store settings in Documents or the install folder have
//	               nothing here to copy.

var (
	errGameRunning            = errors.New("Toast_GameConfig_GameRunning")
	errGameConfigRolledBack   = errors.New("Toast_GameConfig_FailedRolledBack")
	errGameConfigNotRolledBck = errors.New("Toast_GameConfig_FailedNotRolledBack")
	errNotARevertPoint        = errors.New("Toast_Dota_SnapshotMissing")
)

// dotaWriteMu serialises every operation that mutates a live config tree or the snapshot library.
// The frontend's `busy` flag is not a lock, and the tray/CLI can reach these paths too, so two
// overlapping applies could otherwise interleave RemoveAll/CopyDir on the same destination.
var dotaWriteMu sync.Mutex

// gameConfigExcludedApps are numeric folders in userdata that are Steam's own, not a game's:
// 7 is the client's settings, 760 screenshots. Copying those is not what this tool is for.
var gameConfigExcludedApps = map[string]bool{"7": true, "760": true}

func normalizeConfigAppID(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !isAllDigitRunes(s) || len(s) > 10 || strings.Trim(s, "0") == "" || gameConfigExcludedApps[s] {
		return "", errSteamDataInvalidID
	}
	return s, nil
}

type gameProfile struct {
	name string
	// processNames in Windows form; other OSes drop the suffix.
	processNames []string
	// globalCfg finds a machine-wide cfg folder that snapshots may capture.
	globalCfg func(steamRoot string) string
	// localNotCloudSynced is only set where that has been established for this game.
	localNotCloudSynced bool
}

var gameProfiles = map[string]gameProfile{
	DotaAppID: {name: "Dota 2", processNames: dotaProcessNames, globalCfg: resolveDotaGlobalCfg, localNotCloudSynced: true},
	CS2AppID:  {name: "Counter-Strike 2", processNames: cs2ProcessNames},
}

func gamePartPath(steamRoot, id32, appID, part string) (string, bool) {
	switch part {
	case DotaPartLocal:
		return filepath.Join(steamRoot, "userdata", id32, appID, "local"), true
	case DotaPartRemote:
		return filepath.Join(steamRoot, "userdata", id32, appID, "remote"), true
	case DotaPartGlobalCfg:
		if resolve := gameProfiles[appID].globalCfg; resolve != nil {
			p := resolve(steamRoot)
			return p, p != ""
		}
	}
	return "", false
}

func partCloudSynced(appID, part string) bool {
	switch part {
	case DotaPartRemote:
		return true
	case DotaPartLocal:
		return !gameProfiles[appID].localNotCloudSynced
	}
	return false
}

// normalizeGameParts: an empty selection means the two per-account parts, deliberately not the
// machine-wide folder, so a caller that forgot to choose can never overwrite it.
func normalizeGameParts(appID string, parts []string) ([]string, error) {
	if len(parts) == 0 {
		return []string{DotaPartLocal, DotaPartRemote}, nil
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		switch p {
		case DotaPartLocal, DotaPartRemote:
		case DotaPartGlobalCfg:
			if gameProfiles[appID].globalCfg == nil {
				continue
			}
		default:
			continue
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil, errDotaNoParts
	}
	// Keep a stable order so logs and snapshot metadata read consistently.
	rank := map[string]int{DotaPartLocal: 0, DotaPartRemote: 1, DotaPartGlobalCfg: 2}
	sort.Slice(out, func(i, j int) bool { return rank[out[i]] < rank[out[j]] })
	return out, nil
}

func containsPart(parts []string, want string) bool {
	for _, p := range parts {
		if p == want {
			return true
		}
	}
	return false
}

// gameCloudGuard blocks writes to cloud-synced parts while Steam is running, because Steam Cloud
// would overwrite them on next launch.
//
// allowCloudRisk is the user having been told exactly that and having said yes anyway. It is a
// parameter rather than a setting because the answer belongs to one copy, not to the app. The
// default is refusal, so a caller that forgets this argument gets the safe behaviour.
func gameCloudGuard(appID string, parts []string, allowCloudRisk bool) error {
	cloud := false
	for _, p := range parts {
		cloud = cloud || partCloudSynced(appID, p)
	}
	if !cloud {
		return nil
	}
	if allowCloudRisk {
		// When a copy does get reverted by Cloud, this line is the difference between "the app
		// is broken" and "the warning was accepted at 14:02".
		actionlog.Record("gameconfig.cloudRiskAccepted", appID, strings.Join(parts, ","), nil)
		return nil
	}
	// "Cannot tell" must not read as "not running": nothing else stops a cloud-synced write from
	// being silently reverted.
	if err := requireProcessInspection(); err != nil {
		return err
	}
	if steamClientRunning() {
		return ErrDotaSteamRunning
	}
	return nil
}

// gameIsRunning asks every way there is of knowing. A game with no listed process name is caught
// by Steam's own Running flag, or by a process started from its install folder. A flag left set
// by a killed client must not block forever, so it only counts while the client is up.
func gameIsRunning(steamRoot, appID string) (bool, error) {
	if err := requireProcessInspection(); err != nil {
		return false, err
	}
	for _, name := range gameProfiles[appID].processNames {
		if runtime.GOOS != "windows" {
			name = strings.TrimSuffix(name, ".exe")
		}
		if backend.IsRunning(name) {
			return true, nil
		}
	}
	if steamClientRunning() && backend.AppMarkedRunning(steamRoot, appID) {
		return true, nil
	}
	for _, dir := range gameInstallDirs(steamRoot, appID) {
		if backend.ProcessRunningFrom(dir) {
			return true, nil
		}
	}
	return false, nil
}

func gameInstallDirs(steamRoot, appID string) []string {
	libs, err := steamAppsDirs(steamRoot)
	if err != nil {
		return nil
	}
	var out []string
	for _, lib := range libs {
		data, err := os.ReadFile(filepath.Join(lib, "appmanifest_"+appID+".acf"))
		if err != nil {
			continue
		}
		if dir := strings.TrimSpace(extractACFValue(string(data), "installdir")); dir != "" {
			out = append(out, filepath.Join(lib, "common", dir))
		}
	}
	return out
}

func gameWriteGuard(steamRoot, appID string) error {
	running, err := gameIsRunning(steamRoot, appID)
	if err != nil {
		return err
	}
	if running {
		return errGameRunning
	}
	return nil
}

// requireKnownAccounts refuses an ID that is well-formed but not an account on this machine:
// writing there would create a userdata folder for nobody. An unreadable loginusers.vdf gives no
// opinion, since the format checks have already passed.
func requireKnownAccounts(steamRoot string, ids ...string) error {
	users, err := ParseLoginUsers(LoginUsersPath(steamRoot))
	if err != nil {
		return nil
	}
	known := make(map[string]bool, len(users))
	for _, u := range users {
		known[strings.TrimSpace(u.SteamID64)] = true
	}
	for _, id := range ids {
		if !known[strings.TrimSpace(id)] {
			return errSteamDataInvalidID
		}
	}
	return nil
}

func dirHasContent(p string) bool {
	ent, err := os.ReadDir(p)
	return err == nil && len(ent) > 0
}

func dirStats(root string) (files int, size int64) {
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		files++
		if info, e := d.Info(); e == nil {
			size += info.Size()
		}
		return nil
	})
	return files, size
}

func dirSizeBytes(root string) int64 {
	_, size := dirStats(root)
	return size
}

// replaceDir removes dst and copies src over it, leaving dst alone if src has nothing.
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

// Snapshots. Dota's stay where they have always been so existing libraries keep working.

func gameSnapshotsRoot(appID string) (string, error) {
	r, err := paths.DataRoot()
	if err != nil {
		return "", err
	}
	if appID == DotaAppID {
		return filepath.Join(r, "Backups", "Dota"), nil
	}
	return filepath.Join(r, "Backups", "Games", appID), nil
}

func gameSnapshotDir(appID, id string) (string, error) {
	root, err := gameSnapshotsRoot(appID)
	if err != nil {
		return "", err
	}
	id = strings.TrimSpace(id)
	if id == "" || strings.ContainsAny(id, `/\.`) {
		return "", errDotaNoSnapshot
	}
	return filepath.Join(root, id), nil
}

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

// maxAutoRevertSnapshots caps automatic revert points per game. Each is a full copy of a config
// tree, so an uncapped library would grow without bound and bury curated configs, which are never
// pruned.
const maxAutoRevertSnapshots = 10

// pruneAutoSnapshots is best-effort: pruning must never fail a copy.
func pruneAutoSnapshots(appID string) {
	root, err := gameSnapshotsRoot(appID)
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
		if dir, err := gameSnapshotDir(appID, snap.ID); err == nil {
			_ = os.RemoveAll(dir)
		}
	}
}

// captureGameSnapshot copies the requested parts of an account's live config into a new snapshot.
//
// A curated snapshot of nothing is refused. An automatic revert point of nothing is kept: "this
// account had no config" is exactly what an undo needs to know.
func (s *SteamService) captureGameSnapshot(appID, steamRoot, id32, steamID64, accountName, label, note string, parts []string, auto bool) (DotaSnapshot, error) {
	id := uuid.NewString()
	dir, err := gameSnapshotDir(appID, id)
	if err != nil {
		return DotaSnapshot{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return DotaSnapshot{}, err
	}

	captured := []string{}
	var empty []string
	for _, part := range parts {
		src, ok := gamePartPath(steamRoot, id32, appID, part)
		if !ok || !dirHasContent(src) {
			empty = append(empty, part)
			continue
		}
		if err := fsutil.CopyDir(src, filepath.Join(dir, part)); err != nil {
			_ = os.RemoveAll(dir)
			return DotaSnapshot{}, err
		}
		captured = append(captured, part)
	}
	if len(captured) == 0 && !auto {
		_ = os.RemoveAll(dir)
		return DotaSnapshot{}, errDotaNothingCopied
	}

	snap := DotaSnapshot{
		ID:                id,
		AppID:             appID,
		Label:             label,
		Note:              note,
		SourceSteamID64:   steamID64,
		SourceAccountName: accountName,
		Parts:             captured,
		CreatedAt:         time.Now().UTC().Format(time.RFC3339Nano),
		Auto:              auto,
		SizeBytes:         dirSizeBytes(dir),
	}
	if auto {
		snap.EmptyParts = empty
	}
	if err := writeSnapshotMeta(dir, snap); err != nil {
		_ = os.RemoveAll(dir)
		return DotaSnapshot{}, err
	}
	return snap, nil
}

// SnapshotGameConfig saves an account's config for this game under a label.
func (s *SteamService) SnapshotGameConfig(appID, steamID64, label, note string, parts []string) (snap DotaSnapshot, err error) {
	dotaWriteMu.Lock()
	defer dotaWriteMu.Unlock()
	defer func() { actionlog.Record("gameconfig.snapshot", appID+":"+tailSteamID(steamID64), "", err) }()
	if err := security.RequireUnlocked(); err != nil {
		return DotaSnapshot{}, err
	}
	if appID, err = normalizeConfigAppID(appID); err != nil {
		return DotaSnapshot{}, err
	}
	label = strings.TrimSpace(label)
	if label == "" {
		return DotaSnapshot{}, errDotaInvalidLabel
	}
	sel, err := normalizeGameParts(appID, parts)
	if err != nil {
		return DotaSnapshot{}, err
	}
	root, err := s.configSteamRoot()
	if err != nil {
		return DotaSnapshot{}, err
	}
	id32, err := s.dotaAccountID32(steamID64)
	if err != nil {
		return DotaSnapshot{}, err
	}
	return s.captureGameSnapshot(appID, root, id32, steamID64, s.dotaAccountDisplayName(steamID64), label, strings.TrimSpace(note), sel, false)
}

// steamInstallRootOverride lets tests use a temp Steam root without real app settings on disk.
var steamInstallRootOverride string

func (s *SteamService) configSteamRoot() (string, error) {
	if steamInstallRootOverride != "" {
		return steamInstallRootOverride, nil
	}
	root, err := s.steamInstallRoot()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(root) == "" {
		return "", errSteamDataInvalidID
	}
	return root, nil
}

// ListGameConfigSnapshots returns this game's snapshot library, newest first.
func (s *SteamService) ListGameConfigSnapshots(appID string) ([]DotaSnapshot, error) {
	if err := security.RequireUnlocked(); err != nil {
		return nil, err
	}
	appID, err := normalizeConfigAppID(appID)
	if err != nil {
		return nil, err
	}
	root, err := gameSnapshotsRoot(appID)
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

// RenameGameConfigSnapshot updates a label and note. A snapshot the user has deliberately named is
// a curated config, not a revert point, so it stops being pruned.
func (s *SteamService) RenameGameConfigSnapshot(appID, snapshotID, label, note string) (DotaSnapshot, error) {
	if err := security.RequireUnlocked(); err != nil {
		return DotaSnapshot{}, err
	}
	appID, err := normalizeConfigAppID(appID)
	if err != nil {
		return DotaSnapshot{}, err
	}
	label = strings.TrimSpace(label)
	if label == "" {
		return DotaSnapshot{}, errDotaInvalidLabel
	}
	dir, err := gameSnapshotDir(appID, snapshotID)
	if err != nil {
		return DotaSnapshot{}, err
	}
	snap, err := readSnapshotMeta(dir)
	if err != nil {
		return DotaSnapshot{}, errDotaNoSnapshot
	}
	snap.Label = label
	snap.Note = strings.TrimSpace(note)
	snap.Auto = false
	if err := writeSnapshotMeta(dir, snap); err != nil {
		return DotaSnapshot{}, err
	}
	return snap, nil
}

// DeleteGameConfigSnapshot removes a snapshot from the library.
func (s *SteamService) DeleteGameConfigSnapshot(appID, snapshotID string) (err error) {
	dotaWriteMu.Lock()
	defer dotaWriteMu.Unlock()
	defer func() { actionlog.Record("gameconfig.deleteSnapshot", appID, "", err) }()
	if err := security.RequireUnlocked(); err != nil {
		return err
	}
	if appID, err = normalizeConfigAppID(appID); err != nil {
		return err
	}
	dir, err := gameSnapshotDir(appID, snapshotID)
	if err != nil {
		return err
	}
	if _, err := readSnapshotMeta(dir); err != nil {
		return errDotaNoSnapshot
	}
	return os.RemoveAll(dir)
}

// OpenGameConfigSnapshotFolder reveals a snapshot on disk.
func (s *SteamService) OpenGameConfigSnapshotFolder(appID, snapshotID string) error {
	if err := security.RequireUnlocked(); err != nil {
		return err
	}
	appID, err := normalizeConfigAppID(appID)
	if err != nil {
		return err
	}
	dir, err := gameSnapshotDir(appID, snapshotID)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err != nil {
		return errDotaNoSnapshot
	}
	return platform.OpenPathInFileManager(dir)
}

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

// applyGameParts writes each part from srcFor into the destination, after taking a revert point
// that records what it held. removeParts are parts to delete instead, which is how an undo takes
// away what a copy created. If a write fails, the destination is put back straight away: the UI
// never gets a half-copied config to sort out.
func (s *SteamService) applyGameParts(
	appID, steamRoot, destID64 string,
	parts, removeParts []string,
	srcFor func(part string) (string, bool),
) (DotaCopyResult, error) {
	var res DotaCopyResult
	destID32, err := s.dotaAccountID32(destID64)
	if err != nil {
		return res, err
	}

	var writes []string
	sources := map[string]string{}
	for _, part := range parts {
		src, ok := srcFor(part)
		if ok && dirHasContent(src) {
			if _, live := gamePartPath(steamRoot, destID32, appID, part); live {
				writes = append(writes, part)
				sources[part] = src
				continue
			}
		}
		res.SkippedParts = append(res.SkippedParts, part)
	}
	if len(writes) == 0 && len(removeParts) == 0 {
		// Before the revert point, so a copy from an empty account leaves no clutter behind.
		return res, errDotaNothingCopied
	}

	destName := s.dotaAccountDisplayName(destID64)
	revert, err := s.captureGameSnapshot(appID, steamRoot, destID32, destID64, destName,
		autoRevertLabel(destName, destID64), "Automatic revert point", append(append([]string(nil), writes...), removeParts...), true)
	if err != nil {
		return res, err
	}
	res.RevertSnapshotID = revert.ID
	pruneAutoSnapshots(appID)

	var touched []string
	for _, part := range writes {
		dst, _ := gamePartPath(steamRoot, destID32, appID, part)
		touched = append(touched, part)
		if err := replaceDir(sources[part], dst); err != nil {
			return res, s.rollBackGameParts(&res, appID, steamRoot, destID32, revert, touched, err)
		}
		if partCloudSynced(appID, part) {
			// remotecache.vdf is Steam's local inventory of the cloud payload (paths, sizes,
			// hashes). Leaving the pre-copy version in place invites Steam to treat the new files
			// as stale and re-download the old ones.
			_ = os.Remove(filepath.Join(steamRoot, "userdata", destID32, appID, "remotecache.vdf"))
			res.CloudFollowUp = true
		}
		res.CopiedParts = append(res.CopiedParts, part)
	}
	for _, part := range removeParts {
		dst, ok := gamePartPath(steamRoot, destID32, appID, part)
		if !ok {
			continue
		}
		touched = append(touched, part)
		if err := os.RemoveAll(dst); err != nil {
			return res, s.rollBackGameParts(&res, appID, steamRoot, destID32, revert, touched, err)
		}
		if partCloudSynced(appID, part) {
			_ = os.Remove(filepath.Join(steamRoot, "userdata", destID32, appID, "remotecache.vdf"))
			res.CloudFollowUp = true
		}
		res.RemovedParts = append(res.RemovedParts, part)
	}
	return res, nil
}

func (s *SteamService) rollBackGameParts(res *DotaCopyResult, appID, steamRoot, destID32 string, revert DotaSnapshot, touched []string, cause error) error {
	res.Failed = true
	dir, err := gameSnapshotDir(appID, revert.ID)
	if err != nil {
		return fmt.Errorf("%w\n%v", errGameConfigNotRolledBck, cause)
	}
	for _, part := range touched {
		dst, ok := gamePartPath(steamRoot, destID32, appID, part)
		if !ok {
			continue
		}
		var rerr error
		if containsPart(revert.Parts, part) {
			rerr = replaceDir(filepath.Join(dir, part), dst)
		} else {
			rerr = os.RemoveAll(dst)
		}
		if rerr != nil {
			return fmt.Errorf("%w\n%v", errGameConfigNotRolledBck, cause)
		}
	}
	return fmt.Errorf("%w\n%v", errGameConfigRolledBack, cause)
}

// CopyGameConfigBetween copies one game's config from one account to another. Neither has to be
// the account signed in to Steam, and reversing the direction is just swapping the arguments.
func (s *SteamService) CopyGameConfigBetween(appID, sourceSteamID64, destSteamID64 string, parts []string, allowCloudRisk bool) (res DotaCopyResult, err error) {
	// Before dotaWriteMu, never after. The guard reads the engine's state, which takes Engine.mu;
	// taking dotaWriteMu first would invert the documented lock order and deadlock against a
	// transaction that already holds Engine.mu and is waiting for dotaWriteMu.
	if err := guardManualConfigWrite(); err != nil {
		return res, err
	}
	dotaWriteMu.Lock()
	defer dotaWriteMu.Unlock()
	sourceSteamID64 = strings.TrimSpace(sourceSteamID64)
	destSteamID64 = strings.TrimSpace(destSteamID64)
	defer func() {
		actionlog.Record("gameconfig.copy", appID+":"+tailSteamID(sourceSteamID64)+">"+tailSteamID(destSteamID64),
			strings.Join(res.CopiedParts, ","), err)
	}()
	if err := security.RequireUnlocked(); err != nil {
		return res, err
	}
	if appID, err = normalizeConfigAppID(appID); err != nil {
		return res, err
	}
	sel, err := normalizeGameParts(appID, parts)
	if err != nil {
		return res, err
	}
	srcID32, err := s.dotaAccountID32(sourceSteamID64)
	if err != nil {
		return res, err
	}
	dstID32, err := s.dotaAccountID32(destSteamID64)
	if err != nil {
		return res, err
	}
	// By account number, not by string: the parser accepts more than one spelling of an ID.
	if srcID32 == dstID32 {
		return res, errDotaSameAccount
	}
	root, err := s.configSteamRoot()
	if err != nil {
		return res, err
	}
	if err := requireKnownAccounts(root, sourceSteamID64, destSteamID64); err != nil {
		return res, err
	}
	// The machine-wide folder is the same for both accounts, so copying it would change nothing.
	var copyParts, skipped []string
	for _, p := range sel {
		if p == DotaPartGlobalCfg {
			skipped = append(skipped, p)
			continue
		}
		copyParts = append(copyParts, p)
	}
	if len(copyParts) == 0 {
		return DotaCopyResult{SkippedParts: skipped}, errDotaNothingCopied
	}
	if err := gameWriteGuard(root, appID); err != nil {
		return res, err
	}
	if err := gameCloudGuard(appID, copyParts, allowCloudRisk); err != nil {
		return res, err
	}
	res, err = s.applyGameParts(appID, root, destSteamID64, copyParts, nil, func(part string) (string, bool) {
		return gamePartPath(root, srcID32, appID, part)
	})
	res.SkippedParts = append(res.SkippedParts, skipped...)
	return res, err
}

// ApplyGameConfigSnapshot writes a snapshot onto an account, taking a revert point first.
func (s *SteamService) ApplyGameConfigSnapshot(appID, snapshotID, destSteamID64 string, parts []string, allowCloudRisk bool) (res DotaCopyResult, err error) {
	// See CopyGameConfigBetween: the engine guard must run before dotaWriteMu.
	if err := guardManualConfigWrite(); err != nil {
		return res, err
	}
	dotaWriteMu.Lock()
	defer dotaWriteMu.Unlock()
	defer func() {
		actionlog.Record("gameconfig.apply", appID+":"+tailSteamID(destSteamID64), strings.Join(res.CopiedParts, ","), err)
	}()
	if err := security.RequireUnlocked(); err != nil {
		return res, err
	}
	if appID, err = normalizeConfigAppID(appID); err != nil {
		return res, err
	}
	dir, err := gameSnapshotDir(appID, snapshotID)
	if err != nil {
		return res, err
	}
	snap, err := readSnapshotMeta(dir)
	if err != nil {
		return res, errDotaNoSnapshot
	}
	sel, err := normalizeGameParts(appID, parts)
	if err != nil {
		return res, err
	}
	var usable, removes []string
	for _, p := range sel {
		if containsPart(snap.Parts, p) {
			usable = append(usable, p)
		} else if containsPart(snap.EmptyParts, p) {
			removes = append(removes, p)
		}
	}
	return s.writeSnapshot(appID, dir, destSteamID64, usable, removes, allowCloudRisk)
}

// UndoGameConfigCopy puts an account back from the revert point a copy left, including removing
// what the copy created. The account comes from the revert point, so the undo cannot land on
// whichever account happens to be selected now.
func (s *SteamService) UndoGameConfigCopy(appID, revertSnapshotID string, allowCloudRisk bool) (res DotaCopyResult, err error) {
	if err := guardManualConfigWrite(); err != nil {
		return res, err
	}
	dotaWriteMu.Lock()
	defer dotaWriteMu.Unlock()
	defer func() { actionlog.Record("gameconfig.undo", appID, strings.Join(res.CopiedParts, ","), err) }()
	if err := security.RequireUnlocked(); err != nil {
		return res, err
	}
	if appID, err = normalizeConfigAppID(appID); err != nil {
		return res, err
	}
	dir, err := gameSnapshotDir(appID, revertSnapshotID)
	if err != nil {
		return res, err
	}
	snap, err := readSnapshotMeta(dir)
	if err != nil || !snap.Auto {
		return res, errNotARevertPoint
	}
	return s.writeSnapshot(appID, dir, snap.SourceSteamID64, snap.Parts, snap.EmptyParts, allowCloudRisk)
}

func (s *SteamService) writeSnapshot(appID, dir, destSteamID64 string, usable, removes []string, allowCloudRisk bool) (DotaCopyResult, error) {
	var res DotaCopyResult
	if len(usable) == 0 && len(removes) == 0 {
		return res, errDotaNothingCopied
	}
	if _, err := s.dotaAccountID32(destSteamID64); err != nil {
		return res, err
	}
	root, err := s.configSteamRoot()
	if err != nil {
		return res, err
	}
	if err := requireKnownAccounts(root, destSteamID64); err != nil {
		return res, err
	}
	if err := gameWriteGuard(root, appID); err != nil {
		return res, err
	}
	if err := gameCloudGuard(appID, append(append([]string(nil), usable...), removes...), allowCloudRisk); err != nil {
		return res, err
	}
	return s.applyGameParts(appID, root, destSteamID64, usable, removes, func(part string) (string, bool) {
		return filepath.Join(dir, part), true
	})
}

// ConfigGame is one entry in the game picker.
type ConfigGame struct {
	AppID string `json:"appId"`
	Name  string `json:"name"`
	// Accounts counts how many of the given accounts have config for it on this machine.
	Accounts int `json:"accounts"`
}

// ListConfigGames lists the games at least one of these accounts has per-account config for,
// Dota and CS2 first, then by name.
func (s *SteamService) ListConfigGames(steamIDs []string) ([]ConfigGame, error) {
	if err := security.RequireUnlocked(); err != nil {
		return nil, err
	}
	root, err := s.configSteamRoot()
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, id := range steamIDs {
		id32, err := s.dotaAccountID32(id)
		if err != nil {
			continue
		}
		for _, appID := range listNumericSubdirNames(filepath.Join(root, "userdata", id32)) {
			if _, err := normalizeConfigAppID(appID); err != nil {
				continue
			}
			for _, part := range []string{DotaPartLocal, DotaPartRemote} {
				if p, ok := gamePartPath(root, id32, appID, part); ok && dirHasContent(p) {
					counts[appID]++
					break
				}
			}
		}
	}
	names, _ := getSteamAppNameMapCached()
	out := make([]ConfigGame, 0, len(counts))
	for appID, n := range counts {
		out = append(out, ConfigGame{AppID: appID, Name: configGameName(root, appID, names), Accounts: n})
	}
	rank := func(appID string) int {
		switch appID {
		case DotaAppID:
			return 0
		case CS2AppID:
			return 1
		}
		return 2
	}
	sort.Slice(out, func(i, j int) bool {
		if ri, rj := rank(out[i].AppID), rank(out[j].AppID); ri != rj {
			return ri < rj
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func configGameName(steamRoot, appID string, names map[string]string) string {
	if p, ok := gameProfiles[appID]; ok {
		return p.name
	}
	if libs, err := steamAppsDirs(steamRoot); err == nil {
		for _, lib := range libs {
			if data, err := os.ReadFile(filepath.Join(lib, "appmanifest_"+appID+".acf")); err == nil {
				if n := strings.TrimSpace(extractACFValue(string(data), "name")); n != "" {
					return n
				}
			}
		}
	}
	if n := strings.TrimSpace(names[appID]); n != "" {
		return n
	}
	return "App " + appID
}

// GameConfigPartStat is what one account holds of one part.
type GameConfigPartStat struct {
	ID        string `json:"id"`
	Files     int    `json:"files"`
	SizeBytes int64  `json:"sizeBytes"`
}

// GameConfigAccount is one account's config for a game on disk.
type GameConfigAccount struct {
	SteamID64 string               `json:"steamId64"`
	Parts     []GameConfigPartStat `json:"parts"`
	// Other names what else is in the game's userdata folder. It is never copied, and the UI
	// says so rather than letting someone assume a copy took everything.
	Other []string `json:"other"`
}

// GameConfigPart describes one selectable part for this game.
type GameConfigPart struct {
	ID          string `json:"id"`
	CloudSynced bool   `json:"cloudSynced"`
	// MachineWide parts are shared by all accounts: snapshots only, never account to account.
	MachineWide bool `json:"machineWide"`
}

// GameConfigInfo is everything the page shows about one game.
type GameConfigInfo struct {
	AppID        string              `json:"appId"`
	Name         string              `json:"name"`
	Parts        []GameConfigPart    `json:"parts"`
	Accounts     []GameConfigAccount `json:"accounts"`
	GameRunning  bool                `json:"gameRunning"`
	SteamRunning bool                `json:"steamRunning"`
}

// GetGameConfigInfo reports, from disk, what each account holds for a game.
func (s *SteamService) GetGameConfigInfo(appID string, steamIDs []string) (GameConfigInfo, error) {
	if err := security.RequireUnlocked(); err != nil {
		return GameConfigInfo{}, err
	}
	appID, err := normalizeConfigAppID(appID)
	if err != nil {
		return GameConfigInfo{}, err
	}
	root, err := s.configSteamRoot()
	if err != nil {
		return GameConfigInfo{}, err
	}
	names, _ := getSteamAppNameMapCached()
	info := GameConfigInfo{
		AppID:    appID,
		Name:     configGameName(root, appID, names),
		Accounts: make([]GameConfigAccount, 0, len(steamIDs)),
		Parts: []GameConfigPart{
			{ID: DotaPartLocal, CloudSynced: partCloudSynced(appID, DotaPartLocal)},
			{ID: DotaPartRemote, CloudSynced: partCloudSynced(appID, DotaPartRemote)},
		},
		SteamRunning: steamClientRunning(),
	}
	if resolve := gameProfiles[appID].globalCfg; resolve != nil && resolve(root) != "" {
		info.Parts = append(info.Parts, GameConfigPart{ID: DotaPartGlobalCfg, MachineWide: true})
	}
	// "Cannot tell" reads as running here too: the page must not offer what the write would refuse.
	running, err := gameIsRunning(root, appID)
	info.GameRunning = running || err != nil

	for _, id := range steamIDs {
		acc := GameConfigAccount{SteamID64: strings.TrimSpace(id), Parts: []GameConfigPartStat{}, Other: []string{}}
		if id32, err := s.dotaAccountID32(id); err == nil {
			for _, part := range []string{DotaPartLocal, DotaPartRemote} {
				if p, ok := gamePartPath(root, id32, appID, part); ok && dirHasContent(p) {
					files, size := dirStats(p)
					acc.Parts = append(acc.Parts, GameConfigPartStat{ID: part, Files: files, SizeBytes: size})
				}
			}
			if ents, err := os.ReadDir(filepath.Join(root, "userdata", id32, appID)); err == nil {
				for _, e := range ents {
					switch e.Name() {
					case "local", "remote", "remotecache.vdf":
					default:
						acc.Other = append(acc.Other, e.Name())
					}
				}
			}
		}
		info.Accounts = append(info.Accounts, acc)
	}
	return info, nil
}
