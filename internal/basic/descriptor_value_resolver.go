package basic

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"steamswitch/internal/platform"
)

const latestModifiedFilePrefix = "LATEST_MODIFIED_FILE:"
const sqlitePrefix = "SQLITE:"

func resolveLatestModifiedFileValue(v, folder string, ctx platform.PathTokenContext) (string, bool, error) {
	trimmed := strings.TrimSpace(v)
	if !strings.HasPrefix(strings.ToUpper(trimmed), latestModifiedFilePrefix) {
		return "", false, nil
	}
	pattern := strings.TrimSpace(trimmed[len(latestModifiedFilePrefix):])
	if pattern == "" {
		return "", true, fmt.Errorf("empty latest modified file pattern")
	}
	pattern = expandPlatformPath(pattern, folder, ctx)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", true, fmt.Errorf("glob latest modified file %q: %w", pattern, err)
	}
	if len(matches) == 0 {
		return "", true, nil
	}
	latestPath := ""
	var latestModTime int64
	for _, p := range matches {
		st, statErr := os.Stat(p)
		if statErr != nil || st.IsDir() {
			continue
		}
		mt := st.ModTime().UnixNano()
		if latestPath == "" || mt > latestModTime {
			latestPath = p
			latestModTime = mt
		}
	}
	return strings.TrimSpace(latestPath), true, nil
}

// SQLITE: descriptors were only ever used by upstream's non-Steam platforms, which the
// Steam-only strip removed. The modernc.org/sqlite driver they needed is no longer linked.
// The prefix is still recognised and reported as handled so that a stray SQLITE: value
// fails here rather than falling through to plain path expansion in resolveDescriptorValue,
// which would hand back the literal expression as a username, path or ID.
func resolveSQLiteValue(v, folder string, ctx platform.PathTokenContext) (string, bool, error) {
	trimmed := strings.TrimSpace(v)
	if !strings.HasPrefix(strings.ToUpper(trimmed), sqlitePrefix) {
		return "", false, nil
	}
	return "", true, fmt.Errorf("SQLITE descriptor values are not supported in this Steam-only build")
}
