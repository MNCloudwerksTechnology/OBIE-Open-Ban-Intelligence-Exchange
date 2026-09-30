package sim

import (
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/MNCloudwerksTechnology/obie/internal/store"
)

// Scenario is a simulated network with its adversaries and traffic (ADR
// 0033).
type Scenario struct {
	Name    string
	Group   string
	Summary string
	// Variants are the router variants the scenario runs in.
	Variants []string
	// Honest is the number of honest nodes, the N of ln N / ln(D−1).
	Honest int
	// Params describe the scenario in the report.
	Params [][2]string
	// Checks are assertions over the results of every seed.
	Checks []Check
	build  func(w *world) error
}

// Check is an assertion over a scenario's results (harness validation
// and CI regression guard).
type Check struct {
	Name string
	// Test returns whether the results pass and what they showed.
	Test func(results map[string][]*Result) (bool, string)
}

// Scenarios returns every scenario by name.
func Scenarios() map[string]*Scenario {
	out := map[string]*Scenario{}
	for _, s := range []*Scenario{
		scenarioA("eclipse"), scenarioA("coldboot"), scenarioA("covertflash"),
		scenarioB(0.1), scenarioB(0.3), scenarioB(0.5),
		scenarioFlood(), scenarioJunk(), scenarioPreempt(), scenarioOffline(), scenarioBootKill(),
		scenarioTraffic("T-static", "static", 1000, 1, false), scenarioTraffic("T-regular", "regular", 1000, 1, false),
		scenarioTraffic("T-lowrate", "static", 1000, 0.1, false),
		scenarioTraffic("T-burst-static", "static", 300, 1, true), scenarioTraffic("T-burst-regular", "regular", 300, 1, true),
		scenarioReduced(),
	} {
		out[s.Name] = s
	}
	return out
}

// Common parameters of the scenarios (ADR 0033).
const (
	publisherShare = 0.10
	regularDegree  = 20
	// In the paper's testbed every honest node dials honestDials random
	// honest nodes.
	honestDials = 20
	// A static-bootstrap graph has a hub per nodesPerHub nodes; every node
	// lists hubsPerNode hubs, every hub hubsPerHub others.
	nodesPerHub = 50
	hubsPerNode = 2
	hubsPerHub  = 3
	trafficFrom = 30 * time.Second
)

// regularHonest adds n honest nodes on a random regular graph.
func regularHonest(w *world, n int) ([]int32, error) {
	idx, err := w.addNodes(n, true)
	if err != nil {
		return nil, err
	}
	g, err := randomRegular(w.rng, n, regularDegree)
	if err != nil {
		return nil, err
	}
	w.setGraph(idx, g)
	return idx, nil
}

// paperHonest adds n honest nodes that each dial honestDials random
// honest nodes, as in the paper's testbed.
func paperHonest(w *world, n int) ([]int32, error) {
	idx, err := w.addNodes(n, true)
	if err != nil {
		return nil, err
	}
	g, err := randomDial(w.rng, n, honestDials)
	if err != nil {
		return nil, err
	}
	w.setGraph(idx, g)
	return idx, nil
}

// staticHonest adds n honest nodes on today's static-bootstrap graph and
// returns them and the hubs.
func staticHonest(w *world, n int) (nodes, hubs []int32, err error) {
	idx, err := w.addNodes(n, true)
	if err != nil {
		return nil, nil, err
	}
	g, err := staticBootstrap(w.rng, n, max(2, n/nodesPerHub), hubsPerNode, hubsPerHub)
	if err != nil {
		return nil, nil, err
	}
	w.setGraph(idx, g)
	for _, h := range g.hubs {
		hubs = append(hubs, idx[h])
	}
	return idx, hubs, nil
}

// topologyHonest adds n honest nodes on the named topology.
func topologyHonest(w *world, topology string, n int) ([]int32, error) {
	if topology == "regular" {
		return regularHonest(w, n)
	}
	idx, _, err := staticHonest(w, n)
	return idx, err
}

// addSybils adds n sybils that each dial perSybil random nodes of targets
// at connectAt and stop forwarding at attackAt (negative: never).
func addSybils(w *world, n, perSybil int, targets []int32, connectAt, attackAt time.Duration) ([]int32, error) {
	idx, err := w.addNodes(n, false)
	if err != nil {
		return nil, err
	}
	for _, i := range idx {
		s := w.nodes[i]
		for _, t := range pick(w.rng, len(targets), perSybil, -1) {
			s.dials = append(s.dials, int(targets[t]))
		}
		s.connectAt, s.attackAt = connectAt, attackAt
	}
	return idx, nil
}

// delayHonestLinks brings the links between honest nodes up 1–5 s after
// the start, so that sybils connect first (cold boot).
func delayHonestLinks(w *world) {
	w.delayedLink = func(a, b int32) time.Duration {
		if w.nodes[a].honest && w.nodes[b].honest {
			return time.Second + time.Duration(w.rng.Float64()*float64(4*time.Second))
		}
		return 0
	}
}

// scenarioA reproduces an attack of Vyzovitis et al. 2020 (ADR 0033).
func scenarioA(attack string) *Scenario {
	const honest, sybils = 1000, 4000
	perSybil, connectAt, attackAt, end := 20, time.Duration(0), time.Duration(0), 240*time.Second
	summary := ""
	switch attack {
	case "eclipse":
		perSybil, connectAt = 100, time.Minute
		summary = "a warm network; at 60 s the Sybils connect and drop everything"
	case "coldboot":
		summary = "the Sybils connect as the honest nodes start, before the honest links come up (1–5 s), and drop everything"
	case "covertflash":
		attackAt, end = 2*time.Minute, 300*time.Second
		summary = "like cold boot, but the Sybils forward until 120 s, then drop everything"
	}
	disrupt := max(connectAt, attackAt)
	return &Scenario{
		Name: "A-" + attack, Group: "A", Summary: summary, Variants: []string{Plain, Paper, V01}, Honest: honest,
		Params: [][2]string{
			{"honest nodes", "1,000 (10 % publish); each dials 20 random honest nodes (about 40 links each), as in the paper's testbed"},
			{"Sybils", fmt.Sprintf("4,000, %d links each to random honest nodes, connect at %v", perSybil, connectAt)},
			{"attack", fmt.Sprintf("Sybils drop everything from %v", disrupt)},
			{"traffic", "Poisson, 1 verdict/s network-wide, from 30 s"},
			{"run", fmt.Sprintf("%v; publishing stops at %v", end, end-drain)},
		},
		Checks: validationChecks(),
		build: func(w *world) error {
			h, err := paperHonest(w, honest)
			if err != nil {
				return err
			}
			if _, err := addSybils(w, sybils, perSybil, h, connectAt, attackAt); err != nil {
				return err
			}
			if attack != "eclipse" {
				delayHonestLinks(w)
			}
			w.end = end
			pubs := w.choosePublishers(publisherShare)
			w.verdicts(pubs, poisson(w.rng, trafficFrom, end-drain, 1))
			w.attackFrom = disrupt
			w.disruption = disruption{kind: "attack", at: disrupt}
			if disrupt > trafficFrom {
				w.windows = []window{{name: "before the attack", from: trafficFrom, to: disrupt},
					{name: "during the attack", from: disrupt, to: end - drain}}
			}
			return nil
		},
	}
}

// validationChecks are the harness validation of AC 2: plain GossipSub
// loses verdicts under attack, the paper's scoring loses none.
func validationChecks() []Check {
	return []Check{
		{Name: "plain GossipSub loses verdicts (measurable loss)", Test: func(res map[string][]*Result) (bool, string) {
			// Measurable: every seed lost verdicts, or the confidence
			// interval of the delivery ratio lies below 1.
			s := summarize(values(res[Plain], mDelivery))
			return s.n > 0 && (s.maxV < 1 || s.high < 1),
				fmt.Sprintf("delivery ratio %s, highest seed %.6f", formatSummary(s, 4), s.maxV)
		}},
		{Name: "the paper's scoring loses none", Test: func(res map[string][]*Result) (bool, string) {
			s := summarize(values(res[Paper], mDelivery))
			return s.n > 0 && s.minV == 1, fmt.Sprintf("delivery ratio %s, lowest seed %.6f", formatSummary(s, 4), s.minV)
		}},
	}
}

// scenarioB is the scale scenario: 10,000 honest nodes and adversaries
// making up a share f of all nodes, on one random regular graph.
func scenarioB(f float64) *Scenario {
	const honest = 10000
	adversaries := int(math.Round(honest * f / (1 - f)))
	const end = 150 * time.Second
	return &Scenario{
		Name: fmt.Sprintf("B-f%02.0f", f*100), Group: "B", Variants: []string{V01}, Honest: honest,
		Summary: fmt.Sprintf("10,000 honest nodes and %d adversaries (f = %.1f) on one random 20-regular graph; the adversaries drop everything", adversaries, f),
		Params: [][2]string{
			{"honest nodes", "10,000 (10 % publish)"},
			{"adversaries", fmt.Sprintf("%d (f = %.1f of all nodes), same graph, GRAFT every peer, drop everything from the start", adversaries, f)},
			{"graph", "random 20-regular over all nodes; links between adversaries are left out"},
			{"traffic", "Poisson, 1 verdict/s network-wide, from 30 s"},
			{"run", "150 s; publishing stops at 120 s"},
		},
		build: func(w *world) error {
			if _, err := w.addNodes(honest, true); err != nil {
				return err
			}
			if _, err := w.addNodes(adversaries, false); err != nil {
				return err
			}
			g, err := randomRegular(w.rng, honest+adversaries, regularDegree)
			if err != nil {
				return err
			}
			for i, dials := range g.dials {
				for _, d := range dials {
					if w.nodes[i].honest || w.nodes[d].honest {
						w.nodes[i].dials = append(w.nodes[i].dials, d)
					}
				}
			}
			w.end = end
			w.verdicts(w.choosePublishers(publisherShare), poisson(w.rng, trafficFrom, end-drain, 1))
			w.attackFrom = 0
			w.disruption = disruption{kind: "attack"}
			return nil
		},
	}
}

// junkSigner holds the identities of weight-0 publishers that sign junk
// verdicts in turn, across every sybil of a run (the run's actions use it
// one at a time).
type junkSigner struct {
	keys []*simIdentity
	n    uint64
}

// newJunkSigner returns a signer with n identities.
func newJunkSigner(w *world, n int) (*junkSigner, error) {
	keys := make([]*simIdentity, n)
	for i := range keys {
		id, err := w.newIdentity()
		if err != nil {
			return nil, err
		}
		keys[i] = id
	}
	return &junkSigner{keys: keys}, nil
}

// flood schedules junk verdicts, signed by the signer's keys in turn, that
// the sybil injects at rate per second from from to to, each to all of
// targets or, with distinct, a fresh one to each.
func flood(w *world, s *node, junk *junkSigner, targets []int32, from, to time.Duration, rate float64, distinct bool) {
	salt := w.rng.Uint32()
	for _, t := range uniform(from, to, rate) {
		w.at(t, "junk", func() error {
			s.mu.Lock()
			sy := s.sybil
			s.mu.Unlock()
			if sy == nil {
				return nil
			}
			groups := [][]int32{targets}
			if distinct {
				groups = make([][]int32, len(targets))
				for i, target := range targets {
					groups[i] = []int32{target}
				}
			}
			for _, g := range groups {
				key := junk.keys[junk.n%uint64(len(junk.keys))]
				ev := newVerdict(key, w.start.Add(w.elapsed()), junkTTL, salt, junk.n)
				junk.n++
				data, err := marshalEvent(ev)
				if err != nil {
					return err
				}
				to := make([]peer.ID, len(g))
				for i, target := range g {
					to[i] = w.nodes[target].pid
				}
				sy.inject(data, to)
			}
			return nil
		})
	}
}

// scenarioFlood: weight-0 Sybil publishers flood the network with junk
// verdicts that outlive the trusted ones, aimed at store eviction.
func scenarioFlood() *Scenario {
	const honest, capacity, flooders, perFlooder = 200, 2000, 10, 10
	const floodFrom, floodTo, end = 150 * time.Second, 210 * time.Second, 240 * time.Second
	const rate = 100.0
	return &Scenario{
		Name: "C-flood", Group: "C", Variants: []string{V01}, Honest: honest,
		Summary: "weight-0 Sybil publishers flood junk verdicts with the longest TTL; the real store evicts the verdict that expires first",
		Params: [][2]string{
			{"honest nodes", "200 (10 % publish), static-bootstrap graph, real store (BadgerDB in memory) with store.max_indicators 2,000 (scaled from 1,000,000)"},
			{"trusted verdicts", "Poisson, 1/s from 30 s, TTL 7 days; the retained share counts those published before the flood (30–150 s)"},
			{"flood", "10 Sybil hosts, 10 links each, that forward honest traffic; 100 junk verdicts/s network-wide from 150 s to 210 s (6,000), TTL 30 days, signed by 1,000 weight-0 keys in turn"},
			{"run", "240 s; publishing stops at 210 s"},
		},
		build: func(w *world) error {
			h, _, err := staticHonest(w, honest)
			if err != nil {
				return err
			}
			w.useRealStores(store.Options{MaxIndicators: capacity})
			pubs := w.choosePublishers(publisherShare)
			w.end = end
			w.verdicts(pubs, poisson(w.rng, trafficFrom, end-drain, 1))
			syb, err := addSybils(w, flooders, perFlooder, h, 0, -1)
			if err != nil {
				return err
			}
			junk, err := newJunkSigner(w, 1000)
			if err != nil {
				return err
			}
			for _, s := range syb {
				targets := toInt32(w.nodes[s].dials)
				flood(w, w.nodes[s], junk, targets, floodFrom, floodTo, rate/flooders, false)
			}
			w.trusted = window{name: "trusted", from: trafficFrom, to: floodFrom}
			w.windows = []window{{name: "before the flood", from: trafficFrom, to: floodFrom},
				{name: "during the flood", from: floodFrom, to: floodTo}}
			return nil
		},
	}
}

// scenarioJunk: junk routed through one honest relay (a hub) empties the
// per-peer buckets its neighbors keep for it.
func scenarioJunk() *Scenario {
	const honest, end = 300, 210 * time.Second
	const junkFrom, junkTo, rate = 60 * time.Second, 180 * time.Second, 50.0
	return &Scenario{
		Name: "C-junk", Group: "C", Variants: []string{V01}, Honest: honest,
		Summary: "two Sybil hosts inject valid junk into one hub at the per-peer rate limit; the hub relays it and its neighbors' buckets for it run dry",
		Params: [][2]string{
			{"honest nodes", "300 (10 % publish), static-bootstrap graph (6 hubs)"},
			{"junk", "2 Sybil hosts linked to hub 0 that forward honest traffic, each injecting 50 junk verdicts/s (the per-peer limit) from 60 s to 180 s, signed by 500 weight-0 keys in turn"},
			{"traffic", "Poisson, 1 verdict/s network-wide, from 30 s"},
			{"run", "210 s; publishing stops at 180 s"},
		},
		build: func(w *world) error {
			_, hubs, err := staticHonest(w, honest)
			if err != nil {
				return err
			}
			w.end = end
			w.verdicts(w.choosePublishers(publisherShare), poisson(w.rng, trafficFrom, end-drain, 1))
			syb, err := addSybils(w, 2, 1, hubs[:1], 0, -1)
			if err != nil {
				return err
			}
			junk, err := newJunkSigner(w, 500)
			if err != nil {
				return err
			}
			for _, s := range syb {
				flood(w, w.nodes[s], junk, hubs[:1], junkFrom, junkTo, rate, true)
			}
			w.windows = []window{{name: "before the junk", from: trafficFrom, to: junkFrom},
				{name: "during the junk", from: junkFrom, to: junkTo}}
			return nil
		},
	}
}

// scenarioPreempt: preempters answer chosen revocations with a forged
// message of the same ID (ADR 0009).
func scenarioPreempt() *Scenario {
	const honest, preempters, perPreempter, end = 1000, 10, 100, 180 * time.Second
	revocations := []time.Duration{60 * time.Second, 90 * time.Second, 120 * time.Second}
	return &Scenario{
		Name: "C-preempt", Group: "C", Variants: []string{V01}, Honest: honest,
		Summary: "preempters linked to the revokers and to many honest nodes answer each chosen revocation at once with a forgery of its ID",
		Params: [][2]string{
			{"honest nodes", "1,000 (10 % publish), static-bootstrap graph"},
			{"preempters", "10, each linked to 100 random honest nodes and to the 3 revokers; they forward everything else"},
			{"chosen revocations", "3 per seed, at 60, 90 and 120 s, each by a different random publisher of a verdict it published 20 s before"},
			{"traffic", "Poisson, 1 verdict/s network-wide, from 30 s"},
			{"run", "180 s; publishing stops at 150 s"},
		},
		build: func(w *world) error {
			h, _, err := staticHonest(w, honest)
			if err != nil {
				return err
			}
			w.end = end
			pubs := w.choosePublishers(publisherShare)
			w.verdicts(pubs, poisson(w.rng, trafficFrom, end-drain, 1))
			// pubs is sorted by index, and the hubs come first.
			revokers := make([]int32, 0, len(revocations))
			for _, p := range pick(w.rng, len(pubs), len(revocations), -1) {
				revokers = append(revokers, pubs[p])
			}
			syb, err := addSybils(w, preempters, perPreempter, h, 0, -1)
			if err != nil {
				return err
			}
			for _, s := range syb {
				w.nodes[s].preempt = true
				for _, r := range revokers {
					if !slices.Contains(w.nodes[s].dials, int(r)) {
						w.nodes[s].dials = append(w.nodes[s].dials, int(r))
					}
				}
			}
			salt := w.rng.Uint32()
			for i, t := range revocations {
				p := w.nodes[revokers[i]]
				v := newVerdict(p.id, w.start.Add(t-20*time.Second), honestTTL, salt, uint64(i)) // #nosec G115 -- i >= 0.
				ve := w.addEvent(&event{ev: v, publisher: p.idx, at: t - 20*time.Second})
				w.at(t-20*time.Second, "publish", func() error { w.publish(p, ve); return nil })
				rv := newRevocation(p.id, w.start.Add(t), v)
				re := w.addEvent(&event{ev: rv, publisher: p.idx, at: t, chosen: true})
				w.at(t, "revoke", func() error { w.publish(p, re); return nil })
			}
			w.windows = []window{{name: "chosen revocations", from: 0, to: end, chosen: true}}
			return nil
		},
	}
}

// scenarioOffline: nodes go offline for an hour and rejoin with their
// identity and store; v0.1 has no anti-entropy.
func scenarioOffline() *Scenario {
	const honest, watched = 300, 10
	const down, up, end = 60 * time.Second, 3660 * time.Second, 3780 * time.Second
	return &Scenario{
		Name: "C-offline", Group: "C", Variants: []string{V01}, Honest: honest,
		Summary: "10 nodes are offline for an hour, then rejoin with the same identity and store",
		Params: [][2]string{
			{"honest nodes", "300 (10 % publish), static-bootstrap graph"},
			{"outage", "10 random nodes that are not hubs or publishers, offline from 60 s to 3,660 s; their links are gone meanwhile"},
			{"traffic", "Poisson, 1 verdict/s network-wide, from 30 s"},
			{"run", "3,780 s; publishing stops at 3,750 s"},
		},
		build: func(w *world) error {
			h, hubs, err := staticHonest(w, honest)
			if err != nil {
				return err
			}
			w.end = end
			pubs := w.choosePublishers(publisherShare)
			w.verdicts(pubs, poisson(w.rng, trafficFrom, end-drain, 1))
			var candidates []int32
			for _, i := range h {
				if !slices.Contains(hubs, i) && !w.nodes[i].publisher {
					candidates = append(candidates, i)
				}
			}
			for _, c := range pick(w.rng, len(candidates), watched, -1) {
				n := w.nodes[candidates[c]]
				w.watched = append(w.watched, n.idx)
				w.at(down, "offline", func() error { w.takeDown(n); return nil })
				w.at(up, "rejoin", func() error { return w.bringUp(n) })
			}
			w.disruption = disruption{kind: "outage", at: up, baseline: [2]time.Duration{trafficFrom, down}}
			w.windows = []window{{name: "during the outage, rejoined nodes", from: down, to: up, watched: true},
				{name: "after the rejoin, rejoined nodes", from: up, to: end - drain, watched: true}}
			return nil
		},
	}
}

// scenarioBootKill: every bootstrap hub stops for good at 10 min.
func scenarioBootKill() *Scenario {
	const honest, kill, end = 1000, 600 * time.Second, 900 * time.Second
	return &Scenario{
		Name: "C-bootkill", Group: "C", Variants: []string{V01}, Honest: honest,
		Summary: "every bootstrap hub stops for good at 10 min; nodes know no other peers",
		Params: [][2]string{
			{"honest nodes", "1,000 (10 % of the non-hubs publish), static-bootstrap graph (20 hubs)"},
			{"kill", "all 20 hubs stop at 600 s"},
			{"traffic", "Poisson, 1 verdict/s network-wide, from 30 s"},
			{"run", "900 s; publishing stops at 870 s"},
		},
		build: func(w *world) error {
			h, hubs, err := staticHonest(w, honest)
			if err != nil {
				return err
			}
			w.end = end
			var others []int32
			for _, i := range h {
				if !slices.Contains(hubs, i) {
					others = append(others, i)
				}
			}
			var pubs []int32
			for _, p := range pick(w.rng, len(others), max(1, len(others)/10), -1) {
				w.nodes[others[p]].publisher = true
				pubs = append(pubs, others[p])
			}
			w.verdicts(pubs, poisson(w.rng, trafficFrom, end-drain, 1))
			for _, hub := range hubs {
				n := w.nodes[hub]
				w.at(kill, "kill hub", func() error { w.takeDown(n); return nil })
			}
			w.watched = others
			w.disruption = disruption{kind: "outage", at: kill, baseline: [2]time.Duration{kill - 30*time.Second, kill}}
			w.windows = []window{{name: "before the kill", from: trafficFrom, to: kill},
				{name: "after the kill", from: kill, to: end - drain}}
			return nil
		},
	}
}

// scenarioTraffic is a traffic and topology baseline without attack.
func scenarioTraffic(name, topology string, honest int, rate float64, burst bool) *Scenario {
	end := 180 * time.Second
	if rate < 1 {
		end = 660 * time.Second
	}
	const burstFrom, burstTo, burstRate = 60 * time.Second, 120 * time.Second, 200.0
	params := [][2]string{
		{"honest nodes", fmt.Sprintf("%d (10 %% publish), %s graph", honest, topologyName(topology))},
		{"traffic", fmt.Sprintf("Poisson, %v verdict/s network-wide, from 30 s", rate)},
		{"run", fmt.Sprintf("%v; publishing stops at %v", end, end-drain)},
	}
	summary := fmt.Sprintf("no attack; %v verdict/s on the %s graph", rate, topologyName(topology))
	if burst {
		params = append(params, [2]string{"burst", "a scanning campaign: Poisson, 200 verdicts/s from 60 s to 120 s on top"})
		summary += ", with a scanning burst of 200 verdicts/s for 60 s"
	}
	return &Scenario{
		Name: name, Group: "T", Variants: []string{V01}, Honest: honest, Summary: summary, Params: params,
		build: func(w *world) error {
			if _, err := topologyHonest(w, topology, honest); err != nil {
				return err
			}
			w.end = end
			pubs := w.choosePublishers(publisherShare)
			times := poisson(w.rng, trafficFrom, end-drain, rate)
			if burst {
				times = append(times, poisson(w.rng, burstFrom, burstTo, burstRate)...)
				slices.Sort(times)
				w.windows = []window{{name: "during the burst", from: burstFrom, to: burstTo}}
			}
			w.verdicts(pubs, times)
			return nil
		},
	}
}

func topologyName(t string) string {
	if t == "regular" {
		return "random 20-regular"
	}
	return "static-bootstrap"
}

// reducedFloor is the delivery ratio v0.1 keeps at least in the reduced
// scenario, where its baseline delivers every verdict; the CI job fails
// below it.
const reducedFloor = 0.99

// scenarioReduced is the CI regression guard: a small cold boot attack.
func scenarioReduced() *Scenario {
	const honest, sybils, perSybil, end = 100, 400, 20, 180 * time.Second
	checks := validationChecks()
	checks = append(checks, Check{Name: fmt.Sprintf("v0.1 delivers at least %.2f", reducedFloor),
		Test: func(res map[string][]*Result) (bool, string) {
			s := summarize(values(res[V01], mDelivery))
			return s.n > 0 && s.minV >= reducedFloor, fmt.Sprintf("delivery ratio %s", formatSummary(s, 4))
		}})
	return &Scenario{
		Name: "reduced", Group: "reduced", Variants: []string{Plain, Paper, V01}, Honest: honest,
		Summary: "the CI regression guard: a cold boot attack on 100 honest nodes",
		Params: [][2]string{
			{"honest nodes", "100 (10 % publish), random 20-regular graph"},
			{"Sybils", "400, 20 links each; they connect before the honest links come up and drop everything"},
			{"traffic", "Poisson, 1 verdict/s network-wide, from 30 s"},
			{"run", "180 s; publishing stops at 150 s"},
		},
		Checks: checks,
		build: func(w *world) error {
			h, err := regularHonest(w, honest)
			if err != nil {
				return err
			}
			if _, err := addSybils(w, sybils, perSybil, h, 0, 0); err != nil {
				return err
			}
			delayHonestLinks(w)
			w.end = end
			w.verdicts(w.choosePublishers(publisherShare), poisson(w.rng, trafficFrom, end-drain, 1))
			w.attackFrom = 0
			w.disruption = disruption{kind: "attack"}
			return nil
		},
	}
}

func toInt32(xs []int) []int32 {
	out := make([]int32, len(xs))
	for i, x := range xs {
		out[i] = int32(x) // #nosec G115 -- node counts fit in int32.
	}
	return out
}

// values returns the metric of every result that has it.
func values(results []*Result, metric string) []float64 {
	var out []float64
	for _, r := range results {
		if v, ok := r.Metrics[metric]; ok {
			out = append(out, v)
		}
	}
	return out
}
