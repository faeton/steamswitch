package steam

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Jleagle/steam-go/steamvdf"
)

type LoginUser struct {
	SteamID64    string
	PersonaName  string
	AccountName  string
	Timestamp    string
	WantsOffline string
	// MostRecent is "1" when Steam marks this row as the active session (when present).
	MostRecent string
	// AutoLogin is the active-session marker in current Steam loginusers.vdf files.
	AutoLogin        string
	RememberPassword string
	SkipOfflineWarn  string // SkipOfflineModeWarning
}

func childStringCI(kv steamvdf.KeyValue, key string) string {
	klow := strings.ToLower(key)
	for _, ch := range kv.Children {
		if strings.ToLower(ch.Key) == klow {
			if ch.Value != "" {
				return ch.Value
			}
			if len(ch.Children) > 0 {
				return ch.String()
			}
		}
	}
	return ""
}

// ParseLoginUsers tries the file, then a sibling with .vdf_last extension.
func ParseLoginUsers(path string) ([]LoginUser, error) {
	try := func(p string) ([]LoginUser, error) {
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		raw = bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
		kv, err := steamvdf.ReadBytes(raw)
		if err != nil {
			return nil, err
		}
		usersKV, ok := kv.GetChild("users")
		if !ok {
			for _, ch := range kv.Children {
				if strings.EqualFold(ch.Key, "users") {
					usersKV = ch
					ok = true
					break
				}
			}
		}
		if !ok && len(kv.Children) > 0 && looksLikeSteamID64(kv.Children[0].Key) {
			usersKV = steamvdf.KeyValue{Children: kv.Children}
			ok = true
		}
		if !ok {
			return nil, nil
		}
		var out []LoginUser
		for _, u := range usersKV.Children {
			sid := strings.TrimSpace(u.Key)
			if sid == "" {
				continue
			}
			persona := childStringCI(u, "PersonaName")
			if persona == "" {
				persona = childStringCI(u, "personaname")
			}
			acc := childStringCI(u, "AccountName")
			if acc == "" {
				acc = childStringCI(u, "accountname")
			}
			if persona == "" && acc == "" {
				continue
			}
			ts := childStringCI(u, "Timestamp")
			off := childStringCI(u, "WantsOfflineMode")
			mr := childStringCI(u, "MostRecent")
			if mr == "" {
				mr = childStringCI(u, "mostrecent")
			}
			auto := childStringCI(u, "AutoLogin")
			rem := childStringCI(u, "RememberPassword")
			if rem == "" {
				rem = childStringCI(u, "rememberpassword")
			}
			skip := childStringCI(u, "SkipOfflineModeWarning")
			if skip == "" {
				skip = childStringCI(u, "skipofflinemodewarning")
			}
			out = append(out, LoginUser{
				SteamID64:        sid,
				PersonaName:      persona,
				AccountName:      acc,
				Timestamp:        ts,
				WantsOffline:     off,
				MostRecent:       mr,
				AutoLogin:        auto,
				RememberPassword: rem,
				SkipOfflineWarn:  skip,
			})
		}
		return out, nil
	}

	out, err := try(path)
	if err == nil && len(out) > 0 {
		return out, nil
	}
	alt := strings.TrimSuffix(path, ".vdf") + ".vdf_last"
	if st, e := os.Stat(alt); e == nil && !st.IsDir() {
		out2, err2 := try(alt)
		if err2 == nil && len(out2) > 0 {
			return out2, nil
		}
		if err == nil {
			err = err2
		}
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ActiveSessionSteamID64 prefers current Steam's AutoLogin marker, then legacy MostRecent.
func ActiveSessionSteamID64(users []LoginUser) string {
	id, _ := activeSession(users)
	return id
}

// activeSession is ActiveSessionSteamID64 with the one bit it throws away: whether the empty
// answer means "nobody is selected" or "more than one row claims to be".
//
// The distinction is the whole of REDESIGN_BRIEF A4. Collapsing both into "" made the UI say
// "not signed in" for a loginusers.vdf that names two most-recent accounts — an assertion the
// app has no basis for, on the one screen whose job is to be truthful about who is live.
func activeSession(users []LoginUser) (steamID64 string, ambiguous bool) {
	var autoLoginID string
	nAutoLogin := 0
	hasAutoLogin := false
	for _, u := range users {
		autoLogin := strings.TrimSpace(u.AutoLogin)
		if autoLogin != "" {
			hasAutoLogin = true
		}
		if autoLogin == "1" {
			nAutoLogin++
			autoLoginID = u.SteamID64
		}
	}
	if hasAutoLogin {
		if nAutoLogin == 1 && autoLoginID != "" {
			return autoLoginID, false
		}
		return "", nAutoLogin > 1
	}

	var mostRecentID string
	nMost := 0
	for _, u := range users {
		if strings.TrimSpace(u.MostRecent) == "1" {
			nMost++
			mostRecentID = u.SteamID64
		}
	}
	if nMost == 1 && mostRecentID != "" {
		return mostRecentID, false
	}
	return "", nMost > 1
}

// SessionState is how much the app actually knows about who Steam is signed in as.
//
// Frozen strings: they cross the bindings into the switcher's hero card, which renders one
// treatment per state.
type SessionState string

const (
	// SessionOK — one account is named and nothing contradicts it.
	SessionOK SessionState = "ok"
	// SessionNone — Steam has no account selected. The honest state after "Add another Steam
	// login", a failed switch, or a fresh install.
	SessionNone SessionState = "none"
	// SessionMismatch — Steam's own auto-login selection names an account that is not the one
	// loginusers.vdf points at, or names one with no row at all. Someone signed in outside
	// SteamSwitch, or a switch stopped half way.
	SessionMismatch SessionState = "mismatch"
	// SessionUnknown — the two sources disagree in a way that names nobody, or the file marks
	// several accounts as current. The app cannot pick one and must not pretend to.
	SessionUnknown SessionState = "unknown"
)

// SessionVerdict is the answer to "who is signed in", including how sure the app is.
type SessionVerdict struct {
	State SessionState `json:"state"`
	// SteamID64 is the account to present as current. Empty for none and unknown, and for a
	// mismatch where loginusers.vdf itself names nobody.
	SteamID64 string `json:"steamId64"`
	// ConflictAccountName is the login name Steam's own selection carries when it disagrees.
	// It is a Steam *account name*, not a persona — often the only thing the app knows about
	// an account it has never seen.
	ConflictAccountName string `json:"conflictAccountName,omitempty"`
}

// ResolveSession decides which of the A4 states the machine is in.
//
// Steam keeps this answer in two places that a switch writes together but that anything else
// can move apart: the per-user rows in `loginusers.vdf`, and `AutoLoginUser` in the registry
// (registry.vdf on macOS). Reading only the first is what made a stale "Signed in as X"
// possible — the file still named X while Steam was set to sign in as somebody else entirely.
//
// autoLoginErr is the backend's failure to read that second source. It is not an error here:
// a backend that cannot see the registry simply gives no opinion, and the file's answer stands
// unchallenged rather than being reported as a conflict it cannot substantiate.
func ResolveSession(users []LoginUser, autoLoginUser string, autoLoginErr error) SessionVerdict {
	fileID, ambiguous := activeSession(users)

	fallback := func() SessionVerdict {
		switch {
		case fileID != "":
			return SessionVerdict{State: SessionOK, SteamID64: fileID}
		case ambiguous:
			return SessionVerdict{State: SessionUnknown}
		default:
			return SessionVerdict{State: SessionNone}
		}
	}

	if autoLoginErr != nil {
		return fallback()
	}
	selected := strings.TrimSpace(autoLoginUser)
	if selected == "" {
		// No auto-login selection is the normal state with "remember password" off, so it is
		// not on its own evidence of anything wrong. The file still names who was last used.
		return fallback()
	}

	var matched []string
	for _, u := range users {
		if strings.EqualFold(strings.TrimSpace(u.AccountName), selected) {
			matched = append(matched, u.SteamID64)
		}
	}

	switch len(matched) {
	case 1:
		if fileID == "" {
			// The file could not decide and the registry can. Resolving the tie is strictly
			// better than reporting "unknown" when one source gives a clean answer.
			return SessionVerdict{State: SessionOK, SteamID64: matched[0]}
		}
		if fileID == matched[0] {
			return SessionVerdict{State: SessionOK, SteamID64: fileID}
		}
		return SessionVerdict{
			State:               SessionMismatch,
			SteamID64:           fileID,
			ConflictAccountName: selected,
		}
	case 0:
		// Steam will sign in as an account this machine has no row for. Nothing here is
		// trustworthy enough to show as current.
		return SessionVerdict{State: SessionMismatch, ConflictAccountName: selected}
	default:
		// Two rows share a login name. Rare, and not something to resolve by guessing.
		return SessionVerdict{State: SessionUnknown, ConflictAccountName: selected}
	}
}

func looksLikeSteamID64(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 15 || len(s) > 20 {
		return false
	}
	_, err := strconv.ParseUint(s, 10, 64)
	return err == nil
}

func LoginUsersFileExists(steamRoot string) bool {
	p := filepath.Join(steamRoot, "config", "loginusers.vdf")
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
