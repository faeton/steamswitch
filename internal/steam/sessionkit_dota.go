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

	"steamswitch/internal/fsutil"
	"steamswitch/internal/sessionkit"
)

// The Dota 2 session-kit module (REDESIGN.md §5).
//
// It is a thin adapter over the path knowledge already in `dota.go` — `dotaAccountPartPath`,
// `resolveDotaGlobalCfg`, the part identifiers — rather than a second implementation of it.
// What it deliberately does *not* reuse is `replaceDir` (dota.go:240), which does
// `os.RemoveAll(dst)` followed by a copy: that leaves a window where the destination is gone
// and nothing on disk records what it held, which is the exact failure the journal exists to
// survive. Writes here go through `sessionkit.ReplacePart` instead.
//
// `dotaProcessNames` is consumed by the Lifecycle adapter, not here: the engine enforces
// process closure centrally, because the inherited `dotaSteamRunningGuard` (dota.go:202)
// only checks `steam.exe`, and only when the cloud-synced part is selected.
var dotaProcessNames = []string{"dota2.exe"}

// DotaModule implements sessionkit.Module for Dota 2.
//
// Stateless: every method re-resolves paths from the `steamRoot` it is handed, so a Steam
// library change between two steps of a transaction cannot leave it writing to a stale tree.
type DotaModule struct{}

var (
	_ sessionkit.Module           = DotaModule{}
	_ sessionkit.LivePathResolver = DotaModule{}
	_ sessionkit.ScratchAnchor    = DotaModule{}
)

// DotaModuleID is the stable journal/disk identifier. Never localise it, and never change it
// — journals and snapshot directories on users' machines are keyed by this string.
const DotaModuleID = "dota2"

func (DotaModule) ID() string          { return DotaModuleID }
func (DotaModule) DisplayName() string { return "Dota 2" }

// dotaID32 converts an account reference to the userdata folder name.
func dotaID32(account sessionkit.AccountRef) (string, error) {
	f, err := FormatsFromID64(strings.TrimSpace(account.SteamID64))
	if err != nil {
		return "", errSteamDataInvalidID
	}
	return f.ID32, nil
}

// kitParts are the parts that travel with a kit.
//
// `globalcfg` is excluded outright, not merely marked ineligible: it resolves to a single
// machine-wide folder shared by every account, so "copying" it from Home to a shared account
// is at best a no-op and at worst clobbers settings belonging to neither. REDESIGN.md §5
// states it is never part of a kit. The snapshot lab in `dota.go` still handles it, because
// there the user is explicitly choosing to move it.
func kitParts() []sessionkit.Part {
	return []sessionkit.Part{
		{
			ID:          DotaPartLocal,
			Label:       "Local settings",
			Risk:        sessionkit.PartLocalSafe,
			KitEligible: true,
		},
		{
			// Hero grids, builds and item sets live here and are Steam Cloud synced, so a
			// successful write is not a durable one — Cloud can pull the old copy back after
			// the next login. The engine downgrades the post-apply status accordingly.
			ID:          DotaPartRemote,
			Label:       "Cloud settings (hero grids, builds)",
			Risk:        sessionkit.PartCloudRisk,
			KitEligible: true,
		},
	}
}

// Detect reports whether Dota is installed and the module can run.
func (m DotaModule) Detect(ctx context.Context, req sessionkit.DetectRequest) (sessionkit.Detection, error) {
	det := sessionkit.Detection{
		Parts:     kitParts(),
		CheckedAt: time.Now().UTC(),
	}
	root := strings.TrimSpace(req.SteamRoot)
	if root == "" {
		det.Reason = "Toast_Kit_NoSteamRoot"
		return det, nil
	}

	// The global cfg folder is the cheapest reliable proof the game is installed in *some*
	// library: it ships with the game and `resolveDotaGlobalCfg` already walks every library
	// in libraryfolders.vdf, which is where Dota usually is.
	cfg := resolveDotaGlobalCfg(root)
	det.Installed = cfg != ""
	if !det.Installed {
		det.Reason = "Toast_Kit_DotaNotInstalled"
		return det, nil
	}

	det.Fingerprint = dotaFingerprint(root, cfg)
	det.Ready = true
	return det, nil
}

// dotaFingerprint changes when the game's on-disk layout changes, which is what auto-pause
// keys off (REDESIGN.md §5).
//
// It is built from the resolved install location plus the app manifest's recorded build id,
// so a Dota patch that moves or replaces the config folders invalidates it. Deliberately not
// a hash of the config contents: those change every time the user edits a setting, which
// would pause the module constantly.
func dotaFingerprint(steamRoot, globalCfg string) string {
	h := sha256.New()
	fmt.Fprintf(h, "cfg\x00%s\x00", filepath.ToSlash(globalCfg))
	if dirs, err := steamAppsDirs(steamRoot); err == nil {
		for _, d := range dirs {
			manifest := filepath.Join(d, "appmanifest_"+DotaAppID+".acf")
			data, err := os.ReadFile(manifest)
			if err != nil {
				continue
			}
			fmt.Fprintf(h, "buildid\x00%s\x00", extractACFValue(string(data), "buildid"))
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// extractACFValue pulls one top-level `"key" "value"` pair out of an ACF manifest. A tiny
// scanner rather than the full VDF parser: only one scalar is needed and the manifest format
// is stable.
func extractACFValue(acf, key string) string {
	needle := `"` + key + `"`
	idx := strings.Index(acf, needle)
	if idx < 0 {
		return ""
	}
	rest := acf[idx+len(needle):]
	open := strings.Index(rest, `"`)
	if open < 0 {
		return ""
	}
	rest = rest[open+1:]
	close := strings.Index(rest, `"`)
	if close < 0 {
		return ""
	}
	return rest[:close]
}

// Preflight freezes what this transaction will touch, without writing anything.
func (m DotaModule) Preflight(ctx context.Context, req sessionkit.PreflightRequest) (sessionkit.ModulePlan, error) {
	plan := sessionkit.ModulePlan{
		ModuleID: m.ID(),
		Source:   req.Source,
		Target:   req.Target,
	}
	det, err := m.Detect(ctx, sessionkit.DetectRequest{SteamRoot: req.SteamRoot})
	if err != nil {
		return plan, err
	}
	plan.DetectionFingerprint = det.Fingerprint

	if _, err := dotaID32(req.Target); err != nil {
		return plan, err
	}

	wanted := map[string]bool{}
	for _, id := range req.Parts {
		wanted[strings.ToLower(strings.TrimSpace(id))] = true
	}

	for _, part := range kitParts() {
		if len(wanted) > 0 && !wanted[part.ID] {
			continue
		}
		if req.Operation == sessionkit.OperationEnter {
			// Only carry a part the source actually has. An empty Home `remote/` would
			// otherwise blank the other person's hero grids and call it a kit.
			src, ok := dotaAccountPartPath(req.SteamRoot, mustID32(req.Source), part.ID)
			if !ok || !dirHasContent(src) {
				continue
			}
		}
		plan.Parts = append(plan.Parts, part)
	}
	return plan, nil
}

// mustID32 is the non-erroring form for callers that have already validated the ref, or that
// treat an unresolvable id as "no content" rather than as a failure.
func mustID32(account sessionkit.AccountRef) string {
	id, err := dotaID32(account)
	if err != nil {
		return ""
	}
	return id
}

// Snapshot copies an account's planned parts into the engine's destination directory.
//
// A part that does not exist is recorded as absent rather than skipped: restoring "this
// account had no cloud config" has to be able to remove a directory the kit created, and
// that is only possible if absence was captured as a fact.
func (m DotaModule) Snapshot(ctx context.Context, req sessionkit.SnapshotRequest) (sessionkit.SnapshotResult, error) {
	id32, err := dotaID32(req.Account)
	if err != nil {
		return sessionkit.SnapshotResult{}, err
	}
	if err := os.MkdirAll(req.Destination, 0o755); err != nil {
		return sessionkit.SnapshotResult{}, err
	}

	var captured []string
	for _, part := range req.Plan.Parts {
		live, ok := dotaAccountPartPath(req.SteamRoot, id32, part.ID)
		if !ok {
			continue
		}
		dst := filepath.Join(req.Destination, part.ID)
		if dirHasContent(live) {
			if err := fsutil.CopyDir(live, dst); err != nil {
				return sessionkit.SnapshotResult{}, err
			}
		} else if err := os.MkdirAll(dst, 0o755); err != nil {
			return sessionkit.SnapshotResult{}, err
		}
		captured = append(captured, part.ID)
	}

	// Hash the copy rather than the live tree: the manifest has to describe exactly what
	// landed in the snapshot, so that a later restore can verify what it is about to write.
	manifest, err := sessionkit.HashParts(m.ID(), req.Plan.Parts, func(partID string) (string, bool) {
		if !containsPart(captured, partID) {
			return "", false
		}
		return filepath.Join(req.Destination, partID), true
	})
	if err != nil {
		return sessionkit.SnapshotResult{}, err
	}
	return sessionkit.SnapshotResult{CapturedParts: captured, Manifest: manifest}, nil
}

// Apply writes the staged kit onto the target account.
func (m DotaModule) Apply(ctx context.Context, req sessionkit.ApplyRequest) (sessionkit.ApplyResult, error) {
	res, err := m.write(ctx, writeRequest{
		txID:         req.TransactionID,
		plan:         req.Plan,
		account:      req.Plan.Target,
		steamRoot:    req.SteamRoot,
		payload:      req.PayloadPath,
		expected:     req.Expected,
		stageRoot:    req.StageRoot,
		rollbackRoot: req.RollbackRoot,
		journal:      req.Journal,
		// Apply never removes a part: a kit with nothing for `remote/` leaves whatever the
		// other person had, rather than deleting it.
		honourAbsence: false,
	})
	if err != nil {
		return sessionkit.ApplyResult{}, err
	}

	out := sessionkit.ApplyResult{
		AppliedParts: res.parts,
		Manifest:     res.manifest,
		CloudRisk:    req.Plan.HasCloudRisk(),
	}
	if out.CloudRisk {
		// Steam re-reads remotecache.vdf to decide what to upload. Leaving the old one in
		// place makes Steam believe the files it already has are current and re-download
		// them over the kit. Recorded as a side effect rather than a hard failure: the kit
		// is on disk either way, and the status line has to be able to say so honestly.
		out.SideEffects = map[string]bool{
			"remotecache-cleared": clearDotaRemoteCache(req.SteamRoot, mustID32(req.Plan.Target)) == nil,
		}
	}
	return out, nil
}

// Restore puts a captured "their setup" back, honouring absence.
func (m DotaModule) Restore(ctx context.Context, req sessionkit.RestoreRequest) (sessionkit.RestoreResult, error) {
	res, err := m.write(ctx, writeRequest{
		txID:          req.TransactionID,
		plan:          req.Plan,
		account:       req.Plan.Target,
		steamRoot:     req.SteamRoot,
		payload:       req.PayloadPath,
		expected:      req.Expected,
		stageRoot:     req.StageRoot,
		rollbackRoot:  req.RollbackRoot,
		journal:       req.Journal,
		honourAbsence: true,
	})
	if err != nil {
		return sessionkit.RestoreResult{}, err
	}
	if req.Plan.HasCloudRisk() {
		_ = clearDotaRemoteCache(req.SteamRoot, mustID32(req.Plan.Target))
	}
	return sessionkit.RestoreResult{RestoredParts: res.parts, Manifest: res.manifest}, nil
}

type writeRequest struct {
	txID          string
	plan          sessionkit.ModulePlan
	account       sessionkit.AccountRef
	steamRoot     string
	payload       string
	expected      sessionkit.Manifest
	stageRoot     string
	rollbackRoot  string
	journal       sessionkit.PartJournal
	honourAbsence bool
}

type writeResult struct {
	parts    []string
	manifest sessionkit.Manifest
}

// write is the shared body of Apply and Restore: they differ only in whether an absent
// source means "leave it alone" or "remove it".
func (m DotaModule) write(ctx context.Context, req writeRequest) (writeResult, error) {
	id32, err := dotaID32(req.account)
	if err != nil {
		return writeResult{}, err
	}
	journal := req.journal
	if journal == nil {
		return writeResult{}, fmt.Errorf("sessionkit: dota module needs a part journal")
	}

	// Hold the same lock the manual snapshot tools use, so a Tools operation that slipped
	// past `guardManualConfigWrite` in the instant before this transaction started still
	// cannot interleave with it. Acquired here, under Engine.mu, which is why the guard runs
	// *before* dotaWriteMu on the manual side — the order has to match in both directions.
	dotaWriteMu.Lock()
	defer dotaWriteMu.Unlock()

	var written []string
	for _, part := range req.plan.Parts {
		if err := ctx.Err(); err != nil {
			return writeResult{}, err
		}
		live, ok := dotaAccountPartPath(req.steamRoot, id32, part.ID)
		if !ok {
			continue
		}
		src := filepath.Join(req.payload, part.ID)
		srcAbsent := !dirHasContent(src)
		if srcAbsent && !req.honourAbsence {
			continue
		}

		err := sessionkit.ReplacePart(
			src, live,
			filepath.Join(req.stageRoot, part.ID),
			filepath.Join(req.rollbackRoot, part.ID),
			srcAbsent,
			func(state sessionkit.ReplaceState) error { return journal(part.ID, state) },
		)
		if err != nil {
			return writeResult{}, err
		}

		// Verify before declaring the part done, so a truncated copy is caught here rather
		// than surfacing later as a phantom "external change".
		//
		// A missing manifest entry is a hard error, not a reason to skip the check: the part
		// would otherwise be journalled `verified` having never been verified at all, and
		// recovery would then refuse to roll it back.
		if !srcAbsent {
			expected, ok := req.expected.Parts[part.ID]
			if !ok {
				return writeResult{}, fmt.Errorf("%w: no expected digest for part %q",
					sessionkit.ErrVerifyFailed, part.ID)
			}
			if err := sessionkit.VerifyPart(part.ID, live, expected); err != nil {
				return writeResult{}, err
			}
		}
		if err := journal(part.ID, sessionkit.ReplaceVerified); err != nil {
			return writeResult{}, err
		}
		written = append(written, part.ID)
	}

	manifest, err := m.hashLive(req.steamRoot, id32, written)
	if err != nil {
		return writeResult{}, err
	}
	return writeResult{parts: written, manifest: manifest}, nil
}

// Verify hashes the live tree and compares it with what was recorded. Read-only, so it is
// the one module method that may run while Steam is up.
func (m DotaModule) Verify(ctx context.Context, req sessionkit.VerifyRequest) (sessionkit.VerifyResult, error) {
	id32, err := dotaID32(req.Account)
	if err != nil {
		return sessionkit.VerifyResult{}, err
	}
	var ids []string
	for _, part := range req.Plan.Parts {
		if _, ok := req.Expected.Parts[part.ID]; ok {
			ids = append(ids, part.ID)
		}
	}
	current, err := m.hashLive(req.SteamRoot, id32, ids)
	if err != nil {
		return sessionkit.VerifyResult{}, err
	}
	return sessionkit.VerifyResult{
		Match:       current.Equal(req.Expected),
		Current:     current,
		Differences: current.Diff(req.Expected),
	}, nil
}

// hashLive builds a manifest over the account's live part directories.
func (m DotaModule) hashLive(steamRoot, id32 string, partIDs []string) (sessionkit.Manifest, error) {
	man := sessionkit.Manifest{ModuleID: m.ID(), Parts: map[string]sessionkit.PartManifest{}}
	for _, id := range partIDs {
		live, ok := dotaAccountPartPath(steamRoot, id32, id)
		if !ok {
			continue
		}
		pm, err := sessionkit.HashPart(id, live)
		if err != nil {
			return sessionkit.Manifest{}, err
		}
		man.Parts[id] = pm
	}
	return man, nil
}

// LivePath lets the engine roll a part back without knowing Dota's layout.
func (DotaModule) LivePath(steamRoot string, account sessionkit.AccountRef, partID string) (string, bool) {
	id32, err := dotaID32(account)
	if err != nil {
		return "", false
	}
	return dotaAccountPartPath(steamRoot, id32, partID)
}

// ScratchAnchor puts staging and rollback inside the account's own Dota folder.
//
// `local/` and `remote/` are both children of `userdata/<id32>/570/`, so a sibling directory
// there is guaranteed to be on the same filesystem — which is what makes the install a
// rename rather than a copy that can fail halfway. Anchoring under the data root instead
// would break outright whenever Steam lives on a different drive from %AppData%.
//
// Steam Cloud only syncs the `remote/` subtree, so scratch space beside it is not uploaded.
func (DotaModule) ScratchAnchor(steamRoot string, account sessionkit.AccountRef) (string, bool) {
	id32, err := dotaID32(account)
	if err != nil || strings.TrimSpace(steamRoot) == "" {
		return "", false
	}
	return filepath.Join(steamRoot, "userdata", id32, DotaAppID), true
}

// clearDotaRemoteCache removes Steam's record of what it last uploaded for Dota.
//
// Best-effort by design: the file is absent on a fresh install, and its absence simply makes
// Steam re-scan `remote/` rather than trusting a stale index.
func clearDotaRemoteCache(steamRoot, id32 string) error {
	if id32 == "" {
		return nil
	}
	path := filepath.Join(steamRoot, "userdata", id32, DotaAppID, "remotecache.vdf")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
