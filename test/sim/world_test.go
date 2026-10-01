package sim

import (
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/eventtrace"
)

// smallScenario is n honest nodes on a random regular graph of the degree,
// 10 % of them publishing verdicts at 1/s from 10 s until publishing
// stops; extra adds to it.
func smallScenario(n, degree int, end time.Duration, extra func(w *world, honest []int32) error) *Scenario {
	return &Scenario{Name: "small", Variants: []string{V01}, Honest: n, build: func(w *world) error {
		idx, err := w.addNodes(n, true)
		if err != nil {
			return err
		}
		g, err := randomRegular(w.rng, n, degree)
		if err != nil {
			return err
		}
		w.setGraph(idx, g)
		w.end = end
		w.verdicts(w.choosePublishers(publisherShare), poisson(w.rng, 10*time.Second, end-drain, 1))
		if extra != nil {
			return extra(w, idx)
		}
		return nil
	}}
}

// runSmall runs sc in the variant in a bubble of its own and returns the
// world and its result.
func runSmall(t *testing.T, sc *Scenario, variant string, seed int64) (*world, *Result) {
	t.Helper()
	var w *world
	var res *Result
	synctest.Test(t, func(t *testing.T) {
		w = newWorld(sc, variants()[variant], seed)
		var err error
		if res, err = w.run(); err != nil {
			t.Fatal(err)
		}
	})
	if res == nil {
		t.FailNow()
	}
	return w, res
}

// TestVariantsDeliverWithoutAttack: every router variant delivers every
// verdict of a small network without adversaries, and every path is
// traced back to its publisher.
func TestVariantsDeliverWithoutAttack(t *testing.T) {
	for _, v := range []string{V01, Plain, Paper} {
		t.Run(v, func(t *testing.T) {
			_, res := runSmall(t, smallScenario(16, 4, 70*time.Second, nil), v, 1)
			if res.Metrics[mDelivery] != 1 || len(res.Loss) != 0 || res.Hops["unknown"] != 0 {
				t.Errorf("delivery %v, loss %v, unknown hops %v; want everything delivered on traced paths",
					res.Metrics[mDelivery], res.Loss, res.Hops["unknown"])
			}
			if res.Metrics[mP50] <= 0 || res.Metrics[mHops] < 1 || res.Metrics[mDuplicates] < 1 || res.Metrics[mMeshDegree] <= 0 {
				t.Errorf("metrics %v; want positive latency and mesh degree, at least one hop and one copy", res.Metrics)
			}
			if _, ok := res.Metrics[mSybilShare]; ok {
				t.Error("a network without adversaries has a Sybil share")
			}
			if res.Honest != 16 || res.Adversaries != 0 || res.Links != 32 || res.Events == 0 {
				t.Errorf("result describes %d honest nodes, %d adversaries, %d links, %d events; want 16, 0, 32, some",
					res.Honest, res.Adversaries, res.Links, res.Events)
			}
		})
	}
}

// TestHopsMatchTheEventTrace: the trace files the honest nodes write
// (mesh.trace_path), joined by internal/eventtrace (ADR 0032), give every
// event at every honest node the hop count and the copies the harness
// measures from its own trace (ADR 0035), in every router variant. Sybils
// write no file, so where the harness follows a path through one that
// relayed, the files cannot trace it.
func TestHopsMatchTheEventTrace(t *testing.T) {
	for _, tc := range []struct {
		name, variant string
		sybils        int
	}{
		{name: "v0.1", variant: V01},
		{name: "plain", variant: Plain},
		{name: "paper", variant: Paper},
		{name: "forwarding sybils", variant: Plain, sybils: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			sc := smallScenario(16, 4, 70*time.Second, func(w *world, honest []int32) error {
				w.traceDir = dir
				_, err := addSybils(w, tc.sybils, 8, honest, 0, -1)
				return err
			})
			w, _ := runSmall(t, sc, tc.variant, 4)
			compared, viaSybil := compareWithTraceFiles(t, w, dir)
			if compared == 0 {
				t.Error("no hop count compared")
			}
			if tc.sybils > 0 && viaSybil == 0 {
				t.Error("no path ran through a sybil")
			}
		})
	}
}

// compareWithTraceFiles checks the trace files in dir against the trace of
// w and returns how many hop counts it compared and how many paths ran
// through an adversary.
func compareWithTraceFiles(t *testing.T, w *world, dir string) (compared, viaSybil int) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if want := len(w.honest()); err != nil || len(paths) != want {
		t.Fatalf("trace files %v (%v), want %d, one per honest node", paths, err, want)
	}
	recs, err := eventtrace.ReadFiles(paths...)
	if err != nil {
		t.Fatal(err)
	}
	spreads := map[string]eventtrace.Spread{}
	for _, s := range eventtrace.Join(recs) {
		spreads[s.Event] = s
	}
	j := join(w)
	for e, ev := range w.events {
		s, ok := spreads[ev.ev.ID]
		if !ok {
			t.Errorf("event %d is not in the trace files", e)
			continue
		}
		hops := j.hops(e)
		for _, n := range w.honest() {
			id := w.nodes[n].pid.String()
			got, reached := s.Reached[id]
			switch {
			case hops[n] == 0:
				if s.Origin != id {
					t.Errorf("event %d: the trace files name %s as its origin, want node %d", e, s.Origin, n)
				}
			case hops[n] < 0:
				if reached {
					t.Errorf("event %d reached node %d in the trace files only", e, n)
				}
			case throughAdversary(w, j, e, int(n)):
				viaSybil++
				if !reached || got.Hops != -1 {
					t.Errorf("event %d at node %d came through a sybil, but the trace files give %d hops (reached %v)",
						e, n, got.Hops, reached)
				}
			case !reached || got.Hops != hops[n]:
				t.Errorf("event %d at node %d: %d hops in the trace files (reached %v), want %d", e, n, got.Hops, reached, hops[n])
			default:
				compared++
			}
			if want := int(j.d[e][n].copies); s.Copies[id] != want {
				t.Errorf("event %d at node %d: %d copies in the trace files, want %d", e, n, s.Copies[id], want)
			}
		}
	}
	return compared, viaSybil
}

// throughAdversary reports whether the path of event e to node n, as the
// harness joined it, runs through an adversary.
func throughAdversary(w *world, j *joined, e, n int) bool {
	for range w.nodes {
		p := int(j.d[e][n].parent)
		switch {
		case p < 0 || p == n:
			return false
		case !w.nodes[p].honest:
			return true
		}
		n = p
	}
	return false
}

// sybilScenario adds sybils to a small network: each dials 8 honest
// nodes at 0 s, before the honest links come up, and stops forwarding at
// attackAt.
func sybilScenario(sybils int, attackAt, end time.Duration) *Scenario {
	return smallScenario(16, 4, end, func(w *world, honest []int32) error {
		if _, err := addSybils(w, sybils, 8, honest, 0, attackAt); err != nil {
			return err
		}
		delayHonestLinks(w)
		w.attackFrom = 0
		w.disruption = disruption{kind: "attack"}
		return nil
	})
}

// fromSybil returns the honest nodes' records of copies that came from an
// adversary.
func fromSybil(w *world) []record {
	var out []record
	for _, n := range w.nodes {
		if !n.honest {
			continue
		}
		for _, r := range n.trace.records {
			if r.from >= 0 && !w.nodes[r.from].honest {
				out = append(out, r)
			}
		}
	}
	return out
}

// TestSybilsTakeMeshSlotsAndDrop: sybils that connect first hold mesh
// slots of plain GossipSub and relay nothing.
func TestSybilsTakeMeshSlotsAndDrop(t *testing.T) {
	w, res := runSmall(t, sybilScenario(6, 0, 70*time.Second), Plain, 2)
	if res.Metrics[mSybilShare] <= 0 {
		t.Errorf("Sybil share of mesh slots %v, want some", res.Metrics[mSybilShare])
	}
	if recs := fromSybil(w); len(recs) != 0 {
		t.Errorf("honest nodes got %d copies from dropping sybils, e.g. %+v", len(recs), recs[0])
	}
	relayed := 0
	for _, n := range w.nodes {
		if !n.honest {
			relayed += len(n.trace.records)
		}
	}
	if relayed == 0 {
		t.Error("the sybils recorded no copy they got")
	}
}

// TestCovertSybilsForwardUntilTheAttack: covert sybils forward copies to
// their mesh until the attack starts, and none after.
func TestCovertSybilsForwardUntilTheAttack(t *testing.T) {
	const attackAt = 40 * time.Second
	w, _ := runSmall(t, sybilScenario(6, attackAt, 100*time.Second), Plain, 3)
	recs := fromSybil(w)
	if len(recs) == 0 {
		t.Fatal("honest nodes got no copy from the covert sybils before the attack")
	}
	for _, r := range recs {
		// A copy forwarded just before the attack arrives within a link's
		// latency.
		if r.at >= attackAt+time.Second {
			t.Errorf("a copy from sybil %d at %v, after the attack started at %v", r.from, r.at, attackAt)
		}
	}
}

// TestPreempterWinsTheRace: a preempter close to everyone answers a
// chosen revocation with a forgery of its ID, and every node that gets the
// forgery first loses the revocation; other events pass.
func TestPreempterWinsTheRace(t *testing.T) {
	sc := smallScenario(12, 4, 60*time.Second, func(w *world, honest []int32) error {
		syb, err := addSybils(w, 1, len(honest), honest, 0, -1)
		if err != nil {
			return err
		}
		w.nodes[syb[0]].preempt = true
		// Honest links take 100 ms, the preempter's 5 ms.
		for _, l := range w.linkPairs() {
			lat := 100 * time.Millisecond
			if !w.nodes[l[0]].honest || !w.nodes[l[1]].honest {
				lat = 5 * time.Millisecond
			}
			w.latency[l] = lat
		}
		revoker := w.nodes[honest[0]]
		v := newVerdict(revoker.id, w.start.Add(12*time.Second), honestTTL, 77, 0)
		ve := w.addEvent(&event{ev: v, publisher: revoker.idx, at: 12 * time.Second})
		w.at(12*time.Second, "publish", func() error { w.publish(revoker, ve); return nil })
		rv := newRevocation(revoker.id, w.start.Add(20*time.Second), v)
		re := w.addEvent(&event{ev: rv, publisher: revoker.idx, at: 20 * time.Second, chosen: true})
		w.at(20*time.Second, "revoke", func() error { w.publish(revoker, re); return nil })
		w.windows = []window{{name: "chosen", to: w.end, chosen: true}}
		return nil
	})
	_, res := runSmall(t, sc, V01, 4)
	if got := res.Metrics[windowPrefix+"chosen"]; got != 0 {
		t.Errorf("delivery of the chosen revocation %v, want 0: every node got the forgery first", got)
	}
	// 11 of the pairs are the revocation's; the rest were delivered.
	pairs := float64(11 * res.Events)
	if got, want := res.Loss[causePreempted], 11/pairs; !near(got, want) || len(res.Loss) != 1 {
		t.Errorf("loss %v, want only the revocation's 11 pairs preempted (%v)", res.Loss, want)
	}
}

// TestFlooderInjectsJunk: junk a sybil injects into one node is valid, so
// every node accepts it, but the run measures only its own events.
func TestFlooderInjectsJunk(t *testing.T) {
	const junk = 100
	var stores []*memStore
	sc := smallScenario(8, 4, 60*time.Second, func(w *world, honest []int32) error {
		syb, err := addSybils(w, 1, 1, honest[:1], 0, -1)
		if err != nil {
			return err
		}
		signer, err := newJunkSigner(w, 20)
		if err != nil {
			return err
		}
		flood(w, w.nodes[syb[0]], signer, honest[:1], 10*time.Second, 20*time.Second, junk/10, false)
		for _, i := range honest {
			stores = append(stores, w.nodes[i].store.(*memStore))
		}
		return nil
	})
	w, res := runSmall(t, sc, V01, 5)
	for i, s := range stores {
		if n := len(s.seen); n < junk {
			t.Errorf("node %d accepted %d events, want at least the %d junk verdicts", i, n, junk)
		}
	}
	if res.Events != len(w.events) || res.Metrics[mDelivery] != 1 {
		t.Errorf("%d events measured, delivery %v; want only the run's own %d, all delivered", res.Events, res.Metrics[mDelivery], len(w.events))
	}
}

// TestNodeOfflineAndRejoin: a node that is down misses what is published
// meanwhile, and gets what is published after it rejoined.
func TestNodeOfflineAndRejoin(t *testing.T) {
	const down, up = 20 * time.Second, 50 * time.Second
	var gone *node
	sc := smallScenario(12, 4, 110*time.Second, func(w *world, honest []int32) error {
		for _, i := range honest {
			if !w.nodes[i].publisher {
				gone = w.nodes[i]
				break
			}
		}
		w.at(down, "offline", func() error { w.takeDown(gone); return nil })
		w.at(up, "rejoin", func() error { return w.bringUp(gone) })
		w.watched = []int32{gone.idx}
		w.windows = []window{{name: "during", from: down, to: up, watched: true},
			{name: "after", from: up + 15*time.Second, to: w.end - drain, watched: true}}
		return nil
	})
	_, res := runSmall(t, sc, V01, 6)
	if got := res.Metrics[windowPrefix+"during"]; got != 0 {
		t.Errorf("the node got %v of the verdicts published while it was down, want none", got)
	}
	if got := res.Metrics[windowPrefix+"after"]; got != 1 {
		t.Errorf("the node got %v of the verdicts published after it rejoined, want all", got)
	}
	// A verdict published just after the rejoin may miss the node while it
	// reconnects; that loss is never_received.
	if res.Loss[causeOffline] == 0 {
		t.Errorf("loss %v, want some offline", res.Loss)
	}
	// The run's end stops every node, and records that too.
	if want := [2]int64{int64(down), int64(up)}; len(gone.offline) != 2 || gone.offline[0] != want {
		t.Errorf("offline periods %v, want %v and the end", gone.offline, want)
	}
}
