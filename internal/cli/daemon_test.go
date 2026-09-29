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
			wantStderr: []string{"obied: the configuration " + invalid + " is invalid:\n", "\n  " + invalid + ":2: node.mode: must be one of",
				"\n  " + invalid + ":4: decision.quorum: must be at least 1", "Next: fix these settings, then check the file again: sudo obied --check-config --config " + invalid}},
		{name: "unknown key", args: []string{"--config", unknownKey, "--check-config"}, wantCode: ExitInvalidConfig,
			wantStderr: []string{unknownKey + ":1: lgo: unknown key"}},
		{name: "missing file", args: []string{"--config", missing, "--check-config"}, wantCode: ExitInvalidConfig,
			wantStderr: []string{"obied: the configuration file " + missing + " does not exist\n", "Next: write it after a few questions: sudo obied setup --config " + missing}},
		{name: "invalid allow-list file", args: []string{"--config", badAllowlist, "--check-config"}, wantCode: ExitInvalidConfig,
			wantStderr: []string{badAllowFile + ":2: invalid address \"not-a-cidr\"", "Next: fix or create the file, or remove it from allowlist.files in " + badAllowlist}},
		{name: "missing allow-list file", args: []string{"--config", missingAllowlist, "--check-config"}, wantCode: ExitInvalidConfig,
			wantStderr: []string{"allow-list file", "no such file"}},
		{name: "run checks", args: []string{"run", "--config", valid, "--check-config"}, wantCode: ExitOK,
			wantStdout: "obied run: configuration " + valid + " is valid\n"},
		{name: "version needs no config", args: []string{"--version", "--config", missing}, wantCode: ExitOK,
			wantStdout: "obied dev\n"},
		{name: "run version is the tool's", args: []string{"run", "--version"}, wantCode: ExitOK,
			wantStdout: "obied dev\n"},
		{name: "unknown command", args: []string{"start"}, wantCode: ExitUsage,
			wantStderr: []string{`unknown command "start"`, "Manage: set up, run and look after the node"}},
		{name: "run with argument", args: []string{"run", "now"}, wantCode: ExitUsage,
			wantStderr: []string{`obied run: unexpected argument "now"`}},
		{name: "flags with argument", args: []string{"--config", valid, "now"}, wantCode: ExitUsage,
			wantStderr: []string{`obied: unexpected argument "now"`}},
		{name: "no arguments", args: nil, wantCode: ExitUsage,
			wantStderr: []string{"Start here: sudo obied setup", "  run  ", "  self-check  "}},
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
