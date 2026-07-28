package cli

import (
	"log/slog"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestParsePassthroughLaunchArgs(t *testing.T) {
	idx := &PlatformIndex{
		Names: map[string]string{"steam": "Steam"},
	}
	argv := []string{"+s:76561197960287930", "-dev", "-x", "-y"}
	p, err := Parse(argv, idx)
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != KindSwapSteam {
		t.Fatalf("kind: got %v want KindSwapSteam", p.Kind)
	}
	if p.SteamID64 != "76561197960287930" {
		t.Fatalf("steam id: got %q", p.SteamID64)
	}
	want := []string{"-dev", "-x", "-y"}
	if !reflect.DeepEqual(p.PassthroughLaunchArgs, want) {
		t.Fatalf("passthrough: got %#v want %#v", p.PassthroughLaunchArgs, want)
	}
}

func TestParseRunAppIDSteam(t *testing.T) {
	idx := &PlatformIndex{Names: map[string]string{"steam": "Steam"}}
	p, err := Parse([]string{"+s:76561197960287930", "--run-appid=945360"}, idx)
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != KindSwapSteam || p.RunAppID != "945360" || p.RunShortcutFile != "" {
		t.Fatalf("got %#v", p)
	}
}

func TestParseRunAppIDRequiresSteam(t *testing.T) {
	idx := &PlatformIndex{
		Names:             map[string]string{"steam": "Steam", "epic": "Epic Games"},
		FirstIdentifier:   map[string]string{"e": "Epic Games"},
		IdentifierAliases: map[string]string{"e": "Epic Games", "epic": "Epic Games"},
	}
	_, err := Parse([]string{"+e:someid", "--run-appid=123"}, idx)
	if err == nil || !strings.Contains(err.Error(), "--run-appid requires") {
		t.Fatalf("want error, got %v", err)
	}
}

func TestParsePlusTokenAcceptsAnyIdentifierAlias(t *testing.T) {
	idx := &PlatformIndex{
		Names:             map[string]string{"steam": "Steam"},
		FirstIdentifier:   map[string]string{"s": "Steam"},
		IdentifierAliases: map[string]string{"s": "Steam", "steam": "Steam", "valve": "Steam"},
	}
	p, err := Parse([]string{"+valve:76561197960287930"}, idx)
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != KindSwapSteam || p.PlatformKey != "Steam" || p.SteamID64 != "76561197960287930" {
		t.Fatalf("got %#v", p)
	}
}

func TestParseOpenPageAcceptsIdentifierAlias(t *testing.T) {
	idx := &PlatformIndex{
		Names:             map[string]string{"steam": "Steam"},
		IdentifierAliases: map[string]string{"steam": "Steam", "valve": "Steam"},
	}
	p, err := Parse([]string{"--page=valve"}, idx)
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != KindOpenPage || p.OpenPage != "Steam" {
		t.Fatalf("got %#v", p)
	}
}

func TestParseRunShortcutWithSteam(t *testing.T) {
	idx := &PlatformIndex{Names: map[string]string{"steam": "Steam"}}
	enc := url.QueryEscape("My Game.lnk")
	p, err := Parse([]string{"+s:76561197960287930", "--run-shortcut=" + enc}, idx)
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != KindSwapSteam || p.RunShortcutFile != "My Game.lnk" || p.RunAppID != "" {
		t.Fatalf("got %#v", p)
	}
}

func TestParseRunShortcutMutuallyExclusive(t *testing.T) {
	idx := &PlatformIndex{Names: map[string]string{"steam": "Steam"}}
	_, err := Parse([]string{"+s:76561197960287930", "--run-appid=1", "--run-shortcut=x.lnk"}, idx)
	if err == nil || !strings.Contains(err.Error(), "cannot combine") {
		t.Fatalf("want error, got %v", err)
	}
}

func TestParseOpenPageFlag(t *testing.T) {
	idx := &PlatformIndex{
		Names: map[string]string{"steam": "Steam"},
	}
	p, err := Parse([]string{"--page=steam"}, idx)
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != KindOpenPage || p.OpenPage != "Steam" {
		t.Fatalf("got %#v", p)
	}
	// Post-redesign the account list *is* the home route, so --page=<platform> lands
	// there rather than on a per-platform page that no longer exists.
	if j := p.RouteJSONForOpenPage(); j != `{"page":"home"}` {
		t.Fatalf("route json: %q", j)
	}
}

func TestParseLogLevel(t *testing.T) {
	idx := &PlatformIndex{Names: map[string]string{"steam": "Steam"}}
	p, err := Parse([]string{"--log-level=warn", "Steam"}, idx)
	if err != nil {
		t.Fatal(err)
	}
	if !p.LogLevelSet || p.LogLevel != slog.LevelWarn {
		t.Fatalf("log level: got set=%v level=%v", p.LogLevelSet, p.LogLevel)
	}
	if p.EffectiveSlogLevel() != slog.LevelWarn {
		t.Fatalf("effective: %v", p.EffectiveSlogLevel())
	}
}

func TestParseVerboseSetsDebug(t *testing.T) {
	p, err := Parse([]string{"-v"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.Verbose != true || p.EffectiveSlogLevel() != slog.LevelDebug {
		t.Fatalf("got %#v effective=%v", p, p.EffectiveSlogLevel())
	}
}

func TestParseLogLevelOverridesVerbose(t *testing.T) {
	p, err := Parse([]string{"-v", "--log-level=error"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.EffectiveSlogLevel() != slog.LevelError {
		t.Fatalf("want error, got %v", p.EffectiveSlogLevel())
	}
}

func TestParseDuplicateLogLevel(t *testing.T) {
	_, err := Parse([]string{"--log-level=info", "--log-level=debug"}, nil)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("want duplicate error, got %v", err)
	}
}

func TestParseSealRoster(t *testing.T) {
	// The passphrase must never be parseable from argv — it travels in the stdin document —
	// so the only thing this flag carries is an optional output path.
	cases := []struct {
		argv    []string
		wantOut string
	}{
		{[]string{"--seal-roster"}, ""},
		{[]string{"-seal-roster"}, ""},
		{[]string{"--seal-roster=/tmp/roster.ssroster"}, "/tmp/roster.ssroster"},
	}
	for _, tc := range cases {
		p, err := Parse(tc.argv, nil)
		if err != nil {
			t.Fatalf("Parse(%v): %v", tc.argv, err)
		}
		if p.Kind != KindSealRoster {
			t.Errorf("Parse(%v).Kind = %v, want KindSealRoster", tc.argv, p.Kind)
		}
		if p.SealRosterOut != tc.wantOut {
			t.Errorf("Parse(%v).SealRosterOut = %q, want %q", tc.argv, p.SealRosterOut, tc.wantOut)
		}
		if !p.IsStdioCommand() {
			t.Errorf("Parse(%v).IsStdioCommand() = false; it must run before the singleton", tc.argv)
		}
		if p.IsListCommand() {
			t.Errorf("Parse(%v).IsListCommand() = true; it reads no account data", tc.argv)
		}
	}
}

func TestParseSealRoster_IsExclusiveWithASwap(t *testing.T) {
	if _, err := Parse([]string{"--seal-roster", "+s:76561198000000001"}, nil); err == nil {
		t.Fatal("sealing a roster and swapping an account in one invocation should be refused")
	}
}
