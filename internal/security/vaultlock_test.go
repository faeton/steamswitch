package security

import (
	"errors"
	"testing"
)

// The vault lock exists so that "lock the vault" stops meaning "freeze the app". These tests
// pin the two halves of that: the app gate must stay down, and the vault gate must hold —
// including in the configuration where the master key cannot be dropped.

func mustSetPassword(t *testing.T, password string) {
	t.Helper()
	if err := SetAppPassword(password); err != nil {
		t.Fatal(err)
	}
}

func statusOf(t *testing.T) Status {
	t.Helper()
	st, err := GetStatus()
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func keyResident() bool {
	defaultManager.mu.Lock()
	defer defaultManager.mu.Unlock()
	return len(defaultManager.masterKey) > 0
}

// TestLockVault_LeavesTheAppUsable is the whole point of the feature: the account paths guard
// on RequireUnlocked, and if a vault lock tripped that, the switcher would stop working.
func TestLockVault_LeavesTheAppUsable(t *testing.T) {
	resetSecurityTest(t)
	mustSetPassword(t, "correct horse battery staple")

	if err := LockVault(); err != nil {
		t.Fatal(err)
	}

	st := statusOf(t)
	if st.AppLocked {
		t.Error("AppLocked = true; locking the vault must not raise the app gate")
	}
	if !st.VaultLocked {
		t.Error("VaultLocked = false after LockVault")
	}
	if err := RequireUnlocked(); err != nil {
		t.Errorf("RequireUnlocked = %v; switching must still work with the vault locked", err)
	}
	if err := RequireVaultUnlocked(); !errors.Is(err, ErrLocked) {
		t.Errorf("RequireVaultUnlocked = %v, want ErrLocked", err)
	}
}

// TestLockVault_DropsTheKeyWhenNothingElseNeedsIt — with saved-account encryption off, the
// master key has no other job, so the lock can be a real cryptographic boundary.
func TestLockVault_DropsTheKeyWhenNothingElseNeedsIt(t *testing.T) {
	resetSecurityTest(t)
	mustSetPassword(t, "correct horse battery staple")

	if st := statusOf(t); st.SavedAccountDataEncrypted {
		t.Fatal("precondition: saved-account encryption should be off by default")
	}
	if err := LockVault(); err != nil {
		t.Fatal(err)
	}

	if keyResident() {
		t.Error("master key still resident; with encryption off it should have been zeroed")
	}
	if st := statusOf(t); !st.VaultLockCryptographic {
		t.Error("VaultLockCryptographic = false, want true when the key was dropped")
	}
	if _, err := SubKey("vault"); !errors.Is(err, ErrLocked) {
		t.Errorf("SubKey = %v, want ErrLocked", err)
	}
}

// TestLockVault_KeepsTheKeyWhenAccountBlobsNeedIt, and — the part that actually matters —
// still refuses to hand out the vault subkey. This is the soft lock: the key is in memory, so
// the gate is the only thing enforcing it, and a regression here would be silent.
func TestLockVault_KeepsTheKeyWhenAccountBlobsNeedIt(t *testing.T) {
	resetSecurityTest(t)
	const password = "correct horse battery staple"
	mustSetPassword(t, password)

	if err := EnableSavedAccountEncryption(password); err != nil {
		t.Fatal(err)
	}
	if err := LockVault(); err != nil {
		t.Fatal(err)
	}

	if !keyResident() {
		t.Fatal("master key was dropped; encrypted account blobs would become unreadable")
	}
	st := statusOf(t)
	if st.AppLocked {
		t.Error("AppLocked = true; the app must stay usable")
	}
	if !st.VaultLocked {
		t.Error("VaultLocked = false after LockVault")
	}
	if st.VaultLockCryptographic {
		t.Error("VaultLockCryptographic = true, but the key is still in memory — the UI would overpromise")
	}
	if _, err := SubKey("vault"); !errors.Is(err, ErrLocked) {
		t.Errorf("SubKey = %v, want ErrLocked — the resident key must not be reachable", err)
	}
}

func TestUnlockVault_RequiresThePasswordEvenWhenTheKeyNeverLeft(t *testing.T) {
	resetSecurityTest(t)
	const password = "correct horse battery staple"
	mustSetPassword(t, password)
	if err := EnableSavedAccountEncryption(password); err != nil {
		t.Fatal(err)
	}
	if err := LockVault(); err != nil {
		t.Fatal(err)
	}

	if err := UnlockVault("wrong password"); err == nil {
		t.Fatal("UnlockVault accepted a wrong password")
	}
	if st := statusOf(t); !st.VaultLocked {
		t.Fatal("a failed unlock left the vault open")
	}

	if err := UnlockVault(password); err != nil {
		t.Fatal(err)
	}
	if st := statusOf(t); st.VaultLocked || st.AppLocked {
		t.Fatalf("still locked after a correct password: %+v", st)
	}
	if _, err := SubKey("vault"); err != nil {
		t.Errorf("SubKey after unlock = %v, want success", err)
	}
}

// TestUnlockVault_RestoresTheDroppedKey covers the cryptographic case: the key is genuinely
// gone, so unlocking has to re-derive it rather than just clearing a flag.
func TestUnlockVault_RestoresTheDroppedKey(t *testing.T) {
	resetSecurityTest(t)
	const password = "correct horse battery staple"
	mustSetPassword(t, password)
	if err := LockVault(); err != nil {
		t.Fatal(err)
	}
	if keyResident() {
		t.Fatal("precondition: the key should have been dropped")
	}

	if err := UnlockVault(password); err != nil {
		t.Fatal(err)
	}
	if !keyResident() {
		t.Fatal("UnlockVault cleared the flag without restoring the key")
	}
	if _, err := SubKey("vault"); err != nil {
		t.Errorf("SubKey after unlock = %v, want success", err)
	}
}

// TestLockApp_AlsoLocksTheVault — there must be no state where the app is sealed but the
// vault reads.
func TestLockApp_AlsoLocksTheVault(t *testing.T) {
	resetSecurityTest(t)
	mustSetPassword(t, "correct horse battery staple")

	if err := LockApp(); err != nil {
		t.Fatal(err)
	}
	st := statusOf(t)
	if !st.AppLocked || !st.VaultLocked {
		t.Fatalf("status = %+v, want both gates up", st)
	}
	if err := RequireUnlocked(); !errors.Is(err, ErrLocked) {
		t.Errorf("RequireUnlocked = %v, want ErrLocked", err)
	}
}

// TestUnlockApp_ClearsAVaultOnlyLock — otherwise a user who locked the vault, then locked the
// app, would pass the app gate and land behind a second lock with no explanation.
func TestUnlockApp_ClearsAVaultOnlyLock(t *testing.T) {
	resetSecurityTest(t)
	const password = "correct horse battery staple"
	mustSetPassword(t, password)

	if err := LockVault(); err != nil {
		t.Fatal(err)
	}
	if err := LockApp(); err != nil {
		t.Fatal(err)
	}
	if err := UnlockApp(password); err != nil {
		t.Fatal(err)
	}

	if st := statusOf(t); st.AppLocked || st.VaultLocked {
		t.Fatalf("status = %+v, want fully unlocked", st)
	}
}

func TestLockVault_BeforeAPasswordExists(t *testing.T) {
	resetSecurityTest(t)
	// Nothing to lock: the vault is unguarded, not unlocked. Reporting success would let the
	// UI show a locked vault that no password can open.
	if err := LockVault(); !errors.Is(err, ErrPasswordNotSet) {
		t.Fatalf("LockVault = %v, want ErrPasswordNotSet", err)
	}
}

func TestLockVault_RefusesDuringOperation(t *testing.T) {
	resetSecurityTest(t)
	mustSetPassword(t, "correct horse battery staple")

	defaultManager.mu.Lock()
	defaultManager.operationBusy = true
	defaultManager.mu.Unlock()
	t.Cleanup(func() {
		defaultManager.mu.Lock()
		defaultManager.operationBusy = false
		defaultManager.mu.Unlock()
	})

	if err := LockVault(); !errors.Is(err, ErrOperationBusy) {
		t.Fatalf("LockVault = %v, want ErrOperationBusy", err)
	}
	if st := statusOf(t); st.VaultLocked {
		t.Fatal("a refused lock still reported the vault as locked")
	}
}

func TestLockVault_EmitsStatusChanged(t *testing.T) {
	resetSecurityTest(t)
	mustSetPassword(t, "correct horse battery staple")

	// The hook is what drops the decrypted vault cache (see main.go). A silent lock would
	// leave the secrets in memory behind a gate the user believes closed.
	fired := 0
	SetStatusChangedHook(func() { fired++ })
	if err := LockVault(); err != nil {
		t.Fatal(err)
	}
	if fired == 0 {
		t.Fatal("LockVault did not emit a status change")
	}
}
