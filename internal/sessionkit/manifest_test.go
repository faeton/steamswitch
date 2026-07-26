package sessionkit

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestHashPart_AbsentEmptyFilledAreDistinct(t *testing.T) {
	base := t.TempDir()

	absent, err := HashPart("local", filepath.Join(base, "nope"))
	if err != nil {
		t.Fatalf("absent: %v", err)
	}
	if absent.State != PartAbsent {
		t.Fatalf("state = %q, want absent", absent.State)
	}

	emptyDir := filepath.Join(base, "empty")
	if err := os.MkdirAll(emptyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	empty, err := HashPart("local", emptyDir)
	if err != nil {
		t.Fatalf("empty: %v", err)
	}
	if empty.State != PartEmpty {
		t.Fatalf("state = %q, want empty", empty.State)
	}

	// This is the distinction restore depends on: "there was nothing here" must not hash
	// the same as "there was an empty folder here".
	if absent.Digest == empty.Digest {
		t.Fatal("absent and empty parts produced the same digest")
	}

	filledDir := filepath.Join(base, "filled")
	writeFile(t, filepath.Join(filledDir, "a.cfg"), "x")
	filled, err := HashPart("local", filledDir)
	if err != nil {
		t.Fatalf("filled: %v", err)
	}
	if filled.State != PartFilled || len(filled.Files) != 1 {
		t.Fatalf("filled = %+v", filled)
	}
	if filled.Digest == empty.Digest {
		t.Fatal("filled and empty parts produced the same digest")
	}
}

func TestHashPart_DetectsSameSizeEdit(t *testing.T) {
	// The failure mode a size-only check misses: an edit that keeps the byte count.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "hero_grid.json"), "aaaa")
	before, err := HashPart("remote", dir)
	if err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(dir, "hero_grid.json"), "bbbb")
	after, err := HashPart("remote", dir)
	if err != nil {
		t.Fatal(err)
	}

	if before.Digest == after.Digest {
		t.Fatal("same-size edit did not change the digest")
	}
	if before.Files[0].Size != after.Files[0].Size {
		t.Fatal("test is not exercising the same-size case")
	}
}

func TestHashPart_IsStableAcrossWalkOrder(t *testing.T) {
	build := func(dir string, order []string) PartManifest {
		for _, name := range order {
			writeFile(t, filepath.Join(dir, name), name)
		}
		pm, err := HashPart("local", dir)
		if err != nil {
			t.Fatal(err)
		}
		return pm
	}
	a := build(t.TempDir(), []string{"a.cfg", "sub/b.cfg", "c.cfg"})
	b := build(t.TempDir(), []string{"c.cfg", "a.cfg", "sub/b.cfg"})
	if a.Digest != b.Digest {
		t.Fatalf("digest depends on creation order: %s vs %s", a.Digest, b.Digest)
	}
}

func TestHashPart_RejectsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevation on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "outside.cfg")
	writeFile(t, target, "secret")
	if err := os.Symlink(target, filepath.Join(dir, "link.cfg")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	// Silently skipping a symlink would let a tree change without changing its digest.
	if _, err := HashPart("local", dir); !errors.Is(err, ErrUnsupportedEntry) {
		t.Fatalf("err = %v, want ErrUnsupportedEntry", err)
	}
}

func TestManifestDiff(t *testing.T) {
	dirA := t.TempDir()
	writeFile(t, filepath.Join(dirA, "keep.cfg"), "same")
	writeFile(t, filepath.Join(dirA, "gone.cfg"), "old")
	writeFile(t, filepath.Join(dirA, "edit.cfg"), "one")

	dirB := t.TempDir()
	writeFile(t, filepath.Join(dirB, "keep.cfg"), "same")
	writeFile(t, filepath.Join(dirB, "edit.cfg"), "two")
	writeFile(t, filepath.Join(dirB, "new.cfg"), "new")

	parts := []Part{{ID: "local", Risk: PartLocalSafe, KitEligible: true}}
	expected, err := HashParts("dota2", parts, func(string) (string, bool) { return dirA, true })
	if err != nil {
		t.Fatal(err)
	}
	current, err := HashParts("dota2", parts, func(string) (string, bool) { return dirB, true })
	if err != nil {
		t.Fatal(err)
	}

	if expected.Equal(current) {
		t.Fatal("manifests compared equal despite differing content")
	}

	got := map[string]string{}
	for _, d := range expected.Diff(current) {
		got[d.Path] = d.Kind
	}
	want := map[string]string{"gone.cfg": "removed", "edit.cfg": "changed", "new.cfg": "added"}
	if len(got) != len(want) {
		t.Fatalf("diff = %v, want %v", got, want)
	}
	for path, kind := range want {
		if got[path] != kind {
			t.Fatalf("diff[%s] = %q, want %q", path, got[path], kind)
		}
	}
}

func TestManifestDiff_WholePartAppearanceIsOneEntry(t *testing.T) {
	filled := t.TempDir()
	writeFile(t, filepath.Join(filled, "a.cfg"), "1")
	writeFile(t, filepath.Join(filled, "b.cfg"), "2")

	parts := []Part{{ID: "remote", Risk: PartCloudRisk, KitEligible: true}}
	absent, err := HashParts("dota2", parts, func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	present, err := HashParts("dota2", parts, func(string) (string, bool) { return filled, true })
	if err != nil {
		t.Fatal(err)
	}

	diffs := absent.Diff(present)
	if len(diffs) != 1 || diffs[0].Kind != "added" || diffs[0].Path != "" {
		t.Fatalf("diffs = %+v, want a single part-level 'added'", diffs)
	}
}

func TestManifestEqual_IdenticalTreesMatch(t *testing.T) {
	mk := func() string {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "sub/x.cfg"), "hello")
		return dir
	}
	parts := []Part{{ID: "local"}}
	a, err := HashParts("dota2", parts, func(string) (string, bool) { return mk(), true })
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashParts("dota2", parts, func(string) (string, bool) { return mk(), true })
	if err != nil {
		t.Fatal(err)
	}
	if !a.Equal(b) {
		t.Fatal("identical trees did not compare equal")
	}
}
