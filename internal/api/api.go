// Package api used to hold the endpoints of the upstream TcNo web service.
//
// SteamSwitch is a private fork and deliberately talks to no remote service: no
// anonymous statistics, no crash reports, no stability ratings, no feedback and no
// version checks. Every endpoint below therefore returns an empty string, and each
// caller treats an empty URL as "this feature is disabled" and returns early.
//
// Keeping the functions (rather than deleting them) means the call sites still compile
// and stay visible, so pointing the fork at your own service later is a one-file change.
package api

import "strings"

// Disabled reports whether outbound reporting is compiled out. Always true in this fork.
func Disabled() bool { return true }

func UserAgent(version string) string {
	return "SteamSwitch/" + strings.TrimSpace(version)
}

// AnonymousStatsUploadURL returns "" — anonymous statistics upload is removed.
func AnonymousStatsUploadURL() string { return "" }

// CrashURL returns "" — crash reports are kept locally and never uploaded.
func CrashURL() string { return "" }

// StabilityRateURL returns "" — stability ratings are not reported.
func StabilityRateURL() string { return "" }

// FeedbackURL returns "" — in-app feedback submission is removed.
func FeedbackURL() string { return "" }

// VersionCheckURL returns "" — this fork has no release feed and never self-updates.
func VersionCheckURL(version string, debug bool) string { return "" }
