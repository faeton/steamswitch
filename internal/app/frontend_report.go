package app

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"steamswitch/internal/crashlog"
)

// The page reporting on itself, so a blank window leaves evidence.
//
// When the window shows nothing there is no DevTools and no console to read. The page therefore
// tells the backend how far it got and what went wrong, over the app's own local asset scheme, and
// it lands in the normal log. Nothing leaves the machine. Reading the log then separates "the page
// ran and painted, so the window is not showing it" from "the script failed" from "it never loaded".
const frontendReportPath = "/__frontend-report"

const (
	frontendSilentAfter  = 20 * time.Second
	maxFrontendErrorLogs = 40
	maxFrontendTextLen   = 500
)

var (
	frontendReported   atomic.Bool
	frontendErrorCount atomic.Int32
	frontendStagesSeen sync.Map
)

func handleFrontendReport(w http.ResponseWriter, r *http.Request) {
	frontendReported.Store(true)
	q := r.URL.Query()
	stage := clipFrontendText(q.Get("stage"))
	detail := clipFrontendText(q.Get("detail"))
	if stage == "error" {
		if frontendErrorCount.Add(1) <= maxFrontendErrorLogs {
			slog.Warn("page error", "detail", detail)
		}
	} else if _, seen := frontendStagesSeen.LoadOrStore(stage, true); !seen {
		slog.Info("page reached", "stage", stage, "detail", detail)
	}
	w.WriteHeader(http.StatusNoContent)
}

func clipFrontendText(s string) string {
	if len(s) > maxFrontendTextLen {
		return s[:maxFrontendTextLen] + "…"
	}
	return s
}

// watchFrontendReports leaves a line in the log when the page never reports at all.
func watchFrontendReports(ctx context.Context) {
	go func() {
		defer crashlog.Capture()
		select {
		case <-ctx.Done():
		case <-time.After(frontendSilentAfter):
			if !frontendReported.Load() {
				slog.Warn("page has not reported after 20s: the webview may not be loading or running it")
			}
		}
	}()
}
