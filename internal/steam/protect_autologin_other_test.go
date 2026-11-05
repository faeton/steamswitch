//go:build !windows

package steam

import "testing"

// protectSteamAutoLogin has nothing to do here: other platforms keep these values inside the
// test's own temp Steam root. The Windows version guards the live registry.
func protectSteamAutoLogin(*testing.T) {}
