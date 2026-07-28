package basic

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"steamswitch/internal/platform"
)

func TestResolveDescriptorValue_LatestModifiedFile(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "battle.net-old.log")
	newPath := filepath.Join(dir, "battle.net-new.log")
	if err := os.WriteFile(oldPath, []byte("old"), 0o644); err != nil {
		t.Fatalf("write old log: %v", err)
	}
	if err := os.WriteFile(newPath, []byte("new"), 0o644); err != nil {
		t.Fatalf("write new log: %v", err)
	}
	now := time.Now()
	if err := os.Chtimes(oldPath, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("set old file modtime: %v", err)
	}
	if err := os.Chtimes(newPath, now.Add(-1*time.Hour), now.Add(-1*time.Hour)); err != nil {
		t.Fatalf("set new file modtime: %v", err)
	}
	got := resolveDescriptorValue(platform.Descriptor{}, "LATEST_MODIFIED_FILE:"+filepath.Join(dir, "battle.net-*.log"), "", platform.PathTokenContext{}, map[string]string{}, "", false)
	if got != newPath {
		t.Fatalf("unexpected latest modified file: got %q want %q", got, newPath)
	}
}

func TestParseBattleNetAccountIDFromLogData_LastMatch(t *testing.T) {
	data := []byte(`I 2026-05-07 16:47:10.188794 [Main] {Main} Opened database at: C:\Users\gamer\AppData\Local\Battle.net\CachedData.db
I 2026-05-07 16:47:14.311645 [Main] {Main} Opened database at: C:\Users\gamer\AppData\Local\Battle.net\Account\1111185922\account.db
I 2026-05-07 16:50:14.311645 [Main] {Main} Opened database at: C:\Users\gamer\AppData\Local\Battle.net\Account\9999999999\account.db`)
	got := parseBattleNetAccountIDFromLogData(data)
	want := "9999999999"
	if got != want {
		t.Fatalf("unexpected account id: got %q want %q", got, want)
	}
}

func TestResolveSQLiteValue_RecognisedButUnsupported(t *testing.T) {
	raw := `SQLITE:C:\Users\gamer\CachedData.db|SELECT battle_tag FROM login_cache`
	resolved, handled, err := resolveSQLiteValue(raw, "", platform.PathTokenContext{})
	if !handled {
		t.Fatal("SQLITE: prefix must be reported as handled, otherwise the value falls through to path expansion")
	}
	if err == nil {
		t.Fatal("expected an error for an unsupported SQLITE: value")
	}
	if resolved != "" {
		t.Fatalf("expected no resolved value, got %q", resolved)
	}
}

func TestResolveDescriptorValue_SQLiteDoesNotLeakLiteralExpression(t *testing.T) {
	raw := `SQLITE:C:\Users\gamer\CachedData.db|SELECT battle_tag FROM login_cache`
	if got := resolveDescriptorValue(platform.Descriptor{}, raw, "", platform.PathTokenContext{}, map[string]string{}, "", false); got != "" {
		t.Fatalf("SQLITE: value must resolve to empty, got %q", got)
	}
}

func TestResolveBuiltInRuntimeVariables_BattleNetReadsAccountIDFromLatestLog(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "battle.net-1.log")
	logData := []byte(`I 2026-05-07 16:47:10.188794 [Main] {Main} Opened database at: C:\Users\gamer\AppData\Local\Battle.net\CachedData.db
I 2026-05-07 16:47:14.311645 [Main] {Main} Opened database at: C:\Users\gamer\AppData\Local\Battle.net\Account\1111185922\account.db`)
	if err := os.WriteFile(logPath, logData, 0o644); err != nil {
		t.Fatalf("write battlenet log: %v", err)
	}
	d := platform.Descriptor{
		Extras: platform.DescriptorExtras{
			BuiltInUserId: "LATEST_MODIFIED_FILE:" + filepath.Join(dir, "battle.net-*.log"),
		},
	}
	vars := resolveBuiltInRuntimeVariables("BattleNet", d, "", platform.PathTokenContext{}, map[string]string{}, "", false)
	if vars["BuiltInUserId"] != "1111185922" {
		t.Fatalf("unexpected BuiltInUserId var: %q", vars["BuiltInUserId"])
	}
	if vars["builtinuserid"] != "1111185922" {
		t.Fatalf("unexpected builtinuserid var: %q", vars["builtinuserid"])
	}
}
