package security

import (
	"testing"
	"time"
)

// KDF parameters do not always come from this machine. A handoff bundle carries its own so
// the recipient can re-derive, and a bundle is a file that arrived from somebody else — so
// what it claims has to be bounded before Argon2 is told to do it.
func TestNormalizeKDFParams_ClampsHostileValues(t *testing.T) {
	got := normalizeKDFParams(KDFParams{
		Algorithm: "argon2id",
		Time:      1 << 20,
		MemoryKB:  4 << 20, // 4 GB
		Threads:   255,
		KeyLen:    1 << 20,
	})
	if got.Time != kdfMaxTime {
		t.Errorf("Time = %d, want it clamped to %d", got.Time, kdfMaxTime)
	}
	if got.MemoryKB != kdfMaxMemoryKB {
		t.Errorf("MemoryKB = %d, want it clamped to %d", got.MemoryKB, kdfMaxMemoryKB)
	}
	if got.Threads != kdfMaxThreads {
		t.Errorf("Threads = %d, want it clamped to %d", got.Threads, kdfMaxThreads)
	}
	if got.KeyLen != vaultKeyBytes {
		t.Errorf("KeyLen = %d, want %d — every consumer wants a 32-byte AES key", got.KeyLen, vaultKeyBytes)
	}
}

// Clamping must not disturb parameters a normal calibration produces, or every existing
// app-lock file would start deriving a different key.
func TestNormalizeKDFParams_LeavesOrdinaryValuesAlone(t *testing.T) {
	in := defaultKDFParams()
	got := normalizeKDFParams(in)
	if got.Time != in.Time || got.MemoryKB != in.MemoryKB || got.Threads != in.Threads || got.KeyLen != in.KeyLen {
		t.Fatalf("normalize changed the defaults: %+v -> %+v", in, got)
	}
}

// The bound is only useful if it actually stops the work being done. Without it this call
// would ask Argon2 for 4 GB and a million passes.
func TestDeriveWithParams_HostileBundleDoesNotHang(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		DeriveWithParams("passphrase", []byte("0123456789abcdef"), KDFParams{
			Time:     1 << 20,
			MemoryKB: 4 << 20,
			Threads:  255,
			KeyLen:   1 << 20,
		})
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("deriving with hostile parameters did not finish; the clamp is not holding")
	}
}

// A bundle opens on a machine whose Security/ directory knows nothing about the one that
// wrote it, so the round-trip has to work from the stored parameters alone.
func TestDeriveFromPassphrase_RoundTripsThroughStoredParams(t *testing.T) {
	salt, err := RandomBytes(16)
	if err != nil {
		t.Fatal(err)
	}
	params, key := DeriveFromPassphrase("correct horse battery staple", salt)
	again := DeriveWithParams("correct horse battery staple", salt, params)
	if string(key) != string(again) {
		t.Fatal("re-deriving with the stored parameters produced a different key")
	}
	wrong := DeriveWithParams("not the passphrase", salt, params)
	if string(key) == string(wrong) {
		t.Fatal("a different passphrase produced the same key")
	}
}
