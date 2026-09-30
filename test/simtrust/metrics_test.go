package simtrust

import (
	"math"
	"net/netip"
	"testing"
	"time"
)

// at returns testStart plus h hours and m minutes.
func at(h, m int) time.Time {
	return testStart.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute)
}

// handResult is a run of three hours built by hand: an observer, an
// honest publisher and a naive poisoner; attackers a1 (banned by the
// publisher's operator in hour 0), a2 (the observer's, hour 1) and a3 (the
// poisoner's, hour 2), a CDN edge in a published range and a customer.
func handResult() *Result {
	a1, a2, a3 := netip.MustParseAddr("2001:db8:a::1"), netip.MustParseAddr("2001:db8:a::2"), netip.MustParseAddr("2001:db8:a::3")
	cdn, customer := netip.MustParseAddr("2001:db8:c:1::1"), netip.MustParseAddr("2001:db8:b::1")
	tr := &Trace{
		Start: testStart, Hours: 3,
		Operators: []Operator{{Name: "observer", Bantime: time.Hour}, {Name: "honest", Bantime: time.Hour}, {Name: "naive", Bantime: time.Hour}},
		Addresses: []Address{{a1, ClassAttacker, 1}, {a2, ClassAttacker, 1}, {a3, ClassAttacker, 1}, {cdn, ClassCDN, 2}, {customer, ClassCustomer, 3}},
		Published: []Range{{netip.MustParsePrefix("2001:db8:c:1::/64"), ClassCDN}},
		Observations: []Observation{
			{at(0, 20), 1, a1}, {at(1, 40), 0, a2}, {at(2, 10), 2, a3},
		},
	}
	return &Result{
		Spec:  RunSpec{Model: ModelNaive},
		Trace: tr,
		Cast:  cast{roles: []Role{RoleObserver, RoleHonest, Role(ModelNaive)}, asns: []uint32{1, 1, 1}},
		Episodes: []Episode{
			{Addr: a1, Start: at(0, 30), End: at(1, 30), Contributors: []Contribution{{"p1", "h1", at(0, 30)}}},
			{Addr: customer, Start: at(1, 10), End: at(2, 10), Contributors: []Contribution{{"p1", "h2", at(1, 10)}, {"p2", "m1", at(1, 10)}}},
			{Addr: cdn, Start: at(2, 0), End: at(2, 5), Contributors: []Contribution{{"p2", "h3", at(2, 0)}}},
		},
		keys: []keyRecord{
			{peerID: "p0", actor: 0, role: RoleObserver, joined: testStart},
			{peerID: "p1", actor: 1, role: RoleHonest, joined: testStart},
			{peerID: "p2", actor: 2, role: Role(ModelNaive), joined: testStart, defected: at(1, 0),
				events: []time.Time{at(0, 10), at(1, 0), at(1, 30), at(2, 30)}},
		},
		weights: []weightSample{{at(0, 0), 1}, {at(0, 30), 1}, {at(1, 0), 0.5}, {at(2, 0), 0.5}},
		received: []received{
			{at: at(0, 10), key: 2, addr: customer, confidence: 1, end: at(3, 0)},
			{at: at(0, 40), key: 1, addr: a1, confidence: 0.8, end: at(1, 40)},
			{at: at(1, 40), key: 0, addr: a2, confidence: 0.8, end: at(2, 40)},
		},
		malicious: map[string]bool{"m1": true},
	}
}

func near(a, b float64) bool {
	return (math.IsNaN(a) && math.IsNaN(b)) || math.Abs(a-b) < 1e-9
}

func TestMeasure(t *testing.T) {
	m := Measure(handResult())
	nan := math.NaN()
	for _, tt := range []struct {
		hour   int
		window Window
		metric Metric
		want   float64
	}{
		{0, WindowHour, MetricPrecision, 1},
		{0, WindowHour, MetricRecall, 1},
		{0, WindowHour, MetricF1, 1},
		{1, WindowHour, MetricPrecision, 0.5},
		{1, WindowHour, MetricRecall, 0}, // a2 is not banned
		{1, WindowHour, MetricF1, 0},
		// a3 was banned by the poisoner's operator: an attacker in the
		// network, although its publisher is an adversary.
		{2, WindowHour, MetricPrecision, 0},
		{2, WindowHour, MetricRecall, 0},
		{2, WindowCumulative, MetricPrecision, 1.0 / 3},
		{2, WindowCumulative, MetricRecall, 1.0 / 3},
		{1, WindowHour, MetricFalseBans, 1},
		// The customer is banned from 1:10 to 2:10, the CDN edge from
		// 2:00 to 2:05.
		{1, WindowHour, MetricFalseBanHours, 50.0 / 60},
		{2, WindowHour, MetricFalseBanHours, 15.0 / 60},
		{2, WindowCumulative, MetricFalseBanHours, 65.0 / 60},
		{0, WindowHour, MetricFalseBanHours, 0},
		{2, WindowCumulative, MetricFalseBans, 2},
		{2, WindowCumulative, MetricFalseBansPerVictim, 1}, // 2 of 2 protected addresses
		// Only the customer's ban has a malicious verdict among its
		// contributors; the CDN edge's is the poisoner's camouflage.
		{1, WindowHour, MetricFalseBansBeforeNeutralization, 1},
		{2, WindowHour, MetricFalseBansBeforeNeutralization, 0},
		{0, WindowCumulative, MetricNeutralizationHours, nan},
		{1, WindowHour, MetricNeutralizationHours, nan},
		{1, WindowCumulative, MetricNeutralizationHours, 1},
		{1, WindowCumulative, MetricNeutralizationEvents, 2},
		{2, WindowCumulative, MetricNeutralizationHours, 2},
		{2, WindowCumulative, MetricNeutralizedShare, 0},
		{0, WindowHour, MetricHonestWeight, 1},
		{1, WindowHour, MetricHonestWeight, 0.5},
		{1, WindowCumulative, MetricHonestWeight, 2.5 / 3},
		{2, WindowCumulative, MetricNewcomerConvergenceHours, nan},
		{2, WindowCumulative, MetricWhitewashPayoff, nan},
		// Hour 0: confidence 1 on a customer and 0.8 on an attacker.
		{0, WindowHour, MetricBrier, (1 + 0.04) / 2},
		{0, WindowHour, MetricECE, 0.5*1 + 0.5*0.2},
		{2, WindowHour, MetricBrier, nan},
	} {
		if got := m.Hours[tt.hour][tt.window][tt.metric]; !near(got, tt.want) {
			t.Errorf("hour %d, %s, %s = %v, want %v", tt.hour, tt.window, tt.metric, got, tt.want)
		}
	}
	// a1: one honest remote, banned; a2: the observer's own, not banned;
	// a3: nobody reported it.
	want := Corroboration{}
	want[1][0].Attackers, want[1][0].Banned = 1, 1
	want[0][1].Attackers = 1
	want[0][0].Attackers = 1
	if m.Corroboration != want {
		t.Errorf("corroboration = %+v, want %+v", m.Corroboration, want)
	}
	if f := m.Feeds[Role(ModelNaive)]; f.Volume != 1 || f.Exclusive != 1 || f.Bound != 1 || f.Accuracy != 0 || !math.IsNaN(f.LatencyMinutes) {
		t.Errorf("poisoner's feed = %+v, want one exclusive customer", f)
	}
}

func TestNeutralizationAndPayoff(t *testing.T) {
	r := handResult()
	r.Spec.Model = ModelWhitewash
	r.Cast.roles[2] = Role(ModelWhitewash)
	r.keys[2].role = Role(ModelWhitewash)
	r.keys[2].neutralized = at(1, 30)
	r.keys[2].burned = at(1, 30)
	m := Measure(r)
	for _, tt := range []struct {
		hour   int
		metric Metric
		want   float64
	}{
		{1, MetricNeutralizedShare, 1},
		{1, MetricNeutralizationHours, 0.5},
		{1, MetricNeutralizationEvents, 1},
		// The customer's ban came before the neutralization, from one
		// burned key.
		{2, MetricWhitewashPayoff, 1},
	} {
		if got := m.Hours[tt.hour][WindowCumulative][tt.metric]; !near(got, tt.want) {
			t.Errorf("hour %d, %s = %v, want %v", tt.hour, tt.metric, got, tt.want)
		}
	}
	// A contribution after the neutralization is not caused before it.
	r.Episodes[1].Contributors[1].At = at(1, 45)
	if got := Measure(r).Hours[1][WindowHour][MetricFalseBansBeforeNeutralization]; got != 0 {
		t.Errorf("false bans before neutralization = %v, want 0", got)
	}
}

// TestCausedWhenThePoisonCounts checks that a false ban that poison joins
// later is caused in the hour the poison counted, not when the ban began.
func TestCausedWhenThePoisonCounts(t *testing.T) {
	r := handResult()
	r.Episodes[1].Start, r.Episodes[1].Contributors[0].At = at(0, 5), at(0, 5)
	r.Episodes[1].Contributors[1].At = at(2, 20)
	m := Measure(r)
	if h0, h2 := m.Hours[0][WindowHour][MetricFalseBansBeforeNeutralization], m.Hours[2][WindowHour][MetricFalseBansBeforeNeutralization]; h0 != 0 || h2 != 1 {
		t.Errorf("caused false bans in hours 0 and 2: %v and %v, want 0 and 1", h0, h2)
	}
	if got := m.Hours[0][WindowHour][MetricFalseBans]; got != 1 {
		t.Errorf("false bans in hour 0 = %v, want 1: the ban began then", got)
	}
}

func TestNewcomerConvergence(t *testing.T) {
	r := handResult()
	r.Cast.roles[1] = RoleNewcomer
	r.keys[1].role = RoleNewcomer
	r.keys[1].joined = at(1, 0)
	r.keys[1].converged = at(1, 15)
	m := Measure(r)
	if got := m.Hours[0][WindowCumulative][MetricNewcomerConvergenceHours]; !math.IsNaN(got) {
		t.Errorf("convergence before joining = %v, want none", got)
	}
	if got := m.Hours[2][WindowCumulative][MetricNewcomerConvergenceHours]; !near(got, 0.25) {
		t.Errorf("convergence = %v h, want 0.25", got)
	}
	// Its operator's ban of a1 came before it joined.
	if got := m.Hours[0][WindowHour][MetricRecall]; !math.IsNaN(got) {
		t.Errorf("recall in hour 0 = %v, want none: no attacker in the network", got)
	}
}

func TestMaxConcurrent(t *testing.T) {
	iv := func(from, to int) interval { return interval{at(0, from), at(0, to)} }
	for _, tt := range []struct {
		byKeys map[int][]interval
		want   int
	}{
		{map[int][]interval{1: {iv(0, 10)}, 2: {iv(5, 15)}, 3: {iv(10, 20)}}, 2},
		// One key's overlapping verdicts count once.
		{map[int][]interval{1: {iv(0, 10), iv(5, 20)}, 2: {iv(6, 7)}}, 2},
		{map[int][]interval{1: {iv(0, 30)}, 2: {iv(1, 29)}, 3: {iv(2, 28)}, 4: {iv(40, 50)}}, 3},
		{map[int][]interval{1: {iv(5, 5)}}, 0},
	} {
		if got := maxConcurrent(tt.byKeys); got != tt.want {
			t.Errorf("maxConcurrent(%v) = %d, want %d", tt.byKeys, got, tt.want)
		}
	}
}

func TestMedianAndCount(t *testing.T) {
	if got := median([]float64{3, 1, 2, 10}); got != 2.5 {
		t.Errorf("median = %v, want 2.5", got)
	}
	if got := median(nil); !math.IsNaN(got) {
		t.Errorf("median of nothing = %v, want NaN", got)
	}
	times := []time.Time{at(0, 1), at(0, 2), at(0, 2), at(0, 5)}
	if got := countIn(times, at(0, 2), at(0, 5)); got != 2 {
		t.Errorf("countIn = %d, want 2", got)
	}
}
