package app

import (
	"context"
	"log/slog"
	"regexp"
	"sync/atomic"

	"steamswitch/internal/logsanitize"
)

// windowGoneRe is what Wails logs for an OS window event that arrives after it has already
// dropped the window. On quit that is expected, and repeats.
var windowGoneRe = regexp.MustCompile(`^(Webview)?Window #\d+ not found$`)

// wailsLogHandler is what Wails logs through: SteamIDs masked, and quit-time window noise dropped.
// Only during shutdown: the same line at any other time would mean something is really wrong.
type wailsLogHandler struct {
	slog.Handler
	shuttingDown *atomic.Bool
}

func newWailsLogHandler(next slog.Handler) *wailsLogHandler {
	return &wailsLogHandler{Handler: logsanitize.NewSteamIDHandler(next), shuttingDown: &atomic.Bool{}}
}

func (h *wailsLogHandler) markShuttingDown() { h.shuttingDown.Store(true) }

func (h *wailsLogHandler) Handle(ctx context.Context, r slog.Record) error {
	if h.shuttingDown.Load() && windowGoneRe.MatchString(r.Message) {
		return nil
	}
	return h.Handler.Handle(ctx, r)
}

func (h *wailsLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &wailsLogHandler{Handler: h.Handler.WithAttrs(attrs), shuttingDown: h.shuttingDown}
}

func (h *wailsLogHandler) WithGroup(name string) slog.Handler {
	return &wailsLogHandler{Handler: h.Handler.WithGroup(name), shuttingDown: h.shuttingDown}
}
