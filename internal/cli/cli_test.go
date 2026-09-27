package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{name: "version", args: []string{"--version"}, wantCode: ExitOK, wantStdout: "obied dev\n"},
		{name: "version single dash", args: []string{"-version"}, wantCode: ExitOK, wantStdout: "obied dev\n"},
		{name: "help", args: []string{"--help"}, wantCode: ExitOK, wantStderr: "-version"},
		{name: "unknown flag", args: []string{"--bogus"}, wantCode: ExitUsage, wantStderr: "flag provided but not defined"},
		{name: "positional argument", args: []string{"start"}, wantCode: ExitUsage, wantStderr: `unexpected argument "start"`},
		{name: "no arguments", args: nil, wantCode: ExitNotImplemented, wantStderr: "not implemented yet"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run("obied", tt.args, &stdout, &stderr)
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
	if code := Run("obiectl", []string{"--version"}, failingWriter{}, &stderr); code != ExitIOError {
		t.Errorf("exit code = %d, want %d", code, ExitIOError)
	}
	if !strings.Contains(stderr.String(), "broken pipe") {
		t.Errorf("stderr = %q, want it to mention the write error", stderr.String())
	}
}
