package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"steamswitch/internal/security"
)

var (
	jsonUnmarshal = json.Unmarshal
	securityOpen  = security.Open
)

const testPassphrase = "seven unrelated words make a decent passphrase"

func sampleRoster() RosterPayload {
	return RosterPayload{
		Version: 1,
		Accounts: []RosterRecord{
			{SteamID64: "76561198000000001", AccountName: "one", Password: "pw-one"},
			{SteamID64: "76561198000000002", AccountName: "two", SharedSecret: "seed-two"},
		},
	}
}

func TestSealOpenRoster_RoundTrip(t *testing.T) {
	sealed, err := SealRoster(sampleRoster(), testPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	// Nothing about the accounts may be legible in the envelope. A roster on a shared drive
	// should say only that somebody uses SteamSwitch.
	for _, leak := range []string{"76561198000000001", "one", "pw-one", "seed-two"} {
		if bytes.Contains(sealed, []byte(leak)) {
			t.Fatalf("sealed roster leaks %q in cleartext", leak)
		}
	}

	got, err := OpenRoster(sealed, testPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Accounts) != 2 || got.Accounts[0].Password != "pw-one" || got.Accounts[1].SharedSecret != "seed-two" {
		t.Fatalf("round trip lost data: %+v", got)
	}
}

func TestOpenRoster_WrongPassphrase(t *testing.T) {
	sealed, err := SealRoster(sampleRoster(), testPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenRoster(sealed, testPassphrase+"x"); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("err = %v, want ErrWrongPassphrase", err)
	}
}

// TestRosterAndHandoffAreNotInterchangeable pins the distinct AAD. Two formats that share a
// KDF and a cipher but mean different things must not open as one another even when the user
// reuses one passphrase across both.
func TestRosterAndHandoffAreNotInterchangeable(t *testing.T) {
	sealed, err := SealRoster(sampleRoster(), testPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	// openBundle expects a file; the envelope shape is identical, so the only thing that can
	// reject it is the AAD.
	var sr sealedRoster
	if err := jsonUnmarshal(sealed, &sr); err != nil {
		t.Fatal(err)
	}
	salt, _ := decodeB64(sr.Salt)
	nonce, _ := decodeB64(sr.Nonce)
	ct, _ := decodeB64(sr.Ciphertext)
	key := deriveBundleKey(testPassphrase, salt, sr.KDF)
	if _, err := securityOpen(key, nonce, ct, []byte(bundleAAD)); err == nil {
		t.Fatal("a roster opened under the handoff AAD; the two formats are interchangeable")
	}
}

func TestSealRoster_RejectsAShortPassphrase(t *testing.T) {
	if _, err := SealRoster(sampleRoster(), "short"); !errors.Is(err, ErrNoPassphrase) {
		t.Fatalf("err = %v, want ErrNoPassphrase", err)
	}
}

func TestParseRosterPlaintext_Shapes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{
			name: "the documented wrapper",
			in:   `{"version":1,"accounts":[{"steamId64":"76561198000000001"}]}`,
			want: 1,
		},
		{
			// What an agent asked for "the accounts" writes about half the time. Refusing it
			// would send someone back to hand-editing a file full of passwords.
			name: "a bare array",
			in:   `[{"steamId64":"76561198000000001"},{"steamId64":"76561198000000002"}]`,
			want: 2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseRosterPlaintext([]byte(tc.in))
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Accounts) != tc.want {
				t.Fatalf("parsed %d accounts, want %d", len(got.Accounts), tc.want)
			}
		})
	}
}

func TestParseRosterCSV(t *testing.T) {
	csv := "SteamID64,Login,Password,TOTP,Email,Email Password,IMAP Host\n" +
		"76561198000000001,one,pw-one,seed-one,one@example.test,mailpw,imap.example.test\n" +
		"76561198000000002,two,pw-two,,,,\n"

	got, err := ParseRosterPlaintext([]byte(csv))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Accounts) != 2 {
		t.Fatalf("parsed %d rows, want 2", len(got.Accounts))
	}
	first := got.Accounts[0]
	if first.AccountName != "one" || first.Password != "pw-one" || first.SharedSecret != "seed-one" {
		t.Fatalf("row 1 = %+v", first)
	}
	if first.Email == nil || first.Email.Source != EmailSourceIMAP || first.Email.IMAP == nil {
		t.Fatalf("row 1 email = %+v", first.Email)
	}
	if first.Email.IMAP.Port != 993 || !first.Email.IMAP.UseTLS {
		t.Fatalf("IMAP defaults not applied: %+v", first.Email.IMAP)
	}
	if got.Accounts[1].Email != nil {
		t.Fatalf("row 2 should have no email binding, got %+v", got.Accounts[1].Email)
	}
}

// TestParseRosterCSV_RefusesAnUnknownHeader — guessing column order for a file of credentials
// is how a password lands in the email field.
func TestParseRosterCSV_RefusesAnUnknownHeader(t *testing.T) {
	if _, err := ParseRosterPlaintext([]byte("a,b,c\n1,2,3\n")); !errors.Is(err, ErrBadRoster) {
		t.Fatalf("err = %v, want ErrBadRoster", err)
	}
}

func TestValidateRosterRecord(t *testing.T) {
	cases := []struct {
		id      string
		wantBad bool
	}{
		{"76561198000000001", false},
		{"", true},
		{"not-a-number", true},
		{"123", true},
		{"76561197960265727", true}, // one below the individual-account base
	}
	for _, tc := range cases {
		got := validateRosterRecord(RosterRecord{SteamID64: tc.id})
		if (got != "") != tc.wantBad {
			t.Errorf("validateRosterRecord(%q) = %q, wantBad=%v", tc.id, got, tc.wantBad)
		}
	}
}

func TestSealRosterStream(t *testing.T) {
	in := strings.NewReader(`{"passphrase":"` + testPassphrase + `","accounts":[{"steamId64":"76561198000000001","password":"pw"}]}`)
	var out bytes.Buffer
	if err := SealRosterStream(in, &out); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out.Bytes(), []byte("pw")) || bytes.Contains(out.Bytes(), []byte(testPassphrase)) {
		t.Fatal("sealed output contains plaintext")
	}
	got, err := OpenRoster(out.Bytes(), testPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Accounts) != 1 || got.Accounts[0].Password != "pw" {
		t.Fatalf("round trip = %+v", got)
	}
	if got.IssuedAt == "" {
		t.Fatal("IssuedAt should be stamped when the caller omits it")
	}
}

func TestSealRosterStream_RejectsEmptyInput(t *testing.T) {
	var out bytes.Buffer
	if err := SealRosterStream(strings.NewReader(""), &out); !errors.Is(err, ErrRosterEmpty) {
		t.Fatalf("err = %v, want ErrRosterEmpty", err)
	}
}
