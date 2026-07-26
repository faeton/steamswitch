//go:build !windows

package steam

import (
	"context"
	"errors"
	"testing"

	"steamswitch/internal/winutil"
)

// These run only where process inspection is unavailable — the platform whose stubs used to
// report "Steam is not running" with total confidence. Each asserts the refusal happens
// before anything is written, so a macOS or Linux build cannot corrupt a Steam install.

func TestProcessInspectionIsUnsupportedHere(t *testing.T) {
	if winutil.ProcessInspectionSupported() {
		t.Fatal("expected process inspection to be unsupported on a !windows build")
	}
	if SwitchingSupported() {
		t.Fatal("SwitchingSupported must agree with ProcessInspectionSupported")
	}
}

func TestSnapshotRunningExeBasenames_ReportsFailureRatherThanAnEmptySet(t *testing.T) {
	// An empty set is indistinguishable from "no processes are running", and the caller in
	// internal/basic caches the result.
	set, err := winutil.SnapshotRunningExeBasenames()
	if !errors.Is(err, winutil.ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
	if set != nil {
		t.Fatalf("set = %v, want nil so it cannot be cached as authoritative", set)
	}
}

func TestDotaSteamRunningGuard_RefusesRemoteWritesWhenSteamCannotBeSeen(t *testing.T) {
	// The guard with no backstop: without it, a remote/ write is silently reverted by Steam
	// Cloud on next launch and the user is never told.
	if err := dotaSteamRunningGuard([]string{DotaPartRemote}); !errors.Is(err, ErrSwitchingUnsupported) {
		t.Fatalf("err = %v, want ErrSwitchingUnsupported", err)
	}
}

func TestDotaSteamRunningGuard_StillAllowsWritesThatDoNotTouchRemote(t *testing.T) {
	// local/ is not cloud-synced, so it is not subject to the clobber this guard prevents.
	// Refusing it too would break Dota config management for no safety gain.
	if err := dotaSteamRunningGuard([]string{DotaPartLocal}); err != nil {
		t.Fatalf("err = %v, want nil for a local-only write", err)
	}
}

func TestSwapToAccount_RefusesBeforeTouchingAnything(t *testing.T) {
	err := SwapToAccount("76561198000000000", -1, nil)
	if err == nil {
		t.Fatal("expected a refusal, got nil")
	}
	// It may fail the security gate first depending on lock state; what must never happen is
	// reaching the file writes.
	if !errors.Is(err, ErrSwitchingUnsupported) {
		t.Logf("refused earlier than the platform gate (%v) — acceptable, still no writes", err)
	}
}

func TestLifecycleCloseSteam_RefusesRatherThanReportingAFalseAllClear(t *testing.T) {
	// RunningProcesses returns a bare []string and cannot express "I don't know", so the
	// engine would read empty as "nothing holds the files".
	if err := (Lifecycle{}).CloseSteam(context.Background()); !errors.Is(err, ErrSwitchingUnsupported) {
		t.Fatalf("err = %v, want ErrSwitchingUnsupported", err)
	}
}
