package simtrust

import (
	"cmp"
	"math"
	"net/netip"
	"slices"
	"time"
)

// Metric is a metric of a run (ADR 0034).
type Metric int

// Metrics of a run.
const (
	MetricPrecision Metric = iota
	MetricRecall
	MetricF1
	MetricFalseBans
	MetricFalseBansPerVictim
	MetricFalseBanHours
	MetricNeutralizedShare
	MetricNeutralizationHours
	MetricNeutralizationEvents
	MetricFalseBansBeforeNeutralization
	MetricHonestWeight
	MetricNewcomerConvergedShare
	MetricNewcomerConvergenceHours
	MetricWhitewashPayoff
	MetricECE
	MetricBrier
	numMetrics
)

// metricInfo describes a metric: its CSV name, its title, and whether it
// has a value within one hour; the others are durations per key, reported
// cumulatively only.
type metricInfo struct {
	name, title string
	hourly      bool
}

var metricInfos = [numMetrics]metricInfo{
	MetricPrecision:                     {"precision", "Precision", true},
	MetricRecall:                        {"recall", "Recall", true},
	MetricF1:                            {"f1", "F1", true},
	MetricFalseBans:                     {"false_bans", "False bans", true},
	MetricFalseBansPerVictim:            {"false_bans_per_victim", "False bans per protected victim", true},
	MetricFalseBanHours:                 {"false_ban_hours", "False ban hours", true},
	MetricNeutralizedShare:              {"neutralized_share", "Defectors neutralized", false},
	MetricNeutralizationHours:           {"neutralization_hours", "Defection to neutralization (h)", false},
	MetricNeutralizationEvents:          {"neutralization_events", "Defection to neutralization (events)", false},
	MetricFalseBansBeforeNeutralization: {"false_bans_before_neutralization", "False bans before neutralization", true},
	MetricHonestWeight:                  {"honest_weight", "Honest weight / ceiling", true},
	MetricNewcomerConvergedShare:        {"newcomers_converged", "Newcomers converged", false},
	MetricNewcomerConvergenceHours:      {"newcomer_convergence_hours", "Newcomer convergence (h)", false},
	MetricWhitewashPayoff:               {"whitewash_payoff", "Whitewashing payoff", false},
	MetricECE:                           {"ece", "ECE", true},
	MetricBrier:                         {"brier", "Brier", true},
}

func (m Metric) String() string { return metricInfos[m].name }

// Title is the metric's name in a report.
func (m Metric) Title() string { return metricInfos[m].title }

// Window is the time a value covers.
type Window int

// Windows.
const (
	// WindowHour: the hour [h, h+1).
	WindowHour Window = iota
	// WindowCumulative: the run so far, [0, h+1).
	WindowCumulative
	numWindows
)

func (w Window) String() string {
	if w == WindowHour {
		return "hour"
	}
	return "cumulative"
}

// Values are the value of every metric in one window; NaN where a
// metric is not defined, e.g. precision without bans.
type Values [numMetrics]float64

// eceBins is the number of equal-width confidence bins of the ECE (Guo et
// al. 2017).
const eceBins = 10

// supportBuckets group attackers by the most honest remotes that
// reported them at the same time: 0, 1, 2, 3 and 4 or more.
const supportBuckets = 5

// Corroboration counts attackers by their support and by whether the
// observer reported them itself, and how many of them were banned.
type Corroboration [supportBuckets][2]struct{ Attackers, Banned int }

// Feed are the feed metrics of a publisher after Li et al. 2019.
type Feed struct {
	// Volume is the number of addresses it reported.
	Volume float64
	// Exclusive is the share of them no other publisher reported.
	Exclusive float64
	// LatencyMinutes is the median delay of its first report behind the
	// first report of any publisher, over the addresses others reported
	// too.
	LatencyMinutes float64
	// Bound is 1 − the share of its addresses in the published ranges:
	// the accuracy bound of a benign set; Accuracy the true share of
	// attackers.
	Bound, Accuracy float64
}

// PublisherFeed are the feed metrics of one publisher key: the Key-th
// key (from 0) of slot Slot, which has Role.
type PublisherFeed struct {
	Slot, Key int
	Role      Role
	Feed
}

// Metrics are the metrics of one run.
type Metrics struct {
	Spec RunSpec
	// Hours holds the values of hour h in both windows.
	Hours         [][numWindows]Values
	Corroboration Corroboration
	// Feeds are the mean feed metrics of the publishers of each role;
	// Publishers those of every publisher key.
	Feeds      map[Role]Feed
	Publishers []PublisherFeed
}

// measure holds what Measure derives from a result.
type measure struct {
	r       *Result
	w       *worldIndex
	hours   int
	keyOf   map[string]int
	falseEp []Episode
}

// hourOf returns the hour of at within the run.
func (m *measure) hourOf(at time.Time) int {
	return min(max(int(at.Sub(m.r.Trace.Start)/time.Hour), 0), m.hours-1)
}

// endOf returns the end of hour h.
func (m *measure) endOf(h int) time.Time {
	return m.r.Trace.Start.Add(time.Duration(h+1) * time.Hour)
}

// Measure computes the metrics of a run.
func Measure(r *Result) *Metrics {
	m := &measure{r: r, w: newWorldIndex(r.Trace), hours: r.Trace.Hours, keyOf: map[string]int{}}
	for i, k := range r.keys {
		m.keyOf[k.peerID] = i
	}
	out := &Metrics{Spec: r.Spec, Hours: make([][numWindows]Values, m.hours)}
	for h := range out.Hours {
		for w := range numWindows {
			for i := range out.Hours[h][w] {
				out.Hours[h][w][i] = math.NaN()
			}
		}
	}
	m.bans(out)
	m.defectors(out)
	m.weights(out)
	m.calibration(out)
	out.Corroboration = m.corroboration()
	out.Feeds, out.Publishers = m.feeds()
	return out
}

// active returns the attackers each hour: those that the Fail2Ban of an
// operator in the network banned in it, whatever the operator's
// publisher does with the ban. Newcomers are in the network once they
// joined. The attackers are thus the same for every model and fraction.
func (m *measure) active() []map[netip.Addr]bool {
	out := make([]map[netip.Addr]bool, m.hours)
	for h := range out {
		out[h] = map[netip.Addr]bool{}
	}
	joined := make([]time.Time, len(m.r.Cast.roles))
	for i := len(m.r.keys) - 1; i >= 0; i-- {
		joined[m.r.keys[i].actor] = m.r.keys[i].joined
	}
	for _, o := range m.r.Trace.Observations {
		if m.w.class[o.Addr] == ClassAttacker && !o.At.Before(joined[o.Operator]) {
			out[m.hourOf(o.At)][o.Addr] = true
		}
	}
	return out
}

// banned returns the addresses banned in each hour: those an episode
// overlaps.
func (m *measure) banned() []map[netip.Addr]bool {
	out := make([]map[netip.Addr]bool, m.hours)
	for h := range out {
		out[h] = map[netip.Addr]bool{}
	}
	for _, e := range m.r.Episodes {
		last := m.hourOf(e.Start)
		if e.End.After(e.Start) {
			last = m.hourOf(e.End.Add(-time.Nanosecond))
		}
		for h := m.hourOf(e.Start); h <= last; h++ {
			out[h][e.Addr] = true
		}
	}
	return out
}

// bans computes precision, recall, F1 and the false bans.
func (m *measure) bans(out *Metrics) {
	active, banned := m.active(), m.banned()
	// firstActive and firstBanned are the first hour an address was
	// active or banned.
	firstActive, firstBanned := map[netip.Addr]int{}, map[netip.Addr]int{}
	for h := m.hours - 1; h >= 0; h-- {
		for a := range active[h] {
			firstActive[a] = h
		}
		for a := range banned[h] {
			firstBanned[a] = h
		}
	}
	falseByHour, causedByHour := make([]int, m.hours), make([]int, m.hours)
	banHours := make([]float64, m.hours)
	for _, e := range m.r.Episodes {
		if !m.w.class[e.Addr].Benign() {
			continue
		}
		for h := m.hourOf(e.Start); h < m.hours && m.endOf(h-1).Before(e.End); h++ {
			banHours[h] += overlap(e.Start, e.End, m.endOf(h-1), m.endOf(h)).Hours()
		}
		falseByHour[m.hourOf(e.Start)]++
		if at, ok := m.causedAt(e, func(keyRecord) bool { return true }); ok {
			causedByHour[m.hourOf(at)]++
		}
		m.falseEp = append(m.falseEp, e)
	}
	victims := float64(len(m.w.benign))
	var cum struct{ banned, bannedAttackers, active, activeBanned, falseBans, caused int }
	var cumBanHours float64
	for h := range m.hours {
		var hour struct{ bannedAttackers, activeBanned int }
		for a := range banned[h] {
			if m.w.class[a] == ClassAttacker {
				hour.bannedAttackers++
			}
			if first := firstBanned[a]; first == h {
				cum.banned++
				if m.w.class[a] == ClassAttacker {
					cum.bannedAttackers++
					if fa, ok := firstActive[a]; ok && fa <= h {
						cum.activeBanned++
					}
				}
			}
		}
		for a := range active[h] {
			if banned[h][a] {
				hour.activeBanned++
			}
			if firstActive[a] == h {
				cum.active++
				if fb, ok := firstBanned[a]; ok && fb < h {
					cum.activeBanned++
				}
			}
		}
		cum.falseBans += falseByHour[h]
		cum.caused += causedByHour[h]
		setBans(&out.Hours[h][WindowHour], hour.bannedAttackers, len(banned[h]), hour.activeBanned, len(active[h]), falseByHour[h], causedByHour[h], victims)
		setBans(&out.Hours[h][WindowCumulative], cum.bannedAttackers, cum.banned, cum.activeBanned, cum.active, cum.falseBans, cum.caused, victims)
		cumBanHours += banHours[h]
		out.Hours[h][WindowHour][MetricFalseBanHours] = banHours[h]
		out.Hours[h][WindowCumulative][MetricFalseBanHours] = cumBanHours
	}
}

// setBans sets the ban metrics of one window.
func setBans(v *Values, bannedAttackers, banned, activeBanned, active, falseBans, caused int, victims float64) {
	v[MetricPrecision] = ratio(bannedAttackers, banned)
	v[MetricRecall] = ratio(activeBanned, active)
	p, r := v[MetricPrecision], v[MetricRecall]
	v[MetricF1] = 2 * p * r / (p + r)
	if p+r == 0 {
		v[MetricF1] = 0
	}
	v[MetricFalseBans] = float64(falseBans)
	v[MetricFalseBansPerVictim] = float64(falseBans) / victims
	v[MetricFalseBansBeforeNeutralization] = float64(caused)
}

// overlap returns how long [a, b) and [c, d) overlap.
func overlap(a, b, c, d time.Time) time.Duration {
	from, to := a, b
	if c.After(from) {
		from = c
	}
	if d.Before(to) {
		to = d
	}
	return max(to.Sub(from), 0)
}

// ratio returns a/b, NaN if b is 0.
func ratio(a, b int) float64 {
	if b == 0 {
		return math.NaN()
	}
	return float64(a) / float64(b)
}

// causedAt returns when a malicious verdict of a key that of selects, not
// neutralized yet, first counted in the false ban e: at its start or an
// update. An adversary's honest reports, e.g. of a shared NAT address, do
// not make it a cause.
func (m *measure) causedAt(e Episode, of func(keyRecord) bool) (time.Time, bool) {
	var first time.Time
	for _, c := range e.Contributors {
		i, ok := m.keyOf[c.PeerID]
		if !ok || !m.r.malicious[c.EventID] || !of(m.r.keys[i]) {
			continue
		}
		if k := &m.r.keys[i]; (k.neutralized.IsZero() || c.At.Before(k.neutralized)) && (first.IsZero() || c.At.Before(first)) {
			first = c.At
		}
	}
	return first, !first.IsZero()
}

// defectors computes the neutralization of the defecting keys, the
// convergence of the newcomers and the whitewashing payoff, as their
// running values at the end of every hour.
func (m *measure) defectors(out *Metrics) {
	for h := range m.hours {
		end := m.endOf(h)
		v := &out.Hours[h][WindowCumulative]
		var n, neutralized int
		var hours, events float64
		var newcomers, converged int
		var convergence float64
		for i := range m.r.keys {
			k := &m.r.keys[i]
			if k.role.Adversary() && !k.defected.IsZero() && k.defected.Before(end) {
				until := end
				if !k.neutralized.IsZero() && k.neutralized.Before(end) {
					until = k.neutralized
					neutralized++
				}
				n++
				hours += until.Sub(k.defected).Hours()
				events += float64(countIn(k.events, k.defected, until))
			}
			if k.role == RoleNewcomer && k.joined.Before(end) {
				until := end
				if !k.converged.IsZero() && k.converged.Before(end) {
					until = k.converged
					converged++
				}
				newcomers++
				convergence += until.Sub(k.joined).Hours()
			}
		}
		if n > 0 {
			v[MetricNeutralizedShare] = float64(neutralized) / float64(n)
			v[MetricNeutralizationHours] = hours / float64(n)
			v[MetricNeutralizationEvents] = events / float64(n)
		}
		if newcomers > 0 {
			v[MetricNewcomerConvergedShare] = float64(converged) / float64(newcomers)
			v[MetricNewcomerConvergenceHours] = convergence / float64(newcomers)
		}
		if m.r.Spec.Model == ModelWhitewash {
			v[MetricWhitewashPayoff] = m.payoff(end)
		}
	}
}

// countIn counts the times in [from, to).
func countIn(times []time.Time, from, to time.Time) int {
	lo, _ := slices.BinarySearchFunc(times, from, time.Time.Compare)
	hi, _ := slices.BinarySearchFunc(times, to, time.Time.Compare)
	return hi - lo
}

// payoff is the whitewashers' mean false bans per burned key until end:
// the false bans that a malicious verdict of any of an actor's keys caused
// before the key was neutralized, over the keys it burned, at least 1.
func (m *measure) payoff(end time.Time) float64 {
	var sum float64
	actors := 0
	for actor, role := range m.r.Cast.roles {
		if role != Role(ModelWhitewash) {
			continue
		}
		actors++
		burned := 0
		for _, k := range m.r.keys {
			if k.actor == actor && !k.burned.IsZero() && k.burned.Before(end) {
				burned++
			}
		}
		bans := 0
		for _, e := range m.falseEp {
			if at, ok := m.causedAt(e, func(k keyRecord) bool { return k.actor == actor }); ok && at.Before(end) {
				bans++
			}
		}
		sum += float64(bans) / float64(max(burned, 1))
	}
	if actors == 0 {
		return math.NaN()
	}
	return sum / float64(actors)
}

// weights computes the honest publishers' mean weight in each window.
func (m *measure) weights(out *Metrics) {
	hourSum, hourN := make([]float64, m.hours), make([]int, m.hours)
	for _, s := range m.r.weights {
		if s.at.Before(m.r.Trace.End()) {
			h := m.hourOf(s.at)
			hourSum[h] += s.mean
			hourN[h]++
		}
	}
	var cumSum float64
	cumN := 0
	for h := range m.hours {
		cumSum += hourSum[h]
		cumN += hourN[h]
		if hourN[h] > 0 {
			out.Hours[h][WindowHour][MetricHonestWeight] = hourSum[h] / float64(hourN[h])
		}
		if cumN > 0 {
			out.Hours[h][WindowCumulative][MetricHonestWeight] = cumSum / float64(cumN)
		}
	}
}

// calibrationSums are the sums of the ECE and the Brier score.
type calibrationSums struct {
	n                  int
	brier              float64
	bins               [eceBins]int
	binConf, binAttack [eceBins]float64
}

func (c *calibrationSums) add(confidence float64, attacker bool) {
	y := 0.0
	if attacker {
		y = 1
	}
	c.n++
	c.brier += (confidence - y) * (confidence - y)
	b := min(int(confidence*eceBins), eceBins-1)
	c.bins[b]++
	c.binConf[b] += confidence
	c.binAttack[b] += y
}

func (c *calibrationSums) merge(o *calibrationSums) {
	c.n += o.n
	c.brier += o.brier
	for b := range eceBins {
		c.bins[b] += o.bins[b]
		c.binConf[b] += o.binConf[b]
		c.binAttack[b] += o.binAttack[b]
	}
}

func (c *calibrationSums) set(v *Values) {
	if c.n == 0 {
		return
	}
	v[MetricBrier] = c.brier / float64(c.n)
	ece := 0.0
	for b := range eceBins {
		if c.bins[b] > 0 {
			ece += math.Abs(c.binAttack[b]-c.binConf[b]) / float64(c.n)
		}
	}
	v[MetricECE] = ece
}

// calibration computes the ECE and the Brier score of the received ban
// verdicts' confidence against the ground truth.
func (m *measure) calibration(out *Metrics) {
	hours := make([]calibrationSums, m.hours)
	for _, rv := range m.r.received {
		hours[m.hourOf(rv.at)].add(rv.confidence, m.w.class[rv.addr] == ClassAttacker)
	}
	var cum calibrationSums
	for h := range m.hours {
		cum.merge(&hours[h])
		hours[h].set(&out.Hours[h][WindowHour])
		cum.set(&out.Hours[h][WindowCumulative])
	}
}

// interval is a time a verdict counted.
type interval struct{ from, to time.Time }

// corroboration counts the attackers of the run by their support: the
// most honest remotes whose verdicts on them were active at the same
// time.
func (m *measure) corroboration() Corroboration {
	type reports struct {
		local  bool
		byKeys map[int][]interval
	}
	byAddr := map[netip.Addr]*reports{}
	for _, rv := range m.r.received {
		if m.w.class[rv.addr] != ClassAttacker {
			continue
		}
		rp := byAddr[rv.addr]
		if rp == nil {
			rp = &reports{byKeys: map[int][]interval{}}
			byAddr[rv.addr] = rp
		}
		switch m.r.keys[rv.key].role {
		case RoleObserver:
			rp.local = true
		case RoleHonest, RoleNewcomer:
			rp.byKeys[rv.key] = append(rp.byKeys[rv.key], interval{rv.at, rv.end})
		}
	}
	banned := map[netip.Addr]bool{}
	for _, e := range m.r.Episodes {
		banned[e.Addr] = true
	}
	var out Corroboration
	active := m.active()
	seen := map[netip.Addr]bool{}
	for _, hour := range active {
		for a := range hour {
			if seen[a] {
				continue
			}
			seen[a] = true
			support, local := 0, false
			if rp := byAddr[a]; rp != nil {
				support, local = maxConcurrent(rp.byKeys), rp.local
			}
			l := 0
			if local {
				l = 1
			}
			c := &out[min(support, supportBuckets-1)][l]
			c.Attackers++
			if banned[a] {
				c.Banned++
			}
		}
	}
	return out
}

// maxConcurrent returns the most keys with an interval active at the
// same time.
func maxConcurrent(byKeys map[int][]interval) int {
	type edge struct {
		at    time.Time
		delta int
	}
	var edges []edge
	for _, ivs := range byKeys {
		for _, iv := range merge(ivs) {
			edges = append(edges, edge{iv.from, 1}, edge{iv.to, -1})
		}
	}
	// An interval that ends when another starts does not overlap it.
	slices.SortFunc(edges, func(a, b edge) int { return cmp.Or(a.at.Compare(b.at), cmp.Compare(a.delta, b.delta)) })
	n, best := 0, 0
	for _, e := range edges {
		n += e.delta
		best = max(best, n)
	}
	return best
}

// merge returns the union of intervals as disjoint intervals.
func merge(ivs []interval) []interval {
	slices.SortFunc(ivs, func(a, b interval) int { return a.from.Compare(b.from) })
	var out []interval
	for _, iv := range ivs {
		if !iv.to.After(iv.from) {
			continue
		}
		if n := len(out); n > 0 && !iv.from.After(out[n-1].to) {
			if iv.to.After(out[n-1].to) {
				out[n-1].to = iv.to
			}
			continue
		}
		out = append(out, iv)
	}
	return out
}

// feeds computes the feed metrics of every key, and their means by role.
func (m *measure) feeds() (map[Role]Feed, []PublisherFeed) {
	first := make([]map[netip.Addr]time.Time, len(m.r.keys))
	earliest := map[netip.Addr]time.Time{}
	reporters := map[netip.Addr]int{}
	for _, rv := range m.r.received {
		if first[rv.key] == nil {
			first[rv.key] = map[netip.Addr]time.Time{}
		}
		if _, ok := first[rv.key][rv.addr]; ok {
			continue
		}
		first[rv.key][rv.addr] = rv.at
		reporters[rv.addr]++
		if e, ok := earliest[rv.addr]; !ok || rv.at.Before(e) {
			earliest[rv.addr] = rv.at
		}
	}
	sums := map[Role]*[5]meanOf{}
	var publishers []PublisherFeed
	keysOf := map[int]int{}
	for i, k := range m.r.keys {
		n := keysOf[k.actor]
		keysOf[k.actor]++
		if len(first[i]) == 0 {
			continue
		}
		var exclusive, inRanges, attackers int
		var delays []float64
		for a, at := range first[i] {
			if reporters[a] == 1 {
				exclusive++
			} else {
				delays = append(delays, at.Sub(earliest[a]).Minutes())
			}
			if m.w.isPublished(a) {
				inRanges++
			}
			if m.w.class[a] == ClassAttacker {
				attackers++
			}
		}
		volume := float64(len(first[i]))
		f := Feed{Volume: volume, Exclusive: float64(exclusive) / volume, LatencyMinutes: median(delays),
			Bound: 1 - float64(inRanges)/volume, Accuracy: float64(attackers) / volume}
		publishers = append(publishers, PublisherFeed{Slot: k.actor, Key: n, Role: k.role, Feed: f})
		s := sums[k.role]
		if s == nil {
			s = &[5]meanOf{}
			sums[k.role] = s
		}
		for j, x := range []float64{f.Volume, f.Exclusive, f.LatencyMinutes, f.Bound, f.Accuracy} {
			s[j].add(x)
		}
	}
	out := map[Role]Feed{}
	for role, s := range sums {
		out[role] = Feed{Volume: s[0].mean(), Exclusive: s[1].mean(), LatencyMinutes: s[2].mean(), Bound: s[3].mean(), Accuracy: s[4].mean()}
	}
	return out, publishers
}

// meanOf averages the values added to it, skipping NaN.
type meanOf struct {
	sum float64
	n   int
}

func (m *meanOf) add(x float64) {
	if !math.IsNaN(x) {
		m.sum += x
		m.n++
	}
}

func (m *meanOf) mean() float64 {
	if m.n == 0 {
		return math.NaN()
	}
	return m.sum / float64(m.n)
}

// median returns the median of xs, NaN if there are none; it sorts xs.
func median(xs []float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	slices.Sort(xs)
	n := len(xs)
	if n%2 == 1 {
		return xs[n/2]
	}
	return (xs[n/2-1] + xs[n/2]) / 2
}
