package simtrust

import (
	"math"
	"net/netip"
	"slices"
	"sort"
	"testing"
	"time"
)

// testWorld is a synthetic world long enough for the adversaries to
// defect.
func testWorld(t *testing.T) *Trace {
	t.Helper()
	p := DefaultWorld()
	p.Hours = 96
	return GenerateWorld(p, 11, testStart)
}

func TestCast(t *testing.T) {
	w := testWorld(t)
	p := DefaultModels()
	for _, tt := range []struct {
		model Model
		f     float64
		want  map[Role]int
	}{
		{ModelHonest, 0.4, map[Role]int{RoleObserver: 1, RoleNewcomer: 2, RoleHonest: 18}},
		{ModelNaive, 0.1, map[Role]int{RoleObserver: 1, RoleNewcomer: 2, RoleHonest: 16, Role(ModelNaive): 2}},
		{ModelSuppressor, 0.3, map[Role]int{RoleObserver: 1, RoleNewcomer: 2, RoleHonest: 12, Role(ModelSuppressor): 6}},
		{ModelSybil1ASN, 0.4, map[Role]int{RoleObserver: 1, RoleNewcomer: 2, RoleHonest: 10, RoleSybil: 8}},
		{ModelSpies, 0.3, map[Role]int{RoleObserver: 1, RoleNewcomer: 2, RoleHonest: 12, RoleBad: 3, RoleSpy: 3}},
		{ModelSpies, 0.1, map[Role]int{RoleObserver: 1, RoleNewcomer: 2, RoleHonest: 16, RoleBad: 1, RoleSpy: 1}},
	} {
		c := castOf(w, tt.model, tt.f, p)
		got := map[Role]int{}
		for _, r := range c.roles {
			got[r]++
		}
		if len(got) != len(tt.want) {
			t.Errorf("%s at %.0f %%: roles %v, want %v", tt.model, 100*tt.f, got, tt.want)
			continue
		}
		for r, n := range tt.want {
			if got[r] != n {
				t.Errorf("%s at %.0f %%: roles %v, want %v", tt.model, 100*tt.f, got, tt.want)
				break
			}
		}
		if c.roles[1] != RoleNewcomer || c.roles[20].Adversary() != (tt.model != ModelHonest) {
			t.Errorf("%s: slot 1 is %s and slot 20 %s; want a newcomer and the last adversary", tt.model, c.roles[1], c.roles[20])
		}
	}
	asns := func(model Model) map[uint32]bool {
		c := castOf(w, model, 0.3, p)
		out := map[uint32]bool{}
		for s, r := range c.roles {
			if r.Adversary() {
				out[c.asns[s]] = true
			}
		}
		return out
	}
	if n1, nm := len(asns(ModelSybil1ASN)), len(asns(ModelSybilMASN)); n1 != 1 || nm != p.CoalitionASNs {
		t.Errorf("the coalition announces %d ASNs under sybil-1asn and %d under sybil-masn, want 1 and %d", n1, nm, p.CoalitionASNs)
	}
}

// itemsOf returns the items of actor.
func itemsOf(items []item, actor int) []item {
	var out []item
	for _, it := range items {
		if it.actor == actor {
			out = append(out, it)
		}
	}
	return out
}

func TestStreamsAreOrderedAndReproducible(t *testing.T) {
	w := testWorld(t)
	p := DefaultModels()
	c := castOf(w, ModelSpies, 0.2, p)
	a, b := streams(w, ModelSpies, c, p, 3), streams(w, ModelSpies, c, p, 3)
	if !slices.EqualFunc(a, b, func(x, y item) bool { return x == y }) {
		t.Error("one seed gave two streams")
	}
	if !slices.IsSortedFunc(a, func(x, y item) int { return x.at.Compare(y.at) }) {
		t.Error("the stream is not ordered by time")
	}
	ids := map[string]bool{}
	for _, it := range a {
		if ids[it.id] || it.at.Nanosecond() != 0 {
			t.Fatalf("item %+v: duplicate ID or a time within a second", it)
		}
		ids[it.id] = true
	}
}

// TestHonestPartIsCommon checks the common random numbers: a publisher's
// honest reports are the same whatever the model, and so are an
// adversary's until it defects.
func TestHonestPartIsCommon(t *testing.T) {
	w := testWorld(t)
	p := DefaultModels()
	honest := func(model Model, slot int) []item {
		var out []item
		for _, it := range itemsOf(streams(w, model, castOf(w, model, 0.4, p), p, 5), slot) {
			if !it.malicious && it.at.Before(w.Start.Add(p.DefectAt)) {
				it.id = ""
				out = append(out, it)
			}
		}
		return out
	}
	for _, slot := range []int{0, 5, 20} {
		base := honest(ModelHonest, slot)
		if len(base) == 0 {
			t.Fatalf("slot %d reports nothing", slot)
		}
		for _, model := range []Model{ModelNaive, ModelSuppressor, ModelSybilMASN} {
			if got := honest(model, slot); !slices.Equal(got, base) {
				t.Errorf("slot %d: its honest reports under %s differ from those under %s", slot, model, ModelHonest)
			}
		}
	}
}

func TestHonestStream(t *testing.T) {
	w := testWorld(t)
	p := DefaultModels()
	p.MisconfigRate = 0.2
	c := castOf(w, ModelHonest, 0, p)
	items := streams(w, ModelHonest, c, p, 1)
	idx := newWorldIndex(w)
	for _, slot := range []int{0, 1, 7} {
		var bans int
		join := w.Start
		if c.roles[slot] == RoleNewcomer {
			join = w.Start.Add(p.JoinAt)
		}
		for _, o := range w.Observations {
			if o.Operator == slot && !o.At.Before(join) {
				bans++
			}
		}
		mine := itemsOf(items, slot)
		if len(mine) != bans {
			t.Errorf("slot %d (%s): %d reports of %d bans", slot, c.roles[slot], len(mine), bans)
		}
		misconfigured := 0
		for _, it := range mine {
			if it.kind != itemVerdict || it.malicious || it.confidence != p.HonestConfidence || it.ttl != w.Operators[slot].Bantime || it.at.Before(join) {
				t.Fatalf("slot %d: report %+v, want an honest verdict with the operator's bantime after %s", slot, it, join)
			}
			if idx.class[it.addr] == ClassCDN {
				misconfigured++
			}
		}
		if share := float64(misconfigured) / float64(len(mine)); math.Abs(share-p.MisconfigRate) > 0.08 {
			t.Errorf("slot %d: %.2f of the reports name a CDN edge, want about %.2f", slot, share, p.MisconfigRate)
		}
	}
}

// maliciousOf returns the malicious items and checks that none comes
// before the defection.
func maliciousOf(t *testing.T, w *Trace, p ModelParams, items []item) []item {
	t.Helper()
	var out []item
	for _, it := range items {
		if !it.malicious {
			continue
		}
		if it.at.Before(w.Start.Add(p.DefectAt)) {
			t.Fatalf("malicious item %+v before the defection", it)
		}
		out = append(out, it)
	}
	if len(out) == 0 {
		t.Fatal("no malicious item")
	}
	return out
}

func TestPoisoners(t *testing.T) {
	w := testWorld(t)
	p := DefaultModels()
	idx := newWorldIndex(w)
	hours := float64(w.Hours) - p.DefectAt.Hours()
	for _, tt := range []struct {
		model Model
		how   poisoning
		// published: whether victims may be in published ranges.
		published bool
		hours     float64
	}{
		{ModelNaive, p.Naive, true, hours},
		{ModelWhitewash, p.Naive, true, hours},
		{ModelCareful, p.Careful, false, hours},
		{ModelOnOff, p.OnOff, false, math.Ceil(hours/p.Period.Hours()) * p.Duty * p.Period.Hours()},
	} {
		c := castOf(w, tt.model, 0.2, p)
		bad := maliciousOf(t, w, p, streams(w, tt.model, c, p, 2))
		victims := map[netip.Addr]bool{}
		for _, it := range bad {
			if !c.roles[it.actor].Adversary() || it.kind != itemVerdict || it.confidence != tt.how.Confidence || it.ttl != tt.how.TTL {
				t.Fatalf("%s: poison %+v, want a verdict of an adversary with %+v", tt.model, it, tt.how)
			}
			if !idx.class[it.addr].Benign() || (!tt.published && idx.isPublished(it.addr)) {
				t.Fatalf("%s: victim %s is %s, published %v", tt.model, it.addr, idx.class[it.addr], idx.isPublished(it.addr))
			}
			if tt.model == ModelOnOff && it.at.Sub(w.Start.Add(p.DefectAt))%p.Period >= time.Duration(p.Duty*float64(p.Period)) {
				t.Fatalf("on-off poison at %s, in an off phase", it.at)
			}
			victims[it.addr] = true
		}
		if len(victims) > p.PoolSize {
			t.Errorf("%s: %d victims, want at most the pool of %d", tt.model, len(victims), p.PoolSize)
		}
		want := tt.how.PerHour * tt.hours * 4 // four adversaries
		if got := float64(len(bad)); math.Abs(got-want) > 5*math.Sqrt(want) {
			t.Errorf("%s: %v poison verdicts, want about %v", tt.model, got, want)
		}
	}
}

func TestCoalitionReportsTogether(t *testing.T) {
	w := testWorld(t)
	p := DefaultModels()
	c := castOf(w, ModelSybil1ASN, 0.2, p)
	bad := maliciousOf(t, w, p, streams(w, ModelSybil1ASN, c, p, 4))
	byVictim := map[netip.Addr][]item{}
	for _, it := range bad {
		byVictim[it.addr] = append(byVictim[it.addr], it)
	}
	for addr, its := range byVictim {
		// A victim chosen twice has two reports of every member.
		if len(its)%4 != 0 {
			t.Fatalf("victim %s has %d reports, want a multiple of the 4 members", addr, len(its))
		}
		sort.Slice(its, func(i, j int) bool { return its[i].at.Before(its[j].at) })
		for i := 0; i < len(its); i += 4 {
			actors := map[int]bool{}
			for _, it := range its[i : i+4] {
				actors[it.actor] = true
			}
			if len(actors) != 4 || its[i+3].at.Sub(its[i].at) > p.CoalitionSpread {
				t.Errorf("victim %s: reports %+v, want every member within %s", addr, its[i:i+4], p.CoalitionSpread)
			}
		}
	}
}

func TestSpiesCorroborate(t *testing.T) {
	w := testWorld(t)
	p := DefaultModels()
	c := castOf(w, ModelSpies, 0.2, p) // 2 bad, 2 spies
	bad := maliciousOf(t, w, p, streams(w, ModelSpies, c, p, 6))
	var poison, corroborations int
	for _, it := range bad {
		if c.roles[it.actor] == RoleBad {
			poison++
			continue
		}
		corroborations++
		// Its bad verdict came within the spies' delay before.
		found := false
		for _, b := range bad {
			d := it.at.Sub(b.at)
			found = found || (c.roles[b.actor] == RoleBad && b.addr == it.addr && d >= 0 && d <= p.SpyDelay[1])
		}
		if !found || it.confidence != p.HonestConfidence {
			t.Fatalf("corroboration %+v without a bad verdict before it", it)
		}
	}
	if corroborations != 2*poison {
		t.Errorf("%d corroborations of %d poison verdicts, want two spies each", corroborations, poison)
	}
}

func TestSuppressor(t *testing.T) {
	w := testWorld(t)
	p := DefaultModels()
	c := castOf(w, ModelSuppressor, 0.2, p)
	items := streams(w, ModelSuppressor, c, p, 8)
	idx := newWorldIndex(w)
	byID := map[string]item{}
	for _, it := range items {
		byID[it.id] = it
	}
	var withheld, revoked int
	for _, it := range maliciousOf(t, w, p, items) {
		if c.roles[it.actor] != Role(ModelSuppressor) || idx.class[it.addr] != ClassAttacker {
			t.Fatalf("suppression %+v of a %s by a %s", it, idx.class[it.addr], c.roles[it.actor])
		}
		switch it.kind {
		case itemWithhold:
			withheld++
		case itemRevoke:
			revoked++
			v, ok := byID[it.revokes]
			if d := it.at.Sub(v.at); !ok || v.addr != it.addr || d < p.RevokeDelay[0]-time.Second || d > p.RevokeDelay[1] {
				t.Errorf("revocation %+v of %+v, want one of its verdict within %v", it, v, p.RevokeDelay)
			}
		}
	}
	if withheld == 0 || revoked == 0 || math.Abs(float64(withheld-revoked)) > 4*math.Sqrt(float64(withheld+revoked)) {
		t.Errorf("%d withheld and %d revoked, want about half each", withheld, revoked)
	}
}
