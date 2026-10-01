package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/audit"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/decision"
	"github.com/MNCloudwerksTechnology/obie/internal/mesh"
	"github.com/MNCloudwerksTechnology/obie/internal/verdicts"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// startAudit starts an audit log in a temporary directory.
func startAudit(t *testing.T) (*audit.Log, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	l := audit.New(path, audit.Options{Mode: func() string { return "observe" }}, slog.New(slog.DiscardHandler))
	if err := l.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Stop(context.Background()) })
	return l, path
}

// auditEntries returns "action indicator" of every line at path.
func auditEntries(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- test file.
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for line := range strings.Lines(string(data)) {
		var doc struct {
			Event struct{ Action string }
			Obie  struct{ Indicator string }
		}
		if err := json.Unmarshal([]byte(line), &doc); err != nil {
			t.Fatalf("audit line %q: %v", line, err)
		}
		out = append(out, doc.Event.Action+" "+doc.Obie.Indicator)
	}
	return out
}

func waitEntries(t *testing.T, path string, n int) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := auditEntries(t, path)
		if len(got) >= n || time.Now().After(deadline) {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestAuditDecisionChanges: block changes and allow-listings are
// recorded, the blocks that existed at subscription are not, and a reload
// reopens the file.
func TestAuditDecisionChanges(t *testing.T) {
	f := newReloadFixture(t)
	f.put(t, "198.18.0.11", self)
	deadline := time.Now().Add(5 * time.Second)
	for len(f.engine.Decisions(decision.StateBlock)) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	log, path := startAudit(t)
	subscribeAudit(f.engine, log)
	f.rl.audit = log

	f.put(t, "198.18.0.12", self)
	if got := waitEntries(t, path, 1); strings.Join(got, ",") != "block-added ipv4:198.18.0.12" {
		t.Fatalf("after a verdict: %q", got)
	}

	rotated := path + ".1"
	if err := os.Rename(path, rotated); err != nil {
		t.Fatal(err)
	}
	f.next.Allowlist.CIDRs = []string{"198.18.0.0/24"}
	if err := f.rl.reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := "block-removed ipv4:198.18.0.11,allowed-by-allowlist ipv4:198.18.0.11," +
		"block-removed ipv4:198.18.0.12,allowed-by-allowlist ipv4:198.18.0.12,config-reloaded "
	if got := auditEntries(t, path); strings.Join(got, ",") != want {
		t.Errorf("after the reload: %q\nwant %q", got, want)
	}
	if got := auditEntries(t, rotated); len(got) != 1 {
		t.Errorf("rotated file: %q", got)
	}
	// Both the block and its removal name this node's verdict (ADR 0032).
	checked := 0
	for _, file := range []string{rotated, path} {
		data, _ := os.ReadFile(file) // #nosec G304 -- test file.
		for line := range strings.Lines(string(data)) {
			var doc struct {
				Event struct{ Action string }
				Obie  struct {
					Indicator    string
					Contributors []audit.Contributor
				}
			}
			if err := json.Unmarshal([]byte(line), &doc); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(doc.Event.Action, "block-") || doc.Obie.Indicator != "ipv4:198.18.0.12" {
				continue
			}
			want := []audit.Contributor{{PeerID: self, Weight: 1, Confidence: 1, VerdictID: "01900000-0000-7000-8000-000000000012"}}
			if !reflect.DeepEqual(doc.Obie.Contributors, want) {
				t.Errorf("%s of 198.18.0.12: contributors %+v, want %+v", doc.Event.Action, doc.Obie.Contributors, want)
			}
			checked++
		}
	}
	if checked != 2 {
		t.Errorf("%d block records of 198.18.0.12, want its addition and its removal", checked)
	}
}

// fakeVerdicts answers with fixed results; other methods are not used.
type fakeVerdicts struct {
	admin.VerdictService
	result      verdicts.Result
	revocations []*obieproto.Event
	err         error
}

func (f fakeVerdicts) Report(context.Context, verdicts.Report) (verdicts.Result, error) {
	return f.result, f.err
}

func (f fakeVerdicts) Revoke(context.Context, verdicts.Revocation) ([]*obieproto.Event, error) {
	return f.revocations, f.err
}

func TestAuditedVerdicts(t *testing.T) {
	log, path := startAudit(t)
	ind := obieproto.Indicator{Kind: obieproto.KindIPv4, Value: "198.18.0.1", Scope: "/32"}
	ev := &obieproto.Event{ID: "e1", Type: obieproto.TypeVerdict, Indicator: ind,
		Evidence: &obieproto.Evidence{Events: 1}, Verdict: &obieproto.Verdict{SuggestedAction: obieproto.ActionBan}}
	rev := &obieproto.Event{ID: "e2", Type: obieproto.TypeRevoke, Indicator: ind, Revokes: "e1", Reason: "false_positive"}
	ctx := audit.WithOrigin(context.Background(), audit.Origin{Via: audit.OriginConsole, UserID: "1000", UserName: "alice"})

	_, _ = auditedVerdicts{fakeVerdicts{result: verdicts.Result{Event: ev}}, log}.Report(ctx, verdicts.Report{})
	_, _ = auditedVerdicts{fakeVerdicts{result: verdicts.Result{Event: ev, Coalesced: true}}, log}.Report(ctx, verdicts.Report{})
	_, _ = auditedVerdicts{fakeVerdicts{err: verdicts.ErrRefused}, log}.Report(ctx, verdicts.Report{})
	_, _ = auditedVerdicts{fakeVerdicts{revocations: []*obieproto.Event{rev}, err: errors.New("publish failed")}, log}.
		Revoke(ctx, verdicts.Revocation{})

	want := "local-report ipv4:198.18.0.1,revocation ipv4:198.18.0.1"
	if got := auditEntries(t, path); strings.Join(got, ",") != want {
		t.Errorf("audit = %q, want %q", got, want)
	}
	// Both name the door and the user the context carries (ADR 0026).
	data, _ := os.ReadFile(path) // #nosec G304 -- test file.
	if strings.Count(string(data), `"user":{"id":"1000","name":"alice"}`) != 2 || strings.Count(string(data), `"origin":"console"`) != 2 {
		t.Errorf("audit lacks the origin:\n%s", data)
	}
}

func TestAuditedOverrides(t *testing.T) {
	log, path := startAudit(t)
	s := storeOverrides{store: newStore(t), now: time.Now, audit: log}
	ind := obieproto.Indicator{Kind: obieproto.KindIPv4, Value: "198.18.0.1", Scope: "/32"}
	console := audit.WithOrigin(context.Background(), audit.Origin{Via: audit.OriginConsole, UserID: "1000"})
	if _, err := s.Set(console, ind, admin.ActionForceAllow, time.Hour, "partner"); err != nil {
		t.Fatal(err)
	}
	api := audit.WithOrigin(context.Background(), audit.Origin{Via: audit.OriginAdminAPI, UserID: "0", UserName: "root"})
	for range 2 { // the second delete finds nothing
		if _, err := s.Delete(api, ind); err != nil {
			t.Fatal(err)
		}
	}
	want := "override-set ipv4:198.18.0.1,override-removed ipv4:198.18.0.1"
	if got := auditEntries(t, path); strings.Join(got, ",") != want {
		t.Errorf("audit = %q, want %q", got, want)
	}
	data, _ := os.ReadFile(path) // #nosec G304 -- test file.
	if strings.Count(string(data), `"rule":{"name":"force_allow"}`) != 2 || !strings.Contains(string(data), `"note":"partner"`) {
		t.Errorf("audit lack the override details:\n%s", data)
	}
	if !strings.Contains(string(data), `"user":{"id":"1000"},"obie":{"indicator":"ipv4:198.18.0.1","mode":"observe","expires_at"`) ||
		!strings.Contains(string(data), `"user":{"id":"0","name":"root"}`) || !strings.Contains(string(data), `"origin":"admin-api"`) {
		t.Errorf("audit lacks the origins:\n%s", data)
	}
}

// TestNewAuditLog: without audit.path the trail keeps its records in
// memory only; with it, it writes the file too. Both record the gate's
// mode.
func TestNewAuditLog(t *testing.T) {
	gate := newReloadFixture(t).gate
	mode := func() config.Mode { return gate.Mode() }
	l := newAuditLog("", mode, slog.New(slog.DiscardHandler))
	if l == nil || l.Path() != "" {
		t.Fatalf("newAuditLog without path = %v", l)
	}
	l.Write(audit.ModeChanged("enforce", "observe"))
	if r := l.Since(0, 10, func(*audit.Entry) bool { return true }); len(r.Entries) != 1 || r.Entries[0].Obie.Mode != "observe" {
		t.Errorf("memory = %+v", r.Entries)
	}
	if l := newAuditLog("/var/log/obie/audit.jsonl", mode, slog.New(slog.DiscardHandler)); l.Path() != "/var/log/obie/audit.jsonl" {
		t.Errorf("newAuditLog with path = %q", l.Path())
	}
}

// TestAuditConnections: peers connecting and disconnecting are recorded
// with their name and role.
func TestAuditConnections(t *testing.T) {
	log, path := startAudit(t)
	record := auditConnections(log)
	record(mesh.Connection{ID: "12D3KooWB", Name: "beta", Bootstrap: true, Connected: true})
	record(mesh.Connection{ID: "12D3KooWB", Name: "beta", Bootstrap: true})
	if got, want := strings.Join(auditEntries(t, path), ","), "peer-connected ,peer-disconnected "; got != want {
		t.Errorf("audit = %q, want %q", got, want)
	}
	data, _ := os.ReadFile(path) // #nosec G304 -- test file.
	if strings.Count(string(data), `"peer_id":"12D3KooWB","peer_name":"beta"`) != 2 ||
		!strings.Contains(string(data), "bootstrap peer beta (12D3KooWB) disconnected") {
		t.Errorf("audit lacks the peer:\n%s", data)
	}
}
