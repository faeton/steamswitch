package steam

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"steamswitch/internal/paths"
)

const (
	cfgID64A = "76561198000000001"
	cfgID64B = "76561198000000002"
	cfgID32A = "39734273"
	cfgID32B = "39734274"
)

// cfgBackend sees Steam and game processes as the test says.
type cfgBackend struct {
	unsupportedBackend
	steamRunning bool
	running      map[string]bool
	marked       bool
	fromDir      string
}

func (b *cfgBackend) ProcessInspectionSupported() bool { return true }
func (b *cfgBackend) ProcessNames() []string           { return []string{"steam-client"} }
func (b *cfgBackend) IsRunning(name string) bool {
	if name == "steam-client" {
		return b.steamRunning
	}
	return b.running[name]
}
func (b *cfgBackend) AppMarkedRunning(string, string) bool { return b.marked }
func (b *cfgBackend) ProcessRunningFrom(dir string) bool {
	return b.fromDir != "" && strings.HasSuffix(dir, b.fromDir)
}

// cfgFixture points the service at a temp Steam root through its settings.
func cfgFixture(t *testing.T) (*SteamService, string, *cfgBackend) {
	t.Helper()
	base := t.TempDir()
	paths.ResetForTest(filepath.Join(base, "data"))
	root := filepath.Join(base, "steam")
	writeFile(t, LoginUsersPath(root), fmt.Sprintf("\"users\"\n{\n\t\"%s\"\n\t{\n\t\t\"AccountName\"\t\t\"a\"\n\t}\n\t\"%s\"\n\t{\n\t\t\"AccountName\"\t\t\"b\"\n\t\t\"MostRecent\"\t\t\"1\"\n\t}\n}\n", cfgID64A, cfgID64B))
	b := &cfgBackend{running: map[string]bool{}}
	prev := backend
	backend = b
	t.Cleanup(func() { backend = prev })
	prevRoot := steamInstallRootOverride
	steamInstallRootOverride = root
	t.Cleanup(func() { steamInstallRootOverride = prevRoot })
	return &SteamService{}, root, b
}

func gamePath(root, id32, app string, rel ...string) string {
	return filepath.Join(append([]string{root, "userdata", id32, app}, rel...)...)
}

func TestGameCopy_BothDirectionsForAnyGame(t *testing.T) {
	for _, app := range []string{DotaAppID, CS2AppID, "12345"} {
		s, root, _ := cfgFixture(t)
		writeFile(t, gamePath(root, cfgID32A, app, "local", "a.cfg"), "from A")
		writeFile(t, gamePath(root, cfgID32B, app, "local", "b.cfg"), "from B")

		if _, err := s.CopyGameConfigBetween(app, cfgID64A, cfgID64B, []string{"local"}, false); err != nil {
			t.Fatalf("%s A to B: %v", app, err)
		}
		if got := readFile(t, gamePath(root, cfgID32B, app, "local", "a.cfg")); got != "from A" {
			t.Fatalf("%s: %q", app, got)
		}
		// Replace, not merge.
		if _, err := os.Stat(gamePath(root, cfgID32B, app, "local", "b.cfg")); !os.IsNotExist(err) {
			t.Fatalf("%s: destination's old file survived, that is a merge", app)
		}

		writeFile(t, gamePath(root, cfgID32B, app, "local", "new.cfg"), "from B now")
		if _, err := s.CopyGameConfigBetween(app, cfgID64B, cfgID64A, []string{"local"}, false); err != nil {
			t.Fatalf("%s B to A: %v", app, err)
		}
		if got := readFile(t, gamePath(root, cfgID32A, app, "local", "new.cfg")); got != "from B now" {
			t.Fatalf("%s reverse: %q", app, got)
		}
	}
}

func TestGameCopy_UndoRestoresWhatWasThere(t *testing.T) {
	s, root, _ := cfgFixture(t)
	writeFile(t, gamePath(root, cfgID32A, CS2AppID, "local", "cfg", "x.vcfg"), "A")
	writeFile(t, gamePath(root, cfgID32B, CS2AppID, "local", "cfg", "x.vcfg"), "B")
	res, err := s.CopyGameConfigBetween(CS2AppID, cfgID64A, cfgID64B, []string{"local"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.RevertSnapshotID == "" {
		t.Fatal("no revert point")
	}
	if _, err := s.UndoGameConfigCopy(CS2AppID, res.RevertSnapshotID, false); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, gamePath(root, cfgID32B, CS2AppID, "local", "cfg", "x.vcfg")); got != "B" {
		t.Fatalf("after undo %q", got)
	}
}

// The gap this closes: a destination with no config used to get no revert point at all.
func TestGameCopy_UndoRemovesWhatTheCopyCreated(t *testing.T) {
	s, root, _ := cfgFixture(t)
	writeFile(t, gamePath(root, cfgID32A, "12345", "remote", "s.dat"), "A")
	writeFile(t, gamePath(root, cfgID32A, "12345", "local", "l.dat"), "A")
	writeFile(t, gamePath(root, cfgID32B, "12345", "local", "l.dat"), "B")
	res, err := s.CopyGameConfigBetween("12345", cfgID64A, cfgID64B, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UndoGameConfigCopy("12345", res.RevertSnapshotID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(gamePath(root, cfgID32B, "12345", "remote")); !os.IsNotExist(err) {
		t.Fatal("remote/ the copy created is still there after undo")
	}
	if got := readFile(t, gamePath(root, cfgID32B, "12345", "local", "l.dat")); got != "B" {
		t.Fatalf("local after undo %q", got)
	}
}

func TestGameCopy_EmptySourceChangesNothingAndLeavesNoRevertPoint(t *testing.T) {
	s, root, _ := cfgFixture(t)
	writeFile(t, gamePath(root, cfgID32B, "12345", "local", "l.dat"), "B")
	if _, err := s.CopyGameConfigBetween("12345", cfgID64A, cfgID64B, nil, false); !errors.Is(err, errDotaNothingCopied) {
		t.Fatalf("err = %v", err)
	}
	snaps, err := s.ListGameConfigSnapshots("12345")
	if err != nil || len(snaps) != 0 {
		t.Fatalf("snaps %v err %v", snaps, err)
	}
	if got := readFile(t, gamePath(root, cfgID32B, "12345", "local", "l.dat")); got != "B" {
		t.Fatal("destination changed")
	}
}

func TestGameCopy_RefusesCloudWritesWhileSteamRunsUnlessAccepted(t *testing.T) {
	s, root, b := cfgFixture(t)
	b.steamRunning = true
	writeFile(t, gamePath(root, cfgID32A, DotaAppID, "remote", "r"), "A")
	if _, err := s.CopyGameConfigBetween(DotaAppID, cfgID64A, cfgID64B, []string{"remote"}, false); !errors.Is(err, ErrDotaSteamRunning) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(gamePath(root, cfgID32B, DotaAppID, "remote")); !os.IsNotExist(err) {
		t.Fatal("wrote despite the refusal")
	}
	res, err := s.CopyGameConfigBetween(DotaAppID, cfgID64A, cfgID64B, []string{"remote"}, true)
	if err != nil || !res.CloudFollowUp {
		t.Fatalf("accepted: %+v %v", res, err)
	}
}

// Dota's local/ is known not to sync. CS2's is described as synced, and unknown games get the
// cautious answer.
func TestGameCopy_LocalIsCloudRiskExceptWhereKnownNotToBe(t *testing.T) {
	s, root, b := cfgFixture(t)
	b.steamRunning = true
	for _, app := range []string{DotaAppID, CS2AppID, "12345"} {
		writeFile(t, gamePath(root, cfgID32A, app, "local", "l"), "A")
	}
	if _, err := s.CopyGameConfigBetween(DotaAppID, cfgID64A, cfgID64B, []string{"local"}, false); err != nil {
		t.Fatalf("dota local: %v", err)
	}
	for _, app := range []string{CS2AppID, "12345"} {
		if _, err := s.CopyGameConfigBetween(app, cfgID64A, cfgID64B, []string{"local"}, false); !errors.Is(err, ErrDotaSteamRunning) {
			t.Fatalf("%s local under running Steam: %v", app, err)
		}
	}
}

func TestGameCopy_RefusesWhileGameRunsEvenWithRiskAccepted(t *testing.T) {
	s, root, b := cfgFixture(t)
	writeFile(t, gamePath(root, cfgID32A, DotaAppID, "local", "l"), "A")
	dota := "dota2.exe"
	b.running[dota], b.running[strings.TrimSuffix(dota, ".exe")] = true, true
	if _, err := s.CopyGameConfigBetween(DotaAppID, cfgID64A, cfgID64B, []string{"local"}, true); !errors.Is(err, errGameRunning) {
		t.Fatalf("err = %v", err)
	}
}

func TestGameRunning_UnlistedGameIsCaughtBySteamsFlagOrInstallFolder(t *testing.T) {
	_, root, b := cfgFixture(t)
	writeFile(t, filepath.Join(root, "steamapps", "appmanifest_12345.acf"), "\"AppState\"\n{\n\t\"installdir\"\t\t\"Some Game\"\n}\n")

	b.steamRunning, b.marked = true, true
	if running, err := gameIsRunning(root, "12345"); err != nil || !running {
		t.Fatalf("flag with Steam up: %v %v", running, err)
	}
	// A killed client leaves the flag behind.
	b.steamRunning = false
	if running, _ := gameIsRunning(root, "12345"); running {
		t.Fatal("a stale flag with Steam closed read as running")
	}
	b.marked, b.fromDir = false, "Some Game"
	if running, _ := gameIsRunning(root, "12345"); !running {
		t.Fatal("process under the install folder not noticed")
	}
}

func TestGameCopy_CannotTellIsARefusal(t *testing.T) {
	s, root, _ := cfgFixture(t)
	writeFile(t, gamePath(root, cfgID32A, "12345", "local", "l"), "A")
	backend = unsupportedBackend{}
	if _, err := s.CopyGameConfigBetween("12345", cfgID64A, cfgID64B, []string{"local"}, true); !errors.Is(err, ErrSwitchingUnsupported) {
		t.Fatalf("err = %v", err)
	}
}

func TestGameCopy_ValidatesWhatTheFrontendSends(t *testing.T) {
	s, root, _ := cfgFixture(t)
	writeFile(t, gamePath(root, cfgID32A, "12345", "local", "l"), "A")
	bad := []struct{ name, app, src, dst string }{
		{"app traversal", "../../etc", cfgID64A, cfgID64B},
		{"app empty", "", cfgID64A, cfgID64B},
		{"app zero", "0", cfgID64A, cfgID64B},
		{"app unicode digits", "١٢٣", cfgID64A, cfgID64B},
		{"steam client settings", "7", cfgID64A, cfgID64B},
		{"screenshots", "760", cfgID64A, cfgID64B},
		{"source traversal", "12345", "../../x", cfgID64B},
		{"dest traversal", "12345", cfgID64A, "../../x"},
		{"dest empty", "12345", cfgID64A, ""},
		{"well-formed but not on this machine", "12345", cfgID64A, "76561198000000099"},
	}
	for _, c := range bad {
		if _, err := s.CopyGameConfigBetween(c.app, c.src, c.dst, []string{"local"}, false); err == nil {
			t.Errorf("%s: accepted", c.name)
		}
	}
	if _, err := s.CopyGameConfigBetween("12345", cfgID64A, " "+cfgID64A+" ", []string{"local"}, false); !errors.Is(err, errDotaSameAccount) {
		t.Fatalf("same account: %v", err)
	}
	if _, err := s.CopyGameConfigBetween("12345", cfgID64A, cfgID64B, []string{"saves"}, false); !errors.Is(err, errDotaNoParts) {
		t.Fatalf("unknown part: %v", err)
	}
	// Only games that have one may name the machine-wide folder.
	if _, err := normalizeGameParts(CS2AppID, []string{DotaPartGlobalCfg}); !errors.Is(err, errDotaNoParts) {
		t.Fatalf("cs2 globalcfg: %v", err)
	}
}

func TestGameCopy_NeverWritesTheMachineWideFolder(t *testing.T) {
	s, root, _ := cfgFixture(t)
	cfg := filepath.Join(root, "steamapps", "common", "dota 2 beta", "game", "dota", "cfg", "autoexec.cfg")
	writeFile(t, cfg, "machine")
	writeFile(t, gamePath(root, cfgID32A, DotaAppID, "local", "l"), "A")
	res, err := s.CopyDotaConfigBetween(cfgID64A, cfgID64B, DotaPartsAll, false)
	if err != nil {
		t.Fatal(err)
	}
	if !containsPart(res.SkippedParts, DotaPartGlobalCfg) {
		t.Fatalf("skipped = %v", res.SkippedParts)
	}
	if got := readFile(t, cfg); got != "machine" {
		t.Fatal("machine-wide cfg changed")
	}
}

func TestGameCopy_RemotecacheGoesWithACloudWrite(t *testing.T) {
	s, root, _ := cfgFixture(t)
	writeFile(t, gamePath(root, cfgID32A, "12345", "remote", "r"), "A")
	cache := gamePath(root, cfgID32B, "12345", "remotecache.vdf")
	writeFile(t, cache, "old inventory")
	if _, err := s.CopyGameConfigBetween("12345", cfgID64A, cfgID64B, []string{"remote"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Fatal("stale remotecache.vdf left in place")
	}
}

func TestGameSnapshots_AreKeptPerGameAndDotaStaysWhereItWas(t *testing.T) {
	s, root, _ := cfgFixture(t)
	writeFile(t, gamePath(root, cfgID32A, DotaAppID, "local", "l"), "A")
	writeFile(t, gamePath(root, cfgID32A, CS2AppID, "local", "l"), "A")
	dota, err := s.SnapshotGameConfig(DotaAppID, cfgID64A, "d", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SnapshotGameConfig(CS2AppID, cfgID64A, "c", "", nil); err != nil {
		t.Fatal(err)
	}
	dr, _ := paths.DataRoot()
	if _, err := os.Stat(filepath.Join(dr, "Backups", "Dota", dota.ID, "snapshot.json")); err != nil {
		t.Fatal("existing Dota libraries depend on this location")
	}
	list, err := s.ListGameConfigSnapshots(CS2AppID)
	if err != nil || len(list) != 1 || list[0].Label != "c" {
		t.Fatalf("cs2 list %v %v", list, err)
	}
	if err := s.DeleteGameConfigSnapshot(CS2AppID, dota.ID); err == nil {
		t.Fatal("deleted another game's snapshot")
	}
}

func TestGameConfigInfo_ReportsContentsAndWhatIsNotCopied(t *testing.T) {
	s, root, b := cfgFixture(t)
	writeFile(t, gamePath(root, cfgID32A, CS2AppID, "local", "cfg", "x"), "12345")
	writeFile(t, gamePath(root, cfgID32A, CS2AppID, "ac", "y"), "1")
	writeFile(t, gamePath(root, cfgID32A, CS2AppID, "remotecache.vdf"), "1")
	b.running["cs2"], b.running["cs2.exe"] = true, true
	info, err := s.GetGameConfigInfo(CS2AppID, []string{cfgID64A, cfgID64B})
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "Counter-Strike 2" || !info.GameRunning {
		t.Fatalf("info = %+v", info)
	}
	a := info.Accounts[0]
	if len(a.Parts) != 1 || a.Parts[0].ID != "local" || a.Parts[0].Files != 1 || a.Parts[0].SizeBytes != 5 {
		t.Fatalf("parts %+v", a.Parts)
	}
	if len(a.Other) != 1 || a.Other[0] != "ac" {
		t.Fatalf("other %v, want only ac", a.Other)
	}
	if len(info.Accounts[1].Parts) != 0 {
		t.Fatalf("B parts %+v", info.Accounts[1].Parts)
	}
	for _, p := range info.Parts {
		if p.ID == DotaPartGlobalCfg {
			t.Fatal("CS2 offered a machine-wide part")
		}
	}
}

func TestListConfigGames_DotaAndCS2FirstAndSteamsOwnFoldersHidden(t *testing.T) {
	s, root, _ := cfgFixture(t)
	for _, app := range []string{"12345", CS2AppID, DotaAppID, "7", "760"} {
		writeFile(t, gamePath(root, cfgID32A, app, "remote", "r"), "A")
	}
	writeFile(t, filepath.Join(root, "steamapps", "appmanifest_12345.acf"), "\"AppState\"\n{\n\t\"name\"\t\t\"Some Game\"\n}\n")
	writeFile(t, gamePath(root, cfgID32A, "999", "ac", "only"), "not config")
	games, err := s.ListConfigGames([]string{cfgID64A, cfgID64B})
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, g := range games {
		got = append(got, g.AppID+"="+g.Name)
	}
	want := "570=Dota 2,730=Counter-Strike 2,12345=Some Game"
	if strings.Join(got, ",") != want {
		t.Fatalf("got %v, want %s", got, want)
	}
}
