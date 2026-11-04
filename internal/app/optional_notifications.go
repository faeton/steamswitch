package app

import (
	"context"
	"log/slog"

	"steamswitch/internal/platform"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// notifierLifecycle is the part of Wails' NotificationService this wrapper needs.
type notifierLifecycle interface {
	ServiceStartup(ctx context.Context, options application.ServiceOptions) error
	ServiceShutdown() error
}

// optionalNotifications stops a missing notification service from stopping the app.
//
// On macOS the service refuses to start without a bundle identifier, so a bare binary (one not
// inside a .app) exited at launch with "notifications require a valid bundle identifier". Native
// notifications are a nicety and the rest of the app works without them. Every send goes through
// platform.NotifyNative, which does nothing once the notifier is cleared.
type optionalNotifications struct {
	inner   notifierLifecycle
	started bool
}

func (n *optionalNotifications) ServiceName() string { return "notifications" }

func (n *optionalNotifications) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	if err := n.inner.ServiceStartup(ctx, options); err != nil {
		slog.Warn("native notifications unavailable, continuing without them", "err", err)
		platform.SetNativeNotifier(nil)
		return nil
	}
	n.started = true
	return nil
}

func (n *optionalNotifications) ServiceShutdown() error {
	if !n.started {
		return nil
	}
	return n.inner.ServiceShutdown()
}
