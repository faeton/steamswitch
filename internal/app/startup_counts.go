package app

import (
	"strings"

	"steamswitch/internal/basic"
	"steamswitch/internal/platform"
	"steamswitch/internal/security"
	"steamswitch/internal/stats"
	"steamswitch/internal/steam"
)

// RegisterStartupAccountCounts wires per-platform account totals for GetStartup skeleton hints.
func RegisterStartupAccountCounts() {
	platform.SetStartupAccountCountResolver(resolveStartupAccountCounts)
	platform.SetStartupTagCountResolver(resolveStartupTagCounts)
}

func resolveStartupAccountCounts(platformNames []string, statsEnabled bool) map[string]int {
	out := make(map[string]int, len(platformNames))
	if security.AppLocked() {
		return out
	}
	for _, name := range platformNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if statsEnabled {
			if count, ok := stats.LookupPlatformAccountCount(name); ok {
				out[name] = count
				continue
			}
		}
		if strings.EqualFold(name, steam.PlatformKey) {
			out[name] = steam.CountSavedAccounts()
		} else {
			out[name] = basic.CountSavedAccounts(name)
		}
	}
	return out
}

func resolveStartupTagCounts(platformNames []string, statsEnabled bool) map[string]platform.PlatformTagCountInfo {
	out := make(map[string]platform.PlatformTagCountInfo, len(platformNames))
	if security.AppLocked() {
		return out
	}
	for _, name := range platformNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out[name] = platform.PlatformTagCountInfo{
			TagCount:           basic.CountTags(name),
			TaggedAccountCount: basic.CountTaggedAccounts(name),
		}
	}
	return out
}
