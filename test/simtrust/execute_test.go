package simtrust

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/csv"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEstimate(t *testing.T) {
	e := estimate([]float64{1, 2, 3, math.NaN()})
	// mean 2, s = 1, t(2) = 4.303, half-width 4.303/√3
	if e.N != 3 || !near(e.Mean, 2) || !near(e.HalfWidth(), 4.303/math.Sqrt(3)) {
		t.Errorf("estimate = %+v", e)
	}
	if e := estimate([]float64{5}); e.N != 1 || e.Mean != 5 || !math.IsNaN(e.Low) {
		t.Errorf("estimate of one value = %+v, want its mean without interval", e)
	}
	if e := estimate([]float64{math.NaN()}); e.N != 0 || !math.IsNaN(e.Mean) {
		t.Errorf("estimate of none = %+v", e)
	}
	for _, tt := range []struct {
		df   int
		want float64
	}{{1, 12.706}, {19, 2.093}, {30, 2.042}, {35, 2.030}, {120, 1.980}, {100000, 1.960}} {
		if got := tQuantile975(tt.df); math.Abs(got-tt.want) > 0.002 {
			t.Errorf("t(0.975, %d) = %.4f, want %.3f", tt.df, got, tt.want)
		}
	}
}

func TestScenarios(t *testing.T) {
	baseline, err := ScenarioNamed("baseline")
	if err != nil {
		t.Fatal(err)
	}
	// Honest-only and eight models at four fractions, in three settings.
	if got := len(baseline.Configs); got != 3*(1+8*4) {
		t.Errorf("baseline has %d configurations, want %d", got, 3*(1+8*4))
	}
	for _, m := range Models {
		sc, err := ScenarioNamed(string(m))
		if err != nil || len(sc.Configs) == 0 || sc.Configs[0].Model != m {
			t.Errorf("scenario %s: %+v, %v", m, sc.Configs, err)
		}
	}
	reduced, err := ScenarioNamed("reduced")
	if err != nil || reduced.World.Hours != 36 || len(reduced.Configs) != 2*(1+8*2) {
		t.Errorf("reduced: %d hours, %d configurations, %v", reduced.World.Hours, len(reduced.Configs), err)
	}
	if _, err := ScenarioNamed("weekly"); err == nil {
		t.Error("an unknown scenario was found")
	}
}

// tinyScenario runs in seconds: a 12-hour world, three models, two
// settings.
func tinyScenario() Scenario {
	w := DefaultWorld()
	w.Hours, w.Operators, w.AttackersPerHour = 12, 11, 10
	p := DefaultModels()
	p.JoinAt, p.DefectAt = 2*time.Hour, 4*time.Hour
	return Scenario{Name: "tiny", Description: "a test", World: w, Models: p,
		Configs: configsOf([]Model{ModelHonest, ModelNaive, ModelCareful}, []float64{0.4}, []Profile{ProfileDefault, ProfileLab})}
}

func TestExecuteAndReport(t *testing.T) {
	sc := tinyScenario()
	runs := 0
	rep, err := Execute(context.Background(), sc, ExecOptions{Seeds: 3, Workers: 2,
		Progress: func(done, total int, _ RunSpec, _ time.Duration) { runs, _ = done, total }})
	if err != nil {
		t.Fatal(err)
	}
	if runs != 18 || rep.Runs != 18 || len(rep.Aggregates) != 6 || rep.Hours != 12 {
		t.Fatalf("%d runs reported, %d counted, %d aggregates, %d hours", runs, rep.Runs, len(rep.Aggregates), rep.Hours)
	}
	if problems := CheckV01(rep); len(problems) > 0 {
		t.Errorf("v0.1 invariants: %q", problems)
	}
	dir, info := t.TempDir(), ReportInfo{Version: "v0.1.0-test", Generated: testStart, ADR: "../adr/0034.md"}
	if err := WriteReport(dir, rep, info); err != nil {
		t.Fatal(err)
	}
	readme, err := os.ReadFile(filepath.Join(dir, reportFile)) // #nosec G304 -- a file the test wrote.
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# Trust simulation: tiny\n", "| Format | 1 |", "`v0.1.0-test`", "### How many trusted remotes a ban needs",
		"needs **3** fully\n  trusted remotes at confidence 0.8, which score 3 × 0.8 = 2.4.", "needs **2** fully", "### Adversaries at 40 %",
		"No defector's weight fell to 0", "Hours: `lab`", "A careful poisoner's benign-set bound", "### Settings `lab`", "## Hour by hour", "### False ban hours per hour", "## Feeds of the publishers", "## Limitations",
		"[ADR 0034](../adr/0034.md) defines", "by the class of the victim", "| Settings | `cdn` | `crawler` | `customer` | `nat` |",
		"every value has at most 6 significant digits.",
		"(`neutralized_share`, `neutralization_hours`, `neutralization_events`, `newcomers_converged`, `newcomer_convergence_hours`, `whitewash_payoff`, `keys_burned`)",
		"None of the remotes reported an attacker of the first row", "\n| 0 | ", "Every newcomer had converged at the first probe",
	} {
		if !bytes.Contains(readme, []byte(want)) {
			t.Errorf("report lacks %q", want)
		}
	}
	checkTables(t, string(readme))

	summary := readCSV(t, filepath.Join(dir, summaryFile), false)
	// Every metric and the false bans of the four benign classes.
	if len(summary) != 1+6*(int(numMetrics)+4) || strings.Join(summary[0], ",") != "model,fraction,profile,metric,n,mean,ci_low,ci_high" {
		t.Errorf("summary.csv has %d lines, header %q", len(summary), summary[0])
	}
	if last := summary[len(summary)-1]; last[3] != "false_bans_nat" {
		t.Errorf("last row of summary.csv: %q, want the false bans of NAT addresses", last)
	}
	hourly := readCSV(t, filepath.Join(dir, hourlyFile), true)
	if len(hourly) < 1+6*12*2*9 || hourly[1][3] != "0" || hourly[1][4] != "hour" {
		t.Errorf("hourly.csv.gz has %d lines, first %q", len(hourly), hourly[1])
	}
	publishers := readCSV(t, filepath.Join(dir, publishersFile), true)
	// Every run has the observer and 10 publishers, all of which report.
	if len(publishers) != 1+18*11 || publishers[1][6] != string(RoleObserver) {
		t.Errorf("publishers.csv.gz has %d lines, first %q", len(publishers), publishers[1])
	}
	for _, name := range []string{feedsFile, corroborationFile} {
		if rows := readCSV(t, filepath.Join(dir, name), false); len(rows) < 2 {
			t.Errorf("%s has %d lines", name, len(rows))
		}
	}
	// The same report gives the same files, the gzipped one included.
	again := t.TempDir()
	if err := WriteReport(again, rep, info); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{reportFile, summaryFile, hourlyFile, publishersFile} {
		a, _ := os.ReadFile(filepath.Join(dir, name))   // #nosec G304 -- a file the test wrote.
		b, _ := os.ReadFile(filepath.Join(again, name)) // #nosec G304 -- a file the test wrote.
		if !bytes.Equal(a, b) {
			t.Errorf("%s differs between two writes of one report", name)
		}
	}
}

// TestCheckV01OneSeed checks that a quick run of a single seed, which has
// no intervals, is checked by its means.
func TestCheckV01OneSeed(t *testing.T) {
	rep, err := Execute(context.Background(), tinyScenario(), ExecOptions{Seeds: 1})
	if err != nil {
		t.Fatal(err)
	}
	if problems := CheckV01(rep); len(problems) > 0 {
		t.Errorf("v0.1 invariants of one seed: %q", problems)
	}
}

// checkTables checks that every row of a Markdown table has as many cells
// as its header.
func checkTables(t *testing.T, md string) {
	t.Helper()
	cells := -1
	for i, line := range strings.Split(md, "\n") {
		if !strings.HasPrefix(line, "|") {
			cells = -1
			continue
		}
		n := strings.Count(line, "|") - 1
		if cells >= 0 && n != cells {
			t.Errorf("report line %d has %d cells, its table %d: %s", i+1, n, cells, line)
		}
		cells = n
	}
}

func readCSV(t *testing.T, path string, gzipped bool) [][]string {
	t.Helper()
	f, err := os.Open(path) // #nosec G304 -- a file the test wrote.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var r io.Reader = f
	if gzipped {
		gz, err := gzip.NewReader(f)
		if err != nil {
			t.Fatal(err)
		}
		r = gz
	}
	rows, err := csv.NewReader(r).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// TestOnOffPhaseNote checks that the report says when the on-off
// attackers poison if the hours it shows all fall in their off phase.
func TestOnOffPhaseNote(t *testing.T) {
	md := &markdown{rep: &Report{Scenario: Scenario{Models: DefaultModels()}}}
	md.onOffPhase([]int{23, 47, 71, 95})
	if want := "poison in hours 48 to 53, 72 to 77 and so on"; !strings.Contains(md.String(), want) {
		t.Errorf("note %q lacks %q", md.String(), want)
	}
	md = &markdown{rep: md.rep}
	md.onOffPhase([]int{23, 47, 50})
	if md.Len() != 0 {
		t.Errorf("note %q, want none: hour 50 is in the on phase", md.String())
	}
}

func TestCSVFloat(t *testing.T) {
	for _, tt := range []struct {
		x    float64
		want string
	}{
		{0.89531234, "0.895312"}, {12345.6, "12345.6"}, {1234567.8, "1234570"}, {-2.5, "-2.5"}, {1, "1"}, {0, "0"},
		{0.000012345, "0.000012345"}, {math.NaN(), ""},
	} {
		if got := csvFloat(tt.x); got != tt.want {
			t.Errorf("csvFloat(%v) = %q, want %q", tt.x, got, tt.want)
		}
	}
	// Rounded up beyond the largest float, a value keeps its exponent.
	if got := formatSig(math.MaxFloat64, 4); got != "1.798e+308" {
		t.Errorf("formatSig(MaxFloat64, 4) = %q", got)
	}
}

// TestEstimateFields checks that a mean and its bounds are rounded to the
// second significant digit of the half-width, at most to 6 digits, so
// that no interval collapses.
func TestEstimateFields(t *testing.T) {
	for _, tt := range []struct {
		e    Estimate
		want string
	}{
		{Estimate{Mean: 0.92991, Low: 0.92804, High: 0.93178, N: 20}, "20,0.9299,0.928,0.9318"},
		{Estimate{Mean: 5055.3, Low: 4451.2, High: 5659.4, N: 20}, "20,5060,4450,5660"},
		{Estimate{Mean: 120.0123, Low: 120.0033, High: 120.0213, N: 20}, "20,120.012,120.003,120.021"},
		{Estimate{Mean: 0.25, Low: -0.0021, High: 0.5021, N: 20}, "20,0.25,-0.002,0.5"},
		{Estimate{Mean: 1, Low: 1, High: 1, N: 20}, "20,1,1,1"},
		{Estimate{Mean: 5.123456789, Low: math.NaN(), High: math.NaN(), N: 1}, "1,5.12346,,"},
		{Estimate{Mean: math.NaN(), Low: math.NaN(), High: math.NaN()}, "0,,,"},
	} {
		if got := strings.Join(estimateFields(tt.e), ","); got != tt.want {
			t.Errorf("estimateFields(%+v) = %s, want %s", tt.e, got, tt.want)
		}
	}
}

func TestCellFormats(t *testing.T) {
	for _, tt := range []struct {
		e    Estimate
		want string
	}{
		{Estimate{Mean: 0.93412, Low: 0.93, High: 0.938, N: 20}, "0.934 ± 0.004"},
		{Estimate{Mean: 302.4, Low: 290, High: 314.8, N: 20}, "302 ± 12.4"},
		{Estimate{Mean: 1, Low: 1, High: 1, N: 17}, "1.00 ± 0.000 (n=17)"},
		{Estimate{Mean: math.NaN(), N: 0}, "—"},
	} {
		if got := cell(tt.e, 20); got != tt.want {
			t.Errorf("cell(%+v) = %q, want %q", tt.e, got, tt.want)
		}
	}
}

// TestExecuteResumes checks the run cache: a second execution takes every
// run from it and reports the same, and a damaged entry is run again.
func TestExecuteResumes(t *testing.T) {
	sc := tinyScenario()
	sc.Configs = sc.Configs[:2]
	cache := t.TempDir()
	first, err := Execute(context.Background(), sc, ExecOptions{Seeds: 2, Cache: cache})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := filepath.Glob(filepath.Join(cache, "*.gob"))
	if err != nil || len(entries) != 4 || first.Cached != 0 {
		t.Fatalf("%d cached runs after the first execution (%d taken from the cache), %v; want 4 and 0", len(entries), first.Cached, err)
	}
	if err := os.WriteFile(entries[0], []byte("damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := Execute(context.Background(), sc, ExecOptions{Seeds: 2, Cache: cache})
	if err != nil {
		t.Fatal(err)
	}
	if second.Cached != 3 {
		t.Errorf("%d runs taken from the cache, want 3: one entry was damaged", second.Cached)
	}
	// Every metric, feed, false ban by class and corroboration survives the
	// cache: the data files of both reports are the same.
	dirs := []string{t.TempDir(), t.TempDir()}
	for i, rep := range []*Report{first, second} {
		if err := WriteReport(dirs[i], rep, ReportInfo{Version: "test", Generated: testStart}); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{summaryFile, hourlyFile, feedsFile, publishersFile, corroborationFile} {
		a, errA := os.ReadFile(filepath.Join(dirs[0], name)) // #nosec G304 -- a file the test wrote.
		b, errB := os.ReadFile(filepath.Join(dirs[1], name)) // #nosec G304 -- a file the test wrote.
		if errA != nil || errB != nil || !bytes.Equal(a, b) {
			t.Errorf("%s differs between the first and the resumed execution (%v, %v)", name, errA, errB)
		}
	}
}
