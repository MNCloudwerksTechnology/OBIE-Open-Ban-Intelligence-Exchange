package cli

import (
	"bytes"
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
	badAllowFile := filepath.Join(t.TempDir(), "allow.txt")
	if err := os.WriteFile(badAllowFile, []byte("198.18.0.0/24\nnot-a-cidr\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	badAllowlist := writeConfig(t, "allowlist:\n  files: ["+badAllowFile+"]\n")
	missingAllowlist := writeConfig(t, "allowlist:\n  files: ["+missing+"]\n")

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
		{name: "invalid allow-list file", args: []string{"--config", badAllowlist, "--check-config"}, wantCode: ExitInvalidConfig,
			wantStderr: []string{badAllowFile + ":2: invalid address \"not-a-cidr\""}},
		{name: "missing allow-list file", args: []string{"--config", missingAllowlist, "--check-config"}, wantCode: ExitInvalidConfig,
			wantStderr: []string{"allow-list file", "no such file"}},
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
