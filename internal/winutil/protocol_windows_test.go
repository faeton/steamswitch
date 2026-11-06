//go:build windows

package winutil

import (
	"testing"

	"golang.org/x/sys/windows/registry"
)

// Never the real key: a test run must not repoint the user's links.
func useTestClasses(t *testing.T) {
	t.Helper()
	const root = `Software\SteamSwitchProtocolTest`
	prev := protocolClassesKey
	protocolClassesKey = root + `\Classes`
	t.Cleanup(func() {
		// Before the key is put back, or this deletes the user's own registration.
		_ = UnregisterProtocol()
		_ = registry.DeleteKey(registry.CURRENT_USER, root+`\Classes`)
		_ = registry.DeleteKey(registry.CURRENT_USER, root)
		protocolClassesKey = prev
	})
}

func TestProtocol_RegistersPerUserWithAQuotedCommand(t *testing.T) {
	useTestClasses(t)
	exe := `C:\Program Files\Steam Switch\SteamSwitch.exe`
	if err := RegisterProtocol(exe); err != nil {
		t.Fatalf("register: %v", err)
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, protocolClassesKey+`\steamswitch`, registry.QUERY_VALUE)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer k.Close()
	if v, _, err := k.GetStringValue("URL Protocol"); err != nil || v != "" {
		t.Fatalf("URL Protocol = %q, %v; want an empty string value", v, err)
	}
	cmdKey, err := registry.OpenKey(registry.CURRENT_USER, protocolClassesKey+`\steamswitch\shell\open\command`, registry.QUERY_VALUE)
	if err != nil {
		t.Fatalf("open command: %v", err)
	}
	defer cmdKey.Close()
	want := `"` + exe + `" "%1"`
	if v, _, _ := cmdKey.GetStringValue(""); v != want {
		t.Fatalf("command = %q, want %q", v, want)
	}
}

func TestProtocol_UnregisterRemovesItAndIsIdempotent(t *testing.T) {
	useTestClasses(t)
	if err := RegisterProtocol(`C:\a.exe`); err != nil {
		t.Fatalf("register: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := UnregisterProtocol(); err != nil {
			t.Fatalf("unregister %d: %v", i, err)
		}
	}
	if _, err := registry.OpenKey(registry.CURRENT_USER, protocolClassesKey+`\steamswitch`, registry.QUERY_VALUE); err == nil {
		t.Fatal("key still there")
	}
}
