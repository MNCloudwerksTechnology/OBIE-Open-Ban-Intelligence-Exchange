package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/decision"
	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

var update = flag.Bool("update", false, "rewrite the golden files")

var t0 = time.Date(2026, 9, 28, 12, 0, 0, 123e6, time.UTC)

func ipv4(value string) obieproto.Indicator {
	return obieproto.Indicator{Kind: obieproto.KindIPv4, Value: value, Scope: "/32"}
}

func cidr(value string) obieproto.Indicator {
	return obieproto.Indicator{Kind: obieproto.KindCIDR, Value: value, Scope: value[strings.Index(value, "/"):]}
}

// startLog starts a Log at a file in a temporary directory in mode.
func startLog(t *testing.T, mode string, logs *bytes.Buffer) (*Log, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	l := New(path, Options{Mode: func() string { return mode }, Now: func() time.Time { return t0 }},
		slog.New(slog.NewJSONHandler(logs, nil)))
	if err := l.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Stop(context.Background()) })
	return l, path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- test file.
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// goldenRecords has one record of every action.
func goldenRecords() []Record {
	consensus := decision.Decision{Indicator: ipv4("203.0.113.7"), State: decision.StateBlock, Score: 1.8, Threshold: 1.8,
		Contributors: 2, Quorum: 2, ExpiresAt: t0.Add(24 * time.Hour),
		Reason: "consensus: score 1.8 >= threshold 1.8, 2 >= quorum 2"}
	autoblock := consensus
	autoblock.Indicator, autoblock.Score, autoblock.Contributors, autoblock.Autoblock = cidr("2001:db8:4::/48"), 1, 1, true
	autoblock.Reason = "local autoblock: this node's own ban verdict (score 1 < threshold 1.8, 1 < quorum 2)"
	removed := decision.Decision{Indicator: ipv4("203.0.113.7"), State: decision.StateNone, Score: 0.9, Threshold: 1.8,
		Contributors: 1, Quorum: 2, Reason: "below consensus: score 0.9 < threshold 1.8, 1 < quorum 2"}
	allowed := consensus
	allowed.Indicator, allowed.State, allowed.ExpiresAt = ipv4("198.51.100.20"), decision.StateAllowed, time.Time{}
	allowed.Sovereignty = sovereignty.Ruling{Effect: sovereignty.EffectAllow, Rule: sovereignty.RuleAllowlist}
	allowed.Reason = "allow-listed: allowlist.cidrs entry 198.51.100.20/32; verdicts: " + consensus.Reason
	forced := decision.Decision{Indicator: ipv4("192.0.2.99"), State: decision.StateBlock, Threshold: 1.8, Quorum: 2,
		ExpiresAt:   t0.Add(time.Hour),
		Sovereignty: sovereignty.Ruling{Effect: sovereignty.EffectBlock, Rule: sovereignty.RuleForceBlock},
		Reason:      "operator force-block override on ipv4:192.0.2.99 until 2026-09-28T13:00:00Z; verdicts: no active verdicts"}

	override := store.Override{Indicator: ipv4("192.0.2.99"), Action: store.ForceBlock, Note: "scanner", CreatedAt: t0,
		ExpiresAt: t0.Add(time.Hour)}
	report := &obieproto.Event{ID: "0199a1b2-c3d4-7e5f-8a6b-000000000001", Type: obieproto.TypeVerdict,
		IssuedAt: obieproto.NewTimestamp(t0), Indicator: ipv4("203.0.113.7"), Protocol: "ssh",
		Evidence: &obieproto.Evidence{Events: 47, Reason: "password_bruteforce"},
		Verdict:  &obieproto.Verdict{SuggestedAction: obieproto.ActionBan, Confidence: 0.8, TTLSeconds: 86400}}
	revoke := &obieproto.Event{ID: "0199a1b2-c3d4-7e5f-8a6b-000000000002", Type: obieproto.TypeRevoke,
		IssuedAt: obieproto.NewTimestamp(t0), Indicator: ipv4("203.0.113.7"), Revokes: report.ID, Reason: "false_positive"}

	// Operator actions name the door and the local user (ADR 0026).
	set := OverrideSet(&override)
	set.Origin = Origin{Via: OriginConsole, UserID: "1000", UserName: "alice"}
	unset := OverrideRemoved(override.Indicator, override.Action)
	unset.Origin = Origin{Via: OriginAdminAPI, UserID: "0", UserName: "root"}
	reported := LocalReport(report)
	reported.Origin = Origin{Via: OriginAdminAPI, UserID: "994"}
	revoked := Revocation(revoke)
	revoked.Origin = Origin{Via: OriginConsole, UserID: "1000", UserName: "alice"}

	// Blocks name the verdicts that count in them; a removal those of the
	// block that ended (ADR 0032).
	alpha := decision.Contributor{PeerID: "12D3KooWPeerA", EventID: "0199a1b2-c3d4-7e5f-8a6b-00000000000a", Weight: 1,
		Confidence: 0.9}
	bravo := decision.Contributor{PeerID: "12D3KooWPeerB", EventID: "0199a1b2-c3d4-7e5f-8a6b-00000000000b", Weight: 0.9,
		Confidence: 1}
	self := decision.Contributor{PeerID: "12D3KooWSelf", EventID: "0199a1b2-c3d4-7e5f-8a6b-00000000000c", Weight: 1,
		Confidence: 1}

	return []Record{
		BlockChange(decision.Change{Type: decision.ChangeAdded, Key: consensus.Indicator.Key(), Decision: consensus, Cause: "verdict",
			Contributors: []decision.Contributor{alpha, bravo}}),
		BlockChange(decision.Change{Type: decision.ChangeUpdated, Key: autoblock.Indicator.Key(), Decision: autoblock, Cause: "refresh",
			Contributors: []decision.Contributor{self}}),
		BlockChange(decision.Change{Type: decision.ChangeRemoved, Key: removed.Indicator.Key(), Decision: removed, Cause: "revoke",
			Contributors: []decision.Contributor{alpha, bravo}}),
		Allowed(decision.Transition{Key: allowed.Indicator.Key(), From: decision.StateNone, Decision: allowed, Cause: "verdict"}),
		set,
		BlockChange(decision.Change{Type: decision.ChangeAdded, Key: forced.Indicator.Key(), Decision: forced, Cause: "override"}),
		unset,
		reported,
		revoked,
		PeerConnection(Peer{ID: "12D3KooWPeerB", Name: "node-b", Bootstrap: true, Publisher: true}, true),
		PeerConnection(Peer{ID: "12D3KooWPeerC"}, false),
		ConfigReloaded("/etc/obie/obied.yaml", []string{"node.mode", "decision.threshold"}, []string{"mesh.listen"}),
		ModeChanged("observe", "enforce"),
	}
}

// aboutAddress reports whether records of action are about an address,
// and so carry obie.indicator and rule.name.
func aboutAddress(action Action) bool {
	switch action {
	case ActionPeerConnected, ActionPeerDisconnected, ActionConfigReloaded, ActionModeChanged:
		return false
	default:
		return true
	}
}

// TestGolden: every action is written as one ECS JSON line.
func TestGolden(t *testing.T) {
	var logs bytes.Buffer
	l, path := startLog(t, "enforce", &logs)
	for _, r := range goldenRecords() {
		l.Write(r)
	}
	got := readFile(t, path)
	golden := filepath.Join("testdata", "audit.golden.jsonl")
	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if want := readFile(t, golden); got != want {
		t.Errorf("audit log differs from %s (run go test -update to accept):\ngot:\n%s\nwant:\n%s", golden, got, want)
	}
	if strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Errorf("errors logged: %s", logs.String())
	}
}

// TestLinesAreECS: every line is a JSON object with the ECS base fields,
// source.ip only for single addresses.
func TestLinesAreECS(t *testing.T) {
	var logs bytes.Buffer
	l, path := startLog(t, "observe", &logs)
	for _, r := range goldenRecords() {
		l.Write(r)
	}
	for i, line := range strings.Split(strings.TrimSuffix(readFile(t, path), "\n"), "\n") {
		var doc struct {
			Timestamp string `json:"@timestamp"`
			Event     struct{ Action, Outcome string }
			Source    *struct{ IP string }
			Rule      struct{ Name string }
			Obie      struct {
				Indicator, Mode string
				Contributors    *[]Contributor
			}
		}
		if err := json.Unmarshal([]byte(line), &doc); err != nil {
			t.Fatalf("line %d is not JSON: %v", i+1, err)
		}
		if _, err := time.Parse(time.RFC3339, doc.Timestamp); err != nil || doc.Event.Action == "" ||
			doc.Event.Outcome != OutcomeSuccess || doc.Obie.Mode != "observe" {
			t.Errorf("line %d lacks ECS fields: %s", i+1, line)
		}
		if about := aboutAddress(Action(doc.Event.Action)); about != (doc.Rule.Name != "") || about != (doc.Obie.Indicator != "") {
			t.Errorf("line %d: rule %q, indicator %q for %s", i+1, doc.Rule.Name, doc.Obie.Indicator, doc.Event.Action)
		}
		// Every block record lists its contributors, [] if none (ADR 0032).
		if block := strings.HasPrefix(doc.Event.Action, "block-"); block != (doc.Obie.Contributors != nil) {
			t.Errorf("line %d: contributors %v for %s", i+1, doc.Obie.Contributors, doc.Event.Action)
		}
		if isRange := strings.HasPrefix(doc.Obie.Indicator, "cidr:") || doc.Obie.Indicator == ""; isRange != (doc.Source == nil) {
			t.Errorf("line %d: source = %+v for %s", i+1, doc.Source, doc.Obie.Indicator)
		}
	}
}

// TestReopen: after the file was moved away, Reopen continues in a new
// file at the configured path, as logrotate expects.
func TestReopen(t *testing.T) {
	var logs bytes.Buffer
	l, path := startLog(t, "observe", &logs)
	l.Write(Record{Action: ActionOverrideRemoved, Indicator: ipv4("192.0.2.1")})
	rotated := path + ".1"
	if err := os.Rename(path, rotated); err != nil {
		t.Fatal(err)
	}
	l.Write(Record{Action: ActionOverrideRemoved, Indicator: ipv4("192.0.2.2")}) // still to the moved file
	if err := l.Reopen(); err != nil {
		t.Fatal(err)
	}
	l.Write(Record{Action: ActionOverrideRemoved, Indicator: ipv4("192.0.2.3")})

	if old := readFile(t, rotated); strings.Count(old, "\n") != 2 || !strings.Contains(old, "192.0.2.2") {
		t.Errorf("rotated file:\n%s", old)
	}
	if cur := readFile(t, path); strings.Count(cur, "\n") != 1 || !strings.Contains(cur, "192.0.2.3") {
		t.Errorf("new file:\n%s", cur)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != fileMode {
		t.Errorf("new file mode = %v, %v; want %v", info.Mode().Perm(), err, os.FileMode(fileMode))
	}
}

// TestReopenFailureKeepsWriting: if the path cannot be opened again, the
// records go on to the previous file.
func TestReopenFailureKeepsWriting(t *testing.T) {
	var logs bytes.Buffer
	l, path := startLog(t, "observe", &logs)
	dir := filepath.Dir(path)
	moved := dir + ".moved"
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Rename(moved, dir) })
	if err := l.Reopen(); err == nil {
		t.Fatal("Reopen succeeded without the directory")
	}
	l.Write(Record{Action: ActionOverrideRemoved, Indicator: ipv4("192.0.2.1")})
	if got := readFile(t, filepath.Join(moved, "audit.jsonl")); !strings.Contains(got, "192.0.2.1") {
		t.Errorf("record not written to the previous file: %q", got)
	}
	if !strings.Contains(logs.String(), "reopening the audit log failed") {
		t.Errorf("failure not logged: %s", logs.String())
	}
}

func TestStartFailsWithoutDirectory(t *testing.T) {
	l := New(filepath.Join(t.TempDir(), "missing", "audit.jsonl"), Options{Mode: func() string { return "observe" }},
		slog.New(slog.DiscardHandler))
	if err := l.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "open audit log") {
		t.Errorf("Start = %v, want an open error", err)
	}
}

func TestWriteAfterStopIsLogged(t *testing.T) {
	var logs bytes.Buffer
	l, _ := startLog(t, "observe", &logs)
	if err := l.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	l.Write(Record{Action: ActionOverrideRemoved, Indicator: ipv4("192.0.2.1")})
	if !strings.Contains(logs.String(), "audit log is not open; record lost") {
		t.Errorf("lost record not logged: %s", logs.String())
	}
}

func TestNilLogDoesNothing(t *testing.T) {
	var l *Log
	l.Write(Record{Action: ActionOverrideRemoved})
	if err := l.Reopen(); err != nil {
		t.Errorf("Reopen on nil = %v", err)
	}
}

// TestReasons: the reasons of peer, reload and mode records say what
// happened in one line.
func TestReasons(t *testing.T) {
	for _, tc := range []struct {
		r    Record
		want string
	}{
		{PeerConnection(Peer{ID: "12D3KooWA", Publisher: true}, true), "publisher 12D3KooWA connected"},
		{PeerConnection(Peer{ID: "12D3KooWA", Name: "a", Bootstrap: true}, false), "bootstrap peer a (12D3KooWA) disconnected"},
		{PeerConnection(Peer{ID: "12D3KooWA"}, true), "peer 12D3KooWA connected"},
		{ConfigReloaded("", nil, nil), "configuration reloaded: no setting changed"},
		{ConfigReloaded("/etc/obie/obied.yaml", []string{"trust.default_weight"}, nil),
			"configuration reloaded from /etc/obie/obied.yaml: trust.default_weight changed"},
		{ConfigReloaded("/c.yaml", nil, []string{"mesh.listen", "store.max_indicators"}),
			"configuration reloaded from /c.yaml: no setting changed; 2 settings wait for a restart (mesh.listen, store.max_indicators)"},
		{ModeChanged("enforce", "observe"), "node.mode changed from enforce to observe: blocks are only logged, and withdrawn from the firewall"},
	} {
		if tc.r.Reason != tc.want {
			t.Errorf("reason = %q, want %q", tc.r.Reason, tc.want)
		}
	}
}
