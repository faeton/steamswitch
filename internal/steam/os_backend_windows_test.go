//go:build windows

package steam

import (
	"path/filepath"
	"reflect"
	"testing"

	"steamswitch/internal/winutil"
)

func TestWindows_ExitRequestUsesTheClientsOwnShutdown(t *testing.T) {
	root := `C:\Games\Steam`
	client := filepath.Join(root, "steam.exe")
	name, args, ok := windowsExitRequest(root, func(p string) bool { return p == client })
	if !ok || name != client || !reflect.DeepEqual(args, []string{"-shutdown"}) {
		t.Fatalf("got %q %v ok=%v", name, args, ok)
	}
}

// With no client to run there is nothing to ask. Reporting that lets Close skip the grace period
// instead of waiting on a request that was never sent.
func TestWindows_ExitRequestNeedsAClient(t *testing.T) {
	for _, root := range []string{"", `C:\Games\Steam`} {
		if name, _, ok := windowsExitRequest(root, func(string) bool { return false }); ok {
			t.Fatalf("root %q: expected no request, got %q", root, name)
		}
	}
	if _, _, ok := windowsExitRequest("", func(string) bool { return true }); ok {
		t.Fatal("an empty root must not resolve to a steam.exe in the working directory")
	}
}

// TaskKill is the setting for someone who asked for an immediate kill, so it must not wait.
func TestWindows_OnlyTaskKillSkipsTheRequest(t *testing.T) {
	for method, want := range map[winutil.ClosingMethod]bool{
		winutil.ClosingTaskKill: false,
		winutil.ClosingCombined: true,
		winutil.ClosingClose:    true,
		"":                      true,
	} {
		if got := closingMethodAsksFirst(method); got != want {
			t.Errorf("method %q: asks first = %v, want %v", method, got, want)
		}
	}
}

func TestWindows_SplitServicesKeepsTheProcessList(t *testing.T) {
	processes, services := splitServices(windowsBackend{}.ProcessNames())
	if len(processes) == 0 || processes[0] != "steam.exe" {
		t.Fatalf("processes = %v; the client must stay first", processes)
	}
	if !reflect.DeepEqual(services, []string{"SERVICE:Steam Client Service"}) {
		t.Fatalf("services = %v", services)
	}
	for _, p := range processes {
		if p == "SERVICE:Steam Client Service" {
			t.Fatal("a service name cannot be looked up as a process and would read as never running")
		}
	}
}
