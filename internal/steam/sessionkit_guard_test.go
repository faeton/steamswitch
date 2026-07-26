package steam

import (
	"errors"
	"testing"
)

// A process whose engine failed to build must refuse to switch, not fall back to a bare swap.
//
// The first version of this guard allowed it, on the reasoning that headless dispatch has no
// engine. That was simply wrong — `main` calls `InitSessionKit` before both the headless
// command and the GUI — and the case it actually described is engine *construction failure*,
// which means the journal could not be read. That is the one state where an unjournaled swap
// is most likely to strand a live transaction, so it is the last state that should permit one.
func TestGuardedSwap_RefusesWithNoEngine(t *testing.T) {
	kitEngineMu.Lock()
	prev := kitEngine
	kitEngine = nil
	kitEngineMu.Unlock()
	t.Cleanup(func() {
		kitEngineMu.Lock()
		kitEngine = prev
		kitEngineMu.Unlock()
	})

	ran := false
	err := guardedSwap("76561198000000002", func() error {
		ran = true
		return nil
	})
	if !errors.Is(err, ErrKitEngineUnavailable) {
		t.Fatalf("guardedSwap with no engine = %v, want ErrKitEngineUnavailable", err)
	}
	if ran {
		t.Fatal("the swap body ran despite the guard refusing")
	}

	if err := swapPermitted("76561198000000002"); !errors.Is(err, ErrKitEngineUnavailable) {
		t.Fatalf("swapPermitted = %v, want ErrKitEngineUnavailable", err)
	}
	if err := RestorePermitted(); !errors.Is(err, ErrKitEngineUnavailable) {
		t.Fatalf("RestorePermitted = %v, want ErrKitEngineUnavailable", err)
	}
}
