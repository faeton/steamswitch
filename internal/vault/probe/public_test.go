package probe

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

const publicID = "76561198000000100"

func TestParsePublicProfile(t *testing.T) {
	clean := []byte(`<?xml version="1.0" encoding="UTF-8"?><profile><steamID64>` + publicID +
		`</steamID64><vacBanned>0</vacBanned><tradeBanState>None</tradeBanState><isLimitedAccount>0</isLimitedAccount></profile>`)
	got, err := parsePublicProfile(clean, publicID)
	if err != nil {
		t.Fatal(err)
	}
	if got.VACBanned || got.Limited || got.TradeBan != "None" {
		t.Fatalf("clean profile = %+v", got)
	}

	banned := []byte(`<profile><steamID64>` + publicID +
		`</steamID64><vacBanned>1</vacBanned><tradeBanState>Banned</tradeBanState><isLimitedAccount>1</isLimitedAccount></profile>`)
	got, err = parsePublicProfile(banned, publicID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.VACBanned || !got.Limited || got.TradeBan != "Banned" {
		t.Fatalf("banned profile = %+v", got)
	}
}

// No answer must never read as a clean account.
func TestParsePublicProfile_NoAnswerIsAnError(t *testing.T) {
	cases := map[string]string{
		"html page":          `<!DOCTYPE html><html><body>not found</body></html>`,
		"error response":     `<response><error>The specified profile could not be found.</error></response>`,
		"no trade ban field": `<profile><steamID64>` + publicID + `</steamID64><vacBanned>0</vacBanned><isLimitedAccount>0</isLimitedAccount></profile>`,
		"fields missing":     `<profile><steamID64>` + publicID + `</steamID64></profile>`,
		"another account":    `<profile><steamID64>76561198000000999</steamID64><vacBanned>0</vacBanned><tradeBanState>None</tradeBanState><isLimitedAccount>0</isLimitedAccount></profile>`,
		"empty":              ``,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parsePublicProfile([]byte(raw), publicID); !errors.Is(err, ErrRequest) {
				t.Fatalf("err = %v, want ErrRequest", err)
			}
		})
	}
}

func TestFetchPublicProfile_RateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	old := communityBase
	communityBase = srv.URL
	defer func() { communityBase = old }()
	if _, err := FetchPublicProfile(context.Background(), publicID); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
}
