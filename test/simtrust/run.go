package simtrust

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// probeInterval is how often in virtual time a run sweeps the store, as
// obied does every minute at most, and reads the weights (ADR 0034).
const probeInterval = 5 * time.Minute

// convergedShare of the ceiling is the weight at which a newcomer has
// converged.
const convergedShare = 0.9

// RunSpec is one run: a behavior model at an adversary fraction under a
// settings profile, on the world of a seed.
type RunSpec struct {
	Model    Model
	Fraction float64
	Profile  Profile
	Seed     uint64
}

func (s RunSpec) String() string {
	return fmt.Sprintf("%s at %.0f %% under %s, seed %d", s.Model, 100*s.Fraction, s.Profile, s.Seed)
}

// keyRecord is what a run records about one publisher key.
type keyRecord struct {
	peerID string
	actor  int
	role   Role
	// joined is when the key could first publish; defected when it first
	// acted maliciously, neutralized when its weight was 0 after that,
	// converged when a newcomer's weight first reached convergedShare of
	// the ceiling, burned when a whitewasher abandoned it; zero if never.
	joined, defected, neutralized, converged, burned time.Time
	// trusted is set once the key's weight was above 0.
	trusted bool
	// events are the times of the events delivered under the key.
	events []time.Time
}

// weightSample is the honest publishers' mean weight at a probe, as a
// fraction of the ceiling.
type weightSample struct {
	at   time.Time
	mean float64
}

// received is a ban verdict the observer received.
type received struct {
	at         time.Time
	key        int
	addr       netip.Addr
	confidence float64
	// end is when it stopped counting: its expiry or its revocation.
	end time.Time
}

// Result is the history of one run, from which the metrics are computed.
type Result struct {
	Spec     RunSpec
	Trace    *Trace
	Cast     cast
	Episodes []Episode
	keys     []keyRecord
	weights  []weightSample
	received []received
	// malicious holds the IDs of the malicious verdicts delivered.
	malicious map[string]bool
}

// runner delivers the streams of one run to the observer.
type runner struct {
	spec  RunSpec
	trace *Trace
	cast  cast
	p     ModelParams
	node  *Node
	res   *Result
	// current is the key index each actor publishes under; -1 while a
	// whitewasher waits to switch, until switchAt.
	current  []int
	switchAt []time.Time
	// verdicts are the received verdicts by event ID, for revocations.
	verdicts map[string]int
	// weight reads a key's weight at a probe.
	weight weightFunc
}

// weightFunc returns the weight of the key with peerID at a probe at at.
type weightFunc func(n *Node, peerID string, at time.Time) float64

// engineWeight reads the weight the engine gives the key.
func engineWeight(n *Node, peerID string, _ time.Time) float64 {
	return n.Weight(peerID)
}

// Run replays the trace t under spec through a new observer node.
func Run(ctx context.Context, t *Trace, spec RunSpec, p ModelParams) (*Result, error) {
	return newRunner(t, spec, p, engineWeight).run(ctx)
}

// newRunner prepares the run of spec on t, with the weights read by
// weight, and the first key of every publisher.
func newRunner(t *Trace, spec RunSpec, p ModelParams, weight weightFunc) *runner {
	c := castOf(t, spec.Model, spec.Fraction, p)
	r := &runner{spec: spec, trace: t, cast: c, p: p, verdicts: map[string]int{}, weight: weight,
		res: &Result{Spec: spec, Trace: t, Cast: c, malicious: map[string]bool{}}}
	for s, role := range c.roles {
		joined := t.Start
		if role == RoleNewcomer {
			joined = t.Start.Add(p.JoinAt)
		}
		r.current = append(r.current, r.addKey(s, role, joined))
	}
	r.switchAt = make([]time.Time, len(c.roles))
	return r
}

// run starts the observer, trusting every publisher's first key, replays
// the streams and returns the result.
func (r *runner) run(ctx context.Context) (*Result, error) {
	var trusted []string
	for _, k := range r.res.keys[1:] {
		trusted = append(trusted, k.peerID)
	}
	var published []netip.Prefix
	for _, pr := range r.trace.Published {
		published = append(published, pr.Prefix)
	}
	cfg, err := NodeSpec{Profile: r.spec.Profile, Trusted: trusted, Published: published}.Config()
	if err != nil {
		return nil, err
	}
	bans := NewBanLog()
	node, err := StartNode(ctx, r.res.keys[0].peerID, &cfg, r.trace.Start, bans.Record)
	if err != nil {
		return nil, err
	}
	r.node = node
	err = r.replay(streams(r.trace, r.spec.Model, r.cast, r.p, r.spec.Seed))
	err = errors.Join(err, node.Close(context.WithoutCancel(ctx)))
	if err != nil {
		return nil, fmt.Errorf("run %s: %w", r.spec, err)
	}
	if r.res.Episodes, err = bans.Episodes(r.trace.End()); err != nil {
		return nil, fmt.Errorf("run %s: %w", r.spec, err)
	}
	return r.res, nil
}

// addKey adds a key of actor and returns its index. A whitewasher's later
// keys are numbered from its slot.
func (r *runner) addKey(actor int, role Role, joined time.Time) int {
	label := fmt.Sprintf("slot-%02d", actor)
	if n := r.keysOf(actor); n > 0 {
		label = fmt.Sprintf("%s-%d", label, n)
	}
	r.res.keys = append(r.res.keys, keyRecord{peerID: NewKey(r.spec.Seed, label).PeerID, actor: actor, role: role, joined: joined})
	return len(r.res.keys) - 1
}

func (r *runner) keysOf(actor int) int {
	n := 0
	for _, k := range r.res.keys {
		if k.actor == actor {
			n++
		}
	}
	return n
}

// replay delivers the items in order, probing every probeInterval, until
// the end of the trace.
func (r *runner) replay(items []item) error {
	next := r.trace.Start
	end := r.trace.End()
	for _, it := range items {
		for ; !next.After(it.at); next = next.Add(probeInterval) {
			if err := r.probe(next); err != nil {
				return err
			}
		}
		if err := r.deliver(it); err != nil {
			return err
		}
	}
	for ; next.Before(end); next = next.Add(probeInterval) {
		if err := r.probe(next); err != nil {
			return err
		}
	}
	if err := r.node.Advance(end); err != nil {
		return err
	}
	return r.node.Sweep()
}

// probe sweeps the store at at and reads the weights: the honest
// publishers' mean, and the neutralization, convergence and burning of
// keys. A whitewasher whose key lost its weight switches keys after the
// switch delay.
func (r *runner) probe(at time.Time) error {
	if err := r.node.Advance(at); err != nil {
		return err
	}
	if err := r.node.Sweep(); err != nil {
		return err
	}
	sum, honest := 0.0, 0
	for i := range r.res.keys {
		k := &r.res.keys[i]
		if at.Before(k.joined) {
			continue
		}
		w := r.weight(r.node, k.peerID, at)
		switch {
		case w > 0:
			k.trusted = true
		case !k.defected.IsZero() && k.neutralized.IsZero():
			k.neutralized = at
		}
		if k.role == RoleNewcomer && k.converged.IsZero() && w >= convergedShare*Ceiling {
			k.converged = at
		}
		if k.role == RoleHonest || k.role == RoleNewcomer {
			sum += w / Ceiling
			honest++
		}
		if k.role == Role(ModelWhitewash) && w <= 0 && k.trusted && k.burned.IsZero() && r.current[k.actor] == i {
			k.burned = at
			r.current[k.actor], r.switchAt[k.actor] = -1, at.Add(r.p.SwitchDelay)
		}
	}
	for actor, due := range r.switchAt {
		if r.current[actor] == -1 && !at.Before(due) {
			r.current[actor] = r.addKey(actor, r.cast.roles[actor], at)
		}
	}
	if honest > 0 {
		r.res.weights = append(r.res.weights, weightSample{at: at, mean: sum / float64(honest)})
	}
	return nil
}

// deliver delivers it under its actor's current key.
func (r *runner) deliver(it item) error {
	k := r.current[it.actor]
	if k < 0 {
		return nil // a whitewasher between keys
	}
	key := &r.res.keys[k]
	if it.malicious && key.defected.IsZero() {
		key.defected = it.at
	}
	if it.kind == itemWithhold {
		return nil
	}
	if err := r.node.Advance(it.at); err != nil {
		return err
	}
	pub := publisherOf(key.peerID, r.cast.asns[it.actor])
	var ev *obieproto.Event
	switch it.kind {
	case itemVerdict:
		ev = newVerdict(it.id, pub, it.addr, it.at, it.confidence, it.ttl)
	case itemRevoke:
		ev = newRevocation(it.id, it.revokes, pub, it.addr, it.at)
	}
	if _, err := r.node.Deliver(ev); err != nil {
		return err
	}
	key.events = append(key.events, it.at)
	switch it.kind {
	case itemVerdict:
		if it.malicious {
			r.res.malicious[it.id] = true
		}
		r.verdicts[it.id] = len(r.res.received)
		r.res.received = append(r.res.received, received{at: it.at, key: k, addr: it.addr, confidence: it.confidence, end: it.at.Add(it.ttl)})
	case itemRevoke:
		if i, ok := r.verdicts[it.revokes]; ok && it.at.Before(r.res.received[i].end) {
			r.res.received[i].end = it.at
		}
	}
	return nil
}
