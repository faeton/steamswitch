package app

import "testing"

func TestRequireSteamPlatform(t *testing.T) {
	for _, name := range []string{"Steam", "steam", " Steam "} {
		if err := requireSteamPlatform(name); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
	for _, name := range []string{"Epic", "Epic Games", "s", ""} {
		if err := requireSteamPlatform(name); err == nil {
			t.Errorf("%q should be refused: it would otherwise act on Steam", name)
		}
	}
}
