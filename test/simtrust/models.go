package simtrust

import (
	"cmp"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand/v2"
	"net/netip"
	"slices"
	"time"
)

// Model is a publisher behavior model (ADR 0034).
type Model string

// Behavior models. ModelHonest has no adversaries.
const (
	ModelHonest     Model = "honest"
	ModelNaive      Model = "naive"
	ModelCareful    Model = "careful"
	ModelOnOff      Model = "onoff"
	ModelWhitewash  Model = "whitewash"
	ModelSybil1ASN  Model = "sybil-1asn"
	ModelSybilMASN  Model = "sybil-masn"
	ModelSpies      Model = "spies"
	ModelSuppressor Model = "suppressor"
)

// Models are the behavior models in report order.
var Models = []Model{ModelHonest, ModelNaive, ModelCareful, ModelOnOff, ModelWhitewash, ModelSybil1ASN, ModelSybilMASN, ModelSpies, ModelSuppressor}

// Role is what a publisher does in a run.
type Role string

// Roles. An adversary's role is its model's name, except for the two
// roles of ModelSpies and the members of a Sybil coalition.
const (
	RoleObserver Role = "observer"
	RoleHonest   Role = "honest"
	RoleNewcomer Role = "newcomer"
	RoleSybil    Role = "sybil"
	RoleBad      Role = "bad"
	RoleSpy      Role = "spy"
)

// Adversary reports whether the role is an adversary's.
func (r Role) Adversary() bool {
	return r != RoleObserver && r != RoleHonest && r != RoleNewcomer
}

// poisoning is how an adversary reports its victims.
type poisoning struct {
	PerHour    float64
	Confidence float64
	TTL        time.Duration
}

// ModelParams are the parameters of the behavior models (ADR 0034).
type ModelParams struct {
	// Newcomers is the number of publishers, the first slots, that join at
	// JoinAt.
	Newcomers int
	JoinAt    time.Duration
	// Adversaries defect at DefectAt; before, they behave honestly.
	DefectAt time.Duration
	// Every publisher reports its operator's bans with HonestConfidence;
	// MisconfigRate of the reports name a CDN edge instead.
	HonestConfidence float64
	MisconfigRate    float64
	// PoolSize is the number of victims the adversaries of a run target.
	PoolSize int

	Naive, Careful, OnOff, Bad poisoning
	// OnOff poisons during the first Duty of every Period.
	Period time.Duration
	Duty   float64
	// A whitewasher goes on under a new key SwitchDelay after its key's
	// weight fell to 0.
	SwitchDelay time.Duration
	// A Sybil coalition reports CoalitionPerHour victims, every member
	// within CoalitionSpread, with Careful's confidence and TTL; its
	// members are in CoalitionASNs ASNs under ModelSybilMASN.
	CoalitionPerHour float64
	CoalitionSpread  time.Duration
	CoalitionASNs    int
	// Spies corroborate every poison verdict of the bad publishers after
	// a delay drawn from SpyDelay, with HonestConfidence and Bad's TTL.
	SpyDelay [2]time.Duration
	// A suppressor shields the attackers of ShieldShare of the networks:
	// it withholds WithholdShare of its bans of them and revokes the
	// others after a delay drawn from RevokeDelay.
	ShieldShare, WithholdShare float64
	RevokeDelay                [2]time.Duration
}

// DefaultModels returns the model parameters of the baseline.
func DefaultModels() ModelParams {
	return ModelParams{
		Newcomers:        2,
		JoinAt:           24 * time.Hour,
		DefectAt:         48 * time.Hour,
		HonestConfidence: 0.8,
		MisconfigRate:    0.01,
		PoolSize:         100,
		Naive:            poisoning{PerHour: 20, Confidence: 1, TTL: 7 * 24 * time.Hour},
		Careful:          poisoning{PerHour: 2, Confidence: 0.8, TTL: 24 * time.Hour},
		OnOff:            poisoning{PerHour: 8, Confidence: 0.8, TTL: time.Hour},
		Bad:              poisoning{PerHour: 4, Confidence: 1, TTL: 24 * time.Hour},
		Period:           24 * time.Hour,
		Duty:             0.25,
		SwitchDelay:      time.Hour,
		CoalitionPerHour: 2,
		CoalitionSpread:  2 * time.Minute,
		CoalitionASNs:    3,
		SpyDelay:         [2]time.Duration{time.Minute, 10 * time.Minute},
		ShieldShare:      1.0 / 3,
		WithholdShare:    0.5,
		RevokeDelay:      [2]time.Duration{time.Minute, 5 * time.Minute},
	}
}

// adversaryASN is the ASN of Sybil coalition member i of k ASNs.
func adversaryASN(i, k int) uint32 {
	return uint32(65000 + i%k) // #nosec G115 -- a handful of ASNs.
}

// itemKind is what a stream item does.
type itemKind int

const (
	// itemVerdict publishes a ban verdict.
	itemVerdict itemKind = iota
	// itemRevoke revokes the verdict of item revokes.
	itemRevoke
	// itemWithhold is a ban its publisher did not publish; nothing is
	// delivered, but it is a defection.
	itemWithhold
)

// item is an event of a publisher's stream; the publisher's key is
// chosen when it is delivered, since a whitewasher changes keys.
type item struct {
	at    time.Time
	actor int
	kind  itemKind
	addr  netip.Addr
	// confidence and ttl of a verdict.
	confidence float64
	ttl        time.Duration
	// malicious marks a defection: poison, a corroboration of poison or
	// a suppression.
	malicious bool
	// id is the event ID; revokes is the ID of the verdict a revocation
	// revokes.
	id, revokes string
}

// cast is who is who in a run: the role of every publisher slot (0 is
// the observer) and the ASN each announces.
type cast struct {
	roles []Role
	asns  []uint32
}

// adversaries returns the number of adversaries among n publishers at
// fraction f.
func adversaries(n int, f float64) int {
	return int(math.Round(float64(n) * f))
}

// castOf assigns the roles of model at adversary fraction f: the last
// round(N·f) publishers are adversaries, the first p.Newcomers of the
// honest ones newcomers.
func castOf(t *Trace, model Model, f float64, p ModelParams) cast {
	n := len(t.Operators) - 1
	a := 0
	if model != ModelHonest {
		a = adversaries(n, f)
	}
	c := cast{roles: make([]Role, n+1), asns: make([]uint32, n+1)}
	c.roles[0], c.asns[0] = RoleObserver, t.Operators[0].ASN
	for s := 1; s <= n; s++ {
		c.asns[s] = t.Operators[s].ASN
		switch {
		case s <= n-a && s <= p.Newcomers:
			c.roles[s] = RoleNewcomer
		case s <= n-a:
			c.roles[s] = RoleHonest
		default:
			c.roles[s] = adversaryRole(model, s-(n-a)-1, a)
		}
		if model == ModelSybil1ASN && c.roles[s].Adversary() {
			c.asns[s] = adversaryASN(0, 1)
		}
		if model == ModelSybilMASN && c.roles[s].Adversary() {
			c.asns[s] = adversaryASN(s-(n-a)-1, p.CoalitionASNs)
		}
	}
	return c
}

// adversaryRole is the role of adversary i of a under model.
func adversaryRole(model Model, i, a int) Role {
	switch model {
	case ModelSybil1ASN, ModelSybilMASN:
		return RoleSybil
	case ModelSpies:
		if i < (a+1)/2 {
			return RoleBad
		}
		return RoleSpy
	default:
		return Role(model)
	}
}

// streams builds the items every publisher of the cast sends, ordered by
// time. The honest part of a publisher's stream depends only on the seed
// and its slot, whatever its role, so that every configuration of a seed
// replays the same bans (common random numbers).
func streams(t *Trace, model Model, c cast, p ModelParams, seed uint64) []item {
	w := newWorldIndex(t)
	b := &builder{ids: idSource{rng: rngOf(seed, "ids", 0)}}
	shielded := shieldedNetworks(w, p.ShieldShare, rngOf(seed, "shield", 0).Uint64())
	for s := range c.roles {
		b.honest(t, w, s, c.roles[s], shielded, p, rngOf(seed, "honest", s))
	}
	pools := victimPools(w, p.PoolSize, rngOf(seed, "pools", 0))
	defect := t.Start.Add(p.DefectAt)
	end := t.End()
	var members []int
	for s, r := range c.roles {
		if r.Adversary() {
			members = append(members, s)
		}
	}
	rng := rngOf(seed, string(model), 0)
	switch model {
	case ModelNaive, ModelWhitewash:
		for _, s := range members {
			b.poison(s, pools.all, p.Naive, defect, end, rng)
		}
	case ModelCareful:
		for _, s := range members {
			b.poison(s, pools.unpublished, p.Careful, defect, end, rng)
		}
	case ModelOnOff:
		on := time.Duration(p.Duty * float64(p.Period))
		for from := defect; from.Before(end); from = from.Add(p.Period) {
			for _, s := range members {
				b.poison(s, pools.unpublished, p.OnOff, from, minTime(from.Add(on), end), rng)
			}
		}
	case ModelSybil1ASN, ModelSybilMASN:
		b.coalition(members, pools.unpublished, p, defect, end, rng)
	case ModelSpies:
		b.spies(c, pools.all, p, defect, end, rng)
	case ModelHonest, ModelSuppressor:
		// Their streams are honest ones.
	}
	// A corroboration or revocation after the end is never sent.
	items := slices.DeleteFunc(b.items, func(it item) bool { return !it.at.Before(end) })
	slices.SortStableFunc(items, func(x, y item) int {
		return cmp.Or(x.at.Compare(y.at), cmp.Compare(x.actor, y.actor))
	})
	return items
}

// builder collects the items of a run, each with its event ID.
type builder struct {
	ids   idSource
	items []item
}

// add adds it with a new event ID and returns that ID.
func (b *builder) add(it item) string {
	it.at = it.at.Truncate(time.Second)
	it.id = b.ids.next(it.at)
	b.items = append(b.items, it)
	return it.id
}

// rngOf returns the generator of one part of a run with seed.
func rngOf(seed uint64, part string, slot int) *rand.Rand {
	h := fnv.New64a()
	_, _ = h.Write([]byte(part))
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(slot)) // #nosec G115 -- slot >= 0.
	_, _ = h.Write(b[:])
	return rand.New(rand.NewPCG(seed, h.Sum64())) // #nosec G404 -- a reproducible simulation, no secret.
}

// worldIndex indexes a trace's addresses.
type worldIndex struct {
	class       map[netip.Addr]Class
	network     map[netip.Addr]string
	cdn, benign []netip.Addr
	unpublished []netip.Addr
	published   []netip.Prefix
}

func newWorldIndex(t *Trace) *worldIndex {
	w := &worldIndex{class: map[netip.Addr]Class{}, network: map[netip.Addr]string{}}
	for _, r := range t.Published {
		w.published = append(w.published, r.Prefix)
	}
	for _, a := range t.Addresses {
		w.class[a.Addr] = a.Class
		// An address's network is its ASN, or its /48 if the ASN is
		// unknown, as in an imported trace.
		w.network[a.Addr] = fmt.Sprint(a.ASN)
		if a.ASN == 0 {
			w.network[a.Addr] = netip.PrefixFrom(a.Addr, 48).Masked().String()
		}
		if !a.Class.Benign() {
			continue
		}
		w.benign = append(w.benign, a.Addr)
		if a.Class == ClassCDN {
			w.cdn = append(w.cdn, a.Addr)
		}
		if !w.isPublished(a.Addr) {
			w.unpublished = append(w.unpublished, a.Addr)
		}
	}
	return w
}

// isPublished reports whether a is in a published range.
func (w *worldIndex) isPublished(a netip.Addr) bool {
	return slices.ContainsFunc(w.published, func(p netip.Prefix) bool { return p.Contains(a) })
}

// honest adds what publisher s reports of its operator's bans, from
// joining: verdicts with the honest confidence and the operator's
// bantime, some naming a CDN edge. A suppressor withholds or revokes its
// bans of attackers in shielded networks from its defection.
func (b *builder) honest(t *Trace, w *worldIndex, s int, role Role, shielded map[string]bool, p ModelParams, rng *rand.Rand) {
	join := t.Start
	if role == RoleNewcomer {
		join = t.Start.Add(p.JoinAt)
	}
	defect := t.Start.Add(p.DefectAt)
	for _, o := range t.Observations {
		if o.Operator != s {
			continue
		}
		// The draws do not depend on the role, so that every role sees
		// the same misconfigurations.
		misconfigured, edge := rng.Float64() < p.MisconfigRate, rng.IntN(max(len(w.cdn), 1))
		withhold, delay := rng.Float64() < p.WithholdShare, between(rng, p.RevokeDelay)
		if o.At.Before(join) {
			continue
		}
		v := item{at: o.At, actor: s, kind: itemVerdict, addr: o.Addr, confidence: p.HonestConfidence, ttl: t.Operators[s].Bantime}
		if misconfigured && len(w.cdn) > 0 {
			v.addr = w.cdn[edge]
		}
		suppress := role == Role(ModelSuppressor) && !o.At.Before(defect) &&
			w.class[v.addr] == ClassAttacker && shielded[w.network[v.addr]]
		switch {
		case !suppress:
			b.add(v)
		case withhold:
			v.kind, v.malicious = itemWithhold, true
			b.add(v)
		default:
			v.malicious = true
			id := b.add(v)
			b.add(item{at: v.at.Add(delay), actor: s, kind: itemRevoke, addr: v.addr, malicious: true, revokes: id})
		}
	}
}

// shieldedNetworks picks share of the attackers' networks with salt.
func shieldedNetworks(w *worldIndex, share float64, salt uint64) map[string]bool {
	out := map[string]bool{}
	for a, class := range w.class {
		if class != ClassAttacker {
			continue
		}
		h := fnv.New64a()
		_, _ = h.Write([]byte(w.network[a]))
		if x := h.Sum64() ^ salt; float64(x%1000) < share*1000 {
			out[w.network[a]] = true
		}
	}
	return out
}

// pools are the victims the adversaries of a run target: any benign
// address, or only those outside the published ranges.
type pools struct {
	all, unpublished []netip.Addr
}

func victimPools(w *worldIndex, size int, rng *rand.Rand) pools {
	pick := func(from []netip.Addr) []netip.Addr {
		out := slices.Clone(from)
		rng.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
		return out[:min(size, len(out))]
	}
	return pools{all: pick(w.benign), unpublished: pick(w.unpublished)}
}

// poison adds the poison verdicts of publisher s on victims from pool,
// arriving as a Poisson process in [from, to).
func (b *builder) poison(s int, pool []netip.Addr, how poisoning, from, to time.Time, rng *rand.Rand) {
	for _, v := range arrivals(pool, how.PerHour, from, to, rng) {
		b.add(item{at: v.at, actor: s, kind: itemVerdict, addr: v.addr, confidence: how.Confidence, ttl: how.TTL, malicious: true})
	}
}

// target is a victim chosen at a time.
type target struct {
	at   time.Time
	addr netip.Addr
}

// arrivals draws victims from pool as a Poisson process of perHour in
// [from, to).
func arrivals(pool []netip.Addr, perHour float64, from, to time.Time, rng *rand.Rand) []target {
	if len(pool) == 0 {
		return nil
	}
	var out []target
	for at := from.Add(exp(rng, perHour)); at.Before(to); at = at.Add(exp(rng, perHour)) {
		out = append(out, target{at, pool[rng.IntN(len(pool))]})
	}
	return out
}

// coalition adds the Sybil coalition's reports: every member reports each
// victim within the spread.
func (b *builder) coalition(members []int, pool []netip.Addr, p ModelParams, from, to time.Time, rng *rand.Rand) {
	for _, v := range arrivals(pool, p.CoalitionPerHour, from, to, rng) {
		for _, s := range members {
			b.add(item{at: v.at.Add(between(rng, [2]time.Duration{0, p.CoalitionSpread})), actor: s, kind: itemVerdict,
				addr: v.addr, confidence: p.Careful.Confidence, ttl: p.Careful.TTL, malicious: true})
		}
	}
}

// spies adds the bad publishers' poison and the spies' corroboration of
// it.
func (b *builder) spies(c cast, pool []netip.Addr, p ModelParams, from, to time.Time, rng *rand.Rand) {
	for s, r := range c.roles {
		if r != RoleBad {
			continue
		}
		for _, v := range arrivals(pool, p.Bad.PerHour, from, to, rng) {
			b.add(item{at: v.at, actor: s, kind: itemVerdict, addr: v.addr, confidence: p.Bad.Confidence, ttl: p.Bad.TTL, malicious: true})
			for spy, r := range c.roles {
				if r == RoleSpy {
					b.add(item{at: v.at.Add(between(rng, p.SpyDelay)), actor: spy, kind: itemVerdict, addr: v.addr,
						confidence: p.HonestConfidence, ttl: p.Bad.TTL, malicious: true})
				}
			}
		}
	}
}

// exp draws the time to the next arrival of a Poisson process with
// perHour arrivals an hour.
func exp(rng *rand.Rand, perHour float64) time.Duration {
	return time.Duration(rng.ExpFloat64() / perHour * float64(time.Hour))
}

// between draws a duration uniformly from [r[0], r[1]).
func between(rng *rand.Rand, r [2]time.Duration) time.Duration {
	return r[0] + time.Duration(rng.Float64()*float64(r[1]-r[0]))
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
