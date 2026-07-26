package updatecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"steamswitch/internal/api"
	"steamswitch/internal/appclient"
)

// PlatformsJSONRawURL is the canonical remote Platforms.json used for background updates.
// TODO: switch refs/heads/go to refs/heads/main when the go branch is merged to main.
// PlatformsJSONRawURL points at upstream's 24-platform descriptor file. Fetching it is
// disabled in this Steam-only fork (see platform.runLaunchPlatformsJSONCheck); the
// constant remains only so the disabled code path still compiles.
const PlatformsJSONRawURL = "https://raw.githubusercontent.com/faeton/steamswitch/refs/heads/master/Platforms.json"

const maxPlatformsJSONBytes = 4 << 20

type platformsVersion struct {
	Version string `json:"Version"`
}

// FetchRemotePlatformsJSON downloads Platforms.json from GitHub.
func FetchRemotePlatformsJSON(ctx context.Context, appVersion string) ([]byte, error) {
	if appclient.IsOfflineMode() {
		return nil, appclient.ErrOfflineMode
	}
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, PlatformsJSONRawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", api.UserAgent(strings.TrimSpace(appVersion)))
	resp, err := appclient.Shared.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPlatformsJSONBytes))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("updatecheck: platforms HTTP %d", resp.StatusCode)
	}
	return body, nil
}

// ParsePlatformsJSONVersion reads the top-level Version field from Platforms.json.
func ParsePlatformsJSONVersion(data []byte) (string, error) {
	var pv platformsVersion
	if err := json.Unmarshal(data, &pv); err != nil {
		return "", err
	}
	v := strings.TrimSpace(pv.Version)
	if v == "" {
		return "", fmt.Errorf("updatecheck: Platforms.json missing Version")
	}
	return v, nil
}

// IsVersionNewer reports whether latest is strictly newer than current using the
// project semver clock (MAJOR.MINOR.PATCH as year.month.day).
func IsVersionNewer(latest, current string) bool {
	latest = strings.TrimSpace(latest)
	current = strings.TrimSpace(current)
	if latest == "" {
		return false
	}
	if current == "" {
		return true
	}
	if _, err := ParseVersionClock(latest); err != nil {
		return false
	}
	if _, err := ParseVersionClock(current); err != nil {
		return true
	}
	return !IsUpToDate(current, latest)
}
