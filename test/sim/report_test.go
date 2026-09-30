package sim

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeResults returns results of two seeds in plain (losing a verdict in
// seed 2) and v0.1.
func fakeResults() map[string][]*Result {
	res := func(variant string, seed int64, delivery float64, loss map[string]float64) *Result {
		return &Result{Scenario: "A-eclipse", Variant: variant, Seed: seed, Version: "v0.1.0-7-gabc",
			Metrics: map[string]float64{mDelivery: delivery, mP99: 150 + float64(seed), windowPrefix + "during the attack": delivery},
			Hops:    map[string]float64{"1": 0.5, "2": 0.5}, Loss: loss,
			Honest: 1000, Adversaries: 4000, Links: 410000, Events: 180, WallSeconds: 100}
	}
	return map[string][]*Result{
		Plain: {res(Plain, 1, 1, map[string]float64{}), res(Plain, 2, 0.9, map[string]float64{causeNeverReceived: 0.1})},
		V01:   {res(V01, 1, 1, map[string]float64{}), res(V01, 2, 1, map[string]float64{})},
	}
}

func TestWriteReport(t *testing.T) {
	dir := t.TempDir()
	sc := Scenarios()["A-eclipse"]
	byVariant := fakeResults()
	checks := Evaluate(sc, byVariant)
	if err := WriteReport(dir, Header{Generated: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)}, sc, byVariant, checks); err != nil {
		t.Fatal(err)
	}
	md, err := os.ReadFile(filepath.Join(dir, "routing-A-eclipse.md")) // #nosec G304 -- a file the test wrote.
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# Routing simulation: A-eclipse",
		"| Format | `" + ReportFormat + "` |",
		"| OBIE | `v0.1.0-7-gabc` |",
		"| go-libp2p-pubsub | `v0.17.0` |",
		"| Seeds | 1, 2 per variant |",
		"| Generated | 2026-09-30 |",
		"| Delivery ratio | 0.9500 [0.3147, 1.5853] | – | 1.0000 [1.0000, 1.0000] |",
		"| Delivery ratio, during the attack |",
		"| Hop count predicted, ln N / ln(D−1) | 3.55 (N 1000, D 8) | 3.55 (N 1000, D 8) | 4.29 (N 1000, D 6) |",
		"| `never_received` | 0.0500 [-0.5853, 0.6853] | – | 0.0000 [0.0000, 0.0000] |",
		"| plain GossipSub loses verdicts (measurable loss) | **fail** |",
		"| `v0.1` | 2 | 1000 | 4000 | 410000 | 180 | 100 |",
	} {
		if !strings.Contains(string(md), want) {
			t.Errorf("the report lacks %q:\n%s", want, md)
		}
	}
	summary := readCSV(t, filepath.Join(dir, "routing-A-eclipse.csv"))
	if !slices.Equal(summary[0], []string{"scenario", "variant", "metric", "n", "mean", "ci95_low", "ci95_high", "sd", "min", "max"}) {
		t.Errorf("summary header %v", summary[0])
	}
	if !slices.ContainsFunc(summary, func(r []string) bool {
		return slices.Equal(r[:5], []string{"A-eclipse", "plain", "loss:never_received", "2", "0.05"})
	}) {
		t.Errorf("the summary has no row of plain's never_received loss: %v", summary)
	}
	seeds := readCSV(t, filepath.Join(dir, "routing-A-eclipse-seeds.csv"))
	if !slices.ContainsFunc(seeds, func(r []string) bool {
		return slices.Equal(r, []string{"A-eclipse", "plain", "2", "delivery_ratio", "0.9"})
	}) {
		t.Errorf("the seed rows lack plain seed 2's delivery ratio: %v", seeds)
	}
}

func readCSV(t *testing.T, path string) [][]string {
	t.Helper()
	f, err := os.Open(path) // #nosec G304 G703 -- a file the test wrote.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestValidationChecks(t *testing.T) {
	sc := Scenarios()["A-coldboot"]
	lossy := &Result{Metrics: map[string]float64{mDelivery: 0.95}}
	whole := &Result{Metrics: map[string]float64{mDelivery: 1}}
	for _, tc := range []struct {
		name        string
		plain       []*Result
		paper       []*Result
		plainPasses bool
		paperPasses bool
	}{
		{"as the paper", []*Result{lossy, lossy}, []*Result{whole, whole}, true, true},
		{"plain loses nothing", []*Result{whole, whole}, []*Result{whole}, false, true},
		{"paper loses", []*Result{lossy}, []*Result{whole, lossy}, true, false},
		{"no results", nil, nil, false, false},
	} {
		got := Evaluate(sc, map[string][]*Result{Plain: tc.plain, Paper: tc.paper})
		if got[0].Pass != tc.plainPasses || got[1].Pass != tc.paperPasses {
			t.Errorf("%s: checks pass %v and %v, want %v and %v", tc.name, got[0].Pass, got[1].Pass, tc.plainPasses, tc.paperPasses)
		}
	}
}

func TestCache(t *testing.T) {
	c := Cache{Dir: t.TempDir()}
	sc := Scenarios()["reduced"]
	r := Run{Scenario: sc, Variant: variants()[V01], Seed: 3}
	if res, err := c.Load(r); res != nil || err != nil {
		t.Fatalf("Load before Store = %v, %v; want nil, nil", res, err)
	}
	want := fakeResults()[V01][0]
	if err := c.Store(r, want); err != nil {
		t.Fatal(err)
	}
	got, err := c.Load(r)
	if err != nil || got == nil || got.Version != want.Version || got.Metrics[mP99] != want.Metrics[mP99] || got.Honest != want.Honest {
		t.Fatalf("Load after Store = %+v, %v; want %+v", got, err, want)
	}
	// A result of another report format is not reused.
	path := filepath.Join(c.Dir, "reduced", V01, "seed-03.json")
	data, err := os.ReadFile(path) // #nosec G304 -- a test file.
	if err != nil {
		t.Fatal(err)
	}
	old := strings.Replace(string(data), ReportFormat, "routing-report/0", 1)
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil { // #nosec G703 -- a file the test wrote.
		t.Fatal(err)
	}
	if res, err := c.Load(r); res != nil || err != nil {
		t.Errorf("Load of an old format = %v, %v; want nil, nil", res, err)
	}
	if res, err := (Cache{}).Load(r); res != nil || err != nil || (Cache{}).Store(r, want) != nil {
		t.Errorf("an empty cache returned %v, %v", res, err)
	}
}

func TestSelect(t *testing.T) {
	names := func(scs []*Scenario) []string {
		out := make([]string, len(scs))
		for i, s := range scs {
			out[i] = s.Name
		}
		return out
	}
	for sel, want := range map[string][]string{
		"A":         {"A-coldboot", "A-covertflash", "A-eclipse"},
		"B":         {"B-f10", "B-f30", "B-f50"},
		"C":         {"C-bootkill", "C-flood", "C-junk", "C-offline", "C-preempt"},
		"T":         {"T-burst-regular", "T-burst-static", "T-lowrate", "T-regular", "T-static"},
		"reduced":   {"reduced"},
		"C-offline": {"C-offline"},
	} {
		got, err := Select(sel)
		if err != nil || !slices.Equal(names(got), want) {
			t.Errorf("Select(%q) = %v, %v; want %v", sel, names(got), err, want)
		}
	}
	all, err := Select("all")
	if err != nil || len(all) != 16 || slices.Contains(names(all), "reduced") {
		t.Errorf("Select(all) = %v, %v; want the 16 scenarios of A, B, C and T", names(all), err)
	}
	if _, err := Select("Z"); err == nil || !strings.Contains(err.Error(), "A-eclipse") {
		t.Errorf("Select(Z) = %v, want an error listing the scenarios", err)
	}
}

func TestRuns(t *testing.T) {
	scs, err := Select("A")
	if err != nil {
		t.Fatal(err)
	}
	runs, err := Runs(scs, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 3*3*20 || runs[0].Name() != "A-coldboot/plain/seed-01" {
		t.Errorf("%d runs, the first %s; want 180 from A-coldboot/plain/seed-01", len(runs), runs[0].Name())
	}
	if _, err := Runs([]*Scenario{{Name: "x", Variants: []string{"v9"}}}, 1); err == nil {
		t.Error("a scenario with an unknown variant has runs")
	}
}

func TestSeedList(t *testing.T) {
	byVariant := map[string][]*Result{}
	for _, s := range []int64{3, 1, 2, 4} {
		byVariant[V01] = append(byVariant[V01], &Result{Seed: s})
	}
	if got := seedList(byVariant); got != "1–4" {
		t.Errorf("seedList(1–4) = %q", got)
	}
	byVariant[Plain] = []*Result{{Seed: 7}}
	if got := seedList(byVariant); got != "1, 2, 3, 4, 7" {
		t.Errorf("seedList(1–4, 7) = %q", got)
	}
}

func TestRequiredVersion(t *testing.T) {
	gomod := []byte("module x\n\nrequire github.com/a/b v1.2.3\n\nrequire (\n\tgithub.com/c/d v0.4.0 // indirect\n)\n")
	for path, want := range map[string]string{"github.com/a/b": "v1.2.3", "github.com/c/d": "v0.4.0", "github.com/e/f": "unknown"} {
		if got := requiredVersion(gomod, path); got != want {
			t.Errorf("requiredVersion(%s) = %q, want %q", path, got, want)
		}
	}
}

// TestScenariosBuild: every scenario small enough for a unit test builds,
// with its events before the end and its variants known.
func TestScenariosBuild(t *testing.T) {
	all := variants()
	for name, sc := range Scenarios() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, v := range sc.Variants {
				if _, ok := all[v]; !ok {
					t.Errorf("unknown variant %s", v)
				}
			}
			if sc.Honest > 1000 {
				t.Skip("too large for a unit test")
			}
			w := newWorld(sc, all[sc.Variants[0]], 1)
			if err := sc.build(w); err != nil {
				t.Fatal(err)
			}
			if len(w.honest()) != sc.Honest || w.end <= 0 || len(w.events) == 0 {
				t.Errorf("%d honest nodes, end %v, %d events; want %d honest nodes", len(w.honest()), w.end, len(w.events), sc.Honest)
			}
			for _, e := range w.events {
				if e.at >= w.end-drain {
					t.Errorf("an event at %v, after publishing stopped at %v", e.at, w.end-drain)
					break
				}
			}
		})
	}
}
