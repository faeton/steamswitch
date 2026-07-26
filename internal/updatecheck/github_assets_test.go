package updatecheck

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

func TestGitHubAssetMatcher_explicitWindowsExe(t *testing.T) {
	assets := []github.ReleaseAsset{
		{Name: "SteamSwitch.7z"},
		{Name: "SteamSwitch.exe.sig"},
		{Name: "SteamSwitch.exe"},
		{Name: "SomeOtherApp.Installer.exe"},
	}
	req := updater.CheckRequest{Platform: "windows", Arch: "amd64"}
	if got := GitHubAssetMatcher(req, assets); got != 2 {
		t.Fatalf("got index %d, want 2 (SteamSwitch.exe)", got)
	}
}

func TestGitHubAssetMatcher_wailsConventionFallback(t *testing.T) {
	assets := []github.ReleaseAsset{
		{Name: "SteamSwitch-windows-amd64.zip"},
	}
	req := updater.CheckRequest{Platform: "windows", Arch: "amd64"}
	if got := GitHubAssetMatcher(req, assets); got != 0 {
		t.Fatalf("got index %d, want 0", got)
	}
}

func TestGitHubAssetMatcher_unknownPlatform(t *testing.T) {
	assets := []github.ReleaseAsset{{Name: "SteamSwitch-linux-amd64.tar.gz"}}
	req := updater.CheckRequest{Platform: "linux", Arch: "amd64"}
	if got := GitHubAssetMatcher(req, assets); got != 0 {
		t.Fatalf("got index %d, want 0 via DefaultAssetMatcher", got)
	}
}

func TestGitHubAssetMatcher_unmappedPlatformNoDefault(t *testing.T) {
	assets := []github.ReleaseAsset{{Name: "SteamSwitch.exe"}}
	req := updater.CheckRequest{Platform: "linux", Arch: "arm64"}
	if got := GitHubAssetMatcher(req, assets); got != -1 {
		t.Fatalf("got index %d, want -1", got)
	}
}
