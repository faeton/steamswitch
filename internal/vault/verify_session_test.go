package vault

import (
	"context"
	"testing"

	"steamswitch/internal/vault/steamauth"
)

// classifySessionError must keep the fail-versus-unknown line the whole health feature depends
// on: a token Steam refuses is a dead account and blocks; a token Steam could not answer for is
// a network fact and must never read as dead, or a working account gets flagged (and maybe
// deleted) on a blip.
func TestClassifySessionError(t *testing.T) {
	base := Signal{Name: SignalSession, Status: VerdictUnknown, Detail: "Vault_Signal_SessionUnknown"}
	cases := []struct {
		name    string
		err     error
		status  string
		blocker bool
		detail  string
	}{
		{"revoked token → access denied", steamauth.ErrAccessDenied, VerdictFail, true, "Vault_Signal_SessionRevoked"},
		{"token no longer valid → bad credentials", steamauth.ErrBadCredentials, VerdictFail, true, "Vault_Signal_SessionRevoked"},
		{"account gone → no such account", steamauth.ErrNoSuchAccount, VerdictFail, true, "Vault_Signal_SessionRevoked"},
		{"rate limited is not a verdict on the account", steamauth.ErrRateLimited, VerdictUnknown, false, "Vault_Signal_RateLimited"},
		{"steam down is not a verdict on the account", steamauth.ErrServiceDown, VerdictUnknown, false, "Vault_Signal_SteamUnavailable"},
		{"timeout is not a verdict on the account", context.DeadlineExceeded, VerdictUnknown, false, "Vault_Signal_CheckTimedOut"},
		{"anything unmodelled stays unknown", steamauth.ErrRequestFailed, VerdictUnknown, false, "Vault_Signal_SessionUnknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifySessionError(base, tc.err)
			if got.Status != tc.status || got.Blocker != tc.blocker || got.Detail != tc.detail {
				t.Fatalf("classifySessionError(%v) = {%s blocker=%v %s}; want {%s blocker=%v %s}",
					tc.err, got.Status, got.Blocker, got.Detail, tc.status, tc.blocker, tc.detail)
			}
		})
	}

	// A rate-limited session probe must trip the process latch, exactly as a rate-limited login
	// does — the endpoint shares Steam's login rate limit.
	ResetRateLimitForTest()
	if RateLimited() {
		t.Fatal("latch should start clear")
	}
	classifySessionError(base, steamauth.ErrRateLimited)
	if !RateLimited() {
		t.Fatal("a rate-limited session probe should have latched the process rate limit")
	}
	ResetRateLimitForTest()
}
