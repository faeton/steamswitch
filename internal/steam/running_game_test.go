package steam

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPickRunningApp(t *testing.T) {
	known := []string{"570", "730"}
	cases := []struct {
		name    string
		running map[string]bool
		steam   string
		want    string
	}{
		{"nothing running", nil, "", ""},
		{"steam records zero for none", nil, "0", ""},
		{"known game by process", map[string]bool{"730": true}, "", "730"},
		{"known game wins over steam's record", map[string]bool{"570": true}, "440", "570"},
		{"unlisted game from steam's record", nil, "440", "440"},
		{"whitespace is trimmed", nil, " 440 ", "440"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := pickRunningApp(known,
				func(id string) bool { return c.running[id] },
				func() string { return c.steam })
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestGameDisplayName(t *testing.T) {
	root := t.TempDir()
	apps := filepath.Join(root, "steamapps")
	if err := os.MkdirAll(apps, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "\"AppState\"\n{\n\t\"appid\"\t\t\"440\"\n\t\"name\"\t\t\"Team Fortress 2\"\n}\n"
	if err := os.WriteFile(filepath.Join(apps, "appmanifest_440.acf"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{
		DotaAppID: "Dota 2",
		"440":     "Team Fortress 2",
		"999":     "Steam app 999",
	} {
		if got := gameDisplayName(root, id); got != want {
			t.Errorf("app %s: got %q, want %q", id, got, want)
		}
	}
}

func TestExitGraceFor(t *testing.T) {
	base := 10 * time.Second
	if got := exitGraceFor(base, false); got != base {
		t.Fatalf("no game: got %v", got)
	}
	if got := exitGraceFor(base, true); got != gameExitGrace {
		t.Fatalf("game running: got %v", got)
	}
	if long := time.Minute; exitGraceFor(long, true) != long {
		t.Fatal("a game must never shorten the grace")
	}
}
