package steam

import (
	"errors"
	"testing"

	"steamswitch/internal/sessionkit"
)

// The rule this guard enforces used to live only in Accounts.svelte, so every switch that did
// not originate from a tile — tray menu, `steamswitch://` URL, desktop shortcut, `--run-appid`
// — skipped it. These cases are written from those entry points, not from the state machine.
func TestSwapAllowedBy(t *testing.T) {
	const shared = "76561198000000002"
	const other = "76561198000000003"

	cases := []struct {
		name   string
		state  sessionkit.RecoveryState
		target string
		want   error
	}{
		{
			name:   "nothing outstanding",
			state:  sessionkit.RecoveryState{Kind: sessionkit.RecoveryNone},
			target: other,
		},
		{
			// The case that was silently corrupting state: a tray click while someone else's
			// account is carrying this machine's kit.
			name:   "kit live elsewhere, switching away",
			state:  sessionkit.RecoveryState{Kind: sessionkit.RecoveryKitActive, TargetSteamID64: shared},
			target: other,
			want:   ErrKitLeaveRequired,
		},
		{
			// A shortcut that says "log in as X and launch the game" while already on X is
			// not leaving the kit, and must not be blocked — that is the common case for
			// the shortcut feature existing at all.
			name:   "kit live on the account being selected",
			state:  sessionkit.RecoveryState{Kind: sessionkit.RecoveryKitActive, TargetSteamID64: shared},
			target: shared,
		},
		{
			name:   "id comparison ignores surrounding space",
			state:  sessionkit.RecoveryState{Kind: sessionkit.RecoveryKitActive, TargetSteamID64: " " + shared},
			target: shared + " ",
		},
		{
			name:   "interrupted transaction",
			state:  sessionkit.RecoveryState{Kind: sessionkit.RecoveryInterrupted, TargetSteamID64: shared},
			target: shared,
			want:   ErrKitRecoveryRequired,
		},
		{
			// Even switching to the account the dead transaction was about: the files are in
			// an unknown state, and a swap would launch Steam on top of them.
			name:   "external change blocks even the same account",
			state:  sessionkit.RecoveryState{Kind: sessionkit.RecoveryExternalChange, TargetSteamID64: shared},
			target: shared,
			want:   ErrKitRecoveryRequired,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := swapAllowedBy(tc.state, tc.target)
			if !errors.Is(err, tc.want) {
				t.Fatalf("swapAllowedBy = %v, want %v", err, tc.want)
			}
		})
	}
}

// A process that never built an engine — headless dispatch — must still be able to switch.
// Failing closed here would break `steamswitch +s:<id>` entirely.
func TestRequireNoActiveKit_AllowsWhenNoEngineExists(t *testing.T) {
	kitEngineMu.Lock()
	prev := kitEngine
	kitEngine = nil
	kitEngineMu.Unlock()
	t.Cleanup(func() {
		kitEngineMu.Lock()
		kitEngine = prev
		kitEngineMu.Unlock()
	})

	if err := requireNoActiveKit("76561198000000002"); err != nil {
		t.Fatalf("requireNoActiveKit with no engine = %v, want nil", err)
	}
}
