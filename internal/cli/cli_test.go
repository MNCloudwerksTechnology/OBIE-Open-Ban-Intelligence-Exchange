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
		{name: "help", args: []string{"--help"}, wantCode: ExitOK, wantStdout: "\n  status      show whether the node runs"},
		{name: "help lists socket", args: []string{"--help"}, wantCode: ExitOK, wantStdout: "(default: /run/obie/obie.sock)"},
		{name: "help command", args: []string{"help"}, wantCode: ExitOK, wantStdout: "Start here: sudo obiectl status"},
		{name: "help on a command", args: []string{"help", "status"}, wantCode: ExitOK, wantStdout: "obiectl status: show whether"},
		{name: "help on two commands", args: []string{"help", "status", "peers"}, wantCode: ExitUsage, wantStderr: "expects one command"},
		{name: "help on unknown command", args: []string{"help", "stats"}, wantCode: ExitUsage, wantStderr: `unknown command "stats"; did you mean "status"?`},
		{name: "unknown flag", args: []string{"--bogus"}, wantCode: ExitUsage, wantStderr: "obiectl: unknown flag --bogus\n  Next: see how to use it: obiectl --help\n"},
		{name: "no command", args: nil, wantCode: ExitUsage, wantStderr: "Look: what the node knows and does"},
		{name: "unknown command", args: []string{"start"}, wantCode: ExitUsage, wantStderr: `unknown command "start"`},
		{name: "misspelled command", args: []string{"stauts"}, wantCode: ExitUsage, wantStderr: `unknown command "stauts"; did you mean "status"?`},
		{name: "status argument", args: []string{"status", "now"}, wantCode: ExitUsage, wantStderr: `unexpected argument "now"`},
		{name: "status unknown flag", args: []string{"status", "--yaml"}, wantCode: ExitUsage, wantStderr: "unknown flag --yaml"},
		{name: "status misspelled flag", args: []string{"status", "--jsn"}, wantCode: ExitUsage, wantStderr: "unknown flag --jsn; did you mean --json?"},
		{name: "status help", args: []string{"status", "--help"}, wantCode: ExitOK, wantStdout: "  --json\n"},
		{name: "decisions missing value", args: []string{"decisions", "--state"}, wantCode: ExitUsage, wantStderr: "--state needs a value"},
		{name: "bad timeout", args: []string{"--timeout", "soon", "status"}, wantCode: ExitUsage, wantStderr: `invalid value "soon" for --timeout`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := RunCtl(tt.args, &stdout, &stderr)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d", code, tt.wantCode)
			}
			if !strings.Contains(stdout.String(), tt.wantStdout) || tt.wantStdout == "" && stdout.Len() > 0 {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tt.wantStdout)
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) || tt.wantStderr == "" && stderr.Len() > 0 {
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
