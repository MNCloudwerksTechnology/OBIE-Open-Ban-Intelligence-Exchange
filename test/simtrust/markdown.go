package simtrust

import (
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

// num formats x with a precision that fits its size.
func num(x float64) string {
	a := math.Abs(x)
	switch {
	case math.IsNaN(x):
		return "—"
	case a >= 100:
		return fmt.Sprintf("%.0f", x)
	case a >= 10:
		return fmt.Sprintf("%.1f", x)
	case a >= 1:
		return fmt.Sprintf("%.2f", x)
	default:
		return fmt.Sprintf("%.3f", x)
	}
}

// param formats a parameter such as a threshold or a confidence.
func param(x float64) string {
	return strconv.FormatFloat(x, 'g', 3, 64)
}

// cell formats an estimate as mean ± half-width, marking one defined in
// fewer than all seeds.
func cell(e Estimate, seeds int) string {
	if e.N == 0 {
		return "—"
	}
	s := num(e.Mean)
	if e.N >= 2 {
		s += " ± " + num(e.HalfWidth())
	}
	if e.N < seeds {
		s += fmt.Sprintf(" (n=%d)", e.N)
	}
	return s
}

// percentOf formats part of whole as a percentage with both counts.
func percentOf(part, whole int) string {
	if whole == 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f %% (%d of %d)", 100*float64(part)/float64(whole), part, whole)
}

// markdown builds the report's Markdown.
type markdown struct {
	strings.Builder
	rep *Report
}

func (md *markdown) line(format string, args ...any) {
	_, _ = fmt.Fprintf(md, format+"\n", args...) // a strings.Builder never fails
}

func (md *markdown) table(header []string, rows [][]string) {
	md.line("| %s |", strings.Join(header, " | "))
	sep := make([]string, len(header))
	for i := range sep {
		sep[i] = "---"
	}
	md.line("|%s|", strings.Join(sep, "|"))
	for _, r := range rows {
		md.line("| %s |", strings.Join(r, " | "))
	}
	md.line("")
}

// find returns the aggregate of model at fraction under profile.
func (md *markdown) find(model Model, fraction float64, profile Profile) *Aggregate {
	for i := range md.rep.Aggregates {
		a := &md.rep.Aggregates[i]
		if a.Model == model && a.Fraction == fraction && a.Profile == profile {
			return a
		}
	}
	return nil
}

// profiles returns the profiles of the report in order.
func (md *markdown) profiles() []Profile {
	var out []Profile
	for _, a := range md.rep.Aggregates {
		if !slices.Contains(out, a.Profile) {
			out = append(out, a.Profile)
		}
	}
	return out
}

// maxFraction returns the largest adversary fraction of the report.
func (md *markdown) maxFraction() float64 {
	f := 0.0
	for _, a := range md.rep.Aggregates {
		f = max(f, a.Fraction)
	}
	return f
}

// adversarialModels returns the models with adversaries in report order.
func (md *markdown) adversarialModels() []Model {
	var out []Model
	for _, a := range md.rep.Aggregates {
		if a.Model != ModelHonest && !slices.Contains(out, a.Model) {
			out = append(out, a.Model)
		}
	}
	return out
}

// settingsOf describes a profile's decision settings from its
// configuration.
func settingsOf(p Profile) string {
	cfg, err := NodeSpec{Profile: p}.Config()
	if err != nil {
		return string(p)
	}
	s := fmt.Sprintf("`decision.threshold` %s, `decision.quorum` %d", param(cfg.Decision.Threshold), cfg.Decision.Quorum)
	if p == ProfileAllowlist {
		s += ", the published CDN and crawler ranges in `allowlist.cidrs`"
	}
	return s
}

// writeMarkdown writes the Markdown report.
func writeMarkdown(w io.Writer, rep *Report, info ReportInfo) error {
	md := &markdown{rep: rep}
	md.header(info)
	md.findings()
	md.results()
	md.hourByHour()
	md.feeds()
	md.files()
	md.limitations()
	_, err := io.WriteString(w, strings.TrimRight(md.String(), "\n")+"\n")
	return err
}

func (md *markdown) header(info ReportInfo) {
	rep := md.rep
	md.line("# Trust simulation: %s", rep.Scenario.Name)
	md.line("")
	md.table([]string{"Report", ""}, [][]string{
		{"Format", fmt.Sprint(ReportFormat)},
		{"OBIE", "`" + info.Version + "`"},
		{"Scenario", fmt.Sprintf("`%s`: %s", rep.Scenario.Name, rep.Scenario.Description)},
		{"Trace", rep.Trace},
		{"Hours", fmt.Sprint(rep.Hours)},
		{"Seeds", fmt.Sprintf("%d (1 to %d)", rep.Seeds, rep.Seeds)},
		{"Runs", fmt.Sprintf("%d in %s", rep.Runs, rep.Wall.Round(time.Second))},
		{"Generated", info.Generated.UTC().Format("2006-01-02")},
	})
	md.line("`make sim-trust SCENARIO=%s` wrote this report; do not edit it. Each run replays", rep.Scenario.Name)
	md.line("the bans of the trace and the verdicts of publishers of known behavior")
	md.line("through the store, allow-list and decision engine of one observer node,")
	md.line("and scores the bans that the engine enforces against the ground truth.")
	md.line("[ADR 0034](../../adr/0034-trust-simulation-by-trace-replay.md) defines the")
	md.line("world, the models and every metric.")
	md.line("")
	md.line("A value is the mean over the seeds ± half the width of its 95 %% confidence")
	md.line("interval (Student t). A dash means the metric is not defined, e.g. precision")
	md.line("without bans; (n=k) marks a value defined in only k seeds.")
	md.line("")
}

// minRemotes returns how many fully trusted remotes at confidence c a
// ban needs under profile p.
func minRemotes(p Profile, c float64) (int, float64, int) {
	cfg, err := NodeSpec{Profile: p}.Config()
	if err != nil {
		return 0, 0, 0
	}
	k := max(cfg.Decision.Quorum, 1)
	for float64(k)*Ceiling*c < cfg.Decision.Threshold-1e-9 {
		k++
	}
	return k, cfg.Decision.Threshold, cfg.Decision.Quorum
}

func (md *markdown) findings() {
	profiles := md.profiles()
	var honest []*Aggregate
	for _, p := range profiles {
		if a := md.find(ModelHonest, 0, p); a != nil {
			honest = append(honest, a)
		}
	}
	adversarial := md.adversarialModels()
	if len(honest) == 0 && len(adversarial) == 0 {
		return
	}
	md.line("## Findings")
	md.line("")
	if len(honest) > 0 {
		md.remotesNeeded(honest)
		md.honestOnly(honest)
	}
	if len(adversarial) > 0 {
		md.adversaries(adversarial, profiles)
	}
}

// remotesNeeded shows how many trusted remotes a ban needs.
func (md *markdown) remotesNeeded(honest []*Aggregate) {
	conf := md.rep.Scenario.Models.HonestConfidence
	md.line("### How many trusted remotes a ban needs")
	md.line("")
	for _, a := range honest {
		k, thr, q := minRemotes(a.Profile, conf)
		md.line("- `%s` (threshold %s, quorum %d): a ban on remote verdicts alone needs **%d** fully",
			a.Profile, param(thr), q, k)
		md.line("  trusted remotes at confidence %s, which score %d × %s = %s.", param(conf), k, param(conf), param(float64(k)*conf))
	}
	md.line("")
	md.line("The attackers of the honest-only runs, by the most trusted remotes that")
	md.line("had verdicts on them active at the same time, and the share of them")
	md.line("banned. The observer bans the attackers it saw itself anyway (last row).")
	md.line("")
	header := []string{"Trusted remotes at once", "Attackers per seed"}
	for _, a := range honest {
		header = append(header, "Banned: `"+string(a.Profile)+"`")
	}
	var rows [][]string
	for b := 1; b < supportBuckets; b++ {
		row := []string{supportLabel(b), cell(honest[0].Corroboration[b][0].Attackers, md.rep.Seeds)}
		for _, a := range honest {
			c := a.Corroboration[b][0]
			row = append(row, percentOf(c.Total, c.TotalAttackers))
		}
		rows = append(rows, row)
	}
	local := []string{"any, and the observer's own", ""}
	var localN []float64
	for _, a := range honest {
		total, banned := 0, 0
		for b := range supportBuckets {
			total += a.Corroboration[b][1].TotalAttackers
			banned += a.Corroboration[b][1].Total
		}
		local = append(local, percentOf(banned, total))
		localN = append(localN, float64(total)/float64(md.rep.Seeds))
	}
	local[1] = num(localN[0])
	rows = append(rows, local)
	md.table(header, rows)
	for _, a := range honest {
		two := a.Corroboration[2][0]
		k, thr, _ := minRemotes(a.Profile, conf)
		md.line("- `%s`: %s of the attackers that at most two trusted remotes reported at the",
			a.Profile, percentOf(two.Total, two.TotalAttackers))
		md.line("  same time were banned; two remotes score %s against a threshold of %s,", param(2*conf), param(thr))
		md.line("  so the engine needs %d.", k)
	}
	md.line("")
}

// honestOnly shows the quality of the bans without adversaries.
func (md *markdown) honestOnly(honest []*Aggregate) {
	md.line("### Honest publishers only")
	md.line("")
	md.line("At the end of the run, cumulatively. The false bans are the shared NAT")
	md.line("addresses that attackers use and the CDN edges that misconfigured")
	md.line("publishers report.")
	md.line("")
	metrics := []Metric{MetricPrecision, MetricRecall, MetricF1, MetricFalseBans, MetricFalseBansPerVictim, MetricECE, MetricBrier}
	header := []string{"Settings"}
	for _, m := range metrics {
		header = append(header, m.Title())
	}
	var rows [][]string
	for _, a := range honest {
		row := []string{"`" + string(a.Profile) + "`"}
		end := a.End()
		for _, m := range metrics {
			row = append(row, cell(end[m], md.rep.Seeds))
		}
		rows = append(rows, row)
	}
	md.table(header, rows)
}

// adversaries shows the damage the adversaries do and that none is
// neutralized.
func (md *markdown) adversaries(models []Model, profiles []Profile) {
	f := md.maxFraction()
	md.line("### Adversaries at %.0f %%", 100*f)
	md.line("")
	md.line("False bans that malicious verdicts caused before their key was")
	md.line("neutralized, per run, at the end:")
	md.line("")
	header := []string{"Model"}
	for _, p := range profiles {
		header = append(header, "`"+string(p)+"`")
	}
	var rows [][]string
	for _, m := range models {
		row := []string{string(m)}
		for _, p := range profiles {
			if a := md.find(m, f, p); a != nil {
				row = append(row, cell(a.End()[MetricFalseBansBeforeNeutralization], md.rep.Seeds))
			} else {
				row = append(row, "—")
			}
		}
		rows = append(rows, row)
	}
	md.table(header, rows)
	configs, neutralizing := 0, 0
	for _, a := range md.rep.Aggregates {
		if e := a.End()[MetricNeutralizedShare]; e.N > 0 {
			configs++
			if e.Mean > 0 {
				neutralizing++
			}
		}
	}
	if neutralizing == 0 {
		md.line("No defector lost its weight in any run of the %d configurations with", configs)
		md.line("adversaries: static weights never change. Every defector keeps its full")
		md.line("weight until the run ends, so the time from its defection to its")
		md.line("neutralization is the rest of the run, and the whitewashers never need a")
		md.line("new key.")
	} else {
		md.line("Some defectors were neutralized in %d of the %d configurations with", neutralizing, configs)
		md.line("adversaries; see the results below.")
	}
	md.line("")
}

// resultMetrics are the columns of the results tables.
var resultMetrics = []Metric{
	MetricPrecision, MetricRecall, MetricF1, MetricFalseBansPerVictim, MetricFalseBansBeforeNeutralization,
	MetricNeutralizedShare, MetricNeutralizationHours, MetricNeutralizationEvents, MetricHonestWeight,
	MetricNewcomerConvergenceHours, MetricWhitewashPayoff, MetricECE, MetricBrier,
}

func (md *markdown) results() {
	md.line("## Results at the end of the run")
	md.line("")
	md.line("Every metric cumulatively over the whole run; `summary.csv` has them all.")
	md.line("")
	for _, p := range md.profiles() {
		md.line("### Settings `%s`", p)
		md.line("")
		md.line("%s.", settingsOf(p))
		md.line("")
		header := []string{"Model", "Adversaries"}
		for _, m := range resultMetrics {
			header = append(header, m.Title())
		}
		var rows [][]string
		for _, a := range md.rep.Aggregates {
			if a.Profile != p {
				continue
			}
			row := []string{string(a.Model), fmt.Sprintf("%.0f %%", 100*a.Fraction)}
			end := a.End()
			for _, m := range resultMetrics {
				row = append(row, cell(end[m], md.rep.Seeds))
			}
			rows = append(rows, row)
		}
		md.table(header, rows)
	}
}

// reportHours returns the hours the hour-by-hour tables show: the last
// of every day, or of every 6 hours in a shorter run.
func reportHours(hours int) []int {
	step := 24
	if hours < 48 {
		step = 6
	}
	var out []int
	for h := step - 1; h < hours; h += step {
		out = append(out, h)
	}
	if len(out) == 0 || out[len(out)-1] != hours-1 {
		out = append(out, hours-1)
	}
	return out
}

func (md *markdown) hourByHour() {
	profiles := md.profiles()
	if len(profiles) == 0 {
		return
	}
	p, f := profiles[0], md.maxFraction()
	var shown []*Aggregate
	if a := md.find(ModelHonest, 0, p); a != nil {
		shown = append(shown, a)
	}
	for _, m := range md.adversarialModels() {
		if a := md.find(m, f, p); a != nil {
			shown = append(shown, a)
		}
	}
	hours := reportHours(md.rep.Hours)
	md.line("## Hour by hour")
	md.line("")
	md.line("Settings `%s`, honest-only and every model at %.0f %%, within single hours;", p, 100*f)
	md.line("`hourly.csv.gz` has every metric of every configuration, per hour and")
	md.line("cumulatively. The adversaries defect at hour %d.", int(md.rep.Scenario.Models.DefectAt.Hours()))
	md.line("")
	for _, m := range []Metric{MetricPrecision, MetricRecall, MetricFalseBans} {
		md.line("### %s per hour", m.Title())
		md.line("")
		header := []string{"Model"}
		for _, h := range hours {
			header = append(header, fmt.Sprintf("Hour %d", h))
		}
		var rows [][]string
		for _, a := range shown {
			row := []string{string(a.Model)}
			for _, h := range hours {
				row = append(row, cell(a.Hours[h][WindowHour][m], md.rep.Seeds))
			}
			rows = append(rows, row)
		}
		md.table(header, rows)
	}
}

func (md *markdown) feeds() {
	profiles := md.profiles()
	if len(profiles) == 0 {
		return
	}
	p, f := profiles[0], md.maxFraction()
	md.line("## Feeds of the publishers")
	md.line("")
	md.line("After Li et al. 2019, over the whole run, the mean of the publishers of")
	md.line("each role; settings `%s`, honest-only and every model at %.0f %%. The", p, 100*f)
	md.line("benign-set bound is 1 − the share of a feed's addresses in the published")
	md.line("ranges; the accuracy is its true share of attackers.")
	md.line("")
	header := []string{"Model", "Role", "Volume", "Exclusive", "Relative latency (min)", "Benign-set bound", "Accuracy"}
	var rows [][]string
	add := func(a *Aggregate) {
		for _, r := range sortedRoles(a.Feeds) {
			fe := a.Feeds[r]
			rows = append(rows, []string{string(a.Model), string(r), cell(fe.Volume, md.rep.Seeds), cell(fe.Exclusive, md.rep.Seeds),
				cell(fe.LatencyMinutes, md.rep.Seeds), cell(fe.Bound, md.rep.Seeds), cell(fe.Accuracy, md.rep.Seeds)})
		}
	}
	if a := md.find(ModelHonest, 0, p); a != nil {
		add(a)
	}
	for _, m := range md.adversarialModels() {
		if a := md.find(m, f, p); a != nil {
			add(a)
		}
	}
	md.table(header, rows)
}

func (md *markdown) files() {
	md.line("## Files")
	md.line("")
	md.line("- `summary.csv`: every metric of every configuration at the end, cumulatively:")
	md.line("  `model,fraction,profile,metric,n,mean,ci_low,ci_high`.")
	md.line("- `hourly.csv.gz`: every metric of every configuration in every hour, within")
	md.line("  the hour and cumulatively: `model,fraction,profile,hour,window,metric,n,mean,ci_low,ci_high`.")
	md.line("- `feeds.csv`: the feed metrics of every role: `model,fraction,profile,role,metric,n,mean,ci_low,ci_high`.")
	md.line("- `corroboration.csv`: the attackers by the most trusted honest remotes at")
	md.line("  once and by whether the observer reported them, and the share banned.")
	md.line("")
}

func (md *markdown) limitations() {
	md.line("## Limitations")
	md.line("")
	md.line("- The trace is synthetic unless the header names a recorded one. Its")
	md.line("  parameters are in ADR 0034; the licensing of real operators' Fail2Ban")
	md.line("  logs and their treatment under the DSGVO are not settled yet.")
	md.line("- One observer trusts every publisher with the same weight. Verdicts")
	md.line("  reach it the second they are issued; routing is measured apart.")
	md.line("- The store is swept every 5 virtual minutes, so a ban whose score")
	md.line("  falls when one of its verdicts expires may last up to 5 minutes longer.")
	md.line("- An address is an attacker or benign for the whole run; addresses that")
	md.line("  change hands are not modeled.")
}
