package sim

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/MNCloudwerksTechnology/obie/internal/store"
)

// fakeWorld returns a world of honest running nodes and adversaries that
// never ran, for measuring hand-made traces; its run ends at 100 s.
func fakeWorld(t *testing.T, honest, adversaries int) *world {
	t.Helper()
	w := newWorld(&Scenario{Name: "fake"}, variants()[V01], 1)
	for i := range honest + adversaries {
		id := testIdentity(t, byte(i+1))
		pid, err := peer.Decode(id.PeerID())
		if err != nil {
			t.Fatal(err)
		}
		idx := int32(i) // #nosec G115 -- a handful of nodes.
		w.nodes = append(w.nodes, &node{idx: idx, id: id, pid: pid, honest: i < honest, running: i < honest})
		w.byPeer[pid] = idx
	}
	w.end = 100 * time.Second
	return w
}

// fakeEvent adds an event of the publisher at t to w.
func fakeEvent(t *testing.T, w *world, publisher int32, at time.Duration) int32 {
	t.Helper()
	ev := newVerdict(w.nodes[publisher].id, w.start.Add(at), honestTTL, 9, uint64(len(w.events))) // #nosec G115 -- small.
	return w.addEvent(&event{ev: ev, publisher: publisher, at: at})
}

func rec(w *world, n int32, at time.Duration, e, from int32, o outcome) {
	w.nodes[n].trace.add(record{at: at, event: e, from: from, outcome: o})
}

func ms(v float64) time.Duration { return time.Duration(v * float64(time.Millisecond)) }

// traceWorld is 4 honest nodes (0–3) and an adversary (4) with two events:
//
//   - e0 by 0 at 10 s: 1 accepts it from 0 after 50 ms and gets a
//     duplicate from 2; 2 accepts it from 1 after 100 ms; 3 first gets a
//     forgery from 4, so the copy from 2 is a duplicate: lost, preempted.
//   - e1 by 1 at 20 s: the adversary relays it to 2, which accepts it
//     after 30 ms; 0 accepts it from 2 after 50 ms; 3 gets no copy.
//   - e2 by 0 at 80 s, after publishing stopped: not measured.
func traceWorld(t *testing.T) *world {
	w := fakeWorld(t, 4, 1)
	e0 := fakeEvent(t, w, 0, 10*time.Second)
	rec(w, 0, 10*time.Second, e0, 0, published)
	rec(w, 1, 10*time.Second+ms(50), e0, 0, accepted)
	rec(w, 1, 10*time.Second+ms(200), e0, 2, duplicate)
	rec(w, 2, 10*time.Second+ms(100), e0, 1, accepted)
	rec(w, 3, 10*time.Second+ms(20), e0, 4, invalidSignature)
	rec(w, 3, 10*time.Second+ms(150), e0, 2, duplicate)
	rec(w, 4, 10*time.Second+ms(10), e0, 0, relayed)

	e1 := fakeEvent(t, w, 1, 20*time.Second)
	rec(w, 1, 20*time.Second, e1, 1, published)
	rec(w, 4, 20*time.Second+ms(10), e1, 1, relayed)
	rec(w, 2, 20*time.Second+ms(30), e1, 4, accepted)
	rec(w, 0, 20*time.Second+ms(50), e1, 2, accepted)

	e2 := fakeEvent(t, w, 0, 80*time.Second)
	rec(w, 0, 80*time.Second, e2, 0, published)
	return w
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestHops(t *testing.T) {
	w := traceWorld(t)
	j := join(w)
	for e, want := range [][]int{{0, 1, 2, -1, 1}, {3, 0, 2, -1, 1}} {
		got := j.hops(e)
		for n := range want {
			if got[n] != want[n] {
				t.Errorf("event %d: hops = %v, want %v", e, got, want)
				break
			}
		}
	}
}

func TestHopsCycle(t *testing.T) {
	w := fakeWorld(t, 3, 0)
	e := fakeEvent(t, w, 0, time.Second)
	rec(w, 0, time.Second, e, 0, published)
	// 1 and 2 claim each other as the source: the path is not traced.
	rec(w, 1, 2*time.Second, e, 2, accepted)
	rec(w, 2, 2*time.Second, e, 1, accepted)
	if got := join(w).hops(int(e)); got[0] != 0 || got[1] != -1 || got[2] != -1 {
		t.Errorf("hops = %v, want [0 -1 -1]", got)
	}
}

func TestMeasureDelivery(t *testing.T) {
	w := traceWorld(t)
	r := &Result{Metrics: map[string]float64{}, Hops: map[string]float64{}, Loss: map[string]float64{}}
	w.measureDelivery(r, join(w), w.eligible())
	for name, want := range map[string]float64{
		mDelivery: 4.0 / 6, mP50: 50, mP99: 100, mMax: 100, mDuplicates: 5.0 / 4, mHops: 2,
	} {
		if got := r.Metrics[name]; !near(got, want) {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	for bin, want := range map[string]float64{"1": 0.25, "2": 0.5, "3": 0.25, "4": 0, "unknown": 0} {
		if got := r.Hops[bin]; !near(got, want) {
			t.Errorf("hops %s = %v, want %v", bin, got, want)
		}
	}
	if len(r.Loss) != 2 || !near(r.Loss[causePreempted], 1.0/6) || !near(r.Loss[causeNeverReceived], 1.0/6) {
		t.Errorf("loss = %v, want preempted and never_received 1/6 each", r.Loss)
	}
}

// TestCauseIsTheValidatedCopy: copies that arrive at the same instant are
// validated in parallel, so the duplicate of a forgery can be recorded
// before it; the loss is still the forgery's.
func TestCauseIsTheValidatedCopy(t *testing.T) {
	w := fakeWorld(t, 2, 1)
	e := fakeEvent(t, w, 0, 10*time.Second)
	rec(w, 0, 10*time.Second, e, 0, published)
	rec(w, 1, 10*time.Second+ms(10), e, 2, duplicate)
	rec(w, 1, 10*time.Second+ms(10), e, 2, invalidSignature)
	rec(w, 1, 10*time.Second+ms(90), e, 0, duplicate)
	r := &Result{Metrics: map[string]float64{}, Hops: map[string]float64{}, Loss: map[string]float64{}}
	w.measureDelivery(r, join(w), w.eligible())
	if len(r.Loss) != 1 || r.Loss[causePreempted] != 1 {
		t.Errorf("loss %v, want all preempted", r.Loss)
	}
}

// TestCauseSkipsAFullQueue: GossipSub forgets a copy it dropped from a
// full validation queue, so a loss is the next copy's; only a node that got
// nothing else lost the event to the queue.
func TestCauseSkipsAFullQueue(t *testing.T) {
	w := fakeWorld(t, 3, 0)
	e := fakeEvent(t, w, 0, 10*time.Second)
	rec(w, 0, 10*time.Second, e, 0, published)
	rec(w, 1, 10*time.Second+ms(10), e, 0, queueFull)
	rec(w, 1, 10*time.Second+ms(90), e, 2, rateLimited)
	rec(w, 2, 10*time.Second+ms(10), e, 0, queueFull)
	r := &Result{Metrics: map[string]float64{}, Hops: map[string]float64{}, Loss: map[string]float64{}}
	w.measureDelivery(r, join(w), w.eligible())
	if len(r.Loss) != 2 || r.Loss["rate_limited"] != 0.5 || r.Loss["queue_full"] != 0.5 {
		t.Errorf("loss %v, want half rate_limited and half queue_full", r.Loss)
	}
}

// TestRetainedLeavesOutThePublisher: a store never evicts its own node's
// verdicts, so only the other nodes' stores show what a flood evicted.
func TestRetainedLeavesOutThePublisher(t *testing.T) {
	w := fakeWorld(t, 3, 0)
	w.useRealStores(store.Options{})
	for _, n := range w.nodes {
		if err := n.db.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = n.db.Stop(context.Background()) })
	}
	e := fakeEvent(t, w, 0, 10*time.Second)
	rec(w, 0, 10*time.Second, e, 0, published)
	rec(w, 1, 10*time.Second+ms(50), e, 0, accepted)
	rec(w, 2, 10*time.Second+ms(60), e, 0, accepted)
	// The publisher and node 1 hold the verdict; node 2 lost it.
	for _, n := range w.nodes[:2] {
		if _, err := n.db.Put(w.events[e].ev); err != nil {
			t.Fatal(err)
		}
	}
	w.trusted = window{from: 0, to: 20 * time.Second}
	if got, ok := w.retained(join(w)); !ok || got != 0.5 {
		t.Errorf("retained %v, %v; want 0.5 of the two nodes that accepted it", got, ok)
	}
}

func TestLossWhileOffline(t *testing.T) {
	w := fakeWorld(t, 2, 0)
	e := fakeEvent(t, w, 0, 10*time.Second)
	rec(w, 0, 10*time.Second, e, 0, published)
	// Node 1 was down from 5 s to 40 s.
	w.nodes[1].offline = [][2]int64{{int64(5 * time.Second), int64(40 * time.Second)}}
	r := &Result{Metrics: map[string]float64{}, Hops: map[string]float64{}, Loss: map[string]float64{}}
	w.measureDelivery(r, join(w), w.eligible())
	if r.Metrics[mDelivery] != 0 || r.Loss[causeOffline] != 1 {
		t.Errorf("delivery %v, loss %v; want 0 and all offline", r.Metrics[mDelivery], r.Loss)
	}
}

func TestOfflineAt(t *testing.T) {
	n := &node{offline: [][2]int64{{int64(100 * time.Second), int64(200 * time.Second)}, {int64(300 * time.Second), 0}}}
	for _, tc := range []struct {
		at   time.Duration
		want bool
	}{
		{30 * time.Second, false},  // more than a minute before the outage
		{50 * time.Second, true},   // it went down while the event spread
		{150 * time.Second, true},  // down
		{200 * time.Second, false}, // back up
		{400 * time.Second, true},  // down until the end
	} {
		if got := n.offlineAt(tc.at); got != tc.want {
			t.Errorf("offlineAt(%v) = %v, want %v", tc.at, got, tc.want)
		}
	}
}

func TestDeliveryInWindow(t *testing.T) {
	w := traceWorld(t)
	j := join(w)
	if got, ok := w.deliveryIn(j, w.eligible(), window{from: 0, to: 15 * time.Second}); !ok || !near(got, 2.0/3) {
		t.Errorf("delivery of e0 = %v, %v; want 2/3", got, ok)
	}
	if _, ok := w.deliveryIn(j, w.eligible(), window{from: 90 * time.Second, to: 100 * time.Second}); ok {
		t.Error("a window without events has a delivery ratio")
	}
	w.events[1].chosen = true
	if got, ok := w.deliveryIn(j, w.eligible(), window{to: 100 * time.Second, chosen: true}); !ok || !near(got, 2.0/3) {
		t.Errorf("delivery of the chosen event e1 = %v, %v; want 2/3", got, ok)
	}
}

func sampleEvery(shares []float64, watched []int) []sample {
	out := make([]sample, 0, len(shares)+len(watched))
	for i, s := range shares {
		out = append(out, sample{at: time.Duration(i+1) * time.Second, slots: 100, sybil: int(s * 100)})
	}
	for i, v := range watched {
		out = append(out, sample{at: time.Duration(i+1) * time.Second, slots: 100, watched: v})
	}
	return out
}

func TestMeshRecoveryAfterAttack(t *testing.T) {
	w := fakeWorld(t, 2, 0)
	w.attackFrom = 2 * time.Second
	w.disruption = disruption{kind: "attack", at: 2 * time.Second}
	// The share rises, falls to 10 % at 5 s, spikes at 6 s, and stays low
	// from 7 s.
	w.samples = sampleEvery([]float64{0, 0.6, 0.4, 0.2, 0.1, 0.3, 0.05, 0.05, 0.1, 0}, nil)
	r := &Result{Metrics: map[string]float64{}}
	w.measureMesh(r)
	if r.Metrics[mRecovered] != 1 || r.Metrics[mRecovery] != 5 {
		t.Errorf("recovered %v after %v s, want 1 after 5 s", r.Metrics[mRecovered], r.Metrics[mRecovery])
	}
	if want := (0.6 + 0.4 + 0.2 + 0.1 + 0.3 + 0.05 + 0.05 + 0.1 + 0) / 9; !near(r.Metrics[mSybilShare], want) {
		t.Errorf("Sybil share %v, want %v", r.Metrics[mSybilShare], want)
	}
	if r.Metrics[mMeshDegree] != 50 {
		t.Errorf("mesh degree %v, want 100 slots over 2 nodes", r.Metrics[mMeshDegree])
	}
}

func TestMeshNotRecovered(t *testing.T) {
	w := fakeWorld(t, 1, 0)
	w.attackFrom = 0
	w.disruption = disruption{kind: "attack"}
	w.samples = sampleEvery([]float64{0.5, 0.5, 0.05, 0.5}, nil)
	r := &Result{Metrics: map[string]float64{}}
	w.measureMesh(r)
	if r.Metrics[mRecovered] != 0 || r.Metrics[mRecovery] != 4 {
		t.Errorf("recovered %v after %v s, want 0 after the whole run (4 s)", r.Metrics[mRecovered], r.Metrics[mRecovery])
	}
}

func TestMeshRecoveryAfterOutage(t *testing.T) {
	w := fakeWorld(t, 1, 0)
	w.disruption = disruption{kind: "outage", at: 4 * time.Second, baseline: [2]time.Duration{time.Second, 4 * time.Second}}
	// 60 watched slots before the outage (1–3 s), none after it, 54 (90 %)
	// again from 7 s.
	w.samples = sampleEvery(nil, []int{60, 60, 60, 0, 10, 50, 54, 58, 60})
	r := &Result{Metrics: map[string]float64{}}
	w.measureMesh(r)
	if r.Metrics[mRecovered] != 1 || r.Metrics[mRecovery] != 3 {
		t.Errorf("recovered %v after %v s, want 1 after 3 s", r.Metrics[mRecovered], r.Metrics[mRecovery])
	}
	if _, ok := r.Metrics[mSybilShare]; ok {
		t.Error("an outage without attack has a Sybil share")
	}
}

func TestPercentile(t *testing.T) {
	d := []time.Duration{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	for p, want := range map[float64]time.Duration{0.5: 5, 0.99: 10, 0.1: 1, 0: 1, 1: 10} {
		if got := percentile(d, p); got != want {
			t.Errorf("percentile(1..10, %v) = %v, want %v", p, got, want)
		}
	}
}

func TestWriteTrace(t *testing.T) {
	w := traceWorld(t)
	var buf bytes.Buffer
	if err := writeTrace(&buf, w); err != nil {
		t.Fatal(err)
	}
	var recs []Record
	sc := bufio.NewScanner(&buf)
	for sc.Scan() {
		var r Record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("line %q: %v", sc.Text(), err)
		}
		recs = append(recs, r)
	}
	if len(recs) != 12 {
		t.Fatalf("%d records, want 12", len(recs))
	}
	// The first line is node 0 publishing e0: its own peer is the source.
	first := recs[0]
	if first.Node != w.nodes[0].pid.String() || first.From != first.Node || first.Event != w.events[0].ev.ID ||
		first.Outcome != "published" || !first.At.Equal(w.start.Add(10*time.Second).UTC()) {
		t.Errorf("first record %+v, want node 0 publishing e0 at 10 s", first)
	}
	var forged int
	for _, r := range recs {
		if r.Outcome == "invalid_signature" {
			forged++
			if r.From != w.nodes[4].pid.String() {
				t.Errorf("the forgery came from %s, want the adversary", r.From)
			}
		}
	}
	if forged != 1 {
		t.Errorf("%d forged copies in the trace, want 1", forged)
	}
}
