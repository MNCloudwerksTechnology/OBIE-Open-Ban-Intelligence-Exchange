package verdicts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/identity"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

const (
	attacker = "85.10.0.7"
	day      = 24 * time.Hour
)

// clock is a settable test clock starting at the current second, because
// Badger applies TTLs against the wall clock.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// publisher stores events like Mesh.Publish and records what it sent.
type publisher struct {
	db   *store.DB
	now  func() time.Time
	fail error
	// receive are extra obieproto.Receive options, as the mesh's.
	receive []obieproto.Option

	mu   sync.Mutex
	sent [][]byte
}

func (p *publisher) Publish(_ context.Context, ev *obieproto.Event) error {
	if p.fail != nil {
		return p.fail
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	checked, err := obieproto.Receive(data, append(p.receive, obieproto.WithClock(p.now))...)
	if err != nil {
		return err
	}
	if _, err := p.db.Put(checked); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, data)
	return nil
}

func (p *publisher) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.sent)
}

type fixture struct {
	svc   *Service
	db    *store.DB
	dbDir string
	pub   *publisher
	clock *clock
	key   *identity.Key
	logs  *syncBuffer
}

// syncBuffer is a bytes.Buffer the store's goroutines can log into while
// the test reads it.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func newFixture(t *testing.T, allowlist ...string) *fixture {
	t.Helper()
	dir := t.TempDir()
	key, err := identity.Create(filepath.Join(dir, "state"), false)
	if err != nil {
		t.Fatal(err)
	}
	c := &clock{t: time.Now().UTC().Truncate(time.Second)}
	var logs syncBuffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	dbDir := filepath.Join(dir, "db")
	db := store.New(dbDir, log, store.Options{Now: c.Now})
	if err := db.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Stop(context.Background()) })
	prefixes := make([]netip.Prefix, len(allowlist))
	for i, a := range allowlist {
		prefixes[i] = netip.MustParsePrefix(a)
	}
	pub := &publisher{db: db, now: c.Now}
	svc := New(Options{
		Store: db, Publisher: pub, Signer: key,
		DefaultTTL: 7 * day, MaxTTL: 30 * day,
		Allowlist: prefixes, Now: c.Now,
	}, log)
	return &fixture{svc: svc, db: db, dbDir: dbDir, pub: pub, clock: c, key: key, logs: &logs}
}

func ipv4(value string) obieproto.Indicator {
	return obieproto.Indicator{Kind: obieproto.KindIPv4, Value: value}
}

func sshReport(value string, events int64) Report {
	return Report{Indicator: ipv4(value), Protocol: "ssh", Reason: "password_bruteforce", Events: events}
}

func (f *fixture) report(t *testing.T, r Report) Result {
	t.Helper()
	res, err := f.svc.Report(context.Background(), r)
	if err != nil {
		t.Fatalf("Report(%+v): %v", r, err)
	}
	return res
}

func TestReportDefaults(t *testing.T) {
	f := newFixture(t)
	res := f.report(t, sshReport(attacker, 5))
	ev := res.Event
	if res.Coalesced || res.Supersedes != "" {
		t.Errorf("first report: %+v", res)
	}
	if ev.Type != obieproto.TypeVerdict || ev.Indicator.Key() != "ipv4:"+attacker || ev.Indicator.Scope != "/32" ||
		ev.Verdict.SuggestedAction != obieproto.ActionBan || ev.Verdict.Confidence != DefaultConfidence ||
		ev.Verdict.TTLSeconds != int64(7*day/time.Second) || ev.Evidence.Events != 5 || ev.Evidence.LogHash != "" ||
		ev.MITRE != nil || !ev.IssuedAt.Equal(f.clock.Now()) || ev.Publisher.PeerID != f.key.PeerID() {
		t.Errorf("verdict = %+v, evidence %+v, verdict %+v", ev, ev.Evidence, ev.Verdict)
	}
	if err := obieproto.Verify(ev); err != nil {
		t.Errorf("verdict signature: %v", err)
	}
	if f.pub.count() != 1 {
		t.Errorf("published %d events, want 1", f.pub.count())
	}
	stored, err := f.db.Get(ev.ID)
	if err != nil || stored.Publisher.Signature != ev.Publisher.Signature {
		t.Errorf("stored verdict = %+v, %v", stored, err)
	}
}

func TestReportOptions(t *testing.T) {
	f := newFixture(t)
	confidence := 0.0
	ev := f.report(t, Report{
		Indicator: obieproto.Indicator{Kind: obieproto.KindCIDR, Value: "85.10.3.9/24"},
		Protocol:  "http", Reason: "scanning", Events: 12, Confidence: &confidence,
		TTL: 90*time.Minute + 500*time.Millisecond, Action: obieproto.ActionWatch, MITRE: []string{"T1595"},
	}).Event
	if ev.Indicator.Key() != "cidr:85.10.3.0/24" || ev.Verdict.Confidence != 0 || ev.Verdict.TTLSeconds != 5400 ||
		ev.Verdict.SuggestedAction != obieproto.ActionWatch || len(ev.MITRE) != 1 || ev.Protocol != "http" {
		t.Errorf("verdict = %+v, %+v", ev, ev.Verdict)
	}
	capped := f.report(t, Report{Indicator: ipv4("85.10.0.8"), Protocol: "ssh", Reason: "x", Events: 1, TTL: 365 * day, MITRE: []string{}})
	if capped.Event.Verdict.TTLSeconds != int64(30*day/time.Second) || capped.Event.MITRE != nil {
		t.Errorf("capped verdict = %+v, mitre %v", capped.Event.Verdict, capped.Event.MITRE)
	}
}

func TestReportMaxTTLBelowProtocolLimit(t *testing.T) {
	f := newFixture(t)
	f.svc.opts.MaxTTL = 2 * time.Hour
	ev := f.report(t, sshReport(attacker, 1)).Event
	if ev.Verdict.TTLSeconds != 7200 {
		t.Errorf("default TTL above max_ttl gave %d s, want 7200", ev.Verdict.TTLSeconds)
	}
}

// TestEvidenceNeverLeavesTheHash checks that raw log lines appear nowhere:
// not in the published or stored event, not in the logs and not in the
// database files.
func TestEvidenceNeverLeavesTheHash(t *testing.T) {
	f := newFixture(t)
	lines := []string{
		"Sep 28 12:00:01 host sshd[4711]: Failed password for secret-operator-name from 85.10.0.7 port 50123 ssh2",
		"Sep 28 12:00:03 host sshd[4711]: Failed password for secret-operator-name from 85.10.0.7 port 50125 ssh2",
	}
	r := sshReport(attacker, 2)
	r.EvidenceLines = lines
	ev := f.report(t, r).Event

	sum := sha256.Sum256([]byte(lines[0] + "\n" + lines[1]))
	if want := "sha256:" + hex.EncodeToString(sum[:]); ev.Evidence.LogHash != want {
		t.Errorf("log_hash = %q, want %q", ev.Evidence.LogHash, want)
	}
	const secret = "secret-operator-name"
	f.pub.mu.Lock()
	for _, data := range f.pub.sent {
		if bytes.Contains(data, []byte(secret)) || bytes.Contains(data, []byte("sshd[4711]")) {
			t.Errorf("published event contains raw evidence: %s", data)
		}
	}
	f.pub.mu.Unlock()
	stored, err := f.db.Get(ev.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(secret)) {
		t.Errorf("stored event contains raw evidence: %s", data)
	}
	if strings.Contains(f.logs.String(), secret) || strings.Contains(f.logs.String(), "port 50123") {
		t.Errorf("logs contain raw evidence:\n%s", f.logs.String())
	}

	if err := f.db.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(f.dbDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		content, err := os.ReadFile(path) // #nosec G304 G122 -- files of the test database, which nothing else touches.
		if err != nil {
			return err
		}
		if bytes.Contains(content, []byte(secret)) {
			t.Errorf("database file %s contains raw evidence", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRepeatedReportRefreshesVerdict(t *testing.T) {
	f := newFixture(t)
	first := f.report(t, sshReport(attacker, 5)).Event
	f.clock.Advance(CoalesceWindow)
	// issued_at has whole seconds: a verdict issued at x.9 s has issued_at
	// x s, so a report 60 s after issued_at is still coalesced.
	if res := f.report(t, sshReport(attacker, 1)); !res.Coalesced {
		t.Fatalf("report 60 s after issued_at = %+v, want coalesced", res)
	}
	f.clock.Advance(time.Second)
	res := f.report(t, sshReport(attacker, 2))
	second := res.Event
	if res.Coalesced || res.Supersedes != first.ID || second.ID == first.ID {
		t.Fatalf("refresh = %+v", res)
	}
	if second.Evidence.Events != 8 || !second.ExpiresAt().After(first.ExpiresAt()) {
		t.Errorf("refreshed verdict events %d, expires %v (first expires %v)", second.Evidence.Events,
			second.ExpiresAt(), first.ExpiresAt())
	}
	active, err := f.svc.Lookup(ipv4(attacker))
	if err != nil || len(active) != 1 || active[0].ID != second.ID {
		t.Errorf("active verdicts = %v, %v; want only the refreshed one", active, err)
	}
}

func TestReportsWithinWindowAreCoalesced(t *testing.T) {
	f := newFixture(t)
	lines := []string{"line one"}
	r := sshReport(attacker, 5)
	r.EvidenceLines = lines
	first := f.report(t, r).Event

	f.clock.Advance(10 * time.Second)
	res := f.report(t, sshReport(attacker, 3))
	if !res.Coalesced || res.Event.ID != first.ID || res.Supersedes != "" {
		t.Errorf("report within window = %+v, want coalesced into %s", res, first.ID)
	}
	f.clock.Advance(40 * time.Second)
	f.report(t, sshReport(attacker, 4))
	if f.pub.count() != 1 {
		t.Fatalf("published %d events within the window, want 1", f.pub.count())
	}

	f.clock.Advance(11 * time.Second)
	refreshed := f.report(t, sshReport(attacker, 1))
	if refreshed.Coalesced || refreshed.Supersedes != first.ID || refreshed.Event.Evidence.Events != 5+3+4+1 {
		t.Errorf("refresh after window = %+v, events %d, want 13", refreshed, refreshed.Event.Evidence.Events)
	}
	if refreshed.Event.Evidence.LogHash != first.Evidence.LogHash {
		t.Errorf("refresh without evidence lost the log hash: %q", refreshed.Event.Evidence.LogHash)
	}

	// The pending counts were consumed by the refresh.
	f.clock.Advance(CoalesceWindow + time.Second)
	if again := f.report(t, sshReport(attacker, 1)); again.Event.Evidence.Events != 14 {
		t.Errorf("second refresh events = %d, want 14", again.Event.Evidence.Events)
	}
}

func TestPendingCountsOfExpiredVerdictsArePruned(t *testing.T) {
	f := newFixture(t)
	r := sshReport(attacker, 5)
	r.TTL = 2 * time.Minute
	f.report(t, r)
	f.clock.Advance(time.Second)
	f.report(t, sshReport(attacker, 3))
	f.clock.Advance(2 * time.Minute)
	res := f.report(t, sshReport(attacker, 1))
	if res.Supersedes != "" || res.Event.Evidence.Events != 1 {
		t.Errorf("report after expiry = %+v, events %d; want a fresh verdict with 1 event", res, res.Event.Evidence.Events)
	}
	if len(f.svc.pending) != 0 {
		t.Errorf("pending = %v, want empty", f.svc.pending)
	}
}

func TestReportDocumentationRanges(t *testing.T) {
	f := newFixture(t)
	f.svc.opts.AllowDocumentationRanges = true
	f.pub.receive = []obieproto.Option{obieproto.ReceiveDocumentationRanges()}
	res := f.report(t, sshReport("203.0.113.7", 3))
	if res.Event == nil || res.Event.Indicator.Value != "203.0.113.7" || f.pub.count() != 1 {
		t.Errorf("Report(documentation address) with AllowDocumentationRanges = %+v, published %d; want one verdict",
			res, f.pub.count())
	}
	r := sshReport("10.1.2.3", 1)
	if _, err := f.svc.Report(context.Background(), r); !errors.Is(err, ErrRefused) {
		t.Errorf("Report(private address) with AllowDocumentationRanges = %v, want ErrRefused", err)
	}
}

func TestReportRefused(t *testing.T) {
	f := newFixture(t, "85.20.0.0/16", "85.30.1.0/24", "2a01:4f8::/32")
	for name, ind := range map[string]obieproto.Indicator{
		"private":            ipv4("10.1.2.3"),
		"loopback":           ipv4("127.0.0.1"),
		"documentation":      ipv4("203.0.113.7"),
		"link-local ipv6":    {Kind: obieproto.KindIPv6, Value: "fe80::1"},
		"allow-listed":       ipv4("85.20.1.1"),
		"allow-listed range": {Kind: obieproto.KindCIDR, Value: "85.20.0.0/24"},
		"covers allow-list":  {Kind: obieproto.KindCIDR, Value: "85.30.0.0/16"},
		"allow-listed ipv6":  {Kind: obieproto.KindIPv6, Value: "2a01:4f8::1"},
	} {
		t.Run(name, func(t *testing.T) {
			r := sshReport("", 1)
			r.Indicator = ind
			if _, err := f.svc.Report(context.Background(), r); !errors.Is(err, ErrRefused) {
				t.Errorf("Report(%v) error = %v, want ErrRefused", ind, err)
			}
			if _, err := f.svc.Check(r); !errors.Is(err, ErrRefused) {
				t.Errorf("Check(%v) error = %v, want ErrRefused", ind, err)
			}
		})
	}
	if f.pub.count() != 0 {
		t.Errorf("published %d events for refused reports", f.pub.count())
	}
}

func TestReportInvalid(t *testing.T) {
	f := newFixture(t)
	confidence := 1.5
	for name, mutate := range map[string]func(*Report){
		"no indicator":     func(r *Report) { r.Indicator = obieproto.Indicator{} },
		"bad address":      func(r *Report) { r.Indicator.Value = "85.10.0" },
		"no protocol":      func(r *Report) { r.Protocol = "" },
		"protocol text":    func(r *Report) { r.Protocol = "SSH login" },
		"no reason":        func(r *Report) { r.Reason = "" },
		"zero events":      func(r *Report) { r.Events = 0 },
		"negative events":  func(r *Report) { r.Events = -1 },
		"confidence > 1":   func(r *Report) { r.Confidence = &confidence },
		"unknown action":   func(r *Report) { r.Action = "drop" },
		"bad mitre":        func(r *Report) { r.MITRE = []string{"brute force"} },
		"ttl below 60s":    func(r *Report) { r.TTL = 59 * time.Second },
		"negative ttl":     func(r *Report) { r.TTL = -time.Hour },
		"broad cidr range": func(r *Report) { r.Indicator = obieproto.Indicator{Kind: obieproto.KindCIDR, Value: "85.0.0.0/8"} },
	} {
		t.Run(name, func(t *testing.T) {
			r := sshReport(attacker, 1)
			mutate(&r)
			if _, err := f.svc.Report(context.Background(), r); !errors.Is(err, ErrInvalid) {
				t.Errorf("Report() error = %v, want ErrInvalid", err)
			}
			if _, err := f.svc.Check(r); !errors.Is(err, ErrInvalid) {
				t.Errorf("Check() error = %v, want ErrInvalid", err)
			}
		})
	}
	if f.pub.count() != 0 {
		t.Errorf("published %d events for invalid reports", f.pub.count())
	}
}

// TestCheckPlansAReport: Check tells what a report would do — the
// verdict with its lifetime resolved, a first one, a refresh or a report
// coalesced into the current verdict — and issues nothing (ADR 0026).
func TestCheckPlansAReport(t *testing.T) {
	f := newFixture(t, "85.20.0.0/16")
	long := sshReport(attacker, 3)
	long.TTL = 365 * day
	plan, err := f.svc.Check(long)
	if err != nil || plan.Current != nil || plan.Coalesced || plan.Verdict.Verdict.TTLSeconds != int64(30*day/time.Second) ||
		plan.Verdict.Verdict.Confidence != DefaultConfidence || plan.Verdict.Indicator.Scope != "/32" {
		t.Fatalf("Check(first) = %+v, %v", plan, err)
	}
	if f.pub.count() != 0 {
		t.Fatalf("Check published %d events", f.pub.count())
	}
	current := f.report(t, sshReport(attacker, 5)).Event
	if plan, err := f.svc.Check(sshReport(attacker, 1)); err != nil || !plan.Coalesced || plan.Current.ID != current.ID {
		t.Errorf("Check(within the window) = %+v, %v; want coalesced into %s", plan, err, current.ID)
	}
	f.clock.Advance(CoalesceWindow + 2*time.Second)
	if plan, err := f.svc.Check(sshReport(attacker, 1)); err != nil || plan.Coalesced || plan.Current.ID != current.ID {
		t.Errorf("Check(after the window) = %+v, %v; want a refresh of %s", plan, err, current.ID)
	}
	if f.pub.count() != 1 {
		t.Errorf("published %d events, want only the report's", f.pub.count())
	}
}

func TestInvalidReportDoesNotCoalesce(t *testing.T) {
	f := newFixture(t)
	f.report(t, sshReport(attacker, 5))
	bad := sshReport(attacker, 3)
	bad.Protocol = ""
	if _, err := f.svc.Report(context.Background(), bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid report error = %v", err)
	}
	f.clock.Advance(CoalesceWindow + time.Second)
	if ev := f.report(t, sshReport(attacker, 1)).Event; ev.Evidence.Events != 6 {
		t.Errorf("refresh counted the invalid report: events %d, want 6", ev.Evidence.Events)
	}
}

func TestReportPublishFailure(t *testing.T) {
	f := newFixture(t)
	f.pub.fail = errors.New("mesh not started")
	if _, err := f.svc.Report(context.Background(), sshReport(attacker, 1)); err == nil ||
		errors.Is(err, ErrInvalid) || errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "mesh not started") {
		t.Errorf("Report() error = %v, want the publish error", err)
	}
}

func TestRevokeByEventID(t *testing.T) {
	f := newFixture(t)
	v := f.report(t, sshReport(attacker, 5)).Event
	revs, err := f.svc.Revoke(context.Background(), Revocation{EventID: v.ID, Reason: "false_positive"})
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 1 || revs[0].Type != obieproto.TypeRevoke || revs[0].Revokes != v.ID || revs[0].Reason != "false_positive" ||
		revs[0].Indicator != v.Indicator || revs[0].Publisher.PeerID != f.key.PeerID() {
		t.Fatalf("revocations = %+v", revs)
	}
	if err := obieproto.Verify(revs[0]); err != nil {
		t.Errorf("revocation signature: %v", err)
	}
	if active, err := f.svc.Lookup(ipv4(attacker)); err != nil || len(active) != 0 {
		t.Errorf("active verdicts after revocation = %v, %v", active, err)
	}
	if _, err := f.svc.Revoke(context.Background(), Revocation{EventID: v.ID, Reason: "false_positive"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("second revocation error = %v, want ErrNotFound", err)
	}
	// A report right after the revocation issues a new verdict.
	if res := f.report(t, sshReport(attacker, 1)); res.Coalesced || res.Supersedes != "" || res.Event.Evidence.Events != 1 {
		t.Errorf("report after revocation = %+v", res)
	}
}

func TestRevokeByIndicator(t *testing.T) {
	f := newFixture(t)
	v := f.report(t, sshReport(attacker, 5)).Event
	f.report(t, sshReport("85.10.0.8", 1))
	f.clock.Advance(time.Second)
	f.report(t, sshReport(attacker, 2)) // coalesced, pending counts are dropped by the revocation
	ind := ipv4(" " + attacker + " ")
	revs, err := f.svc.Revoke(context.Background(), Revocation{Indicator: &ind, Reason: "operator_request"})
	if err != nil || len(revs) != 1 || revs[0].Revokes != v.ID {
		t.Fatalf("Revoke() = %+v, %v", revs, err)
	}
	if _, ok := f.svc.pending[v.Key()]; ok {
		t.Error("pending counts survived the revocation")
	}
	if other, err := f.svc.Lookup(ipv4("85.10.0.8")); err != nil || len(other) != 1 {
		t.Errorf("unrelated verdict = %v, %v", other, err)
	}
}

func TestRevokeErrors(t *testing.T) {
	f := newFixture(t)
	first := f.report(t, sshReport(attacker, 5)).Event
	f.clock.Advance(CoalesceWindow + time.Second)
	f.report(t, sshReport(attacker, 1))

	// A verdict of another publisher, stored as the mesh would.
	otherKey, err := identity.Create(filepath.Join(t.TempDir(), "other"), false)
	if err != nil {
		t.Fatal(err)
	}
	foreign := &obieproto.Event{
		ID: obieproto.NewID(f.clock.Now()), Spec: obieproto.Spec, Type: obieproto.TypeVerdict,
		IssuedAt: obieproto.NewTimestamp(f.clock.Now()), Indicator: obieproto.Indicator{Kind: obieproto.KindIPv4, Value: "85.10.0.9", Scope: "/32"},
		Protocol: "ssh", Evidence: &obieproto.Evidence{Events: 1, Reason: "x"},
		Verdict: &obieproto.Verdict{SuggestedAction: obieproto.ActionBan, Confidence: 1, TTLSeconds: 3600},
	}
	if err := obieproto.SignWith(foreign, otherKey); err != nil {
		t.Fatal(err)
	}
	if ok, err := f.db.Put(foreign); err != nil || !ok {
		t.Fatalf("Put foreign verdict: %v, %v", ok, err)
	}

	foreignInd := foreign.Indicator
	private := ipv4("not an address")
	for name, tc := range map[string]struct {
		r    Revocation
		want error
	}{
		"neither":             {Revocation{Reason: "x"}, ErrInvalid},
		"both":                {Revocation{EventID: first.ID, Indicator: &foreignInd, Reason: "x"}, ErrInvalid},
		"no reason":           {Revocation{EventID: first.ID}, ErrInvalid},
		"bad indicator":       {Revocation{Indicator: &private, Reason: "x"}, ErrInvalid},
		"unknown id":          {Revocation{EventID: obieproto.NewID(f.clock.Now()), Reason: "x"}, ErrNotFound},
		"superseded verdict":  {Revocation{EventID: first.ID, Reason: "x"}, ErrNotFound},
		"foreign verdict":     {Revocation{EventID: foreign.ID, Reason: "x"}, ErrNotFound},
		"foreign indicator":   {Revocation{Indicator: &foreignInd, Reason: "x"}, ErrNotFound},
		"indicator unknown":   {Revocation{Indicator: &obieproto.Indicator{Kind: obieproto.KindIPv4, Value: "85.10.0.99"}, Reason: "x"}, ErrNotFound},
		"invalid reason text": {Revocation{Indicator: &obieproto.Indicator{Kind: obieproto.KindIPv4, Value: attacker}, Reason: "False Positive!"}, ErrInvalid},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := f.svc.Revoke(context.Background(), tc.r); !errors.Is(err, tc.want) {
				t.Errorf("Revoke(%+v) error = %v, want %v", tc.r, err, tc.want)
			}
		})
	}
}

func TestListAndLookup(t *testing.T) {
	f := newFixture(t)
	f.report(t, sshReport(attacker, 5))
	f.report(t, sshReport("85.10.0.8", 1))

	page, err := f.svc.List("", store.Page{Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0].Key != "ipv4:"+attacker || page.Next == "" {
		t.Fatalf("first page = %+v, %v", page, err)
	}
	page, err = f.svc.List(f.key.PeerID(), store.Page{After: page.Next})
	if err != nil || len(page.Items) != 1 || page.Items[0].Key != "ipv4:85.10.0.8" || page.Next != "" {
		t.Errorf("second page = %+v, %v", page, err)
	}
	if page, err := f.svc.List("12D3KooWSomebodyElse", store.Page{}); err != nil || len(page.Items) != 0 {
		t.Errorf("foreign publisher page = %+v, %v", page, err)
	}
	if _, err := f.svc.Lookup(ipv4("85.10")); !errors.Is(err, ErrInvalid) {
		t.Errorf("Lookup of a bad indicator error = %v, want ErrInvalid", err)
	}
}
