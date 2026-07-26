package steam

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"steamswitch/internal/fsutil"
	"steamswitch/internal/paths"
	"steamswitch/internal/platform"

	"github.com/tidwall/gjson"
)

const settingsFileName = "SteamSettings.json"

// Settings adds Steam-only fields; shared options are embedded from platform.PlatformSettings.
type Settings struct {
	platform.PlatformSettings

	FolderPath string `json:"FolderPath"`

	SteamShowSteamID     bool `json:"Steam_ShowSteamID"`
	SteamShowVAC         bool `json:"Steam_ShowVAC"`
	SteamShowLimited     bool `json:"Steam_ShowLimited"`
	SteamShowLastLogin   bool `json:"Steam_ShowLastLogin"`
	SteamShowAccUsername bool `json:"Steam_ShowAccUsername"`
	SteamTrayAccountName bool `json:"Steam_TrayAccountName"`

	SteamImageExpiryTime int `json:"Steam_ImageExpiryTime"`
	SteamOverrideState   int `json:"Steam_OverrideState"`

	// SteamAutoRefreshOnLaunch runs a profile/avatar/VAC refresh shortly after the app
	// starts, so the account list is current without a manual refresh.
	// Stored without omitempty so an explicit opt-out survives a restart.
	SteamAutoRefreshOnLaunch bool `json:"Steam_AutoRefreshOnLaunch"`

	// SteamAutoRefreshIntervalMinutes re-runs that refresh on a timer while the app is
	// open. 0 disables the timer; anything below SteamAutoRefreshMinMinutes is clamped
	// up to it so the app cannot be configured to hammer the Steam community pages.
	SteamAutoRefreshIntervalMinutes int `json:"Steam_AutoRefreshIntervalMinutes"`

	ShortcutsJSON map[string]string `json:"ShortcutsJson,omitempty"`

	SteamWebAPIKey string `json:"SteamWebApiKey"`

	ShowSteamSwitcher bool `json:"ShowSteamSwitcher"`
	CollectInfo       bool `json:"CollectInfo"`

	// SteamRememberPassword controls whether a switch tells Steam to keep the target
	// account signed in (`RememberPassword` in loginusers.vdf, and the matching auto-login
	// selector value).
	//
	// Defaults to true, which is what you want on a machine you own: switching then lands
	// you straight in. Turn it off on a shared or public machine — Steam will ask for the
	// password on every launch, and no session is left behind for the next person.
	//
	// This only governs what a switch *writes*. It cannot reach back and delete credentials
	// Steam has already cached for accounts that were signed in before it was turned off.
	SteamRememberPassword bool `json:"Steam_RememberPassword"`

	SteamShowMiniProfile bool `json:"Steam_ShowMiniProfile"`
	SteamShowAvatarFrame bool `json:"Steam_ShowAvatarFrame"`

	// HomeSteamID64 is the primary account for this machine — the single source of every
	// Session Kit (REDESIGN.md §2). Empty until the user nominates one.
	HomeSteamID64 string `json:"HomeSteamId64,omitempty"`

	// SharedSteamIDs are accounts other people also use. Switching *to* one carries the
	// Home account's kit along; leaving one prompts to put their setup back.
	SharedSteamIDs []string `json:"SharedSteamIds,omitempty"`
}

// IsShared reports whether the given account is marked as shared with someone else.
func (s Settings) IsShared(steamID64 string) bool {
	id := strings.TrimSpace(steamID64)
	if id == "" {
		return false
	}
	for _, v := range s.SharedSteamIDs {
		if strings.TrimSpace(v) == id {
			return true
		}
	}
	return false
}

// IsHome reports whether the given account is the machine's Home account.
func (s Settings) IsHome(steamID64 string) bool {
	id := strings.TrimSpace(steamID64)
	return id != "" && strings.TrimSpace(s.HomeSteamID64) == id
}

func defaultSettings() Settings {
	ps := platform.DefaultPlatformSettings()
	return Settings{
		PlatformSettings:     ps,
		FolderPath:           `C:\Program Files (x86)\Steam\`,
		SteamShowVAC:         true,
		SteamShowLimited:     true,
		SteamShowLastLogin:   true,
		SteamShowAccUsername: true,
		SteamShowSteamID:     false,
		SteamImageExpiryTime: 7,
		SteamOverrideState:   -1,
		ShortcutsJSON:        nil,
		CollectInfo:          true,
		SteamShowMiniProfile: true,
		SteamShowAvatarFrame: true,

		SteamAutoRefreshOnLaunch:        true,
		SteamAutoRefreshIntervalMinutes: 0,
		SteamRememberPassword:           true,
	}
}

func settingsPath() (string, error) {
	dir, err := paths.SettingsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, settingsFileName), nil
}

// ResetToDefaults overwrites SteamSettings.json with defaults (wired from [platform.SetSteamReset]).
func ResetToDefaults() error {
	return SaveSettings(defaultSettings())
}

// boolWithDefault reads a JSON bool that defaults to something other than false.
//
// `json.Unmarshal` cannot express this: an absent key and an explicit `false` both leave the
// field zero. For an option that ships enabled, that difference is the whole story — a
// settings file written before the option existed must keep the default, while a user who
// deliberately turned it off must not have it switched back on at every launch.
func boolWithDefault(data []byte, key string, def bool) bool {
	if r := gjson.GetBytes(data, key); r.Exists() {
		return r.Bool()
	}
	return def
}

// LoadSettings reads Settings/SteamSettings.json.
func LoadSettings() (Settings, error) {
	path, err := settingsPath()
	if err != nil {
		return Settings{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultSettings(), nil
		}
		return Settings{}, err
	}

	// Migrate legacy JSON keys into embedded PlatformSettings before unmarshalling.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return defaultSettings(), err
	}
	if _, has := raw["RunAsAdmin"]; !has {
		if v, ok := raw["Steam_Admin"]; ok {
			raw["RunAsAdmin"] = v
		}
		delete(raw, "Steam_Admin")
	}
	if _, has := raw["TrayAccNumber"]; !has {
		if v, ok := raw["Steam_TrayAccNumber"]; ok {
			raw["TrayAccNumber"] = v
		}
		delete(raw, "Steam_TrayAccNumber")
	}
	// Accept lowercase shortcuts key (some exports / hand edits); Go tag is "Shortcuts".
	if _, has := raw["Shortcuts"]; !has {
		if v, ok := raw["shortcuts"]; ok {
			raw["Shortcuts"] = v
			delete(raw, "shortcuts")
		}
	}
	// Legacy StartSilent bool → LaunchArguments token (-silent); key dropped on save.
	legacySilent := jsonRawMessageBool(raw["StartSilent"])
	delete(raw, "StartSilent")
	delete(raw, "OldUi")
	data2, err := json.Marshal(raw)
	if err != nil {
		return Settings{}, err
	}

	var s Settings
	if err := json.Unmarshal(data2, &s); err != nil {
		return defaultSettings(), err
	}
	if legacySilent {
		s.LaunchArguments = platform.EnsureLaunchArg(s.LaunchArguments, "-silent")
	}
	s.AlwaysSwapOnShortcut = boolWithDefault(data2, "AlwaysSwapOnShortcut", true)
	s.SteamRememberPassword = boolWithDefault(data2, "Steam_RememberPassword", true)
	s.FolderPath = NormalizeFolderPath(s.FolderPath)
	if len(s.Shortcuts) == 0 && len(s.ShortcutsJSON) > 0 {
		s.Shortcuts = migrateLegacyShortcutsJSON(s.ShortcutsJSON)
		s.ShortcutsJSON = nil
	}
	if s.Shortcuts == nil {
		s.Shortcuts = []platform.GameShortcutEntry{}
	}
	if s.AccountNotes == nil {
		s.AccountNotes = map[string]string{}
	}
	if s.SteamImageExpiryTime <= 0 {
		s.SteamImageExpiryTime = 7
	}
	// Honor explicit 0 to disable tray MRU; default to 3 when the key was absent from JSON.
	if gjson.GetBytes(data, "TrayAccNumber").Exists() || gjson.GetBytes(data, "Steam_TrayAccNumber").Exists() {
		// keep unmarshaled TrayAccNumber (including 0)
	} else if s.TrayAccNumber <= 0 {
		s.TrayAccNumber = 3
	}
	if strings.TrimSpace(s.ClosingMethod) == "" {
		s.ClosingMethod = "Combined"
	}
	s.ClosingMethod = platform.NormalizeClosingMethod(s.ClosingMethod)
	defClose, forceClose := platform.DescriptorClosingPolicy("Steam")
	if strings.TrimSpace(s.ClosingMethod) == "" {
		s.ClosingMethod = defClose
	}
	if forceClose {
		s.ClosingMethod = defClose
	}
	s.ClosingMethodForced = forceClose
	if strings.TrimSpace(s.StartingMethod) == "" {
		s.StartingMethod = "Default"
	}
	// New bools default to true when absent from JSON (unmarshal sets false).
	if !gjson.GetBytes(data2, "Steam_ShowMiniProfile").Exists() {
		s.SteamShowMiniProfile = true
	}
	if !gjson.GetBytes(data2, "Steam_ShowAvatarFrame").Exists() {
		s.SteamShowAvatarFrame = true
	}
	return s, nil
}

// SaveSettings writes Settings/SteamSettings.json.
func SaveSettings(s Settings) error {
	path, err := settingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if s.Shortcuts == nil {
		s.Shortcuts = []platform.GameShortcutEntry{}
	}
	if s.AccountNotes == nil {
		s.AccountNotes = map[string]string{}
	}
	s.ClosingMethodForced = false
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, data, 0o644)
}

// NormalizeFolderPath strips a trailing steam.exe and ensures directory form.
func NormalizeFolderPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if strings.HasSuffix(strings.ToLower(p), ".exe") {
		p = filepath.Dir(p)
	}
	p = filepath.Clean(p)
	if len(p) >= 2 && p[1] == ':' && !strings.HasSuffix(p, `\`) {
		// keep as clean path
	}
	return p
}

// PlatformKey is the folder name under img/profiles.
const PlatformKey = "Steam"
const ProfileFolderSlug = "steam"

func migrateLegacyShortcutsJSON(m map[string]string) []platform.GameShortcutEntry {
	type kv struct {
		k int
		v string
	}
	var neg, pos []kv
	for ks, v := range m {
		ki, err := strconv.Atoi(strings.TrimSpace(ks))
		if err != nil {
			continue
		}
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if ki < 0 {
			neg = append(neg, kv{ki, v})
		} else {
			pos = append(pos, kv{ki, v})
		}
	}
	sort.Slice(neg, func(i, j int) bool { return neg[i].k < neg[j].k })
	sort.Slice(pos, func(i, j int) bool { return pos[i].k < pos[j].k })
	out := make([]platform.GameShortcutEntry, 0, len(neg)+len(pos))
	for _, e := range neg {
		out = append(out, platform.GameShortcutEntry{FileName: e.v, Pinned: true})
	}
	for _, e := range pos {
		out = append(out, platform.GameShortcutEntry{FileName: e.v, Pinned: false})
	}
	return out
}

func jsonRawMessageBool(m json.RawMessage) bool {
	if len(m) == 0 || string(m) == "null" {
		return false
	}
	var b bool
	if err := json.Unmarshal(m, &b); err != nil {
		return false
	}
	return b
}
