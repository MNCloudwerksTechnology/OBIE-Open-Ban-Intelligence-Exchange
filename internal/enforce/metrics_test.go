package enforce

import (
	"context"
	"log/slog"
	"net/netip"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
)

// counters snapshots the event-driven enforcer metrics.
type counters struct {
	success, failure, allowlist, maxEntries float64
	applies                                 uint64
}

func snapshot(t *testing.T) counters {
	t.Helper()
	var m dto.Metric
	if err := applyDuration.Write(&m); err != nil {
		t.Fatal(err)
	}
	return counters{
		success:    testutil.ToFloat64(applyTotal.WithLabelValues(resultSuccess)),
		failure:    testutil.ToFloat64(applyTotal.WithLabelValues(resultError)),
		allowlist:  testutil.ToFloat64(skippedTotal.WithLabelValues(SkipAllowlist)),
		maxEntries: testutil.ToFloat64(skippedTotal.WithLabelValues(SkipMaxEntries)),
		applies:    m.GetHistogram().GetSampleCount(),
	}
}

// since returns the change of every counter from before.
func (c counters) since(before counters) counters {
	return counters{
		success: c.success - before.success, failure: c.failure - before.failure,
		allowlist: c.allowlist - before.allowlist, maxEntries: c.maxEntries - before.maxEntries,
		applies: c.applies - before.applies,
	}
}

func entries(family string) float64 { return testutil.ToFloat64(entriesGauge.WithLabelValues(family)) }

func TestMetricsRegistered(t *testing.T) {
	for _, c := range []prometheus.Collector{nodeMode, entriesGauge, applyTotal, applyDuration, skippedTotal} {
		if err := prometheus.Register(c); err == nil {
			t.Errorf("collector was not registered")
		}
	}
}

func TestNodeModeMetric(t *testing.T) {
	mode := func(m config.Mode) float64 { return testutil.ToFloat64(nodeMode.WithLabelValues(string(m))) }
	g := NewGate(config.ModeEnforce, nil, slog.New(slog.DiscardHandler))
	if mode(config.ModeEnforce) != 1 || mode(config.ModeObserve) != 0 {
		t.Errorf("obie_node_mode after NewGate(enforce): enforce=%v observe=%v", mode(config.ModeEnforce), mode(config.ModeObserve))
	}
	g.SetMode(config.ModeObserve)
	if mode(config.ModeEnforce) != 0 || mode(config.ModeObserve) != 1 {
		t.Errorf("obie_node_mode after SetMode(observe): enforce=%v observe=%v", mode(config.ModeEnforce), mode(config.ModeObserve))
	}
}

// TestReconcileMetrics: every pass is counted by result, calls that apply
// a change are timed, and the applied entries are counted by family.
func TestReconcileMetrics(t *testing.T) {
	enf := newFake()
	f := newFixture(t, config.ModeEnforce, enf, Options{})
	f.add("198.51.100.1", time.Hour, 1)
	f.add("2a00:1450::/32", time.Hour, 1)
	before := snapshot(t)
	f.reconcile(t)
	f.reconcile(t) // nothing to change: counted, not timed
	if got := snapshot(t).since(before); got != (counters{success: 2, applies: 1}) {
		t.Errorf("after two passes: %+v", got)
	}
	if entries(familyIPv4) != 1 || entries(familyIPv6) != 1 {
		t.Errorf("obie_enforcer_entries: ipv4=%v ipv6=%v, want 1 each", entries(familyIPv4), entries(familyIPv6))
	}

	enf.failApply = 1
	f.add("198.51.100.2", time.Hour, 1)
	before = snapshot(t)
	if err := f.rec.Reconcile(context.Background()); err == nil {
		t.Fatal("Reconcile succeeded despite a failing apply")
	}
	if got := snapshot(t).since(before); got != (counters{failure: 1, applies: 1}) {
		t.Errorf("after a failed pass: %+v", got)
	}

	f.gate.SetMode(config.ModeObserve)
	f.reconcile(t)
	if entries(familyIPv4) != 0 || entries(familyIPv6) != 0 {
		t.Errorf("obie_enforcer_entries in observe mode: ipv4=%v ipv6=%v, want 0", entries(familyIPv4), entries(familyIPv6))
	}
}

// TestSkippedMetric: a block is counted once when it becomes skipped, not
// on every pass.
func TestSkippedMetric(t *testing.T) {
	allow := sovereignty.NewAllowlist()
	f := newFixture(t, config.ModeEnforce, newFake(), Options{MaxEntries: 1,
		Allowlist: func() *sovereignty.Allowlist { return allow }})
	f.add("198.51.100.1", time.Hour, 2)
	f.add("198.51.100.2", time.Hour, 1)
	f.add("203.0.113.5", time.Hour, 1)
	allow = sovereignty.NewAllowlist(sovereignty.Entry{Prefix: netip.MustParsePrefix("203.0.113.0/24"), Source: sovereignty.SourceConfig})
	before := snapshot(t)
	f.reconcile(t)
	f.reconcile(t)
	if got := snapshot(t).since(before); got.allowlist != 1 || got.maxEntries != 1 {
		t.Errorf("obie_enforcer_skipped_total rose by allowlist=%v max_entries=%v, want 1 each", got.allowlist, got.maxEntries)
	}
}
