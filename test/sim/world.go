package sim

import (
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"slices"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/test/sim/internal/mocknet"
)

// world is one run of a scenario in one variant with one seed. It runs
// inside a testing/synctest bubble: time.Now is virtual.
type world struct {
	sc      *Scenario
	variant Variant
	seed    int64
	rng     *rand.Rand
	logger  *slog.Logger
	start   time.Time

	mn      mocknet.Mocknet
	nodes   []*node
	byPeer  map[peer.ID]int32
	latency map[[2]int32]time.Duration
	// links are the node pairs that have a link, the lower index first.
	links [][2]int32

	events   []*event
	eventIdx map[string]int32

	// end is when the run ends; publishing stops drain before it.
	end time.Duration
	// actions run in time order; see at.
	actions []action
	// disruption is what mesh recovery is measured from.
	disruption disruption
	// attackFrom starts the window of the Sybil share of mesh slots; a
	// negative value means no attack.
	attackFrom time.Duration
	// windows are the extra event windows the report gives the delivery
	// ratio of.
	windows []window
	// watched are the nodes whose mesh an outage disrupts.
	watched []int32
	// trusted is the window of the trusted verdicts whose retention the
	// run measures (C-flood); zero if none.
	trusted window

	// delayedLink, if set, returns when the link between two nodes comes
	// up; 0 is at the start.
	delayedLink func(a, b int32) time.Duration

	// traceDir, if set, is where every honest node writes its trace file
	// (mesh.trace_path, ADR 0032), named after its peer ID.
	traceDir string

	samples []sample
}

// drain is how long before the end publishing stops.
const drain = 30 * time.Second

// action is something the run does at a time.
type action struct {
	at   time.Duration
	name string
	do   func() error
}

// disruption is when a scenario disrupts the mesh, and how recovery is
// judged: attack (the Sybil share falls to at most 10 %) or outage (the
// watched nodes' honest mesh slots are back to 90 % of before).
type disruption struct {
	kind string
	at   time.Duration
	// baseline is the window before the outage the slots are compared with.
	baseline [2]time.Duration
}

// window selects the events published in [from, to), only the chosen
// ones if chosen is set, and the watched nodes if watched is set.
type window struct {
	name     string
	from, to time.Duration
	watched  bool
	chosen   bool
}

// sample is the mesh at one time.
type sample struct {
	at time.Duration
	// slots are the mesh slots of the running honest nodes, sybil those
	// holding an adversary; watched the honest slots of the watched nodes.
	slots, sybil, watched int
}

func newWorld(sc *Scenario, v Variant, seed int64) *world {
	return &world{
		sc: sc, variant: v, seed: seed,
		rng:        rand.New(rand.NewPCG(uint64(seed), 0x0b1e)), // #nosec G115 G404 -- seeds are small and positive; a simulation.
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		start:      time.Now(),
		mn:         mocknet.New(),
		byPeer:     map[peer.ID]int32{},
		latency:    map[[2]int32]time.Duration{},
		eventIdx:   map[string]int32{},
		attackFrom: -1,
	}
}

// elapsed is the virtual time since the run started.
func (w *world) elapsed() time.Duration { return time.Since(w.start) }

// index returns the index of the node p, or -1.
func (w *world) index(p peer.ID) int32 {
	if i, ok := w.byPeer[p]; ok {
		return i
	}
	return -1
}

// at schedules do at t.
func (w *world) at(t time.Duration, name string, do func() error) {
	w.actions = append(w.actions, action{at: t, name: name, do: do})
}

// addNodes adds n nodes, honest or adversaries, placed in random regions,
// and returns their indices.
func (w *world) addNodes(n int, honest bool) ([]int32, error) {
	out := make([]int32, 0, n)
	regionsOf := placeNodes(w.rng, n)
	for i := range n {
		id, err := w.newIdentity()
		if err != nil {
			return nil, err
		}
		pid, err := peer.Decode(id.PeerID())
		if err != nil {
			return nil, err
		}
		idx := int32(len(w.nodes)) // #nosec G115 -- node counts fit in int32.
		first := byte(10)
		if !honest {
			first = 11
		}
		addr, err := ma.NewMultiaddr(fmt.Sprintf("/ip4/%d.%d.%d.%d/tcp/4001", first, idx>>16&0xff, idx>>8&0xff, idx&0xff))
		if err != nil {
			return nil, err
		}
		nd := &node{idx: idx, id: id, pid: pid, addr: addr, region: regionsOf[i], honest: honest}
		if honest {
			nd.store = newMemStore()
		}
		w.nodes = append(w.nodes, nd)
		w.byPeer[pid] = idx
		out = append(out, idx)
	}
	return out, nil
}

// newIdentity returns a key drawn from the run's random numbers.
func (w *world) newIdentity() (*simIdentity, error) {
	var seed [ed25519.SeedSize]byte
	for i := 0; i < len(seed); i += 8 {
		binary.LittleEndian.PutUint64(seed[i:], w.rng.Uint64())
	}
	return newSimIdentity(seed)
}

// honest returns the indices of the honest nodes.
func (w *world) honest() []int32 {
	var out []int32
	for _, n := range w.nodes {
		if n.honest {
			out = append(out, n.idx)
		}
	}
	return out
}

// useRealStores gives the honest nodes the real store with the options.
func (w *world) useRealStores(opts store.Options) {
	for _, n := range w.nodes {
		if n.honest {
			o := opts
			o.Self = n.pid.String()
			n.db = store.NewMemory(w.logger, o)
			n.store = n.db
		}
	}
}

// setGraph gives the nodes in idx the dials of g, whose node i is idx[i].
func (w *world) setGraph(idx []int32, g *graph) {
	for i, dials := range g.dials {
		n := w.nodes[idx[i]]
		for _, d := range dials {
			n.dials = append(n.dials, int(idx[d]))
		}
	}
}

// choosePublishers makes a share of the honest nodes publishers.
func (w *world) choosePublishers(share float64) []int32 {
	honest := w.honest()
	k := max(1, int(share*float64(len(honest))+0.5))
	perm := w.rng.Perm(len(honest))[:k]
	out := make([]int32, 0, k)
	for _, p := range perm {
		w.nodes[honest[p]].publisher = true
		out = append(out, honest[p])
	}
	slices.Sort(out)
	return out
}

// addEvent adds an honest event to publish from the node at t.
func (w *world) addEvent(e *event) int32 {
	idx := int32(len(w.events)) // #nosec G115 -- event counts fit in int32.
	w.events = append(w.events, e)
	w.eventIdx[e.ev.ID] = idx
	return idx
}

// verdicts schedules honest verdicts at the times from random publishers.
func (w *world) verdicts(publishers []int32, times []time.Duration) {
	salt := w.rng.Uint32()
	for i, t := range times {
		p := publishers[w.rng.IntN(len(publishers))]
		ev := newVerdict(w.nodes[p].id, w.start.Add(t), honestTTL, salt, uint64(i)) // #nosec G115 -- i >= 0.
		e := w.addEvent(&event{ev: ev, publisher: p, at: t})
		w.at(t, "publish", func() error { w.publish(w.nodes[p], e); return nil })
	}
}

// link joins the nodes a and b, with a latency drawn once for the pair.
func (w *world) link(a, b int32) error {
	key := [2]int32{min(a, b), max(a, b)}
	lat, ok := w.latency[key]
	if !ok {
		lat = linkLatency(w.rng, w.nodes[a].region, w.nodes[b].region)
		w.latency[key] = lat
	}
	l, err := w.mn.LinkPeers(w.nodes[a].pid, w.nodes[b].pid)
	if err != nil {
		return err
	}
	l.SetOptions(mocknet.LinkOptions{Latency: lat})
	return nil
}

// linkPairs returns every pair of nodes one of which dials the other.
func (w *world) linkPairs() [][2]int32 {
	seen := map[[2]int32]bool{}
	var out [][2]int32
	for _, n := range w.nodes {
		for _, d := range n.dials {
			p := [2]int32{min(n.idx, int32(d)), max(n.idx, int32(d))} // #nosec G115 -- node counts fit in int32.
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// takeDown stops the honest node n and removes its links, as if its
// server went away.
func (w *world) takeDown(n *node) {
	w.stopNode(n)
	for _, l := range w.links {
		if l[0] == n.idx || l[1] == n.idx {
			_ = w.mn.UnlinkPeers(w.nodes[l[0]].pid, w.nodes[l[1]].pid)
		}
	}
}

// bringUp restarts the honest node n with its identity and store, after
// takeDown.
func (w *world) bringUp(n *node) error {
	h, err := w.newHost(n)
	if err != nil {
		return err
	}
	for _, l := range w.links {
		if l[0] == n.idx || l[1] == n.idx {
			if err := w.link(l[0], l[1]); err != nil {
				return err
			}
		}
	}
	n.mu.Lock()
	if k := len(n.offline); k > 0 && n.offline[k-1][1] == 0 {
		n.offline[k-1][1] = int64(w.elapsed())
	}
	n.mu.Unlock()
	return w.startHonest(n, h)
}

// sampleMesh records the running honest nodes' mesh slots.
func (w *world) sampleMesh() {
	s := sample{at: w.elapsed()}
	watched := map[int32]bool{}
	for _, i := range w.watched {
		watched[i] = true
	}
	for _, n := range w.nodes {
		if !n.honest || !n.up() {
			continue
		}
		for _, p := range n.trace.meshPeers() {
			s.slots++
			switch {
			case p >= 0 && !w.nodes[p].honest:
				s.sybil++
			case watched[n.idx]:
				s.watched++
			}
		}
	}
	w.samples = append(w.samples, s)
}

// run builds the scenario, runs it until its end and returns the
// measurements. It must run inside a synctest bubble.
func (w *world) run() (*Result, error) {
	if err := w.sc.build(w); err != nil {
		return nil, fmt.Errorf("build %s: %w", w.sc.Name, err)
	}
	w.links = w.linkPairs()
	if err := w.startAll(w.delayedLink); err != nil {
		return nil, fmt.Errorf("start %s: %w", w.sc.Name, err)
	}
	for sec := time.Second; sec < w.end; sec += time.Second {
		w.at(sec, "sample", func() error { w.sampleMesh(); return nil })
	}
	slices.SortStableFunc(w.actions, func(a, b action) int { return int(a.at - b.at) })
	defer w.shutdown()
	for _, a := range w.actions {
		if d := a.at - w.elapsed(); d > 0 {
			time.Sleep(d)
		}
		if err := a.do(); err != nil {
			return nil, fmt.Errorf("%s at %v: %w", a.name, a.at, err)
		}
	}
	time.Sleep(w.end - w.elapsed())
	return w.measure(), nil
}

// startAll creates every node's host and every link that exists from the
// start, then starts the honest nodes and the sybils. delayed links, if
// set, come up at the time it returns instead.
func (w *world) startAll(delayed func(a, b int32) time.Duration) error {
	hosts := make(map[int32]host.Host, len(w.nodes))
	for _, n := range w.nodes {
		h, err := w.newHost(n)
		if err != nil {
			return err
		}
		hosts[n.idx] = h
	}
	for _, l := range w.linkPairs() {
		if delayed != nil {
			if t := delayed(l[0], l[1]); t > 0 {
				w.at(t, "link", func() error { return w.link(l[0], l[1]) })
				continue
			}
		}
		if err := w.link(l[0], l[1]); err != nil {
			return err
		}
	}
	for _, n := range w.nodes {
		if !n.honest {
			w.startSybil(n, hosts[n.idx])
			continue
		}
		if n.db != nil {
			if err := n.db.Start(context.Background()); err != nil {
				return err
			}
		}
		if err := w.startHonest(n, hosts[n.idx]); err != nil {
			return err
		}
	}
	return nil
}

// startSybil runs the adversary n on h; it dials its targets at its
// connect time.
func (w *world) startSybil(n *node, h host.Host) {
	s := newSybil(w, n, h, n.attackAt)
	s.preempt = n.preempt
	n.mu.Lock()
	n.host, n.sybil, n.running = h, s, true
	n.mu.Unlock()
	w.at(n.connectAt, "sybil connects", func() error {
		for _, d := range n.dials {
			s.dial(w.nodes[d])
		}
		return nil
	})
}

// shutdown stops every node, the stores and the mocknet, and waits for
// them.
func (w *world) shutdown() {
	var wg sync.WaitGroup
	for _, n := range w.nodes {
		wg.Go(func() { w.stopNode(n) })
	}
	wg.Wait()
	for _, n := range w.nodes {
		if n.db != nil {
			_ = n.db.Stop(context.Background())
		}
	}
	_ = w.mn.Close()
}
