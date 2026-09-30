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

// tinyScenario runs in seconds: a 12-hour world, two models, one setting.
func tinyScenario() Scenario {
	w := DefaultWorld()
	w.Hours, w.Operators, w.AttackersPerHour = 12, 11, 10
	p := DefaultModels()
	p.JoinAt, p.DefectAt = 2*time.Hour, 4*time.Hour
	return Scenario{Name: "tiny", Description: "a test", World: w, Models: p,
		Configs: configsOf([]Model{ModelHonest, ModelNaive}, []float64{0.4}, []Profile{ProfileDefault, ProfileLab})}
}

func TestExecuteAndReport(t *testing.T) {
	sc := tinyScenario()
	runs := 0
	rep, err := Execute(context.Background(), sc, ExecOptions{Seeds: 3, Workers: 2,
		Progress: func(done, total int, _ RunSpec, _ time.Duration) { runs, _ = done, total }})
	if err != nil {
		t.Fatal(err)
	}
	if runs != 12 || rep.Runs != 12 || len(rep.Aggregates) != 4 || rep.Hours != 12 {
		t.Fatalf("%d runs reported, %d counted, %d aggregates, %d hours", runs, rep.Runs, len(rep.Aggregates), rep.Hours)
	}
	if problems := CheckV01(rep); len(problems) > 0 {
		t.Errorf("v0.1 invariants: %q", problems)
	}
	dir := t.TempDir()
	if err := WriteReport(dir, rep, ReportInfo{Version: "v0.1.0-test", Generated: testStart}); err != nil {
		t.Fatal(err)
	}
	readme, err := os.ReadFile(filepath.Join(dir, reportFile)) // #nosec G304 -- a file the test wrote.
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# Trust simulation: tiny\n", "| Format | 1 |", "`v0.1.0-test`", "### How many trusted remotes a ban needs",
		"needs **3** fully\n  trusted remotes at confidence 0.8, which score 3 × 0.8 = 2.4.", "needs **2** fully", "### Adversaries at 40 %",
		"No defector lost its weight", "### Settings `lab`", "## Hour by hour", "## Feeds of the publishers", "## Limitations",
	} {
		if !bytes.Contains(readme, []byte(want)) {
			t.Errorf("report lacks %q", want)
		}
	}
	checkTables(t, string(readme))

	summary := readCSV(t, filepath.Join(dir, summaryFile), false)
	if len(summary) != 1+4*int(numMetrics) || strings.Join(summary[0], ",") != "model,fraction,profile,metric,n,mean,ci_low,ci_high" {
		t.Errorf("summary.csv has %d lines, header %q", len(summary), summary[0])
	}
	hourly := readCSV(t, filepath.Join(dir, hourlyFile), true)
	if len(hourly) < 1+4*12*2*9 || hourly[1][3] != "0" || hourly[1][4] != "hour" {
		t.Errorf("hourly.csv.gz has %d lines, first %q", len(hourly), hourly[1])
	}
	for _, name := range []string{feedsFile, corroborationFile} {
		if rows := readCSV(t, filepath.Join(dir, name), false); len(rows) < 2 {
			t.Errorf("%s has %d lines", name, len(rows))
		}
	}
	// The same report gives the same files, the gzipped one included.
	again := t.TempDir()
	if err := WriteReport(again, rep, ReportInfo{Version: "v0.1.0-test", Generated: testStart}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{reportFile, summaryFile, hourlyFile} {
		a, _ := os.ReadFile(filepath.Join(dir, name))   // #nosec G304 -- a file the test wrote.
		b, _ := os.ReadFile(filepath.Join(again, name)) // #nosec G304 -- a file the test wrote.
		if !bytes.Equal(a, b) {
			t.Errorf("%s differs between two writes of one report", name)
		}
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
