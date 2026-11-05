package basic

import (
	"path/filepath"
	"testing"

	"steamswitch/internal/paths"
	"steamswitch/internal/platform"
)

// resetDataPathsForTest points the data root at a temp folder. It sets global path singletons, so
// tests using it must not call t.Parallel().
func resetDataPathsForTest(t *testing.T) {
	t.Helper()
	exeDir := t.TempDir()
	platform.ResetPathSingletonsForTest(exeDir)
	paths.ResetForTest(filepath.Join(exeDir, "SteamSwitch"))
}
