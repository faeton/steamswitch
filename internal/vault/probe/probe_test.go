package probe

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// makeToken builds a JWT-shaped string with the given claims. The signature is filler: this
// package deliberately does not verify it, because there is no public key to verify against
// and the purpose is to report what the token says about itself.
func makeToken(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"typ":"JWT","alg":"EdDSA"}`))
	body, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(body) + ".c2lnbmF0dXJl"
}

func TestDecodeToken(t *testing.T) {
	exp := time.Now().Add(180 * 24 * time.Hour).Unix()
	tok := makeToken(t, map[string]any{
		"iss":          "r:0000_2000_A0A0A",
		"sub":          "76561198000000001",
		"aud":          []string{"client", "web", "renew", "derive"},
		"exp":          exp,
		"iat":          time.Now().Unix(),
		"jti":          "0001_2000_B0B0B",
		"ip_subject":   "203.0.113.7",
		"ip_confirmer": "203.0.113.7",
	})

	c, err := DecodeToken(tok)
	if err != nil {
		t.Fatal(err)
	}
	if c.Subject != "76561198000000001" {
		t.Fatalf("sub = %q", c.Subject)
	}
	if c.ExpiresAt.Unix() != exp {
		t.Fatalf("exp = %v", c.ExpiresAt)
	}
	if c.IPSubject != "203.0.113.7" {
		t.Fatalf("ip_subject = %q", c.IPSubject)
	}
	// This is the claim that makes a token dangerous to hand to anyone: a client-audience
	// token signs in from another machine with no password and no Guard challenge.
	if !c.IsClientAudience() {
		t.Fatal("a client-audience token was not recognised as one")
	}
	if c.Expired(time.Now()) {
		t.Fatal("a token six months from expiry reported as expired")
	}
}

// A web-only token cannot sign in as the account. Confusing it with a client token would
// make the handoff warnings wrong in the dangerous direction — understating access.
func TestIsClientAudience_WebOnly(t *testing.T) {
	tok := makeToken(t, map[string]any{"aud": []string{"web", "renew"}})
	c, err := DecodeToken(tok)
	if err != nil {
		t.Fatal(err)
	}
	if c.IsClientAudience() {
		t.Fatal("a web-only token reported as client-audience")
	}
}

// `aud` is a string or an array of strings depending on the token; both are valid JWT and
// both turn up.
func TestDecodeToken_ScalarAudience(t *testing.T) {
	c, err := DecodeToken(makeToken(t, map[string]any{"aud": "client"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Audience) != 1 || c.Audience[0] != "client" {
		t.Fatalf("audience = %v", c.Audience)
	}
	if !c.IsClientAudience() {
		t.Fatal("a scalar client audience was not recognised")
	}
}

func TestDecodeToken_Malformed(t *testing.T) {
	for _, bad := range []string{
		"",
		"not-a-token",
		"only.two",
		"a.b.c.d",
		"header.!!!not-base64!!!.sig",
		base64.RawURLEncoding.EncodeToString([]byte("{}")) + "." + base64.RawURLEncoding.EncodeToString([]byte("not json")) + ".sig",
	} {
		if _, err := DecodeToken(bad); !errors.Is(err, ErrBadToken) {
			t.Fatalf("DecodeToken(%q) = %v, want ErrBadToken", bad, err)
		}
	}
}

func TestExpired(t *testing.T) {
	past := makeToken(t, map[string]any{"exp": time.Now().Add(-time.Hour).Unix()})
	c, err := DecodeToken(past)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Expired(time.Now()) {
		t.Fatal("an expired token did not report as expired")
	}

	// A token with no exp claim must not report as expired — "no expiry stated" is not the
	// same as "expired", and treating it as such would warn about a working token.
	none, err := DecodeToken(makeToken(t, map[string]any{"sub": "1"}))
	if err != nil {
		t.Fatal(err)
	}
	if none.Expired(time.Now()) {
		t.Fatal("a token with no exp claim reported as expired")
	}
}

func TestDescribeAudience(t *testing.T) {
	c, _ := DecodeToken(makeToken(t, map[string]any{"aud": []string{"client", "web"}}))
	if got := c.DescribeAudience(); got != "[client, web]" {
		t.Fatalf("DescribeAudience = %q", got)
	}
	empty := TokenClaims{}
	if got := empty.DescribeAudience(); got != "—" {
		t.Fatalf("empty DescribeAudience = %q", got)
	}
}

// Without a key the ban and profile signals must report "unknown", not fail the whole
// check. An optional API key that breaks the feature when absent is not optional.
func TestFetchWithoutAPIKeyIsItsOwnCondition(t *testing.T) {
	ctx := t.Context()
	if _, err := FetchBans(ctx, "", "76561198000000001"); !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("FetchBans without a key = %v, want ErrNoAPIKey", err)
	}
	if _, err := FetchSummary(ctx, "  ", "76561198000000001"); !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("FetchSummary without a key = %v, want ErrNoAPIKey", err)
	}
}
