//go:build !windows && !darwin

package steam

// No backend for this OS, so every mutating path refuses.
//
// Linux is the obvious next candidate and is closer than it looks: Steam there uses the same
// `registry.vdf` this package already parses, under `~/.steam/steam` or
// `~/.local/share/Steam`, and the process names are `steam` and `steamwebhelper`. What stops
// it being a five-line change is that Flatpak and Snap installs relocate the whole data
// directory into a sandbox and the running process is not reachable by name from outside it —
// so a Linux backend that only handled the native install would report "Steam is not running"
// with confidence for a large share of users, which is the exact failure this seam exists to
// prevent.
func newOSBackend() osBackend { return unsupportedBackend{} }
