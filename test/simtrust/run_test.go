package simtrust

import (
	"context"
	"math"
	"testing"
	"time"
)

// smallWorld is a 12-hour world, with the newcomers joining at hour 2 and
// the adversaries defecting at hour 4.
func smallWorld(t *testing.T) (*Trace, ModelParams) {
	t.Helper()
	w := DefaultWorld()
	w.Hours = 12
	p := DefaultModels()
	p.JoinAt, p.DefectAt, p.Period = 2*time.Hour, 4*time.Hour, 4*time.Hour
	return GenerateWorld(w, 21, testStart), p
}

func runOf(t *testing.T, tr *Trace, spec RunSpec, p ModelParams) *Metrics {
	t.Helper()
	res, err := Run(context.Background(), tr, spec, p)
	if err != nil {
		t.Fatal(err)
	}
	return Measure(res)
}

// TestRunKeepsV01Invariants checks what v0.1 guarantees in a whole run:
// an attacker on which exactly two trusted remotes, and never more, had
// verdicts at the same time is banned under the lab's threshold, never
// under the default one, unless the observer reported it itself; three
// always ban; weights never move.
func TestRunKeepsV01Invariants(t *testing.T) {
	t.Parallel()
	tr, p := smallWorld(t)
	for _, profile := range []Profile{ProfileDefault, ProfileLab} {
		m := runOf(t, tr, RunSpec{Model: ModelHonest, Profile: profile, Seed: 21}, p)
		c := m.Corroboration
		two, three := c[2][0], c[3][0]
		if two.Attackers == 0 || three.Attackers == 0 {
			t.Fatalf("%s: corroboration %+v has no attacker reported by two or three remotes", profile, c)
		}
		wantTwo := 0
		if profile == ProfileLab {
			wantTwo = two.Attackers
		}
		if two.Banned != wantTwo || three.Banned != three.Attackers || c[1][0].Banned != 0 {
			t.Errorf("%s: corroboration %+v; want two remotes to ban %d of %d, three all, one none", profile, c, wantTwo, two.Attackers)
		}
		for s := range supportBuckets {
			if local := c[s][1]; local.Banned != local.Attackers {
				t.Errorf("%s: %d of %d attackers the observer reported were banned, want all", profile, local.Banned, local.Attackers)
			}
		}
		last := m.Hours[len(m.Hours)-1][WindowCumulative]
		if last[MetricHonestWeight] != 1 || last[MetricNewcomerConvergenceHours] != 0 || last[MetricFalseBansBeforeNeutralization] != 0 {
			t.Errorf("%s: honest weight %v, convergence %v h, false bans by defectors %v; want 1, 0, 0", profile,
				last[MetricHonestWeight], last[MetricNewcomerConvergenceHours], last[MetricFalseBansBeforeNeutralization])
		}
		if last[MetricPrecision] < 0.5 || last[MetricRecall] <= 0 {
			t.Errorf("%s: precision %v, recall %v", profile, last[MetricPrecision], last[MetricRecall])
		}
	}
}

func TestRunIsReproducible(t *testing.T) {
	t.Parallel()
	tr, p := smallWorld(t)
	spec := RunSpec{Model: ModelSpies, Fraction: 0.2, Profile: ProfileDefault, Seed: 21}
	a, b := runOf(t, tr, spec, p), runOf(t, tr, spec, p)
	for h := range a.Hours {
		for w := range numWindows {
			for i := range numMetrics {
				if x, y := a.Hours[h][w][i], b.Hours[h][w][i]; !near(x, y) {
					t.Fatalf("hour %d, %s, %s: %v and %v", h, Window(w), Metric(i), x, y)
				}
			}
		}
	}
}

func TestPoisonersCauseFalseBansInV01(t *testing.T) {
	t.Parallel()
	tr, p := smallWorld(t)
	honest := runOf(t, tr, RunSpec{Model: ModelHonest, Profile: ProfileDefault, Seed: 21}, p)
	for _, model := range []Model{ModelNaive, ModelSybil1ASN, ModelSpies} {
		m := runOf(t, tr, RunSpec{Model: model, Fraction: 0.4, Profile: ProfileDefault, Seed: 21}, p)
		last, base := m.Hours[len(m.Hours)-1][WindowCumulative], honest.Hours[len(m.Hours)-1][WindowCumulative]
		if last[MetricFalseBansBeforeNeutralization] == 0 || last[MetricFalseBans] <= base[MetricFalseBans] {
			t.Errorf("%s: %v false bans (%v by defectors), %v without adversaries; want more", model,
				last[MetricFalseBans], last[MetricFalseBansBeforeNeutralization], base[MetricFalseBans])
		}
		// They defect with their first poison verdict, soon after hour 4.
		if h := last[MetricNeutralizationHours]; last[MetricNeutralizedShare] != 0 || h > 8 || h < 7 {
			t.Errorf("%s: %v of the defectors neutralized after %v h; want none, counted until the end, 8 hours after hour 4", model,
				last[MetricNeutralizedShare], last[MetricNeutralizationHours])
		}
	}
}

func TestSuppressorLowersRecall(t *testing.T) {
	t.Parallel()
	tr, p := smallWorld(t)
	honest := runOf(t, tr, RunSpec{Model: ModelHonest, Profile: ProfileLab, Seed: 21}, p)
	m := runOf(t, tr, RunSpec{Model: ModelSuppressor, Fraction: 0.4, Profile: ProfileLab, Seed: 21}, p)
	last, base := m.Hours[len(m.Hours)-1][WindowCumulative], honest.Hours[len(m.Hours)-1][WindowCumulative]
	if last[MetricRecall] >= base[MetricRecall] {
		t.Errorf("recall %v with suppressors, %v without; want lower", last[MetricRecall], base[MetricRecall])
	}
}

// TestWhitewasherSwitchesKeys replaces the static weights by a model that
// takes a key's weight half an hour after it defected: the whitewashers
// burn their keys and go on under new ones, which no trust entry lists.
func TestWhitewasherSwitchesKeys(t *testing.T) {
	t.Parallel()
	tr, p := smallWorld(t)
	spec := RunSpec{Model: ModelWhitewash, Fraction: 0.2, Profile: ProfileDefault, Seed: 21}
	r := newRunner(tr, spec, p, nil)
	r.weight = func(n *Node, peerID string, now time.Time) float64 {
		for _, k := range r.res.keys {
			if k.peerID == peerID && k.role == Role(ModelWhitewash) && !k.defected.IsZero() && now.Sub(k.defected) >= 30*time.Minute {
				return 0
			}
		}
		return n.Weight(peerID)
	}
	res, err := r.run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	keys := map[int][]keyRecord{}
	for _, k := range res.keys {
		if k.role == Role(ModelWhitewash) {
			keys[k.actor] = append(keys[k.actor], k)
		}
	}
	if len(keys) != 4 {
		t.Fatalf("keys of the whitewashers: %v, want four whitewashers", keys)
	}
	for actor, ks := range keys {
		// The new key is never trusted, so it is never burned, and it is
		// never neutralized either: it had nothing to lose.
		if len(ks) != 2 || ks[0].burned.IsZero() || !ks[1].burned.IsZero() || len(ks[1].events) == 0 {
			t.Fatalf("whitewasher %d: keys %+v, want one burned and a new one in use", actor, ks)
		}
		if ks[0].neutralized.IsZero() || !ks[1].trusted.IsZero() || !ks[1].neutralized.IsZero() {
			t.Errorf("whitewasher %d: keys %+v, want the first neutralized and the new one never trusted nor neutralized", actor, ks)
		}
		if d := ks[1].joined.Sub(ks[0].burned); d != p.SwitchDelay {
			t.Errorf("whitewasher %d switched %s after burning its key, want %s", actor, d, p.SwitchDelay)
		}
	}
	// Only the first keys count, each neutralized at the first probe half
	// an hour after its defection.
	last := Measure(res).Hours[tr.Hours-1][WindowCumulative]
	if h := last[MetricNeutralizationHours]; last[MetricNeutralizedShare] != 1 || h < 0.5 || h > (30*time.Minute+probeInterval).Hours() ||
		math.IsNaN(last[MetricWhitewashPayoff]) {
		t.Errorf("neutralized %v after %v h, payoff %v", last[MetricNeutralizedShare], last[MetricNeutralizationHours], last[MetricWhitewashPayoff])
	}
}

func TestRunRefusesThePast(t *testing.T) {
	tr, p := smallWorld(t)
	// The same trace some 75 years earlier.
	const week = 7 * 24 * time.Hour
	back := 3900 * week
	past := *tr
	past.Start = tr.Start.Add(-back)
	past.Observations = nil
	for _, o := range tr.Observations {
		o.At = o.At.Add(-back)
		past.Observations = append(past.Observations, o)
	}
	if _, err := Run(context.Background(), &past, RunSpec{Model: ModelHonest, Profile: ProfileDefault, Seed: 1}, p); err == nil {
		t.Error("a trace in the past was run")
	}
	shifted := past.ShiftedTo(worldStart)
	if shifted.Start.Before(worldStart) || shifted.Start.Sub(past.Start)%week != 0 {
		t.Errorf("shifted from %s to %s, want whole weeks to %s or later", past.Start, shifted.Start, worldStart)
	}
	for i, o := range shifted.Observations {
		if o.At.Sub(shifted.Start) != tr.Observations[i].At.Sub(tr.Start) {
			t.Fatalf("observation %d is %s into the shifted trace, %s into the original", i, o.At.Sub(shifted.Start), tr.Observations[i].At.Sub(tr.Start))
		}
	}
	if tr.ShiftedTo(testStart) != tr {
		t.Error("a trace in the future was moved")
	}
}

// TestCoalitionsDifferOnlyInASNs checks that the two coalitions poison
// alike: v0.1 does not read publisher.asn, so their results are the same.
func TestCoalitionsDifferOnlyInASNs(t *testing.T) {
	t.Parallel()
	tr, p := smallWorld(t)
	one := runOf(t, tr, RunSpec{Model: ModelSybil1ASN, Fraction: 0.3, Profile: ProfileDefault, Seed: 21}, p)
	many := runOf(t, tr, RunSpec{Model: ModelSybilMASN, Fraction: 0.3, Profile: ProfileDefault, Seed: 21}, p)
	for h := range one.Hours {
		for i := range numMetrics {
			if x, y := one.Hours[h][WindowCumulative][i], many.Hours[h][WindowCumulative][i]; !near(x, y) {
				t.Fatalf("hour %d, %s: %v in one ASN, %v in several", h, Metric(i), x, y)
			}
		}
	}
}
