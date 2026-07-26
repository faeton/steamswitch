package steam

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"steamswitch/internal/fsutil"
	"steamswitch/internal/platform"
	"steamswitch/internal/security"
	"steamswitch/internal/stability"
	"steamswitch/internal/stats"
	"steamswitch/internal/tray"
	"steamswitch/internal/vault"
)

// SwapToAccount: empty steamID64 clears AutoLoginUser (Add New). personaState -1 uses Steam_OverrideState; < -1 skips localconfig persona edit. extraLaunchArgs append after settings argv.
//
// This is the *bare* swap, taken by everything that does not go through the Session Kit
// engine: the tray, desktop shortcuts, `steamswitch://` and the CLI. It runs under the
// engine's transaction lock so it cannot interleave with a journaled switch, and it is refused
// outright when a kit is live on a different account — see sessionkit_guard.go.
func SwapToAccount(steamID64 string, personaState int, extraLaunchArgs []string) error {
	return guardedSwap(steamID64, func() error {
		// A switch beginning is the earliest honest signal that a Guard code will be
		// wanted, and fetching from here rather than from the moment Steam shows its
		// prompt is most of the perceived speed of that feature.
		//
		// Fire-and-forget, and cancelled if the swap fails: a mail server must never delay
		// a switch, let alone fail one.
		vault.Prewarm(steamID64)
		err := swapToAccountLocked(steamID64, personaState, extraLaunchArgs)
		if err != nil {
			vault.CancelPrewarm(steamID64)
		}
		return err
	})
}

// swapToAccountLocked is the swap body. It runs with the engine's transaction lock held and
// must not call back into the engine.
func swapToAccountLocked(steamID64 string, personaState int, extraLaunchArgs []string) (err error) {
	if err := security.RequireUnlocked(); err != nil {
		return err
	}
	// Before the already-on-this-account short circuit: on a build that cannot see processes
	// that check reads loginusers.vdf and can return nil, reporting success for a switch that
	// never happened.
	if err := requireProcessInspection(); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			platform.EmitActionBarStatusI18n("Status_FailedLog")
			return
		}
		platform.EmitActionBarStatus("")
	}()
	platform.EmitActionBarStatusI18n("Status_Init")

	st, err := LoadSettings()
	if err != nil {
		return err
	}
	exeDir, err := platform.ResolveExeDir()
	if err != nil {
		return err
	}
	app, err := platform.LoadAppSettings(exeDir)
	if err != nil {
		return err
	}
	raw, err := platform.LoadPlatformsJSON(exeDir)
	if err != nil {
		return err
	}
	root, err := ResolveInstallFolder(exeDir, st, app, raw)
	if err != nil {
		return err
	}
	if root == "" {
		return fmt.Errorf("steam install folder not found")
	}

	if tr := strings.TrimSpace(steamID64); tr != "" && len(extraLaunchArgs) == 0 {
		if users, err := ParseLoginUsers(LoginUsersPath(root)); err == nil {
			if a := ActiveSessionSteamID64(users); a != "" && strings.EqualFold(strings.TrimSpace(a), tr) {
				return nil
			}
		}
	}

	platform.EmitActionBarStatusI18nPlatform("Status_ClosingPlatform", "Steam")
	names := backend.ProcessNames()
	if err := backend.CanClose(names, st.ClosingMethod); err != nil {
		platform.EmitActionBarStatusI18nPlatform("Status_ClosingPlatformFailed", "Steam")
		return err
	}
	if err := backend.Close(names, st.ClosingMethod); err != nil {
		steamLog.Warn("close steam processes", slog.Any("err", err))
	}

	platform.EmitActionBarStatusI18n("Status_ActionBar_UpdatingSteamLogin")
	pS := personaState
	if pS == -1 {
		pS = st.SteamOverrideState
	}

	if err := writeLoginUsersAndRegistry(root, steamID64, st.SteamRememberPassword); err != nil {
		return err
	}

	if err := setShowSteamSwitcher(root, st.ShowSteamSwitcher); err != nil {
		steamLog.Warn("config.vdf AlwaysShowUserChooser", slog.Any("err", err))
	}

	if pS >= 0 && strings.TrimSpace(steamID64) != "" {
		platform.EmitActionBarStatusI18n("Status_ActionBar_UpdatingSteamPersona")
		platform.EmitActionBarStatusI18nVars("Status_UpdatingFile", map[string]string{"file": "localconfig.vdf"})
		if err := setPersonaStateLocalConfig(root, steamID64, pS); err != nil {
			steamLog.Warn("localconfig ePersonaState", slog.Any("err", err))
		}
	}

	RecordTrayRecentAfterSwap(steamID64)
	stability.OnSuccessfulSwitch("Steam")
	if err := stats.IncrementSwitches("Steam"); err != nil {
		return err
	}
	platform.TriggerDiscordPresenceRefresh()

	if !st.AutoStart {
		tray.MaybeHideMainWindow()
		return nil
	}

	platform.EmitActionBarStatusI18nPlatform("Status_StartingPlatform", "Steam")
	if err := backend.Launch(root, buildSteamArgs(st, extraLaunchArgs), launchOpts(st, st.RunAsAdmin)); err != nil {
		return err
	}
	tray.MaybeHideMainWindow()
	return nil
}

func LaunchSteamOnly(extraLaunchArgs []string) error {
	if err := security.RequireUnlocked(); err != nil {
		return err
	}
	defer platform.EmitActionBarStatus("")
	platform.EmitActionBarStatusI18nPlatform("Status_StartingPlatform", "Steam")

	st, err := LoadSettings()
	if err != nil {
		return err
	}
	exeDir, err := platform.ResolveExeDir()
	if err != nil {
		return err
	}
	app, err := platform.LoadAppSettings(exeDir)
	if err != nil {
		return err
	}
	raw, err := platform.LoadPlatformsJSON(exeDir)
	if err != nil {
		return err
	}
	root, err := ResolveInstallFolder(exeDir, st, app, raw)
	if err != nil {
		return err
	}
	if root == "" {
		return fmt.Errorf("steam install folder not found")
	}
	return backend.Launch(root, buildSteamArgs(st, extraLaunchArgs), launchOpts(st, st.RunAsAdmin))
}

func LaunchSteamOnlyAs(forceAdmin bool, extraLaunchArgs []string) error {
	if err := security.RequireUnlocked(); err != nil {
		return err
	}
	defer platform.EmitActionBarStatus("")
	platform.EmitActionBarStatusI18nPlatform("Status_StartingPlatform", "Steam")

	st, err := LoadSettings()
	if err != nil {
		return err
	}
	exeDir, err := platform.ResolveExeDir()
	if err != nil {
		return err
	}
	app, err := platform.LoadAppSettings(exeDir)
	if err != nil {
		return err
	}
	raw, err := platform.LoadPlatformsJSON(exeDir)
	if err != nil {
		return err
	}
	root, err := ResolveInstallFolder(exeDir, st, app, raw)
	if err != nil {
		return err
	}
	if root == "" {
		return fmt.Errorf("steam install folder not found")
	}
	admin := st.RunAsAdmin || forceAdmin
	return backend.Launch(root, buildSteamArgs(st, extraLaunchArgs), launchOpts(st, admin))
}

// launchOpts narrows the settings to the OS-neutral pair a backend can act on.
func launchOpts(st Settings, admin bool) LaunchOpts {
	return LaunchOpts{Admin: admin, StartingMethod: strings.TrimSpace(st.StartingMethod)}
}

func buildSteamArgs(st Settings, extraLaunchArgs []string) []string {
	args := append([]string{}, platform.LaunchArgTokens(st.LaunchArguments)...)
	if len(extraLaunchArgs) > 0 {
		args = append(args, extraLaunchArgs...)
	}
	return args
}

// rememberPassword mirrors Settings.SteamRememberPassword: false leaves no signed-in session
// behind, which is the point of the option on a shared machine.
func writeLoginUsersAndRegistry(steamRoot, selectedID64 string, rememberPassword bool) error {
	loginPath := LoginUsersPath(steamRoot)
	// Read-modify-write the real tree rather than rebuilding it from LoginUser: see
	// loginusers_edit.go. Rebuilding drops any field this build does not model.
	f, err := readLoginUsersTree(loginPath)
	if err != nil {
		return err
	}
	autoUser := f.applyLoginSelection(selectedID64, rememberPassword)

	if data, err := os.ReadFile(loginPath); err == nil && len(data) > 0 {
		_ = fsutil.WriteFileAtomic(strings.TrimSuffix(loginPath, ".vdf")+".vdf_last", data, 0o644)
	}

	out := f.render()
	platform.EmitActionBarStatusI18nVars("Status_UpdatingFile", map[string]string{"file": "loginusers.vdf"})
	if err := fsutil.WriteFileAtomic(loginPath, out, 0o644); err != nil {
		return err
	}

	// Where this lands is the one genuinely OS-specific part of a switch: the Windows
	// registry, or registry.vdf on macOS. The backend keeps AutoLoginUser and
	// RememberPassword in step with each other, since two stores disagreeing about whether
	// to stay signed in is how an account reaches a password prompt it never should.
	platform.EmitActionBarStatusI18n("Status_UpdatingRegistry")
	return backend.SetAutoLogin(steamRoot, autoUser, rememberPassword)
}

func setShowSteamSwitcher(steamRoot string, show bool) error {
	path := filepath.Join(steamRoot, "config", "config.vdf")
	platform.EmitActionBarStatusI18nVars("Status_UpdatingFile", map[string]string{"file": "config.vdf"})
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(raw)
	val := "0"
	if show {
		val = "1"
	}
	lines := strings.Split(s, "\n")
	var out []string
	done := false
	for _, line := range lines {
		if strings.Contains(line, "AlwaysShowUserChooser") && strings.Contains(line, `"`) && !done {
			out = append(out, fmt.Sprintf(`				"AlwaysShowUserChooser"		"%s"`, val))
			done = true
			continue
		}
		out = append(out, line)
	}
	if !done {
		return nil
	}
	return fsutil.WriteFileAtomic(path, []byte(strings.Join(out, "\n")), 0o644)
}

func RemoveSteamAccountFromVDF(steamRoot, steamID64 string) error {
	loginPath := LoginUsersPath(steamRoot)
	f, err := readLoginUsersTree(loginPath)
	if err != nil {
		return err
	}
	f.removeAccount(steamID64)
	if data, err := os.ReadFile(loginPath); err == nil && len(data) > 0 {
		_ = fsutil.WriteFileAtomic(strings.TrimSuffix(loginPath, ".vdf")+".vdf_last", data, 0o644)
	}
	out := f.render()
	return fsutil.WriteFileAtomic(loginPath, out, 0o644)
}
