package steam

import (
	"strconv"
	"time"

	"steamswitch/internal/platform"
)

// This file is the one-way bridge from the Steam engine to the account vault.
//
// The dependency arrow points this way on purpose: `internal/vault` must not import
// `internal/steam`. The vault is a store of secrets keyed by SteamID64 and knows nothing
// about switching; making it read the engine's files would tie a secrets store to a
// platform engine for the sake of one timestamp.

// LastLoginTimes reports when Steam last recorded a login for each known account, read from
// `loginusers.vdf`.
//
// This is the closest thing to "last used" that exists without the vault reading the
// engine's own files. It is Steam's own record, so it also counts logins that happened
// outside this app — which is what makes it useful for spotting an idle account.
// A failure to resolve anything returns an empty map rather than an error: an unknown last
// login is reported as the "never used" signal, which is honest, and is not a reason to
// fail the health check that asked.
func LastLoginTimes() map[string]time.Time {
	out := map[string]time.Time{}

	exeDir, err := platform.ResolveExeDir()
	if err != nil {
		return out
	}
	app, err := platform.LoadAppSettings(exeDir)
	if err != nil {
		return out
	}
	st, err := LoadSettings()
	if err != nil {
		return out
	}
	raw, err := platform.LoadPlatformsJSON(exeDir)
	if err != nil {
		return out
	}
	root, err := ResolveInstallFolder(exeDir, st, app, raw)
	if err != nil || root == "" {
		return out
	}
	users, err := ParseLoginUsers(LoginUsersPath(root))
	if err != nil {
		return out
	}
	for _, u := range users {
		sec, err := strconv.ParseInt(u.Timestamp, 10, 64)
		if err != nil || sec <= 0 {
			continue
		}
		out[u.SteamID64] = time.Unix(sec, 0)
	}
	return out
}
