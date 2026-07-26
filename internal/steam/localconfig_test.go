package steam

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"steamswitch/internal/fsutil"
)

func TestWriteFileAtomic_LockedFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "locked.vdf")

	os.WriteFile(path, []byte("original"), 0o644)

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	err = fsutil.WriteFileAtomic(path, []byte("replacement"), 0o644)
	if err == nil {
		t.Error("expected error when writing over open file, got nil")
	}
}

func TestSetPersonaStateLocalConfig_Leaf(t *testing.T) {
	dir := t.TempDir()

	accountID := uint32(12345678)
	ud := filepath.Join(dir, "userdata", strconv.FormatUint(uint64(accountID), 10), "config")
	os.MkdirAll(ud, 0o755)
	localPath := filepath.Join(ud, "localconfig.vdf")

	vdfContent := `"UserLocalConfigStore"
{
	"friends"
	{
		"ePersonaState"		"0"
	}
}
`
	os.WriteFile(localPath, []byte(vdfContent), 0o644)

	steamID64 := "76561197972611406" // base + 12345678

	if err := setPersonaStateLocalConfig(dir, steamID64, 3); err != nil {
		t.Fatalf("setPersonaStateLocalConfig: %v", err)
	}

	data, _ := os.ReadFile(localPath)
	content := string(data)
	if !strings.Contains(content, `"ePersonaState"`) {
		t.Error("ePersonaState not found in output")
	}
	if !strings.Contains(content, strconv.Itoa(3)) {
		t.Errorf("ePersonaState not updated to 3: %s", content)
	}
}

func TestSetPersonaStateLocalConfig_FriendStorePrefs(t *testing.T) {
	dir := t.TempDir()

	accountID := uint32(87654321)
	ud := filepath.Join(dir, "userdata", strconv.FormatUint(uint64(accountID), 10), "config")
	os.MkdirAll(ud, 0o755)
	localPath := filepath.Join(ud, "localconfig.vdf")

	vdfContent := `"UserLocalConfigStore"
{
	"WebStorage"
	{
		"FriendStoreLocalPrefs_87654321"		"{\"ePersonaState\":0,\"strNonFriendsAllowedToMsg\":\"\"}"
	}
}
`
	os.WriteFile(localPath, []byte(vdfContent), 0o644)

	steamID64 := "76561198047920049" // base + 87654321

	if err := setPersonaStateLocalConfig(dir, steamID64, 5); err != nil {
		t.Fatalf("setPersonaStateLocalConfig: %v", err)
	}

	data, _ := os.ReadFile(localPath)
	content := string(data)
	if !strings.Contains(content, "ePersonaState") || !strings.Contains(content, strconv.Itoa(5)) {
		t.Errorf("FriendStoreLocalPrefs ePersonaState not updated: %s", content)
	}
}

func TestSetPersonaStateLocalConfig_MissingFile(t *testing.T) {
	dir := t.TempDir()
	err := setPersonaStateLocalConfig(dir, "76561197960265828", 1)
	if err == nil {
		t.Fatal("expected error for missing localconfig.vdf")
	}
}

func TestSetPersonaStateLocalConfig_BadSteamID(t *testing.T) {
	dir := t.TempDir()
	err := setPersonaStateLocalConfig(dir, "not-a-valid-id", 1)
	if err == nil {
		t.Fatal("expected error for invalid SteamID64")
	}
}

func TestWriteLoginUsersAndRegistry_FieldSwapping(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	os.MkdirAll(configDir, 0o755)

	loginPath := filepath.Join(configDir, "loginusers.vdf")
	initialVDF := `"users"
{
	"76561198000000100"
	{
		"AccountName"		"alpha"
		"PersonaName"		"Alpha"
		"Timestamp"		"1700000000"
		"MostRecent"		"0"
		"RememberPassword"		"0"
	}
	"76561198000000200"
	{
		"AccountName"		"beta"
		"PersonaName"		"Beta"
		"Timestamp"		"1690000000"
		"MostRecent"		"1"
		"RememberPassword"		"1"
	}
	"76561198000000300"
	{
		"AccountName"		"gamma"
		"PersonaName"		"Gamma"
		"Timestamp"		"1680000000"
		"MostRecent"		"0"
		"RememberPassword"		"0"
	}
}
`
	initialVDF = strings.Replace(initialVDF, `"RememberPassword"`, `"AutoLogin" "1"
		"RememberPassword"`, 1)
	os.WriteFile(loginPath, []byte(initialVDF), 0o644)

	if err := writeLoginUsersAndRegistry(dir, "76561198000000100", true); err != nil {
		t.Fatalf("writeLoginUsersAndRegistry: %v", err)
	}

	users, _ := ParseLoginUsers(loginPath)

	// Two deliberate departures from upstream here, both in loginusers_edit.go.
	//
	// AutoLogin reads "" rather than "0" on the non-target accounts: only account 100 carried
	// the key to begin with, and the writer clears a marker only where the block already has
	// one. Adding `"AutoLogin" "0"` to blocks that never had it would be this build inventing
	// a field. Steam reads an absent AutoLogin as false, so the effect is identical.
	//
	// RememberPassword keeps whatever each account already had. A switch owns the target's
	// "keep me signed in" flag and nobody else's — account 300 started at "0" and stays
	// there, 200 started at "1" and stays there. Upstream zeroed them all.
	checks := map[string]struct{ mr, auto, rp string }{
		"76561198000000100": {"1", "1", "1"},
		"76561198000000200": {"0", "", "1"},
		"76561198000000300": {"0", "", "0"},
	}

	for _, u := range users {
		want, ok := checks[u.SteamID64]
		if !ok {
			continue
		}
		if u.MostRecent != want.mr {
			t.Errorf("%s MostRecent = %q, want %q", u.SteamID64, u.MostRecent, want.mr)
		}
		if u.AutoLogin != want.auto {
			t.Errorf("%s AutoLogin = %q, want %q", u.SteamID64, u.AutoLogin, want.auto)
		}
		if u.RememberPassword != want.rp {
			t.Errorf("%s RememberPassword = %q, want %q", u.SteamID64, u.RememberPassword, want.rp)
		}
	}

	for _, u := range users {
		if u.SteamID64 == "76561198000000100" && u.AccountName != "alpha" {
			t.Errorf("AccountName changed: %q", u.AccountName)
		}
		if u.SteamID64 == "76561198000000200" && u.PersonaName != "Beta" {
			t.Errorf("PersonaName changed: %q", u.PersonaName)
		}
	}
}

func TestRemoveSteamAccountFromVDF_PreservesFields(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	os.MkdirAll(configDir, 0o755)

	loginPath := filepath.Join(configDir, "loginusers.vdf")
	initialVDF := `"users"
{
	"76561198000000100"
	{
		"AccountName"		"keeper"
		"PersonaName"		"Keeper"
		"Timestamp"		"1700000000"
		"WantsOfflineMode"		"0"
		"MostRecent"		"1"
		"RememberPassword"		"1"
		"SkipOfflineModeWarning"		"0"
	}
	"76561198000000999"
	{
		"AccountName"		"goner"
		"PersonaName"		"Goner"
		"Timestamp"		"1"
	}
}
`
	initialVDF = strings.Replace(initialVDF, `"RememberPassword"`, `"AutoLogin" "1"
		"RememberPassword"`, 1)
	os.WriteFile(loginPath, []byte(initialVDF), 0o644)

	if err := RemoveSteamAccountFromVDF(dir, "76561198000000999"); err != nil {
		t.Fatalf("RemoveSteamAccountFromVDF: %v", err)
	}

	users, _ := ParseLoginUsers(loginPath)
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}

	u := users[0]
	if u.SteamID64 != "76561198000000100" {
		t.Errorf("SteamID64 = %q", u.SteamID64)
	}
	if u.AccountName != "keeper" {
		t.Errorf("AccountName = %q", u.AccountName)
	}
	if u.PersonaName != "Keeper" {
		t.Errorf("PersonaName = %q", u.PersonaName)
	}
	if u.MostRecent != "1" {
		t.Errorf("MostRecent = %q, want 1", u.MostRecent)
	}
	if u.AutoLogin != "1" {
		t.Errorf("AutoLogin = %q, want 1", u.AutoLogin)
	}
	if u.RememberPassword != "1" {
		t.Errorf("RememberPassword = %q, want 1", u.RememberPassword)
	}
	if u.SkipOfflineWarn != "0" {
		t.Errorf("SkipOfflineWarn = %q", u.SkipOfflineWarn)
	}
	if u.WantsOffline != "0" {
		t.Errorf("WantsOffline = %q", u.WantsOffline)
	}
}
