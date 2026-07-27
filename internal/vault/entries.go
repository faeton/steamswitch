package vault

import (
	"sort"
	"strings"
	"time"
)

// Summary is what leaves the package for a list view: which secrets exist, never what they
// are. Every field here is safe to render on screen, log, or put in a screenshot.
type Summary struct {
	SteamID64   string `json:"steamId64"`
	AccountName string `json:"accountName,omitempty"`
	Label       string `json:"label,omitempty"`
	Standalone  bool   `json:"standalone"`

	HasPassword       bool `json:"hasPassword"`
	HasSharedSecret   bool `json:"hasSharedSecret"`
	HasIdentitySecret bool `json:"hasIdentitySecret"`
	HasRefreshToken   bool `json:"hasRefreshToken"`

	EmailAddress string `json:"emailAddress,omitempty"`
	EmailSource  string `json:"emailSource"`
	HasEmailAuth bool   `json:"hasEmailAuth"`

	Source     string `json:"source,omitempty"`
	AcquiredAt string `json:"acquiredAt,omitempty"`
	SecretNote string `json:"secretNote,omitempty"`

	TokenExpiresAt string        `json:"tokenExpiresAt,omitempty"`
	Health         *HealthReport `json:"health,omitempty"`
	NextEligibleAt string        `json:"nextEligibleAt,omitempty"`

	UpdatedAt string `json:"updatedAt,omitempty"`
}

// The email address is deliberately *not* redacted: it is the entry's most useful
// identifier, the user typed it, and hiding it would make the list unusable. The email
// *password* is the secret, and that never appears here.
func summarise(e Entry) Summary {
	s := Summary{
		SteamID64:         e.SteamID64,
		AccountName:       e.AccountName,
		Label:             e.Label,
		Standalone:        e.Standalone,
		HasPassword:       e.Password != "",
		HasSharedSecret:   e.SharedSecret != "",
		HasIdentitySecret: e.IdentitySecret != "",
		HasRefreshToken:   e.RefreshToken != "",
		EmailAddress:      e.Email.Address,
		EmailSource:       emailSourceOf(e),
		Source:            e.Source,
		AcquiredAt:        e.AcquiredAt,
		SecretNote:        e.SecretNote,
		TokenExpiresAt:    e.TokenExpiresAt,
		Health:            e.Health,
		NextEligibleAt:    e.NextEligibleAt,
		UpdatedAt:         e.UpdatedAt,
	}
	switch s.EmailSource {
	case EmailSourceIMAP:
		s.HasEmailAuth = e.Email.IMAP != nil && e.Email.IMAP.Password != ""
	case EmailSourceMailbox:
		s.HasEmailAuth = e.Email.Mailbox != nil && e.Email.Mailbox.Token != ""
	}
	return s
}

func emailSourceOf(e Entry) string {
	if s := strings.TrimSpace(e.Email.Source); s != "" {
		return s
	}
	return EmailSourceNone
}

// List returns every entry, sorted so the order does not depend on Go's map iteration.
func List() ([]Summary, error) {
	doc, err := load()
	if err != nil {
		return nil, err
	}
	out := make([]Summary, 0, len(doc.Entries))
	for _, e := range doc.Entries {
		out = append(out, summarise(e))
	}
	sort.Slice(out, func(i, j int) bool {
		li, lj := strings.ToLower(out[i].displayKey()), strings.ToLower(out[j].displayKey())
		if li != lj {
			return li < lj
		}
		return out[i].SteamID64 < out[j].SteamID64
	})
	return out, nil
}

func (s Summary) displayKey() string {
	if s.Label != "" {
		return s.Label
	}
	if s.AccountName != "" {
		return s.AccountName
	}
	return s.SteamID64
}

// Get returns one summary. Absence is not an error for callers that are merely asking
// whether an account has a vault entry, so Has is provided separately.
func Get(steamID64 string) (Summary, error) {
	id := normID(steamID64)
	if id == "" {
		return Summary{}, ErrNoSteamID
	}
	doc, err := load()
	if err != nil {
		return Summary{}, err
	}
	e, ok := doc.Entries[id]
	if !ok {
		return Summary{}, ErrNotFound
	}
	return summarise(e), nil
}

// Has answers the tile-badge question without decrypting anything the caller then has to be
// trusted to drop.
func Has(steamID64 string) bool {
	doc, err := load()
	if err != nil {
		return false
	}
	_, ok := doc.Entries[normID(steamID64)]
	return ok
}

// Fields that Reveal and SetField address by name. Anything not in this set is rejected
// rather than reflected onto the struct, so a compromised frontend cannot name its way to a
// field the UI was never meant to expose.
const (
	FieldPassword       = "password"
	FieldSharedSecret   = "sharedSecret"
	FieldIdentitySecret = "identitySecret"
	FieldRefreshToken   = "refreshToken"
	FieldGuardData      = "guardData"
	FieldEmailPassword  = "emailPassword"
	FieldMailboxToken   = "mailboxToken"
)

func revealable(field string) bool {
	switch field {
	case FieldPassword, FieldSharedSecret, FieldIdentitySecret,
		FieldRefreshToken, FieldGuardData, FieldEmailPassword, FieldMailboxToken:
		return true
	}
	return false
}

// Reveal returns exactly one secret, named explicitly by the caller. There is no "give me
// the whole entry" counterpart on purpose: a bulk accessor is how secrets end up in a list
// payload by accident.
func Reveal(steamID64, field string) (string, error) {
	id := normID(steamID64)
	if id == "" {
		return "", ErrNoSteamID
	}
	if !revealable(field) {
		return "", ErrUnknownField
	}
	doc, err := load()
	if err != nil {
		return "", err
	}
	e, ok := doc.Entries[id]
	if !ok {
		return "", ErrNotFound
	}
	switch field {
	case FieldPassword:
		return e.Password, nil
	case FieldSharedSecret:
		return e.SharedSecret, nil
	case FieldIdentitySecret:
		return e.IdentitySecret, nil
	case FieldRefreshToken:
		return e.RefreshToken, nil
	case FieldGuardData:
		return e.GuardData, nil
	case FieldEmailPassword:
		if e.Email.IMAP == nil {
			return "", nil
		}
		return e.Email.IMAP.Password, nil
	case FieldMailboxToken:
		if e.Email.Mailbox == nil {
			return "", nil
		}
		return e.Email.Mailbox.Token, nil
	}
	return "", ErrUnknownField
}

// Draft is the write shape. Pointer fields distinguish "leave this alone" from "set this to
// empty", which matters because the UI saves a form that never displayed the secrets it is
// not changing — without that distinction, opening and saving the dialog would wipe them.
type Draft struct {
	SteamID64   string  `json:"steamId64"`
	AccountName *string `json:"accountName,omitempty"`
	Label       *string `json:"label,omitempty"`
	Standalone  *bool   `json:"standalone,omitempty"`

	Password       *string `json:"password,omitempty"`
	SharedSecret   *string `json:"sharedSecret,omitempty"`
	IdentitySecret *string `json:"identitySecret,omitempty"`

	EmailAddress  *string `json:"emailAddress,omitempty"`
	EmailSource   *string `json:"emailSource,omitempty"`
	IMAPHost      *string `json:"imapHost,omitempty"`
	IMAPPort      *int    `json:"imapPort,omitempty"`
	IMAPUser      *string `json:"imapUser,omitempty"`
	IMAPPassword  *string `json:"imapPassword,omitempty"`
	IMAPUseTLS    *bool   `json:"imapUseTls,omitempty"`
	MailboxURL    *string `json:"mailboxUrl,omitempty"`
	MailboxToken  *string `json:"mailboxToken,omitempty"`
	MailboxID     *string `json:"mailboxId,omitempty"`
	Source        *string `json:"source,omitempty"`
	AcquiredAt    *string `json:"acquiredAt,omitempty"`
	SecretNote    *string `json:"secretNote,omitempty"`
	ClearPassword bool    `json:"clearPassword,omitempty"`
	ClearTOTP     bool    `json:"clearTotp,omitempty"`
	ClearToken    bool    `json:"clearToken,omitempty"`
}

// Put creates or updates an entry from a draft.
func Put(d Draft) error {
	id := normID(d.SteamID64)
	if id == "" {
		return ErrNoSteamID
	}
	return mutate(func(doc *Doc) error {
		e, ok := doc.Entries[id]
		if !ok {
			e = Entry{SteamID64: id, Email: EmailBinding{Source: EmailSourceNone}}
		}
		applyDraft(&e, d)
		e.SteamID64 = id
		e.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		doc.Entries[id] = e
		return nil
	})
}

func applyDraft(e *Entry, d Draft) {
	setStr(&e.AccountName, d.AccountName)
	setStr(&e.Label, d.Label)
	if d.Standalone != nil {
		e.Standalone = *d.Standalone
	}
	setStr(&e.Password, d.Password)
	setStr(&e.SharedSecret, d.SharedSecret)
	setStr(&e.IdentitySecret, d.IdentitySecret)
	setStr(&e.Source, d.Source)
	setStr(&e.AcquiredAt, d.AcquiredAt)
	setStr(&e.SecretNote, d.SecretNote)

	setStr(&e.Email.Address, d.EmailAddress)
	if d.EmailSource != nil {
		e.Email.Source = normaliseEmailSource(*d.EmailSource)
	}
	if d.IMAPHost != nil || d.IMAPPort != nil || d.IMAPUser != nil || d.IMAPPassword != nil || d.IMAPUseTLS != nil {
		if e.Email.IMAP == nil {
			e.Email.IMAP = &IMAPCreds{Port: 993, UseTLS: true}
		}
		setStr(&e.Email.IMAP.Host, d.IMAPHost)
		setStr(&e.Email.IMAP.User, d.IMAPUser)
		setStr(&e.Email.IMAP.Password, d.IMAPPassword)
		if d.IMAPPort != nil {
			e.Email.IMAP.Port = *d.IMAPPort
		}
		if d.IMAPUseTLS != nil {
			e.Email.IMAP.UseTLS = *d.IMAPUseTLS
		}
	}
	if d.MailboxURL != nil || d.MailboxToken != nil || d.MailboxID != nil {
		if e.Email.Mailbox == nil {
			e.Email.Mailbox = &MailboxRef{}
		}
		setStr(&e.Email.Mailbox.BaseURL, d.MailboxURL)
		setStr(&e.Email.Mailbox.Token, d.MailboxToken)
		setStr(&e.Email.Mailbox.MailboxID, d.MailboxID)
	}

	// The explicit clears are separate from the pointer fields because "erase this secret"
	// is a different intent from "I did not touch this field", and the UI must be able to
	// express it without echoing the current value back first.
	if d.ClearPassword {
		e.Password = ""
	}
	if d.ClearTOTP {
		e.SharedSecret = ""
		e.IdentitySecret = ""
	}
	if d.ClearToken {
		e.RefreshToken = ""
		e.GuardData = ""
		e.TokenExpiresAt = ""
	}
	if e.Email.Source == "" {
		e.Email.Source = EmailSourceNone
	}
}

func normaliseEmailSource(s string) string {
	switch strings.TrimSpace(strings.ToLower(s)) {
	case EmailSourceIMAP:
		return EmailSourceIMAP
	case EmailSourceMailbox:
		return EmailSourceMailbox
	case EmailSourceManual:
		return EmailSourceManual
	default:
		return EmailSourceNone
	}
}

func setStr(dst *string, src *string) {
	if src != nil {
		*dst = strings.TrimSpace(*src)
	}
}

// Delete removes an entry and rewrites the whole blob. No tombstone: a delete that leaves
// the ciphertext in place is not a delete.
func Delete(steamID64 string) error {
	id := normID(steamID64)
	if id == "" {
		return ErrNoSteamID
	}
	return mutate(func(doc *Doc) error {
		if _, ok := doc.Entries[id]; !ok {
			return ErrNotFound
		}
		delete(doc.Entries, id)
		return nil
	})
}

// entry reads a full decrypted entry for in-process use — code fetch, health probe, login.
// Unexported: nothing outside this package gets a struct with the secrets still in it.
func entry(steamID64 string) (Entry, error) {
	doc, err := load()
	if err != nil {
		return Entry{}, err
	}
	e, ok := doc.Entries[normID(steamID64)]
	if !ok {
		return Entry{}, ErrNotFound
	}
	return e, nil
}

// recordSession stores what a successful verification login was issued. Kept separate from
// Put because it is written by the app, not by the user's form.
func recordSession(steamID64, refreshToken, guardData, expiresAt string) error {
	id := normID(steamID64)
	if id == "" {
		return ErrNoSteamID
	}
	return mutate(func(doc *Doc) error {
		e, ok := doc.Entries[id]
		if !ok {
			e = Entry{SteamID64: id, Email: EmailBinding{Source: EmailSourceNone}}
		}
		if refreshToken != "" {
			e.RefreshToken = refreshToken
			e.TokenExpiresAt = expiresAt
		}
		if guardData != "" {
			e.GuardData = guardData
		}
		e.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		doc.Entries[id] = e
		return nil
	})
}

// recordHealth stores a report and advances the deep-check schedule. failures resets to
// zero on success so backoff does not creep upward across unrelated runs.
func recordHealth(steamID64 string, rep HealthReport, nextEligible time.Time, failures int) error {
	id := normID(steamID64)
	if id == "" {
		return ErrNoSteamID
	}
	return mutate(func(doc *Doc) error {
		e, ok := doc.Entries[id]
		if !ok {
			e = Entry{SteamID64: id, Email: EmailBinding{Source: EmailSourceNone}}
		}
		e.Health = &rep
		e.CheckFailures = failures
		if !nextEligible.IsZero() {
			e.NextEligibleAt = nextEligible.UTC().Format(time.RFC3339)
		}
		e.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		doc.Entries[id] = e
		return nil
	})
}
