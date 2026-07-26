// Package totp generates Steam Guard mobile-authenticator codes.
//
// Steam's scheme is RFC 6238 up to the dynamic truncation, then diverges: instead of
// rendering the truncated integer as decimal digits it renders it in base 26 over an
// alphabet chosen to avoid characters that are easy to misread aloud.
package totp

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// Steam's alphabet: no 0/1/A/E/I/O/S/U/L/Z, so codes cannot be misheard or misread as each
// other. Order is load-bearing — it is the digit ordering of the base-26 rendering.
const alphabet = "23456789BCDFGHJKMNPQRTVWXY"

// Period is the code lifetime. Codes are single-use in practice, but Steam accepts the
// current and adjacent windows.
const Period = 30 * time.Second

// CodeLength is fixed by Steam; it is not a parameter.
const CodeLength = 5

var (
	ErrEmptySecret   = errors.New("Toast_Vault_NoSharedSecret")
	ErrInvalidSecret = errors.New("Toast_Vault_BadSharedSecret")
)

// DecodeSecret accepts a shared secret in the two encodings it is found in: standard base64
// (what Steam's own files and every extraction tool emit) and hex (what a few tools emit
// instead). Both are accepted because rejecting the wrong one produces a mystifying failure
// at code-generation time rather than at entry time.
func DecodeSecret(secret string) ([]byte, error) {
	s := strings.TrimSpace(secret)
	if s == "" {
		return nil, ErrEmptySecret
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) > 0 {
		return b, nil
	}
	if b, err := hex.DecodeString(s); err == nil && len(b) > 0 {
		return b, nil
	}
	return nil, ErrInvalidSecret
}

// Generate returns the code for the window containing t.
func Generate(secret string, t time.Time) (string, error) {
	key, err := DecodeSecret(secret)
	if err != nil {
		return "", err
	}
	return generateFromKey(key, uint64(t.Unix())/uint64(Period.Seconds())), nil
}

// Now returns the code for the current window, plus how long it remains valid. The
// remaining time is what lets the UI show a countdown instead of a code that silently
// expires while the user is typing it.
func Now(secret string) (code string, remaining time.Duration, err error) {
	t := time.Now()
	code, err = Generate(secret, t)
	if err != nil {
		return "", 0, err
	}
	elapsed := time.Duration(t.Unix()%int64(Period.Seconds())) * time.Second
	return code, Period - elapsed, nil
}

// dynamicTruncate is RFC 4226's DT, shared verbatim with HOTP/TOTP: HMAC-SHA1 the counter,
// let the low nibble of the last byte pick a 4-byte offset, and mask the high bit so the
// result is unsigned. Steam diverges only in how the result is rendered.
func dynamicTruncate(key []byte, counter uint64) uint32 {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)

	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	return binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
}

func generateFromKey(key []byte, counter uint64) string {
	value := dynamicTruncate(key, counter)
	out := make([]byte, CodeLength)
	for i := range out {
		out[i] = alphabet[value%uint32(len(alphabet))]
		value /= uint32(len(alphabet))
	}
	return string(out)
}

// Valid reports whether a secret decodes, without generating a code. Used by the health
// check to say whether the fastest rung of the code ladder is available for an account.
func Valid(secret string) bool {
	_, err := DecodeSecret(secret)
	return err == nil
}
