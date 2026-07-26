package steam

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"steamswitch/internal/paths"
)

// steamID64 -> id32 is 76561197960287930 -> 22202. Used to build userdata paths.
const (
	testID64A = "76561197960287930"
	testID32A = "22202"
	testID64B = "76561197960287931"
	testID32B = "22203"
)

func newDotaTestEnv(t *testing.T) (steamRoot string) {
	t.Helper()
	base := t.TempDir()
	paths.ResetForTest(filepath.Join(base, "data"))
	return filepath.Join(base, "steam")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestNormalizeDotaParts_DefaultsToAccountPartsAndDedupes(t *testing.T) {
	got, err := normalizeDotaParts(nil)
	if err != nil {
		t.Fatal(err)
	}
	// An empty selection must never imply the machine-wide cfg folder: a caller that
	// forgot to pass parts would otherwise overwrite config shared by every account.
	if !reflect.DeepEqual(got, []string{DotaPartLocal, DotaPartRemote}) {
		t.Fatalf("default parts = %v, want [local remote]", got)
	}
	if containsPart(got, DotaPartGlobalCfg) {
		t.Fatal("default selection includes the machine-wide cfg folder")
	}

	got, err = normalizeDotaParts([]string{"REMOTE", "remote", " local "})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{DotaPartLocal, DotaPartRemote}) {
		t.Fatalf("parts = %v, want [local remote]", got)
	}
}

func TestNormalizeDotaParts_RejectsUnknownOnly(t *testing.T) {
	if _, err := normalizeDotaParts([]string{"saves", "junk"}); err != errDotaNoParts {
		t.Fatalf("err = %v, want errDotaNoParts", err)
	}
}

func TestCaptureSnapshot_OnlyRecordsPartsThatExist(t *testing.T) {
	root := newDotaTestEnv(t)
	var s SteamService

	// Only local/ exists for this account; remote/ and the global cfg folder do not.
	writeFile(t, filepath.Join(root, "userdata", testID32A, DotaAppID, "local", "cfg", "video.txt"), "res=1920")

	snap, err := s.captureDotaSnapshot(root, testID32A, testID64A, "AccA", "Ceb's config", "from a friend", DotaPartsAll, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snap.Parts, []string{DotaPartLocal}) {
		t.Fatalf("snapshot parts = %v, want [local]", snap.Parts)
	}
	if snap.Label != "Ceb's config" || snap.Note != "from a friend" {
		t.Fatalf("label/note not stored: %+v", snap)
	}
	if snap.SizeBytes <= 0 {
		t.Fatalf("SizeBytes = %d, want > 0", snap.SizeBytes)
	}

	dir, err := dotaSnapshotDir(snap.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dir, "local", "cfg", "video.txt")); got != "res=1920" {
		t.Fatalf("snapshot payload = %q", got)
	}
}

func TestCaptureSnapshot_EmptyAccountIsNotSnapshotted(t *testing.T) {
	root := newDotaTestEnv(t)
	var s SteamService

	_, err := s.captureDotaSnapshot(root, testID32A, testID64A, "AccA", "empty", "", DotaPartsAll, false)
	if err != errDotaNothingCopied {
		t.Fatalf("err = %v, want errDotaNothingCopied", err)
	}
	// A failed capture must not leave an orphan folder behind.
	snaps, err := s.ListDotaSnapshots()
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 0 {
		t.Fatalf("snapshots = %d, want 0", len(snaps))
	}
}

func TestSnapshotLibrary_ListRenameDelete(t *testing.T) {
	root := newDotaTestEnv(t)
	var s SteamService
	writeFile(t, filepath.Join(root, "userdata", testID32A, DotaAppID, "local", "a.txt"), "x")

	snap, err := s.captureDotaSnapshot(root, testID32A, testID64A, "AccA", "auto point", "", DotaPartsAll, true)
	if err != nil {
		t.Fatal(err)
	}

	snaps, err := s.ListDotaSnapshots()
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 1 || snaps[0].ID != snap.ID || !snaps[0].Auto {
		t.Fatalf("list = %+v", snaps)
	}

	// Naming a revert point promotes it to a curated config.
	renamed, err := s.RenameDotaSnapshot(snap.ID, "Ceb's config", "imported")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Label != "Ceb's config" || renamed.Note != "imported" || renamed.Auto {
		t.Fatalf("renamed = %+v", renamed)
	}
	if _, err := s.RenameDotaSnapshot(snap.ID, "   ", ""); err != errDotaInvalidLabel {
		t.Fatalf("blank label err = %v, want errDotaInvalidLabel", err)
	}

	if err := s.DeleteDotaSnapshot(snap.ID); err != nil {
		t.Fatal(err)
	}
	snaps, err = s.ListDotaSnapshots()
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 0 {
		t.Fatalf("after delete, snapshots = %d, want 0", len(snaps))
	}
}

func TestDotaSnapshotDir_RejectsTraversal(t *testing.T) {
	newDotaTestEnv(t)
	for _, id := range []string{"", "..", "../evil", `a\b`, "a/b"} {
		if _, err := dotaSnapshotDir(id); err != errDotaNoSnapshot {
			t.Fatalf("dotaSnapshotDir(%q) err = %v, want errDotaNoSnapshot", id, err)
		}
	}
}

func TestApplyDotaParts_OverwritesDestAndLeavesRevertPoint(t *testing.T) {
	root := newDotaTestEnv(t)
	var s SteamService

	srcLocal := filepath.Join(root, "userdata", testID32A, DotaAppID, "local")
	dstLocal := filepath.Join(root, "userdata", testID32B, DotaAppID, "local")
	writeFile(t, filepath.Join(srcLocal, "cfg", "video.txt"), "source")
	writeFile(t, filepath.Join(dstLocal, "cfg", "video.txt"), "destination")

	res, err := s.applyDotaParts(root, testID64B, []string{DotaPartLocal}, func(part string) (string, bool) {
		return dotaAccountPartPath(root, testID32A, part)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.CopiedParts, []string{DotaPartLocal}) {
		t.Fatalf("copied = %v", res.CopiedParts)
	}
	if got := readFile(t, filepath.Join(dstLocal, "cfg", "video.txt")); got != "source" {
		t.Fatalf("destination content = %q, want source", got)
	}

	// The destination's previous config must be recoverable.
	if res.RevertSnapshotID == "" {
		t.Fatal("no revert snapshot recorded")
	}
	revertDir, err := dotaSnapshotDir(res.RevertSnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(revertDir, "local", "cfg", "video.txt")); got != "destination" {
		t.Fatalf("revert point content = %q, want destination", got)
	}
}

func TestApplyDotaParts_StaleDestFilesAreNotLeftBehind(t *testing.T) {
	root := newDotaTestEnv(t)
	var s SteamService

	writeFile(t, filepath.Join(root, "userdata", testID32A, DotaAppID, "local", "keep.txt"), "new")
	writeFile(t, filepath.Join(root, "userdata", testID32B, DotaAppID, "local", "stale.txt"), "old")

	if _, err := s.applyDotaParts(root, testID64B, []string{DotaPartLocal}, func(part string) (string, bool) {
		return dotaAccountPartPath(root, testID32A, part)
	}); err != nil {
		t.Fatal(err)
	}

	stale := filepath.Join(root, "userdata", testID32B, DotaAppID, "local", "stale.txt")
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale file survived the copy (err=%v); dest should be replaced, not merged", err)
	}
}

func TestApplyDotaSnapshot_AppliesOnlyStoredParts(t *testing.T) {
	root := newDotaTestEnv(t)
	var s SteamService

	// Snapshot holds local/ only.
	writeFile(t, filepath.Join(root, "userdata", testID32A, DotaAppID, "local", "a.txt"), "snap")
	snap, err := s.captureDotaSnapshot(root, testID32A, testID64A, "AccA", "cfg", "", DotaPartsAll, false)
	if err != nil {
		t.Fatal(err)
	}

	// Asking for remote/ too must not fail; it is simply not present in the snapshot.
	var usable []string
	for _, p := range []string{DotaPartLocal, DotaPartRemote} {
		if containsPart(snap.Parts, p) {
			usable = append(usable, p)
		}
	}
	if !reflect.DeepEqual(usable, []string{DotaPartLocal}) {
		t.Fatalf("usable = %v, want [local]", usable)
	}

	dir, err := dotaSnapshotDir(snap.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.applyDotaParts(root, testID64B, usable, func(part string) (string, bool) {
		return filepath.Join(dir, part), true
	}); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, filepath.Join(root, "userdata", testID32B, DotaAppID, "local", "a.txt"))
	if got != "snap" {
		t.Fatalf("applied content = %q, want snap", got)
	}
}

func TestDotaSteamRunningGuard_IgnoresLocalOnlyCopies(t *testing.T) {
	// local/ is not cloud-synced, so a running Steam is not a hazard for it.
	if err := dotaSteamRunningGuard([]string{DotaPartLocal}); err != nil {
		t.Fatalf("guard blocked a local-only copy: %v", err)
	}
}

func TestResolveDotaGlobalCfg_FindsSecondaryLibrary(t *testing.T) {
	base := t.TempDir()
	paths.ResetForTest(filepath.Join(base, "data"))
	root := filepath.Join(base, "steam")
	lib := filepath.Join(base, "OtherDrive")

	// Dota installed on a second library, not under the Steam root.
	writeFile(t, filepath.Join(lib, "steamapps", "common", "dota 2 beta", "game", "dota", "cfg", "autoexec.cfg"), "echo hi")
	writeFile(t, filepath.Join(root, "steamapps", "libraryfolders.vdf"),
		"\"libraryfolders\"\n{\n\t\"0\"\n\t{\n\t\t\"path\"\t\t\""+filepath.ToSlash(lib)+"\"\n\t}\n}\n")

	got := resolveDotaGlobalCfg(root)
	want := filepath.Join(lib, "steamapps", "common", "dota 2 beta", "game", "dota", "cfg")
	if got != want {
		t.Fatalf("resolveDotaGlobalCfg = %q, want %q", got, want)
	}
}

func TestResolveDotaGlobalCfg_MissingInstallReportsUnavailable(t *testing.T) {
	base := t.TempDir()
	paths.ResetForTest(filepath.Join(base, "data"))
	root := filepath.Join(base, "steam")

	if got := resolveDotaGlobalCfg(root); got != "" {
		t.Fatalf("resolveDotaGlobalCfg = %q, want empty when Dota is not installed", got)
	}
	if _, ok := dotaAccountPartPath(root, testID32A, DotaPartGlobalCfg); ok {
		t.Fatal("globalcfg reported available with no Dota install")
	}
}
