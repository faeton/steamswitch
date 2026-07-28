package platform

import (
	"encoding/json"
	"testing"
)

func TestSanitizeAutoLockMinutes(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		// 0 is a real answer ("never"), not an absent value, so it must survive verbatim.
		{"zero means never", 0, 0},
		{"negative collapses to never", -5, 0},
		{"ordinary value passes through", 5, 5},
		{"the ceiling passes through", MaxVaultAutoLockMinutes, MaxVaultAutoLockMinutes},
		// A hand-edited "lock after three days" is a lock that never fires; clamping it keeps
		// the settings UI from advertising a protection that is not there.
		{"absurd value clamps to the ceiling", 100000, MaxVaultAutoLockMinutes},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeAutoLockMinutes(tc.in); got != tc.want {
				t.Fatalf("sanitizeAutoLockMinutes(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestSanitizeRevealSeconds(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		// Unlike auto-lock there is no "never": a secret that stays visible until dismissed is
		// exactly the failure the setting exists to prevent, so 0 means "use the default".
		{"zero means the default", 0, DefaultVaultRevealSeconds},
		{"negative means the default", -1, DefaultVaultRevealSeconds},
		{"below the floor clamps up", 1, MinVaultRevealSeconds},
		{"ordinary value passes through", 8, 8},
		{"above the ceiling clamps down", 9999, MaxVaultRevealSeconds},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeRevealSeconds(tc.in); got != tc.want {
				t.Fatalf("sanitizeRevealSeconds(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeAppSettings_VaultDefaults(t *testing.T) {
	// A settings file written before these keys existed must land on the safe side of each:
	// lock after five minutes, hide after eight seconds, clear the clipboard.
	var s AppSettings
	normalizeAppSettingsDefaults(&s, map[string]json.RawMessage{})

	if s.VaultAutoLockMinutes != DefaultVaultAutoLockMinutes {
		t.Errorf("VaultAutoLockMinutes = %d, want %d", s.VaultAutoLockMinutes, DefaultVaultAutoLockMinutes)
	}
	if s.VaultRevealSeconds != DefaultVaultRevealSeconds {
		t.Errorf("VaultRevealSeconds = %d, want %d", s.VaultRevealSeconds, DefaultVaultRevealSeconds)
	}
	if !s.VaultClearClipboard {
		t.Error("VaultClearClipboard = false, want true for an upgraded settings file")
	}
}

func TestNormalizeAppSettings_VaultExplicitNeverSurvives(t *testing.T) {
	// The distinction that matters: a user who explicitly chose "never" must not be silently
	// put back on a five-minute timer at every load, which is what a bare zero-check would do.
	s := AppSettings{VaultAutoLockMinutes: 0, VaultClearClipboard: false}
	normalizeAppSettingsDefaults(&s, map[string]json.RawMessage{
		"vaultAutoLockMinutes": json.RawMessage("0"),
		"vaultClearClipboard":  json.RawMessage("false"),
	})

	if s.VaultAutoLockMinutes != 0 {
		t.Errorf("VaultAutoLockMinutes = %d, want 0 (explicit never)", s.VaultAutoLockMinutes)
	}
	if s.VaultClearClipboard {
		t.Error("VaultClearClipboard = true, want the explicit false to round-trip")
	}
}
