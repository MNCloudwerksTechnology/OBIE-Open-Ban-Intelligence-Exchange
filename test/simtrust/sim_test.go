//go:build simtrust

package simtrust

import (
	"context"
	"flag"
	"path/filepath"
	"testing"
	"time"
)

var (
	flagScenario = flag.String("simtrust.scenario", "reduced", "scenario to run (ADR 0034)")
	flagSeeds    = flag.Int("simtrust.seeds", 20, "number of seeds")
	flagTrace    = flag.String("simtrust.trace", "", "trace file to replay instead of the synthetic world")
	flagOut      = flag.String("simtrust.out", "", "directory to write the report to; a temporary one if empty")
	flagVersion  = flag.String("simtrust.version", "dev", "OBIE version the report names")
	flagWorkers  = flag.Int("simtrust.workers", 0, "runs at once; GOMAXPROCS if 0")
)

// TestSimTrust runs a scenario of the trust simulation and writes its
// report: `make sim-trust SCENARIO=…`. Without a trace, it checks what
// v0.1's static weights guarantee.
func TestSimTrust(t *testing.T) {
	sc, err := ScenarioNamed(*flagScenario)
	if err != nil {
		t.Fatal(err)
	}
	var tr *Trace
	if *flagTrace != "" {
		if tr, err = readFileWith(*flagTrace, ReadTrace); err != nil {
			t.Fatalf("trace %s: %v", *flagTrace, err)
		}
	}
	out := *flagOut
	if out == "" {
		out = t.TempDir()
	}
	shown := -1
	rep, err := Execute(context.Background(), sc, ExecOptions{
		Seeds:   *flagSeeds,
		Trace:   tr,
		Workers: *flagWorkers,
		Progress: func(done, total int, spec RunSpec, took time.Duration) {
			if step := done * 50 / total; step != shown {
				shown = step
				t.Logf("%d of %d runs; last: %s in %s", done, total, spec, took.Round(time.Millisecond))
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteReport(out, rep, ReportInfo{Version: *flagVersion, Generated: time.Now()}); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d runs in %s; report: %s", rep.Runs, rep.Wall.Round(time.Second), filepath.Join(out, reportFile))
	if tr == nil {
		for _, problem := range CheckV01(rep) {
			t.Error(problem)
		}
	}
}
