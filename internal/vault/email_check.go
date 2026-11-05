package vault

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"time"

	"steamswitch/internal/appclient"
	"steamswitch/internal/vault/mail"
)

// What an email check found.
const (
	EmailCheckOK          = "ok"
	EmailCheckAuth        = "auth"         // the mailbox refused the credentials
	EmailCheckAppPassword = "app-password" // ...and this provider requires a generated password
	EmailCheckUnsupported = "unsupported"  // this provider offers no password sign-in over IMAP
	EmailCheckConnect     = "connect"      // the server could not be reached
	EmailCheckConfig      = "config"       // host, user or password is missing
)

// EmailCheck is the last result of signing in to an account's mailbox. It holds no secret and
// is safe to show in a list.
type EmailCheck struct {
	CheckedAt string `json:"checkedAt"`
	Status    string `json:"status"`
	// Messages is the inbox size, only when the sign-in succeeded. A pointer because an empty
	// inbox is a real answer, different from not knowing.
	Messages *int   `json:"messages,omitempty"`
	Provider string `json:"provider,omitempty"`
}

// EmailDetection is what could be worked out from an address alone.
type EmailDetection struct {
	Found    bool   `json:"found"`
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port"`
	Provider string `json:"provider,omitempty"`
	Access   string `json:"access"`
	Foreign  bool   `json:"foreign,omitempty"`
}

// DetectEmail finds the mail server for an address without sending a password anywhere.
func DetectEmail(ctx context.Context, address string) (EmailDetection, error) {
	if _, known := mail.KnownProvider(address); !known && appclient.IsOfflineMode() {
		return EmailDetection{}, mail.ErrOfflineOrN
	}
	d := mail.Detect(ctx, address)
	return EmailDetection{Found: d.Found, Host: d.Host, Port: d.Port, Provider: d.Provider, Access: d.Access, Foreign: d.Foreign}, nil
}

// CheckEmail signs in to an entry's saved mailbox and records what happened. It reads nothing
// and marks nothing read.
func CheckEmail(ctx context.Context, steamID64 string) (EmailCheck, error) {
	e, err := entry(steamID64)
	if err != nil {
		return EmailCheck{}, err
	}
	return runEmailCheck(ctx, normID(steamID64), e.Email)
}

// CheckEmailDraft checks what the editor currently shows, saved or not, with any secret left
// blank taken from the saved entry. The result is kept against those exact settings, so it
// appears on the row once they are saved and not before.
func CheckEmailDraft(ctx context.Context, d Draft) (EmailCheck, error) {
	id := normID(d.SteamID64)
	e := Entry{Email: EmailBinding{Source: EmailSourceNone}}
	if id != "" {
		stored, err := entry(id)
		if err == nil {
			e = cloneEntry(stored)
		} else if !errors.Is(err, ErrNotFound) {
			return EmailCheck{}, err
		}
	}
	before := cloneEntry(e).Email // applyDraft edits the IMAP and mailbox settings in place
	applyDraft(&e, d)
	withholdSavedSecret(&e.Email, before, d)
	return runEmailCheck(ctx, id, e.Email)
}

// withholdSavedSecret stops a typed but unsaved server from receiving the saved password. The
// field looks empty to the user, so sending what it stands for to a new destination is not
// something they could have meant. With no secret the check reports that one is needed.
func withholdSavedSecret(b *EmailBinding, before EmailBinding, d Draft) {
	if b.IMAP != nil && d.IMAPPassword == nil &&
		(before.IMAP == nil || before.IMAP.Host != b.IMAP.Host || before.IMAP.Port != b.IMAP.Port) {
		b.IMAP.Password = ""
	}
	if b.Mailbox != nil && d.MailboxToken == nil &&
		(before.Mailbox == nil || before.Mailbox.BaseURL != b.Mailbox.BaseURL) {
		b.Mailbox.Token = ""
	}
}

func runEmailCheck(ctx context.Context, id string, b EmailBinding) (EmailCheck, error) {
	source := emailSourceOf(Entry{Email: b})
	if source != EmailSourceIMAP && source != EmailSourceMailbox {
		return EmailCheck{}, mail.ErrNoSource
	}
	if appclient.IsOfflineMode() {
		return EmailCheck{}, mail.ErrOfflineOrN
	}

	var messages int
	src, err := sourceFor(Entry{Email: b})
	if err == nil {
		messages, err = src.Probe(ctx)
	}
	check := classifyEmailCheck(b, messages, err, time.Now())
	if id != "" {
		logStep(id, "Vault_EmailCheck_"+emailCheckKey(check.Status), check.tone(), map[string]string{
			"count": strconv.Itoa(messages), "provider": check.Provider,
		})
	}
	if id != "" && Has(id) {
		if rerr := recordEmailCheck(id, b, check); rerr != nil {
			return check, rerr
		}
		return check, nil
	}
	unsavedCheckMu.Lock()
	unsavedCheck, unsavedCheckFor = &check, emailFingerprint(b)
	unsavedCheckMu.Unlock()
	return check, nil
}

// The result of checking a form that has no entry yet, kept in memory so it can join the entry
// if those exact settings are then saved. Add, check, save is the normal order.
var (
	unsavedCheckMu  sync.Mutex
	unsavedCheck    *EmailCheck
	unsavedCheckFor string
)

// adoptUnsavedCheck gives a newly created entry the result for its settings, if there is one.
func adoptUnsavedCheck(e *Entry) {
	unsavedCheckMu.Lock()
	defer unsavedCheckMu.Unlock()
	if unsavedCheck == nil {
		return
	}
	if fp := emailFingerprint(e.Email); fp == unsavedCheckFor {
		e.EmailCheck, e.EmailCheckFor = unsavedCheck, fp
		unsavedCheck, unsavedCheckFor = nil, ""
	}
}

func forgetUnsavedCheck() {
	unsavedCheckMu.Lock()
	defer unsavedCheckMu.Unlock()
	unsavedCheck, unsavedCheckFor = nil, ""
}

// emailCheckKey names a status the way the translations do.
func emailCheckKey(status string) string {
	switch status {
	case EmailCheckOK:
		return "OK"
	case EmailCheckAuth:
		return "Auth"
	case EmailCheckAppPassword:
		return "AppPassword"
	case EmailCheckUnsupported:
		return "Unsupported"
	case EmailCheckConfig:
		return "Config"
	default:
		return "Connect"
	}
}

func (c EmailCheck) tone() string {
	switch c.Status {
	case EmailCheckOK:
		return VerdictOK
	case EmailCheckAuth, EmailCheckAppPassword:
		return VerdictFail
	default:
		return VerdictWarn
	}
}

func classifyEmailCheck(b EmailBinding, messages int, err error, now time.Time) EmailCheck {
	check := EmailCheck{CheckedAt: now.UTC().Format(time.RFC3339)}
	var provider mail.Provider
	if emailSourceOf(Entry{Email: b}) == EmailSourceIMAP {
		provider, _ = mail.KnownProvider(b.Address)
		check.Provider = provider.Name
	}
	switch {
	case err == nil:
		check.Status = EmailCheckOK
		check.Messages = &messages
	case provider.Access == mail.AccessNoIMAP && !usesLocalServer(b):
		// Whatever went wrong, the reason is the provider. Reporting "wrong password" here
		// would send the user off to change a password that was never the problem.
		check.Status = EmailCheckUnsupported
	case errors.Is(err, mail.ErrBadConfig):
		check.Status = EmailCheckConfig
	case errors.Is(err, mail.ErrAuth) && provider.Access == mail.AccessAppPassword:
		check.Status = EmailCheckAppPassword
	case errors.Is(err, mail.ErrAuth):
		check.Status = EmailCheckAuth
	default:
		check.Status = EmailCheckConnect
	}
	return check
}

// usesLocalServer is true for something like Proton Bridge, where the mailbox's provider has no
// say and an ordinary failure means what it usually means.
func usesLocalServer(b EmailBinding) bool {
	return b.IMAP != nil && mail.IsLocalHost(b.IMAP.Host)
}

// recordEmailCheck keeps a result against the settings it tested. Skipping a missing entry is
// deliberate: a check of a new entry has nothing to attach to, and one that outlives a delete
// must not bring the entry back.
func recordEmailCheck(id string, b EmailBinding, check EmailCheck) error {
	return mutate(func(doc *Doc) error {
		e, ok := doc.Entries[id]
		if !ok {
			return nil
		}
		e.EmailCheck = &check
		e.EmailCheckFor = emailFingerprint(b)
		doc.Entries[id] = e
		return nil
	})
}

// currentEmailCheck returns the stored result only if it tested the settings the entry has now,
// so an edited password never sits under an "OK" from the old one. Comparing here covers every
// way a binding can change: the editor, an import, a handoff, or a check still running during a
// save.
func currentEmailCheck(e Entry) *EmailCheck {
	if e.EmailCheck == nil || e.EmailCheckFor != emailFingerprint(e.Email) {
		return nil
	}
	return e.EmailCheck
}

// emailFingerprint identifies a binding's settings. It stays inside the sealed vault, and
// handoff bundles and summaries never carry it.
func emailFingerprint(b EmailBinding) string {
	raw, err := json.Marshal(b)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
