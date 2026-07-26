package mail

import (
	"context"
	"errors"
	"testing"
	"time"
)

// The body shapes below are the ones that actually turn up: Steam's HTML mail, its
// plain-text alternative, and the near-misses that a naive "five characters" regex would
// happily return instead of the code.
func TestExtractCode(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
		ok   bool
	}{
		{
			name: "labelled, html",
			body: `<p>Here is the Steam Guard code you need to login to account smurf_one:</p><h2>Login Code</h2><div class="code">4K7BX</div>`,
			want: "4K7BX",
			ok:   true,
		},
		{
			name: "labelled, plain text",
			body: "Login Code\n\n5MWGC\n\nIf you did not request this, ignore it.",
			want: "5MWGC",
			ok:   true,
		},
		{
			name: "bare code on its own line in a steam mail",
			body: "Your Steam account: someone\n\nR87JJ\n\nThanks,\nThe Steam Support Team",
			want: "R87JJ",
			ok:   true,
		},
		{
			name: "recovery wording",
			body: "Use this verification code to complete your request: QW3RT",
			want: "QW3RT",
			ok:   true,
		},
		{
			name: "no code at all",
			body: "Your Steam purchase receipt. Thanks for your order.",
			ok:   false,
		},
		{
			// A bare 5-char line in a mail that is not about Steam must not be mined for a
			// code — this is what stops an unrelated inbox message being handed over.
			name: "bare code in an unrelated mail",
			body: "Your parcel reference:\n\nAB123\n\nRegards",
			ok:   false,
		},
		{
			// The bounded window is what keeps the label from matching something far away
			// in the message.
			name: "label far from any code",
			body: "Login Code" + string(make([]byte, 200)) + "ZZZZZ",
			ok:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ExtractCode(tc.body)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v (got %q)", ok, tc.ok, got)
			}
			if ok && got != tc.want {
				t.Fatalf("code = %q, want %q", got, tc.want)
			}
		})
	}
}

// The staleness guard is the difference between "here is your code" and "here is the code
// from your last login, which Steam will reject".
func TestFreshEnough(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		sent time.Time
		want bool
	}{
		{"after the login started", now.Add(10 * time.Second), true},
		{"exactly at the login", now, true},
		{"inside the clock-skew window", now.Add(-30 * time.Second), true},
		{"at the edge of the skew window", now.Add(-StaleSkew), true},
		{"just outside the skew window", now.Add(-StaleSkew - time.Second), false},
		{"the previous login's code", now.Add(-10 * time.Minute), false},
		{"no date at all", time.Time{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FreshEnough(tc.sent, now); got != tc.want {
				t.Fatalf("FreshEnough = %v, want %v", got, tc.want)
			}
		})
	}
}

// One inbox often receives codes for a dozen bought accounts. Handing over the wrong one is
// worse than handing over none: the user cannot tell it is wrong until Steam rejects it.
func TestAddressedTo(t *testing.T) {
	to := []string{"Smurf Two <smurf2@example.test>", "someone.else@example.test"}
	if !AddressedTo(to, "smurf2@example.test") {
		t.Fatal("the bound address was not matched")
	}
	if AddressedTo(to, "smurf1@example.test") {
		t.Fatal("another account's code was accepted")
	}
	if !AddressedTo(to, "") {
		t.Fatal("with no binding recorded there is nothing to filter on, so it must pass")
	}
	if !AddressedTo(to, "  SMURF2@EXAMPLE.TEST  ") {
		t.Fatal("matching must be case- and space-insensitive")
	}
}

func TestFromSteam(t *testing.T) {
	if !FromSteam("noreply@steampowered.com") {
		t.Fatal("Valve's own sender rejected")
	}
	if !FromSteam("Steam Support <support@help.steampowered.com>") {
		t.Fatal("a steampowered.com subdomain rejected")
	}
	// The shapes a phishing sender actually takes. Each of these contains the literal
	// "steampowered.com", so a substring test would trust all of them.
	for _, spoof := range []string{
		"noreply@steampowered.com.attacker.test",
		"noreply@notsteampowered.com",
		"Steam <noreply@steampowered.com.evil.example>",
		"steampowered.com@attacker.test",
	} {
		if FromSteam(spoof) {
			t.Fatalf("FromSteam(%q) = true; a lookalike sender was trusted", spoof)
		}
	}
	// Multi-address headers must still find Valve's address among the others.
	if !FromSteam("Someone <a@example.test>, Steam <noreply@steampowered.com>") {
		t.Fatal("a valid sender in a multi-address header was missed")
	}
}

func TestAutoconfigCandidates(t *testing.T) {
	got := AutoconfigCandidates("user@zorrodemail.test")
	want := []string{"imap.zorrodemail.test", "mail.zorrodemail.test", "imap.mail.zorrodemail.test", "zorrodemail.test"}
	if len(got) != len(want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate %d = %q, want %q", i, got[i], want[i])
		}
	}
	for _, bad := range []string{"", "not-an-address", "trailing@"} {
		if c := AutoconfigCandidates(bad); c != nil {
			t.Fatalf("AutoconfigCandidates(%q) = %v, want nil", bad, c)
		}
	}
}

// The mailbox source carries a bearer token out and a Guard code back. Neither belongs on
// plain HTTP, and a config typo is not a good enough reason to allow it.
func TestNewMailbox_RejectsInsecureAndIncompleteConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  MailboxConfig
		ok   bool
	}{
		{"https", MailboxConfig{BaseURL: "https://mail.example.test", MailboxID: "m1", Token: "t"}, true},
		{"trailing slash", MailboxConfig{BaseURL: "https://mail.example.test/", MailboxID: "m1"}, true},
		{"loopback http is allowed for local development", MailboxConfig{BaseURL: "http://localhost:8080", MailboxID: "m1"}, true},
		{"plain http", MailboxConfig{BaseURL: "http://mail.example.test", MailboxID: "m1"}, false},
		{"no mailbox id", MailboxConfig{BaseURL: "https://mail.example.test"}, false},
		{"no base url", MailboxConfig{MailboxID: "m1"}, false},
		{"not a url", MailboxConfig{BaseURL: "mail.example.test", MailboxID: "m1"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewMailbox(tc.cfg)
			if tc.ok && err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if !tc.ok && !errors.Is(err, ErrBadConfig) {
				t.Fatalf("err = %v, want ErrBadConfig", err)
			}
		})
	}
}

func TestNewIMAP_RequiresCredentials(t *testing.T) {
	if _, err := NewIMAP(IMAPConfig{}); !errors.Is(err, ErrBadConfig) {
		t.Fatalf("err = %v, want ErrBadConfig", err)
	}
	src, err := NewIMAP(IMAPConfig{Host: "imap.example.test", User: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	// An entry saved without touching the port must still be dialable, so the zero value
	// has to land on the implicit-TLS port rather than on 0.
	if got := src.(*imapSource).cfg; got.Port != 993 || !got.UseTLS {
		t.Fatalf("defaults = port %d, tls %v; want 993 and TLS", got.Port, got.UseTLS)
	}
}

// A cancelled context must end the poll promptly. Without this the pre-warm goroutine
// started by a switch outlives the switch that started it.
func TestPollUntil_HonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := pollUntil(ctx, func(context.Context) (string, error) { return "", ErrNoCode })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// The first attempt must not wait for the first tick — the mail is very often already
// there, and a mandatory 5-second pause is the difference between instant and sluggish.
func TestPollUntil_TriesImmediately(t *testing.T) {
	start := time.Now()
	code, err := pollUntil(context.Background(), func(context.Context) (string, error) {
		return "4K7BX", nil
	})
	if err != nil || code != "4K7BX" {
		t.Fatalf("code = %q, err = %v", code, err)
	}
	if time.Since(start) > PollInterval {
		t.Fatalf("took %v; the immediate first attempt is not happening", time.Since(start))
	}
}

// A terminal error must stop the poll rather than being retried until the deadline. Retrying
// bad credentials on a 5-second timer is how an account gets locked out of its own mailbox.
func TestPollUntil_StopsOnTerminalError(t *testing.T) {
	calls := 0
	_, err := pollUntil(context.Background(), func(context.Context) (string, error) {
		calls++
		return "", ErrAuth
	})
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("err = %v, want ErrAuth", err)
	}
	if calls != 1 {
		t.Fatalf("called %d times, want 1 — auth failures must not be retried", calls)
	}
}
