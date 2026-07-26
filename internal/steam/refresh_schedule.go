package steam

import (
	"log/slog"
	"sync"
	"time"

	"steamswitch/internal/crashlog"
	"steamswitch/internal/security"
)

const (
	// SteamAutoRefreshMinMinutes is the floor for the periodic refresh interval. Each
	// refresh scrapes community pages for every account, so a shorter cycle would be
	// rude to Steam and pointless: avatars and VAC state do not change that fast.
	SteamAutoRefreshMinMinutes = 15

	// steamAutoRefreshLaunchDelay lets the window paint and the account list load
	// before the first refresh starts competing for network and disk.
	steamAutoRefreshLaunchDelay = 5 * time.Second
)

// ClampAutoRefreshInterval normalises a stored interval: 0 (and any negative value)
// means "off", anything else is raised to SteamAutoRefreshMinMinutes.
func ClampAutoRefreshInterval(minutes int) int {
	if minutes <= 0 {
		return 0
	}
	if minutes < SteamAutoRefreshMinMinutes {
		return SteamAutoRefreshMinMinutes
	}
	return minutes
}

var (
	autoRefreshMu     sync.Mutex
	autoRefreshTicker *time.Ticker
	autoRefreshStop   chan struct{}
	autoRefreshLaunch *time.Timer
)

// StartAutoRefreshScheduler applies the saved refresh cadence: an optional refresh
// shortly after launch, plus an optional repeating one. Safe to call again after the
// settings change; the previous schedule is torn down first.
func (s *SteamService) StartAutoRefreshScheduler() {
	st, err := LoadSettings()
	if err != nil {
		slog.Warn("steam auto-refresh: load settings", "error", err)
		return
	}
	s.applyAutoRefreshSchedule(st.SteamAutoRefreshOnLaunch, ClampAutoRefreshInterval(st.SteamAutoRefreshIntervalMinutes))
}

// StopAutoRefreshScheduler cancels any pending or repeating refresh.
func (s *SteamService) StopAutoRefreshScheduler() {
	autoRefreshMu.Lock()
	defer autoRefreshMu.Unlock()
	stopAutoRefreshLocked()
}

func stopAutoRefreshLocked() {
	if autoRefreshLaunch != nil {
		autoRefreshLaunch.Stop()
		autoRefreshLaunch = nil
	}
	if autoRefreshTicker != nil {
		autoRefreshTicker.Stop()
		autoRefreshTicker = nil
	}
	if autoRefreshStop != nil {
		close(autoRefreshStop)
		autoRefreshStop = nil
	}
}

func (s *SteamService) applyAutoRefreshSchedule(onLaunch bool, intervalMinutes int) {
	autoRefreshMu.Lock()
	defer autoRefreshMu.Unlock()
	stopAutoRefreshLocked()

	if onLaunch {
		autoRefreshLaunch = time.AfterFunc(steamAutoRefreshLaunchDelay, func() {
			defer crashlog.Capture()
			if security.AppLocked() {
				return
			}
			s.StartSteamProfileRefresh()
		})
	}

	if intervalMinutes <= 0 {
		return
	}
	ticker := time.NewTicker(time.Duration(intervalMinutes) * time.Minute)
	stop := make(chan struct{})
	autoRefreshTicker = ticker
	autoRefreshStop = stop

	go func() {
		defer crashlog.Capture()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				// A locked app must not scrape in the background.
				if security.AppLocked() {
					continue
				}
				s.StartSteamProfileRefresh()
			}
		}
	}()
}
