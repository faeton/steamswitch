package platform

import (
	"os/exec"
	"runtime"

	"steamswitch/internal/winutil"
)

// Latest GitHub releases; may later be replaced or extended by an in-app auto-updater.
const updateDownloadPageURL = "https://github.com/faeton/steamswitch/releases/latest"

// OpenUpdateDownloadPage opens the latest GitHub release page in the default browser.
func (p *PlatformService) OpenUpdateDownloadPage() error {
	switch runtime.GOOS {
	case "windows":
		return winutil.Start("cmd.exe", []string{"/c", "start", "", updateDownloadPageURL}, winutil.StartOpts{})
	case "darwin":
		return exec.Command("open", updateDownloadPageURL).Start()
	default:
		return exec.Command("xdg-open", updateDownloadPageURL).Start()
	}
}
