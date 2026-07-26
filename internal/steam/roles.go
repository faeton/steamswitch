package steam

import (
	"errors"
	"sort"
	"strconv"
	"strings"

	"steamswitch/internal/actionlog"
	"steamswitch/internal/security"
)

// Account roles — the "Home" / "Shared" distinction the Session Kit is built on
// (REDESIGN.md §2). Roles are pure metadata: nominating an account changes no file on
// disk, it only changes what a later switch decides to do.

var errRolesInvalidID = errors.New("Toast_Steam_InvalidId")

// errRolesHomeIsShared guards the one combination that has no meaning: the kit's source
// cannot also be a destination the kit travels to.
var errRolesHomeIsShared = errors.New("Toast_Roles_HomeCannotBeShared")

// AccountRoles is the role map the frontend renders badges from.
type AccountRoles struct {
	HomeSteamID64 string   `json:"homeSteamId64"`
	SharedIDs     []string `json:"sharedIds"`
}

// GetAccountRoles returns the Home account and the set of shared accounts.
func (s *SteamService) GetAccountRoles() (AccountRoles, error) {
	if err := security.RequireUnlocked(); err != nil {
		return AccountRoles{}, err
	}
	st, err := LoadSettings()
	if err != nil {
		return AccountRoles{}, err
	}
	shared := append([]string(nil), st.SharedSteamIDs...)
	sort.Strings(shared)
	if shared == nil {
		shared = []string{}
	}
	return AccountRoles{HomeSteamID64: strings.TrimSpace(st.HomeSteamID64), SharedIDs: shared}, nil
}

// SetHomeAccount nominates the machine's primary account. Passing an empty id clears it.
func (s *SteamService) SetHomeAccount(steamID64 string) (AccountRoles, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := security.RequireUnlocked(); err != nil {
		return AccountRoles{}, err
	}
	id := strings.TrimSpace(steamID64)
	st, err := LoadSettings()
	if err != nil {
		return AccountRoles{}, err
	}
	if id != "" && st.IsShared(id) {
		return AccountRoles{}, errRolesHomeIsShared
	}
	st.HomeSteamID64 = id
	if err := SaveSettings(st); err != nil {
		return AccountRoles{}, err
	}
	actionlog.Record("steam:setHome", id, "", nil)
	return s.rolesFrom(st), nil
}

// SetAccountShared marks or unmarks an account as shared with another person.
func (s *SteamService) SetAccountShared(steamID64 string, shared bool) (AccountRoles, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := security.RequireUnlocked(); err != nil {
		return AccountRoles{}, err
	}
	id := strings.TrimSpace(steamID64)
	if id == "" {
		return AccountRoles{}, errRolesInvalidID
	}
	st, err := LoadSettings()
	if err != nil {
		return AccountRoles{}, err
	}
	if shared && st.IsHome(id) {
		return AccountRoles{}, errRolesHomeIsShared
	}

	next := make([]string, 0, len(st.SharedSteamIDs)+1)
	for _, v := range st.SharedSteamIDs {
		if t := strings.TrimSpace(v); t != "" && t != id {
			next = append(next, t)
		}
	}
	if shared {
		next = append(next, id)
	}
	sort.Strings(next)
	st.SharedSteamIDs = next

	if err := SaveSettings(st); err != nil {
		return AccountRoles{}, err
	}
	actionlog.Record("steam:setShared", id, strconv.FormatBool(shared), nil)
	return s.rolesFrom(st), nil
}

func (s *SteamService) rolesFrom(st Settings) AccountRoles {
	shared := append([]string(nil), st.SharedSteamIDs...)
	if shared == nil {
		shared = []string{}
	}
	return AccountRoles{HomeSteamID64: strings.TrimSpace(st.HomeSteamID64), SharedIDs: shared}
}
