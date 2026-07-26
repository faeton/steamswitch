package sessionkit

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"steamswitch/internal/fsutil"
	"steamswitch/internal/paths"
)

// On-disk layout under the data root:
//
//	<DataRoot>/SessionKit/
//	  active.json
//	  transactions/<tx>/journal.json
//	  transactions/<tx>/staging/<module>/<part>/
//	  transactions/<tx>/rollback/<module>/<part>/
//	  snapshots/<module>/<steamId64>/<snapshot>/{snapshot.json,manifest.json,payload/<part>/}
//	  last-known-good/<module>/<steamId64>.json
//	  archive/<tx>.json
//
// Snapshot directories are immutable once committed. Committing is a rename of a fully
// written, fully hashed temporary directory, so a half-copied snapshot can never be mistaken
// for a good one.

// ErrNoSnapshot is returned when a referenced snapshot is missing or unreadable.
var ErrNoSnapshot = errors.New("Toast_Kit_SnapshotMissing")

// SnapshotMeta is the metadata beside a snapshot payload.
type SnapshotMeta struct {
	ID            string   `json:"id"`
	ModuleID      string   `json:"moduleId"`
	SteamID64     string   `json:"steamId64"`
	Purpose       string   `json:"purpose"`
	TransactionID string   `json:"transactionId,omitempty"`
	Parts         []string `json:"parts"`
	CreatedAt     string   `json:"createdAt"`
}

// store owns every path under the session-kit root.
type store struct {
	root    string
	journal journalStore
}

// SessionKitRoot is `<DataRoot>/SessionKit`. Must be called after platform.InitDataPaths.
func SessionKitRoot() (string, error) {
	r, err := paths.DataRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(r, "SessionKit"), nil
}

func newStore() (store, error) {
	root, err := SessionKitRoot()
	if err != nil {
		return store{}, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return store{}, err
	}
	return store{root: root, journal: journalStore{root: root}}, nil
}

// newID returns a filesystem-safe random identifier. Random rather than sequential so a
// snapshot directory is never reused, which is half of "never overwrite the last
// known-good snapshot".
func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func (s store) txDir(txID string) (string, error) { return s.journal.txDir(txID) }

func (s store) stagingDir(txID, moduleID string) (string, error) {
	dir, err := s.txDir(txID)
	if err != nil {
		return "", err
	}
	if !isSafeID(moduleID) {
		return "", fmt.Errorf("%w: module id %q", ErrJournalCorrupt, moduleID)
	}
	return filepath.Join(dir, "staging", moduleID), nil
}

func (s store) rollbackDir(txID, moduleID string) (string, error) {
	dir, err := s.txDir(txID)
	if err != nil {
		return "", err
	}
	if !isSafeID(moduleID) {
		return "", fmt.Errorf("%w: module id %q", ErrJournalCorrupt, moduleID)
	}
	return filepath.Join(dir, "rollback", moduleID), nil
}

// accountKey turns a SteamID64 into a path segment. It must never be able to escape.
func accountKey(a AccountRef) (string, error) {
	id := strings.TrimSpace(a.SteamID64)
	if id == "" {
		return "", fmt.Errorf("%w: empty account id", ErrJournalCorrupt)
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return "", fmt.Errorf("%w: account id %q", ErrJournalCorrupt, id)
		}
	}
	return id, nil
}

func (s store) snapshotsDir(moduleID string, account AccountRef) (string, error) {
	if !isSafeID(moduleID) {
		return "", fmt.Errorf("%w: module id %q", ErrJournalCorrupt, moduleID)
	}
	key, err := accountKey(account)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.root, "snapshots", moduleID, key), nil
}

func (s store) snapshotDir(moduleID string, account AccountRef, snapshotID string) (string, error) {
	base, err := s.snapshotsDir(moduleID, account)
	if err != nil {
		return "", err
	}
	if !isSafeID(snapshotID) {
		return "", fmt.Errorf("%w: snapshot id %q", ErrJournalCorrupt, snapshotID)
	}
	return filepath.Join(base, snapshotID), nil
}

// beginSnapshot creates a temporary snapshot directory and returns its payload path.
// The snapshot is invisible to readers until commitSnapshot renames it into place.
func (s store) beginSnapshot(moduleID string, account AccountRef) (tmpDir, payloadDir, id string, err error) {
	id, err = newID()
	if err != nil {
		return "", "", "", err
	}
	base, err := s.snapshotsDir(moduleID, account)
	if err != nil {
		return "", "", "", err
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", "", "", err
	}
	tmpDir = filepath.Join(base, ".tmp-"+id)
	payloadDir = filepath.Join(tmpDir, "payload")
	if err := os.MkdirAll(payloadDir, 0o755); err != nil {
		return "", "", "", err
	}
	return tmpDir, payloadDir, id, nil
}

// commitSnapshot writes the metadata and manifest, then atomically publishes the snapshot.
func (s store) commitSnapshot(tmpDir, moduleID string, account AccountRef, id string, meta SnapshotMeta, manifest Manifest) (string, error) {
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return "", err
	}
	if err := fsutil.WriteFileAtomic(filepath.Join(tmpDir, "snapshot.json"), metaBytes, 0o644); err != nil {
		return "", err
	}
	manBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	if err := fsutil.WriteFileAtomic(filepath.Join(tmpDir, "manifest.json"), manBytes, 0o644); err != nil {
		return "", err
	}

	final, err := s.snapshotDir(moduleID, account, id)
	if err != nil {
		return "", err
	}
	if err := os.Rename(tmpDir, final); err != nil {
		return "", err
	}
	return final, nil
}

func (s store) abortSnapshot(tmpDir string) {
	_ = os.RemoveAll(tmpDir)
}

// readSnapshot loads a committed snapshot's metadata and manifest.
func (s store) readSnapshot(moduleID string, account AccountRef, id string) (SnapshotMeta, Manifest, string, error) {
	dir, err := s.snapshotDir(moduleID, account, id)
	if err != nil {
		return SnapshotMeta{}, Manifest{}, "", err
	}
	metaBytes, err := os.ReadFile(filepath.Join(dir, "snapshot.json"))
	if err != nil {
		return SnapshotMeta{}, Manifest{}, "", ErrNoSnapshot
	}
	var meta SnapshotMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return SnapshotMeta{}, Manifest{}, "", ErrNoSnapshot
	}
	manBytes, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return SnapshotMeta{}, Manifest{}, "", ErrNoSnapshot
	}
	var man Manifest
	if err := json.Unmarshal(manBytes, &man); err != nil {
		return SnapshotMeta{}, Manifest{}, "", ErrNoSnapshot
	}
	return meta, man, filepath.Join(dir, "payload"), nil
}

// ---------------------------------------------------------------------------
// Last known good
// ---------------------------------------------------------------------------

// lkgPointer names the most recent verified "their setup" snapshot for an account.
type lkgPointer struct {
	SchemaVersion int    `json:"schemaVersion"`
	ModuleID      string `json:"moduleId"`
	SteamID64     string `json:"steamId64"`
	SnapshotID    string `json:"snapshotId"`
	UpdatedAt     string `json:"updatedAt"`
}

func (s store) lkgPath(moduleID string, account AccountRef) (string, error) {
	if !isSafeID(moduleID) {
		return "", fmt.Errorf("%w: module id %q", ErrJournalCorrupt, moduleID)
	}
	key, err := accountKey(account)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.root, "last-known-good", moduleID, key+".json"), nil
}

// setLastKnownGood advances the pointer. It is only ever called after the snapshot payload
// and manifest have both been written and verified, and it does not delete the snapshot the
// pointer previously named — that is what makes "never overwrite the last known-good
// snapshot" structural rather than a convention.
func (s store) setLastKnownGood(moduleID string, account AccountRef, snapshotID string) error {
	path, err := s.lkgPath(moduleID, account)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(lkgPointer{
		SchemaVersion: JournalSchemaVersion,
		ModuleID:      moduleID,
		SteamID64:     account.SteamID64,
		SnapshotID:    snapshotID,
		UpdatedAt:     nowRFC3339(),
	}, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, data, 0o644)
}

func (s store) lastKnownGood(moduleID string, account AccountRef) (string, bool) {
	path, err := s.lkgPath(moduleID, account)
	if err != nil {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var p lkgPointer
	if err := json.Unmarshal(data, &p); err != nil || !isSafeID(p.SnapshotID) {
		return "", false
	}
	return p.SnapshotID, true
}

// ---------------------------------------------------------------------------
// Pruning
// ---------------------------------------------------------------------------

// maxSnapshotsPerAccount caps how many snapshots are kept per module/account. Each is a
// full copy of a config tree, so an uncapped library grows without bound.
const maxSnapshotsPerAccount = 10

// prune deletes the oldest snapshots for one module/account beyond the cap.
//
// `protected` must contain every snapshot referenced by the active journal and by the
// last-known-good pointer. Age alone is not a safe criterion: the oldest snapshot for a
// rarely-touched account may be the only copy of that person's setup, and deleting it
// mid-transaction would strand a restore.
func (s store) prune(moduleID string, account AccountRef, protected map[string]bool) {
	base, err := s.snapshotsDir(moduleID, account)
	if err != nil {
		return
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}

	var candidates []snapshotAge
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".tmp-") {
			continue
		}
		if protected[e.Name()] {
			continue
		}
		meta, _, _, err := s.readSnapshot(moduleID, account, e.Name())
		if err != nil {
			continue
		}
		created, err := time.Parse(time.RFC3339Nano, meta.CreatedAt)
		if err != nil {
			// Unparseable timestamp: treat as ancient but still prunable.
			created = time.Time{}
		}
		candidates = append(candidates, snapshotAge{id: e.Name(), created: created})
	}

	// Keep the newest `maxSnapshotsPerAccount`, counting protected ones against the cap so
	// the total stays bounded.
	keep := maxSnapshotsPerAccount - len(protected)
	if keep < 0 {
		keep = 0
	}
	if len(candidates) <= keep {
		return
	}
	sortByCreatedDesc(candidates)
	for _, c := range candidates[keep:] {
		if dir, err := s.snapshotDir(moduleID, account, c.id); err == nil {
			_ = os.RemoveAll(dir)
		}
	}
}

// snapshotAge pairs a snapshot id with its creation time for pruning.
type snapshotAge struct {
	id      string
	created time.Time
}

// sortByCreatedDesc puts the newest first. Insertion sort: the slice is bounded by the
// number of snapshots kept for one account, which is a handful.
func sortByCreatedDesc(c []snapshotAge) {
	for i := 1; i < len(c); i++ {
		for j := i; j > 0 && c[j].created.After(c[j-1].created); j-- {
			c[j], c[j-1] = c[j-1], c[j]
		}
	}
}

// protectedSnapshots collects every snapshot id the given journal depends on, plus the
// last-known-good pointer for each account it touches.
func (s store) protectedSnapshots(j *Journal, moduleID string, account AccountRef) map[string]bool {
	out := map[string]bool{}
	if lkg, ok := s.lastKnownGood(moduleID, account); ok {
		out[lkg] = true
	}
	if j == nil {
		return out
	}
	if snaps, ok := j.Snapshots[moduleID]; ok {
		if snaps.TheirSetup != "" {
			out[snaps.TheirSetup] = true
		}
		if snaps.KitSource != "" {
			out[snaps.KitSource] = true
		}
	}
	return out
}
