// Package probe asks Steam's public Web API what it will say about an account, and decodes
// what the vault already holds offline.
//
// Everything here is read-only and cheap. Nothing in this package logs in, consumes a Guard
// code, or has a side effect on the account — that distinction is what makes it safe to run
// across every account at once, and it must stay true.
package probe

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"steamswitch/internal/appclient"
)

const apiBase = "https://api.steampowered.com"

var (
	ErrNoAPIKey    = errors.New("Toast_Vault_NoWebAPIKey")
	ErrRateLimited = errors.New("Toast_Vault_RateLimited")
	ErrRequest     = errors.New("Toast_Vault_ProbeFailed")
	ErrBadToken    = errors.New("Toast_Vault_TokenUnreadable")
)

// Bans is what GetPlayerBans reports.
type Bans struct {
	SteamID          string `json:"SteamId"`
	CommunityBanned  bool   `json:"CommunityBanned"`
	VACBanned        bool   `json:"VACBanned"`
	NumberOfVACBans  int    `json:"NumberOfVACBans"`
	DaysSinceLastBan int    `json:"DaysSinceLastBan"`
	NumberOfGameBans int    `json:"NumberOfGameBans"`
	EconomyBan       string `json:"EconomyBan"`
}

// Summary is the subset of GetPlayerSummaries that says anything about account health.
type Summary struct {
	SteamID        string `json:"steamid"`
	PersonaName    string `json:"personaname"`
	ProfileState   int    `json:"profilestate"`
	TimeCreated    int64  `json:"timecreated"`
	CommunityState int    `json:"communityvisibilitystate"`
	LastLogoff     int64  `json:"lastlogoff"`
}

// get issues a Web API request through the shared client, so offline mode blocks it at the
// transport layer rather than each caller having to remember.
func get(ctx context.Context, path string, q url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+path+"?"+q.Encode(), nil)
	if err != nil {
		return ErrRequest
	}
	resp, err := appclient.Shared.Do(req)
	if err != nil {
		return ErrRequest
	}
	defer func() { _ = resp.Body.Close() }()

	// 429 is the one status that must never be retried automatically. Backing off is the
	// documented difference between a warning and a block, and the caller has to see it as
	// its own condition to obey that.
	if resp.StatusCode == http.StatusTooManyRequests {
		return ErrRateLimited
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return ErrNoAPIKey
	}
	if resp.StatusCode >= 400 {
		return ErrRequest
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return ErrRequest
	}
	return nil
}

// FetchBans returns ban state for one account.
func FetchBans(ctx context.Context, apiKey, steamID64 string) (Bans, error) {
	if strings.TrimSpace(apiKey) == "" {
		return Bans{}, ErrNoAPIKey
	}
	var body struct {
		Players []Bans `json:"players"`
	}
	q := url.Values{"key": {apiKey}, "steamids": {steamID64}}
	if err := get(ctx, "/ISteamUser/GetPlayerBans/v1/", q, &body); err != nil {
		return Bans{}, err
	}
	if len(body.Players) == 0 {
		return Bans{}, ErrRequest
	}
	return body.Players[0], nil
}

// FetchSummary returns profile state for one account.
func FetchSummary(ctx context.Context, apiKey, steamID64 string) (Summary, error) {
	if strings.TrimSpace(apiKey) == "" {
		return Summary{}, ErrNoAPIKey
	}
	var body struct {
		Response struct {
			Players []Summary `json:"players"`
		} `json:"response"`
	}
	q := url.Values{"key": {apiKey}, "steamids": {steamID64}}
	if err := get(ctx, "/ISteamUser/GetPlayerSummaries/v2/", q, &body); err != nil {
		return Summary{}, err
	}
	if len(body.Response.Players) == 0 {
		return Summary{}, ErrRequest
	}
	return body.Response.Players[0], nil
}

// LimitedAccountAgeDays is the heuristic threshold for "probably still limited".
//
// Steam does not expose limited status through any public API. What it does expose is the
// creation date, and an account younger than this has almost certainly not cleared the $5
// spend threshold. It is reported as a warning with its reasoning visible, never as fact.
const LimitedAccountAgeDays = 30

// TokenClaims is the part of a Steam refresh token worth showing.
//
// Steam's refresh tokens are ordinary JWTs. The audience is what makes one dangerous: a
// client-audience token authenticates from another machine with no password and no Guard
// challenge, and the ip_subject/ip_confirmer claims it carries are not enforced.
type TokenClaims struct {
	Issuer      string    `json:"iss"`
	Subject     string    `json:"sub"`
	Audience    []string  `json:"aud"`
	ExpiresAt   time.Time `json:"exp"`
	IssuedAt    time.Time `json:"iat"`
	JTI         string    `json:"jti"`
	IPSubject   string    `json:"ip_subject"`
	IPConfirmer string    `json:"ip_confirmer"`
}

// DecodeToken reads a refresh token's claims. The signature is deliberately not verified:
// there is no public key to verify it against, and the purpose here is to report what the
// token says about itself, not to trust it.
func DecodeToken(token string) (TokenClaims, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return TokenClaims{}, ErrBadToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return TokenClaims{}, ErrBadToken
	}
	var raw struct {
		Iss         string          `json:"iss"`
		Sub         string          `json:"sub"`
		Aud         json.RawMessage `json:"aud"`
		Exp         int64           `json:"exp"`
		Iat         int64           `json:"iat"`
		JTI         string          `json:"jti"`
		IPSubject   string          `json:"ip_subject"`
		IPConfirmer string          `json:"ip_confirmer"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return TokenClaims{}, ErrBadToken
	}
	c := TokenClaims{
		Issuer:      raw.Iss,
		Subject:     raw.Sub,
		JTI:         raw.JTI,
		IPSubject:   raw.IPSubject,
		IPConfirmer: raw.IPConfirmer,
	}
	if raw.Exp > 0 {
		c.ExpiresAt = time.Unix(raw.Exp, 0).UTC()
	}
	if raw.Iat > 0 {
		c.IssuedAt = time.Unix(raw.Iat, 0).UTC()
	}
	// `aud` is a string or an array of strings depending on the token; both are valid JWT.
	if len(raw.Aud) > 0 {
		var list []string
		if err := json.Unmarshal(raw.Aud, &list); err == nil {
			c.Audience = list
		} else {
			var one string
			if err := json.Unmarshal(raw.Aud, &one); err == nil && one != "" {
				c.Audience = []string{one}
			}
		}
	}
	return c, nil
}

// IsClientAudience reports whether a token can be used to sign in as the account, as
// opposed to only reaching the website. This is the single most important thing to know
// before handing a token to anyone.
func (c TokenClaims) IsClientAudience() bool {
	for _, a := range c.Audience {
		if strings.EqualFold(strings.TrimSpace(a), "client") {
			return true
		}
	}
	return false
}

// Expired reports whether the token is past its own expiry.
func (c TokenClaims) Expired(now time.Time) bool {
	return !c.ExpiresAt.IsZero() && now.After(c.ExpiresAt)
}

// DescribeAudience renders the audience list for the debug panel.
func (c TokenClaims) DescribeAudience() string {
	if len(c.Audience) == 0 {
		return "—"
	}
	return fmt.Sprintf("[%s]", strings.Join(c.Audience, ", "))
}
