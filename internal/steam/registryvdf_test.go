package steam

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A trimmed copy of a real macOS registry.vdf, keeping the shape that matters: two hives, a
// nested "steamglobal" subtree inside the Steam key, and a sibling key ("Steamsteamglobal")
// one level up that a naive rebuild would lose.
const sampleRegistryVDF = `"Registry"
{
	"HKLM"
	{
		"Software"
		{
			"Valve"
			{
				"Steam"
				{
					"SteamPID"		"0"
					"ClientLauncherType"		"0"
				}
			}
		}
	}
	"HKCU"
	{
		"Software"
		{
			"Valve"
			{
				"Steam"
				{
					"StartupModeTmpIsValid"		"0"
					"steamglobal"
					{
						"language"		"english"
					}
					"language"		"english"
					"AutoLoginUser"		"someone"
					"Rate"		"30000"
				}
				"Steamsteamglobal"
				{
					"language"		"english"
				}
			}
		}
	}
}
`

func writeSampleRegistry(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(registryVDFPath(root), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestRegistryVDF_ReadsTheSteamHive(t *testing.T) {
	root := writeSampleRegistry(t, sampleRegistryVDF)
	reg, err := readRegistryVDF(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := reg.Get("AutoLoginUser"); got != "someone" {
		t.Fatalf("AutoLoginUser = %q, want %q", got, "someone")
	}
	if got := reg.Get("NotThere"); got != "" {
		t.Fatalf("absent key read as %q, want empty", got)
	}
	// Must read HKCU, not HKLM: both hives have a Steam key and they hold different things.
	if got := reg.Get("ClientLauncherType"); got != "" {
		t.Fatalf("read %q from the HKLM hive; the Steam key we own is under HKCU", got)
	}
}

// TestRegistryVDF_WriteIsLossless is the point of the whole file. registry.vdf holds the
// client's entire configuration, so a switch that rebuilt it from the two keys it owns would
// cost the user everything else in it.
func TestRegistryVDF_WriteIsLossless(t *testing.T) {
	root := writeSampleRegistry(t, sampleRegistryVDF)
	reg, err := readRegistryVDF(root)
	if err != nil {
		t.Fatal(err)
	}
	reg.Set("AutoLoginUser", "otherperson")
	if err := reg.Write(root); err != nil {
		t.Fatal(err)
	}

	out, err := os.ReadFile(registryVDFPath(root))
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)

	for _, want := range []string{
		`"SteamPID"`,              // the other hive survived
		`"ClientLauncherType"`,    //
		`"StartupModeTmpIsValid"`, // siblings inside the Steam key survived
		`"Rate"`,                  //
		`"steamglobal"`,           // a nested subtree survived
		`"Steamsteamglobal"`,      // a sibling of the Steam key survived
		`"otherperson"`,           // and the edit landed
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("%s is missing after a write:\n%s", want, text)
		}
	}
	if strings.Contains(text, `"someone"`) {
		t.Fatalf("the old AutoLoginUser is still present:\n%s", text)
	}

	// And it must still parse, at the same values.
	reread, err := readRegistryVDF(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := reread.Get("AutoLoginUser"); got != "otherperson" {
		t.Fatalf("after round trip AutoLoginUser = %q", got)
	}
	if got := reread.Get("Rate"); got != "30000" {
		t.Fatalf("after round trip Rate = %q, want 30000", got)
	}
}

func TestRegistryVDF_MissingFileIsAFirstRunNotAnError(t *testing.T) {
	root := t.TempDir()
	reg, err := readRegistryVDF(root)
	if err != nil {
		t.Fatalf("a Steam install that has never been signed into must not be an error: %v", err)
	}
	reg.Set("AutoLoginUser", "first")
	reg.Set("RememberPassword", "1")
	if err := reg.Write(root); err != nil {
		t.Fatal(err)
	}

	reread, err := readRegistryVDF(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := reread.Get("AutoLoginUser"); got != "first" {
		t.Fatalf("AutoLoginUser = %q, want %q", got, "first")
	}
	if got := reread.Get("RememberPassword"); got != "1" {
		t.Fatalf("RememberPassword = %q, want 1", got)
	}
}

func TestRegistryVDF_RefusesAFileThatIsNotARegistry(t *testing.T) {
	root := writeSampleRegistry(t, "\"users\"\n{\n\t\"76561198000000000\"\n\t{\n\t}\n}\n")
	if _, err := readRegistryVDF(root); err == nil {
		t.Fatal("expected a refusal: writing a Steam hive into loginusers.vdf would be inventing state")
	}
}

func TestRegistryVDF_Delete(t *testing.T) {
	root := writeSampleRegistry(t, sampleRegistryVDF)
	reg, err := readRegistryVDF(root)
	if err != nil {
		t.Fatal(err)
	}
	if !reg.Delete("AutoLoginUser") {
		t.Fatal("Delete reported the key was absent")
	}
	if reg.Delete("AutoLoginUser") {
		t.Fatal("Delete reported a second removal of the same key")
	}
	if err := reg.Write(root); err != nil {
		t.Fatal(err)
	}
	reread, _ := readRegistryVDF(root)
	if got := reread.Get("AutoLoginUser"); got != "" {
		t.Fatalf("AutoLoginUser = %q after delete", got)
	}
	if got := reread.Get("Rate"); got != "30000" {
		t.Fatalf("Delete took a sibling with it: Rate = %q", got)
	}
}

// TestSteamPIDFromRegistry_ZeroIsNotRunning pins that a clean exit reads as "not running".
func TestSteamPIDFromRegistry_ZeroIsNotRunning(t *testing.T) {
	root := writeSampleRegistry(t, sampleRegistryVDF)
	if pid, ok := steamPIDFromRegistry(root); ok {
		t.Fatalf("SteamPID 0 read as running (pid %q)", pid)
	}

	running := strings.Replace(sampleRegistryVDF, `"SteamPID"		"0"`, `"SteamPID"		"4242"`, 1)
	root = writeSampleRegistry(t, running)
	pid, ok := steamPIDFromRegistry(root)
	if !ok || pid != "4242" {
		t.Fatalf("SteamPID = %q, ok = %v, want 4242/true", pid, ok)
	}
}

// TestRegistryVDFPath pins the location, which the backend and Advanced Cleaning both assume.
func TestRegistryVDFPath(t *testing.T) {
	if got, want := registryVDFPath("/x/Steam"), filepath.Join("/x/Steam", "registry.vdf"); got != want {
		t.Fatalf("registryVDFPath = %q, want %q", got, want)
	}
}
