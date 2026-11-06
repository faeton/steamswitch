package steam

import (
	"os"
	"path/filepath"
	"testing"
)

// Valve removed the public app list this used to download, and its replacement needs a key. Steam's
// own manifests name everything installed, without the network.
func TestBuildInstalledGamesList_NamesComeFromTheManifests(t *testing.T) {
	root := t.TempDir()
	apps := filepath.Join(root, "steamapps")
	if err := os.MkdirAll(apps, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(id, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(apps, "appmanifest_"+id+".acf"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("440", "\"AppState\"\n{\n\t\"appid\"\t\t\"440\"\n\t\"name\"\t\t\"Team Fortress 2\"\n}\n")
	write("999", "\"AppState\"\n{\n\t\"appid\"\t\t\"999\"\n}\n")

	got, err := BuildInstalledGamesList(root)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, g := range got {
		names[g.AppID] = g.Name
	}
	if names["440"] != "Team Fortress 2" {
		t.Errorf("440 = %q", names["440"])
	}
	if names["999"] != "App 999" {
		t.Errorf("999 = %q, want the fallback", names["999"])
	}
}
