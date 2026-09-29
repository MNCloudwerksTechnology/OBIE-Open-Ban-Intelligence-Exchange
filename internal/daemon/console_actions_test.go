package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/audit"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/console"
	"github.com/MNCloudwerksTechnology/obie/internal/decision"
	"github.com/MNCloudwerksTechnology/obie/internal/identity"
	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/internal/verdicts"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// publishingMesh stores the node's events like Mesh.Publish and holds them
// while it has no peers.
type publishingMesh struct {
	st *store.DB

	mu    sync.Mutex
	peers int
	held  map[string]bool
	// down is why the mesh does not run; nil while it runs. queued holds
	// every event, as behind earlier held ones.
	down   error
	queued bool
}

func (m *publishingMesh) Ready() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.down
}

func (m *publishingMesh) Publish(_ context.Context, ev *obieproto.Event) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	checked, err := obieproto.Receive(data)
	if err != nil {
		return err
	}
	if _, err := m.st.Put(checked); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.peers == 0 || m.queued {
		m.held[ev.ID] = true
	}
	return nil
}

func (m *publishingMesh) TopicPeers() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.peers
}

func (m *publishingMesh) Held(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.held[id]
}

// actionsFixture is a node's store, running engine, verdict service and
// audit trail behind consoleActions and the admin API alike;
// allowlist.cidrs is 85.20.0.0/16.
type actionsFixture struct {
	st      *store.DB
	engine  *decision.Engine
	mesh    *publishingMesh
	trail   *audit.Log
	actions *consoleActions
	admin   http.Handler
}

func newActionsFixture(t *testing.T) *actionsFixture {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	key, err := identity.Create(filepath.Join(t.TempDir(), "state"), false)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Allowlist.CIDRs = []string{"85.20.0.0/16"}
	allow, err := sovereignty.Build(context.Background(), &cfg, noHost, log)
	if err != nil {
		t.Fatal(err)
	}
	st := store.NewMemory(log, store.Options{SweepInterval: time.Hour, GCInterval: time.Hour, Self: key.PeerID()})
	if err := st.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Stop(context.Background()) })
	engine := decision.New(st, decision.NewPolicy(key.PeerID(), cfg.Trust, cfg.Decision), log,
		decision.Options{Allowlist: allow, RefreshInterval: time.Hour})
	if err := engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Stop(context.Background()) })
	mesh := &publishingMesh{st: st, held: map[string]bool{}}
	svc := verdicts.New(verdicts.Options{Store: st, Publisher: mesh, Signer: key, DefaultTTL: cfg.Decision.DefaultTTL.Std(),
		MaxTTL: cfg.Decision.MaxTTL.Std(), Allowlist: []netip.Prefix{netip.MustParsePrefix("85.20.0.0/16")}}, log)
	trail := audit.New("", audit.Options{Mode: func() string { return "observe" }}, log)
	overrides := storeOverrides{store: st, now: time.Now, audit: trail}
	audited := auditedVerdicts{VerdictService: svc, audit: trail}
	f := &actionsFixture{st: st, engine: engine, mesh: mesh, trail: trail,
		actions: &consoleActions{store: st, self: key.PeerID(), now: time.Now, overrides: overrides, verdicts: audited,
			checker: svc, engine: engine, mesh: mesh, mode: func() string { return "enforce" }},
		admin: admin.Handler(admin.Info{Mode: func() string { return "enforce" }, Overrides: overrides, Verdicts: audited}, log)}
	return f
}

// records returns the audit records written so far, oldest first.
func (f *actionsFixture) records() []audit.Entry {
	r := f.trail.Since(0, 100, func(*audit.Entry) bool { return true })
	out := make([]audit.Entry, len(r.Entries))
	for i, e := range r.Entries {
		out[len(out)-1-i] = e
	}
	return out
}

// kept returns the state of the engine's decision on key, "" if none.
func (f *actionsFixture) kept(key string) decision.State {
	d, ok := f.engine.Decision(key)
	if !ok {
		return ""
	}
	return d.State
}

var alice = console.Actor{UID: 1000, Known: true}

// wantActionError checks that err is an ActionError with status and a
// message containing want.
func wantActionError(t *testing.T, what string, err error, status int, want string) {
	t.Helper()
	var ae *console.ActionError
	if !errors.As(err, &ae) || ae.Status != status || !strings.Contains(ae.Message, want) {
		t.Errorf("%s: error = %v, want %d with %q", what, err, status, want)
	}
}

// TestConsoleOverrideActions: the console sets and removes overrides with
// the store's rules, tells the decision before and after, records them as
// the console's, and the engine holds the new decision at once.
func TestConsoleOverrideActions(t *testing.T) {
	f := newActionsFixture(t)
	block := console.ActionRequest{Kind: console.ActionBlock, Address: "85.10.0.7", TTL: time.Hour, Note: "scanner"}
	r, err := f.actions.Review(block)
	if err != nil {
		t.Fatal(err)
	}
	if r.Range != netip.MustParsePrefix("85.10.0.7/32") || r.Override != nil || r.Now.State != "none" ||
		r.After.State != "block" || r.After.Rule != "force_block" || r.Mode != "enforce" {
		t.Errorf("Review(block) = %+v", r)
	}
	if len(f.records()) != 0 || f.kept("ipv4:85.10.0.7") != "" {
		t.Fatal("reviewing changed something")
	}

	out, err := f.actions.Do(context.Background(), block, alice)
	if err != nil {
		t.Fatal(err)
	}
	if out.Override == nil || out.Override.Action != "force_block" || out.Override.Note != "scanner" ||
		out.Override.ExpiresAt.IsZero() || out.Decision.State != "block" || out.Warning != "" {
		t.Errorf("Do(block) = %+v", out)
	}
	if got := f.kept("ipv4:85.10.0.7"); got != decision.StateBlock {
		t.Errorf("kept decision right after the action = %q, want block", got)
	}

	unoverride := console.ActionRequest{Kind: console.ActionUnoverride, Address: "85.10.0.7"}
	r, err = f.actions.Review(unoverride)
	if err != nil || r.Override == nil || r.Override.Note != "scanner" || r.Now.State != "block" || r.After.State != "none" {
		t.Errorf("Review(unoverride) = %+v, %v", r, err)
	}
	if _, err := f.actions.Do(context.Background(), unoverride, console.Actor{}); err != nil {
		t.Fatal(err)
	}
	if got := f.kept("ipv4:85.10.0.7"); got == decision.StateBlock {
		t.Errorf("kept decision after the removal = %q", got)
	}
	_, err = f.actions.Review(unoverride)
	wantActionError(t, "unoverride without override", err, http.StatusNotFound, "no override: ipv4:85.10.0.7")
	_, err = f.actions.Do(context.Background(), unoverride, alice)
	wantActionError(t, "unoverride without override", err, http.StatusNotFound, "no override: ipv4:85.10.0.7")

	recs := f.records()
	if len(recs) < 2 {
		t.Fatalf("records = %+v", recs)
	}
	set, removed := recs[0], recs[len(recs)-1]
	if set.Event.Action != audit.ActionOverrideSet || set.Obie.Origin != audit.OriginConsole || set.User == nil ||
		set.User.ID != "1000" || set.Obie.Note != "scanner" {
		t.Errorf("override-set record = %+v, user %+v", set, set.User)
	}
	if removed.Event.Action != audit.ActionOverrideRemoved || removed.Obie.Origin != audit.OriginConsole || removed.User != nil {
		t.Errorf("override-removed record = %+v", removed)
	}
}

// TestConsoleBlockWithoutEffect: a force-block on a protected address is
// set, as obiectl sets it, and the review and outcome say it does not take
// effect.
func TestConsoleBlockWithoutEffect(t *testing.T) {
	f := newActionsFixture(t)
	block := console.ActionRequest{Kind: console.ActionBlock, Address: "10.0.0.7"}
	r, err := f.actions.Review(block)
	if err != nil || r.After.State != "allowed" || !r.After.Protected {
		t.Errorf("Review(block of a protected address) = %+v, %v", r, err)
	}
	out, err := f.actions.Do(context.Background(), block, alice)
	if err != nil || !strings.HasPrefix(out.Warning, "the force-block does not take effect: ") {
		t.Errorf("Do(block of a protected address) = %+v, %v", out, err)
	}
}

// TestConsoleReportAndRevoke: a report while no peer is reachable is
// stored, counts at once and is held; a second one within a minute would
// be coalesced; the revocation withdraws it. Both are the console's in
// the audit trail.
func TestConsoleReportAndRevoke(t *testing.T) {
	f := newActionsFixture(t)
	report := console.ActionRequest{Kind: console.ActionReport, Address: "85.10.0.8",
		Report: console.ReportDetails{Protocol: "ssh", Reason: "password_bruteforce", Events: 12}}
	r, err := f.actions.Review(report)
	if err != nil {
		t.Fatal(err)
	}
	if r.Planned == nil || r.Planned.Action != "ban" || r.Planned.Confidence != verdicts.DefaultConfidence ||
		r.Planned.TTL != config.Default().Decision.DefaultTTL.Std() || r.Planned.Coalesced || r.Planned.Refreshes ||
		r.Peers != 0 || r.Now.State != "none" || r.After.State != "block" || !r.After.Autoblock {
		t.Errorf("Review(report) = %+v, planned %+v", r, r.Planned)
	}

	out, err := f.actions.Do(context.Background(), report, alice)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.EventIDs) != 1 || !out.Held || out.Peers != 0 || out.Coalesced || out.Decision.State != "block" {
		t.Errorf("Do(report) = %+v", out)
	}
	if got := f.kept("ipv4:85.10.0.8"); got != decision.StateBlock {
		t.Errorf("kept decision right after the report = %q, want block", got)
	}
	if r, err := f.actions.Review(report); err != nil || !r.Planned.Coalesced || r.Verdict == nil || r.Verdict.EventID != out.EventIDs[0] {
		t.Errorf("Review(report again) = %+v, %v", r, err)
	}

	f.mesh.mu.Lock()
	f.mesh.peers = 3
	f.mesh.mu.Unlock()
	revoke := console.ActionRequest{Kind: console.ActionRevoke, Address: "85.10.0.8", Reason: "false_positive"}
	r, err = f.actions.Review(revoke)
	if err != nil || r.Verdict == nil || r.Verdict.Protocol != "ssh" || r.Verdict.Events != 12 || r.Now.State != "block" ||
		r.After.State != "none" || r.Peers != 3 {
		t.Errorf("Review(revoke) = %+v, %v", r, err)
	}
	out, err = f.actions.Do(context.Background(), revoke, alice)
	if err != nil || len(out.EventIDs) != 1 || out.Held || out.Peers != 3 || out.Decision.State == "block" {
		t.Errorf("Do(revoke) = %+v, %v", out, err)
	}
	// Held behind other events while a peer is on the topic, a report is
	// as good as sent.
	f.mesh.mu.Lock()
	f.mesh.queued = true
	f.mesh.mu.Unlock()
	out, err = f.actions.Do(context.Background(), console.ActionRequest{Kind: console.ActionReport, Address: "85.10.0.9",
		Report: console.ReportDetails{Protocol: "ssh", Reason: "password_bruteforce", Events: 1}}, alice)
	if err != nil || !f.mesh.Held(out.EventIDs[0]) || out.Held || out.Peers != 3 {
		t.Errorf("Do(report behind held events) = %+v, %v", out, err)
	}
	_, err = f.actions.Review(revoke)
	wantActionError(t, "revoke without verdict", err, http.StatusNotFound, "this node has no active verdict on ipv4:85.10.0.8")

	var actions []string
	for _, e := range f.records() {
		if e.Event.Action == audit.ActionLocalReport || e.Event.Action == audit.ActionRevocation {
			actions = append(actions, string(e.Event.Action))
			if e.Obie.Origin != audit.OriginConsole || e.User == nil || e.User.ID != "1000" {
				t.Errorf("%s record = %+v", e.Event.Action, e)
			}
		}
	}
	if strings.Join(actions, ",") != "local-report,revocation,local-report" {
		t.Errorf("records = %v", actions)
	}
}

// TestConsoleActionsObeyTheAdminAPI: the console refuses what the admin
// API refuses, with the same status and words (AC3).
func TestConsoleActionsObeyTheAdminAPI(t *testing.T) {
	f := newActionsFixture(t)
	for _, tc := range []struct {
		name       string
		req        console.ActionRequest
		path, body string
	}{
		{"allow-listed report", console.ActionRequest{Kind: console.ActionReport, Address: "85.20.1.1",
			Report: console.ReportDetails{Protocol: "ssh", Reason: "password_bruteforce", Events: 1}},
			admin.ReportsPath, `{"ip":"85.20.1.1","protocol":"ssh","reason":"password_bruteforce","events":1}`},
		{"non-public report", console.ActionRequest{Kind: console.ActionReport, Address: "10.1.2.3",
			Report: console.ReportDetails{Protocol: "ssh", Reason: "password_bruteforce", Events: 1}},
			admin.ReportsPath, `{"ip":"10.1.2.3","protocol":"ssh","reason":"password_bruteforce","events":1}`},
		{"report without protocol", console.ActionRequest{Kind: console.ActionReport, Address: "85.10.0.9",
			Report: console.ReportDetails{Reason: "password_bruteforce", Events: 1}},
			admin.ReportsPath, `{"ip":"85.10.0.9","reason":"password_bruteforce","events":1}`},
		{"report of a broad range", console.ActionRequest{Kind: console.ActionReport, Address: "85.0.0.0/8",
			Report: console.ReportDetails{Protocol: "ssh", Reason: "x", Events: 1}},
			admin.ReportsPath, `{"cidr":"85.0.0.0/8","protocol":"ssh","reason":"x","events":1}`},
		{"override of no address", console.ActionRequest{Kind: console.ActionAllow, Address: "nonsense"},
			admin.OverridesPath, `{"indicator":"nonsense","action":"force_allow"}`},
		{"override with a long note", console.ActionRequest{Kind: console.ActionBlock, Address: "85.10.0.9", Note: strings.Repeat("x", 1025)},
			admin.OverridesPath, `{"indicator":"85.10.0.9","action":"force_block","note":"` + strings.Repeat("x", 1025) + `"}`},
		{"revocation without verdict", console.ActionRequest{Kind: console.ActionRevoke, Address: "85.10.0.9", Reason: "false_positive"},
			admin.RevocationsPath, `{"indicator":"85.10.0.9","reason":"false_positive"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			f.admin.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body)))
			_, err := f.actions.Review(tc.req)
			var ae *console.ActionError
			if !errors.As(err, &ae) {
				t.Fatalf("Review = %v, want an ActionError like the admin API's %d %q", err, rec.Code, rec.Body)
			}
			if ae.Status != rec.Code || ae.Message+"\n" != rec.Body.String() {
				t.Errorf("console: %d %q\nadmin API: %d %q", ae.Status, ae.Message, rec.Code, rec.Body)
			}
			_, err = f.actions.Do(context.Background(), tc.req, alice)
			if !errors.As(err, &ae) || ae.Status != rec.Code {
				t.Errorf("Do = %v, want %d", err, rec.Code)
			}
		})
	}
	if recs := f.records(); len(recs) != 0 {
		t.Errorf("refused actions were recorded: %+v", recs)
	}
}

// TestConsoleActionsWhileTheMeshIsDown: while the mesh does not run, a
// report or revocation is refused with the reason instead of promising to
// send it; overrides need no mesh.
func TestConsoleActionsWhileTheMeshIsDown(t *testing.T) {
	f := newActionsFixture(t)
	f.mesh.mu.Lock()
	f.mesh.down = errors.New("host not started")
	f.mesh.mu.Unlock()
	report := console.ActionRequest{Kind: console.ActionReport, Address: "85.10.0.8",
		Report: console.ReportDetails{Protocol: "ssh", Reason: "password_bruteforce", Events: 1}}
	_, err := f.actions.Review(report)
	wantActionError(t, "review of a report", err, http.StatusServiceUnavailable, "the mesh is not running (host not started)")
	_, err = f.actions.Do(context.Background(), report, alice)
	wantActionError(t, "report", err, http.StatusServiceUnavailable, "the mesh is not running")
	_, err = f.actions.Review(console.ActionRequest{Kind: console.ActionRevoke, Address: "85.10.0.8", Reason: "false_positive"})
	wantActionError(t, "review of a revocation", err, http.StatusServiceUnavailable, "the mesh is not running")
	if _, err := f.actions.Do(context.Background(), console.ActionRequest{Kind: console.ActionAllow, Address: "85.10.0.8"}, alice); err != nil {
		t.Errorf("an override while the mesh is down = %v", err)
	}
	if active, _ := f.st.ActiveVerdicts("ipv4:85.10.0.8", time.Now()); len(active) != 0 {
		t.Errorf("verdicts stored while the mesh is down: %v", active)
	}
}
