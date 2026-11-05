package app

import (
	"fmt"
	"strings"

	"steamswitch/internal/steam"
)

type platformCommandAdapter interface {
	AccountRows() ([]ListAccountRow, error)
	Swap(uniqueID string, personaState int, extraLaunchArgs []string) error
	Logout() error
}

type steamCommandAdapter struct {
	svc *steam.SteamService
}

func (a steamCommandAdapter) AccountRows() ([]ListAccountRow, error) {
	accs, err := a.svc.GetSteamAccounts()
	if err != nil {
		return nil, err
	}
	out := make([]ListAccountRow, 0, len(accs))
	for _, account := range accs {
		out = append(out, ListAccountRow{
			UniqueID:     account.SteamID64,
			DisplayName:  strings.TrimSpace(account.DisplayName),
			LastLoggedIn: strings.TrimSpace(account.LastLogin),
		})
	}
	return out, nil
}

func (a steamCommandAdapter) Swap(uniqueID string, personaState int, extraLaunchArgs []string) error {
	return a.svc.SwapToSteamAccount(uniqueID, personaState, extraLaunchArgs)
}

func (a steamCommandAdapter) Logout() error {
	return a.svc.SteamAddNew()
}

func (d *Dispatch) commandAdapter() platformCommandAdapter {
	return steamCommandAdapter{svc: d.SteamSvc}
}

// requireSteamPlatform stops a mistyped platform in a command or link from acting on Steam.
func requireSteamPlatform(platformKey string) error {
	if strings.EqualFold(strings.TrimSpace(platformKey), steam.PlatformKey) {
		return nil
	}
	return fmt.Errorf("unsupported platform %q: SteamSwitch only handles Steam", strings.TrimSpace(platformKey))
}
