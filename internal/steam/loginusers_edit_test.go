package steam

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A switch must not be a lossy round-trip of loginusers.vdf. The field set is not fixed —
// Steam has added keys across client generations — so anything this build does not model
// still has to survive.

func writeTempLoginUsers(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "loginusers.vdf")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// The real shape, matching the file on a live install: the root node IS the "users" wrapper.
// `AllowAutoLogin` and `SomeFutureKey` stand in for fields this build does not model.
const twoAccountsWithUnknownFields = `"users"
{
	"76561198000000001"
	{
		"AccountName"		"alice"
		"PersonaName"		"Alice"
		"RememberPassword"		"1"
		"AutoLogin"		"1"
		"Timestamp"		"1700000000"
		"AllowAutoLogin"		"1"
		"SomeFutureKey"		"keep-me"
	}
	"76561198000000002"
	{
		"AccountName"		"bob"
		"PersonaName"		"Bob"
		"RememberPassword"		"1"
		"AutoLogin"		"0"
		"Timestamp"		"1700000001"
		"AllowAutoLogin"		"0"
	}
}
`

func TestReadLoginUsersTree_RecognisesTheRootAsTheUsersWrapper(t *testing.T) {
	// steamvdf.ReadBytes returns only the first top-level block, so a normal file parses to a
	// node keyed "users" whose children are the accounts — the wrapper is the root, not a
	// child of it. Getting this wrong would treat account fields as account blocks.
	f, err := readLoginUsersTree(writeTempLoginUsers(t, twoAccountsWithUnknownFields))
	if err != nil {
		t.Fatal(err)
	}
	if !f.rootIsWrapper {
		t.Fatal("expected the root node to be recognised as the users wrapper")
	}
	if got := len(f.blocks()); got != 2 {
		t.Fatalf("blocks = %d, want 2 accounts", got)
	}
}

func TestApplyLoginSelection_PreservesFieldsTheBuildDoesNotModel(t *testing.T) {
	f, err := readLoginUsersTree(writeTempLoginUsers(t, twoAccountsWithUnknownFields))
	if err != nil {
		t.Fatal(err)
	}

	f.applyLoginSelection("76561198000000002", true)
	out := string(f.render())

	for _, want := range []string{"AllowAutoLogin", "SomeFutureKey", "keep-me"} {
		if !strings.Contains(out, want) {
			t.Fatalf("%q was dropped by the switch; got:\n%s", want, out)
		}
	}
}

func TestApplyLoginSelection_MovesTheActiveMarkersToTheChosenAccount(t *testing.T) {
	path := writeTempLoginUsers(t, twoAccountsWithUnknownFields)
	f, err := readLoginUsersTree(path)
	if err != nil {
		t.Fatal(err)
	}

	autoUser := f.applyLoginSelection("76561198000000002", true)
	if autoUser != "bob" {
		t.Fatalf("autoUser = %q, want \"bob\" — this is what goes into the auto-login selector", autoUser)
	}

	users, err := ParseLoginUsers(mustRewrite(t, path, f.render()))
	if err != nil {
		t.Fatal(err)
	}
	if got := ActiveSessionSteamID64(users); got != "76561198000000002" {
		t.Fatalf("active session = %q, want the account just selected", got)
	}
	for _, u := range users {
		if u.SteamID64 == "76561198000000001" && u.AutoLogin != "0" {
			t.Fatalf("alice still has AutoLogin=%q; exactly one account may be active", u.AutoLogin)
		}
	}
}

func TestApplyLoginSelection_EmptySelectionClearsEveryMarker(t *testing.T) {
	// "Add New": Steam must show the account chooser rather than log anyone in.
	path := writeTempLoginUsers(t, twoAccountsWithUnknownFields)
	f, err := readLoginUsersTree(path)
	if err != nil {
		t.Fatal(err)
	}

	if autoUser := f.applyLoginSelection("", true); autoUser != "" {
		t.Fatalf("autoUser = %q, want empty so the selector is cleared", autoUser)
	}
	users, err := ParseLoginUsers(mustRewrite(t, path, f.render()))
	if err != nil {
		t.Fatal(err)
	}
	if got := ActiveSessionSteamID64(users); got != "" {
		t.Fatalf("active session = %q, want none", got)
	}
}

func TestApplyLoginSelection_DoesNotInventFieldsOnUnselectedAccounts(t *testing.T) {
	// A file that never carried MostRecent should not gain "MostRecent" "0" on every account
	// the user did not switch to — that is a diff with no meaning.
	f, err := readLoginUsersTree(writeTempLoginUsers(t, twoAccountsWithUnknownFields))
	if err != nil {
		t.Fatal(err)
	}

	f.applyLoginSelection("76561198000000002", true)
	for _, b := range f.blocks() {
		if strings.TrimSpace(b.Key) != "76561198000000001" {
			continue
		}
		if childValueCI(b, "MostRecent") != "" {
			t.Fatal("MostRecent was added to an account that never had it")
		}
	}
}

func TestRemoveAccount_LeavesTheOtherBlocksIntact(t *testing.T) {
	f, err := readLoginUsersTree(writeTempLoginUsers(t, twoAccountsWithUnknownFields))
	if err != nil {
		t.Fatal(err)
	}

	f.removeAccount("76561198000000001")
	out := string(f.render())

	if strings.Contains(out, "alice") || strings.Contains(out, "SomeFutureKey") {
		t.Fatalf("removed account still present:\n%s", out)
	}
	for _, want := range []string{"bob", "AllowAutoLogin", "76561198000000002"} {
		if !strings.Contains(out, want) {
			t.Fatalf("%q lost while removing a different account:\n%s", want, out)
		}
	}
}

func TestApplyLoginSelection_MirrorsWhicheverActiveMarkerTheFileAlreadyUses(t *testing.T) {
	// The fixture uses AutoLogin and has no MostRecent anywhere. Stamping a legacy MostRecent
	// key onto it would be this build inventing a field, which is what this file exists to
	// prevent — verified against a real macOS install, which also carries no MostRecent.
	f, err := readLoginUsersTree(writeTempLoginUsers(t, twoAccountsWithUnknownFields))
	if err != nil {
		t.Fatal(err)
	}

	f.applyLoginSelection("76561198000000002", true)
	if strings.Contains(string(f.render()), "MostRecent") {
		t.Fatalf("MostRecent was invented for a file that uses AutoLogin:\n%s", f.render())
	}
}

func TestApplyLoginSelection_UsesLegacyMostRecentWhenThatIsWhatTheFileHas(t *testing.T) {
	legacy := `"users"
{
	"76561198000000001"
	{
		"AccountName"		"alice"
		"MostRecent"		"1"
	}
	"76561198000000002"
	{
		"AccountName"		"bob"
		"MostRecent"		"0"
	}
}
`
	path := writeTempLoginUsers(t, legacy)
	f, err := readLoginUsersTree(path)
	if err != nil {
		t.Fatal(err)
	}
	f.applyLoginSelection("76561198000000002", true)

	users, err := ParseLoginUsers(mustRewrite(t, path, f.render()))
	if err != nil {
		t.Fatal(err)
	}
	if got := ActiveSessionSteamID64(users); got != "76561198000000002" {
		t.Fatalf("active session = %q; a legacy file must still switch", got)
	}
}

func TestApplyLoginSelection_ClearsRememberPasswordOnEveryOtherAccount(t *testing.T) {
	// Documents inherited upstream behaviour rather than endorsing it: a switch sets
	// RememberPassword=0 on every account it is not switching to. Pinned here so that if it is
	// ever changed, it is changed deliberately. See the task on "switch looks like a logout".
	path := writeTempLoginUsers(t, twoAccountsWithUnknownFields)
	f, err := readLoginUsersTree(path)
	if err != nil {
		t.Fatal(err)
	}
	f.applyLoginSelection("76561198000000002", true)

	users, err := ParseLoginUsers(mustRewrite(t, path, f.render()))
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		want := "0"
		if u.SteamID64 == "76561198000000002" {
			want = "1"
		}
		if u.RememberPassword != want {
			t.Fatalf("%s RememberPassword = %q, want %q", u.AccountName, u.RememberPassword, want)
		}
	}
}

func TestApplyLoginSelection_LeavesNoSignedInSessionWhenRememberIsOff(t *testing.T) {
	// The public-machine case: switch in, play, and Steam asks the next person for a password
	// instead of handing them the account.
	path := writeTempLoginUsers(t, twoAccountsWithUnknownFields)
	f, err := readLoginUsersTree(path)
	if err != nil {
		t.Fatal(err)
	}

	f.applyLoginSelection("76561198000000002", false)

	users, err := ParseLoginUsers(mustRewrite(t, path, f.render()))
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		if u.RememberPassword != "0" {
			t.Fatalf("%s RememberPassword = %q, want \"0\" for every account", u.AccountName, u.RememberPassword)
		}
	}
	// The switch must still switch — only the remembering is disabled.
	if got := ActiveSessionSteamID64(users); got != "76561198000000002" {
		t.Fatalf("active session = %q; turning off remember must not stop the switch", got)
	}
}

func TestApplyLoginSelection_RememberOffStillPreservesUnknownFields(t *testing.T) {
	f, err := readLoginUsersTree(writeTempLoginUsers(t, twoAccountsWithUnknownFields))
	if err != nil {
		t.Fatal(err)
	}
	f.applyLoginSelection("76561198000000002", false)
	if !strings.Contains(string(f.render()), "SomeFutureKey") {
		t.Fatal("unknown fields must survive regardless of the remember setting")
	}
}

func TestReadLoginUsersTree_RefusesAShapeItDoesNotUnderstand(t *testing.T) {
	// Writing back a structure we did not recognise would corrupt the file that decides which
	// account Steam logs into. Refusing leaves it untouched.
	body := `"SomethingElse"
{
	"NotAnAccount"		"1"
}
`
	if _, err := readLoginUsersTree(writeTempLoginUsers(t, body)); !errors.Is(err, ErrLoginUsersShape) {
		t.Fatalf("err = %v, want ErrLoginUsersShape", err)
	}
}

func TestRoundTripOfAnUnchangedFileKeepsEveryAccount(t *testing.T) {
	// Guards the whole read-modify-write path: selecting an account that is not in the file
	// must still leave all the others exactly as they were.
	path := writeTempLoginUsers(t, twoAccountsWithUnknownFields)
	f, err := readLoginUsersTree(path)
	if err != nil {
		t.Fatal(err)
	}
	f.applyLoginSelection("76561198000000999", true)

	users, err := ParseLoginUsers(mustRewrite(t, path, f.render()))
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 {
		t.Fatalf("users = %d, want both accounts preserved", len(users))
	}
}

func mustRewrite(t *testing.T, path string, data []byte) string {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
