package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/internal/version"
)

func TestStatusDaemonNotRunning(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "obie.sock")
	var stdout, stderr bytes.Buffer
	if code := RunCtl([]string{"--socket", socket, "status"}, &stdout, &stderr); code != ExitFailure {
		t.Errorf("exit code = %d, want %d", code, ExitFailure)
	}
	want := "obiectl: obied is not running: nothing is listening on admin socket " + socket + "\n" +
		"obiectl: start obied, or point --socket at its admin.socket\n"
	if stderr.String() != want || stdout.Len() != 0 {
		t.Errorf("stderr = %q, want %q (stdout %q)", stderr.String(), want, stdout.String())
	}
}

func TestWriteStatusTable(t *testing.T) {
	status := &admin.StatusResponse{
		Version: "v0.1.0", Mode: "enforce", UptimeSeconds: 3723.6, Ready: false,
		Subsystems: map[string]admin.SubsystemStatus{
			"ops":   {State: lifecycle.StateRunning, Ready: true},
			"admin": {State: lifecycle.StateFailed, Error: "boom"},
			"mesh":  {State: lifecycle.StateRunning, Ready: true, Detail: "degraded: 0 peers connected"},
		},
	}
	var out bytes.Buffer
	if err := writeStatusTable(&out, status); err != nil {
		t.Fatal(err)
	}
	want := `Mode:     ENFORCE (blocks are sent to the enforcer)
Version:  v0.1.0
Uptime:   1h2m3s
Ready:    no

SUBSYSTEM  STATE    READY  ERROR  DETAIL
admin      failed   no     boom   -
mesh       running  yes    -      degraded: 0 peers connected
ops        running  yes    -      -
`
	if out.String() != want {
		t.Errorf("table =\n%s\nwant\n%s", out.String(), want)
	}
	for mode, want := range map[string]string{
		"observe": "Mode:     OBSERVE (decisions are logged, nothing is blocked)\n",
		"shadow":  "Mode:     SHADOW\n",
	} {
		out.Reset()
		status.Mode = mode
		if err := writeStatusTable(&out, status); err != nil || !strings.HasPrefix(out.String(), want) {
			t.Errorf("mode %s: %q, %v", mode, out.String(), err)
		}
	}
}

func TestStatusWriteError(t *testing.T) {
	n := newTestNode(t, "")
	var stderr bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exit := startDaemon(ctx, t, n, &stderr, func(ctx context.Context, args []string) int {
		return runDaemon(ctx, args, &bytes.Buffer{}, &stderr)
	})
	defer func() { cancel(); waitExit(t, exit, &stderr) }()

	var ctlStderr bytes.Buffer
	if code := RunCtl([]string{"--socket", n.socket, "status"}, failingWriter{}, &ctlStderr); code != ExitIOError {
		t.Errorf("exit code = %d, want %d (stderr %q)", code, ExitIOError, ctlStderr.String())
	}
}

// TestObiectlStatusAgainstInProcessDaemon starts obied in-process on a
// temporary socket and queries it through the obiectl code path.
func TestObiectlStatusAgainstInProcessDaemon(t *testing.T) {
	n := newTestNode(t, "  mode: enforce\n")
	var stderr bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exit := startDaemon(ctx, t, n, &stderr, func(ctx context.Context, args []string) int {
		return runDaemon(ctx, args, &bytes.Buffer{}, &stderr)
	})

	t.Run("json", func(t *testing.T) {
		var stdout, ctlStderr bytes.Buffer
		if code := RunCtl([]string{"--socket", n.socket, "status", "--json"}, &stdout, &ctlStderr); code != ExitOK {
			t.Fatalf("exit code = %d, stderr %q", code, ctlStderr.String())
		}
		var got admin.StatusResponse
		if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
			t.Fatalf("output is not JSON: %v\n%s", err, stdout.String())
		}
		if got.Version != version.Version || got.Mode != "enforce" || !got.Ready {
			t.Errorf("status = %+v", got)
		}
		if got.UptimeSeconds < 0 || time.Since(got.StartedAt) > time.Minute {
			t.Errorf("implausible uptime %v / start %v", got.UptimeSeconds, got.StartedAt)
		}
		for _, name := range []string{store.Name, admin.Name, "ops", "mesh"} {
			if s := got.Subsystems[name]; s.State != lifecycle.StateRunning || !s.Ready {
				t.Errorf("subsystem %s = %+v, want running and ready", name, s)
			}
		}
	})
	t.Run("table", func(t *testing.T) {
		var stdout, ctlStderr bytes.Buffer
		if code := RunCtl([]string{"--socket=" + n.socket, "status"}, &stdout, &ctlStderr); code != ExitOK {
			t.Fatalf("exit code = %d, stderr %q", code, ctlStderr.String())
		}
		for _, want := range []string{"Version:  dev\n", "Mode:     ENFORCE (blocks are sent to the enforcer)\n", "Ready:    yes\n",
			"admin      running  yes    -      -\n", "ops        running  yes    -      -\n",
			"mesh       running  yes    -      degraded: 0 peers connected (0/0 bootstrap peers)\n",
			"store      running  yes    -      -\n"} {
			if !strings.Contains(stdout.String(), want) {
				t.Errorf("table lacks %q:\n%s", want, stdout.String())
			}
		}
	})

	cancel()
	if code := waitExit(t, exit, &stderr); code != ExitOK {
		t.Fatalf("obied exit code = %d:\n%s", code, stderr.String())
	}
	var stdout, ctlStderr bytes.Buffer
	if code := RunCtl([]string{"--socket", n.socket, "status"}, &stdout, &ctlStderr); code != ExitFailure ||
		!strings.Contains(ctlStderr.String(), "obied is not running") {
		t.Errorf("after shutdown: exit %d, stderr %q", code, ctlStderr.String())
	}
}
