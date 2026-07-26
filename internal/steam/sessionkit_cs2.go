package steam

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"steamswitch/internal/sessionkit"
)

// The CS2 session-kit module (REDESIGN.md §5) — present, detecting, and deliberately not
// writing anything.
//
// Its job right now is to prove the Module interface generalises past the game it was written
// for, and to give the "Game modules" cards something real to show. Detection is genuine:
// it finds the install, resolves both per-account parts and computes a fingerprint exactly as
// Dota does. Everything that mutates refuses.
//
// The refusal is the honest state, not a placeholder. CS2's config layout has not been checked
// against a real install the way Dota's has, and the one thing this package must never do is
// write a half-understood layout over somebody's settings. A module that silently no-ops would
// be worse: the engine would report a kit applied that never was.
//
// Turning it on is a small change — drop `cs2Enabled` to true and implement the four write
// methods against `sessionkit.ReplacePart` the way `sessionkit_dota.go` does — but it needs a
// pass through TESTING.md §C on a machine with CS2 installed first.

// CS2ModuleID is the stable journal/disk identifier. Never localise it, and never change it.
const CS2ModuleID = "cs2"

// CS2AppID is Counter-Strike 2's Steam application ID.
const CS2AppID = "730"

// cs2Enabled gates every write. See the type comment.
const cs2Enabled = false

// cs2GlobalCfgRelPath is the machine-wide cfg folder relative to a steamapps dir. CS2 kept
// the Global Offensive directory name through the rename.
var cs2GlobalCfgRelPath = filepath.Join("common", "Counter-Strike Global Offensive", "game", "csgo", "cfg")

// cs2ProcessNames are the game binaries that hold `userdata/730` open.
var cs2ProcessNames = []string{"cs2.exe"}

// CS2Module implements sessionkit.Module for Counter-Strike 2.
type CS2Module struct{}

var (
	_ sessionkit.Module           = CS2Module{}
	_ sessionkit.LivePathResolver = CS2Module{}
	_ sessionkit.ScratchAnchor    = CS2Module{}
)

func (CS2Module) ID() string          { return CS2ModuleID }
func (CS2Module) DisplayName() string { return "Counter-Strike 2" }

// ErrModuleNotEnabled is returned by every mutating method of a module that detects but does
// not yet write. The message is an i18n key, following the convention in dota.go.
var ErrModuleNotEnabled = fmt.Errorf("Toast_Kit_ModuleNotEnabled")

// cs2Parts mirrors Dota's split. `local` is per-machine; `remote` is Steam Cloud synced and
// holds the config CS2 players actually care about — crosshairs, binds, video settings — so
// it carries the same "a successful write is not a durable one" caveat.
func cs2Parts() []sessionkit.Part {
	return []sessionkit.Part{
		{
			ID:          DotaPartLocal,
			Label:       "Local settings",
			Risk:        sessionkit.PartLocalSafe,
			KitEligible: true,
		},
		{
			ID:          DotaPartRemote,
			Label:       "Cloud settings (crosshair, binds)",
			Risk:        sessionkit.PartCloudRisk,
			KitEligible: true,
		},
	}
}

// resolveCS2GlobalCfg finds the install's cfg folder across every Steam library.
func resolveCS2GlobalCfg(steamRoot string) string {
	dirs, err := steamAppsDirs(steamRoot)
	if err != nil {
		return ""
	}
	for _, d := range dirs {
		p := filepath.Join(d, cs2GlobalCfgRelPath)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
	}
	return ""
}

// Detect reports the install honestly and then reports that the module will not run.
//
// Installed and Ready are separate for exactly this case. Saying "not installed" when the game
// is sitting there would be a lie the user can see through, and it would make the module card
// useless for telling them why nothing happens.
func (m CS2Module) Detect(_ context.Context, req sessionkit.DetectRequest) (sessionkit.Detection, error) {
	det := sessionkit.Detection{
		Parts:     cs2Parts(),
		CheckedAt: time.Now().UTC(),
	}
	root := strings.TrimSpace(req.SteamRoot)
	if root == "" {
		det.Reason = "Toast_Kit_NoSteamRoot"
		return det, nil
	}

	cfg := resolveCS2GlobalCfg(root)
	det.Installed = cfg != ""
	if !det.Installed {
		det.Reason = "Toast_Kit_CS2NotInstalled"
		return det, nil
	}

	det.Fingerprint = cs2Fingerprint(root, cfg)
	if !cs2Enabled {
		det.Reason = "Toast_Kit_ModuleNotEnabled"
		return det, nil
	}
	det.Ready = true
	return det, nil
}

// cs2Fingerprint follows dotaFingerprint: install location plus the app manifest's build id,
// so a patch that moves the config folders invalidates it. Not a hash of the config contents,
// which change whenever the user edits a setting.
func cs2Fingerprint(steamRoot, globalCfg string) string {
	h := sha256.New()
	fmt.Fprintf(h, "cfg\x00%s\x00", filepath.ToSlash(globalCfg))
	if dirs, err := steamAppsDirs(steamRoot); err == nil {
		for _, d := range dirs {
			data, err := os.ReadFile(filepath.Join(d, "appmanifest_"+CS2AppID+".acf"))
			if err != nil {
				continue
			}
			fmt.Fprintf(h, "buildid\x00%s\x00", extractACFValue(string(data), "buildid"))
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// cs2AccountPartPath resolves one part's live directory.
func cs2AccountPartPath(steamRoot, id32, partID string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(partID)) {
	case DotaPartLocal:
		return filepath.Join(steamRoot, "userdata", id32, CS2AppID, "local"), true
	case DotaPartRemote:
		return filepath.Join(steamRoot, "userdata", id32, CS2AppID, "remote"), true
	}
	return "", false
}

func (CS2Module) LivePath(steamRoot string, account sessionkit.AccountRef, partID string) (string, bool) {
	id32, err := dotaID32(account)
	if err != nil {
		return "", false
	}
	return cs2AccountPartPath(steamRoot, id32, partID)
}

// ScratchAnchor keeps the engine's staging and rollback directories on the same volume as the
// files being replaced — `userdata` is frequently on a different drive from the data root, and
// ReplacePart installs by renaming, which cannot cross a filesystem boundary.
func (CS2Module) ScratchAnchor(steamRoot string, account sessionkit.AccountRef) (string, bool) {
	id32, err := dotaID32(account)
	if err != nil || strings.TrimSpace(steamRoot) == "" {
		return "", false
	}
	return filepath.Join(steamRoot, "userdata", id32, CS2AppID), true
}

// Preflight refuses rather than returning an empty plan.
//
// An empty plan is how the engine expresses "this module has nothing to carry", and it would
// quietly degrade the switch to a plain one. The user asked for a kit; if it cannot be
// delivered they need to be told, not left to discover it from settings that did not move.
func (m CS2Module) Preflight(ctx context.Context, req sessionkit.PreflightRequest) (sessionkit.ModulePlan, error) {
	plan := sessionkit.ModulePlan{ModuleID: m.ID(), Source: req.Source, Target: req.Target}
	det, err := m.Detect(ctx, sessionkit.DetectRequest{SteamRoot: req.SteamRoot})
	if err != nil {
		return plan, err
	}
	plan.DetectionFingerprint = det.Fingerprint
	if !det.Ready {
		return plan, ErrModuleNotEnabled
	}
	return plan, ErrModuleNotEnabled
}

func (CS2Module) Snapshot(context.Context, sessionkit.SnapshotRequest) (sessionkit.SnapshotResult, error) {
	return sessionkit.SnapshotResult{}, ErrModuleNotEnabled
}

func (CS2Module) Apply(context.Context, sessionkit.ApplyRequest) (sessionkit.ApplyResult, error) {
	return sessionkit.ApplyResult{}, ErrModuleNotEnabled
}

func (CS2Module) Restore(context.Context, sessionkit.RestoreRequest) (sessionkit.RestoreResult, error) {
	return sessionkit.RestoreResult{}, ErrModuleNotEnabled
}

// Verify is read-only, so it is the one method that could safely run — but reporting a match
// against a manifest this module never wrote would be a claim it has no basis for.
func (CS2Module) Verify(context.Context, sessionkit.VerifyRequest) (sessionkit.VerifyResult, error) {
	return sessionkit.VerifyResult{}, ErrModuleNotEnabled
}
