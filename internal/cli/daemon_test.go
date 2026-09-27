package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "obie.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunDaemonCheckConfig(t *testing.T) {
	valid := writeConfig(t, "node:\n  mode: enforce\n")
	invalid := writeConfig(t, "node:\n  mode: block\ndecision:\n  quorum: 0\n")
	unknownKey := writeConfig(t, "lgo:\n  level: debug\n")
	missing := filepath.Join(t.TempDir(), "missing.yaml")

	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr []string
	}{
		{name: "valid", args: []string{"--config", valid, "--check-config"}, wantCode: ExitOK,
			wantStdout: "obied: configuration " + valid + " is valid\n"},
		{name: "invalid values", args: []string{"--check-config", "--config=" + invalid}, wantCode: ExitInvalidConfig,
			wantStderr: []string{invalid, "node.mode (line 2): must be one of", "decision.quorum (line 4): must be at least 1"}},
		{name: "unknown key", args: []string{"--config", unknownKey, "--check-config"}, wantCode: ExitInvalidConfig,
			wantStderr: []string{"lgo (line 1): unknown key"}},
		{name: "missing file", args: []string{"--config", missing, "--check-config"}, wantCode: ExitInvalidConfig,
			wantStderr: []string{"read config", "no such file"}},
		{name: "help lists flags", args: []string{"--help"}, wantCode: ExitOK,
			wantStderr: []string{"-check-config", "-config file", "/etc/obie/obie.yaml", "-version"}},
		{name: "version needs no config", args: []string{"--version", "--config", missing}, wantCode: ExitOK,
			wantStdout: "obied dev\n"},
		{name: "positional argument", args: []string{"start"}, wantCode: ExitUsage,
			wantStderr: []string{`unexpected argument "start"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := RunDaemon(tt.args, &stdout, &stderr)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d (stderr %q)", code, tt.wantCode, stderr.String())
			}
			if stdout.String() != tt.wantStdout {
				t.Errorf("stdout = %q, want %q", stdout.String(), tt.wantStdout)
			}
			for _, want := range tt.wantStderr {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr = %q, want it to contain %q", stderr.String(), want)
				}
			}
		})
	}
}

func TestRunDaemonCheckConfigWriteError(t *testing.T) {
	path := writeConfig(t, "")
	var stderr bytes.Buffer
	if code := RunDaemon([]string{"--config", path, "--check-config"}, failingWriter{}, &stderr); code != ExitIOError {
		t.Errorf("exit code = %d, want %d", code, ExitIOError)
	}
}

func TestRunDaemonLogsJSONWithComponent(t *testing.T) {
	path := writeConfig(t, "log:\n  level: warn\n")
	var stdout, stderr bytes.Buffer
	code := RunDaemon([]string{"--config", path}, &stdout, &stderr)
	if code != ExitNotImplemented {
		t.Errorf("exit code = %d, want %d", code, ExitNotImplemented)
	}

	var lines []map[string]any
	sc := bufio.NewScanner(&stderr)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("stderr line %q is not JSON: %v", sc.Text(), err)
		}
		lines = append(lines, m)
	}
	// log.level warn suppresses the info line.
	if len(lines) != 1 {
		t.Fatalf("got %d log lines, want 1: %v", len(lines), lines)
	}
	if lines[0]["component"] != "obied" || lines[0]["level"] != "ERROR" ||
		!strings.Contains(lines[0]["msg"].(string), "not implemented yet") {
		t.Errorf("unexpected log line: %v", lines[0])
	}
}
