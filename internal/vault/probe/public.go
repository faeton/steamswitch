package probe

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"strings"

	"steamswitch/internal/appclient"
)

// communityBase is a variable so tests can point it at a local server.
var communityBase = "https://steamcommunity.com"

// SetCommunityBaseForTest points the public profile lookup elsewhere and returns the undo. Tests
// outside this package need it because a check with no API key now goes to the network.
func SetCommunityBaseForTest(base string) (restore func()) {
	old := communityBase
	communityBase = base
	return func() { communityBase = old }
}

// maxPublicProfileBytes bounds what is read. A profile page is a few kilobytes.
const maxPublicProfileBytes = 1 << 20

// PublicProfile is what Steam shows anyone about an account, with no API key. It reports VAC and
// trade bans and limited status, but not game bans.
type PublicProfile struct {
	VACBanned bool
	// TradeBan is Steam's own wording, "None" when there is none.
	TradeBan string
	Limited  bool
}

// FetchPublicProfile reads an account's public profile.
//
// An unknown ID gets an HTML page and an unconfigured profile gets XML without these fields. Both
// are ErrRequest: not having an answer must never read as a clean account.
func FetchPublicProfile(ctx context.Context, steamID64 string) (PublicProfile, error) {
	u := communityBase + "/profiles/" + url.PathEscape(steamID64) + "/?xml=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return PublicProfile{}, ErrRequest
	}
	// appclient.Shared, never http.DefaultClient: offline mode is enforced at the transport.
	resp, err := appclient.Shared.Do(req)
	if err != nil {
		return PublicProfile{}, ErrRequest
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return PublicProfile{}, ErrRateLimited
	case resp.StatusCode != http.StatusOK:
		return PublicProfile{}, ErrRequest
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxPublicProfileBytes))
	if err != nil {
		return PublicProfile{}, ErrRequest
	}
	return parsePublicProfile(raw, steamID64)
}

func parsePublicProfile(raw []byte, steamID64 string) (PublicProfile, error) {
	var doc struct {
		XMLName   xml.Name `xml:"profile"`
		SteamID64 string   `xml:"steamID64"`
		VACBanned *string  `xml:"vacBanned"`
		TradeBan  *string  `xml:"tradeBanState"`
		Limited   *string  `xml:"isLimitedAccount"`
	}
	if err := xml.Unmarshal(raw, &doc); err != nil {
		return PublicProfile{}, ErrRequest
	}
	if strings.TrimSpace(doc.SteamID64) != steamID64 || doc.VACBanned == nil || doc.Limited == nil || doc.TradeBan == nil {
		return PublicProfile{}, ErrRequest
	}
	return PublicProfile{
		VACBanned: strings.TrimSpace(*doc.VACBanned) != "0",
		TradeBan:  strings.TrimSpace(*doc.TradeBan),
		Limited:   strings.TrimSpace(*doc.Limited) != "0",
	}, nil
}
