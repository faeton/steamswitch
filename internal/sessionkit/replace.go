package sessionkit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"steamswitch/internal/fsutil"
)

// Staged tree replacement.
//
// The inherited `steam.replaceDir` does `os.RemoveAll(dst)` and then copies. That leaves a
// window in which the destination is gone or half-written and nothing on disk says what it
// used to hold — precisely the crash the redesign's safety rules exist to survive.
//
// Instead, each part goes through:
//
//	pending → staged → old-moved → installed → verified
//
// with a journal write after every arrow. Each individual step is an atomic rename; the
// *pair* of renames is not atomic, but because the substate is durable the recovery pass
// can always tell which side of the gap it died on and finish or undo accordingly.

// ErrVerifyFailed means the installed tree did not hash to what was staged.
var ErrVerifyFailed = errors.New("Toast_Kit_VerifyFailed")

// ReplaceStep reports progress so the caller can journal between stages.
type ReplaceStep func(state ReplaceState) error

// removeRetryBudget matches the tolerance used elsewhere for Windows file locks: an
// antivirus or a lingering handle can hold a directory for a beat after the process exits.
const removeRetryBudget = 3 * time.Second

// ReplacePart installs `src` at `live`, keeping the previous contents in `rollback`.
//
// `srcAbsent` expresses "the saved state had nothing here", which restore must reproduce by
// removing the live directory rather than leaving the overlay in place. Apply never passes
// it: a kit with no content for a part simply skips that part.
//
// `step` is called after each durable transition. If it returns an error the operation
// aborts and the caller is responsible for recovery via RollbackPart.
func ReplacePart(src, live, staging, rollback string, srcAbsent bool, step ReplaceStep) error {
	if err := os.MkdirAll(filepath.Dir(staging), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(rollback), 0o755); err != nil {
		return err
	}

	// 1. Stage a private copy on the same volume, so the install below is a rename and not
	//    a copy that could fail halfway.
	if err := fsutil.RemoveAllWithRetry(staging, removeRetryBudget, os.RemoveAll); err != nil {
		return err
	}
	if !srcAbsent {
		if err := fsutil.CopyDir(src, staging); err != nil {
			return err
		}
		if err := step(ReplaceStaged); err != nil {
			return err
		}
	} else if err := step(ReplaceStaged); err != nil {
		return err
	}

	// 2. Move the live tree aside. After this point the destination is missing, but the
	//    journal says `old-moved` and names the rollback directory holding the original.
	if err := fsutil.RemoveAllWithRetry(rollback, removeRetryBudget, os.RemoveAll); err != nil {
		return err
	}
	liveExisted, err := pathExists(live)
	if err != nil {
		return err
	}
	if liveExisted {
		if err := os.Rename(live, rollback); err != nil {
			return err
		}
	} else if err := writeAbsentMarker(rollback); err != nil {
		return err
	}
	// RollbackPart treats the presence of `rollback` as the authority on whether this move
	// happened — more trustworthy than the journal, which can lag it. That only holds if the
	// directory entry is durable, so sync both ends of the rename before journalling it.
	if err := syncParents(live, rollback); err != nil {
		return err
	}
	if err := step(ReplaceOldMoved); err != nil {
		return err
	}

	// 3. Install.
	if !srcAbsent {
		if err := os.MkdirAll(filepath.Dir(live), 0o755); err != nil {
			return err
		}
		if err := os.Rename(staging, live); err != nil {
			return err
		}
		if err := syncParents(live, staging); err != nil {
			return err
		}
	}
	return step(ReplaceInstalled)
}

// syncParents makes the directory entries touched by a rename durable. A missing parent is
// not an error: `staging` is removed by a later step, and syncing its parent is only useful
// while it is still there.
func syncParents(paths ...string) error {
	seen := map[string]bool{}
	for _, p := range paths {
		dir := filepath.Dir(p)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		if err := fsutil.SyncDir(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// VerifyPart re-hashes the installed tree and compares it against what was expected.
func VerifyPart(partID, live string, expected PartManifest) error {
	got, err := HashPart(partID, live)
	if err != nil {
		return err
	}
	if got.Digest != expected.Digest {
		return fmt.Errorf("%w: %s (%s != %s)", ErrVerifyFailed, partID, got.Digest, expected.Digest)
	}
	return nil
}

// RollbackPart undoes a replacement from any substate.
//
// It is written to be idempotent and to tolerate being called on a tree it has already
// restored, because recovery on the next launch calls it without knowing how far a previous
// attempt got.
func RollbackPart(live, staging, rollback string, state ReplaceState) error {
	switch state {
	case ReplacePending, ReplaceStaged, ReplaceOldMoved, ReplaceInstalled, ReplaceVerified:
	default:
		return fmt.Errorf("%w: unknown replace state %q", ErrJournalCorrupt, state)
	}

	// The journalled state is a lower bound on what happened, never an upper one: the
	// process can die *between* a rename and the journal write that records it. In
	// particular a crash after `os.Rename(live, rollback)` but before the `old-moved` write
	// leaves the journal saying `staged` while the original tree is already sitting in
	// `rollback` — trusting the state alone would delete it and leave nothing live.
	//
	// So the rollback directory itself is the authority. `ReplacePart` clears it before the
	// rename and always populates it afterwards (with a marker when the live tree was
	// absent), which makes its presence an exact record of whether the move happened,
	// independent of how far the journal got.
	rollbackExists, err := pathExists(rollback)
	if err != nil {
		return err
	}
	if !rollbackExists {
		// Either nothing was moved yet, or a previous rollback already consumed it.
		// Removing `live` here would destroy an intact — possibly just-restored — tree.
		return fsutil.RemoveAllWithRetry(staging, removeRetryBudget, os.RemoveAll)
	}

	absent, err := hasAbsentMarker(rollback)
	if err != nil {
		return err
	}
	if err := fsutil.RemoveAllWithRetry(live, removeRetryBudget, os.RemoveAll); err != nil {
		return err
	}
	if absent {
		// The original state was "no directory here"; restoring it means leaving none.
		if err := fsutil.RemoveAllWithRetry(rollback, removeRetryBudget, os.RemoveAll); err != nil {
			return err
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(live), 0o755); err != nil {
			return err
		}
		if err := os.Rename(rollback, live); err != nil {
			return err
		}
	}
	return fsutil.RemoveAllWithRetry(staging, removeRetryBudget, os.RemoveAll)
}

// ReleaseRollback drops the rollback copy once the transaction reaches a durable safe point.
// Best-effort: leaving it behind wastes disk but is never incorrect.
func ReleaseRollback(rollback string) {
	_ = fsutil.RemoveAllWithRetry(rollback, removeRetryBudget, os.RemoveAll)
}

// absentMarkerName marks a rollback directory that stands in for "there was nothing here".
//
// Without it, an empty `rollback` would be indistinguishable from an already-consumed one,
// and a second rollback pass — which crash recovery may well run — would delete the very
// tree the first pass restored.
const absentMarkerName = ".sessionkit-absent"

func writeAbsentMarker(rollback string) error {
	if err := os.MkdirAll(rollback, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(rollback, absentMarkerName), nil, 0o644)
}

func hasAbsentMarker(rollback string) (bool, error) {
	return pathExists(filepath.Join(rollback, absentMarkerName))
}

func pathExists(p string) (bool, error) {
	_, err := os.Lstat(p)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// partPath is the per-part subdirectory inside a staging or rollback root.
func partPath(root, partID string) string {
	return filepath.Join(root, partID)
}
