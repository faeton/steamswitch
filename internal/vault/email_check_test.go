package vault

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"steamswitch/internal/vault/mail"
)

const emailTestID = "76561198000000100"

func mailboxServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = w.Write([]byte(`[{"id":"1"},{"id":"2"}]`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func saveMailbox(t *testing.T, url string) {
	t.Helper()
	if err := Put(Draft{
		SteamID64:    emailTestID,
		EmailAddress: ptr("fake@example.test"),
		EmailSource:  ptr(EmailSourceMailbox),
		MailboxURL:   ptr(url),
		MailboxToken: ptr("fake-token"),
		MailboxID:    ptr("box"),
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCheckEmail_RecordsCountOnTheSummary(t *testing.T) {
	newVault(t)
	saveMailbox(t, mailboxServer(t, http.StatusOK).URL)

	check, err := CheckEmail(context.Background(), emailTestID)
	if err != nil {
		t.Fatal(err)
	}
	if check.Status != EmailCheckOK || check.Messages == nil || *check.Messages != 2 {
		t.Fatalf("check = %+v", check)
	}
	s, err := Get(emailTestID)
	if err != nil {
		t.Fatal(err)
	}
	if s.EmailCheck == nil || s.EmailCheck.Status != EmailCheckOK {
		t.Fatalf("summary check = %+v", s.EmailCheck)
	}
	raw, _ := json.Marshal(s)
	if strings.Contains(string(raw), "fake-token") || strings.Contains(string(raw), "emailCheckFor") {
		t.Fatalf("summary leaks: %s", raw)
	}
}

// A saved result describes the settings it tested. Changing the password must take the old
// "OK" off the row rather than leave it vouching for a password nobody has tried.
func TestCheckEmail_EditingTheBindingRetiresTheResult(t *testing.T) {
	newVault(t)
	saveMailbox(t, mailboxServer(t, http.StatusOK).URL)
	if _, err := CheckEmail(context.Background(), emailTestID); err != nil {
		t.Fatal(err)
	}
	if err := Put(Draft{SteamID64: emailTestID, MailboxToken: ptr("another-token")}); err != nil {
		t.Fatal(err)
	}
	s, _ := Get(emailTestID)
	if s.EmailCheck != nil {
		t.Fatalf("stale check survived the edit: %+v", s.EmailCheck)
	}
}

func TestCheckEmail_UnrelatedEditKeepsTheResult(t *testing.T) {
	newVault(t)
	saveMailbox(t, mailboxServer(t, http.StatusOK).URL)
	if _, err := CheckEmail(context.Background(), emailTestID); err != nil {
		t.Fatal(err)
	}
	if err := Put(Draft{SteamID64: emailTestID, Label: ptr("renamed")}); err != nil {
		t.Fatal(err)
	}
	s, _ := Get(emailTestID)
	if s.EmailCheck == nil {
		t.Fatal("a label edit dropped the email check")
	}
}

func TestCheckEmailDraft_ResultAppearsOnlyOnceSaved(t *testing.T) {
	newVault(t)
	saveMailbox(t, mailboxServer(t, http.StatusUnauthorized).URL)
	good := mailboxServer(t, http.StatusOK).URL

	d := Draft{SteamID64: emailTestID, MailboxURL: ptr(good), MailboxToken: ptr("typed-token")}
	check, err := CheckEmailDraft(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if check.Status != EmailCheckOK {
		t.Fatalf("draft check = %+v", check)
	}
	s, _ := Get(emailTestID)
	if s.EmailCheck != nil {
		t.Fatalf("an unsaved draft's result showed on the row: %+v", s.EmailCheck)
	}
	stored, _ := entry(emailTestID)
	if stored.Email.Mailbox.BaseURL == good {
		t.Fatal("checking a draft changed the saved settings")
	}
	if err := Put(d); err != nil {
		t.Fatal(err)
	}
	if s, _ = Get(emailTestID); s.EmailCheck == nil {
		t.Fatal("result did not appear after saving the tested settings")
	}
}

func TestCheckEmailDraft_NewEntryIsNotCreated(t *testing.T) {
	newVault(t)
	check, err := CheckEmailDraft(context.Background(), Draft{
		SteamID64:    emailTestID,
		EmailSource:  ptr(EmailSourceMailbox),
		MailboxURL:   ptr(mailboxServer(t, http.StatusOK).URL),
		MailboxToken: ptr("t"),
		MailboxID:    ptr("box"),
	})
	if err != nil || check.Status != EmailCheckOK {
		t.Fatalf("check = %+v, err = %v", check, err)
	}
	if Has(emailTestID) {
		t.Fatal("checking a draft created an entry")
	}
}

func TestCheckEmail_ManualSourceHasNothingToCheck(t *testing.T) {
	newVault(t)
	if err := Put(Draft{SteamID64: emailTestID, EmailSource: ptr(EmailSourceManual)}); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckEmail(context.Background(), emailTestID); !errors.Is(err, mail.ErrNoSource) {
		t.Fatalf("err = %v, want ErrNoSource", err)
	}
}

func TestClassifyEmailCheck(t *testing.T) {
	imapFor := func(address string) EmailBinding {
		return EmailBinding{Address: address, Source: EmailSourceIMAP}
	}
	bridge := EmailBinding{Address: "a@proton.me", Source: EmailSourceIMAP, IMAP: &IMAPCreds{Host: "127.0.0.1"}}
	cases := []struct {
		name string
		b    EmailBinding
		err  error
		want string
	}{
		{"ok", imapFor("a@firstmail.ltd"), nil, EmailCheckOK},
		{"ordinary wrong password", imapFor("a@firstmail.ltd"), mail.ErrAuth, EmailCheckAuth},
		{"gmail wants an app password", imapFor("a@gmail.com"), mail.ErrAuth, EmailCheckAppPassword},
		{"gmail unreachable is not a password problem", imapFor("a@gmail.com"), mail.ErrConnect, EmailCheckConnect},
		{"outlook refusal blames the provider", imapFor("a@outlook.com"), mail.ErrAuth, EmailCheckUnsupported},
		{"proton has no server to reach", imapFor("a@proton.me"), mail.ErrConnect, EmailCheckUnsupported},
		{"proton bridge that works is simply ok", imapFor("a@proton.me"), nil, EmailCheckOK},
		{"proton bridge with a wrong password is a wrong password", bridge, mail.ErrAuth, EmailCheckAuth},
		{"proton bridge not running is a connection problem", bridge, mail.ErrConnect, EmailCheckConnect},
		{"missing settings", imapFor("a@firstmail.ltd"), mail.ErrBadConfig, EmailCheckConfig},
		{"timeout", imapFor("a@firstmail.ltd"), context.DeadlineExceeded, EmailCheckConnect},
		{"mailbox service ignores the address's provider", EmailBinding{Address: "a@outlook.com", Source: EmailSourceMailbox}, mail.ErrAuth, EmailCheckAuth},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyEmailCheck(c.b, 0, c.err, time.Now()).Status; got != c.want {
				t.Fatalf("status = %q, want %q", got, c.want)
			}
		})
	}
}

// The saved token must not travel to a server the user has only typed into the form.
func TestCheckEmailDraft_SavedSecretStaysOffAnUnsavedServer(t *testing.T) {
	newVault(t)
	saveMailbox(t, mailboxServer(t, http.StatusOK).URL)

	var got string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(other.Close)

	check, err := CheckEmailDraft(context.Background(), Draft{SteamID64: emailTestID, MailboxURL: ptr(other.URL)})
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("saved token was sent to the typed server: %q", got)
	}
	_ = check // this test server asks for no token, so the status says nothing here
}

func TestCheckEmailDraft_SavedPasswordIsWithheldFromATypedHost(t *testing.T) {
	newVault(t)
	if err := Put(Draft{
		SteamID64:    emailTestID,
		EmailSource:  ptr(EmailSourceIMAP),
		IMAPHost:     ptr("saved.example.test"),
		IMAPUser:     ptr("u"),
		IMAPPassword: ptr("fake-saved"),
	}); err != nil {
		t.Fatal(err)
	}
	check, err := CheckEmailDraft(context.Background(), Draft{SteamID64: emailTestID, IMAPHost: ptr("typed.example.test")})
	if err != nil {
		t.Fatal(err)
	}
	if check.Status != EmailCheckConfig {
		t.Fatalf("status = %q, want %q: nothing should have been dialled", check.Status, EmailCheckConfig)
	}
	stored, _ := entry(emailTestID)
	if stored.Email.IMAP.Password != "fake-saved" {
		t.Fatal("the check changed the saved password")
	}
}

// Add, check, save is the normal order, so the result must survive onto the new row.
func TestCheckEmailDraft_NewEntryKeepsItsResultWhenSaved(t *testing.T) {
	newVault(t)
	d := Draft{
		AccountName:  ptr("fakelogin"),
		EmailSource:  ptr(EmailSourceMailbox),
		MailboxURL:   ptr(mailboxServer(t, http.StatusOK).URL),
		MailboxToken: ptr("t"),
		MailboxID:    ptr("box"),
	}
	if _, err := CheckEmailDraft(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if err := Put(d); err != nil {
		t.Fatal(err)
	}
	list, _ := List()
	if len(list) != 1 || list[0].EmailCheck == nil || list[0].EmailCheck.Status != EmailCheckOK {
		t.Fatalf("list = %+v", list)
	}
}

func TestPut_LoginOnlyGetsAPendingEntry(t *testing.T) {
	newVault(t)
	if err := Put(Draft{AccountName: ptr("fakelogin"), Password: ptr("fake")}); err != nil {
		t.Fatal(err)
	}
	list, _ := List()
	if len(list) != 1 || !list[0].Pending || list[0].AccountName != "fakelogin" || !list[0].HasPassword {
		t.Fatalf("list = %+v", list)
	}
	if err := Put(Draft{Password: ptr("fake")}); !errors.Is(err, ErrNoSteamID) {
		t.Fatalf("no login and no id: err = %v", err)
	}
}

func TestPut_MissingPendingEntryIsNotRecreated(t *testing.T) {
	newVault(t)
	if err := Put(Draft{SteamID64: pendingIDPrefix + "gone", Label: ptr("x")}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestLinkSteamID(t *testing.T) {
	newVault(t)
	if err := Put(Draft{AccountName: ptr("fakelogin"), Password: ptr("fake")}); err != nil {
		t.Fatal(err)
	}
	list, _ := List()
	pending := list[0].SteamID64

	if err := LinkSteamID(pending, "not-a-steam-id"); err == nil {
		t.Fatal("accepted a bad id")
	}
	if err := Put(Draft{SteamID64: emailTestID, Label: ptr("taken")}); err != nil {
		t.Fatal(err)
	}
	if err := LinkSteamID(pending, emailTestID); !errors.Is(err, ErrSteamIDTaken) {
		t.Fatalf("err = %v, want ErrSteamIDTaken", err)
	}
	const real = "76561198000000200"
	if err := LinkSteamID(pending, real); err != nil {
		t.Fatal(err)
	}
	s, err := Get(real)
	if err != nil {
		t.Fatal(err)
	}
	if s.Pending || s.SteamID64 != real || !s.HasPassword {
		t.Fatalf("moved entry = %+v", s)
	}
	if Has(pending) {
		t.Fatal("the old key survived")
	}
	if err := LinkSteamID(real, "76561198000000300"); !errors.Is(err, ErrNotPending) {
		t.Fatalf("relinking a real id: err = %v", err)
	}
}

func TestQuickCheck_PendingEntryAsksSteamNothing(t *testing.T) {
	newVault(t)
	if err := Put(Draft{AccountName: ptr("fakelogin")}); err != nil {
		t.Fatal(err)
	}
	list, _ := List()
	rep, err := QuickCheck(context.Background(), list[0].SteamID64, QuickCheckInput{APIKey: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, sig := range rep.Signals {
		if sig.Name == SignalBans {
			found = sig.Detail == "Vault_Signal_NeedsSteamID"
		}
	}
	if !found {
		t.Fatalf("signals = %+v", rep.Signals)
	}
}

// A free lookup says nothing about the password, so it must not take away what a sign-in proved.
func TestQuickCheck_KeepsSignInResults(t *testing.T) {
	newVault(t)
	if err := Put(Draft{SteamID64: emailTestID, Password: ptr("fake")}); err != nil {
		t.Fatal(err)
	}
	proved := HealthReport{ProbedAt: "2026-01-01T00:00:00Z", Verdict: VerdictOK, Signals: []Signal{
		{Name: SignalPassword, Status: VerdictOK, Detail: "Vault_Signal_PasswordOK"},
	}}
	if err := recordHealthOnly(emailTestID, proved); err != nil {
		t.Fatal(err)
	}
	rep, err := QuickCheck(context.Background(), emailTestID, QuickCheckInput{})
	if err != nil {
		t.Fatal(err)
	}
	var kept *Signal
	for i, s := range rep.Signals {
		if s.Name == SignalPassword {
			kept = &rep.Signals[i]
		}
	}
	if kept == nil || kept.Status != VerdictOK || kept.At != proved.ProbedAt {
		t.Fatalf("signals = %+v", rep.Signals)
	}
	if rep.Verdict != VerdictOK {
		t.Fatalf("verdict = %q", rep.Verdict)
	}
}

func TestChangingThePasswordRetiresTheProof(t *testing.T) {
	newVault(t)
	if err := Put(Draft{SteamID64: emailTestID, Password: ptr("fake")}); err != nil {
		t.Fatal(err)
	}
	if err := recordHealthOnly(emailTestID, HealthReport{ProbedAt: "x", Verdict: VerdictOK, Signals: []Signal{
		{Name: SignalPassword, Status: VerdictOK, Detail: "Vault_Signal_PasswordOK"},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := Put(Draft{SteamID64: emailTestID, Password: ptr("fake")}); err != nil {
		t.Fatal(err)
	}
	if s, _ := Get(emailTestID); s.Health == nil || len(s.Health.Signals) != 1 {
		t.Fatalf("saving the same password dropped the proof: %+v", s.Health)
	}
	if err := Put(Draft{SteamID64: emailTestID, Password: ptr("fake-new")}); err != nil {
		t.Fatal(err)
	}
	s, _ := Get(emailTestID)
	if len(s.Health.Signals) != 0 || s.Health.Verdict != VerdictUnknown {
		t.Fatalf("proof survived a new password: %+v", s.Health)
	}
}

func TestActivity_RecordsStepsWithoutSecrets(t *testing.T) {
	newVault(t)
	if err := Put(Draft{AccountName: ptr("fakelogin"), Password: ptr("fake-secret-value")}); err != nil {
		t.Fatal(err)
	}
	list, _ := List()
	id := list[0].SteamID64
	if _, err := QuickCheck(context.Background(), id, QuickCheckInput{}); err != nil {
		t.Fatal(err)
	}
	steps := Activity(id)
	if len(steps) == 0 {
		t.Fatal("no activity recorded")
	}
	raw, _ := json.Marshal(steps)
	if strings.Contains(string(raw), "fake-secret-value") || strings.Contains(string(raw), "fakelogin") {
		t.Fatalf("activity leaks: %s", raw)
	}
	const real = "76561198000000200"
	if err := LinkSteamID(id, real); err != nil {
		t.Fatal(err)
	}
	if len(Activity(real)) != len(steps) || len(Activity(id)) != 0 {
		t.Fatal("activity did not follow the entry")
	}
	DropCache()
	if len(Activity(real)) != 0 {
		t.Fatal("activity survived a lock")
	}
}

// A check that finishes after its entry moved must not bring back an empty copy of the old key.
func TestRecordHealth_MovedEntryIsNotRecreated(t *testing.T) {
	newVault(t)
	if err := Put(Draft{AccountName: ptr("fakelogin")}); err != nil {
		t.Fatal(err)
	}
	list, _ := List()
	pending := list[0].SteamID64
	const real = "76561198000000200"
	if err := LinkSteamID(pending, real); err != nil {
		t.Fatal(err)
	}
	if err := recordHealthOnly(pending, HealthReport{Verdict: VerdictOK, ProbedAt: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := recordHealth(pending, HealthReport{Verdict: VerdictOK, ProbedAt: "x"}, time.Time{}, 0); err != nil {
		t.Fatal(err)
	}
	if Has(pending) {
		t.Fatal("the old key came back")
	}
	if got, _ := List(); len(got) != 1 {
		t.Fatalf("entries = %+v", got)
	}
}

// A lookup that started before a sign-in finished must not write the older result back.
func TestRecordHealth_NewerSignInSurvivesAnOlderLookup(t *testing.T) {
	newVault(t)
	if err := Put(Draft{SteamID64: emailTestID, Password: ptr("fake")}); err != nil {
		t.Fatal(err)
	}
	lookup := HealthReport{ProbedAt: "older", Verdict: VerdictUnknown}
	if err := recordHealthOnly(emailTestID, HealthReport{ProbedAt: "newer", Verdict: VerdictFail, Signals: []Signal{
		{Name: SignalPassword, Status: VerdictFail, Detail: "Vault_Signal_PasswordWrong"},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := recordHealthOnly(emailTestID, lookup); err != nil {
		t.Fatal(err)
	}
	s, _ := Get(emailTestID)
	if s.Health.Verdict != VerdictFail || len(s.Health.Signals) != 1 {
		t.Fatalf("health = %+v", s.Health)
	}
}

// A fresh sign-in result replaces the stored one rather than being overruled by it.
func TestRecordHealth_FreshSignInReplacesTheOldOne(t *testing.T) {
	newVault(t)
	if err := Put(Draft{SteamID64: emailTestID, Password: ptr("fake")}); err != nil {
		t.Fatal(err)
	}
	for _, sig := range []Signal{
		{Name: SignalPassword, Status: VerdictFail, Detail: "Vault_Signal_PasswordWrong"},
		{Name: SignalPassword, Status: VerdictOK, Detail: "Vault_Signal_PasswordOK"},
	} {
		if err := recordHealthOnly(emailTestID, HealthReport{ProbedAt: "x", Verdict: sig.Status, Signals: []Signal{sig}}); err != nil {
			t.Fatal(err)
		}
	}
	s, _ := Get(emailTestID)
	if s.Health.Verdict != VerdictOK {
		t.Fatalf("verdict = %q", s.Health.Verdict)
	}
}

func TestRecordSession_ClearsTheOldVerdict(t *testing.T) {
	newVault(t)
	if err := Put(Draft{SteamID64: emailTestID, Password: ptr("fake")}); err != nil {
		t.Fatal(err)
	}
	if err := recordHealthOnly(emailTestID, HealthReport{ProbedAt: "x", Verdict: VerdictFail, Signals: []Signal{
		{Name: SignalSession, Status: VerdictFail, Detail: "Vault_Signal_SessionRevoked"},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := recordSession(emailTestID, "fake.fresh.token", "", ""); err != nil {
		t.Fatal(err)
	}
	s, _ := Get(emailTestID)
	if s.Health.Verdict == VerdictFail || len(s.Health.Signals) != 0 {
		t.Fatalf("health = %+v", s.Health)
	}
}

func TestChangingTheLoginRetiresTheProof(t *testing.T) {
	newVault(t)
	if err := Put(Draft{SteamID64: emailTestID, AccountName: ptr("fakelogin"), Password: ptr("fake")}); err != nil {
		t.Fatal(err)
	}
	if err := recordHealthOnly(emailTestID, HealthReport{ProbedAt: "x", Verdict: VerdictOK, Signals: []Signal{
		{Name: SignalPassword, Status: VerdictOK, Detail: "Vault_Signal_PasswordOK"},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := Put(Draft{SteamID64: emailTestID, AccountName: ptr("fakelogin")}); err != nil {
		t.Fatal(err)
	}
	if s, _ := Get(emailTestID); len(s.Health.Signals) != 1 {
		t.Fatal("saving the same login dropped the proof")
	}
	if err := Put(Draft{SteamID64: emailTestID, AccountName: ptr("otherlogin")}); err != nil {
		t.Fatal(err)
	}
	if s, _ := Get(emailTestID); len(s.Health.Signals) != 0 {
		t.Fatalf("proof survived a new login: %+v", s.Health)
	}
}

func TestSummary_MailboxURLCarriesNoSecret(t *testing.T) {
	newVault(t)
	cases := map[string]string{
		"https://mail.example.test":                  "https://mail.example.test",
		"https://user:fake-secret@mail.example.test": "",
		"https://mail.example.test/?token=fake":      "",
	}
	for in, want := range cases {
		if err := Put(Draft{SteamID64: emailTestID, EmailSource: ptr(EmailSourceMailbox), MailboxURL: ptr(in)}); err != nil {
			t.Fatal(err)
		}
		if s, _ := Get(emailTestID); s.MailboxURL != want {
			t.Errorf("MailboxURL for %q = %q, want %q", in, s.MailboxURL, want)
		}
		if e, _ := entry(emailTestID); e.Email.Mailbox.BaseURL != in {
			t.Errorf("the saved URL changed: %q", e.Email.Mailbox.BaseURL)
		}
	}
}
