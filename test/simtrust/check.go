package simtrust

import "fmt"

// CheckV01 returns every way rep contradicts what v0.1's static weights
// guarantee (ADR 0034); the reduced scenario in CI asserts none does:
//   - the recall and precision of every configuration are defined in every
//     seed, and weights never move;
//   - in an honest-only run, an attacker that the observer did not report
//     itself is banned iff at least as many trusted remotes as a ban needs
//     had verdicts on it at the same time;
//   - no defector is ever neutralized;
//   - naive poisoners, Sybil coalitions and spies at the largest fraction
//     cause false bans in every setting;
//   - suppressors at the largest fraction lower the recall.
func CheckV01(rep *Report) []string {
	var out []string
	fail := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	md := &markdown{rep: rep}
	top := md.maxFraction()
	for _, a := range rep.Aggregates {
		end := a.End()
		for _, m := range []Metric{MetricPrecision, MetricRecall} {
			if end[m].N != rep.Seeds {
				fail("%s: %s defined in %d of %d seeds", a.Config, m, end[m].N, rep.Seeds)
			}
		}
		if w := end[MetricHonestWeight]; w.Mean != 1 || w.Low != 1 || w.High != 1 {
			fail("%s: honest weight %+v, want 1: static weights never move", a.Config, w)
		}
		if s := end[MetricNeutralizedShare]; s.N > 0 && s.Mean != 0 {
			fail("%s: %v of the defectors neutralized, want none", a.Config, s.Mean)
		}
		switch {
		case a.Model == ModelHonest:
			need, _, _ := minRemotes(a.Profile, rep.Scenario.Models.HonestConfidence)
			for b := 1; b < supportBuckets; b++ {
				c := a.Corroboration[b][0]
				want := 0
				if b >= need {
					want = c.TotalAttackers
				}
				if c.Total != want {
					fail("%s: %d of %d attackers reported by %s trusted remotes at once were banned, want %d",
						a.Config, c.Total, c.TotalAttackers, supportLabel(b), want)
				}
			}
		case a.Fraction != top:
		case a.Model == ModelNaive || a.Model == ModelSybil1ASN || a.Model == ModelSybilMASN || a.Model == ModelSpies:
			if e := end[MetricFalseBansBeforeNeutralization]; !(e.Low > 0) {
				fail("%s: false bans by defectors %+v, want some in every run", a.Config, e)
			}
		case a.Model == ModelSuppressor:
			if h := md.find(ModelHonest, 0, a.Profile); h != nil && !(end[MetricRecall].Mean < h.End()[MetricRecall].Mean) {
				fail("%s: recall %v, want below %v without adversaries", a.Config, end[MetricRecall].Mean, h.End()[MetricRecall].Mean)
			}
		}
	}
	return out
}
