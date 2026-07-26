//go:build !windows

package winutil

import (
	"errors"
	"testing"
)

// These run only where process inspection is unavailable. They pin the fail-closed contract
// that `internal/basic` and `internal/steam` both depend on: this package must never answer
// "nothing is running" when what it means is "I cannot tell".

func TestProcessInspectionIsUnsupportedHere(t *testing.T) {
	if ProcessInspectionSupported() {
		t.Fatal("expected process inspection to be unsupported on a !windows build")
	}
}

func TestSnapshotRunningExeBasenames_ReportsFailureRatherThanAnEmptySet(t *testing.T) {
	// An empty set is indistinguishable from "no processes are running", and the caller in
	// internal/basic caches the result as authoritative.
	set, err := SnapshotRunningExeBasenames()
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
	if set != nil {
		t.Fatalf("set = %v, want nil so it cannot be cached as an answer", set)
	}
}
