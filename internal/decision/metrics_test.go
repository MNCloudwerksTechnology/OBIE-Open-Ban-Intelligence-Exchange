package decision

import (
	"net/netip"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
)

func TestMetricsRegistered(t *testing.T) {
	for _, c := range []prometheus.Collector{decisionsGauge, activeIndicators, activeVerdicts} {
		if err := prometheus.Register(c); err == nil {
			t.Errorf("collector was not registered")
		}
	}
}

// wantGauges checks the decision and store gauges.
func wantGauges(t *testing.T, block, none, allowed, indicators, verdicts float64) {
	t.Helper()
	got := func(s State) float64 { return testutil.ToFloat64(decisionsGauge.WithLabelValues(string(s))) }
	if got(StateBlock) != block || got(StateNone) != none || got(StateAllowed) != allowed {
		t.Errorf("obie_decisions: block=%v none=%v allowed=%v, want %v %v %v",
			got(StateBlock), got(StateNone), got(StateAllowed), block, none, allowed)
	}
	if i, v := testutil.ToFloat64(activeIndicators), testutil.ToFloat64(activeVerdicts); i != indicators || v != verdicts {
		t.Errorf("obie_store_active_indicators=%v obie_store_active_verdicts=%v, want %v %v", i, v, indicators, verdicts)
	}
}

// wantCounts checks Engine.Counts against the gauges' values.
func wantCounts(t *testing.T, e *Engine, block, none, allowed, indicators, verdicts int) {
	t.Helper()
	got := e.Counts()
	if got.Decisions[StateBlock] != block || got.Decisions[StateNone] != none || got.Decisions[StateAllowed] != allowed ||
		got.Indicators != indicators || got.Verdicts != verdicts {
		t.Errorf("Counts() = %+v, want block=%d none=%d allowed=%d indicators=%d verdicts=%d",
			got, block, none, allowed, indicators, verdicts)
	}
}

// TestEngineMetrics: the gauges and the counts follow the kept decisions
// at start and after every evaluation.
func TestEngineMetrics(t *testing.T) {
	f := newFixture(t, testPolicy())
	f.engine = New(f.store, testPolicy(), discardLogger(), Options{Now: f.clock.Now, RefreshInterval: time.Hour,
		Allowlist: sovereignty.NewAllowlist(sovereignty.Entry{Prefix: netip.MustParsePrefix("198.51.100.20/32"), Source: sovereignty.SourceConfig})})
	blocked, watched, allowed := ipv4("203.0.113.1"), ipv4("203.0.113.2"), ipv4("198.51.100.20")
	f.put(t, blocked, pubA, 1, time.Hour)
	f.put(t, blocked, pubB, 1, time.Hour)
	f.put(t, watched, pubA, 1, time.Hour)
	wantCounts(t, f.engine, 0, 0, 0, 0, 0)
	f.start(t)
	wantGauges(t, 1, 1, 0, 2, 3)
	wantCounts(t, f.engine, 1, 1, 0, 2, 3)

	f.put(t, allowed, pubA, 1, time.Hour)
	v := f.put(t, allowed, pubB, 1, time.Hour)
	f.engine.processDirty()
	wantGauges(t, 1, 1, 1, 3, 5)
	wantCounts(t, f.engine, 1, 1, 1, 3, 5)
	if got := f.engine.Detail(); got != "1 blocked of 3 indicators" {
		t.Errorf("Detail() = %q", got)
	}

	f.revoke(t, v)
	f.engine.processDirty()
	wantGauges(t, 1, 1, 1, 3, 4)
	wantCounts(t, f.engine, 1, 1, 1, 3, 4)

	// The counts are a copy.
	f.engine.Counts().Decisions[StateBlock] = 99
	wantCounts(t, f.engine, 1, 1, 1, 3, 4)
}
