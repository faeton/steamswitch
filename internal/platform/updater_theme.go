package platform

import "steamswitch/internal/updatertheme"

func (*PlatformService) SetUpdaterThemeCSS(css string) {
	updatertheme.SetCSS(css)
}
