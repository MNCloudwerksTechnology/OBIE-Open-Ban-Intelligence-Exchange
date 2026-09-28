package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/identity"
)

// activityKinds maps audit actions to the timeline's kinds, as the page
// marks its rows.
var activityKinds = map[string]string{
	"block-added": "blocks", "block-updated": "blocks", "block-removed": "blocks", "allowed-by-allowlist": "allowlist",
	"override-set": "overrides", "override-removed": "overrides", "local-report": "reports", "revocation": "revocations",
	"peer-connected": "peers", "peer-disconnected": "peers", "config-reloaded": "reloads", "mode-changed": "mode",
}

// timelineRow matches a row of the timeline: its kind and time.
var timelineRow = regexp.MustCompile(`<tr data-kind="([a-z]+)">\s*<th scope="row"><time datetime="([^"]+)">`)

// auditStory returns "kind time" of every record of the audit log at
// path, newest first, the time to the second, as the timeline shows them.
func auditStory(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- the test's own audit log.
	if err != nil {
		t.Fatal(err)
	}
	var story []string
	for line := range strings.Lines(string(data)) {
		var rec struct {
			Timestamp string `json:"@timestamp"`
			Event     struct{ Action string }
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("audit line %q: %v", line, err)
		}
		at, err := time.Parse(time.RFC3339Nano, rec.Timestamp)
		if err != nil {
			t.Fatal(err)
		}
		story = append(story, activityKinds[rec.Event.Action]+" "+at.Truncate(time.Second).Format(time.RFC3339))
	}
	slices.Reverse(story)
	return story
}

// timelineStory returns "kind time" of every row of a timeline page.
func timelineStory(page string) []string {
	var story []string
	for _, m := range timelineRow.FindAllStringSubmatch(page, -1) {
		story = append(story, m[1]+" "+m[2])
	}
	return story
}

// TestConsoleActivityEndToEnd runs node B with the console and the audit
// log on, bootstrapping to node A, and follows its activity timeline:
// every kind of activity, live updates, filters and links, the same story
// as the audit log, the history after a restart, and live updates without
// the audit log (WP-1688).
func TestConsoleActivityEndToEnd(t *testing.T) {
	meshA := freeAddr(t)
	host, port, err := net.SplitHostPort(meshA)
	if err != nil {
		t.Fatal(err)
	}
	a := newTestNodeWith(t, "", fmt.Sprintf("mesh:\n  listen: [/ip4/%s/tcp/%s]\n", host, port))
	keyA, err := identity.Create(a.stateDir, false)
	if err != nil {
		t.Fatal(err)
	}
	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
	addr := freeAddr(t)
	sections := fmt.Sprintf("mesh:\n  listen: [/ip4/127.0.0.1/tcp/0]\n  bootstrap: [/ip4/%s/tcp/%s/p2p/%s]\n"+
		"trust:\n  publishers:\n    - {peer_id: %s, name: alpha, weight: 0.6}\n"+
		"console:\n  enabled: true\n  listen: %s\n", host, port, keyA.PeerID(), keyA.PeerID(), addr)
	b := newTestNodeWith(t, "  mode: observe\n", sections+"audit:\n  path: "+auditPath+"\n")

	var stderrA, logsB syncBuffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exitA := startDaemon(ctx, t, a, &stderrA.buf, func(ctx context.Context, args []string) int {
		return runDaemon(ctx, args, &bytes.Buffer{}, &stderrA)
	})
	ctxB, stopB := context.WithCancel(ctx)
	reload := make(chan struct{})
	exitB := startDaemon(ctxB, t, b, &logsB.buf, func(ctx context.Context, args []string) int {
		return runDaemonWith(ctx, reload, args, &bytes.Buffer{}, &logsB)
	})
	ctl := func(args ...string) {
		t.Helper()
		var stderr bytes.Buffer
		if code := RunCtl(append([]string{"--socket", b.socket}, args...), io.Discard, &stderr); code != ExitOK {
			t.Fatalf("obiectl %s = %d: %s", strings.Join(args, " "), code, stderr.String())
		}
	}
	var browser *consoleBrowser
	signIn := func() {
		t.Helper()
		browser = newConsoleBrowser(t, addr)
		eventually(t, "B's console serves", func() bool { return listening(addr) })
		if code, _ := browser.do(http.MethodPost, "/login", url.Values{"token": {consoleOf(t, b).Token}, "next": {"/"}}); code != http.StatusSeeOther {
			t.Fatalf("sign-in = %d", code)
		}
	}
	get := func(path string) string {
		t.Helper()
		code, body := browser.do(http.MethodGet, path, nil)
		if code != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", path, code, body)
		}
		return body
	}
	wantIn := func(what, page string, want ...string) {
		t.Helper()
		for _, w := range want {
			if !strings.Contains(page, w) {
				t.Errorf("%s lacks %q:\n%s", what, w, page)
			}
		}
	}
	signIn()

	// AC1: peers, reports, overrides, revocations and decisions.
	eventually(t, "B records A connecting", func() bool { return strings.Contains(get("/activity?kind=peers"), "Peer connected") })
	ctl("report", "--protocol", "ssh", "--reason", "password_bruteforce", "85.10.20.1")
	ctl("block", "--ttl", "1h", "--note", "scanner", "85.10.30.0/24")
	ctl("revoke", "--reason", "false_positive", "85.10.20.1")
	// The engine records the decisions after the commands return.
	var page string
	eventually(t, "the revocation's decision", func() bool {
		page = get("/activity")
		return strings.Contains(page, "Block removed")
	})
	wantIn("the timeline", page,
		`<p class="callout">From the audit log file `+auditPath+`: `,
		`<span class="activity-kind" data-kind="revocations">Verdict revoked</span>`,
		`<span class="activity-kind" data-kind="blocks">Block removed</span>`,
		`<span class="activity-kind" data-kind="overrides">Always block set</span>`,
		`<a class="mono" href="/decisions/85.10.30.0/24">85.10.30.0/24</a>`,
		`Note: scanner`,
		`<span class="activity-kind" data-kind="reports">Reported by this node</span>`,
		`<a class="activity-link" href="/verdicts?address=85.10.20.1&amp;from=mine">This node&#39;s verdicts on 85.10.20.1</a>`,
		`<span class="activity-kind" data-kind="blocks">Block added</span>`,
		`<a href="/peers/`+keyA.PeerID()+`" title="`+keyA.PeerID()+`">alpha</a>`)
	// AC5: the same story as the audit log, once nothing is being written.
	var got, want []string
	for range 50 {
		if got, want = timelineStory(get("/activity")), auditStory(t, auditPath); slices.Equal(got, want) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !slices.Equal(got, want) {
		t.Errorf("timeline %q\ndiffers from the audit log %q", got, want)
	}
	// AC3: filters by kind and address.
	if got := timelineStory(get("/activity?kind=overrides&address=85.10.30.7")); len(got) != 1 || !strings.HasPrefix(got[0], "overrides ") {
		t.Errorf("filtered timeline = %q", got)
	}
	// AC4: the overview shows the last entries.
	wantIn("the overview", get("/"), `<h2 id="activity-heading">Recent activity</h2>`,
		`<span class="activity-kind" data-kind="revocations">Verdict revoked</span>`)

	// AC2: a reload that switches the mode arrives through the live feed.
	live := regexp.MustCompile(`data-live-feed="/api/activity\?after=([0-9]+)"`).FindStringSubmatch(page)
	if live == nil {
		t.Fatalf("the timeline has no live feed:\n%s", page)
	}
	original, err := os.ReadFile(b.config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b.config, bytes.Replace(original, []byte("  mode: observe\n"), []byte("  mode: enforce\n"), 1), 0o600); err != nil { // #nosec G703 -- the test's own configuration file.
		t.Fatal(err)
	}
	reload <- struct{}{}
	var fragment string
	eventually(t, "the reload in the live feed", func() bool {
		fragment = get("/api/activity?after=" + live[1])
		return strings.Contains(fragment, "Configuration reloaded")
	})
	wantIn("the live feed", fragment,
		`<span class="activity-kind" data-kind="mode">Mode changed to Enforce</span>`,
		`<a href="/configuration#section-node">node.mode</a>`,
		`: node.mode changed`,
		`<a href="/configuration">Configuration</a>`)
	if strings.Contains(fragment, "Verdict revoked") {
		t.Errorf("the live feed repeats entries before its start:\n%s", fragment)
	}

	// Edge case 2: after a restart, the activity from before it is still
	// there.
	before := auditStory(t, auditPath)
	stopB()
	if code := waitExit(t, exitB, &logsB.buf); code != ExitOK {
		t.Fatalf("exit code = %d:\n%s", code, logsB.String())
	}
	ctxB, stopB = context.WithCancel(ctx)
	defer stopB()
	exitB = startDaemon(ctxB, t, b, &logsB.buf, func(ctx context.Context, args []string) int {
		return runDaemon(ctx, args, &bytes.Buffer{}, &logsB)
	})
	signIn()
	page = get("/activity")
	if got := timelineStory(page); len(got) < len(before) || !slices.Equal(got[len(got)-len(before):], before) {
		t.Errorf("after the restart the timeline %q\nlacks the activity before it %q", got, before)
	}

	// Edge case 3: without the audit log, the page says which history is
	// missing, and live updates work.
	stopB()
	if code := waitExit(t, exitB, &logsB.buf); code != ExitOK {
		t.Fatalf("exit code = %d:\n%s", code, logsB.String())
	}
	b = newTestNodeWith(t, "", sections)
	ctxB, stopB = context.WithCancel(ctx)
	defer stopB()
	exitB = startDaemon(ctxB, t, b, &logsB.buf, func(ctx context.Context, args []string) int {
		return runDaemon(ctx, args, &bytes.Buffer{}, &logsB)
	})
	signIn()
	page = get("/activity")
	wantIn("the timeline without audit log", page,
		`<p class="callout" data-level="warning">The audit log is off: audit.path is not set. So this timeline cannot show what happened before obied started at `)
	live = regexp.MustCompile(`data-live-feed="/api/activity\?after=([0-9]+)"`).FindStringSubmatch(page)
	if live == nil {
		t.Fatalf("the timeline without audit log has no live feed:\n%s", page)
	}
	ctl("block", "85.10.40.1")
	if fragment := get("/api/activity?after=" + live[1]); !strings.Contains(fragment, `<a class="mono" href="/decisions/85.10.40.1">85.10.40.1</a>`) {
		t.Errorf("no live update without the audit log:\n%s", fragment)
	}
	cancel()
	if code := waitExit(t, exitB, &logsB.buf); code != ExitOK {
		t.Fatalf("exit code = %d:\n%s", code, logsB.String())
	}
	if code := waitExit(t, exitA, &stderrA.buf); code != ExitOK {
		t.Fatalf("exit code of A = %d:\n%s", code, stderrA.String())
	}
}
