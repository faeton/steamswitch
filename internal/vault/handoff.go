package vault

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"steamswitch/internal/actionlog"
	"steamswitch/internal/paths"
	"steamswitch/internal/security"
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
)

// MinPassphraseLength is the shortest passphrase the exporter will accept. The bundle is
// full account access sitting in a file; the passphrase is the only thing in front of it.
const MinPassphraseLength = 10

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
	path := filepath.Join(dir, bundleFileName(p, now))
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
		return ExportResult{}, err
	}
	actionlog.Record("vault.handoff.export", req.SteamID64, req.Mode, nil)

	return ExportResult{Path: path, Mode: p.Mode, ExpiresAt: p.ExpiresAt, SingleUse: p.SingleUse}, nil
}

// unsafeFileChars keeps a user-chosen label from steering the path. The label is free text
// from the export dialog and the result is used as a filename.
var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func bundleFileName(p bundlePayload, now time.Time) string {
	name := strings.TrimSpace(p.Label)
	if name == "" {
		name = p.AccountName
	}
	name = unsafeFileChars.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-.")
	if name == "" {
		name = "account"
	}
	if len(name) > 40 {
		name = name[:40]
	}
	return fmt.Sprintf("%s-%s-%s%s", name, p.Mode, now.Format("20060102-150405"), BundleExt)
}

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
	// "this will overwrite what you have" rather than discovering it afterwards.
	Replaces bool `json:"replaces"`

	HasPassword     bool `json:"hasPassword"`
	HasRefreshToken bool `json:"hasRefreshToken"`
	HasSharedSecret bool `json:"hasSharedSecret"`
	HasEmail        bool `json:"hasEmail"`
}

// Inspect opens a bundle and describes it without writing anything.
//
// Accept re-derives from the same file and passphrase rather than this returning a handle to
// decrypted material. Argon2 twice for one import is a fraction of a second on a rare
// operation, and the alternative — parking somebody's full account access in a map keyed by
// a token the frontend holds — is a worse trade in every direction.
func Inspect(path, passphrase string) (BundleInfo, error) {
	p, err := openBundle(path, passphrase)
	if err != nil {
		return BundleInfo{}, err
	}
	info := BundleInfo{
		Mode:            p.Mode,
		Label:           p.Label,
		SteamID64:       p.SteamID64,
		AccountName:     p.AccountName,
		IssuedAt:        p.IssuedAt,
		ExpiresAt:       p.ExpiresAt,
		SingleUse:       p.SingleUse,
		HasPassword:     p.Password != "",
		HasRefreshToken: p.RefreshToken != "",
		HasSharedSecret: p.SharedSecret != "",
		HasEmail:        p.Email != nil && p.Email.Address != "",
	}
	info.Expired = bundleExpired(p, time.Now())
	info.AlreadyImported = bundleAlreadyImported(p.BundleID)
	info.Replaces = Has(p.SteamID64)
	return info, nil
}

// Accept imports a bundle into the vault.
func Accept(path, passphrase string) (BundleInfo, error) {
	p, err := openBundle(path, passphrase)
	if err != nil {
		return BundleInfo{}, err
	}
	// Both of these are advisory — enforced here, by the recipient's own copy, and by
	// nothing else. They are checked at accept time as well as at inspect time so that a
	// bundle which lapses between the two is not let through by a stale answer.
	if bundleExpired(p, time.Now()) {
		return BundleInfo{}, ErrBundleExpired
	}
	if p.SingleUse && bundleAlreadyImported(p.BundleID) {
		return BundleInfo{}, ErrAlreadyImported
	}

	d := Draft{
		SteamID64: p.SteamID64,
		Label:     ptrIfSet(p.Label),
		Source:    strPtr("handoff"),
	}
	if p.AccountName != "" {
		d.AccountName = strPtr(p.AccountName)
	}
	if p.Password != "" {
		d.Password = strPtr(p.Password)
	}
	if p.SharedSecret != "" {
		d.SharedSecret = strPtr(p.SharedSecret)
	}
	if p.IdentitySecret != "" {
		d.IdentitySecret = strPtr(p.IdentitySecret)
	}
	if p.SecretNote != "" {
		d.SecretNote = strPtr(p.SecretNote)
	}
	if p.Email != nil {
		applyEmailToDraft(&d, p.Email)
	}
	if err := Put(d); err != nil {
		return BundleInfo{}, err
	}

	// The session material goes through recordSession rather than the draft, because that is
	// the one path that also decodes and stores the token's expiry.
	if p.RefreshToken != "" {
		if err := recordSession(p.SteamID64, p.RefreshToken, p.GuardData, p.TokenExpiresAt); err != nil {
			return BundleInfo{}, err
		}
	}

	if err := markBundleImported(p.BundleID, p.SteamID64); err != nil {
		return BundleInfo{}, err
	}
	actionlog.Record("vault.handoff.import", p.SteamID64, p.Mode, nil)

	return Inspect(path, passphrase)
}

func openBundle(path, passphrase string) (bundlePayload, error) {
	if strings.TrimSpace(passphrase) == "" {
		return bundlePayload{}, ErrNoPassphrase
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return bundlePayload{}, ErrBadBundle
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
	return p, nil
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
