package mesh

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestMinLevelHandler(t *testing.T) {
	var buf bytes.Buffer
	h := minLevelHandler{Handler: slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}), min: slog.LevelWarn}
	log := slog.New(h).With("logger", "swarm").WithGroup("g")
	log.Info("dropped")
	log.Warn("kept", "k", "v")
	out := buf.String()
	if strings.Contains(out, "dropped") || !strings.Contains(out, `"msg":"kept"`) ||
		!strings.Contains(out, `"logger":"swarm"`) || !strings.Contains(out, `"g":{"k":"v"}`) {
		t.Errorf("output = %s", out)
	}
}
