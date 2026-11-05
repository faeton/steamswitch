package vault

import (
	"errors"
	"net/url"
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
	// Pending means the SteamID is not known yet, and SteamID64 holds a stand-in key.
	Pending bool `json:"pending,omitempty"`

	HasPassword       bool `json:"hasPassword"`
	HasSharedSecret   bool `json:"hasSharedSecret"`
	HasIdentitySecret bool `json:"hasIdentitySecret"`
	HasRefreshToken   bool `json:"hasRefreshToken"`

	EmailAddress string `json:"emailAddress,omitempty"`
	EmailSource  string `json:"emailSource"`
	// Where the mailbox lives, so the editor can show it. None of these is a secret.
	EmailHost    string      `json:"emailHost,omitempty"`
	EmailPort    int         `json:"emailPort,omitempty"`
	EmailUser    string      `json:"emailUser,omitempty"`
	MailboxURL   string      `json:"mailboxUrl,omitempty"`
	MailboxID    string      `json:"mailboxId,omitempty"`
	HasEmailAuth bool        `json:"hasEmailAuth"`
	EmailCheck   *EmailCheck `json:"emailCheck,omitempty"`

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
		Pending:           IsPendingID(e.SteamID64),
		HasPassword:       e.Password != "",
		HasSharedSecret:   e.SharedSecret != "",
		HasIdentitySecret: e.IdentitySecret != "",
		HasRefreshToken:   e.RefreshToken != "",
		EmailAddress:      e.Email.Address,
		EmailSource:       emailSourceOf(e),
		EmailCheck:        currentEmailCheck(e),
		Source:            e.Source,
		AcquiredAt:        e.AcquiredAt,
		SecretNote:        e.SecretNote,
		TokenExpiresAt:    e.TokenExpiresAt,
		Health:            e.Health,
		NextEligibleAt:    e.NextEligibleAt,
		UpdatedAt:         e.UpdatedAt,
	}
	if e.Email.IMAP != nil {
		s.EmailHost, s.EmailPort, s.EmailUser = e.Email.IMAP.Host, e.Email.IMAP.Port, e.Email.IMAP.User
	}
	if e.Email.Mailbox != nil {
		s.MailboxURL, s.MailboxID = shareableURL(e.Email.Mailbox.BaseURL), e.Email.Mailbox.MailboxID
	}
	switch s.EmailSource {
	case EmailSourceIMAP:
		s.HasEmailAuth = e.Email.IMAP != nil && e.Email.IMAP.Password != ""
	case EmailSourceMailbox:
		s.HasEmailAuth = e.Email.Mailbox != nil && e.Email.Mailbox.Token != ""
	}
	return s
}

// shareableURL returns a mailbox URL only if it cannot be carrying a secret. Someone can paste
// https://user:password@host or ?token=…, and a Summary must stay safe to show. An empty field in
// the editor leaves the saved URL as it is, because an untouched field is never sent.
func shareableURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	return raw
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
	made := false
	if id == "" {
		// Login and password are enough to store an account. A login does not reveal its
		// SteamID without signing in, so the entry waits under a stand-in key; see LinkSteamID.
		if d.AccountName == nil || strings.TrimSpace(*d.AccountName) == "" {
			return ErrNoSteamID
		}
		token, err := randomToken(pendingIDSize)
		if err != nil {
			return err
		}
		id, made = pendingIDPrefix+token, true
	}
	return mutate(func(doc *Doc) error {
		e, ok := doc.Entries[id]
		if !ok {
			// Seeing a stand-in key with no entry means the entry has since moved to its real
			// SteamID, and a form left open must not bring a second copy back.
			if IsPendingID(id) && !made {
				return ErrNotFound
			}
			e = Entry{SteamID64: id, Email: EmailBinding{Source: EmailSourceNone}}
		}
		applyDraft(&e, d)
		if !ok {
			adoptUnsavedCheck(&e)
		}
		e.SteamID64 = id
		e.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		doc.Entries[id] = e
		return nil
	})
}

// pendingIDPrefix marks the key of an entry whose SteamID is not known yet. It cannot collide
// with a real one, which is 17 digits.
const (
	pendingIDPrefix = "login-"
	pendingIDSize   = 12
)

// IsPendingID reports a stand-in key rather than a real SteamID.
func IsPendingID(id string) bool { return strings.HasPrefix(id, pendingIDPrefix) }

// LinkSteamID moves a login-only entry to its real SteamID, once something has established it:
// a successful sign-in, Steam's own list on this machine, or the user typing it.
func LinkSteamID(pendingID, steamID64 string) error {
	pendingID, steamID64 = normID(pendingID), normID(steamID64)
	if !IsPendingID(pendingID) {
		return ErrNotPending
	}
	if !looksLikeSteamID64Roster(steamID64) {
		return ErrNoSteamID
	}
	return mutate(func(doc *Doc) error {
		e, ok := doc.Entries[pendingID]
		if !ok {
			return ErrNotFound
		}
		if _, taken := doc.Entries[steamID64]; taken {
			return ErrSteamIDTaken
		}
		delete(doc.Entries, pendingID)
		e.SteamID64 = steamID64
		e.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		doc.Entries[steamID64] = e
		moveActivity(pendingID, steamID64)
		return nil
	})
}

func applyDraft(e *Entry, d Draft) {
	// Whatever a sign-in proved, it proved for the old login.
	if d.AccountName != nil && *d.AccountName != e.AccountName {
		forgetSignIn(e, SignalPassword)
		forgetSignIn(e, SignalSession)
	}
	setStr(&e.AccountName, d.AccountName)
	setStr(&e.Label, d.Label)
	if d.Standalone != nil {
		e.Standalone = *d.Standalone
	}
	if d.Password != nil && *d.Password != e.Password {
		forgetSignIn(e, SignalPassword)
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
		forgetSignIn(e, SignalPassword)
	}
	if d.ClearTOTP {
		e.SharedSecret = ""
		e.IdentitySecret = ""
	}
	if d.ClearToken {
		e.RefreshToken = ""
		e.GuardData = ""
		e.TokenExpiresAt = ""
		forgetSignIn(e, SignalSession)
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
			if IsPendingID(id) {
				return nil // it moved to its real SteamID while this was running
			}
			e = Entry{SteamID64: id, Email: EmailBinding{Source: EmailSourceNone}}
		}
		if refreshToken != "" {
			forgetSignIn(&e, SignalSession)
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

// errSkipSuperseded means the session-skip write found the entry no longer eligible under the
// write lock — a deep check recorded a failure, or the deferral cap was reached, since the
// scheduler read it. The skip is abandoned rather than overwriting the fresher state.
var errSkipSuperseded = errors.New("session skip superseded")

// recordSessionSkip stores the result of a scheduled session check that stood in for a deep
// check, and advances the deep-check schedule as a passed check would. It keeps CheckFailures
// at zero — the scheduler only takes this path for an account that is not already backing off,
// so there is no failure history to erase — and increments SessionSkips, which bounds how many
// deep checks in a row a live token may defer.
//
// It re-validates eligibility inside the mutation: the scheduler holds deepMu across the whole
// transaction, but this is a cheap second line of defence against ever overwriting a fresher
// CheckFailures (or exceeding the cap) that some other path recorded in between.
//
// Kept separate from recordHealth because only this path increments the skip counter: a manual
// session check or a quick check must not spend the scheduler's deferral budget for the
// credential login, which verifies something (the password) that neither of those tests.
func recordSessionSkip(steamID64 string, rep HealthReport, nextEligible time.Time) error {
	id := normID(steamID64)
	if id == "" {
		return ErrNoSteamID
	}
	return mutate(func(doc *Doc) error {
		e, ok := doc.Entries[id]
		if !ok {
			// An account deleted mid-check is not one to resurrect with a synthetic entry.
			return ErrNotFound
		}
		if e.CheckFailures > 0 || e.SessionSkips >= MaxSessionSkips {
			return errSkipSuperseded
		}
		carrySignInResults(&rep, e.Health)
		e.Health = &rep
		e.CheckFailures = 0
		e.SessionSkips++
		if !nextEligible.IsZero() {
			e.NextEligibleAt = nextEligible.UTC().Format(time.RFC3339)
		}
		e.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		doc.Entries[id] = e
		return nil
	})
}

// resetSessionSkips clears the deferral budget after a real deep check. Once the credential
// login has run, the password is freshly known and the scheduler may again let a live token
// stand in for future checks.
func resetSessionSkips(steamID64 string) error {
	id := normID(steamID64)
	if id == "" {
		return ErrNoSteamID
	}
	return mutate(func(doc *Doc) error {
		e, ok := doc.Entries[id]
		if !ok || e.SessionSkips == 0 {
			return nil
		}
		e.SessionSkips = 0
		doc.Entries[id] = e
		return nil
	})
}

// recordHealthOnly stores a report while leaving CheckFailures, SessionSkips and the schedule
// exactly as they are on disk. It is what the checks that do NOT test the password use — the
// cheap tier and the session probe — so their stale read of the failure count can never
// overwrite a failure a concurrent deep check recorded. Those paths do not take deepMu (the
// cheap tier is deliberately concurrent), so preserving the field in the read-modify-write is
// the only thing that keeps them from clobbering it.
func recordHealthOnly(steamID64 string, rep HealthReport) error {
	id := normID(steamID64)
	if id == "" {
		return ErrNoSteamID
	}
	return mutate(func(doc *Doc) error {
		e, ok := doc.Entries[id]
		if !ok {
			if IsPendingID(id) {
				return nil // it moved to its real SteamID while this was running
			}
			e = Entry{SteamID64: id, Email: EmailBinding{Source: EmailSourceNone}}
		}
		carrySignInResults(&rep, e.Health)
		e.Health = &rep
		e.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		doc.Entries[id] = e
		return nil
	})
}

// recordHealth stores a report and advances the deep-check schedule. failures resets to
// zero on success so backoff does not creep upward across unrelated runs. Only DeepCheck, which
// actually ran the credential login, sets CheckFailures — the cheap and session checks use
// recordHealthOnly so they cannot overwrite it with a stale value.
func recordHealth(steamID64 string, rep HealthReport, nextEligible time.Time, failures int) error {
	id := normID(steamID64)
	if id == "" {
		return ErrNoSteamID
	}
	return mutate(func(doc *Doc) error {
		e, ok := doc.Entries[id]
		if !ok {
			if IsPendingID(id) {
				return nil // it moved to its real SteamID while this was running
			}
			e = Entry{SteamID64: id, Email: EmailBinding{Source: EmailSourceNone}}
		}
		carrySignInResults(&rep, e.Health)
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
