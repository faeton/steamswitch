package platform

import (
	buildinfo "steamswitch/build"
)

func appVersionFromBuildConfig() string {
	return buildinfo.Version()
}
