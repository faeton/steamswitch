//go:build darwin

package platform

import pos "steamswitch/internal/platform/os/darwin"

func findExeViaStartMenuShortcuts(entry PlatformEntry, exeName string) (string, bool) {
	return pos.FindExeViaShortcuts()
}
