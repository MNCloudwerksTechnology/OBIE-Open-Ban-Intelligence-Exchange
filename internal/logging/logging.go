// Package logging sets up structured JSON logging for the OBIE binaries.
//
// All loggers are obtained from a Factory and are bound to a component name,
// so every log line carries a "component" attribute.
package logging

import (
	"fmt"
	"io"
	"log/slog"
)

// ComponentKey is the attribute key naming the component that emitted a line.
const ComponentKey = "component"

// Levels lists the accepted level names in increasing severity.
var Levels = []string{"debug", "info", "warn", "error"}

// ParseLevel converts a level name (see Levels) into a slog.Level.
func ParseLevel(name string) (slog.Level, error) {
	switch name {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level %q (want one of debug, info, warn, error)", name)
	}
}

// Factory hands out component loggers that share one JSON handler.
type Factory struct {
	handler slog.Handler
}

// New returns a Factory writing JSON lines at or above level to w.
func New(w io.Writer, level slog.Level) *Factory {
	return &Factory{handler: slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})}
}

// Logger returns a logger whose every line has component set to name.
func (f *Factory) Logger(name string) *slog.Logger {
	return slog.New(f.handler).With(ComponentKey, name)
}
