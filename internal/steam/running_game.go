package steam

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// gameExitGrace is how long Steam gets to quit while a game is open. Measured with Dota under
// CrossOver, the client took 10 s because it closes the game first, which is the whole of the
// normal grace and would end in a force-kill for any game slower to exit.
const gameExitGrace = 30 * time.Second

// knownGameAppIDs are checked first, in this order, because they have process names to look for
// and so are still seen when Steam's own record is out of date.
var knownGameAppIDs = []string{DotaAppID, CS2AppID}

// runningGameName names a game that closing Steam would take down, or "" for none. The error
// means this build cannot tell; the switch itself refuses in that case.
func runningGameName(steamRoot string) (string, error) {
	if err := requireProcessInspection(); err != nil {
		return "", err
	}
	id := pickRunningApp(
		knownGameAppIDs,
		func(appID string) bool {
			running, err := gameIsRunning(steamRoot, appID)
			return err == nil && running
		},
		func() string {
			// A record left behind by a killed client must not raise a warning forever.
			if !steamClientRunning() {
				return ""
			}
			return backend.RunningAppID(steamRoot)
		},
	)
	if id == "" {
		return "", nil
	}
	return gameDisplayName(steamRoot, id), nil
}

// pickRunningApp prefers a known game, then whatever Steam says is running. "0" is how Steam
// records that nothing is.
func pickRunningApp(known []string, isRunning func(string) bool, steamReports func() string) string {
	for _, id := range known {
		if isRunning(id) {
			return id
		}
	}
	id := strings.TrimSpace(steamReports())
	if id == "0" {
		return ""
	}
	return id
}

func gameDisplayName(steamRoot, appID string) string {
	if p, ok := gameProfiles[appID]; ok && p.name != "" {
		return p.name
	}
	if name := appManifestName(steamRoot, appID); name != "" {
		return name
	}
	return "Steam app " + appID
}

// appManifestName reads a game's name from its manifest in any Steam library.
func appManifestName(steamRoot, appID string) string {
	libs, err := steamAppsDirs(steamRoot)
	if err != nil {
		return ""
	}
	for _, lib := range libs {
		data, err := os.ReadFile(filepath.Join(lib, "appmanifest_"+appID+".acf"))
		if err != nil {
			continue
		}
		if name := strings.TrimSpace(extractACFValue(string(data), "name")); name != "" {
			return name
		}
	}
	return ""
}

// closeExitGrace is the time Steam gets to act on a request to quit.
func closeExitGrace(base time.Duration, steamRoot string) time.Duration {
	name, err := runningGameName(steamRoot)
	return exitGraceFor(base, err == nil && name != "")
}

func exitGraceFor(base time.Duration, gameRunning bool) time.Duration {
	if gameRunning && gameExitGrace > base {
		return gameExitGrace
	}
	return base
}

// RunningGameName names a game that a switch would close, so the UI can ask first. Empty means
// none. It takes no lock so that it can answer while another service call is in progress.
func (s *SteamService) RunningGameName() (string, error) {
	root, err := s.steamInstallRoot()
	if err != nil {
		return "", err
	}
	return runningGameName(root)
}
