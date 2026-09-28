package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// waitFile waits until the file at path contains every string in want.
func waitFile(t *testing.T, path string, want ...string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		data, _ := os.ReadFile(path) // #nosec G304 -- test file.
		missing := false
		for _, w := range want {
			missing = missing || !strings.Contains(string(data), w)
		}
		if !missing {
			return string(data)
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s lacks %q:\n%s", path, want, data)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestObservabilityAgainstInProcessDaemon: a local report and a
// force-block are written to the audit log, which a reload (SIGHUP)
// reopens after logrotate moved it, and /metrics serves every obie_
// metric.
func TestObservabilityAgainstInProcessDaemon(t *testing.T) {
	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
	n := newTestNodeWith(t, "", "mesh:\n  listen: [/ip4/127.0.0.1/tcp/0]\naudit:\n  path: "+auditPath+"\n")
	var logs syncBuffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reload := make(chan struct{})
	exit := startDaemon(ctx, t, n, &logs.buf, func(ctx context.Context, args []string) int {
		return runDaemonWith(ctx, reload, args, &bytes.Buffer{}, &logs)
	})
	ctl := ctlFunc(t, n)

	ctl("report", "85.10.0.7", "--protocol", "ssh", "--reason", "password_bruteforce")
	waitFile(t, auditPath, `"action":"local-report"`, `"action":"block-added"`, `"source":{"ip":"85.10.0.7"}`,
		`"rule":{"name":"local_autoblock"}`, `"mode":"observe"`)

	// logrotate moves the file, then sends SIGHUP.
	if err := os.Rename(auditPath, auditPath+".1"); err != nil {
		t.Fatal(err)
	}
	reload <- struct{}{}
	waitFile(t, auditPath) // reopened
	ctl("block", "85.10.0.8", "--ttl", "1h", "--note", "scanner")
	rotated := waitFile(t, auditPath, `"action":"override-set"`, `"rule":{"name":"force_block"}`, `"note":"scanner"`)
	if strings.Contains(rotated, "85.10.0.7") {
		t.Errorf("the reopened audit log repeats earlier records:\n%s", rotated)
	}

	resp, err := http.Get("http://" + n.metrics + "/metrics") // #nosec G107 -- test server URL.
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"build_info", "node_mode", "peers_connected", "peers_configured",
		"events_received_total", "events_published_total", "store_active_indicators", "store_active_verdicts",
		"decisions", "enforcer_entries", "enforcer_apply_total", "enforcer_apply_duration_seconds",
		"enforcer_skipped_total", "propagation_delay_seconds", "admin_requests_total"} {
		if !strings.Contains(string(body), "# TYPE obie_"+name+" ") {
			t.Errorf("/metrics lacks obie_%s", name)
		}
	}
	checkDashboard(t, string(body))
	for _, sample := range []string{
		`obie_node_mode{mode="observe"} 1`,
		`obie_decisions{state="block"} 2`,
		`obie_store_active_verdicts 1`,
	} {
		if !strings.Contains(string(body), sample+"\n") {
			t.Errorf("/metrics lacks %s", sample)
		}
	}
	// Counters are process-wide: other tests may have counted before.
	for _, series := range []string{
		`obie_events_published_total{type="verdict"} `,
		`obie_admin_requests_total{code="201",endpoint="POST /v1/reports"} `,
	} {
		if !strings.Contains(string(body), series) || strings.Contains(string(body), series+"0\n") {
			t.Errorf("/metrics lacks a count of %s", series)
		}
	}

	cancel()
	if code := waitExit(t, exit, &logs.buf); code != ExitOK {
		t.Fatalf("obied exit code = %d:\n%s", code, logs.String())
	}
	if findLog(logLines(t, &logs.buf), "audit", "audit log reopened") == nil {
		t.Errorf("no reopen logged:\n%s", logs.String())
	}
}

// dashboard is the Grafana dashboard shipped for operators.
const dashboard = "../../documentation/operations/grafana-dashboard.json"

// checkDashboard checks that every query of the dashboard uses metrics
// that obied serves in metrics, the body of a /metrics scrape.
func checkDashboard(t *testing.T, metrics string) {
	t.Helper()
	data, err := os.ReadFile(dashboard)
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Panels []struct {
			Title   string
			Targets []struct{ Expr string }
		}
	}
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatalf("%s: %v", dashboard, err)
	}
	name := regexp.MustCompile(`obie_[a-z_]+`)
	suffix := regexp.MustCompile(`_(bucket|sum|count)$`)
	queries := 0
	for _, p := range d.Panels {
		for _, target := range p.Targets {
			queries++
			for _, m := range name.FindAllString(target.Expr, -1) {
				if base := suffix.ReplaceAllString(m, ""); !strings.Contains(metrics, "# TYPE "+base+" ") {
					t.Errorf("dashboard panel %q queries %s, which obied does not serve", p.Title, m)
				}
			}
		}
	}
	if queries == 0 {
		t.Errorf("%s has no queries", dashboard)
	}
}
