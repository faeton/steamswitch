package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"strings"
	"testing"
)

func TestTrayIconPNGIsTraySized(t *testing.T) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(trayIconPNG))
	if err != nil {
		t.Fatalf("decode tray icon: %v", err)
	}
	if format != "png" {
		t.Fatalf("tray icon format = %q, want png", format)
	}
	if cfg.Width != 32 || cfg.Height != 32 {
		t.Fatalf("tray icon dimensions = %dx%d, want 32x32", cfg.Width, cfg.Height)
	}
}

// The old TCNO icon got into the Windows exe through release.yml copying other/NSIS/img/icon.ico
// over the app icon. Compared by hash so it cannot come back under any of these names.
func TestUpstreamIconIsGone(t *testing.T) {
	const upstreamIcon = "92b8b9c943ecede8a8feed539aa50333f5f2a02455733a2024dd5f3da33f9aa4"
	for _, p := range []string{"build/windows/icon.ico", "other/img/icon.ico", "other/NSIS/img/icon.ico"} {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprintf("%x", sha256.Sum256(data)) == upstreamIcon {
			t.Errorf("%s is still the upstream TCNO icon", p)
		}
	}
	workflow, err := os.ReadFile(".github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(workflow), `img\icon.ico" "$env:GITHUB_WORKSPACE\build\windows\icon.ico"`) {
		t.Error("release.yml overwrites the app icon again")
	}
}
