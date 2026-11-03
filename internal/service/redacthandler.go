package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/castrowithcee/qatlas-kit/redact"
)

// redactHandler removes registered secrets from the message and the attributes of every record before the
// wrapped handler sees them. Attributes that are not plain strings are rendered as text first.
type redactHandler struct {
	next slog.Handler
	r    *redact.Redactor
}

func (h redactHandler) Enabled(ctx context.Context, l slog.Level) bool { return h.next.Enabled(ctx, l) }

func (h redactHandler) Handle(ctx context.Context, rec slog.Record) error {
	out := slog.NewRecord(rec.Time, rec.Level, h.r.Apply(rec.Message), rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(h.scrub(a))
		return true
	})
	return h.next.Handle(ctx, out)
}

func (h redactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clean := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		clean[i] = h.scrub(a)
	}
	return redactHandler{next: h.next.WithAttrs(clean), r: h.r}
}

func (h redactHandler) WithGroup(name string) slog.Handler {
	return redactHandler{next: h.next.WithGroup(name), r: h.r}
}

func (h redactHandler) scrub(a slog.Attr) slog.Attr {
	v := a.Value.Resolve()
	key := h.r.Apply(a.Key)
	switch v.Kind() {
	case slog.KindString:
		return slog.String(key, h.r.Apply(v.String()))
	case slog.KindGroup:
		group := v.Group()
		clean := make([]any, len(group))
		for i, g := range group {
			clean[i] = h.scrub(g)
		}
		return slog.Group(key, clean...)
	case slog.KindAny:
		return slog.String(key, h.r.Apply(fmt.Sprint(v.Any())))
	}
	return slog.Attr{Key: key, Value: v}
}
