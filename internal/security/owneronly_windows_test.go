//go:build windows

package security

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Wine reports a made-up ACL on the macOS filesystem whatever was set, so only real Windows can say.
func skipOnWine(t *testing.T) {
	t.Helper()
	if windows.NewLazySystemDLL("ntdll.dll").NewProc("wine_get_version").Find() == nil {
		t.Skip("Wine does not keep file ACLs on this filesystem")
	}
}

func assertOnlyCurrentUser(t *testing.T, path string) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("read security of %s: %v", path, err)
	}
	assertDescriptorIsOwnerOnly(t, path, sd)
}

func assertDescriptorIsOwnerOnly(t *testing.T, path string, sd *windows.SECURITY_DESCRIPTOR) {
	t.Helper()
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Errorf("%s: DACL still inherits from its parent", path)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	user, err := currentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	if dacl.AceCount != 1 {
		t.Fatalf("%s: %d ACEs, want only the current user", path, dacl.AceCount)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil {
		t.Fatal(err)
	}
	sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	if !sid.Equals(user) {
		t.Errorf("%s: ACE is for %s, want %s", path, sid, user)
	}
}

func TestOwnerOnlyDescriptor_NamesOnlyTheCurrentUser(t *testing.T) {
	sd, err := ownerOnlyDescriptor()
	if err != nil {
		t.Fatal(err)
	}
	assertDescriptorIsOwnerOnly(t, "descriptor", sd)
}

// The parent grants Everyone read, standing in for a portable folder on a shared drive.
func openParent(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	sd, err := windows.SecurityDescriptorFromString("D:AI(A;OICI;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestWriteSecretFile_ExcludesWhatTheFolderGrants(t *testing.T) {
	skipOnWine(t)
	path := filepath.Join(openParent(t), "secrets.ssvault")
	for _, body := range []string{"first", "second, replacing the first"} {
		if err := WriteSecretFile(path, []byte(body)); err != nil {
			t.Fatalf("write: %v", err)
		}
		assertOnlyCurrentUser(t, path)
		got, err := os.ReadFile(path)
		if err != nil || string(got) != body {
			t.Fatalf("read back %q, %v", got, err)
		}
	}
}

func TestWriteSecretFile_FailureKeepsThePreviousFileAndNoTemp(t *testing.T) {
	// Replaces the target, so this one holds under Wine too.
	dir := openParent(t)
	path := filepath.Join(dir, "secrets.ssvault")
	if err := WriteSecretFile(path, []byte("good")); err != nil {
		t.Fatal(err)
	}
	// A directory in the way of the target makes the final rename fail.
	blocked := filepath.Join(dir, "blocked")
	if err := os.MkdirAll(filepath.Join(blocked, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteSecretFile(blocked, []byte("x")); err == nil {
		t.Fatal("expected the rename onto a directory to fail")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "good" {
		t.Fatalf("previous file = %q", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatalf("entries = %v, want the file and the directory only", entries)
	}
}

func TestTightenOwnerOnlyDir_FixesFilesAlreadyThere(t *testing.T) {
	skipOnWine(t)
	dir := openParent(t)
	sub := filepath.Join(dir, "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "old.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := TightenOwnerOnlyDir(dir); err != nil {
		t.Fatalf("tighten: %v", err)
	}
	assertOnlyCurrentUser(t, path)
	if got, err := os.ReadFile(path); err != nil || string(got) != "old" {
		t.Fatalf("read back %q, %v", got, err)
	}
}

func TestTightenOwnerOnlyDir_MissingDirIsFine(t *testing.T) {
	if err := TightenOwnerOnlyDir(filepath.Join(t.TempDir(), "nope")); err != nil {
		t.Fatal(err)
	}
}
