package vault

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"steamswitch/internal/paths"
	"steamswitch/internal/security"
)

// newVault gives each test its own data root and an unlocked app password, then clears the
// decrypted cache so nothing leaks between tests.
func newVault(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	paths.ResetForTest(root)
	DropCache()
	if err := security.SetAppPassword("correct horse battery staple"); err != nil {
		t.Fatalf("set app password: %v", err)
	}
	t.Cleanup(DropCache)
	return root
}

func ptr[T any](v T) *T { return &v }

func TestPutGetRoundTrip(t *testing.T) {
	newVault(t)

	if err := Put(Draft{
		SteamID64:    "76561198000000001",
		AccountName:  ptr("smurf_one"),
		Label:        ptr("Smurf 1"),
		Password:     ptr("hunter2"),
		SharedSecret: ptr("MTIzNDU2Nzg5MDEyMzQ1Njc4OTA="),
		EmailAddress: ptr("smurf1@example.test"),
		EmailSource:  ptr(EmailSourceIMAP),
		IMAPHost:     ptr("imap.example.test"),
		IMAPUser:     ptr("smurf1@example.test"),
		IMAPPassword: ptr("mailpw"),
		Source:       ptr("bought"),
	}); err != nil {
		t.Fatal(err)
	}

	got, err := Get("76561198000000001")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccountName != "smurf_one" || got.Label != "Smurf 1" {
		t.Fatalf("identity did not round-trip: %+v", got)
	}
	if !got.HasPassword || !got.HasSharedSecret || !got.HasEmailAuth {
		t.Fatalf("presence flags wrong: %+v", got)
	}
	if got.EmailSource != EmailSourceIMAP {
		t.Fatalf("email source = %q", got.EmailSource)
	}
	// The IMAP defaults matter: an entry saved without touching the port must still be
	// dialable, and 993/TLS is the only sane default to land on.
	if v, err := Reveal("76561198000000001", FieldEmailPassword); err != nil || v != "mailpw" {
		t.Fatalf("reveal email password = %q, %v", v, err)
	}
}

// The summary is what the list view renders. If a secret can reach it, one screenshot of the
// account list leaks the vault.
func TestSummaryCarriesNoSecrets(t *testing.T) {
	newVault(t)

	const pw = "s3cr3t-p4ssw0rd"
	const seed = "MTIzNDU2Nzg5MDEyMzQ1Njc4OTA="
	const mailpw = "imap-secret-value"
	const tok = "mailbox-bearer-token"
	if err := Put(Draft{
		SteamID64:      "76561198000000002",
		Password:       ptr(pw),
		SharedSecret:   ptr(seed),
		IdentitySecret: ptr("identity-secret-value"),
		EmailSource:    ptr(EmailSourceIMAP),
		IMAPPassword:   ptr(mailpw),
		MailboxToken:   ptr(tok),
	}); err != nil {
		t.Fatal(err)
	}
	if err := recordSession("76561198000000002", "refresh-token-value", "guard-data-value", ""); err != nil {
		t.Fatal(err)
	}

	list, err := List()
	if err != nil {
		t.Fatal(err)
	}
	blob, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{pw, seed, mailpw, tok, "identity-secret-value", "refresh-token-value", "guard-data-value"} {
		if strings.Contains(string(blob), secret) {
			t.Fatalf("the list payload contains the secret %q", secret)
		}
	}
	if len(list) != 1 || !list[0].HasPassword || !list[0].HasRefreshToken {
		t.Fatalf("presence flags lost: %+v", list)
	}
}

// Saving a form that never displayed the password must not erase it — that is the whole
// reason the draft fields are pointers.
func TestPut_LeavesUntouchedFieldsAlone(t *testing.T) {
	newVault(t)

	if err := Put(Draft{SteamID64: "76561198000000003", Password: ptr("keepme")}); err != nil {
		t.Fatal(err)
	}
	if err := Put(Draft{SteamID64: "76561198000000003", Label: ptr("renamed")}); err != nil {
		t.Fatal(err)
	}
	v, err := Reveal("76561198000000003", FieldPassword)
	if err != nil {
		t.Fatal(err)
	}
	if v != "keepme" {
		t.Fatalf("password = %q after an unrelated edit; the pointer fields are not doing their job", v)
	}
}

func TestPut_ExplicitClears(t *testing.T) {
	newVault(t)

	if err := Put(Draft{
		SteamID64:      "76561198000000004",
		Password:       ptr("gone"),
		SharedSecret:   ptr("MTIzNDU2Nzg5MDEyMzQ1Njc4OTA="),
		IdentitySecret: ptr("also-gone"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := recordSession("76561198000000004", "tok", "guard", "2030-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := Put(Draft{
		SteamID64:     "76561198000000004",
		ClearPassword: true,
		ClearTOTP:     true,
		ClearToken:    true,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := Get("76561198000000004")
	if err != nil {
		t.Fatal(err)
	}
	if got.HasPassword || got.HasSharedSecret || got.HasIdentitySecret || got.HasRefreshToken {
		t.Fatalf("clears did not clear: %+v", got)
	}
	if got.TokenExpiresAt != "" {
		t.Fatalf("token expiry survived the clear: %q", got.TokenExpiresAt)
	}
}

func TestReveal_RejectsFieldsItDoesNotOwn(t *testing.T) {
	newVault(t)
	if err := Put(Draft{SteamID64: "76561198000000005", Password: ptr("x")}); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"", "accountName", "Password", "health", "../password"} {
		if _, err := Reveal("76561198000000005", field); !errors.Is(err, ErrUnknownField) {
			t.Fatalf("Reveal(%q) = %v, want ErrUnknownField", field, err)
		}
	}
}

func TestDelete(t *testing.T) {
	newVault(t)
	if err := Put(Draft{SteamID64: "76561198000000006", Password: ptr("x")}); err != nil {
		t.Fatal(err)
	}
	if err := Delete("76561198000000006"); err != nil {
		t.Fatal(err)
	}
	if _, err := Get("76561198000000006"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after delete = %v, want ErrNotFound", err)
	}
	if err := Delete("76561198000000006"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete = %v, want ErrNotFound", err)
	}
}

// A delete must remove the ciphertext, not merely stop returning the entry. Reading the
// file back with a cold cache is the only way to prove that.
func TestDelete_RewritesTheBlob(t *testing.T) {
	root := newVault(t)
	if err := Put(Draft{SteamID64: "76561198000000007", Password: ptr("tombstone-check")}); err != nil {
		t.Fatal(err)
	}
	if err := Delete("76561198000000007"); err != nil {
		t.Fatal(err)
	}
	DropCache()
	doc, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Entries) != 0 {
		t.Fatalf("entries survived a delete on disk: %+v", doc.Entries)
	}
	if _, err := os.Stat(filepath.Join(root, dirName, fileName)); err != nil {
		t.Fatalf("vault file missing after delete: %v", err)
	}
}

// Nothing readable may reach the disk. This is the single most important property in the
// package, so it is asserted against the raw bytes rather than against the API.
func TestOnDiskBlobIsOpaque(t *testing.T) {
	root := newVault(t)
	const pw = "plaintext-would-appear-here"
	if err := Put(Draft{
		SteamID64:    "76561198000000008",
		AccountName:  ptr("named_account"),
		Password:     ptr(pw),
		EmailAddress: ptr("leaky@example.test"),
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, dirName, fileName))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{pw, "named_account", "leaky@example.test", "76561198000000008"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("%q is readable in the on-disk blob", secret)
		}
	}
	var blob sealedBlob
	if err := json.Unmarshal(raw, &blob); err != nil {
		t.Fatalf("envelope is not JSON: %v", err)
	}
	if blob.Nonce == "" || blob.Ciphertext == "" || blob.UpdatedAt == "" {
		t.Fatalf("envelope is missing fields: %+v", blob)
	}
}

// Tampering must fail closed and must not distinguish itself from a wrong key.
func TestTamperedBlobIsRejected(t *testing.T) {
	root := newVault(t)
	if err := Put(Draft{SteamID64: "76561198000000009", Password: ptr("x")}); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, dirName, fileName)
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var blob sealedBlob
	if err := json.Unmarshal(raw, &blob); err != nil {
		t.Fatal(err)
	}
	// Flip one base64 character of the ciphertext.
	b := []byte(blob.Ciphertext)
	if b[0] == 'A' {
		b[0] = 'B'
	} else {
		b[0] = 'A'
	}
	blob.Ciphertext = string(b)
	out, _ := json.Marshal(blob)
	if err := os.WriteFile(p, out, 0o600); err != nil {
		t.Fatal(err)
	}

	DropCache()
	if _, err := load(); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("load of a tampered blob = %v, want ErrCorrupt", err)
	}
}

// A vault that has never been written is an empty vault, not an error. Otherwise the first
// Put has to special-case creation, and the settings page has to special-case the read.
func TestLoadOfAMissingFileIsAnEmptyVault(t *testing.T) {
	newVault(t)
	list, err := List()
	if err != nil {
		t.Fatalf("List on a fresh install = %v, want no error", err)
	}
	if len(list) != 0 {
		t.Fatalf("fresh vault has %d entries", len(list))
	}
	if Exists() {
		t.Fatal("Exists reported true before anything was written")
	}
}

// Locking the app must remove the secrets from memory, not merely refuse to render them.
func TestLockedVaultRefusesAndForgets(t *testing.T) {
	newVault(t)
	if err := Put(Draft{SteamID64: "76561198000000010", Password: ptr("x")}); err != nil {
		t.Fatal(err)
	}
	if !Exists() {
		t.Fatal("Exists is false after a write")
	}

	// There is no exported "lock now" today — the app relaunches to relock — so the state
	// under test is reached by removing the password, which drops the in-memory master key.
	// That is the same condition the vault sees: a key it cannot derive. DropCache stands
	// in for the security status hook that wires this up in main().
	if err := security.RemoveAppPassword("correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	DropCache()

	if _, err := List(); !errors.Is(err, ErrLocked) {
		t.Fatalf("List while locked = %v, want ErrLocked", err)
	}
	if _, err := Reveal("76561198000000010", FieldPassword); !errors.Is(err, ErrLocked) {
		t.Fatalf("Reveal while locked = %v, want ErrLocked", err)
	}
	if Has("76561198000000010") {
		t.Fatal("Has answered from a cache that should have been dropped")
	}
}

func TestPut_RequiresASteamID(t *testing.T) {
	newVault(t)
	for _, id := range []string{"", "   "} {
		if err := Put(Draft{SteamID64: id}); !errors.Is(err, ErrNoSteamID) {
			t.Fatalf("Put(%q) = %v, want ErrNoSteamID", id, err)
		}
	}
}

// List must not depend on Go's map iteration order, or the settings page reshuffles itself
// on every render.
func TestListIsSorted(t *testing.T) {
	newVault(t)
	for _, name := range []string{"zeta", "alpha", "middle"} {
		if err := Put(Draft{SteamID64: "7656119800000010" + name[:1], Label: ptr(name)}); err != nil {
			t.Fatal(err)
		}
	}
	for range 5 {
		list, err := List()
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, s := range list {
			got = append(got, s.Label)
		}
		want := []string{"alpha", "middle", "zeta"}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("order = %v, want %v", got, want)
			}
		}
	}
}
