package vault

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"steamswitch/internal/actionlog"
	"steamswitch/internal/paths"
	"steamswitch/internal/security"
	"steamswitch/internal/vault/probe"
)

// Handoff — giving an account to another person. VAULT.md §9.
//
// The premise the whole design rests on, and which the UI must never soften: a
// client-audience refresh token authenticates from another machine with no password and no
// Guard challenge, and **there is no revocation**. With no server there is nothing this app
// can do that reaches the recipient's copy. The owner's only real levers — changing the
// password, Steam's Sign Out Everywhere — are both outside this app.
//
// Two consequences run through this file:
//
//   - There is no "lend". The word implies a recall that cannot be implemented without a
//     server, and a button that implies a lie is worse than no button. The modes are Grant
//     and Transfer, both of which are one-way.
//   - Expiry and single-use are **advisory**. They are enforced by the recipient's copy of
//     SteamSwitch, not by cryptography — anyone willing to patch their build ignores both.
//     They are worth having because they stop honest accidents; they are not a control, and
//     nothing here or in the UI may describe them as one.

// Modes. Frozen: they are written into bundles that other machines read.
const (
	// ModeGrant carries the refresh token only. The recipient can sign in until it expires
	// or the owner signs out everywhere; they cannot change the password or read the seed.
	ModeGrant = "grant"

	// ModeTransfer carries everything. The recipient can do anything, including lock the
	// original owner out. This is a gift, not a loan.
	ModeTransfer = "transfer"
)

const (
	bundleVersion = 1
	// BundleExt is the on-disk extension. Distinctive so a bundle is not mistaken for a
	// backup, and so a file manager does not offer to open it with something else.
	BundleExt = ".sshandoff"
	// bundleAAD binds the ciphertext to this format. Frozen.
	bundleAAD    = "steamswitch-handoff-v1"
	handoffDir   = "Handoff"
	bundleIDSize = 16
	saltSize     = 16
)

var (
	ErrBadMode         = errors.New("Toast_Handoff_BadMode")
	ErrNoPassphrase    = errors.New("Toast_Handoff_PassphraseRequired")
	ErrNothingToExport = errors.New("Toast_Handoff_NothingToExport")
	ErrBadBundle       = errors.New("Toast_Handoff_Unreadable")
	ErrWrongPassphrase = errors.New("Toast_Handoff_WrongPassphrase")
	ErrBundleExpired   = errors.New("Toast_Handoff_Expired")
	ErrAlreadyImported = errors.New("Toast_Handoff_AlreadyImported")
	ErrConfirmMismatch = errors.New("Toast_Handoff_ConfirmMismatch")
	ErrTokenExpired    = errors.New("Toast_Handoff_TokenExpired")
	ErrTokenNotClient  = errors.New("Toast_Handoff_TokenNotClient")
)

// MinPassphraseLength is the shortest passphrase the exporter will accept.
//
// The bundle is full account access sitting in a file, its KDF parameters and salt are in
// the clear beside it, and Argon2id only slows guessing down — it does not make a short
// low-entropy secret adequate. Ten was the first value here and it was too low for what this
// file is; the UI now asks for several unrelated words rather than a "password".
const MinPassphraseLength = 16

// sealedBundle is the file. Nothing outside the ciphertext identifies the account — not the
// SteamID, not the label, not the mode. A bundle found on a shared drive says only that
// somebody uses SteamSwitch.
type sealedBundle struct {
	Version    int                `json:"version"`
	KDF        security.KDFParams `json:"kdf"`
	Salt       string             `json:"salt"`
	Nonce      string             `json:"nonce"`
	Ciphertext string             `json:"ciphertext"`
}

// bundlePayload is the plaintext inside. The label and mode live here rather than in the
// envelope precisely so the import flow can state what is about to be accepted only *after*
// the recipient has proved they hold the passphrase.
type bundlePayload struct {
	Version     int    `json:"version"`
	BundleID    string `json:"bundleId"`
	Mode        string `json:"mode"`
	Label       string `json:"label,omitempty"`
	SteamID64   string `json:"steamId64"`
	AccountName string `json:"accountName,omitempty"`
	IssuedAt    string `json:"issuedAt"`
	ExpiresAt   string `json:"expiresAt,omitempty"`
	SingleUse   bool   `json:"singleUse,omitempty"`

	RefreshToken   string `json:"refreshToken,omitempty"`
	GuardData      string `json:"guardData,omitempty"`
	TokenExpiresAt string `json:"tokenExpiresAt,omitempty"`

	// Transfer only.
	Password       string        `json:"password,omitempty"`
	SharedSecret   string        `json:"sharedSecret,omitempty"`
	IdentitySecret string        `json:"identitySecret,omitempty"`
	Email          *EmailBinding `json:"email,omitempty"`
	SecretNote     string        `json:"secretNote,omitempty"`
}

// ExportRequest is what the export dialog collects.
type ExportRequest struct {
	SteamID64     string `json:"steamId64"`
	Mode          string `json:"mode"`
	Label         string `json:"label"`
	Passphrase    string `json:"passphrase"`
	ExpiresInDays int    `json:"expiresInDays"`
	SingleUse     bool   `json:"singleUse"`
	// Confirm must equal the account name for ModeTransfer. A transfer can lock the owner
	// out of their own account, so it takes more than a click.
	Confirm string `json:"confirm"`
}

// ExportResult tells the UI where the file went. The bytes are deliberately not returned:
// handing a decrypted-by-passphrase blob through the binding layer to JavaScript puts full
// account access in the webview for no benefit, when a path is all the UI needs.
type ExportResult struct {
	Path      string `json:"path"`
	Mode      string `json:"mode"`
	ExpiresAt string `json:"expiresAt,omitempty"`
	SingleUse bool   `json:"singleUse"`
}

// Export writes a handoff bundle for one account.
func Export(req ExportRequest) (ExportResult, error) {
	switch req.Mode {
	case ModeGrant, ModeTransfer:
	default:
		return ExportResult{}, ErrBadMode
	}
	if len(strings.TrimSpace(req.Passphrase)) < MinPassphraseLength {
		return ExportResult{}, ErrNoPassphrase
	}

	e, err := entry(req.SteamID64)
	if err != nil {
		return ExportResult{}, err
	}

	if req.Mode == ModeTransfer {
		// The typed confirmation names the account. Deliberately not a checkbox: the
		// difference between grant and transfer is the difference between lending your car
		// and signing over the title, and the click cost should reflect that.
		if !strings.EqualFold(strings.TrimSpace(req.Confirm), strings.TrimSpace(e.AccountName)) || e.AccountName == "" {
			return ExportResult{}, ErrConfirmMismatch
		}
	}

	now := time.Now().UTC()
	id, err := randomToken(bundleIDSize)
	if err != nil {
		return ExportResult{}, err
	}
	p := bundlePayload{
		Version:     bundleVersion,
		BundleID:    id,
		Mode:        req.Mode,
		Label:       strings.TrimSpace(req.Label),
		SteamID64:   e.SteamID64,
		AccountName: e.AccountName,
		IssuedAt:    now.Format(time.RFC3339),
		SingleUse:   req.SingleUse,
	}
	if req.ExpiresInDays > 0 {
		p.ExpiresAt = now.AddDate(0, 0, req.ExpiresInDays).Format(time.RFC3339)
	}

	switch req.Mode {
	case ModeGrant:
		if e.RefreshToken == "" {
			// Without a token there is no session to grant, and quietly writing an empty
			// bundle would hand the recipient a file that fails for reasons neither of them
			// can see.
			return ExportResult{}, ErrNothingToExport
		}
		if err := usableForGrant(e.RefreshToken, now); err != nil {
			return ExportResult{}, err
		}
		p.RefreshToken = e.RefreshToken
		p.TokenExpiresAt = e.TokenExpiresAt
		// GuardData is deliberately withheld from a grant. It is this machine's
		// trusted-device marker; the recipient's machine has to earn its own.
	case ModeTransfer:
		if e.Password == "" && e.RefreshToken == "" {
			return ExportResult{}, ErrNothingToExport
		}
		p.RefreshToken = e.RefreshToken
		p.TokenExpiresAt = e.TokenExpiresAt
		p.Password = e.Password
		p.SharedSecret = e.SharedSecret
		p.IdentitySecret = e.IdentitySecret
		p.SecretNote = e.SecretNote
		if e.Email.Address != "" || e.Email.Source != "" {
			email := e.Email
			p.Email = &email
		}
	}

	plain, err := json.Marshal(p)
	if err != nil {
		return ExportResult{}, err
	}
	salt, err := security.RandomBytes(saltSize)
	if err != nil {
		return ExportResult{}, err
	}
	params, key := security.DeriveFromPassphrase(req.Passphrase, salt)
	nonce, ct, err := sealBundle(key, plain)
	if err != nil {
		return ExportResult{}, err
	}
	out, err := json.MarshalIndent(sealedBundle{
		Version:    bundleVersion,
		KDF:        params,
		Salt:       encodeB64(salt),
		Nonce:      encodeB64(nonce),
		Ciphertext: encodeB64(ct),
	}, "", "  ")
	if err != nil {
		return ExportResult{}, err
	}

	dir, err := HandoffDir()
	if err != nil {
		return ExportResult{}, err
	}
	path := filepath.Join(dir, bundleFileName(p))
	if err := security.WriteSecretFile(path, out); err != nil {
		return ExportResult{}, err
	}

	// The audit log records that an export happened and to which label. It never records
	// the passphrase, and it lives only on the exporter's machine — it is a record for the
	// person who did it, not a report to anyone.
	if err := recordExport(ExportRecord{
		BundleID:   p.BundleID,
		SteamID64:  p.SteamID64,
		Mode:       p.Mode,
		Label:      p.Label,
		ExportedAt: now.Format(time.RFC3339),
		ExpiresAt:  p.ExpiresAt,
		SingleUse:  p.SingleUse,
		Path:       path,
	}); err != nil {
		// The file exists and the log does not, so take the file back out. An unrecorded
		// bundle sitting on disk is the worse of the two failures: the user sees an error,
		// assumes nothing was created, and a file granting account access stays there.
		_ = os.Remove(path)
		return ExportResult{}, err
	}
	actionlog.Record("vault.handoff.export", req.SteamID64, req.Mode, nil)

	return ExportResult{Path: path, Mode: p.Mode, ExpiresAt: p.ExpiresAt, SingleUse: p.SingleUse}, nil
}

// bundleFileName names the file after the bundle's own random id and nothing else.
//
// It used to embed the label (or the account name) and the mode, which quietly made the
// opacity claim false: the envelope revealed nothing, but `For-Kev-transfer-20260726.sshandoff`
// sitting on a shared drive told anyone who saw it who, what and when — without a passphrase.
// The contents test did not catch it because it only ever read the file.
//
// The exporter does not lose anything: the audit log maps id to label, and the export screen
// shows the path the moment it is written.
func bundleFileName(p bundlePayload) string {
	id := unsafeFileChars.ReplaceAllString(p.BundleID, "")
	if id == "" {
		// Only reachable if the id generator changed shape. A fixed name is still opaque.
		id = "bundle"
	}
	return "steamswitch-" + id + BundleExt
}

// unsafeFileChars strips anything that could steer a path out of a value used as a filename.
var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// HandoffDir is where exports land and where imports are looked for. A folder rather than a
// file dialog: the design is "a file the user moves themselves", and a known folder is the
// simplest thing that supports both halves of that.
func HandoffDir() (string, error) {
	root, err := paths.DataRoot()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, handoffDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// BundleInfo is what an import shows the recipient *before* they accept. Every field here is
// a claim made by whoever wrote the bundle; nothing in it has been verified against Steam.
type BundleInfo struct {
	Mode        string `json:"mode"`
	Label       string `json:"label,omitempty"`
	SteamID64   string `json:"steamId64"`
	AccountName string `json:"accountName,omitempty"`
	IssuedAt    string `json:"issuedAt"`
	ExpiresAt   string `json:"expiresAt,omitempty"`
	SingleUse   bool   `json:"singleUse"`

	Expired         bool `json:"expired"`
	AlreadyImported bool `json:"alreadyImported"`
	// Replaces reports that a vault entry for this account already exists, so the UI can say
	// what accepting will do to it *before* it does it. What that is depends on the mode:
	// a transfer replaces the entry outright, a grant only adds its session to it.
	Replaces bool `json:"replaces"`

	HasPassword     bool `json:"hasPassword"`
	HasRefreshToken bool `json:"hasRefreshToken"`
	HasSharedSecret bool `json:"hasSharedSecret"`
	HasEmail        bool `json:"hasEmail"`
}

func describe(p bundlePayload, now time.Time) BundleInfo {
	return BundleInfo{
		Mode:            p.Mode,
		Label:           p.Label,
		SteamID64:       p.SteamID64,
		AccountName:     p.AccountName,
		IssuedAt:        p.IssuedAt,
		ExpiresAt:       p.ExpiresAt,
		SingleUse:       p.SingleUse,
		Expired:         bundleExpired(p, now),
		AlreadyImported: bundleAlreadyImported(p.BundleID),
		Replaces:        Has(p.SteamID64),
		HasPassword:     p.Password != "",
		HasRefreshToken: p.RefreshToken != "",
		HasSharedSecret: p.SharedSecret != "",
		HasEmail:        p.Email != nil && p.Email.Address != "",
	}
}

// Inspect opens a bundle and describes it without writing anything.
//
// Accept re-derives from the same file and passphrase rather than this returning a handle to
// decrypted material: parking somebody's full account access in a map keyed by a token the
// frontend holds is a worse trade in every direction. That costs one extra Argon2 derivation
// per import — one in Inspect, one in Accept, and no more, which is why Accept builds its own
// answer rather than calling back here.
func Inspect(path, passphrase string) (BundleInfo, error) {
	p, err := openBundle(path, passphrase)
	if err != nil {
		return BundleInfo{}, err
	}
	return describe(p, time.Now()), nil
}

// Accept imports a bundle into the vault.
//
// Every durable change happens inside one mutate. Three separate writes — the entry, the
// session, the single-use ledger — meant a disk-full between them left the vault holding
// somebody's password with no session, or a fully imported single-use bundle still marked
// unused, and returned an error either way so the user believed nothing had happened.
//
// Doing the ledger check inside the same mutate also closes the race: two concurrent Accepts
// of one single-use bundle could both read the ledger as empty before either wrote to it.
func Accept(path, passphrase string) (BundleInfo, error) {
	p, err := openBundle(path, passphrase)
	if err != nil {
		return BundleInfo{}, err
	}
	// Advisory — enforced here, by the recipient's own copy, and by nothing else. Re-checked
	// at accept time as well as at inspect time so a bundle that lapses between the two is
	// not let through by a stale answer.
	if bundleExpired(p, time.Now()) {
		return BundleInfo{}, ErrBundleExpired
	}

	id := normID(p.SteamID64)
	now := time.Now().UTC().Format(time.RFC3339)
	err = mutate(func(doc *Doc) error {
		if p.SingleUse {
			if _, used := doc.ImportedBundles[p.BundleID]; used {
				return ErrAlreadyImported
			}
		}

		// A transfer says "this account is yours now", so it replaces the entry rather than
		// merging into it. Merging would leave the recipient with the previous owner's stale
		// seed or email binding silently surviving underneath the new credentials — a hybrid
		// entry that matches neither side, while the UI said it would be replaced.
		//
		// A grant is the opposite: it adds a session to whatever the recipient already has,
		// and must not disturb credentials of their own.
		e, existing := doc.Entries[id]
		if p.Mode == ModeTransfer || !existing {
			e = Entry{SteamID64: id, Email: EmailBinding{Source: EmailSourceNone}}
		}
		e.SteamID64 = id
		if p.AccountName != "" {
			e.AccountName = p.AccountName
		}
		if p.Label != "" {
			e.Label = p.Label
		}
		e.Source = "handoff"
		if p.Mode == ModeTransfer {
			e.Password = p.Password
			e.SharedSecret = p.SharedSecret
			e.IdentitySecret = p.IdentitySecret
			e.SecretNote = p.SecretNote
			if p.Email != nil {
				e.Email = *p.Email
			}
			if e.Email.Source == "" {
				e.Email.Source = EmailSourceNone
			}
		}
		if p.RefreshToken != "" {
			e.RefreshToken = p.RefreshToken
			e.TokenExpiresAt = p.TokenExpiresAt
			e.GuardData = p.GuardData
		}
		// The schedule and the previous owner's health verdict do not describe this machine.
		e.Health = nil
		e.NextEligibleAt = ""
		e.CheckFailures = 0
		e.UpdatedAt = now
		doc.Entries[id] = e

		if doc.ImportedBundles == nil {
			doc.ImportedBundles = map[string]string{}
		}
		doc.ImportedBundles[p.BundleID] = now
		return nil
	})
	if err != nil {
		return BundleInfo{}, err
	}
	actionlog.Record("vault.handoff.import", p.SteamID64, p.Mode, nil)

	return describe(p, time.Now()), nil
}

func openBundle(path, passphrase string) (bundlePayload, error) {
	if strings.TrimSpace(passphrase) == "" {
		return bundlePayload{}, ErrNoPassphrase
	}
	raw, err := readBundleFile(path)
	if err != nil {
		return bundlePayload{}, err
	}
	var sb sealedBundle
	if err := json.Unmarshal(raw, &sb); err != nil || sb.Ciphertext == "" || sb.Salt == "" || sb.Nonce == "" {
		return bundlePayload{}, ErrBadBundle
	}
	if sb.Version > bundleVersion {
		// Written by a newer build. Saying so is more useful than "unreadable", which sends
		// the recipient looking for a corrupt file that is not corrupt.
		return bundlePayload{}, ErrBadBundle
	}
	salt, err1 := decodeB64(sb.Salt)
	nonce, err2 := decodeB64(sb.Nonce)
	ct, err3 := decodeB64(sb.Ciphertext)
	if err1 != nil || err2 != nil || err3 != nil {
		return bundlePayload{}, ErrBadBundle
	}

	key := deriveBundleKey(passphrase, salt, sb.KDF)
	plain, err := security.Open(key, nonce, ct, []byte(bundleAAD))
	if err != nil {
		// A wrong passphrase and a tampered file are one indistinguishable failure. The
		// message names the likely cause rather than the cryptographic one.
		return bundlePayload{}, ErrWrongPassphrase
	}
	var p bundlePayload
	if err := json.Unmarshal(plain, &p); err != nil {
		return bundlePayload{}, ErrBadBundle
	}
	if p.SteamID64 == "" || (p.Mode != ModeGrant && p.Mode != ModeTransfer) {
		return bundlePayload{}, ErrBadBundle
	}
	// Every honest export sets an id, and the single-use ledger is keyed by it. An empty one
	// would make the marker a no-op — the file could be imported for ever while the UI said
	// it was single-use — so it is a malformed bundle, not a tolerable omission.
	if p.BundleID == "" {
		return bundlePayload{}, ErrBadBundle
	}
	return enforceMode(p), nil
}

// enforceMode strips whatever the declared mode does not permit.
//
// Without this the mode is only a promise the *exporter* keeps. Anyone who can write a
// bundle — the format is documented and the passphrase is theirs — could seal one saying
// `mode: "grant"` while carrying the password, seed and email. Inspect would show the
// recipient "session access, does not include the password", and Accept would import
// ownership material anyway. That inverts the one distinction the whole feature rests on.
//
// It runs in openBundle, so Inspect and Accept see the same payload and cannot disagree
// about what a bundle contains.
func enforceMode(p bundlePayload) bundlePayload {
	if p.Mode != ModeGrant {
		return p
	}
	p.Password = ""
	p.SharedSecret = ""
	p.IdentitySecret = ""
	p.SecretNote = ""
	p.Email = nil
	// guard_data is the exporting machine's trusted-device marker. A grant never carries it,
	// so one that claims to is not a grant.
	p.GuardData = ""
	return p
}

// MaxBundleBytes caps what will be read from the handoff folder. A bundle is a small JSON
// envelope; anything larger is not one, and reading it to find that out is how a
// multi-gigabyte file dropped in the folder becomes an out-of-memory crash on Inspect.
const MaxBundleBytes = 1 << 20 // 1 MiB

func readBundleFile(path string) ([]byte, error) {
	// Lstat, not Stat: the extension and base-name checks in resolveBundlePath confine the
	// *name* to the handoff folder, but a symlink placed there with a valid name would still
	// have os.ReadFile follow it anywhere on disk. Refusing links keeps the confinement the
	// comment claims.
	info, err := os.Lstat(path)
	if err != nil {
		return nil, ErrBadBundle
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, ErrBadBundle
	}
	if info.Size() > MaxBundleBytes {
		return nil, ErrBadBundle
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, ErrBadBundle
	}
	return raw, nil
}

func bundleExpired(p bundlePayload, now time.Time) bool {
	if p.ExpiresAt == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, p.ExpiresAt)
	if err != nil {
		// An unreadable expiry reads as expired. The alternative treats a corrupt field as
		// permission, and this field's whole job is to withhold permission.
		return true
	}
	return now.After(t)
}

func applyEmailToDraft(d *Draft, b *EmailBinding) {
	d.EmailAddress = strPtr(b.Address)
	d.EmailSource = strPtr(b.Source)
	if b.IMAP != nil {
		d.IMAPHost = strPtr(b.IMAP.Host)
		d.IMAPPort = &b.IMAP.Port
		d.IMAPUser = strPtr(b.IMAP.User)
		d.IMAPPassword = strPtr(b.IMAP.Password)
		d.IMAPUseTLS = &b.IMAP.UseTLS
	}
	if b.Mailbox != nil {
		d.MailboxURL = strPtr(b.Mailbox.BaseURL)
		d.MailboxToken = strPtr(b.Mailbox.Token)
		d.MailboxID = strPtr(b.Mailbox.MailboxID)
	}
}

// --- the audit log and the single-use ledger ---------------------------------------------

// MaxExportRecords bounds the audit log. It is a record for the user, not an archive, and an
// unbounded list inside the sealed blob means every vault write re-encrypts a growing file.
const MaxExportRecords = 200

func recordExport(r ExportRecord) error {
	return mutate(func(doc *Doc) error {
		doc.Exports = append(doc.Exports, r)
		if len(doc.Exports) > MaxExportRecords {
			doc.Exports = doc.Exports[len(doc.Exports)-MaxExportRecords:]
		}
		return nil
	})
}

// ExportLog returns the audit log, newest first.
func ExportLog() ([]ExportRecord, error) {
	doc, err := load()
	if err != nil {
		return nil, err
	}
	out := make([]ExportRecord, 0, len(doc.Exports))
	for i := len(doc.Exports) - 1; i >= 0; i-- {
		out = append(out, doc.Exports[i])
	}
	return out, nil
}

func bundleAlreadyImported(id string) bool {
	if id == "" {
		return false
	}
	doc, err := load()
	if err != nil {
		return false
	}
	_, ok := doc.ImportedBundles[id]
	return ok
}

func markBundleImported(id, steamID64 string) error {
	if id == "" {
		return nil
	}
	return mutate(func(doc *Doc) error {
		if doc.ImportedBundles == nil {
			doc.ImportedBundles = map[string]string{}
		}
		doc.ImportedBundles[id] = time.Now().UTC().Format(time.RFC3339)
		return nil
	})
}

// AvailableBundle is one file found in the handoff folder. Nothing here is read from inside
// the bundle — until a passphrase is supplied there is nothing readable in it.
type AvailableBundle struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	ModifiedAt string `json:"modifiedAt"`
	SizeBytes  int64  `json:"sizeBytes"`
}

// ListBundles enumerates importable files in the handoff folder, newest first. This is the
// stand-in for a file-open dialog: the recipient drops the bundle in the folder, which is
// the same "a file the user moves themselves" the design calls for.
func ListBundles() ([]AvailableBundle, error) {
	dir, err := HandoffDir()
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []AvailableBundle
	for _, ent := range ents {
		if ent.IsDir() || !strings.EqualFold(filepath.Ext(ent.Name()), BundleExt) {
			continue
		}
		info, err := ent.Info()
		if err != nil {
			continue
		}
		out = append(out, AvailableBundle{
			Name:       ent.Name(),
			Path:       filepath.Join(dir, ent.Name()),
			ModifiedAt: info.ModTime().UTC().Format(time.RFC3339),
			SizeBytes:  info.Size(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModifiedAt > out[j].ModifiedAt })
	return out, nil
}

// resolveBundlePath keeps an import confined to the handoff folder. The path arrives from
// the frontend, and without this a crafted value would turn the importer into a way to read
// any file on the machine — the error message would only ever say "unreadable", but a
// carefully shaped file elsewhere is still somebody else's data being opened.
func resolveBundlePath(name string) (string, error) {
	dir, err := HandoffDir()
	if err != nil {
		return "", err
	}
	clean := filepath.Base(strings.TrimSpace(name))
	if clean == "" || clean == "." || clean == string(filepath.Separator) {
		return "", ErrBadBundle
	}
	if !strings.EqualFold(filepath.Ext(clean), BundleExt) {
		return "", ErrBadBundle
	}
	return filepath.Join(dir, clean), nil
}

// usableForGrant refuses to export a session that will not be one.
//
// A grant is *only* a token, so a token that cannot sign a client in makes the whole bundle
// empty while still looking like access to both parties. Two ways that happens: the stored
// token is web-audience (issued for a browser session, useless to a Steam client), or it has
// already expired. Both are visible here and invisible to the recipient until they try.
func usableForGrant(token string, now time.Time) error {
	claims, err := probe.DecodeToken(token)
	if err != nil {
		// Undecodable is not necessarily unusable — the format is Valve's and may change —
		// so this is deliberately not a refusal. The recipient still learns what it carries.
		return nil
	}
	if claims.Expired(now) {
		return ErrTokenExpired
	}
	if !claims.IsClientAudience() {
		return ErrTokenNotClient
	}
	return nil
}

// --- envelope helpers ---------------------------------------------------------------------

func sealBundle(key, plaintext []byte) (nonce, ciphertext []byte, err error) {
	return security.Seal(key, plaintext, []byte(bundleAAD))
}

// deriveBundleKey re-derives the key from parameters that travelled inside the file, which
// makes them attacker-controlled. That is safe only because security.normalizeKDFParams
// bounds them first — without that clamp, a bundle claiming 4 GB and a million passes would
// hang or OOM the importer before they had agreed to anything.
func deriveBundleKey(passphrase string, salt []byte, p security.KDFParams) []byte {
	return security.DeriveWithParams(passphrase, salt, p)
}

func encodeB64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func decodeB64(s string) ([]byte, error) { return base64.StdEncoding.DecodeString(s) }

func strPtr(s string) *string { return &s }

func ptrIfSet(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func randomToken(n int) (string, error) {
	b, err := security.RandomBytes(n)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
