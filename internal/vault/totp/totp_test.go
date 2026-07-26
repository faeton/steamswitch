package totp

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

// rfcKey is RFC 4226's test key, "12345678901234567890" in ASCII. Steam's scheme shares
// everything up to the dynamic truncation with HOTP, so the RFC's published intermediate
// value is a real external anchor for the half of this code that is not Valve-specific.
var rfcKey = []byte("12345678901234567890")

// RFC 4226 appendix D gives the dynamic binary code for counter 0 as 1284755224 (the
// 6-digit HOTP 755224 is that value mod 10^6). If the truncation drifts, this catches it
// without needing a Steam-issued code to compare against.
func TestTruncationMatchesRFC4226(t *testing.T) {
	got := dynamicTruncate(rfcKey, 0)
	if got != 1284755224 {
		t.Fatalf("dynamic truncation = %d, want RFC 4226's 1284755224", got)
	}
}

// The base-26 rendering on top of that anchor. Pinned so a change to the alphabet or to the
// digit order is a test failure rather than a batch of codes Steam quietly rejects.
func TestGenerate_PinnedAgainstTheRFCAnchor(t *testing.T) {
	secret := base64.StdEncoding.EncodeToString(rfcKey)
	code, err := Generate(secret, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if code != "GG5F5" {
		t.Fatalf("code = %q, want GG5F5", code)
	}
}

func TestGenerate_ShapeAndAlphabet(t *testing.T) {
	secret := base64.StdEncoding.EncodeToString(rfcKey)
	for i := range 200 {
		code, err := Generate(secret, time.Unix(int64(i)*31, 0))
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != CodeLength {
			t.Fatalf("code %q is %d chars, want %d", code, len(code), CodeLength)
		}
		for _, r := range code {
			if !strings.ContainsRune(alphabet, r) {
				// Steam rejects anything outside its alphabet, and the characters it
				// omits are exactly the ones users confuse — a stray O or 0 here would
				// be a support ticket, not a crash.
				t.Fatalf("code %q contains %q, which is not in Steam's alphabet", code, r)
			}
		}
	}
}

// Two calls inside one 30-second window must agree, and the next window must differ —
// otherwise the countdown in the UI is describing something that is not happening.
func TestGenerate_StableWithinAWindowAndChangesAcrossOne(t *testing.T) {
	secret := base64.StdEncoding.EncodeToString(rfcKey)
	// 1700000000 sits 20s into a window that began at ...__980 and ends at ...__010, so
	// these three straddle exactly one boundary.
	a, _ := Generate(secret, time.Unix(1_700_000_000, 0))
	b, _ := Generate(secret, time.Unix(1_700_000_009, 0))
	c, _ := Generate(secret, time.Unix(1_700_000_010, 0))
	if a != b {
		t.Fatalf("same window gave %q then %q", a, b)
	}
	if a == c {
		t.Fatalf("next window gave the same code %q", a)
	}
}

func TestDecodeSecret(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr error
	}{
		{"base64", base64.StdEncoding.EncodeToString(rfcKey), nil},
		{"hex", "31323334353637383930313233343536373839303132", nil},
		{"padded with spaces", "  " + base64.StdEncoding.EncodeToString(rfcKey) + "  ", nil},
		{"empty", "", ErrEmptySecret},
		{"blank", "   ", ErrEmptySecret},
		{"not an encoding at all", "!!!not-a-secret!!!", ErrInvalidSecret},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeSecret(tc.in)
			if err != tc.wantErr {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// Now must report time left in the window, not a constant. A zero or full-period answer
// would make the UI claim every code is fresh.
func TestNow_ReportsRemaining(t *testing.T) {
	secret := base64.StdEncoding.EncodeToString(rfcKey)
	_, remaining, err := Now(secret)
	if err != nil {
		t.Fatal(err)
	}
	if remaining <= 0 || remaining > Period {
		t.Fatalf("remaining = %v, want within (0, %v]", remaining, Period)
	}
}

func TestValid(t *testing.T) {
	if Valid("") {
		t.Fatal("an empty secret reported valid")
	}
	if !Valid(base64.StdEncoding.EncodeToString(rfcKey)) {
		t.Fatal("a good secret reported invalid")
	}
}
