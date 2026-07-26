package main

import (
	"embed"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"

	"steamswitch/internal/actionlog"
	"steamswitch/internal/app"
	"steamswitch/internal/appclient"
	"steamswitch/internal/basic"
	"steamswitch/internal/cli"
	"steamswitch/internal/controllerinput"
	"steamswitch/internal/crashlog"
	"steamswitch/internal/discordrpc"
	"steamswitch/internal/ipc"
	"steamswitch/internal/platform"
	"steamswitch/internal/security"
	"steamswitch/internal/shortcuts"
	"steamswitch/internal/stability"
	"steamswitch/internal/stats"
	"steamswitch/internal/steam"
	"steamswitch/internal/tray"
	"steamswitch/internal/vault"
	"steamswitch/internal/winutil"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/trayicon.png
var trayIconPNG []byte

//go:embed updater-key.pub
var updaterPublicKey []byte

var (
	platformSvc   = &platform.PlatformService{}
	basicSvc      = basic.NewBasicService(platformSvc)
	steamSvc      = steam.NewSteamService()
	controllerSvc = controllerinput.NewService()
	securitySvc   = security.NewService()
	discordRPC    = discordrpc.NewManager()
	// Constructed with a nil engine: bindings are generated from the type, but the engine
	// itself needs the data root, which is not resolved until main(). The service resolves
	// the process-wide engine lazily on first use.
	sessionKitSvc = steam.NewSessionKitService(nil)
	vaultSvc      = vault.NewService()

	crashSubmitted bool
)

func init() {
	winutil.SetEmbeddedFrontendFS(assets)

	application.RegisterEvent[string]("navigate")

	application.RegisterEvent[app.ToastPayload]("toast")
	application.RegisterEvent[stability.StabilityPromptPayload]("stability-prompt")
	application.RegisterEvent[string](controllerinput.EventName)
	application.RegisterEvent[steam.AccountPatch](steam.AccountUpdatedEvent)
	application.RegisterEvent[basic.AccountImagePatch](basic.AccountImageUpdatedEvent)
	application.RegisterEvent[basic.GameStatsUpdatedPatch](basic.GameStatsUpdatedEvent)
	application.RegisterEvent[string](platform.ActionBarStatusEvent)
	application.RegisterEvent[shortcuts.ListPayload](shortcuts.UpdatedEvent)
	application.RegisterEvent[shortcuts.FilesDroppedPayload](shortcuts.FilesDroppedEvent)
	application.RegisterEvent[platform.UpdateAvailablePayload](platform.AppUpdateAvailableEvent)
	application.RegisterEvent[bool](platform.UpdateCheckFailedEvent)
	application.RegisterEvent[platform.PlatformsJSONUpdatePayload](platform.PlatformsJSONUpdateFoundEvent)
	application.RegisterEvent[platform.PlatformsJSONUpdatePayload](platform.PlatformsJSONUpdatedEvent)
	application.RegisterEvent[platform.UserDataMoveProgressPayload](platform.UserDataMoveProgressEvent)

	platform.SetSteamLaunchHooks(steam.SaveFolderFromConfirmedExe, steam.ResolveSteamExePath)
	platform.SetSteamReset(steam.ResetToDefaults)
	// Restoring a backup writes over the same config and userdata trees the Session Kit
	// tracks, so it answers to the same gate a bare swap does.
	platform.SetRestoreGuard(func(platformKey string, restore func() error) error {
		if !strings.EqualFold(strings.TrimSpace(platformKey), steam.PlatformKey) {
			return restore()
		}
		return steam.RestorePermitted(restore)
	})
	platform.SetControllerSupportChangedHook(controllerSvc.SetEnabled)
	platform.SetDiscordPresenceRefreshHook(discordRPC.RefreshAsync)
	platform.SetPlatformLaunchers(func() error { return steam.LaunchSteamOnly(nil) }, func(platformKey string) error {
		return basic.LaunchBasic(basic.FlowDeps{PS: platformSvc}, platformKey, nil)
	})
	platform.SetPlatformLaunchAs(func(forceAdmin bool) error { return steam.LaunchSteamOnlyAs(forceAdmin, nil) }, func(platformKey string, forceAdmin bool) error {
		return basic.LaunchBasicAs(basic.FlowDeps{PS: platformSvc}, platformKey, forceAdmin, nil)
	})
	security.SetStatusChangedHook(func() {
		if security.AppLocked() {
			// Locking must remove the decrypted secrets from memory, not merely refuse to
			// render them. Without this the vault would stay readable to anything in-process
			// for as long as the app ran.
			vault.DropCache()
		} else {
			basic.SyncAllTrayKnownAccounts()
			steam.SyncTrayKnownAccounts()
			refreshVaultInputs()
		}
		tray.RefreshMenuIfSet()
	})
	app.RegisterStartupAccountCounts()
}

func main() {
	exeDir, err := platform.ResolveExeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "exe dir:", err)
		os.Exit(1)
	}
	if err := platform.InitDataPaths(exeDir); err != nil {
		fmt.Fprintln(os.Stderr, "init data paths:", err)
		os.Exit(1)
	}
	security.CleanupTransientState()

	// The session-kit store lives under the data root, so the engine can only be built once
	// InitDataPaths has run. A failure here is not fatal: the app must still start so the
	// user can reach Settings and fix whatever is wrong with the data directory. Switching
	// then reports Toast_Kit_NotReady rather than silently falling back to an unjournalled
	// swap, which is the failure mode the kit exists to prevent.
	if _, err := steam.InitSessionKit(steamSvc); err != nil {
		log.Printf("session kit unavailable: %v", err)
	}
	refreshVaultInputs()

	idx, idxErr := cli.LoadPlatformIndex()
	idxPtr := idx
	if idxErr != nil {
		log.Printf("cli platforms index: %v", idxErr)
		idxPtr = nil
	}

	parsed, err := cli.Parse(os.Args[1:], idxPtr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	lvl := app.ResolvedLogLevel(parsed)
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})))
	actionlog.Init()

	startupSettings, _ := loadStartupSettings()
	syncOfflineModeFromSettings(startupSettings)
	stats.SetStatsCollectionEnabled(startupSettings.StatsEnabled)

	if crashlog.HasPending() && !startupSettings.OfflineMode && startupSettings.CrashReportAutoSubmit {
		crashSubmitted = crashlog.SubmitPending()
	}
	defer crashlog.CaptureFatal()

	if parsed.Kind == cli.KindHelp || parsed.Help {
		fmt.Print(cli.HelpText())
		os.Exit(0)
	}

	disp := &app.Dispatch{
		SteamSvc:    steamSvc,
		BasicSvc:    basicSvc,
		PlatformSvc: platformSvc,
	}

	if parsed.IsListCommand() {
		winutil.AttachParentConsole()
		if err := disp.RunList(parsed, idx); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	releaseSingleton, running, err := winutil.TryAcquireSingleton()
	if err != nil {
		fmt.Fprintln(os.Stderr, "singleton:", err)
		os.Exit(1)
	}
	if running {
		if ferr := ipc.ForwardArgs(os.Args[1:]); ferr != nil {
			fmt.Fprintln(os.Stderr, "another instance is running; IPC forward failed:", ferr)
			os.Exit(1)
		}
		os.Exit(0)
	}
	defer releaseSingleton()
	winutil.RegisterSingletonReleaser(releaseSingleton)

	platform.RunUserDataMoveCleanup(exeDir, parsed.UserDataMoveFrom, parsed.UserDataMoveTo)

	if parsed.NeedsHeadlessMutex() {
		winutil.AttachParentConsole()
		if herr := disp.RunHeadless(parsed); herr != nil {
			fmt.Fprintln(os.Stderr, herr)
			os.Exit(1)
		}
		os.Exit(0)
	}

	app.RunGUI(app.RunGUIParams{
		Parsed:           parsed,
		GuiSettings:      startupSettings,
		Services:         serviceList(),
		Dispatch:         disp,
		DiscordRPC:       discordRPC,
		CrashSubmitted:   crashSubmitted,
		StartupToast:     parsed.StartupToast,
		EmbeddedAssets:   assets,
		TrayIconPNG:      trayIconPNG,
		UpdaterPublicKey: updaterPublicKey,
	})
}

func serviceList() []application.Service {
	return []application.Service{
		application.NewService(&FilesystemService{}),
		application.NewService(platformSvc),
		application.NewService(steamSvc),
		application.NewService(controllerSvc),
		application.NewService(basicSvc),
		application.NewService(securitySvc),
		application.NewService(sessionKitSvc),
		application.NewService(vaultSvc),
		application.NewService(shortcuts.NewService(platformSvc)),
	}
}

// refreshVaultInputs feeds the vault the two things it deliberately does not read for
// itself: the user's Web API key, and Steam's own record of when each account last logged
// in. Both are pushed in rather than pulled, so `internal/vault` never imports the Steam
// engine.
//
// Called at startup and again whenever the app unlocks, since neither value is available
// while locked.
func refreshVaultInputs() {
	if st, err := steam.LoadSettings(); err == nil {
		vault.SetAPIKey(st.SteamWebApiKey)
	}
	vault.SetLastUsed(steam.LastLoginTimes())
}

func loadStartupSettings() (platform.AppSettings, error) {
	d, err := platform.ResolveExeDir()
	if err != nil {
		return platform.AppSettings{}, err
	}
	return platform.LoadAppSettings(d)
}

func syncOfflineModeFromSettings(s platform.AppSettings) {
	appclient.SetOfflineMode(s.OfflineMode)
}
