package steam

import (
	"path/filepath"
	"testing"

	"steamswitch/internal/paths"
	"steamswitch/internal/vault"
)

func TestClampAutoRefreshInterval(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, 0},
		{-5, 0},
		{1, SteamAutoRefreshMinMinutes},
		{SteamAutoRefreshMinMinutes - 1, SteamAutoRefreshMinMinutes},
		{SteamAutoRefreshMinMinutes, SteamAutoRefreshMinMinutes},
		{60, 60},
	}
	for _, c := range cases {
		if got := ClampAutoRefreshInterval(c.in); got != c.want {
			t.Errorf("ClampAutoRefreshInterval(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

// The vault's scheduled check logs in to a Steam account, so what the settings layer lets
// through matters more than for the avatar refresh: off has to be the value that survives,
// and a too-eager cadence has to be raised rather than honoured.
func TestClampVaultDeepCheckDays(t *testing.T) {
	floor := int(vault.MinDeepCheckInterval.Hours() / 24)
	cases := []struct {
		in, want int
	}{
		{0, 0},
		{-3, 0},
		{1, floor},
		{floor, floor},
		{14, 14},
	}
	for _, c := range cases {
		if got := ClampVaultDeepCheckDays(c.in); got != c.want {
			t.Errorf("ClampVaultDeepCheckDays(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestRefreshPresets_AreLoginSafeAndUseRealActions(t *testing.T) {
	paths.ResetForTest(filepath.Join(t.TempDir(), "data"))
	valid := map[string]bool{}
	var s SteamService
	items, err := s.AdvancedClearingItems()
	if err != nil {
		t.Fatal(err)
	}
	loginAction := map[string]bool{}
	for _, it := range items {
		valid[it.ID] = true
		if it.Category == "login" {
			loginAction[it.ID] = true
		}
	}

	for id, preset := range refreshPresets {
		if len(preset.Actions) == 0 {
			t.Errorf("preset %q has no actions", id)
		}
		for _, a := range preset.Actions {
			if !valid[a] {
				t.Errorf("preset %q references unknown action %q", id, a)
			}
			// Both shipped presets promise not to sign anyone out.
			if loginAction[a] {
				t.Errorf("preset %q includes login-affecting action %q but TouchesLogin=%v", id, a, preset.TouchesLogin)
			}
		}
	}
}

func TestRunRefreshPreset_RejectsUnknownID(t *testing.T) {
	paths.ResetForTest(filepath.Join(t.TempDir(), "data"))
	var s SteamService
	if _, err := s.RunRefreshPreset("nope"); err != errUnknownRefreshPreset {
		t.Fatalf("err = %v, want errUnknownRefreshPreset", err)
	}
}
