package sim

import (
	"cmp"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ReportFormat versions the layout of the reports and the per-seed cache.
const ReportFormat = "routing-report/1"

// Run identifies one run: a scenario in a variant with a seed.
type Run struct {
	Scenario *Scenario
	Variant  Variant
	Seed     int64
}

// Name is the run's name, e.g. A-eclipse/plain/seed-03.
func (r Run) Name() string {
	return fmt.Sprintf("%s/%s/seed-%02d", r.Scenario.Name, r.Variant.Name, r.Seed)
}

// Runs returns the runs of the scenarios with seeds 1 to seeds, every
// variant of each.
func Runs(scenarios []*Scenario, seeds int) ([]Run, error) {
	all := variants()
	var out []Run
	for _, sc := range scenarios {
		for _, name := range sc.Variants {
			v, ok := all[name]
			if !ok {
				return nil, fmt.Errorf("scenario %s: unknown variant %q", sc.Name, name)
			}
			for seed := int64(1); seed <= int64(seeds); seed++ {
				out = append(out, Run{Scenario: sc, Variant: v, Seed: seed})
			}
		}
	}
	return out, nil
}

// Select returns the scenarios a SCENARIO value names: a scenario name, a
// group (A, B, C, T, reduced) or all, which is every group but reduced.
func Select(name string) ([]*Scenario, error) {
	all := Scenarios()
	if sc, ok := all[name]; ok {
		return []*Scenario{sc}, nil
	}
	var out []*Scenario
	for _, sc := range all {
		if sc.Group == name || (name == "all" && sc.Group != "reduced") {
			out = append(out, sc)
		}
	}
	if len(out) == 0 {
		names := make([]string, 0, len(all))
		for n := range all {
			names = append(names, n)
		}
		slices.Sort(names)
		return nil, fmt.Errorf("unknown scenario %q; use a group (A, B, C, T, reduced, all) or one of %s", name,
			strings.Join(names, ", "))
	}
	slices.SortFunc(out, func(a, b *Scenario) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// Cache keeps the result of every run, so that an interrupted baseline
// resumes where it stopped. Each result records the OBIE version that
// produced it; a cache must be emptied when the code changes. An empty Dir
// caches nothing.
type Cache struct{ Dir string }

func (c Cache) path(r Run) string {
	return filepath.Join(c.Dir, r.Scenario.Name, r.Variant.Name, fmt.Sprintf("seed-%02d.json", r.Seed))
}

// Load returns the cached result of r, or nil.
func (c Cache) Load(r Run) (*Result, error) {
	if c.Dir == "" {
		return nil, nil
	}
	data, err := os.ReadFile(c.path(r))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cached struct {
		Format string `json:"format"`
		Result
	}
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, fmt.Errorf("cached result %s: %w", c.path(r), err)
	}
	if cached.Format != ReportFormat {
		return nil, nil
	}
	return &cached.Result, nil
}

// Store caches the result of r.
func (c Cache) Store(r Run, res *Result) error {
	if c.Dir == "" {
		return nil
	}
	p := c.path(r)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(struct {
		Format string `json:"format"`
		*Result
	}{ReportFormat, res}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(data, '\n'), 0o600)
}

// Header is what a report records about how it was made besides its
// results.
type Header struct {
	// Generated is when the report was written, in real time.
	Generated time.Time
}

// CheckResult is the outcome of a scenario's check.
type CheckResult struct {
	Name, Detail string
	Pass         bool
}

// Evaluate runs the scenario's checks over the results by variant.
func Evaluate(sc *Scenario, byVariant map[string][]*Result) []CheckResult {
	out := make([]CheckResult, 0, len(sc.Checks))
	for _, c := range sc.Checks {
		pass, detail := c.Test(byVariant)
		out = append(out, CheckResult{Name: c.Name, Detail: detail, Pass: pass})
	}
	return out
}

// metricLabels name the metrics in the report, in order.
var metricLabels = [][2]string{
	{mDelivery, "Delivery ratio"},
	{mP50, "Latency p50 (ms)"},
	{mP99, "Latency p99 (ms)"},
	{mMax, "Latency max (ms)"},
	{mDuplicates, "Duplicate factor (copies per delivered verdict)"},
	{mHops, "Hop count, mean"},
	{mSybilShare, "Sybil share of mesh slots"},
	{mRecovery, "Mesh recovery time (s), seeds that recovered"},
	{mRecovered, "Seeds whose mesh recovered (share)"},
	{mRetained, "Trusted verdicts retained under flood"},
	{mMeshDegree, "Mesh degree at the end"},
}

// WriteReport writes routing-<scenario>.md, .csv and -seeds.csv to dir.
func WriteReport(dir string, h Header, sc *Scenario, byVariant map[string][]*Result, checks []CheckResult) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	base := filepath.Join(dir, "routing-"+sc.Name)
	for suffix, write := range map[string]func(io.Writer) error{
		".md":        func(w io.Writer) error { return writeMarkdown(w, h, sc, byVariant, checks) },
		".csv":       func(w io.Writer) error { return writeSummaryCSV(w, sc, byVariant) },
		"-seeds.csv": func(w io.Writer) error { return writeSeedsCSV(w, sc, byVariant) },
	} {
		f, err := os.Create(base + suffix) // #nosec G304 -- the operator chooses the report directory.
		if err != nil {
			return err
		}
		if err := write(f); err != nil {
			_ = f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	return nil
}

// metricNames returns every metric of the results in report order: the
// labeled ones, then the windows.
func metricNames(byVariant map[string][]*Result) []string {
	have := map[string]bool{}
	for _, rs := range byVariant {
		for _, r := range rs {
			for m := range r.Metrics {
				have[m] = true
			}
		}
	}
	var out []string
	for _, l := range metricLabels {
		if have[l[0]] {
			out = append(out, l[0])
		}
	}
	var windows []string
	for m := range have {
		if strings.HasPrefix(m, windowPrefix) {
			windows = append(windows, m)
		}
	}
	slices.SortFunc(windows, func(a, b string) int {
		if ra, rb := windowRank(a), windowRank(b); ra != rb {
			return ra - rb
		}
		return strings.Compare(a, b)
	})
	return append(out, windows...)
}

// windowRank orders the windows in time: before a disruption, during it,
// after it, then the others.
func windowRank(metric string) int {
	name := strings.TrimPrefix(metric, windowPrefix)
	for i, first := range []string{"before ", "during ", "after "} {
		if strings.HasPrefix(name, first) {
			return i
		}
	}
	return 3
}

// metricValues returns the values of the metric the report summarizes. A
// recovery time counts only where the mesh recovered: a run that did not
// recover records how long it watched.
func metricValues(results []*Result, metric string) []float64 {
	if metric != mRecovery {
		return values(results, metric)
	}
	var out []float64
	for _, r := range results {
		if v, ok := r.Metrics[mRecovery]; ok && r.Metrics[mRecovered] == 1 {
			out = append(out, v)
		}
	}
	return out
}

// metricSummary is the summary of the metric the report shows.
func metricSummary(results []*Result, metric string) summary {
	upper := math.Inf(1)
	switch {
	case metric == mDelivery, metric == mSybilShare, metric == mRecovered, metric == mRetained,
		strings.HasPrefix(metric, windowPrefix):
		upper = 1
	}
	return clipped(summarize(metricValues(results, metric)), upper)
}

// clipped returns s with its confidence interval clipped to [0, upper]:
// no metric is negative, and the t interval of a share near 0 or 1 reaches
// past what the share can be.
func clipped(s summary, upper float64) summary {
	if s.n > 1 {
		s.low, s.high = max(s.low, 0), min(s.high, upper)
	}
	return s
}

func label(metric string) string {
	for _, l := range metricLabels {
		if l[0] == metric {
			return l[1]
		}
	}
	if w, ok := strings.CutPrefix(metric, windowPrefix); ok {
		return "Delivery ratio, " + w
	}
	return metric
}

// keys returns the keys of the maps of every result that get picks, sorted
// by their order in order if listed there, else by name.
func keys(byVariant map[string][]*Result, get func(*Result) map[string]float64, order []string) []string {
	have := map[string]bool{}
	for _, rs := range byVariant {
		for _, r := range rs {
			for k := range get(r) {
				have[k] = true
			}
		}
	}
	out := make([]string, 0, len(have))
	for k := range have {
		out = append(out, k)
	}
	rank := func(k string) int {
		if i := slices.Index(order, k); i >= 0 {
			return i
		}
		return len(order)
	}
	slices.SortFunc(out, func(a, b string) int {
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra - rb
		}
		return strings.Compare(a, b)
	})
	return out
}

// hopOrder lists the hop bins in order.
func hopOrder() []string {
	out := make([]string, 0, hopBins+1)
	for h := 1; h <= hopBins; h++ {
		s := strconv.Itoa(h)
		if h == hopBins {
			s += "+"
		}
		out = append(out, s)
	}
	return append(out, "unknown")
}

// mapValues returns the value of key in each result's map, 0 where absent
// (a hop bin or loss cause a seed never saw).
func mapValues(results []*Result, get func(*Result) map[string]float64, key string) []float64 {
	out := make([]float64, 0, len(results))
	for _, r := range results {
		out = append(out, get(r)[key])
	}
	return out
}

// formatSummary shows a summary as its mean and 95 % confidence interval.
func formatSummary(s summary, digits int) string {
	switch s.n {
	case 0:
		return "–"
	case 1:
		return strconv.FormatFloat(s.mean, 'f', digits, 64) + " (1 seed)"
	}
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', digits, 64) }
	return fmt.Sprintf("%s [%s, %s]", f(s.mean), f(s.low), f(s.high))
}

// digitsFor is the number of decimals a metric is shown with.
func digitsFor(metric string) int {
	switch metric {
	case mP50, mP99, mMax, mRecovery:
		return 1
	case mDuplicates, mHops, mMeshDegree:
		return 2
	default:
		return 4
	}
}

func writeMarkdown(out io.Writer, h Header, sc *Scenario, byVariant map[string][]*Result, checks []CheckResult) error {
	var b strings.Builder
	p := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	all := variants()
	p("# Routing simulation: %s\n\n%s.\n\n", sc.Name, capitalize(sc.Summary))
	p("| Report | |\n|---|---|\n")
	p("| Format | `%s` |\n| OBIE | %s |\n", ReportFormat, codeList(versions(byVariant)))
	for _, mod := range []string{"github.com/libp2p/go-libp2p-pubsub", "github.com/libp2p/go-libp2p"} {
		p("| %s | `%s` |\n", strings.TrimPrefix(mod, "github.com/libp2p/"), moduleVersion(mod))
	}
	p("| Go | `%s` |\n| Seeds | %s per variant |\n| Generated | %s |\n| Harness | `make sim-routing SCENARIO=%s` (test/sim, ADR 0033) |\n\n",
		runtime.Version(), seedList(byVariant), h.Generated.UTC().Format("2006-01-02"), sc.Name)

	p("## Scenario\n\n| Parameter | Value |\n|---|---|\n")
	for _, kv := range sc.Params {
		p("| %s | %s |\n", kv[0], kv[1])
	}
	p("\n## Variants\n\n")
	for _, v := range sc.Variants {
		p("- `%s`: %s.\n", v, all[v].Summary)
	}

	p("\n## Results\n\nEach cell is the mean over the seeds with its 95 %% confidence interval (Student's t) in brackets. Delivery ratios, of the whole run and of its windows, count the pairs of a verdict and an honest node that runs at the end of the run.\n\n")
	header := func(first string) {
		p("| %s |", first)
		for _, v := range sc.Variants {
			p(" `%s` |", v)
		}
		p("\n|---|")
		for range sc.Variants {
			p("---|")
		}
		p("\n")
	}
	header("Metric")
	for _, m := range metricNames(byVariant) {
		p("| %s |", label(m))
		for _, v := range sc.Variants {
			s := metricSummary(byVariant[v], m)
			p(" %s", formatSummary(s, digitsFor(m)))
			if seeds := len(byVariant[v]); s.n > 1 && s.n < seeds {
				p(" (%d of %d seeds)", s.n, seeds)
			}
			p(" |")
		}
		p("\n")
	}
	p("| Hop count predicted, ln N / ln(D−1) |")
	for _, v := range sc.Variants {
		p(" %.2f (N %d, D %d) |", predictedHops(sc.Honest, all[v].D), sc.Honest, all[v].D)
	}
	p("\n")

	p("\n## Hop-count distribution\n\nShare of the delivered verdicts by the hops of the path they took, from the per-event trace; `unknown` paths ran through a node that kept no trace.\n\n")
	header("Hops")
	hopsOf := func(r *Result) map[string]float64 { return r.Hops }
	for _, k := range keys(byVariant, hopsOf, hopOrder()) {
		p("| %s |", k)
		for _, v := range sc.Variants {
			p(" %s |", formatSummary(clipped(summarize(mapValues(byVariant[v], hopsOf, k)), 1), 4))
		}
		p("\n")
	}

	p("\n## Loss by cause\n\nShare of all pairs of a verdict and an honest node that were lost, by the outcome of the first copy the node validated (copies dropped as duplicates or from a full queue do not count), or by why no copy came.\n\n")
	lossOf := func(r *Result) map[string]float64 { return r.Loss }
	causes := keys(byVariant, lossOf, nil)
	if len(causes) == 0 {
		p("No verdict was lost.\n")
	} else {
		header("Cause")
		for _, k := range causes {
			p("| `%s` |", k)
			for _, v := range sc.Variants {
				p(" %s |", formatSummary(clipped(summarize(mapValues(byVariant[v], lossOf, k)), 1), 4))
			}
			p("\n")
		}
	}

	if len(checks) > 0 {
		p("\n## Checks\n\n| Check | Result | Measured |\n|---|---|---|\n")
		for _, c := range checks {
			result := "pass"
			if !c.Pass {
				result = "**fail**"
			}
			p("| %s | %s | %s |\n", c.Name, result, c.Detail)
		}
	}

	p("\n## Runs\n\n| Variant | Seeds | Honest nodes | Adversaries | Links | Verdicts | Wall time per seed (s) |\n|---|---|---|---|---|---|---|\n")
	for _, v := range sc.Variants {
		rs := byVariant[v]
		if len(rs) == 0 {
			continue
		}
		wall := make([]float64, len(rs))
		for i, r := range rs {
			wall[i] = r.WallSeconds
		}
		p("| `%s` | %d | %d | %d | %d | %d | %.0f |\n", v, len(rs), rs[0].Honest, rs[0].Adversaries, rs[0].Links,
			rs[0].Events, summarize(wall).mean)
	}
	_, err := io.WriteString(out, b.String())
	return err
}

// predictedHops is ln N / ln(D−1), the diameter of a random D-regular
// mesh of N nodes.
func predictedHops(n, d int) float64 { return math.Log(float64(n)) / math.Log(float64(d-1)) }

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// moduleVersion returns the version of a module in the build or, since a
// test binary lists none, the version the go.mod above the working
// directory requires; unknown if neither has it.
func moduleVersion(path string) string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range info.Deps {
			if dep.Path == path {
				return dep.Version
			}
		}
	}
	for dir, _ := os.Getwd(); dir != ""; {
		if data, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil { // #nosec G304 -- the module's own go.mod.
			return requiredVersion(data, path)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "unknown"
}

// requiredVersion returns the version go.mod data requires of the module
// path, or unknown.
func requiredVersion(data []byte, path string) string {
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "require "))
		if len(fields) >= 2 && fields[0] == path {
			return fields[1]
		}
	}
	return "unknown"
}

// versions returns the OBIE versions that produced the results, sorted.
func versions(byVariant map[string][]*Result) []string {
	have := map[string]bool{}
	for _, rs := range byVariant {
		for _, r := range rs {
			have[r.Version] = true
		}
	}
	out := make([]string, 0, len(have))
	for v := range have {
		out = append(out, v)
	}
	slices.Sort(out)
	return out
}

// codeList shows each string as code, comma-separated.
func codeList(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = "`" + s + "`"
	}
	return strings.Join(quoted, ", ")
}

// seedList shows the seeds of the results, e.g. "1–20" or "1, 3".
func seedList(byVariant map[string][]*Result) string {
	have := map[int64]bool{}
	for _, rs := range byVariant {
		for _, r := range rs {
			have[r.Seed] = true
		}
	}
	seeds := make([]int64, 0, len(have))
	for s := range have {
		seeds = append(seeds, s)
	}
	slices.Sort(seeds)
	if n := len(seeds); n > 2 && seeds[n-1]-seeds[0] == int64(n-1) {
		return fmt.Sprintf("%d–%d", seeds[0], seeds[n-1])
	}
	parts := make([]string, len(seeds))
	for i, s := range seeds {
		parts[i] = strconv.FormatInt(s, 10)
	}
	return strings.Join(parts, ", ")
}

// sortedBySeed returns the results in seed order.
func sortedBySeed(results []*Result) []*Result {
	out := slices.Clone(results)
	slices.SortFunc(out, func(a, b *Result) int { return cmp.Compare(a.Seed, b.Seed) })
	return out
}

func writeSummaryCSV(out io.Writer, sc *Scenario, byVariant map[string][]*Result) error {
	w := csv.NewWriter(out)
	_ = w.Write([]string{"scenario", "variant", "metric", "n", "mean", "ci95_low", "ci95_high", "sd", "min", "max"})
	row := func(v, metric string, s summary) {
		f := func(x float64) string {
			if math.IsNaN(x) {
				return ""
			}
			return strconv.FormatFloat(x, 'g', 8, 64)
		}
		_ = w.Write([]string{sc.Name, v, metric, strconv.Itoa(s.n), f(s.mean), f(s.low), f(s.high), f(s.sd), f(s.minV), f(s.maxV)})
	}
	hopsOf := func(r *Result) map[string]float64 { return r.Hops }
	lossOf := func(r *Result) map[string]float64 { return r.Loss }
	for _, v := range sc.Variants {
		for _, m := range metricNames(byVariant) {
			row(v, m, metricSummary(byVariant[v], m))
		}
		for _, k := range keys(byVariant, hopsOf, hopOrder()) {
			row(v, "hops:"+k, clipped(summarize(mapValues(byVariant[v], hopsOf, k)), 1))
		}
		for _, k := range keys(byVariant, lossOf, nil) {
			row(v, "loss:"+k, clipped(summarize(mapValues(byVariant[v], lossOf, k)), 1))
		}
	}
	w.Flush()
	return w.Error()
}

func writeSeedsCSV(out io.Writer, sc *Scenario, byVariant map[string][]*Result) error {
	w := csv.NewWriter(out)
	_ = w.Write([]string{"scenario", "variant", "seed", "metric", "value"})
	for _, v := range sc.Variants {
		for _, r := range byVariant[v] {
			seed := strconv.FormatInt(r.Seed, 10)
			emit := func(metric string, value float64) {
				_ = w.Write([]string{sc.Name, v, seed, metric, strconv.FormatFloat(value, 'g', 8, 64)})
			}
			for _, m := range sortedKeys(r.Metrics) {
				emit(m, r.Metrics[m])
			}
			for _, k := range sortedKeys(r.Hops) {
				emit("hops:"+k, r.Hops[k])
			}
			for _, k := range sortedKeys(r.Loss) {
				emit("loss:"+k, r.Loss[k])
			}
			emit("wall_seconds", r.WallSeconds)
		}
	}
	w.Flush()
	return w.Error()
}

func sortedKeys(m map[string]float64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
