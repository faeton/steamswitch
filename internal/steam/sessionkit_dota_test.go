package steam

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"steamswitch/internal/sessionkit"
)

// Two real SteamID64s so `FormatsFromID64` produces usable id32 folder names.
const (
	dotaHomeID   = "76561198000000001"
	dotaSharedID = "76561198000000002"
)

func writeDotaFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func id32For(t *testing.T, id64 string) string {
	t.Helper()
	f, err := FormatsFromID64(id64)
	if err != nil {
		t.Fatalf("id formats for %s: %v", id64, err)
	}
	return f.ID32
}

// dotaTestRoot builds a Steam tree with Dota installed and both accounts populated.
func dotaTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	// resolveDotaGlobalCfg walks the libraries; the default steamapps dir is enough here.
	writeDotaFile(t, filepath.Join(root, "steamapps", dotaGlobalCfgRelPath, "autoexec.cfg"), "bind x")
	writeDotaFile(t, filepath.Join(root, "steamapps", "appmanifest_"+DotaAppID+".acf"),
		"\"AppState\"\n{\n\t\"appid\"\t\"570\"\n\t\"buildid\"\t\"12345\"\n}\n")

	home := id32For(t, dotaHomeID)
	shared := id32For(t, dotaSharedID)
	writeDotaFile(t, filepath.Join(root, "userdata", home, DotaAppID, "local", "cfg", "my.cfg"), "mine")
	writeDotaFile(t, filepath.Join(root, "userdata", home, DotaAppID, "remote", "grid.json"), "my-grid")
	writeDotaFile(t, filepath.Join(root, "userdata", shared, DotaAppID, "local", "cfg", "their.cfg"), "theirs")
	writeDotaFile(t, filepath.Join(root, "userdata", shared, DotaAppID, "remote", "grid.json"), "their-grid")
	return root
}

func TestDotaModule_DetectsInstallAndFingerprintsTheBuild(t *testing.T) {
	root := dotaTestRoot(t)
	det, err := DotaModule{}.Detect(context.Background(), sessionkit.DetectRequest{SteamRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if !det.Installed || !det.Ready {
		t.Fatalf("detect = %+v, want installed and ready", det)
	}
	if det.Fingerprint == "" {
		t.Fatal("no fingerprint, so a game update could never auto-pause the module")
	}
}

func TestDotaModule_FingerprintChangesWhenTheGameUpdates(t *testing.T) {
	// REDESIGN.md §5: after a game update invalidates paths the module auto-pauses until a
	// self-test passes. That needs the fingerprint to actually move on a new build id.
	root := dotaTestRoot(t)
	before, err := DotaModule{}.Detect(context.Background(), sessionkit.DetectRequest{SteamRoot: root})
	if err != nil {
		t.Fatal(err)
	}

	writeDotaFile(t, filepath.Join(root, "steamapps", "appmanifest_"+DotaAppID+".acf"),
		"\"AppState\"\n{\n\t\"appid\"\t\"570\"\n\t\"buildid\"\t\"99999\"\n}\n")

	after, err := DotaModule{}.Detect(context.Background(), sessionkit.DetectRequest{SteamRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if before.Fingerprint == after.Fingerprint {
		t.Fatal("fingerprint survived a build id change, so auto-pause would never trigger")
	}
}

func TestDotaModule_FingerprintIsStableAcrossConfigEdits(t *testing.T) {
	// The opposite failure: fingerprinting config *contents* would pause the module every
	// time the user changed a setting.
	root := dotaTestRoot(t)
	before, _ := DotaModule{}.Detect(context.Background(), sessionkit.DetectRequest{SteamRoot: root})

	home := id32For(t, dotaHomeID)
	writeDotaFile(t, filepath.Join(root, "userdata", home, DotaAppID, "local", "cfg", "my.cfg"), "changed")

	after, _ := DotaModule{}.Detect(context.Background(), sessionkit.DetectRequest{SteamRoot: root})
	if before.Fingerprint != after.Fingerprint {
		t.Fatal("editing a config changed the fingerprint; the module would pause constantly")
	}
}

func TestDotaModule_GlobalCfgNeverTravelsWithAKit(t *testing.T) {
	// It is one machine-wide folder shared by every account, so copying it between accounts
	// is a no-op at best and a cross-account clobber at worst (REDESIGN.md §5).
	root := dotaTestRoot(t)
	plan, err := DotaModule{}.Preflight(context.Background(), sessionkit.PreflightRequest{
		Operation: sessionkit.OperationEnter,
		Source:    sessionkit.AccountRef{SteamID64: dotaHomeID},
		Target:    sessionkit.AccountRef{SteamID64: dotaSharedID},
		SteamRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range plan.Parts {
		if p.ID == DotaPartGlobalCfg {
			t.Fatal("globalcfg was planned into a kit")
		}
	}
	if len(plan.Parts) != 2 {
		t.Fatalf("parts = %v, want local + remote", plan.PartIDs())
	}
}

func TestDotaModule_SkipsPartsTheSourceDoesNotHave(t *testing.T) {
	// An empty Home `remote/` must not be carried across: applying it would blank the other
	// person's hero grids and report that as a successful kit.
	root := dotaTestRoot(t)
	home := id32For(t, dotaHomeID)
	if err := os.RemoveAll(filepath.Join(root, "userdata", home, DotaAppID, "remote")); err != nil {
		t.Fatal(err)
	}

	plan, err := DotaModule{}.Preflight(context.Background(), sessionkit.PreflightRequest{
		Operation: sessionkit.OperationEnter,
		Source:    sessionkit.AccountRef{SteamID64: dotaHomeID},
		Target:    sessionkit.AccountRef{SteamID64: dotaSharedID},
		SteamRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.PartIDs(); len(got) != 1 || got[0] != DotaPartLocal {
		t.Fatalf("parts = %v, want only %q", got, DotaPartLocal)
	}
}

func TestDotaModule_RemoteIsClassifiedAsCloudRisk(t *testing.T) {
	// The status strip has to disclose that Steam Cloud can undo a write to `remote/`.
	root := dotaTestRoot(t)
	plan, err := DotaModule{}.Preflight(context.Background(), sessionkit.PreflightRequest{
		Operation: sessionkit.OperationEnter,
		Source:    sessionkit.AccountRef{SteamID64: dotaHomeID},
		Target:    sessionkit.AccountRef{SteamID64: dotaSharedID},
		SteamRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.HasCloudRisk() {
		t.Fatal("a plan including remote/ did not report cloud risk")
	}
	for _, p := range plan.Parts {
		if p.ID == DotaPartLocal && p.Risk != sessionkit.PartLocalSafe {
			t.Fatalf("local risk = %q, want local-safe", p.Risk)
		}
	}
}

func TestDotaModule_ScratchAnchorSharesTheAccountsVolume(t *testing.T) {
	// The anchor has to be inside userdata/<id32>/570 so staging → live is a rename. It must
	// also stay out of `remote/`, which is the only subtree Steam Cloud uploads.
	root := dotaTestRoot(t)
	anchor, ok := DotaModule{}.ScratchAnchor(root, sessionkit.AccountRef{SteamID64: dotaSharedID})
	if !ok {
		t.Fatal("no anchor reported")
	}
	want := filepath.Join(root, "userdata", id32For(t, dotaSharedID), DotaAppID)
	if anchor != want {
		t.Fatalf("anchor = %q, want %q", anchor, want)
	}
	if strings.Contains(anchor, string(filepath.Separator)+"remote") {
		t.Fatal("anchor sits inside the Steam Cloud-synced remote/ subtree")
	}
}

func TestDotaModule_ScratchAnchorRefusesABadAccount(t *testing.T) {
	if _, ok := (DotaModule{}).ScratchAnchor("/steam", sessionkit.AccountRef{SteamID64: "nonsense"}); ok {
		t.Fatal("reported an anchor for an unparseable account id")
	}
}

func TestDotaModule_SnapshotCapturesAndHashesEveryPlannedPart(t *testing.T) {
	root := dotaTestRoot(t)
	m := DotaModule{}
	plan, err := m.Preflight(context.Background(), sessionkit.PreflightRequest{
		Operation: sessionkit.OperationEnter,
		Source:    sessionkit.AccountRef{SteamID64: dotaHomeID},
		Target:    sessionkit.AccountRef{SteamID64: dotaSharedID},
		SteamRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "snap")
	res, err := m.Snapshot(context.Background(), sessionkit.SnapshotRequest{
		Account:     sessionkit.AccountRef{SteamID64: dotaSharedID},
		Plan:        plan,
		SteamRoot:   root,
		Destination: dest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.CapturedParts) != 2 {
		t.Fatalf("captured = %v, want both parts", res.CapturedParts)
	}
	data, err := os.ReadFile(filepath.Join(dest, DotaPartLocal, "cfg", "their.cfg"))
	if err != nil || string(data) != "theirs" {
		t.Fatalf("snapshot payload = %q (%v), want the shared account's file", data, err)
	}
	for _, id := range res.CapturedParts {
		if res.Manifest.Parts[id].Digest == "" {
			t.Fatalf("part %q has no digest, so a later restore could not be verified", id)
		}
	}
}

func TestDotaModule_ApplyRefusesWithoutAJournal(t *testing.T) {
	// A module that wrote without journalling would leave recovery unable to classify a
	// crash, so this is a programming error rather than something to paper over.
	root := dotaTestRoot(t)
	_, err := DotaModule{}.Apply(context.Background(), sessionkit.ApplyRequest{
		Plan: sessionkit.ModulePlan{
			ModuleID: DotaModuleID,
			Target:   sessionkit.AccountRef{SteamID64: dotaSharedID},
			Parts:    kitParts(),
		},
		SteamRoot:    root,
		PayloadPath:  t.TempDir(),
		StageRoot:    t.TempDir(),
		RollbackRoot: t.TempDir(),
	})
	if err == nil {
		t.Fatal("Apply accepted a nil journal callback")
	}
}

func TestExtractACFValue(t *testing.T) {
	acf := "\"AppState\"\n{\n\t\"appid\"\t\"570\"\n\t\"buildid\"\t\"12345\"\n}\n"
	if got := extractACFValue(acf, "buildid"); got != "12345" {
		t.Fatalf("buildid = %q, want 12345", got)
	}
	if got := extractACFValue(acf, "missing"); got != "" {
		t.Fatalf("missing key = %q, want empty", got)
	}
}
