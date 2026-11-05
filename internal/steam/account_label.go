package steam

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"steamswitch/internal/security"
	"steamswitch/internal/tray"
)

// accountLabelMaxRunes keeps a label short enough for a row and a tray entry.
const accountLabelMaxRunes = 64

// labelOr gives the user's own name for an account, or the fallback when it has none.
func labelOr(labels map[string]string, steamID64, fallback string) string {
	if label := strings.TrimSpace(labels[steamID64]); label != "" {
		return label
	}
	return fallback
}

// withAccountLabel returns the labels with this account's set, or cleared by an empty name.
func withAccountLabel(labels map[string]string, steamID64, name string) (map[string]string, error) {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > accountLabelMaxRunes {
		return nil, fmt.Errorf("name is longer than %d characters", accountLabelMaxRunes)
	}
	out := make(map[string]string, len(labels)+1)
	for id, label := range labels {
		out[id] = label
	}
	if name == "" {
		delete(out, steamID64)
	} else {
		out[steamID64] = name
	}
	return out, nil
}

// SetAccountLabel names an account in SteamSwitch only; Steam itself is not touched. An empty name
// goes back to the Steam name.
func (s *SteamService) SetAccountLabel(steamID64, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := security.RequireUnlocked(); err != nil {
		return err
	}
	steamID64 = strings.TrimSpace(steamID64)
	if !looksLikeSteamID64(steamID64) {
		return errSteamDataInvalidID
	}
	st, err := LoadSettings()
	if err != nil {
		return err
	}
	labels, err := withAccountLabel(st.AccountLabels, steamID64, name)
	if err != nil {
		return err
	}
	st.AccountLabels = labels
	if err := SaveSettings(st); err != nil {
		return err
	}
	SyncTrayKnownAccounts()
	tray.RefreshMenuIfSet()
	return nil
}
