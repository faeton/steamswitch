package vault

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const goodPassphrase = "a passphrase long enough"

// seedExportable puts an entry in the vault with everything a transfer would carry.
func seedExportable(t *testing.T, id string) {
	t.Helper()
	if err := Put(Draft{
		SteamID64:      id,
		AccountName:    ptr("smurf_one"),
		Label:          ptr("Smurf One"),
		Password:       ptr("hunter2"),
		SharedSecret:   ptr("MTIzNDU2Nzg5MDEyMzQ1Njc4OTA="),
		IdentitySecret: ptr("identity-secret"),
		EmailAddress:   ptr("smurf1@example.test"),
		EmailSource:    ptr(EmailSourceIMAP),
		IMAPHost:       ptr("imap.example.test"),
		IMAPUser:       ptr("smurf1@example.test"),
		IMAPPassword:   ptr("mailpw"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := recordSession(id, "refresh-token-value", "guard-data-value", "2030-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
}

// A bundle sitting in a folder must say nothing about which account it is for. Anyone who
// finds one should learn only that somebody uses SteamSwitch.
func TestExport_BundleIsOpaque(t *testing.T) {
	newVault(t)
	const id = "76561198000000501"
	seedExportable(t, id)

	res, err := Export(ExportRequest{
		SteamID64:  id,
		Mode:       ModeGrant,
		Label:      "For Kev",
		Passphrase: goodPassphrase,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{
		"hunter2", "refresh-token-value", "guard-data-value", "mailpw",
		"smurf_one", "smurf1@example.test", id, "For Kev", ModeGrant,
	} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("%q is readable in the bundle envelope", secret)
		}
	}
	var sb sealedBundle
	if err := json.Unmarshal(raw, &sb); err != nil {
		t.Fatalf("bundle is not JSON: %v", err)
	}
	if sb.Salt == "" || sb.Nonce == "" || sb.Ciphertext == "" {
		t.Fatalf("envelope is missing fields: %+v", sb)
	}

	// Owner-only, like every other file holding a secret.
	info, err := os.Stat(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("bundle permissions = %v, want 0600", perm)
	}
}

// The distinction between the two modes is the entire feature. A grant that carries the
// password is a transfer with a reassuring label on it.
func TestExport_GrantCarriesOnlyTheSession(t *testing.T) {
	newVault(t)
	const id = "76561198000000502"
	seedExportable(t, id)

	res, err := Export(ExportRequest{SteamID64: id, Mode: ModeGrant, Passphrase: goodPassphrase})
	if err != nil {
		t.Fatal(err)
	}
	p, err := openBundle(res.Path, goodPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	if p.RefreshToken != "refresh-token-value" {
		t.Fatalf("a grant did not carry the refresh token: %q", p.RefreshToken)
	}
	if p.Password != "" || p.SharedSecret != "" || p.IdentitySecret != "" || p.Email != nil {
		t.Fatalf("a grant carried ownership material: %+v", p)
	}
	// The trusted-device marker is this machine's, not the account's. The recipient's
	// machine has to earn its own or the owner's Guard history follows the account around.
	if p.GuardData != "" {
		t.Fatalf("a grant carried guard data: %q", p.GuardData)
	}
}

func TestExport_TransferCarriesEverything(t *testing.T) {
	newVault(t)
	const id = "76561198000000503"
	seedExportable(t, id)

	res, err := Export(ExportRequest{
		SteamID64:  id,
		Mode:       ModeTransfer,
		Passphrase: goodPassphrase,
		Confirm:    "smurf_one",
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := openBundle(res.Path, goodPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	if p.Password != "hunter2" || p.SharedSecret == "" || p.IdentitySecret == "" {
		t.Fatalf("a transfer is missing credentials: %+v", p)
	}
	if p.Email == nil || p.Email.IMAP == nil || p.Email.IMAP.Password != "mailpw" {
		t.Fatal("a transfer did not carry the email binding")
	}
}

// A transfer can lock the original owner out of their own account. One click is not enough.
func TestExport_TransferNeedsTheTypedConfirmation(t *testing.T) {
	newVault(t)
	const id = "76561198000000504"
	seedExportable(t, id)

	for _, confirm := range []string{"", "wrong", "smurf_two"} {
		_, err := Export(ExportRequest{SteamID64: id, Mode: ModeTransfer, Passphrase: goodPassphrase, Confirm: confirm})
		if !errors.Is(err, ErrConfirmMismatch) {
			t.Fatalf("Export with confirm %q = %v, want ErrConfirmMismatch", confirm, err)
		}
	}
	// Case-insensitive, because the confirmation is a speed bump, not a spelling test.
	if _, err := Export(ExportRequest{SteamID64: id, Mode: ModeTransfer, Passphrase: goodPassphrase, Confirm: "  SMURF_ONE "}); err != nil {
		t.Fatalf("the correct account name was rejected: %v", err)
	}
}

func TestExport_RejectsWeakAndMissingInput(t *testing.T) {
	newVault(t)
	const id = "76561198000000505"
	seedExportable(t, id)

	if _, err := Export(ExportRequest{SteamID64: id, Mode: "lend", Passphrase: goodPassphrase}); !errors.Is(err, ErrBadMode) {
		t.Fatalf("mode 'lend' = %v, want ErrBadMode — there is no lend", err)
	}
	if _, err := Export(ExportRequest{SteamID64: id, Mode: ModeGrant, Passphrase: "short"}); !errors.Is(err, ErrNoPassphrase) {
		t.Fatalf("a short passphrase = %v, want ErrNoPassphrase", err)
	}

	// An account with nothing to hand over must say so rather than writing an empty bundle
	// that fails on the recipient's machine for reasons neither party can see.
	const bare = "76561198000000506"
	if err := Put(Draft{SteamID64: bare, AccountName: ptr("bare")}); err != nil {
		t.Fatal(err)
	}
	if _, err := Export(ExportRequest{SteamID64: bare, Mode: ModeGrant, Passphrase: goodPassphrase}); !errors.Is(err, ErrNothingToExport) {
		t.Fatalf("exporting an account with no token = %v, want ErrNothingToExport", err)
	}
}

// The round trip is the feature: a bundle written here must open on a machine that shares no
// state with this one. A fresh vault with a different app password stands in for that.
func TestRoundTrip_ImportsOntoAFreshMachine(t *testing.T) {
	newVault(t)
	const id = "76561198000000507"
	seedExportable(t, id)
	res, err := Export(ExportRequest{
		SteamID64: id, Mode: ModeTransfer, Label: "Handed over",
		Passphrase: goodPassphrase, Confirm: "smurf_one",
	})
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}

	// A different data root and a different app password: nothing carries over but the file
	// and the passphrase, which is the whole trust model.
	newVault(t)
	dir, err := HandoffDir()
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "incoming"+BundleExt)
	if err := os.WriteFile(dest, bundle, 0o600); err != nil {
		t.Fatal(err)
	}

	info, err := Inspect(dest, goodPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode != ModeTransfer || info.AccountName != "smurf_one" || info.Label != "Handed over" {
		t.Fatalf("inspect described the bundle wrongly: %+v", info)
	}
	if info.Replaces {
		t.Fatal("inspect claims it would replace an entry on a vault that has none")
	}
	// Inspect must not write. That is what makes "see what this is before accepting it" true.
	if Has(id) {
		t.Fatal("Inspect created the entry; it is supposed to be read-only")
	}

	if _, err := Accept(dest, goodPassphrase); err != nil {
		t.Fatal(err)
	}
	got, err := Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if !got.HasPassword || !got.HasSharedSecret || !got.HasRefreshToken {
		t.Fatalf("the import lost credentials: %+v", got)
	}
	if v, err := Reveal(id, FieldPassword); err != nil || v != "hunter2" {
		t.Fatalf("imported password = %q, %v", v, err)
	}
	if v, err := Reveal(id, FieldEmailPassword); err != nil || v != "mailpw" {
		t.Fatalf("imported email password = %q, %v", v, err)
	}
}

func TestOpenBundle_WrongPassphrase(t *testing.T) {
	newVault(t)
	const id = "76561198000000508"
	seedExportable(t, id)
	res, err := Export(ExportRequest{SteamID64: id, Mode: ModeGrant, Passphrase: goodPassphrase})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openBundle(res.Path, "not the passphrase"); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("err = %v, want ErrWrongPassphrase", err)
	}
	if _, err := openBundle(res.Path, ""); !errors.Is(err, ErrNoPassphrase) {
		t.Fatalf("empty passphrase = %v, want ErrNoPassphrase", err)
	}
}

// Tampering and a wrong passphrase are one indistinguishable failure, and both must fail
// closed rather than yielding a partly-decoded payload.
func TestOpenBundle_TamperedAndMalformed(t *testing.T) {
	newVault(t)
	const id = "76561198000000509"
	seedExportable(t, id)
	res, err := Export(ExportRequest{SteamID64: id, Mode: ModeGrant, Passphrase: goodPassphrase})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	var sb sealedBundle
	if err := json.Unmarshal(raw, &sb); err != nil {
		t.Fatal(err)
	}
	b := []byte(sb.Ciphertext)
	if b[0] == 'A' {
		b[0] = 'B'
	} else {
		b[0] = 'A'
	}
	sb.Ciphertext = string(b)
	out, _ := json.Marshal(sb)
	if err := os.WriteFile(res.Path, out, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openBundle(res.Path, goodPassphrase); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("tampered bundle = %v, want the same failure as a wrong passphrase", err)
	}

	dir, _ := HandoffDir()
	junk := filepath.Join(dir, "junk"+BundleExt)
	if err := os.WriteFile(junk, []byte("not json at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openBundle(junk, goodPassphrase); !errors.Is(err, ErrBadBundle) {
		t.Fatalf("malformed bundle = %v, want ErrBadBundle", err)
	}
	if _, err := openBundle(filepath.Join(dir, "missing"+BundleExt), goodPassphrase); !errors.Is(err, ErrBadBundle) {
		t.Fatalf("missing bundle = %v, want ErrBadBundle", err)
	}
}

// Expiry is advisory — enforced here and nowhere else — but "advisory" is not "ignored".
func TestAccept_RefusesAnExpiredBundle(t *testing.T) {
	newVault(t)
	const id = "76561198000000510"
	seedExportable(t, id)
	res, err := Export(ExportRequest{SteamID64: id, Mode: ModeGrant, Passphrase: goodPassphrase, ExpiresInDays: 1})
	if err != nil {
		t.Fatal(err)
	}
	rewritePayload(t, res.Path, func(p *bundlePayload) {
		p.ExpiresAt = time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	})
	if _, err := Accept(res.Path, goodPassphrase); !errors.Is(err, ErrBundleExpired) {
		t.Fatalf("accepting an expired bundle = %v, want ErrBundleExpired", err)
	}
	info, err := Inspect(res.Path, goodPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Expired {
		t.Fatal("inspect did not report the bundle as expired")
	}
}

// An unreadable expiry has to read as expired. Treating a corrupt field as permission
// inverts the one job the field has.
func TestBundleExpired_CorruptTimestampIsExpired(t *testing.T) {
	if !bundleExpired(bundlePayload{ExpiresAt: "not a timestamp"}, time.Now()) {
		t.Fatal("a corrupt expiry read as still valid")
	}
	if bundleExpired(bundlePayload{}, time.Now()) {
		t.Fatal("a bundle with no expiry read as expired")
	}
}

func TestAccept_SingleUseIsRefusedTheSecondTime(t *testing.T) {
	newVault(t)
	const id = "76561198000000511"
	seedExportable(t, id)
	res, err := Export(ExportRequest{SteamID64: id, Mode: ModeGrant, Passphrase: goodPassphrase, SingleUse: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Accept(res.Path, goodPassphrase); err != nil {
		t.Fatal(err)
	}
	if _, err := Accept(res.Path, goodPassphrase); !errors.Is(err, ErrAlreadyImported) {
		t.Fatalf("second accept = %v, want ErrAlreadyImported", err)
	}

	// The ledger is keyed by bundle id, not by account, so deleting the entry must not make
	// the bundle importable again.
	if err := Delete(id); err != nil {
		t.Fatal(err)
	}
	if _, err := Accept(res.Path, goodPassphrase); !errors.Is(err, ErrAlreadyImported) {
		t.Fatalf("after deleting the entry = %v; deleting must not reset the single-use marker", err)
	}
}

// Without single-use set, re-importing is allowed — it is the ordinary way to redo an import
// that went wrong.
func TestAccept_RepeatableWhenNotSingleUse(t *testing.T) {
	newVault(t)
	const id = "76561198000000512"
	seedExportable(t, id)
	res, err := Export(ExportRequest{SteamID64: id, Mode: ModeGrant, Passphrase: goodPassphrase})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		if _, err := Accept(res.Path, goodPassphrase); err != nil {
			t.Fatalf("accept %d = %v", i+1, err)
		}
	}
}

// The audit log is a record for the person who did the exporting. It must not become a
// second copy of the bundle.
func TestExportLog(t *testing.T) {
	newVault(t)
	const id = "76561198000000513"
	seedExportable(t, id)
	if _, err := Export(ExportRequest{SteamID64: id, Mode: ModeGrant, Label: "For Kev", Passphrase: goodPassphrase}); err != nil {
		t.Fatal(err)
	}
	log, err := ExportLog()
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 1 {
		t.Fatalf("log has %d entries, want 1", len(log))
	}
	if log[0].Mode != ModeGrant || log[0].Label != "For Kev" || log[0].SteamID64 != id {
		t.Fatalf("log entry is wrong: %+v", log[0])
	}
	blob, err := json.Marshal(log)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{goodPassphrase, "hunter2", "refresh-token-value", "mailpw"} {
		if strings.Contains(string(blob), secret) {
			t.Fatalf("the audit log contains %q", secret)
		}
	}
}

// A label is free text from a dialog and ends up in a filename. Without sanitising, one
// containing a path separator steers where the bundle is written.
func TestBundleFileName_SanitisesTheLabel(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	for _, label := range []string{"../../etc/passwd", `..\..\windows`, "/absolute", "with spaces & symbols!"} {
		got := bundleFileName(bundlePayload{Label: label, Mode: ModeGrant, AccountName: "acct"}, now)
		if strings.ContainsAny(got, `/\`) {
			t.Fatalf("bundleFileName(%q) = %q; it contains a path separator", label, got)
		}
		if strings.Contains(got, "..") {
			t.Fatalf("bundleFileName(%q) = %q; it contains a parent reference", label, got)
		}
	}
	// An empty label still produces a usable name rather than a file called ".sshandoff".
	got := bundleFileName(bundlePayload{Mode: ModeGrant}, now)
	if !strings.HasSuffix(got, BundleExt) || strings.HasPrefix(got, ".") {
		t.Fatalf("bundleFileName with no label = %q", got)
	}
}

// The import path arrives from the frontend. Confining it to the handoff folder is what
// stops a crafted value turning the importer into a way to open any file on the machine.
func TestResolveBundlePath_StaysInTheFolder(t *testing.T) {
	newVault(t)
	dir, err := HandoffDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"../../../etc/passwd" + BundleExt,
		`..\..\secrets` + BundleExt,
		"/etc/shadow" + BundleExt,
	} {
		got, err := resolveBundlePath(name)
		if err != nil {
			continue
		}
		if filepath.Dir(got) != dir {
			t.Fatalf("resolveBundlePath(%q) = %q, which escapes %q", name, got, dir)
		}
	}
	for _, bad := range []string{"", "   ", "notabundle.txt", "bundle"} {
		if _, err := resolveBundlePath(bad); !errors.Is(err, ErrBadBundle) {
			t.Fatalf("resolveBundlePath(%q) = %v, want ErrBadBundle", bad, err)
		}
	}
	if got, err := resolveBundlePath("fine" + BundleExt); err != nil || filepath.Dir(got) != dir {
		t.Fatalf("a plain name was rejected: %q, %v", got, err)
	}
}

func TestListBundles(t *testing.T) {
	newVault(t)
	const id = "76561198000000514"
	seedExportable(t, id)
	if _, err := Export(ExportRequest{SteamID64: id, Mode: ModeGrant, Passphrase: goodPassphrase}); err != nil {
		t.Fatal(err)
	}
	dir, _ := HandoffDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore me"), 0o600); err != nil {
		t.Fatal(err)
	}
	list, err := ListBundles()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("ListBundles returned %d entries, want just the bundle: %+v", len(list), list)
	}
	if !strings.HasSuffix(list[0].Name, BundleExt) {
		t.Fatalf("listed a non-bundle: %q", list[0].Name)
	}
}

// rewritePayload re-seals a bundle with a modified payload, so tests can construct states an
// honest export never produces — an already-lapsed expiry, for instance.
func rewritePayload(t *testing.T, path string, edit func(*bundlePayload)) {
	t.Helper()
	p, err := openBundle(path, goodPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	edit(&p)
	plain, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var sb sealedBundle
	if err := json.Unmarshal(raw, &sb); err != nil {
		t.Fatal(err)
	}
	salt, err := decodeB64(sb.Salt)
	if err != nil {
		t.Fatal(err)
	}
	key := deriveBundleKey(goodPassphrase, salt, sb.KDF)
	nonce, ct, err := sealBundle(key, plain)
	if err != nil {
		t.Fatal(err)
	}
	sb.Nonce, sb.Ciphertext = encodeB64(nonce), encodeB64(ct)
	out, err := json.Marshal(sb)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}
