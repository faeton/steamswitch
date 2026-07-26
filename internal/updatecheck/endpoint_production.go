//go:build production

package updatecheck

import "steamswitch/internal/api"

func updateAPIURL(version string) string {
	return api.VersionCheckURL(version, false)
}
