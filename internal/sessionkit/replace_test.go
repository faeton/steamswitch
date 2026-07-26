package sessionkit

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type replaceDirs struct {
	src, live, staging, rollback string
}

func setupReplace(t *testing.T, srcFiles, liveFiles map[string]string) replaceDirs {
	t.Helper()
	base := t.TempDir()
	d := replaceDirs{
		src:      filepath.Join(base, "src"),
		live:     filepath.Join(base, "live", "local"),
		staging:  filepath.Join(base, "tx", "staging", "local"),
		rollback: filepath.Join(base, "tx", "rollback", "local"),
	}
	for name, content := range srcFiles {
		writeFile(t, filepath.Join(d.src, name), content)
	}
	for name, content := range liveFiles {
		writeFile(t, filepath.Join(d.live, name), content)
	}
	return d
}

func readTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

func recordSteps(states *[]ReplaceState) ReplaceStep {
	return func(s ReplaceState) error {
		*states = append(*states, s)
		return nil
	}
}

func TestReplacePart_InstallsAndJournalsEveryTransition(t *testing.T) {
	d := setupReplace(t, map[string]string{"new.cfg": "new"}, map[string]string{"old.cfg": "old"})

	var states []ReplaceState
	if err := ReplacePart(d.src, d.live, d.staging, d.rollback, false, recordSteps(&states)); err != nil {
		t.Fatalf("replace: %v", err)
	}

	want := []ReplaceState{ReplaceStaged, ReplaceOldMoved, ReplaceInstalled}
	if len(states) != len(want) {
		t.Fatalf("states = %v, want %v", states, want)
	}
	for i := range want {
		if states[i] != want[i] {
			t.Fatalf("states = %v, want %v", states, want)
		}
	}

	if got := readTree(t, d.live); len(got) != 1 || got["new.cfg"] != "new" {
		t.Fatalf("live = %v", got)
	}
	// The old tree must survive in rollback, not be deleted like the inherited replaceDir did.
	if got := readTree(t, d.rollback); got["old.cfg"] != "old" {
		t.Fatalf("rollback = %v, want the previous contents", got)
	}
}

func TestReplacePart_DoesNotMergeWithStaleFiles(t *testing.T) {
	d := setupReplace(t,
		map[string]string{"new.cfg": "new"},
		map[string]string{"stale.cfg": "stale", "new.cfg": "old"})

	var states []ReplaceState
	if err := ReplacePart(d.src, d.live, d.staging, d.rollback, false, recordSteps(&states)); err != nil {
		t.Fatal(err)
	}

	got := readTree(t, d.live)
	if _, ok := got["stale.cfg"]; ok {
		t.Fatalf("live kept a stale file: %v", got)
	}
	if got["new.cfg"] != "new" {
		t.Fatalf("live = %v", got)
	}
}

func TestReplacePart_AbsentSourceRemovesLiveTree(t *testing.T) {
	// Restoring a saved state of "this account had no config" must delete, not no-op.
	d := setupReplace(t, nil, map[string]string{"theirs.cfg": "theirs"})

	var states []ReplaceState
	if err := ReplacePart(d.src, d.live, d.staging, d.rollback, true, recordSteps(&states)); err != nil {
		t.Fatal(err)
	}

	if exists, _ := pathExists(d.live); exists {
		t.Fatal("live tree still present after restoring an absent part")
	}
	if got := readTree(t, d.rollback); got["theirs.cfg"] != "theirs" {
		t.Fatalf("rollback = %v", got)
	}
}

func TestRollbackPart_RestoresFromEveryState(t *testing.T) {
	for _, state := range []ReplaceState{ReplaceOldMoved, ReplaceInstalled, ReplaceVerified} {
		t.Run(string(state), func(t *testing.T) {
			d := setupReplace(t, map[string]string{"new.cfg": "new"}, map[string]string{"old.cfg": "old"})

			// Drive the replacement to completion, then simulate dying at `state`.
			var states []ReplaceState
			if err := ReplacePart(d.src, d.live, d.staging, d.rollback, false, recordSteps(&states)); err != nil {
				t.Fatal(err)
			}

			if err := RollbackPart(d.live, d.staging, d.rollback, state); err != nil {
				t.Fatalf("rollback: %v", err)
			}
			got := readTree(t, d.live)
			if got["old.cfg"] != "old" {
				t.Fatalf("live = %v, want the original contents restored", got)
			}
			if _, ok := got["new.cfg"]; ok {
				t.Fatalf("live still has the applied file: %v", got)
			}
		})
	}
}

func TestRollbackPart_BeforeAnythingMovedLeavesLiveAlone(t *testing.T) {
	for _, state := range []ReplaceState{ReplacePending, ReplaceStaged} {
		t.Run(string(state), func(t *testing.T) {
			d := setupReplace(t, map[string]string{"new.cfg": "new"}, map[string]string{"old.cfg": "old"})
			if err := RollbackPart(d.live, d.staging, d.rollback, state); err != nil {
				t.Fatalf("rollback: %v", err)
			}
			if got := readTree(t, d.live); got["old.cfg"] != "old" {
				t.Fatalf("live = %v, want untouched", got)
			}
		})
	}
}

func TestRollbackPart_RecoversFromACrashBetweenRenameAndJournal(t *testing.T) {
	// The journalled state is only a lower bound: the process can die after the live tree
	// has been renamed into `rollback` but before the `old-moved` write lands, leaving the
	// journal saying `staged`. Trusting the state alone would delete the only surviving copy.
	d := setupReplace(t, map[string]string{"new.cfg": "new"}, map[string]string{"old.cfg": "old"})

	err := ReplacePart(d.src, d.live, d.staging, d.rollback, false, func(s ReplaceState) error {
		if s == ReplaceOldMoved {
			return errors.New("crash before the journal write")
		}
		return nil
	})
	if err == nil {
		t.Fatal("expected the simulated crash")
	}

	if err := RollbackPart(d.live, d.staging, d.rollback, ReplaceStaged); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if got := readTree(t, d.live); got["old.cfg"] != "old" {
		t.Fatalf("live = %v, want the original restored, not lost", got)
	}
}

func TestRollbackPart_IsIdempotent(t *testing.T) {
	// Recovery re-runs without knowing whether a previous attempt already finished.
	d := setupReplace(t, map[string]string{"new.cfg": "new"}, map[string]string{"old.cfg": "old"})
	var states []ReplaceState
	if err := ReplacePart(d.src, d.live, d.staging, d.rollback, false, recordSteps(&states)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := RollbackPart(d.live, d.staging, d.rollback, ReplaceInstalled); err != nil {
			t.Fatalf("rollback %d: %v", i, err)
		}
	}
	if got := readTree(t, d.live); got["old.cfg"] != "old" {
		t.Fatalf("live = %v after repeated rollback", got)
	}
}

func TestRollbackPart_RestoresAbsenceWhenLiveDidNotExist(t *testing.T) {
	// Live was absent, the kit created it; rolling back must remove it again.
	d := setupReplace(t, map[string]string{"new.cfg": "new"}, nil)
	var states []ReplaceState
	if err := ReplacePart(d.src, d.live, d.staging, d.rollback, false, recordSteps(&states)); err != nil {
		t.Fatal(err)
	}
	if exists, _ := pathExists(d.live); !exists {
		t.Fatal("expected the kit to have created the live tree")
	}
	if err := RollbackPart(d.live, d.staging, d.rollback, ReplaceInstalled); err != nil {
		t.Fatal(err)
	}
	if exists, _ := pathExists(d.live); exists {
		t.Fatal("rollback left a tree behind where there had been none")
	}
}

func TestVerifyPart(t *testing.T) {
	d := setupReplace(t, map[string]string{"a.cfg": "content"}, nil)
	var states []ReplaceState
	if err := ReplacePart(d.src, d.live, d.staging, d.rollback, false, recordSteps(&states)); err != nil {
		t.Fatal(err)
	}

	expected, err := HashPart("local", d.src)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyPart("local", d.live, expected); err != nil {
		t.Fatalf("verify: %v", err)
	}

	writeFile(t, filepath.Join(d.live, "a.cfg"), "tampered")
	if err := VerifyPart("local", d.live, expected); !errors.Is(err, ErrVerifyFailed) {
		t.Fatalf("err = %v, want ErrVerifyFailed", err)
	}
}

func TestReplacePart_StepErrorAbortsBeforeTouchingLive(t *testing.T) {
	d := setupReplace(t, map[string]string{"new.cfg": "new"}, map[string]string{"old.cfg": "old"})

	boom := errors.New("journal write failed")
	err := ReplacePart(d.src, d.live, d.staging, d.rollback, false, func(s ReplaceState) error {
		if s == ReplaceStaged {
			return boom
		}
		return nil
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the step error", err)
	}
	// A journal write that fails must stop the operation while the live tree is still intact.
	if got := readTree(t, d.live); got["old.cfg"] != "old" {
		t.Fatalf("live = %v, want untouched", got)
	}
}
