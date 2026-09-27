package mesh

import (
	"context"
	"log/slog"

	"github.com/libp2p/go-libp2p/gologshim"
)

// UseLogHandler routes go-libp2p's own log output to h, dropping records
// below minLevel. It affects the whole process and must be called before
// the first host is created; otherwise go-libp2p logs to stderr in its own
// format.
func UseLogHandler(h slog.Handler, minLevel slog.Level) {
	gologshim.SetDefaultHandler(minLevelHandler{Handler: h, min: minLevel})
}

// minLevelHandler drops records below min.
type minLevelHandler struct {
	slog.Handler
	min slog.Level
}

func (h minLevelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.min && h.Handler.Enabled(ctx, level)
}

func (h minLevelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return minLevelHandler{Handler: h.Handler.WithAttrs(attrs), min: h.min}
}

func (h minLevelHandler) WithGroup(name string) slog.Handler {
	return minLevelHandler{Handler: h.Handler.WithGroup(name), min: h.min}
}
