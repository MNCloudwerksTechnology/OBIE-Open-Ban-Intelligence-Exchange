package logging

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name    string
		want    slog.Level
		wantErr bool
	}{
		{name: "debug", want: slog.LevelDebug},
		{name: "info", want: slog.LevelInfo},
		{name: "warn", want: slog.LevelWarn},
		{name: "error", want: slog.LevelError},
		{name: "", wantErr: true},
		{name: "INFO", wantErr: true},
		{name: "warning", wantErr: true},
		{name: "info+2", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseLevel(tt.name)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseLevel(%q) error = %v, wantErr %v", tt.name, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseLevel(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestLevelsAreParseable(t *testing.T) {
	for _, name := range Levels {
		if _, err := ParseLevel(name); err != nil {
			t.Errorf("ParseLevel(%q): %v", name, err)
		}
	}
}

func decodeLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	sc := bufio.NewScanner(buf)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("line %q is not JSON: %v", sc.Text(), err)
		}
		lines = append(lines, m)
	}
	return lines
}

func TestFactoryLoggerEmitsJSONWithComponent(t *testing.T) {
	var buf bytes.Buffer
	f := New(&buf, slog.LevelInfo)

	f.Logger("config").Info("loaded", "path", "/etc/obie/obie.yaml")
	f.Logger("mesh").With("peer", "x").WithGroup("g").Warn("slow", "ms", 12)

	lines := decodeLines(t, &buf)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	for i, want := range []string{"config", "mesh"} {
		if got := lines[i][ComponentKey]; got != want {
			t.Errorf("line %d component = %v, want %q", i, got, want)
		}
	}
	if lines[0]["msg"] != "loaded" || lines[0]["level"] != "INFO" || lines[0]["path"] != "/etc/obie/obie.yaml" {
		t.Errorf("unexpected first line: %v", lines[0])
	}
}

func TestFactoryRespectsLevel(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, slog.LevelWarn).Logger("test")

	log.Debug("d")
	log.Info("i")
	log.Warn("w")
	log.Error("e")

	lines := decodeLines(t, &buf)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 (warn, error): %v", len(lines), lines)
	}
	if lines[0]["msg"] != "w" || lines[1]["msg"] != "e" {
		t.Errorf("unexpected lines: %v", lines)
	}
}
