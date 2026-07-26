package steamauth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"strconv"
	"testing"
	"time"
)

// Steam's EResult is how a 200 response says "no". Two of these are worth pinning because
// guessing them wrongly sends the user in the opposite direction: 8 is "no such account",
// not "account disabled", and 84 must never be retried.
func TestEResultMapping(t *testing.T) {
	cases := []struct {
		code string
		want error
	}{
		{"", nil},
		{"0", nil},
		{"1", nil},
		{"  1  ", nil},
		{"5", ErrBadCredentials},
		{"8", ErrNoSuchAccount},
		{"15", ErrAccessDenied},
		{"20", ErrServiceDown},
		{"25", ErrRateLimited},
		{"65", ErrGuardRequired},
		{"84", ErrRateLimited},
		{"85", ErrGuardRequired},
		{"88", ErrGuardRejected},
		{"108", ErrSuspended},
		{"999", ErrRequestFailed},
		{"not a number", ErrRequestFailed},
	}
	for _, tc := range cases {
		got := eresultError(tc.code)
		if tc.want == nil {
			if got != nil {
				t.Fatalf("eresultError(%q) = %v, want nil", tc.code, got)
			}
			continue
		}
		if !errors.Is(got, tc.want) {
			t.Fatalf("eresultError(%q) = %v, want %v", tc.code, got, tc.want)
		}
	}
}

// A terminal condition must stop the poll loop. Retrying one is how a rate limit becomes a
// block and how a wrong password becomes a lockout.
func TestTerminal(t *testing.T) {
	for _, err := range []error{
		ErrRateLimited, ErrBadCredentials, ErrNoSuchAccount,
		ErrSuspended, ErrAccessDenied, ErrGuardRejected,
	} {
		if !terminal(err) {
			t.Fatalf("%v is not treated as terminal", err)
		}
	}
	// These mean "not yet", and treating them as failures would report "could not be
	// confirmed" for logins that were about to succeed.
	for _, err := range []error{ErrUnexpected, ErrRequestFailed, ErrServiceDown} {
		if terminal(err) {
			t.Fatalf("%v was treated as terminal; a transient blip would fail the login", err)
		}
	}
}

// Steam renders 64-bit ids as JSON strings. Without the `,string` tag these decode as zero,
// and every later call is silently about a session that does not exist.
func TestBeginResponse_DecodesStringNumbers(t *testing.T) {
	raw := `{"response":{
		"client_id":"12297829382473034410",
		"request_id":"3q2+7w==",
		"interval":5.5,
		"steamid":"76561198000000001",
		"allowed_confirmations":[{"confirmation_type":3,"associated_message":"authenticator"}]
	}}`
	var body beginResponse
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatal(err)
	}
	if body.Response.ClientID != 12297829382473034410 {
		t.Fatalf("client_id = %d; the ,string tag is missing or wrong", body.Response.ClientID)
	}
	if body.Response.Interval != 5.5 {
		t.Fatalf("interval = %v", body.Response.Interval)
	}
	if body.Response.SteamID != "76561198000000001" {
		t.Fatalf("steamid = %q", body.Response.SteamID)
	}
	if len(body.Response.AllowedConfirmations) != 1 ||
		body.Response.AllowedConfirmations[0].ConfirmationType != guardTypeDeviceCode {
		t.Fatalf("allowed_confirmations = %+v", body.Response.AllowedConfirmations)
	}
}

// request_id comes back as base64 in practice, but the field is declared as bytes and has
// been observed both ways. Getting it wrong makes every poll fail with no useful message.
func TestDecodeRequestID(t *testing.T) {
	want := []byte{0xde, 0xad, 0xbe, 0xef}

	b64, err := decodeRequestID(json.RawMessage(`"` + base64.StdEncoding.EncodeToString(want) + `"`))
	if err != nil {
		t.Fatal(err)
	}
	if string(b64) != string(want) {
		t.Fatalf("base64 form decoded to %x", b64)
	}

	arr, err := decodeRequestID(json.RawMessage(`[222,173,190,239]`))
	if err != nil {
		t.Fatal(err)
	}
	if string(arr) != string(want) {
		t.Fatalf("array form decoded to %x", arr)
	}

	// A non-base64 string is passed through rather than rejected: it is still an opaque
	// token as far as this code is concerned.
	plain, err := decodeRequestID(json.RawMessage(`"not-base64!!"`))
	if err != nil || string(plain) != "not-base64!!" {
		t.Fatalf("plain form = %q, %v", plain, err)
	}

	if _, err := decodeRequestID(nil); err == nil {
		t.Fatal("an empty request_id was accepted")
	}
	if _, err := decodeRequestID(json.RawMessage(`{"a":1}`)); err == nil {
		t.Fatal("an object request_id was accepted")
	}
}

// The password is RSA-encrypted under a per-account key Steam publishes as hex. PKCS#1 v1.5
// is Valve's choice, not a preference — the server accepts nothing else.
func TestEncryptPassword(t *testing.T) {
	// A small but valid RSA key, expressed the way Steam publishes one.
	key, err := generateTestKey()
	if err != nil {
		t.Fatal(err)
	}
	modHex := key.N.Text(16)
	expHex := big.NewInt(int64(key.E)).Text(16)

	ct, err := EncryptPassword("hunter2", modHex, expHex)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(ct)
	if err != nil {
		t.Fatalf("ciphertext is not base64: %v", err)
	}
	if len(raw) != key.Size() {
		t.Fatalf("ciphertext is %d bytes, want the modulus size %d", len(raw), key.Size())
	}
	// Two encryptions of the same password must differ: PKCS#1 v1.5 padding is randomised,
	// and identical output would mean the padding is not being applied.
	again, _ := EncryptPassword("hunter2", modHex, expHex)
	if again == ct {
		t.Fatal("two encryptions produced identical ciphertext")
	}

	for _, bad := range [][2]string{
		{"", "010001"},
		{"nothex", "010001"},
		{modHex, ""},
		{modHex, "nothex"},
		{modHex, "0"},
	} {
		if _, err := EncryptPassword("x", bad[0], bad[1]); err == nil {
			t.Fatalf("EncryptPassword accepted mod=%q exp=%q", bad[0], bad[1])
		}
	}
}

func TestTokenExpiry(t *testing.T) {
	exp := time.Now().Add(180 * 24 * time.Hour).Unix()
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":` + itoa(exp) + `}`))
	tok := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"EdDSA"}`)) + "." + payload + ".sig"

	got, ok := TokenExpiry(tok)
	if !ok {
		t.Fatal("a valid token's expiry was not read")
	}
	if got.Unix() != exp {
		t.Fatalf("exp = %v, want %v", got.Unix(), exp)
	}

	for _, bad := range []string{"", "a.b", "a.b.c.d", "a.!!!.c"} {
		if _, ok := TokenExpiry(bad); ok {
			t.Fatalf("TokenExpiry(%q) reported success", bad)
		}
	}
	// No exp claim is not the same as expired, and must not be reported as a time.
	noExp := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"x"}`)) + "." +
		base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"1"}`)) + ".sig"
	if _, ok := TokenExpiry(noExp); ok {
		t.Fatal("a token with no exp claim reported an expiry")
	}
}

func TestClient_DeviceNameIsHonest(t *testing.T) {
	// The device name lands in the account's authorised-devices list. It names this app
	// rather than impersonating the Steam client.
	c := &Client{}
	if got := c.deviceName(); got != "SteamSwitch" {
		t.Fatalf("default device name = %q", got)
	}
	if got := (&Client{DeviceName: "  "}).deviceName(); got != "SteamSwitch" {
		t.Fatalf("blank device name = %q", got)
	}
	if got := (&Client{DeviceName: "custom"}).deviceName(); got != "custom" {
		t.Fatalf("device name = %q", got)
	}
}

func generateTestKey() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, 2048)
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }
