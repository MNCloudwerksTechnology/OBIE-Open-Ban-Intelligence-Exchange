//go:build sim

package sim

import (
	"flag"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

var (
	simScenario = flag.String("sim.scenario", "reduced", "scenario, group (A, B, C, T, reduced) or all")
	simSeeds    = flag.Int("sim.seeds", 20, "seeds per scenario and variant")
	simOut      = flag.String("sim.out", "", "directory of the reports; empty writes none")
	simCache    = flag.String("sim.cache", "", "directory of the per-seed results; empty caches nothing")
	simVersion  = flag.String("sim.version", "dev", "OBIE version recorded with each result")
	simBudget   = flag.Duration("sim.budget", 0, "start no run after this much real time; 0 is no limit. The cache keeps the finished runs; run again to continue")
	simTrace    = flag.String("sim.trace", "", "directory to write each run's per-event trace to (JSON lines); empty writes none")
)

// TestRouting runs the selected scenarios, every variant with every seed,
// each run in a bubble of its own and up to -test.parallel at once. Runs
// in the cache are not run again. Once every run of a scenario has a
// result, it writes the scenario's report and fails if a check fails
// (make sim-routing).
func TestRouting(t *testing.T) {
	began := time.Now()
	scenarios, err := Select(*simScenario)
	if err != nil {
		t.Fatal(err)
	}
	runs, err := Runs(scenarios, *simSeeds)
	if err != nil {
		t.Fatal(err)
	}
	cache := Cache{Dir: *simCache}
	results := newCollector()
	var missing []Run
	others := map[string]int{}
	for _, r := range runs {
		res, err := cache.Load(r)
		switch {
		case err != nil:
			t.Fatal(err)
		case res != nil:
			if res.Version != *simVersion {
				others[res.Version]++
			}
			results.add(res)
		default:
			missing = append(missing, r)
		}
	}
	t.Logf("%d runs, %d cached, %d to run", len(runs), len(runs)-len(missing), len(missing))
	if len(others) > 0 {
		t.Logf("cached results of other versions than %s (runs by version): %v; empty the cache if the code they ran has changed", *simVersion, others)
	}
	t.Run("runs", func(t *testing.T) {
		for _, r := range missing {
			t.Run(r.Name(), func(t *testing.T) {
				t.Parallel()
				if *simBudget > 0 && time.Since(began) > *simBudget {
					t.Skip("the budget is spent; run again to continue")
				}
				res := execute(t, r)
				if res == nil {
					return
				}
				if err := cache.Store(r, res); err != nil {
					t.Fatal(err)
				}
				results.add(res)
			})
		}
	})
	for _, sc := range scenarios {
		byVariant := results.of(sc.Name)
		if n, want := count(byVariant), len(sc.Variants)*(*simSeeds); n < want {
			t.Logf("%s: %d of %d runs done; no report yet", sc.Name, n, want)
			continue
		}
		checks := Evaluate(sc, byVariant)
		if *simOut != "" {
			if err := WriteReport(*simOut, Header{Generated: time.Now()}, sc, byVariant, checks); err != nil {
				t.Fatal(err)
			}
		}
		for _, c := range checks {
			t.Logf("%s: %s: pass=%v (%s)", sc.Name, c.Name, c.Pass, c.Detail)
			if !c.Pass {
				t.Errorf("%s: check failed: %s: %s", sc.Name, c.Name, c.Detail)
			}
		}
	}
}

// execute runs r in a bubble of its own and returns its result with the
// version and the real time it took.
func execute(t *testing.T, r Run) *Result {
	began := time.Now()
	var res *Result
	synctest.Test(t, func(t *testing.T) {
		w := newWorld(r.Scenario, r.Variant, r.Seed)
		out, err := w.run()
		if err != nil {
			t.Error(err)
			return
		}
		if *simTrace != "" {
			if err := traceFile(filepath.Join(*simTrace, fmt.Sprintf("%s-%s-seed-%02d.jsonl", r.Scenario.Name, r.Variant.Name, r.Seed)), w); err != nil {
				t.Error(err)
			}
		}
		res = out
	})
	if res != nil {
		res.Version = *simVersion
		res.WallSeconds = time.Since(began).Seconds()
		t.Logf("%s: delivery %.4f, p99 %.0f ms, %.0f s", r.Name(), res.Metrics[mDelivery], res.Metrics[mP99], res.WallSeconds)
	}
	return res
}

// collector gathers results by scenario and variant from parallel runs.
type collector struct {
	mu      sync.Mutex
	results map[string]map[string][]*Result
}

func newCollector() *collector { return &collector{results: map[string]map[string][]*Result{}} }

func (c *collector) add(res *Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.results[res.Scenario] == nil {
		c.results[res.Scenario] = map[string][]*Result{}
	}
	c.results[res.Scenario][res.Variant] = append(c.results[res.Scenario][res.Variant], res)
}

// of returns the results of the scenario by variant, each variant's in
// seed order.
func (c *collector) of(scenario string) map[string][]*Result {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := map[string][]*Result{}
	for v, rs := range c.results[scenario] {
		out[v] = sortedBySeed(rs)
	}
	return out
}

func count(byVariant map[string][]*Result) int {
	n := 0
	for _, rs := range byVariant {
		n += len(rs)
	}
	return n
}
