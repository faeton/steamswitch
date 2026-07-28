package steam

import (
	"errors"
	"testing"
)

func user(id, name, mostRecent string) LoginUser {
	return LoginUser{SteamID64: id, AccountName: name, MostRecent: mostRecent}
}

// TestResolveSession covers every A4 state, because the point of the state machine is that the
// hero never asserts a current account it cannot substantiate.
func TestResolveSession(t *testing.T) {
	errNoRegistry := errors.New("no registry here")

	cases := []struct {
		name         string
		users        []LoginUser
		autoLogin    string
		autoLoginErr error
		wantState    SessionState
		wantID       string
		wantConflict string
	}{
		{
			name:      "empty machine is none, not unknown",
			users:     nil,
			wantState: SessionNone,
		},
		{
			name:      "one most-recent row and a matching selection is ok",
			users:     []LoginUser{user("1", "alice", "1"), user("2", "bob", "0")},
			autoLogin: "alice",
			wantState: SessionOK,
			wantID:    "1",
		},
		{
			name:      "selection is matched case-insensitively",
			users:     []LoginUser{user("1", "Alice", "1")},
			autoLogin: "alice",
			wantState: SessionOK,
			wantID:    "1",
		},
		{
			// Somebody signed in through Steam itself. The file still names alice; Steam is
			// set to bob. Asserting alice is the exact stale-but-confident failure A4 names.
			name:         "the file and the selection naming different accounts is a mismatch",
			users:        []LoginUser{user("1", "alice", "1"), user("2", "bob", "0")},
			autoLogin:    "bob",
			wantState:    SessionMismatch,
			wantID:       "1",
			wantConflict: "bob",
		},
		{
			// An external login to an account this machine has never switched to.
			name:         "a selection with no row at all is a mismatch naming nobody",
			users:        []LoginUser{user("1", "alice", "1")},
			autoLogin:    "carol",
			wantState:    SessionMismatch,
			wantID:       "",
			wantConflict: "carol",
		},
		{
			// The improvement that falls out for free: the file cannot decide, the registry
			// can, so the app resolves rather than reporting a shrug.
			name:      "the selection breaks a tie the file cannot",
			users:     []LoginUser{user("1", "alice", "1"), user("2", "bob", "1")},
			autoLogin: "bob",
			wantState: SessionOK,
			wantID:    "2",
		},
		{
			name:      "two most-recent rows and no selection is unknown",
			users:     []LoginUser{user("1", "alice", "1"), user("2", "bob", "1")},
			wantState: SessionUnknown,
		},
		{
			name:         "two rows sharing a login name is unknown, not a guess",
			users:        []LoginUser{user("1", "alice", "0"), user("2", "alice", "0")},
			autoLogin:    "alice",
			wantState:    SessionUnknown,
			wantConflict: "alice",
		},
		{
			// After "Add another Steam login": every row cleared, selection deleted.
			name:      "nothing selected anywhere is none",
			users:     []LoginUser{user("1", "alice", "0"), user("2", "bob", "0")},
			wantState: SessionNone,
		},
		{
			// Remember-password off. Steam shows the picker, but the file still records who
			// was last used, and warning about that on every launch would be crying wolf.
			name:      "no selection with a most-recent row is still ok",
			users:     []LoginUser{user("1", "alice", "1")},
			wantState: SessionOK,
			wantID:    "1",
		},
		{
			// A backend that cannot read the second source gives no opinion. It must not
			// manufacture a conflict out of its own blindness.
			name:         "an unreadable selection falls back to the file",
			users:        []LoginUser{user("1", "alice", "1")},
			autoLogin:    "",
			autoLoginErr: errNoRegistry,
			wantState:    SessionOK,
			wantID:       "1",
		},
		{
			name:         "an unreadable selection over an ambiguous file is unknown",
			users:        []LoginUser{user("1", "alice", "1"), user("2", "bob", "1")},
			autoLoginErr: errNoRegistry,
			wantState:    SessionUnknown,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveSession(tc.users, tc.autoLogin, tc.autoLoginErr)
			if got.State != tc.wantState {
				t.Errorf("State = %q, want %q", got.State, tc.wantState)
			}
			if got.SteamID64 != tc.wantID {
				t.Errorf("SteamID64 = %q, want %q", got.SteamID64, tc.wantID)
			}
			if got.ConflictAccountName != tc.wantConflict {
				t.Errorf("ConflictAccountName = %q, want %q", got.ConflictAccountName, tc.wantConflict)
			}
		})
	}
}

// TestResolveSession_AutoLoginFieldStillWins pins that the newer per-row AutoLogin marker keeps
// precedence over legacy MostRecent, which ActiveSessionSteamID64 has always done.
func TestResolveSession_AutoLoginFieldStillWins(t *testing.T) {
	users := []LoginUser{
		{SteamID64: "1", AccountName: "alice", AutoLogin: "0", MostRecent: "1"},
		{SteamID64: "2", AccountName: "bob", AutoLogin: "1", MostRecent: "0"},
	}
	got := ResolveSession(users, "bob", nil)
	if got.State != SessionOK || got.SteamID64 != "2" {
		t.Fatalf("ResolveSession = %+v, want ok/2", got)
	}
}

// TestActiveSessionSteamID64_UnchangedByTheVerdict guards the callers that still ask only for
// an id (the switcher, the tray, the CLI): adding the verdict must not have moved that answer.
func TestActiveSessionSteamID64_UnchangedByTheVerdict(t *testing.T) {
	cases := []struct {
		users []LoginUser
		want  string
	}{
		{nil, ""},
		{[]LoginUser{user("1", "alice", "1")}, "1"},
		{[]LoginUser{user("1", "alice", "1"), user("2", "bob", "1")}, ""},
		{[]LoginUser{user("1", "alice", "0")}, ""},
	}
	for _, tc := range cases {
		if got := ActiveSessionSteamID64(tc.users); got != tc.want {
			t.Errorf("ActiveSessionSteamID64(%v) = %q, want %q", tc.users, got, tc.want)
		}
	}
}
