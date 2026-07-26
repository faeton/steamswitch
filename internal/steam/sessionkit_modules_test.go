package steam

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"steamswitch/internal/sessionkit"
)

func TestRegisteredModules_HaveDistinctStableIDs(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range registeredModules() {
		id := m.ID()
		if id == "" {
			t.Fatalf("%T has an empty ID; journals and snapshot directories are keyed by it", m)
		}
		if seen[id] {
			t.Fatalf("duplicate module id %q — two modules would share one journal namespace", id)
		}
		seen[id] = true
		if m.DisplayName() == "" {
			t.Fatalf("module %q has no display name", id)
		}
	}
	if !seen[DotaModuleID] || !seen[CS2ModuleID] {
		t.Fatalf("expected both dota2 and cs2 to be registered, got %v", seen)
	}
}

// TestPausableModule_CarriesTheOptionalInterfaces is the trap this wrapper exists to avoid.
// The engine type-asserts for both; losing either silently downgrades recovery — without
// LivePath a part cannot be rolled back, and without ScratchAnchor the transaction is refused.
func TestPausableModule_CarriesTheOptionalInterfaces(t *testing.T) {
	for _, m := range engineModules() {
		if _, ok := m.(sessionkit.LivePathResolver); !ok {
			t.Fatalf("%s lost LivePathResolver through the pause wrapper", m.ID())
		}
		if _, ok := m.(sessionkit.ScratchAnchor); !ok {
			t.Fatalf("%s lost ScratchAnchor through the pause wrapper", m.ID())
		}
	}
}

func TestAutoPauseNeeded(t *testing.T) {
	installed := func(fp string) sessionkit.Detection {
		return sessionkit.Detection{Installed: true, Ready: true, Fingerprint: fp}
	}

	cases := []struct {
		name  string
		state GameModuleState
		det   sessionkit.Detection
		want  bool
	}{
		{
			name:  "layout moved away from the confirmed one",
			state: GameModuleState{KnownGoodFingerprint: "aaa"},
			det:   installed("bbb"),
			want:  true,
		},
		{
			name:  "layout unchanged",
			state: GameModuleState{KnownGoodFingerprint: "aaa"},
			det:   installed("aaa"),
			want:  false,
		},
		{
			// A module that has never passed a self-test has made no claim to have been
			// verified. Pausing it would make a fresh install look like a fault.
			name:  "never self-tested",
			state: GameModuleState{},
			det:   installed("bbb"),
			want:  false,
		},
		{
			name:  "already paused",
			state: GameModuleState{Paused: true, KnownGoodFingerprint: "aaa"},
			det:   installed("bbb"),
			want:  false,
		},
		{
			// Detect already reports this. Pausing on top would leave the module paused
			// after a reinstall, with no cause the user can see.
			name:  "game uninstalled",
			state: GameModuleState{KnownGoodFingerprint: "aaa"},
			det:   sessionkit.Detection{Installed: false},
			want:  false,
		},
		{
			name:  "fingerprint unavailable",
			state: GameModuleState{KnownGoodFingerprint: "aaa"},
			det:   sessionkit.Detection{Installed: true, Ready: true},
			want:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := autoPauseNeeded(tc.state, tc.det); got != tc.want {
				t.Fatalf("autoPauseNeeded = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGameModuleStatus_ActiveRequiresReadyAndUnpaused(t *testing.T) {
	ready := sessionkit.Detection{Installed: true, Ready: true, Fingerprint: "aaa"}

	active := gameModuleStatus(DotaModule{}, ready, GameModuleState{})
	if !active.Active || active.Reason != "" {
		t.Fatalf("a ready, unpaused module = %+v, want active with no reason", active)
	}

	paused := gameModuleStatus(DotaModule{}, ready, GameModuleState{Paused: true})
	if paused.Active {
		t.Fatal("a paused module reported itself active")
	}
	if paused.Reason != "Kit_Module_Paused" {
		t.Fatalf("reason = %q, want Kit_Module_Paused", paused.Reason)
	}

	auto := gameModuleStatus(DotaModule{}, ready, GameModuleState{Paused: true, PausedByFingerprintChange: true})
	if auto.Reason != "Kit_Module_PausedByChange" {
		t.Fatalf("reason = %q — the UI offers a self-test only for the automatic case", auto.Reason)
	}

	// A module that detects but is not ready is not active either, and keeps its own reason.
	notReady := sessionkit.Detection{Installed: true, Reason: "Toast_Kit_ModuleNotEnabled"}
	off := gameModuleStatus(CS2Module{}, notReady, GameModuleState{})
	if off.Active {
		t.Fatal("a not-ready module reported itself active")
	}
	if off.Reason != "Toast_Kit_ModuleNotEnabled" {
		t.Fatalf("reason = %q, want the module's own", off.Reason)
	}
}

// TestCS2Module_DetectsHonestlyAndRefusesToWrite pins the whole point of a disabled module:
// it must not claim the game is missing, and it must not silently no-op. A no-op would have
// the engine report a kit applied that never was.
func TestCS2Module_DetectsHonestlyAndRefusesToWrite(t *testing.T) {
	ctx := context.Background()
	m := CS2Module{}

	// A tree with the cfg folder present is "installed" even though the module will not run.
	root := t.TempDir()
	mkdirAllT(t, filepath.Join(root, "steamapps", cs2GlobalCfgRelPath))

	det, err := m.Detect(ctx, sessionkit.DetectRequest{SteamRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if !det.Installed {
		t.Fatal("CS2 reported not installed with its cfg folder present — the card would be a lie")
	}
	if det.Ready {
		t.Fatal("CS2 reported ready; it has no verified write path yet")
	}
	if det.Reason != "Toast_Kit_ModuleNotEnabled" {
		t.Fatalf("reason = %q, want Toast_Kit_ModuleNotEnabled", det.Reason)
	}
	if det.Fingerprint == "" {
		t.Fatal("no fingerprint, so auto-pause could never fire once this is enabled")
	}

	account := sessionkit.AccountRef{SteamID64: "76561198025067497"}
	if _, err := m.Preflight(ctx, sessionkit.PreflightRequest{Target: account, SteamRoot: root}); !errors.Is(err, ErrModuleNotEnabled) {
		t.Fatalf("Preflight err = %v, want ErrModuleNotEnabled — an empty plan would silently degrade the switch", err)
	}
	if _, err := m.Snapshot(ctx, sessionkit.SnapshotRequest{}); !errors.Is(err, ErrModuleNotEnabled) {
		t.Fatalf("Snapshot err = %v", err)
	}
	if _, err := m.Apply(ctx, sessionkit.ApplyRequest{}); !errors.Is(err, ErrModuleNotEnabled) {
		t.Fatalf("Apply err = %v", err)
	}
	if _, err := m.Restore(ctx, sessionkit.RestoreRequest{}); !errors.Is(err, ErrModuleNotEnabled) {
		t.Fatalf("Restore err = %v", err)
	}
	if _, err := m.Verify(ctx, sessionkit.VerifyRequest{}); !errors.Is(err, ErrModuleNotEnabled) {
		t.Fatalf("Verify err = %v", err)
	}
}

func TestCS2Module_PathsAreUnderItsOwnAppID(t *testing.T) {
	account := sessionkit.AccountRef{SteamID64: "76561198025067497"}
	root := filepath.Join("x", "Steam")

	local, ok := CS2Module{}.LivePath(root, account, DotaPartLocal)
	if !ok {
		t.Fatal("no local path")
	}
	if !containsPathSegment(local, CS2AppID) {
		t.Fatalf("local path %q is not under %s — CS2 would be writing into Dota's userdata", local, CS2AppID)
	}
	if containsPathSegment(local, DotaAppID) {
		t.Fatalf("local path %q is under Dota's app id", local)
	}

	anchor, ok := CS2Module{}.ScratchAnchor(root, account)
	if !ok {
		t.Fatal("no scratch anchor; the engine refuses a transaction without one")
	}
	if !containsPathSegment(anchor, CS2AppID) {
		t.Fatalf("scratch anchor %q is not on the same tree as the files it stages for", anchor)
	}
}

func TestCS2Module_UninstalledIsNotAnError(t *testing.T) {
	det, err := CS2Module{}.Detect(context.Background(), sessionkit.DetectRequest{SteamRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("an uninstalled game must not be an error: %v", err)
	}
	if det.Installed || det.Reason != "Toast_Kit_CS2NotInstalled" {
		t.Fatalf("det = %+v", det)
	}
}

func containsPathSegment(path, segment string) bool {
	return slices.Contains(strings.Split(filepath.ToSlash(path), "/"), segment)
}

func mkdirAllT(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
