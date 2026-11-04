package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"steamswitch/internal/cli"
	"steamswitch/internal/platform"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type fakeNotifier struct {
	startErr error
	stopped  bool
}

func (f *fakeNotifier) ServiceStartup(context.Context, application.ServiceOptions) error {
	return f.startErr
}
func (f *fakeNotifier) ServiceShutdown() error { f.stopped = true; return nil }

// The reported failure: a bare Mac binary exited because this returned an error.
func TestOptionalNotifications_StartupFailureIsNotFatal(t *testing.T) {
	f := &fakeNotifier{startErr: errors.New("notifications require a valid bundle identifier")}
	n := &optionalNotifications{inner: f}
	if err := n.ServiceStartup(context.Background(), application.ServiceOptions{}); err != nil {
		t.Fatalf("startup returned %v, which stops the whole app", err)
	}
	if err := n.ServiceShutdown(); err != nil || f.stopped {
		t.Fatalf("shut down a service that never started: %v %v", err, f.stopped)
	}
}

func TestOptionalNotifications_NormalStartupStillShutsDown(t *testing.T) {
	f := &fakeNotifier{}
	n := &optionalNotifications{inner: f}
	if err := n.ServiceStartup(context.Background(), application.ServiceOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := n.ServiceShutdown(); err != nil || !f.stopped {
		t.Fatalf("not shut down: %v %v", err, f.stopped)
	}
	platform.SetNativeNotifier(nil)
}

func TestWailsLogHandler_DropsWindowNoiseOnlyDuringShutdown(t *testing.T) {
	var buf bytes.Buffer
	h := newWailsLogHandler(slog.NewTextHandler(&buf, nil))
	log := slog.New(h)

	log.Warn("Window #1 not found")
	if !strings.Contains(buf.String(), "Window #1 not found") {
		t.Fatal("before shutdown this line means something is wrong, so it must stay")
	}
	buf.Reset()
	h.markShuttingDown()
	log.Warn("Window #1 not found")
	log.Warn("WebviewWindow #1 not found")
	if buf.Len() != 0 {
		t.Fatalf("logged at quit: %s", buf.String())
	}
	log.Warn("something else")
	if !strings.Contains(buf.String(), "something else") {
		t.Fatal("dropped an unrelated line")
	}
}

func TestWailsLogHandler_MasksSteamIDs(t *testing.T) {
	var buf bytes.Buffer
	slog.New(newWailsLogHandler(slog.NewTextHandler(&buf, nil))).Debug("x")
	slog.New(newWailsLogHandler(slog.NewTextHandler(&buf, nil))).Info("Asset Request:", "path", "/img/profiles/steam/76561198000123456.jpg")
	if strings.Contains(buf.String(), "76561198000123456") {
		t.Fatalf("full id in Wails log: %s", buf.String())
	}
}

func TestFrontendReport_IsLoggedAndAnswered(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	h := newCompositeAssetHandler(fstest.MapFS{"index.html": {Data: []byte("x")}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", frontendReportPath+"?stage=error&detail=boom", nil))
	if rec.Code != 204 {
		t.Fatalf("code %d", rec.Code)
	}
	if !strings.Contains(buf.String(), "boom") {
		t.Fatalf("not logged: %s", buf.String())
	}
	if !frontendReported.Load() {
		t.Fatal("report not recorded")
	}
}

func TestMainWindowOptions_DiagnosticFlags(t *testing.T) {
	if opts := mainWindowOptions(platform.AppSettings{}, cli.Parsed{}); !opts.Frameless || opts.URL != "/" {
		t.Fatalf("defaults changed: frameless %v url %q", opts.Frameless, opts.URL)
	}
	opts := mainWindowOptions(platform.AppSettings{}, cli.Parsed{FramedWindow: true, DiagPage: true})
	if opts.Frameless || opts.URL != "/diag.html" {
		t.Fatalf("frameless %v url %q", opts.Frameless, opts.URL)
	}
}

func TestFrontendReport_DistinctStagesCannotGrowWithoutLimit(t *testing.T) {
	for i := 0; i < maxFrontendStages*3; i++ {
		rec := httptest.NewRecorder()
		handleFrontendReport(rec, httptest.NewRequest("GET", fmt.Sprintf("%s?stage=x%d", frontendReportPath, i), nil))
	}
	n := 0
	frontendStagesSeen.Range(func(_, _ any) bool { n++; return true })
	if n > maxFrontendStages {
		t.Fatalf("%d stages remembered", n)
	}
}
