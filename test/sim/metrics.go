package sim

import (
	"math"
	"slices"
	"strconv"
	"time"
)

// Metric names of a Result, in report order.
const (
	mDelivery    = "delivery_ratio"
	mP50         = "latency_p50_ms"
	mP99         = "latency_p99_ms"
	mMax         = "latency_max_ms"
	mDuplicates  = "duplicate_factor"
	mHops        = "hops_mean"
	mSybilShare  = "sybil_mesh_share"
	mRecovery    = "mesh_recovery_s"
	mRecovered   = "mesh_recovered"
	mRetained    = "trusted_retained"
	mMeshDegree  = "mesh_degree"
	windowPrefix = "delivery_ratio:"
)

// Loss causes besides the outcomes of a first copy (ADR 0033).
const (
	causeNeverReceived = "never_received"
	causeOffline       = "offline"
	causePreempted     = "preempted"
)

// hopBins are the hop counts the report lists; longer paths count in the
// last bin.
const hopBins = 12

// Result is what one run of a scenario measured.
type Result struct {
	Scenario string `json:"scenario"`
	Variant  string `json:"variant"`
	Seed     int64  `json:"seed"`
	// Version is the OBIE version that produced the result (git describe).
	Version string `json:"version"`
	// Metrics are the run's values by metric name; a metric the run does
	// not measure is absent.
	Metrics map[string]float64 `json:"metrics"`
	// Hops is the share of delivered pairs by hop count ("1" … "12+"), and
	// "unknown" for paths through an untraced node.
	Hops map[string]float64 `json:"hops"`
	// Loss is the share of all pairs lost, by cause.
	Loss map[string]float64 `json:"loss"`
	// Honest, Adversaries, Links and Events describe the network.
	Honest, Adversaries, Links, Events int
	// WallSeconds is how long the run took in real time.
	WallSeconds float64 `json:"wall_seconds"`
}

// measure computes the Result of the run from its trace and mesh samples.
func (w *world) measure() *Result {
	r := &Result{Scenario: w.sc.Name, Variant: w.variant.Name, Seed: w.seed,
		Metrics: map[string]float64{}, Hops: map[string]float64{}, Loss: map[string]float64{},
		Links: len(w.links), Events: len(w.events)}
	for _, n := range w.nodes {
		if n.honest {
			r.Honest++
		} else {
			r.Adversaries++
		}
	}
	j := join(w)
	eligible := w.eligible()
	w.measureDelivery(r, j, eligible)
	for _, win := range w.windows {
		if ratio, ok := w.deliveryIn(j, eligible, win); ok {
			r.Metrics[windowPrefix+win.name] = ratio
		}
	}
	w.measureMesh(r)
	if w.trusted.to > 0 {
		if v, ok := w.retained(j); ok {
			r.Metrics[mRetained] = v
		}
	}
	return r
}

// eligible are the honest nodes running at the end: the nodes an event
// should reach.
func (w *world) eligible() []int32 {
	var out []int32
	for _, n := range w.nodes {
		if n.honest && n.up() {
			out = append(out, n.idx)
		}
	}
	return out
}

// measureDelivery fills in delivery, latency, duplicates, hops and loss
// for the events published before publishing stopped.
func (w *world) measureDelivery(r *Result, j *joined, eligible []int32) {
	var pairs, delivered, copies, unknownHops int
	hopCount := make([]int, hopBins+1)
	var latencies []time.Duration
	var hopSum, hopN int
	loss := map[string]int{}
	for e, ev := range w.events {
		if ev.at >= w.end-drain {
			continue
		}
		hops := j.hops(e)
		for _, n := range eligible {
			if n == ev.publisher {
				continue
			}
			pairs++
			d := j.d[e][n]
			if d.parent < 0 {
				loss[w.cause(n, ev, d)]++
				continue
			}
			delivered++
			copies += int(d.copies)
			latencies = append(latencies, d.at-ev.at)
			switch h := hops[n]; {
			case h < 1:
				unknownHops++
			default:
				hopCount[min(h, hopBins)]++
				hopSum += h
				hopN++
			}
		}
	}
	if pairs == 0 {
		return
	}
	r.Metrics[mDelivery] = float64(delivered) / float64(pairs)
	for cause, k := range loss {
		r.Loss[cause] = float64(k) / float64(pairs)
	}
	if delivered == 0 {
		return
	}
	slices.Sort(latencies)
	r.Metrics[mP50] = millis(percentile(latencies, 0.50))
	r.Metrics[mP99] = millis(percentile(latencies, 0.99))
	r.Metrics[mMax] = millis(latencies[len(latencies)-1])
	r.Metrics[mDuplicates] = float64(copies) / float64(delivered)
	if hopN > 0 {
		r.Metrics[mHops] = float64(hopSum) / float64(hopN)
	}
	for h := 1; h <= hopBins; h++ {
		label := strconv.Itoa(h)
		if h == hopBins {
			label += "+"
		}
		r.Hops[label] = float64(hopCount[h]) / float64(delivered)
	}
	r.Hops["unknown"] = float64(unknownHops) / float64(delivered)
}

// cause says why node n did not get the event: the outcome of the first
// copy it validated, since GossipSub then drops every later copy, or why
// no copy came.
func (w *world) cause(n int32, ev *event, d delivery) string {
	switch d.first {
	case none:
		if w.nodes[n].offlineAt(ev.at) {
			return causeOffline
		}
		return causeNeverReceived
	case invalidSignature, invalidSchema:
		// The event is valid: an invalid first copy was forged.
		return causePreempted
	default:
		return d.first.String()
	}
}

// offlineAt reports whether the node was down at t or in the minute after
// it, when the event spread.
func (n *node) offlineAt(t time.Duration) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, o := range n.offline {
		to := time.Duration(o[1])
		if o[1] == 0 {
			to = math.MaxInt64
		}
		if time.Duration(o[0]) <= t+time.Minute && t < to {
			return true
		}
	}
	return false
}

// deliveryIn returns the delivery ratio of the events published in the
// window, to the watched nodes only if the window says so.
func (w *world) deliveryIn(j *joined, eligible []int32, win window) (float64, bool) {
	nodes := eligible
	if win.watched {
		nodes = w.watched
	}
	var pairs, delivered int
	for e, ev := range w.events {
		if ev.at < win.from || ev.at >= win.to || (win.chosen && !ev.chosen) {
			continue
		}
		for _, n := range nodes {
			if n == ev.publisher || !w.nodes[n].honest {
				continue
			}
			pairs++
			if j.d[e][n].parent >= 0 {
				delivered++
			}
		}
	}
	if pairs == 0 {
		return 0, false
	}
	return float64(delivered) / float64(pairs), true
}

// measureMesh fills in the Sybil share of mesh slots, the mean mesh
// degree at the end and the mesh recovery time.
func (w *world) measureMesh(r *Result) {
	if len(w.samples) == 0 {
		return
	}
	if running := len(w.eligible()); running > 0 {
		r.Metrics[mMeshDegree] = float64(w.samples[len(w.samples)-1].slots) / float64(running)
	}
	if w.attackFrom >= 0 {
		var sum float64
		var k int
		for _, s := range w.samples {
			if s.at >= w.attackFrom && s.slots > 0 {
				sum += float64(s.sybil) / float64(s.slots)
				k++
			}
		}
		if k > 0 {
			r.Metrics[mSybilShare] = sum / float64(k)
		}
	}
	if w.disruption.kind == "" {
		return
	}
	healthy := w.healthy()
	var since time.Duration = -1
	last := w.samples[len(w.samples)-1].at
	for _, s := range w.samples {
		if s.at < w.disruption.at {
			continue
		}
		switch {
		case !healthy(s):
			since = -1
		case since < 0:
			since = s.at
		}
	}
	if since < 0 {
		r.Metrics[mRecovered] = 0
		r.Metrics[mRecovery] = (last - w.disruption.at).Seconds()
		return
	}
	r.Metrics[mRecovered] = 1
	r.Metrics[mRecovery] = (since - w.disruption.at).Seconds()
}

// sybilHealthy is the largest Sybil share of mesh slots a recovered mesh
// has; outageHealthy the share of its slots before an outage a recovered
// mesh has again.
const (
	sybilHealthy  = 0.10
	outageHealthy = 0.90
)

// healthy returns the test of a recovered mesh sample.
func (w *world) healthy() func(sample) bool {
	if w.disruption.kind == "attack" {
		return func(s sample) bool { return s.slots > 0 && float64(s.sybil) <= sybilHealthy*float64(s.slots) }
	}
	var sum, k float64
	for _, s := range w.samples {
		if s.at >= w.disruption.baseline[0] && s.at < w.disruption.baseline[1] {
			sum += float64(s.watched)
			k++
		}
	}
	base := sum / max(k, 1)
	return func(s sample) bool { return float64(s.watched) >= outageHealthy*base }
}

// retained returns the share of the trusted verdicts in the trusted window
// that the honest nodes accepted and still hold in their stores.
func (w *world) retained(j *joined) (float64, bool) {
	var held, got int
	for e, ev := range w.events {
		if ev.at < w.trusted.from || ev.at >= w.trusted.to {
			continue
		}
		for _, n := range w.nodes {
			if !n.honest || n.db == nil || j.d[e][n.idx].parent < 0 {
				continue
			}
			got++
			if _, err := n.db.Get(ev.ev.ID); err == nil {
				held++
			}
		}
	}
	if got == 0 {
		return 0, false
	}
	return float64(held) / float64(got), true
}

// percentile returns the p-quantile of the sorted durations (nearest rank).
func percentile(sorted []time.Duration, p float64) time.Duration {
	i := int(math.Ceil(p*float64(len(sorted)))) - 1
	return sorted[min(max(i, 0), len(sorted)-1)]
}

func millis(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
