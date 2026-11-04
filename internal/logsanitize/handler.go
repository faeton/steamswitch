package logsanitize

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
)

// A SteamID64 is 17 digits in the individual-account range. It reached the logs through URLs and
// paths as well as ID fields, so masking at the handler catches what call sites miss.
var steamID64Re = regexp.MustCompile(`\b7656\d{13}\b`)

// MaskSteamIDs keeps the last 6 digits, the same as the steam package's tailSteamID, so a line
// from either source still lines up.
func MaskSteamIDs(text string) string {
	return steamID64Re.ReplaceAllStringFunc(text, func(id string) string {
		return "…" + id[len(id)-6:]
	})
}

// NewSteamIDHandler masks SteamID64s in everything logged through it, the message included.
func NewSteamIDHandler(next slog.Handler) slog.Handler {
	return steamIDHandler{next: next}
}

type steamIDHandler struct{ next slog.Handler }

func (h steamIDHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h steamIDHandler) Handle(ctx context.Context, r slog.Record) error {
	out := slog.NewRecord(r.Time, r.Level, MaskSteamIDs(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(maskAttr(a))
		return true
	})
	return h.next.Handle(ctx, out)
}

func (h steamIDHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	masked := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		masked[i] = maskAttr(a)
	}
	return steamIDHandler{next: h.next.WithAttrs(masked)}
}

func (h steamIDHandler) WithGroup(name string) slog.Handler {
	return steamIDHandler{next: h.next.WithGroup(name)}
}

func maskAttr(a slog.Attr) slog.Attr {
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindString:
		a.Value = slog.StringValue(MaskSteamIDs(v.String()))
	case slog.KindUint64, slog.KindInt64:
		a.Value = slog.StringValue(MaskSteamIDs(v.String()))
	case slog.KindGroup:
		group := v.Group()
		masked := make([]slog.Attr, len(group))
		for i, g := range group {
			masked[i] = maskAttr(g)
		}
		a.Value = slog.GroupValue(masked...)
	case slog.KindAny:
		switch x := v.Any().(type) {
		case error:
			a.Value = slog.StringValue(MaskSteamIDs(x.Error()))
		case fmt.Stringer:
			a.Value = slog.StringValue(MaskSteamIDs(x.String()))
		}
	}
	return a
}
