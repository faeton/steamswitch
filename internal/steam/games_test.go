package steam

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"steamswitch/internal/fsutil"
	"steamswitch/internal/paths"

	"github.com/ulikunitz/xz"
)

func validSteamAppArrayJSON() []byte {
	return []byte(`{"730":"Counter-Strike 2","440":"Team Fortress 2"}`)
}

func TestSteamAppNameMapCacheExpired(t *testing.T) {
	dir := t.TempDir()
	paths.ResetForTest(dir)

	cachePath, err := appIdsUserPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := fsutil.WriteFileAtomic(cachePath, validSteamAppArrayJSON(), 0o644); err != nil {
		t.Fatal(err)
	}

	if steamAppNameMapCacheExpired() {
		t.Fatal("expected fresh cache not to be expired")
	}

	old := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(cachePath, old, old); err != nil {
		t.Fatal(err)
	}
	if !steamAppNameMapCacheExpired() {
		t.Fatal("expected old cache to be expired")
	}
}

func compressXZForTest(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := xz.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDecompressXZSteamAppNameMap(t *testing.T) {
	raw := validSteamAppArrayJSON()
	compressed := compressXZForTest(t, raw)

	got, err := decompressXZSteamAppNameMap(compressed)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(raw) {
		t.Fatalf("decompressed payload mismatch")
	}
	m, err := parseAppNameMapJSON(got)
	if err != nil {
		t.Fatal(err)
	}
	if m["730"] != "Counter-Strike 2" {
		t.Fatalf("unexpected parsed name: %q", m["730"])
	}
}

func TestGetSteamAppNameMapCachedLoadsMemory(t *testing.T) {
	dir := t.TempDir()
	paths.ResetForTest(dir)

	steamAppNameMapMu.Lock()
	steamAppNameMapMem = nil
	steamAppNameMapMu.Unlock()

	cachePath, err := appIdsUserPath()
	if err != nil {
		t.Fatal(err)
	}
	raw := validSteamAppArrayJSON()
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := fsutil.WriteFileAtomic(cachePath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := getSteamAppNameMapCached()
	if err != nil {
		t.Fatal(err)
	}
	if got["730"] != "Counter-Strike 2" {
		t.Fatalf("cached map mismatch: %q", got["730"])
	}

	steamAppNameMapMu.RLock()
	mem := steamAppNameMapMem
	steamAppNameMapMu.RUnlock()
	if mem["730"] != "Counter-Strike 2" {
		t.Fatalf("memory cache was not populated")
	}
}

func TestParseAppNameMapJSON_AcceptsFlatCacheShape(t *testing.T) {
	m, err := parseAppNameMapJSON([]byte(`{"570":"Dota 2","730":"Counter-Strike 2"}`))
	if err != nil {
		t.Fatal(err)
	}
	if m["570"] != "Dota 2" || m["730"] != "Counter-Strike 2" {
		t.Fatalf("flat shape = %v", m)
	}
}

func TestParseAppNameMapJSON_AcceptsValveAppListShape(t *testing.T) {
	raw := []byte(`{"applist":{"apps":[{"appid":570,"name":"Dota 2"},{"appid":730,"name":"Counter-Strike 2"},{"appid":0,"name":"skip"},{"appid":9,"name":"  "}]}}`)
	m, err := parseAppNameMapJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m["570"] != "Dota 2" || m["730"] != "Counter-Strike 2" {
		t.Fatalf("valve shape = %v", m)
	}
	if _, ok := m["0"]; ok {
		t.Error("appid 0 should be skipped")
	}
	if _, ok := m["9"]; ok {
		t.Error("blank name should be skipped")
	}
}

func TestParseAppNameMapJSON_RejectsJunk(t *testing.T) {
	if _, err := parseAppNameMapJSON([]byte(`{"applist":{"apps":[]}}`)); err == nil {
		t.Fatal("expected error for an empty app list")
	}
}
