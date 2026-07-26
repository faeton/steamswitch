//go:build !windows

package winutil

// CanKillProcesses is a no-op allow on non-Windows builds.
//
// It answers "yes, you may kill these" without checking anything, which is only safe because
// the callers that matter now refuse earlier — see steam.requireProcessInspection. Returning
// false instead would be worse, not better: it surfaces as a NeedsAdminError and would send
// the user through an elevated-restart prompt that cannot fix an unimplemented platform.
func CanKillProcesses(names []string, method ClosingMethod) (blocker string, ok bool) {
	return "", true
}
