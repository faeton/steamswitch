package steam

import (
	"errors"
	"strings"

	"steamswitch/internal/security"
)

// errUnknownRefreshPreset is returned for a preset ID the build does not define.
var errUnknownRefreshPreset = errors.New("Toast_Steam_UnknownRefreshPreset")

// RefreshPresetID identifies a named bundle of advanced-clearing actions that the UI can
// run with one click, instead of ticking boxes on the Advanced Cleaning page.
type RefreshPresetID = string

const (
	// RefreshPresetCaches clears the caches that cause most stale-UI and
	// failed-download symptoms. It does not touch anything login related, so the
	// user stays signed in on every account.
	RefreshPresetCaches RefreshPresetID = "caches"

	// RefreshPresetDeep additionally clears crash dumps and logs. Still login-safe.
	RefreshPresetDeep RefreshPresetID = "deep"
)

// RefreshPreset describes a preset for the UI.
type RefreshPreset struct {
	ID string `json:"id"`
	// Actions are advanced-clearing action IDs, run in order.
	Actions []string `json:"actions"`
	// ClosesSteam is true when the preset shuts Steam down first.
	ClosesSteam bool `json:"closesSteam"`
	// TouchesLogin is true when the preset can sign accounts out. Both presets
	// shipped here are login-safe; the field exists so the UI can warn if that
	// ever changes.
	TouchesLogin bool `json:"touchesLogin"`
}

// refreshPresets is the ordered action list per preset. Every entry must be an action
// ID accepted by RunAdvancedClearingAction.
var refreshPresets = map[string]RefreshPreset{
	RefreshPresetCaches: {
		ID:          RefreshPresetCaches,
		ClosesSteam: true,
		Actions: []string{
			acCloseSteam,
			acClearHTMLCache,
			acClearAppCache,
			acClearHTTPCache,
			acClearDepotCache,
		},
	},
	RefreshPresetDeep: {
		ID:          RefreshPresetDeep,
		ClosesSteam: true,
		Actions: []string{
			acCloseSteam,
			acClearHTMLCache,
			acClearAppCache,
			acClearHTTPCache,
			acClearDepotCache,
			acClearLogs,
			acClearUILogs,
			acClearDumps,
		},
	},
}

// ListRefreshPresets returns the one-click presets available to the UI.
func (s *SteamService) ListRefreshPresets() ([]RefreshPreset, error) {
	if err := security.RequireUnlocked(); err != nil {
		return nil, err
	}
	// Stable order: light preset first.
	return []RefreshPreset{refreshPresets[RefreshPresetCaches], refreshPresets[RefreshPresetDeep]}, nil
}

// RunRefreshPreset runs every action in a preset in order and returns the combined log.
// A failing step is recorded and the run continues, so one unavailable folder cannot
// abort the whole refresh.
func (s *SteamService) RunRefreshPreset(presetID string) (AdvancedClearResult, error) {
	if err := security.RequireUnlocked(); err != nil {
		return AdvancedClearResult{}, err
	}
	preset, ok := refreshPresets[strings.TrimSpace(strings.ToLower(presetID))]
	if !ok {
		return AdvancedClearResult{}, errUnknownRefreshPreset
	}

	var lines []string
	for _, action := range preset.Actions {
		res, err := s.RunAdvancedClearingAction(action)
		lines = append(lines, res.Lines...)
		if err != nil {
			lines = append(lines, advancedClearingI18nLine("Clear_ActionFailed", action, err.Error()))
		}
	}
	// A refresh is only useful if the account list is rebuilt afterwards.
	s.StartSteamProfileRefresh()
	return AdvancedClearResult{Lines: lines}, nil
}
