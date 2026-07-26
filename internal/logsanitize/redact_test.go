package logsanitize

import (
	"strings"
	"testing"
)

func TestAliasForAccount_trailingSpecials(t *testing.T) {
	got := aliasForAccount("account1", "kev_in!@#")
	if got != "account1!@#" {
		t.Fatalf("alias = %q, want account1!@#", got)
	}
}

func TestAliasForAccount_alphanumericOnly(t *testing.T) {
	got := aliasForAccount("account2", "76561198123456789")
	if got != "account2" {
		t.Fatalf("alias = %q, want account2", got)
	}
}

func TestReplaceCI_overlapping(t *testing.T) {
	s := replaceCI("ABCDEF", "abc", "X")
	if s != "XDEF" {
		t.Fatalf("got %q", s)
	}
}

func TestRedact_noAccounts(t *testing.T) {
	in := "path C:\\Users\\kevin\\file.txt"
	if got := Redact(in); got != in {
		t.Fatalf("Redact without accounts changed text: %q", got)
	}
}

func TestReplaceCI_preservesSurrounding(t *testing.T) {
	got := replaceCI(`failed copy C:\cache\Kevin\data`, "kevin", "account1")
	if !strings.Contains(strings.ToLower(got), "account1") {
		t.Fatalf("got %q", got)
	}
}

// The vault's email addresses live inside an encrypted blob this package cannot read, so
// they are caught by shape rather than by value. A crash report is the one thing that leaves
// the machine, and an address is both who the user is and — for bought accounts — where the
// accounts came from.
func TestRedactEmails(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "a plain address",
			in:   "imap: could not reach the mailbox for smurf1@example.test",
			want: "imap: could not reach the mailbox for email1@redacted",
		},
		{
			name: "the domain goes too",
			in:   "bound to buyer@bought-accounts-r-us.ltd",
			want: "bound to email1@redacted",
		},
		{
			name: "plus addressing and dots in the local part",
			in:   "first.last+steam@mail.example.co.uk",
			want: "email1@redacted",
		},
		{
			name: "an angle-bracket header keeps its shape",
			in:   `From: "Steam" <noreply@steampowered.com>`,
			want: `From: "Steam" <email1@redacted>`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := redactEmails(tc.in); got != tc.want {
				t.Fatalf("redactEmails(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// One address must read the same throughout a report. Numbering them independently would
// make "the code arrived at one inbox but the login failed for another" unreadable, which is
// the exact question a §V4 failure report has to answer.
func TestRedactEmails_IsStableWithinAReport(t *testing.T) {
	in := "a@one.test tried b@two.test then a@one.test again, and A@ONE.TEST once more"
	got := redactEmails(in)
	want := "email1@redacted tried email2@redacted then email1@redacted again, and email1@redacted once more"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Over-redaction costs readability; under-redaction leaks an address. These are the shapes
// that turn up in a Go crash report and must survive intact, or every stack trace becomes
// useless.
func TestRedactEmails_LeavesNonAddressesAlone(t *testing.T) {
	for _, in := range []string{
		"github.com/wailsapp/wails/v3@v3.0.0-alpha2.117/pkg/application/app.go:214",
		"steamswitch/internal/vault@v1.2.3",
		`C:\Users\kevin\AppData\Roaming\SteamSwitch`,
		"no address here at all",
		"user@host",
	} {
		if got := redactEmails(in); got != in {
			t.Fatalf("redactEmails(%q) = %q; it should have been left alone", in, got)
		}
	}
}

// Redact must apply both passes. With no identifiers on disk the account alias pass is a
// no-op, but the email pass never is.
func TestRedact_MasksEmailsWithNoKnownAccounts(t *testing.T) {
	const in = "vault: probe failed for smurf@example.test"
	got := Redact(in)
	if strings.Contains(got, "smurf@example.test") {
		t.Fatalf("Redact left the address in place: %q", got)
	}
	if !strings.Contains(got, "email1@redacted") {
		t.Fatalf("Redact = %q, want the address aliased", got)
	}
}
