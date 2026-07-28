package vault

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"steamswitch/internal/security"
)

// Roster interchange — bringing many accounts in at once. REDESIGN_BRIEF.md A7.
//
// The naive design for this feature is "drop an accounts.json of passwords next to the exe,
// import it, then securely delete the file". That is unsafe on Windows and the brief rejects
// it: SSDs do not reliably overwrite, Volume Shadow Copies and OneDrive/Drive version history
// and the Search index all retain copies, and the agent that produced the list holds another
// one in its own workspace. A UI that says "securely deleted" teaches a false belief.
//
// So the canonical shape here is the same one handoff already uses: a passphrase-sealed
// bundle. The producer holds plaintext **in memory**, hands it to `--seal-roster` over stdin,
// and what lands on disk is ciphertext. `.ssroster` is the multi-account sibling of
// `.sshandoff` — same Argon2id-then-AES-GCM envelope, its own AAD so the two can never be
// opened as each other.
//
// What a roster deliberately cannot carry is session material: refresh tokens, guard data,
// token expiry. Those are written only by this app's own login and check paths, on the machine
// that earned them — the same boundary `Draft` draws. A roster is identity, credentials, email
// binding and provenance; the session fills in later, here.

const (
	rosterVersion = 1
	// RosterExt is the on-disk extension for a sealed roster.
	RosterExt = ".ssroster"
	// rosterAAD binds the ciphertext to this format. Frozen, and distinct from bundleAAD so a
	// handoff bundle and a roster cannot be opened as one another even under one passphrase.
	rosterAAD = "steamswitch-roster-v1"

	// MaxRosterBytes caps what will be read as a roster, sealed or plain. Large enough for a
	// few thousand accounts, small enough that a wrong file picked by mistake fails fast
	// instead of being read into memory.
	MaxRosterBytes = 8 << 20 // 8 MiB
	// MaxRosterRecords bounds one import. Past this the review table stops being reviewable,
	// which is the step that makes the import safe.
	MaxRosterRecords = 2000
)

var (
	ErrBadRoster       = errors.New("Toast_Roster_Unreadable")
	ErrRosterEmpty     = errors.New("Toast_Roster_Empty")
	ErrRosterTooLarge  = errors.New("Toast_Roster_TooLarge")
	ErrRosterTooMany   = errors.New("Toast_Roster_TooManyRecords")
	ErrRosterNoRecords = errors.New("Toast_Roster_NoValidRecords")
)

// RosterRecord is one account in the pre-encryption payload.
//
// This struct *is* the published schema — the thing an agent or script fills in before the
// encrypt step. Every field is optional except SteamID64, because a roster that only knows
// identity and a password is already useful and refusing it would push people back to
// hand-editing.
type RosterRecord struct {
	SteamID64   string `json:"steamId64"`
	AccountName string `json:"accountName,omitempty"`
	Label       string `json:"label,omitempty"`

	Password       string `json:"password,omitempty"`
	SharedSecret   string `json:"sharedSecret,omitempty"`
	IdentitySecret string `json:"identitySecret,omitempty"`

	Email *EmailBinding `json:"email,omitempty"`

	Source     string `json:"source,omitempty"`
	AcquiredAt string `json:"acquiredAt,omitempty"`
	SecretNote string `json:"secretNote,omitempty"`
}

// RosterPayload is the plaintext document, sealed or pasted.
type RosterPayload struct {
	Version  int            `json:"version"`
	IssuedAt string         `json:"issuedAt,omitempty"`
	Note     string         `json:"note,omitempty"`
	Accounts []RosterRecord `json:"accounts"`
}

// sealedRoster is the file. As with a handoff bundle, nothing outside the ciphertext says
// anything about the accounts — not how many, not which.
type sealedRoster struct {
	Version    int                `json:"version"`
	KDF        security.KDFParams `json:"kdf"`
	Salt       string             `json:"salt"`
	Nonce      string             `json:"nonce"`
	Ciphertext string             `json:"ciphertext"`
}

// SealRoster encrypts a payload under a passphrase and returns the file bytes.
//
// Returns bytes rather than writing them, because the one caller that matters is the CLI
// piping to stdout — the whole point of `--seal-roster` is that the plaintext never becomes a
// file, and having this function choose a path would invite a second, less careful caller.
func SealRoster(p RosterPayload, passphrase string) ([]byte, error) {
	if len(strings.TrimSpace(passphrase)) < MinPassphraseLength {
		return nil, ErrNoPassphrase
	}
	if len(p.Accounts) == 0 {
		return nil, ErrRosterEmpty
	}
	if len(p.Accounts) > MaxRosterRecords {
		return nil, ErrRosterTooMany
	}
	p.Version = rosterVersion

	plain, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	salt, err := security.RandomBytes(saltSize)
	if err != nil {
		return nil, err
	}
	params, key := security.DeriveFromPassphrase(passphrase, salt)
	nonce, ct, err := security.Seal(key, plain, []byte(rosterAAD))
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(sealedRoster{
		Version:    rosterVersion,
		KDF:        params,
		Salt:       encodeB64(salt),
		Nonce:      encodeB64(nonce),
		Ciphertext: encodeB64(ct),
	}, "", "  ")
}

// OpenRoster decrypts a sealed roster.
func OpenRoster(raw []byte, passphrase string) (RosterPayload, error) {
	if strings.TrimSpace(passphrase) == "" {
		return RosterPayload{}, ErrNoPassphrase
	}
	if len(raw) > MaxRosterBytes {
		return RosterPayload{}, ErrRosterTooLarge
	}
	var sr sealedRoster
	if err := json.Unmarshal(raw, &sr); err != nil || sr.Ciphertext == "" || sr.Salt == "" || sr.Nonce == "" {
		return RosterPayload{}, ErrBadRoster
	}
	if sr.Version > rosterVersion {
		return RosterPayload{}, ErrBadRoster
	}
	salt, err1 := decodeB64(sr.Salt)
	nonce, err2 := decodeB64(sr.Nonce)
	ct, err3 := decodeB64(sr.Ciphertext)
	if err1 != nil || err2 != nil || err3 != nil {
		return RosterPayload{}, ErrBadRoster
	}
	// The KDF parameters travelled inside an unauthenticated envelope, so they are
	// attacker-controlled; security.DeriveWithParams clamps them before use, which is what
	// stops a file claiming 4 GB and a million passes from hanging the importer.
	key := security.DeriveWithParams(passphrase, salt, sr.KDF)
	plain, err := security.Open(key, nonce, ct, []byte(rosterAAD))
	if err != nil {
		// A wrong passphrase, a tampered file and a handoff bundle renamed to .ssroster are
		// one indistinguishable failure. Name the likely cause, not the cryptographic one.
		return RosterPayload{}, ErrWrongPassphrase
	}
	return decodeRosterPayload(plain)
}

// ParseRosterPlaintext reads an unencrypted payload: the paste box, the CLI's stdin, and the
// legacy plaintext-file escape hatch all land here.
//
// Deliberately tolerant about the outer shape. An agent asked to "produce the accounts" writes
// a bare array about as often as it writes the documented wrapper, and rejecting one of those
// teaches people to hand-edit a file full of passwords until the parser stops complaining.
func ParseRosterPlaintext(raw []byte) (RosterPayload, error) {
	if len(raw) > MaxRosterBytes {
		return RosterPayload{}, ErrRosterTooLarge
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return RosterPayload{}, ErrRosterEmpty
	}
	if strings.HasPrefix(trimmed, "[") {
		var records []RosterRecord
		if err := json.Unmarshal([]byte(trimmed), &records); err != nil {
			return RosterPayload{}, ErrBadRoster
		}
		return normaliseRoster(RosterPayload{Version: rosterVersion, Accounts: records})
	}
	if strings.HasPrefix(trimmed, "{") {
		return decodeRosterPayload([]byte(trimmed))
	}
	// Not JSON at all — try CSV, which is what a human pasting from a spreadsheet has.
	return parseRosterCSV(trimmed)
}

func decodeRosterPayload(plain []byte) (RosterPayload, error) {
	var p RosterPayload
	if err := json.Unmarshal(plain, &p); err != nil {
		return RosterPayload{}, ErrBadRoster
	}
	return normaliseRoster(p)
}

func normaliseRoster(p RosterPayload) (RosterPayload, error) {
	if len(p.Accounts) == 0 {
		return RosterPayload{}, ErrRosterEmpty
	}
	if len(p.Accounts) > MaxRosterRecords {
		return RosterPayload{}, ErrRosterTooMany
	}
	if p.Version == 0 {
		p.Version = rosterVersion
	}
	for i := range p.Accounts {
		p.Accounts[i].SteamID64 = strings.TrimSpace(p.Accounts[i].SteamID64)
		p.Accounts[i].AccountName = strings.TrimSpace(p.Accounts[i].AccountName)
		p.Accounts[i].Label = strings.TrimSpace(p.Accounts[i].Label)
		p.Accounts[i].Source = strings.TrimSpace(p.Accounts[i].Source)
		p.Accounts[i].AcquiredAt = strings.TrimSpace(p.Accounts[i].AcquiredAt)
		if e := p.Accounts[i].Email; e != nil {
			e.Address = strings.TrimSpace(e.Address)
			e.Source = normaliseEmailSource(e.Source)
		}
	}
	return p, nil
}

// rosterCSVAliases maps the column names people actually write to the fields they mean. Steam
// exports and spreadsheet templates in the wild use all of these.
var rosterCSVAliases = map[string]string{
	"steamid64": "steamid64", "steamid": "steamid64", "id64": "steamid64", "id": "steamid64",
	"accountname": "accountname", "account": "accountname", "login": "accountname",
	"username": "accountname", "user": "accountname",
	"label": "label", "name": "label", "nickname": "label",
	"password": "password", "pass": "password", "pw": "password",
	"sharedsecret": "sharedsecret", "shared_secret": "sharedsecret", "totp": "sharedsecret",
	"totpsecret": "sharedsecret", "seed": "sharedsecret",
	"identitysecret": "identitysecret", "identity_secret": "identitysecret",
	"email": "email", "emailaddress": "email", "mail": "email",
	"emailpassword": "emailpassword", "email_password": "emailpassword", "mailpassword": "emailpassword",
	"imaphost": "imaphost", "imap_host": "imaphost", "host": "imaphost",
	"imapport": "imapport", "imap_port": "imapport", "port": "imapport",
	"source": "source", "note": "secretnote", "secretnote": "secretnote",
	"acquiredat": "acquiredat", "acquired_at": "acquiredat", "acquired": "acquiredat",
}

// parseRosterCSV reads a header row and maps known columns. Unknown columns are ignored
// rather than refused: a spreadsheet exported from somewhere else carries junk columns, and
// making the user delete them by hand means editing a file full of passwords.
func parseRosterCSV(text string) (RosterPayload, error) {
	r := csv.NewReader(strings.NewReader(text))
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true

	header, err := r.Read()
	if err != nil {
		return RosterPayload{}, ErrBadRoster
	}
	cols := make([]string, len(header))
	known := false
	for i, h := range header {
		key := strings.ToLower(strings.TrimSpace(h))
		key = strings.ReplaceAll(key, " ", "")
		if mapped, ok := rosterCSVAliases[key]; ok {
			cols[i] = mapped
			known = true
		}
	}
	if !known {
		// No recognisable header. Guessing the column order for a file of credentials would
		// be a good way to write somebody's password into the email field.
		return RosterPayload{}, ErrBadRoster
	}

	var out []RosterRecord
	for {
		row, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return RosterPayload{}, ErrBadRoster
		}
		if len(out) >= MaxRosterRecords {
			return RosterPayload{}, ErrRosterTooMany
		}
		rec := RosterRecord{}
		var emailAddr, emailPassword, imapHost string
		imapPort := 0
		for i, cell := range row {
			if i >= len(cols) || cols[i] == "" {
				continue
			}
			cell = strings.TrimSpace(cell)
			if cell == "" {
				continue
			}
			switch cols[i] {
			case "steamid64":
				rec.SteamID64 = cell
			case "accountname":
				rec.AccountName = cell
			case "label":
				rec.Label = cell
			case "password":
				rec.Password = cell
			case "sharedsecret":
				rec.SharedSecret = cell
			case "identitysecret":
				rec.IdentitySecret = cell
			case "source":
				rec.Source = cell
			case "secretnote":
				rec.SecretNote = cell
			case "acquiredat":
				rec.AcquiredAt = cell
			case "email":
				emailAddr = cell
			case "emailpassword":
				emailPassword = cell
			case "imaphost":
				imapHost = cell
			case "imapport":
				if n, convErr := strconv.Atoi(cell); convErr == nil {
					imapPort = n
				}
			}
		}
		if emailAddr != "" {
			binding := &EmailBinding{Address: emailAddr, Source: EmailSourceNone}
			if imapHost != "" || emailPassword != "" {
				if imapPort == 0 {
					imapPort = 993
				}
				binding.Source = EmailSourceIMAP
				binding.IMAP = &IMAPCreds{
					Host:     imapHost,
					Port:     imapPort,
					User:     emailAddr,
					Password: emailPassword,
					UseTLS:   true,
				}
			}
			rec.Email = binding
		}
		out = append(out, rec)
	}

	return normaliseRoster(RosterPayload{Version: rosterVersion, Accounts: out})
}

// sealRosterInput is what `--seal-roster` reads on stdin.
//
// The passphrase travels *in the stream* rather than as an argv flag or an environment
// variable, and that is the point of the whole subcommand. Argv is readable by every process
// on the machine through the process list, lands in shell history, and is captured by audit
// and EDR tooling; the environment is inherited by children and dumped in crash reports. A
// pipe is read once by one process and leaves nothing behind, which is the same reason the
// payload goes this way instead of via a file.
type sealRosterInput struct {
	Passphrase string `json:"passphrase"`
	// Roster is the documented wrapper. Accounts is the same thing hoisted to the top level,
	// accepted because that is what half of all callers will write.
	Roster   *RosterPayload `json:"roster,omitempty"`
	Accounts []RosterRecord `json:"accounts,omitempty"`
	Note     string         `json:"note,omitempty"`
}

// SealRosterStream is intake C: read a plaintext payload and a passphrase from `in`, write the
// sealed bundle to `out`.
//
// This exists so an automation never has to write plaintext credentials to disk at all. The
// producer holds the roster in memory, pipes it here, and what lands on disk — via a shell
// redirect or the path form of the flag — is already ciphertext. Nothing in between is a file
// that a backup agent, a cloud-sync client or the Search indexer can get to.
func SealRosterStream(in io.Reader, out io.Writer) error {
	raw, err := io.ReadAll(io.LimitReader(in, MaxRosterBytes+1))
	if err != nil {
		return ErrBadRoster
	}
	if len(raw) > MaxRosterBytes {
		return ErrRosterTooLarge
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return ErrRosterEmpty
	}

	var input sealRosterInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return ErrBadRoster
	}
	payload := RosterPayload{}
	switch {
	case input.Roster != nil:
		payload = *input.Roster
	case len(input.Accounts) > 0:
		payload = RosterPayload{Accounts: input.Accounts, Note: input.Note}
	default:
		return ErrRosterEmpty
	}
	if payload.Note == "" {
		payload.Note = input.Note
	}
	payload, err = normaliseRoster(payload)
	if err != nil {
		return err
	}
	if payload.IssuedAt == "" {
		payload.IssuedAt = time.Now().UTC().Format(time.RFC3339)
	}

	sealed, err := SealRoster(payload, input.Passphrase)
	if err != nil {
		return err
	}
	_, err = out.Write(sealed)
	return err
}

// readRosterFile reads a user-picked roster, sealed or plain.
//
// Unlike a handoff bundle this is an arbitrary path from a file dialog rather than a name
// inside a folder the app owns, so the confinement a handoff gets from resolveBundlePath does
// not apply. What is left to check is that the thing on the end of the path is a real file:
// Lstat refuses a symlink (following one would read wherever it pointed, which for an import
// that then re-encrypts the contents into the vault is a way to slurp an unrelated file), and
// the size cap stops a wrong pick — a disk image, a video — from being read into memory in
// full before it can be rejected.
func readRosterFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, ErrBadRoster
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, ErrBadRoster
	}
	if info.Size() > MaxRosterBytes {
		return nil, ErrRosterTooLarge
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, ErrBadRoster
	}
	return raw, nil
}

// RosterTemplate is the documented pre-encryption payload, for the "what do I hand the
// agent?" question. It carries no real values — the point is the shape and the encrypt step.
func RosterTemplate() string {
	sample := RosterPayload{
		Version:  rosterVersion,
		IssuedAt: time.Now().UTC().Format(time.RFC3339),
		Note:     "Pipe this into: steamswitch --seal-roster > roster" + RosterExt,
		Accounts: []RosterRecord{{
			SteamID64:    "76561198000000000",
			AccountName:  "example_login",
			Label:        "Example",
			Password:     "the-account-password",
			SharedSecret: "base64-totp-seed",
			Email: &EmailBinding{
				Address: "inbox@example.com",
				Source:  EmailSourceIMAP,
				IMAP: &IMAPCreds{
					Host: "imap.example.com", Port: 993,
					User: "inbox@example.com", Password: "mailbox-app-password",
					UseTLS: true,
				},
			},
			Source: "where this came from",
		}},
	}
	out, err := json.MarshalIndent(sample, "", "  ")
	if err != nil {
		return ""
	}
	return string(out)
}

// validateRosterRecord reports why a record cannot be imported, or "" when it can.
//
// Returns a plain-language reason rather than an error because every one of these ends up in a
// review-table cell next to the row it describes.
func validateRosterRecord(rec RosterRecord) string {
	id := strings.TrimSpace(rec.SteamID64)
	if id == "" {
		return "missing SteamID64"
	}
	if !looksLikeSteamID64Roster(id) {
		return fmt.Sprintf("%q is not a SteamID64", id)
	}
	return ""
}

// looksLikeSteamID64Roster is the same 17-digit shape the Steam engine accepts, restated here
// so `internal/vault` does not take a dependency on `internal/steam` for one predicate.
func looksLikeSteamID64Roster(s string) bool {
	if len(s) != 17 {
		return false
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return false
	}
	// Every individual account sits in the 7656119... range; anything below it is a group,
	// a clan or a typo.
	return n >= 76561197960265728
}
