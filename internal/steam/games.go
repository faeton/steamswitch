package steam

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// InstalledGameInfo is one installed Steam app from libraryfolders / appmanifest scan.
type InstalledGameInfo struct {
	AppID string `json:"appId"`
	Name  string `json:"name"`
}

var reAppManifest = regexp.MustCompile(`(?i)^appmanifest_(\d+)\.acf$`)
var reQuotedPath = regexp.MustCompile(`"(?i)path"\s+"([^"]+)"`)

func steamAppsDirs(steamRoot string) ([]string, error) {
	steamRoot = filepath.Clean(steamRoot)
	out := []string{filepath.Join(steamRoot, "steamapps")}
	b, err := os.ReadFile(filepath.Join(steamRoot, "steamapps", "libraryfolders.vdf"))
	if err == nil {
		for _, m := range reQuotedPath.FindAllStringSubmatch(string(b), -1) {
			if len(m) < 2 {
				continue
			}
			p := filepath.Clean(platformExpandPath(m[1]))
			if p == "" {
				continue
			}
			out = append(out, filepath.Join(p, "steamapps"))
		}
	}
	seen := map[string]struct{}{}
	var uniq []string
	for _, d := range out {
		d = filepath.Clean(d)
		if _, ok := seen[d]; ok {
			continue
		}
		seen[d] = struct{}{}
		uniq = append(uniq, d)
	}
	return uniq, nil
}

func platformExpandPath(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, `\\`, `\`)
	return os.ExpandEnv(s)
}

func installedAppIDs(steamRoot string) (map[string]struct{}, error) {
	dirs, err := steamAppsDirs(steamRoot)
	if err != nil {
		return nil, err
	}
	ids := map[string]struct{}{}
	for _, dir := range dirs {
		ents, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if e.IsDir() {
				continue
			}
			m := reAppManifest.FindStringSubmatch(e.Name())
			if len(m) < 2 {
				continue
			}
			ids[m[1]] = struct{}{}
		}
	}
	return ids, nil
}

// BuildInstalledGamesList names installed games from Steam's own manifests.
func BuildInstalledGamesList(steamRoot string) ([]InstalledGameInfo, error) {
	installed, err := installedAppIDs(steamRoot)
	if err != nil {
		return nil, err
	}

	var list []InstalledGameInfo
	for id := range installed {
		nm := appManifestName(steamRoot, id)
		if nm == "" {
			nm = "App " + id
		}
		list = append(list, InstalledGameInfo{AppID: id, Name: nm})
	}
	sort.Slice(list, func(i, j int) bool {
		return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
	})
	return list, nil
}
