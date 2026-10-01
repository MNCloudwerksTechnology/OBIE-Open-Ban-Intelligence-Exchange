package simtrust

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/rand/v2"
	"net/netip"
	"time"
)

// firstASN is the first synthetic ASN: the private range of RFC 6996.
const firstASN = 64512

// bantimeShare is a Fail2Ban bantime and the share of operators using it.
type bantimeShare struct {
	Bantime time.Duration
	Share   float64
}

// WorldParams are the parameters of the synthetic world (ADR 0034).
type WorldParams struct {
	Hours int
	// Operators counts the observer and the publishers.
	Operators int
	// SizeSigma is the σ of the operators' log-normal sizes; attackers
	// pick their targets in proportion to size.
	SizeSigma float64
	Bantimes  []bantimeShare

	AttackersPerHour float64
	// SingleShare of the attackers hit one operator, CampaignShare run a
	// campaign against CampaignTargets operators within CampaignWindow;
	// the others scan every operator with ScanProbability over a spread
	// drawn from ScanSpread.
	SingleShare, CampaignShare float64
	CampaignTargets            [2]int
	CampaignWindow             time.Duration
	ScanProbability            float64
	ScanSpread                 [2]time.Duration
	// ReturnShare of the attackers come back once, ReturnAfter later, and
	// attack the same operators again.
	ReturnShare float64
	ReturnAfter [2]time.Duration
	// Fail2Ban bans an attacker at a hit with DetectProbability, after a
	// delay drawn from DetectDelay.
	DetectProbability float64
	DetectDelay       [2]time.Duration
	// HostingShare of the attackers come from hosting ASNs,
	// ResidentialShare from residential ones, the others from behind a
	// shared NAT address.
	HostingShare, ResidentialShare float64

	HostingASNs, ResidentialASNs, CarrierASNs int
	// The published ranges: CDNs × RangesPerCDN ranges of EdgesPerRange
	// edge addresses, and Crawlers × RangesPerCrawler ranges of
	// CrawlersPerRange crawler addresses.
	CDNs, RangesPerCDN, EdgesPerRange            int
	Crawlers, RangesPerCrawler, CrawlersPerRange int
	Customers, NATs                              int
}

// DefaultWorld returns the synthetic world of the baseline.
func DefaultWorld() WorldParams {
	return WorldParams{
		Hours:     168,
		Operators: 21,
		SizeSigma: 1,
		Bantimes: []bantimeShare{
			{10 * time.Minute, 0.25}, {time.Hour, 0.25}, {24 * time.Hour, 0.35}, {7 * 24 * time.Hour, 0.15},
		},
		AttackersPerHour:  30,
		SingleShare:       0.6,
		CampaignShare:     0.3,
		CampaignTargets:   [2]int{4, 6},
		CampaignWindow:    30 * time.Minute,
		ScanProbability:   0.7,
		ScanSpread:        [2]time.Duration{time.Hour, 12 * time.Hour},
		ReturnShare:       0.25,
		ReturnAfter:       [2]time.Duration{24 * time.Hour, 72 * time.Hour},
		DetectProbability: 0.9,
		DetectDelay:       [2]time.Duration{time.Minute, 10 * time.Minute},
		HostingShare:      0.55,
		ResidentialShare:  0.35,
		HostingASNs:       40,
		ResidentialASNs:   30,
		CarrierASNs:       5,
		CDNs:              3, RangesPerCDN: 4, EdgesPerRange: 25,
		Crawlers: 2, RangesPerCrawler: 3, CrawlersPerRange: 20,
		Customers: 2000,
		NATs:      200,
	}
}

// worldGen draws a world from one seed.
type worldGen struct {
	p   WorldParams
	rng *rand.Rand
	// asns counts the ASNs allocated; asnBase is the index of the first.
	asns, asnBase int
	used          map[netip.Addr]bool
	trace         *Trace
}

func newWorldGen(p WorldParams, seed uint64, asnBase int, t *Trace) *worldGen {
	g := &worldGen{
		p:       p,
		rng:     rand.New(rand.NewPCG(seed, 0x5157)), // #nosec G404 -- a reproducible simulation, no secret.
		asnBase: asnBase,
		used:    map[netip.Addr]bool{},
		trace:   t,
	}
	for _, a := range t.Addresses {
		g.used[a.Addr] = true
	}
	return g
}

// GenerateWorld draws the synthetic trace of seed from p, starting at
// start.
func GenerateWorld(p WorldParams, seed uint64, start time.Time) *Trace {
	t := &Trace{Source: fmt.Sprintf("synthetic world, seed %d", seed), Start: start, Hours: p.Hours}
	g := newWorldGen(p, seed, 0, t)
	sizes := g.operators()
	g.addPublished(ClassCDN, p.CDNs, p.RangesPerCDN, p.EdgesPerRange)
	g.addPublished(ClassCrawler, p.Crawlers, p.RangesPerCrawler, p.CrawlersPerRange)
	nats := g.addHosts(ClassNAT, g.allocASNs(p.CarrierASNs), p.NATs)
	hosting, residential := g.allocASNs(p.HostingASNs), g.allocASNs(p.ResidentialASNs)
	g.addHosts(ClassCustomer, residential, p.Customers)
	g.attacks(sizes, hosting, residential, nats)
	return t
}

// operators adds the operators and returns their sizes.
func (g *worldGen) operators() []float64 {
	sizes := make([]float64, g.p.Operators)
	for i := range sizes {
		asn := g.allocASNs(1)[0]
		g.trace.Operators = append(g.trace.Operators, Operator{
			Name: fmt.Sprintf("op%02d", i), ASN: asnNumber(asn), Bantime: g.bantime(),
		})
		sizes[i] = math.Exp(g.p.SizeSigma * g.rng.NormFloat64())
	}
	return sizes
}

func (g *worldGen) bantime() time.Duration {
	x := g.rng.Float64()
	for _, b := range g.p.Bantimes {
		if x < b.Share {
			return b.Bantime
		}
		x -= b.Share
	}
	return g.p.Bantimes[len(g.p.Bantimes)-1].Bantime
}

// allocASNs allocates n ASNs and returns their indices.
func (g *worldGen) allocASNs(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = g.asnBase + g.asns
		g.asns++
	}
	return out
}

// asnNumber is the ASN of the ASN with index i.
func asnNumber(i int) uint32 {
	return uint32(firstASN + i) // #nosec G115 -- a few thousand ASNs at most.
}

// asnAddr returns the address host in subnet of the ASN with index i: the
// ASN is 2001:db8:<i+1>::/48 and the subnet a /64 in it.
func asnAddr(i int, subnet, host uint16) netip.Addr {
	b := [16]byte{0x20, 0x01, 0x0d, 0xb8}
	binary.BigEndian.PutUint16(b[4:], uint16(i+1)) // #nosec G115 -- fewer than 65,535 ASNs.
	binary.BigEndian.PutUint16(b[6:], subnet)
	binary.BigEndian.PutUint16(b[14:], host)
	return netip.AddrFrom16(b)
}

// addPublished adds n owners of class, each an ASN with ranges published
// /64 ranges of hosts addresses.
func (g *worldGen) addPublished(class Class, n, ranges, hosts int) {
	for _, asn := range g.allocASNs(n) {
		for r := 1; r <= ranges; r++ {
			first := asnAddr(asn, uint16(r), 0) // #nosec G115 -- a handful of ranges.
			g.trace.Published = append(g.trace.Published, Range{Prefix: netip.PrefixFrom(first, 64), Class: class})
			for h := 1; h <= hosts; h++ {
				g.add(asnAddr(asn, uint16(r), uint16(h)), class, asnNumber(asn)) // #nosec G115 -- a few dozen hosts.
			}
		}
	}
}

// addHosts adds n addresses of class at random in the ASNs with indices
// asns and returns them.
func (g *worldGen) addHosts(class Class, asns []int, n int) []netip.Addr {
	out := make([]netip.Addr, n)
	for i := range out {
		asn := asns[g.rng.IntN(len(asns))]
		out[i] = g.randomAddr(asn)
		g.add(out[i], class, asnNumber(asn))
	}
	return out
}

// randomAddr returns an unused random address in the ASN with index asn,
// outside the published subnets.
func (g *worldGen) randomAddr(asn int) netip.Addr {
	for {
		a := asnAddr(asn, uint16(0x100+g.rng.IntN(0xff00)), uint16(1+g.rng.IntN(0xfffe))) // #nosec G115 -- within 16 bits.
		if !g.used[a] {
			return a
		}
	}
}

func (g *worldGen) add(a netip.Addr, class Class, asn uint32) {
	g.used[a] = true
	g.trace.Addresses = append(g.trace.Addresses, Address{Addr: a, Class: class, ASN: asn})
}

// hit is an attack on an operator.
type hit struct {
	at       time.Duration
	operator int
}

// attacks adds the attackers and the operators' bans of them.
func (g *worldGen) attacks(sizes []float64, hosting, residential []int, nats []netip.Addr) {
	p := g.p
	hours := time.Duration(p.Hours) * time.Hour
	var obs []Observation
	attackers := map[netip.Addr]uint32{}
	for arrival := g.exp(p.AttackersPerHour); arrival < hours; arrival += g.exp(p.AttackersPerHour) {
		src, asn := g.attackSource(hosting, residential, nats)
		hits := g.hits(g.rng.Float64(), arrival, sizes)
		// A returning attacker attacks the same operators again.
		visits := []time.Duration{0}
		if g.rng.Float64() < p.ReturnShare {
			visits = append(visits, g.between(p.ReturnAfter))
		}
		for _, v := range visits {
			for _, h := range hits {
				if g.rng.Float64() >= p.DetectProbability {
					continue
				}
				at := (v + h.at + g.between(p.DetectDelay)).Truncate(time.Second)
				if at < hours {
					obs = append(obs, Observation{At: g.trace.Start.Add(at), Operator: h.operator, Addr: src})
					if asn != 0 {
						attackers[src] = asn
					}
				}
			}
		}
	}
	for a, asn := range attackers {
		g.add(a, ClassAttacker, asn)
	}
	sortAddresses(g.trace.Addresses)
	g.trace.Observations = withoutRebans(obs, g.trace.Operators)
}

// attackSource returns a new attacker address and its ASN, or a shared NAT
// address and 0.
func (g *worldGen) attackSource(hosting, residential []int, nats []netip.Addr) (netip.Addr, uint32) {
	x := g.rng.Float64()
	var asns []int
	switch {
	case x < g.p.HostingShare:
		asns = hosting
	case x < g.p.HostingShare+g.p.ResidentialShare:
		asns = residential
	default:
		return nats[g.rng.IntN(len(nats))], 0
	}
	asn := asns[g.rng.IntN(len(asns))]
	a := g.randomAddr(asn)
	g.used[a] = true
	return a, asnNumber(asn)
}

// hits returns the attacks of one visit starting at v; pattern in [0, 1)
// selects single, campaign or scan.
func (g *worldGen) hits(pattern float64, v time.Duration, sizes []float64) []hit {
	p := g.p
	switch {
	case pattern < p.SingleShare:
		return []hit{{v, g.pickOperators(sizes, 1)[0]}}
	case pattern < p.SingleShare+p.CampaignShare:
		n := p.CampaignTargets[0] + g.rng.IntN(p.CampaignTargets[1]-p.CampaignTargets[0]+1)
		var out []hit
		for _, o := range g.pickOperators(sizes, n) {
			out = append(out, hit{v + g.between([2]time.Duration{0, p.CampaignWindow}), o})
		}
		return out
	default:
		spread := g.between(p.ScanSpread)
		var out []hit
		for o := range sizes {
			if g.rng.Float64() < p.ScanProbability {
				out = append(out, hit{v + g.between([2]time.Duration{0, spread}), o})
			}
		}
		return out
	}
}

// pickOperators draws n distinct operators in proportion to their sizes.
func (g *worldGen) pickOperators(sizes []float64, n int) []int {
	left := make([]int, len(sizes))
	for i := range left {
		left[i] = i
	}
	var out []int
	for len(out) < n && len(left) > 0 {
		total := 0.0
		for _, o := range left {
			total += sizes[o]
		}
		x := g.rng.Float64() * total
		i := 0
		for ; i < len(left)-1 && x >= sizes[left[i]]; i++ {
			x -= sizes[left[i]]
		}
		out = append(out, left[i])
		left = append(left[:i], left[i+1:]...)
	}
	return out
}

// exp draws the time to the next arrival of a Poisson process with
// perHour arrivals an hour.
func (g *worldGen) exp(perHour float64) time.Duration {
	return time.Duration(g.rng.ExpFloat64() / perHour * float64(time.Hour))
}

// between draws a duration uniformly from [r[0], r[1]).
func (g *worldGen) between(r [2]time.Duration) time.Duration {
	return r[0] + time.Duration(g.rng.Float64()*float64(r[1]-r[0]))
}

// withoutRebans orders the observations by time and drops those of an
// address that the operator's Fail2Ban still bans: it only bans again
// once its bantime is over.
func withoutRebans(obs []Observation, operators []Operator) []Observation {
	sortObservations(obs)
	type key struct {
		operator int
		addr     netip.Addr
	}
	until := map[key]time.Time{}
	out := obs[:0]
	for _, o := range obs {
		k := key{o.Operator, o.Addr}
		if o.At.Before(until[k]) {
			continue
		}
		until[k] = o.At.Add(operators[o.Operator].Bantime)
		out = append(out, o)
	}
	return out
}
