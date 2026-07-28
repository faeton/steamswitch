package vault

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func fieldOutcome(row ImportRow, field string) string {
	for _, f := range row.Fields {
		if f.Field == field {
			return f.Outcome
		}
	}
	return "<missing>"
}

func planOf(t *testing.T, records ...RosterRecord) ImportPlan {
	t.Helper()
	plan, err := PlanImport(RosterPayload{Version: 1, Accounts: records}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { DiscardImport(plan.SessionID) })
	return plan
}

// TestPlanImport_FillEmptyOnlyIsTheDefault is the decision the whole conflict policy rests on:
// a batch import must never silently replace a credential that already works, because a batch
// is exactly the situation where nobody reads each row.
func TestPlanImport_FillEmptyOnlyIsTheDefault(t *testing.T) {
	newVault(t)
	if err := Put(Draft{
		SteamID64: "76561198000000001",
		Password:  ptr("existing-password"),
	}); err != nil {
		t.Fatal(err)
	}

	plan := planOf(t, RosterRecord{
		SteamID64:    "76561198000000001",
		Password:     "import-password",
		SharedSecret: "import-seed",
	})
	row := plan.Rows[0]
	if row.Action != ActionUpdate {
		t.Fatalf("Action = %q, want update", row.Action)
	}
	if got := fieldOutcome(row, ImportFieldPassword); got != OutcomeKeep {
		t.Errorf("password outcome = %q, want keep", got)
	}
	if got := fieldOutcome(row, ImportFieldTOTP); got != OutcomeFill {
		t.Errorf("totp outcome = %q, want fill", got)
	}

	if _, err := CommitImport(plan.SessionID, nil); err != nil {
		t.Fatal(err)
	}
	pw, err := Reveal("76561198000000001", FieldPassword)
	if err != nil {
		t.Fatal(err)
	}
	if pw != "existing-password" {
		t.Fatalf("password = %q, want the existing one kept", pw)
	}
	seed, err := Reveal("76561198000000001", FieldSharedSecret)
	if err != nil {
		t.Fatal(err)
	}
	if seed != "import-seed" {
		t.Fatalf("sharedSecret = %q, want the imported one filled in", seed)
	}
}

func TestCommitImport_OverwriteIsPerRowAndOptIn(t *testing.T) {
	newVault(t)
	if err := Put(Draft{SteamID64: "76561198000000001", Password: ptr("existing")}); err != nil {
		t.Fatal(err)
	}
	if err := Put(Draft{SteamID64: "76561198000000002", Password: ptr("existing")}); err != nil {
		t.Fatal(err)
	}

	plan := planOf(t,
		RosterRecord{SteamID64: "76561198000000001", Password: "imported"},
		RosterRecord{SteamID64: "76561198000000002", Password: "imported"},
	)
	// Only row 0 is flipped.
	if _, err := CommitImport(plan.SessionID, []RowDecision{{Index: 0, Mode: RowModeOverwrite}}); err != nil {
		t.Fatal(err)
	}

	first, _ := Reveal("76561198000000001", FieldPassword)
	second, _ := Reveal("76561198000000002", FieldPassword)
	if first != "imported" {
		t.Errorf("row 0 password = %q, want the overwrite to apply", first)
	}
	if second != "existing" {
		t.Errorf("row 1 password = %q, want it untouched", second)
	}
}

func TestCommitImport_SkipLeavesTheEntryAlone(t *testing.T) {
	newVault(t)
	if err := Put(Draft{SteamID64: "76561198000000001", Password: ptr("existing")}); err != nil {
		t.Fatal(err)
	}
	plan := planOf(t,
		RosterRecord{SteamID64: "76561198000000001", Password: "imported"},
		RosterRecord{SteamID64: "76561198000000002", Password: "new"},
	)
	summary, err := CommitImport(plan.SessionID, []RowDecision{{Index: 0, Mode: RowModeSkip}})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Skipped != 1 || summary.Created != 1 || summary.Updated != 0 {
		t.Fatalf("summary = %+v, want 1 skipped and 1 created", summary)
	}
	if pw, _ := Reveal("76561198000000001", FieldPassword); pw != "existing" {
		t.Fatalf("skipped row was written: password = %q", pw)
	}
}

// TestCommitImport_SummaryAccountsForEveryRow is a hard requirement from A13: a summary that
// adds up to fewer rows than the source had is how a silently dropped account becomes a
// mystery a week later.
func TestCommitImport_SummaryAccountsForEveryRow(t *testing.T) {
	newVault(t)
	if err := Put(Draft{SteamID64: "76561198000000001"}); err != nil {
		t.Fatal(err)
	}
	plan := planOf(t,
		RosterRecord{SteamID64: "76561198000000001", Password: "x"}, // update
		RosterRecord{SteamID64: "76561198000000002", Password: "x"}, // create
		RosterRecord{SteamID64: "76561198000000003", Password: "x"}, // skipped by decision
		RosterRecord{SteamID64: "not-an-id"},                        // invalid
		RosterRecord{SteamID64: "76561198000000002"},                // duplicate of row 1
	)
	summary, err := CommitImport(plan.SessionID, []RowDecision{{Index: 2, Mode: RowModeSkip}})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Total != 5 {
		t.Fatalf("Total = %d, want 5", summary.Total)
	}
	if got := summary.Created + summary.Updated + summary.Skipped + summary.Invalid; got != summary.Total {
		t.Fatalf("counts sum to %d, want %d (%+v)", got, summary.Total, summary)
	}
	if summary.Invalid != 2 {
		t.Fatalf("Invalid = %d, want 2 (a bad id and a duplicate)", summary.Invalid)
	}
	if len(summary.Rejected) != 2 {
		t.Fatalf("Rejected = %+v, want a reason per invalid row", summary.Rejected)
	}
}

// TestPlanImport_DuplicateWithinTheSameSource — importing the same account twice would
// otherwise apply the second copy over the first with nothing on screen saying it happened.
func TestPlanImport_DuplicateWithinTheSameSource(t *testing.T) {
	newVault(t)
	plan := planOf(t,
		RosterRecord{SteamID64: "76561198000000001", Password: "first"},
		RosterRecord{SteamID64: "76561198000000001", Password: "second"},
	)
	if plan.Rows[1].Action != ActionInvalid || plan.Rows[1].DuplicateOfIndex != 0 {
		t.Fatalf("row 1 = %+v, want it flagged as a duplicate of row 0", plan.Rows[1])
	}
	if _, err := CommitImport(plan.SessionID, nil); err != nil {
		t.Fatal(err)
	}
	if pw, _ := Reveal("76561198000000001", FieldPassword); pw != "first" {
		t.Fatalf("password = %q, want the first occurrence to win", pw)
	}
}

// TestCommitImport_ImportedEntriesAreStandalone pins the fact the brief got wrong: the Steam
// switcher grid is Steam's own loginusers.vdf, so an account this machine has never signed
// into cannot appear there and must not claim it will.
func TestCommitImport_ImportedEntriesAreStandalone(t *testing.T) {
	newVault(t)
	plan := planOf(t, RosterRecord{SteamID64: "76561198000000001", Password: "x"})
	if _, err := CommitImport(plan.SessionID, nil); err != nil {
		t.Fatal(err)
	}
	got, err := Get("76561198000000001")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Standalone {
		t.Fatal("an imported entry should be standalone until Steam signs into it here")
	}
}

// TestCommitImport_DoesNotFlipAnExistingEntryToStandalone — a re-import must not take an
// account off the switcher just because the roster listed it.
func TestCommitImport_DoesNotFlipAnExistingEntryToStandalone(t *testing.T) {
	newVault(t)
	if err := Put(Draft{SteamID64: "76561198000000001", Standalone: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	plan := planOf(t, RosterRecord{SteamID64: "76561198000000001", Password: "x"})
	if _, err := CommitImport(plan.SessionID, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := Get("76561198000000001")
	if got.Standalone {
		t.Fatal("re-importing an existing entry took it off the switcher")
	}
}

// TestApplyRosterEmail_IsAllOrNothing — merging a binding field by field produces an entry
// with one address and another host, which fails in a way nobody can read off the screen.
func TestApplyRosterEmail_IsAllOrNothing(t *testing.T) {
	existing := Entry{Email: EmailBinding{
		Address: "old@example.test",
		Source:  EmailSourceIMAP,
		IMAP:    &IMAPCreds{Host: "old.example.test", Port: 993, User: "old@example.test", Password: "oldpw", UseTLS: true},
	}}

	kept := existing
	applyRosterEmail(&kept, EmailBinding{Address: "new@example.test", Source: EmailSourceIMAP,
		IMAP: &IMAPCreds{Host: "new.example.test"}}, false)
	if kept.Email.Address != "old@example.test" || kept.Email.IMAP.Host != "old.example.test" {
		t.Fatalf("fill-empty-only altered an existing binding: %+v", kept.Email)
	}

	replaced := existing
	applyRosterEmail(&replaced, EmailBinding{Address: "new@example.test", Source: EmailSourceIMAP,
		IMAP: &IMAPCreds{Host: "new.example.test"}}, true)
	if replaced.Email.Address != "new@example.test" || replaced.Email.IMAP.Host != "new.example.test" {
		t.Fatalf("overwrite left a hybrid binding: %+v", replaced.Email)
	}
	if replaced.Email.IMAP.Password != "" {
		t.Fatalf("overwrite kept the old mailbox password: %+v", replaced.Email.IMAP)
	}
	if replaced.Email.IMAP.Port != 993 || replaced.Email.IMAP.User != "new@example.test" {
		t.Fatalf("defaults not applied to the replacement: %+v", replaced.Email.IMAP)
	}
}

func TestCommitImport_NothingToDoIsAnError(t *testing.T) {
	newVault(t)
	plan := planOf(t, RosterRecord{SteamID64: "76561198000000001"})
	if _, err := CommitImport(plan.SessionID, []RowDecision{{Index: 0, Mode: RowModeSkip}}); !errors.Is(err, ErrImportNothingToDo) {
		t.Fatalf("err = %v, want ErrImportNothingToDo", err)
	}
}

func TestCommitImport_SessionIsSingleUse(t *testing.T) {
	newVault(t)
	plan := planOf(t, RosterRecord{SteamID64: "76561198000000001", Password: "x"})
	if _, err := CommitImport(plan.SessionID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := CommitImport(plan.SessionID, nil); !errors.Is(err, ErrNoImportSession) {
		t.Fatalf("err = %v, want the buffer to be gone after a commit", err)
	}
}

// TestDropCacheDropsImportSessions — a decrypted roster surviving an app-lock would route
// around the gate the user believes closed everything.
func TestDropCacheDropsImportSessions(t *testing.T) {
	newVault(t)
	plan := planOf(t, RosterRecord{SteamID64: "76561198000000001", Password: "x"})
	DropCache()
	if _, ok := takeImportSession(plan.SessionID); ok {
		t.Fatal("an import buffer survived DropCache")
	}
}

func TestShredFile_RemovesAndReportsHonestly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roster.json")
	if err := os.WriteFile(path, []byte(`{"accounts":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if !removePlaintextBestEffort(path) {
		t.Fatal("removePlaintextBestEffort reported failure for a removable file")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("file still present after removal")
	}
	// A path that is already gone reports false rather than pretending.
	if removePlaintextBestEffort(path) {
		t.Fatal("removing a missing file reported success")
	}
}
