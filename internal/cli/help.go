package cli

// HelpText returns CLI usage for console / --help.
func HelpText() string {
	return `SteamSwitch — CLI

Swap (Steam):        +s:<steamId64>[:<personaState>]
                     steamswitch://s:<steamId64>[:<personaState>]
Swap (other):        +<platformShort>:<uniqueId>
                     (platformShort is the first Identifiers entry in Platforms.json)

Extra argv for the platform exe (e.g. Steam) after swap/launch from CLI or shortcuts:
                     +s:<id> -dev -x
                     (any token that is not a SteamSwitch flag, +swap, logout, or a GUI page name)

Swap & launch (desktop shortcuts / game tiles):
                     +s:<steamId64> --run-appid=<appId>
                     (Steam only; launches steam://rungameid/<appId> after swap)
                     +s:<id> --run-shortcut=<urlEncodedFile.lnk>
                     +<platformShort>:<uniqueId> --run-shortcut=<urlEncodedFile.lnk>
                     (launches cached .lnk/.url after swap; resolves Desktop / Start Menu if cache missing)

Open GUI to page:    <PlatformName>      e.g. Steam
                     --page=<PlatformName>   (same; useful after restart-as-admin)

Logout:              logout[:<PlatformName>[:<accountId>]]

List:                --list-platforms
                     --list-accounts
                     --list-accounts=<PlatformNameOrAlias>
                     --json        JSON instead of plain text (use with list flags only)

Seal a roster:       --seal-roster            (bundle to stdout)
                     --seal-roster=<file>     (bundle to a file, owner-only)
                     Reads one JSON document on stdin:
                       {"passphrase":"...","accounts":[{"steamId64":"765...", ...}]}
                     Writes a passphrase-sealed .ssroster the app can import.
                     The passphrase is read from stdin, never from argv, so it does
                     not appear in the process list or shell history. Pipe the
                     document in — do not write it to a file first; a plaintext file
                     of credentials cannot be reliably erased afterwards.

Other:               -h, --help    Show this help
                     -v, --verbose Debug logging (same as --log-level=debug)
                     --log-level=  Logging: debug, info, warn, error (app + Wails; default info)
                     -tray, --tray Start with the main window hidden (tray / background)

Second instance forwards arguments to the running GUI via a named pipe (Windows).
`
}
