package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRunCtlUsage(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{name: "version", args: []string{"--version"}, wantCode: ExitOK, wantStdout: "obiectl dev\n"},
		{name: "version single dash", args: []string{"-version"}, wantCode: ExitOK, wantStdout: "obiectl dev\n"},
		{name: "help", args: []string{"--help"}, wantCode: ExitOK, wantStderr: "status      show the node status"},
		{name: "help lists socket", args: []string{"--help"}, wantCode: ExitOK, wantStderr: "/run/obie/obie.sock"},
		{name: "unknown flag", args: []string{"--bogus"}, wantCode: ExitUsage, wantStderr: "flag provided but not defined"},
		{name: "no command", args: nil, wantCode: ExitUsage, wantStderr: "missing command"},
		{name: "unknown command", args: []string{"start"}, wantCode: ExitUsage, wantStderr: `unknown command "start"`},
		{name: "status argument", args: []string{"status", "now"}, wantCode: ExitUsage, wantStderr: `unexpected argument "now"`},
		{name: "status unknown flag", args: []string{"status", "--yaml"}, wantCode: ExitUsage, wantStderr: "flag provided but not defined"},
		{name: "status help", args: []string{"status", "--help"}, wantCode: ExitOK, wantStderr: "-json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := RunCtl(tt.args, &stdout, &stderr)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d", code, tt.wantCode)
			}
			if stdout.String() != tt.wantStdout {
				t.Errorf("stdout = %q, want %q", stdout.String(), tt.wantStdout)
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestRunVersionWriteError(t *testing.T) {
	var stderr bytes.Buffer
	if code := RunCtl([]string{"--version"}, failingWriter{}, &stderr); code != ExitIOError {
		t.Errorf("exit code = %d, want %d", code, ExitIOError)
	}
	if !strings.Contains(stderr.String(), "broken pipe") {
		t.Errorf("stderr = %q, want it to mention the write error", stderr.String())
	}
}
