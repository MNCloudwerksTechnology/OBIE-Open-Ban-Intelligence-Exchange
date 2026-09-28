package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/identity"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

var (
	explainAt    = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	explainUntil = explainAt.Add(time.Hour)
	explained    = admin.DecisionResponse{
		Indicator: obieproto.Indicator{Kind: obieproto.KindIPv4, Value: "203.0.113.7", Scope: "/32"},
		State:     admin.StateBlock, Score: 1.7999999999999998, Threshold: 1.8, Contributors: 2, Quorum: 2,
		ExpiresAt: &explainUntil, Reason: "consensus: score 1.8 >= threshold 1.8, 2 >= quorum 2", EvaluatedAt: explainAt,
		Publishers: []admin.ContributionResponse{
			{PeerID: "12D3KooWAAA", Name: "alpha", Action: "ban", Weight: 1, Confidence: 0.8, Score: 0.8, Contributes: true,
				IssuedAt: explainAt.Add(-time.Hour), ExpiresAt: explainUntil},
			{PeerID: "12D3KooWBBB", Action: "watch", Weight: 0.5, Confidence: 1, IssuedAt: explainAt.Add(-time.Hour), ExpiresAt: explainUntil},
			{PeerID: "12D3KooWSSS", Local: true, Action: "ban", Weight: 1, Confidence: 1, Score: 1, Contributes: true,
				IssuedAt: explainAt.Add(-time.Hour), ExpiresAt: explainUntil},
		},
		Sovereignty: &admin.SovereigntyResponse{Applied: true, Note: "no allow-list entry or override applies"},
	}
)

func TestWriteExplanation(t *testing.T) {
	var out bytes.Buffer
	if err := writeExplanation(&out, &explained); err != nil {
		t.Fatal(err)
	}
	want := `Indicator:             ipv4:203.0.113.7
Decision:              block until 2026-09-28T13:00:00Z
Reason:                consensus: score 1.8 >= threshold 1.8, 2 >= quorum 2
Score:                 1.8 (threshold 1.8)
Publishers:            2 (quorum 2)
Local autoblock:       no
Allow-list/overrides:  none apply
Evaluated:             2026-09-28T12:00:00Z

PUBLISHER    PEER ID      ACTION  WEIGHT  CONFIDENCE  SCORE  COUNTS  ISSUED                EXPIRES
alpha        12D3KooWAAA  ban     1       0.8         0.8    yes     2026-09-28T11:00:00Z  2026-09-28T13:00:00Z
-            12D3KooWBBB  watch   0.5     1           0      no      2026-09-28T11:00:00Z  2026-09-28T13:00:00Z
(this node)  12D3KooWSSS  ban     1       1           1      yes     2026-09-28T11:00:00Z  2026-09-28T13:00:00Z
`
	if out.String() != want {
		t.Errorf("explanation =\n%s\nwant\n%s", out.String(), want)
	}

	out.Reset()
	none := admin.DecisionResponse{Indicator: explained.Indicator, State: admin.StateNone, Threshold: 1.8, Quorum: 2,
		Reason: "no active verdicts", EvaluatedAt: explainAt}
	if err := writeExplanation(&out, &none); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Decision:         none\n") || !strings.HasSuffix(out.String(), "\nNo active verdicts.\n") {
		t.Errorf("explanation without verdicts =\n%s", out.String())
	}
}

func TestWriteDecisionsTable(t *testing.T) {
	var out bytes.Buffer
	watched := admin.DecisionResponse{Indicator: obieproto.Indicator{Kind: obieproto.KindCIDR, Value: "198.51.100.0/24", Scope: "/24"},
		State: admin.StateNone, Score: 0.5, Contributors: 1, Reason: "below consensus"}
	if err := writeDecisionsTable(&out, []admin.DecisionResponse{watched, explained}); err != nil {
		t.Fatal(err)
	}
	want := `INDICATOR             STATE  SCORE  PUBLISHERS  EXPIRES               REASON
cidr:198.51.100.0/24  none   0.5    1           -                     below consensus
ipv4:203.0.113.7      block  1.8    2           2026-09-28T13:00:00Z  consensus: score 1.8 >= threshold 1.8, 2 >= quorum 2
`
	if out.String() != want {
		t.Errorf("table =\n%s\nwant\n%s", out.String(), want)
	}
	out.Reset()
	if err := writeDecisionsTable(&out, nil); err != nil || out.String() != "No decisions.\n" {
		t.Errorf("no decisions: %q, %v", out.String(), err)
	}
}

func TestExplainUsage(t *testing.T) {
	for name, tc := range map[string]struct {
		args   []string
		code   int
		stderr string
	}{
		"no argument":    {[]string{"explain"}, ExitUsage, "want 1 argument(s), got 0"},
		"two arguments":  {[]string{"explain", "203.0.113.7", "203.0.113.8"}, ExitUsage, "want 1 argument(s), got 2"},
		"help":           {[]string{"explain", "--help"}, ExitOK, "Usage: obiectl explain [--json] <ip | cidr | indicator key>"},
		"unknown flag":   {[]string{"explain", "--yaml", "203.0.113.7"}, ExitUsage, "flag provided but not defined"},
		"not running":    {[]string{"explain", "203.0.113.7"}, ExitFailure, "obied is not running"},
		"decisions arg":  {[]string{"decisions", "block"}, ExitUsage, `unexpected argument "block"`},
		"decisions down": {[]string{"decisions"}, ExitFailure, "obied is not running"},
	} {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			args := append([]string{"--socket", "/nonexistent/obie.sock"}, tc.args...)
			if code := RunCtl(args, &stdout, &stderr); code != tc.code || !strings.Contains(stderr.String(), tc.stderr) {
				t.Errorf("exit code = %d, stderr %q; want %d containing %q", code, stderr.String(), tc.code, tc.stderr)
			}
		})
	}
}

// putVerdicts stores verdicts in the database of a node that is not
// running, as the mesh and local detection would.
func putVerdicts(t *testing.T, stateDir string, events ...*obieproto.Event) {
	t.Helper()
	db := store.New(filepath.Join(stateDir, "db"), slog.New(slog.NewTextHandler(io.Discard, nil)), store.Options{})
	if err := db.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Stop(context.Background()) }()
	for _, ev := range events {
		if ok, err := db.Put(ev); err != nil || !ok {
			t.Fatalf("Put: %v, %v", ok, err)
		}
	}
}

func banVerdict(id, publisher, value string, confidence float64) *obieproto.Event {
	return &obieproto.Event{
		ID:        id,
		Spec:      obieproto.Spec,
		Type:      obieproto.TypeVerdict,
		IssuedAt:  obieproto.NewTimestamp(time.Now().Add(-time.Minute)),
		Indicator: obieproto.Indicator{Kind: obieproto.KindIPv4, Value: value, Scope: "/32"},
		Protocol:  "ssh",
		Evidence:  &obieproto.Evidence{Events: 5, Reason: "password_bruteforce"},
		Verdict:   &obieproto.Verdict{SuggestedAction: obieproto.ActionBan, Confidence: confidence, TTLSeconds: 3600},
		Publisher: obieproto.Publisher{PeerID: publisher},
	}
}

// TestObiectlExplainAgainstInProcessDaemon stores a local verdict and a
// verdict of a single trusted peer, and a local verdict on an address of
// the built-in allow-list, starts obied and explains them. The benchmarking
// range 198.18.0.0/15 stands in for public addresses: it is not on the
// built-in allow-list.
func TestObiectlExplainAgainstInProcessDaemon(t *testing.T) {
	const peer = "12D3KooWGzBX6MWMMz3kHmFfyT3vJxFoy4xQF8NbXN7xBAFhGyvd"
	n := newTestNodeWith(t, "", "mesh:\n  listen: [/ip4/127.0.0.1/tcp/0]\n"+
		"trust:\n  publishers:\n    - {peer_id: "+peer+", name: seed, weight: 1}\n")
	key, err := identity.Create(n.stateDir, false)
	if err != nil {
		t.Fatal(err)
	}
	putVerdicts(t, n.stateDir,
		banVerdict("01900000-0000-7000-8000-000000000001", key.PeerID(), "198.18.0.7", 0.8),
		banVerdict("01900000-0000-7000-8000-000000000002", peer, "198.18.0.8", 1),
		banVerdict("01900000-0000-7000-8000-000000000003", key.PeerID(), "203.0.113.9", 1))

	var stderr bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exit := startDaemon(ctx, t, n, &stderr, func(ctx context.Context, args []string) int {
		return runDaemon(ctx, args, &bytes.Buffer{}, &stderr)
	})
	ctl := func(args ...string) string {
		t.Helper()
		var stdout, ctlStderr bytes.Buffer
		if code := RunCtl(append([]string{"--socket", n.socket}, args...), &stdout, &ctlStderr); code != ExitOK {
			t.Fatalf("obiectl %v: exit code = %d, stderr %q", args, code, ctlStderr.String())
		}
		return stdout.String()
	}

	var local admin.DecisionResponse
	if err := json.Unmarshal([]byte(ctl("explain", "--json", "198.18.0.7")), &local); err != nil {
		t.Fatal(err)
	}
	if local.State != admin.StateBlock || !local.LocalAutoblock || len(local.Publishers) != 1 || !local.Publishers[0].Local ||
		local.Publishers[0].PeerID != key.PeerID() || local.ExpiresAt == nil || local.Sovereignty == nil || local.Sovereignty.Effect != "" {
		t.Errorf("local verdict explanation = %+v", local)
	}
	allowed := ctl("explain", "203.0.113.9")
	for _, want := range []string{"Decision:              allowed\n",
		"Allow-list/overrides:  allow-listed: built-in range 203.0.113.0/24 (documentation (TEST-NET-3))\n"} {
		if !strings.Contains(allowed, want) {
			t.Errorf("allow-listed explanation lacks %q:\n%s", want, allowed)
		}
	}

	single := ctl("explain", "198.18.0.8")
	for _, want := range []string{"Decision:              none\n", "Publishers:            1 (quorum 2)\n",
		"seed       " + peer + "  ban     1       1           1      yes"} {
		if !strings.Contains(single, want) {
			t.Errorf("single peer explanation lacks %q:\n%s", want, single)
		}
	}
	if out := ctl("explain", "198.18.0.1"); !strings.Contains(out, "No active verdicts.") || !strings.Contains(out, "Allow-list/overrides:  none apply\n") {
		t.Errorf("unknown indicator explanation:\n%s", out)
	}

	var blocked admin.DecisionsResponse
	if err := json.Unmarshal([]byte(ctl("decisions", "--state", "block", "--json")), &blocked); err != nil {
		t.Fatal(err)
	}
	if len(blocked.Decisions) != 1 || blocked.Decisions[0].Indicator.Value != "198.18.0.7" {
		t.Errorf("blocked decisions = %+v", blocked.Decisions)
	}
	if out := ctl("decisions", "--state", "allowed"); !strings.Contains(out, "ipv4:203.0.113.9") || strings.Contains(out, "198.18.0") {
		t.Errorf("allowed decisions:\n%s", out)
	}
	if out := ctl("decisions"); !strings.Contains(out, "ipv4:198.18.0.8") || !strings.Contains(out, "ipv4:198.18.0.7") {
		t.Errorf("decisions table:\n%s", out)
	}
	if out := ctl("status"); !strings.Contains(out, "decision   running  yes    -      1 blocked of 3 indicators") {
		t.Errorf("status lacks the decision subsystem:\n%s", out)
	}

	var stdout, ctlStderr bytes.Buffer
	if code := RunCtl([]string{"--socket", n.socket, "explain", "not-an-ip"}, &stdout, &ctlStderr); code != ExitFailure ||
		!strings.Contains(ctlStderr.String(), "400 Bad Request") {
		t.Errorf("explain of an invalid indicator: exit %d, stderr %q", code, ctlStderr.String())
	}

	cancel()
	if code := waitExit(t, exit, &stderr); code != ExitOK {
		t.Fatalf("obied exit code = %d:\n%s", code, stderr.String())
	}
}
