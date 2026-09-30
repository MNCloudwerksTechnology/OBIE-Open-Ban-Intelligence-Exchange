package simtrust

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime"
	"slices"
	"sync"
	"time"
)

// ExecOptions say how to execute a scenario.
type ExecOptions struct {
	// Seeds is the number of seeds, 1 to Seeds.
	Seeds int
	// Trace replaces the synthetic world of every seed, if set.
	Trace *Trace
	// Workers is the number of runs at once; GOMAXPROCS if 0.
	Workers int
	// Progress, if set, is called after every run.
	Progress func(done, total int, spec RunSpec, took time.Duration)
	// Cache, if set, is a directory that keeps the metrics of every run,
	// so that an interrupted scenario resumes where it stopped.
	Cache string
}

// Report is the outcome of a scenario over its seeds.
type Report struct {
	Scenario Scenario
	Seeds    int
	// Trace says what was replayed.
	Trace string
	// Hours is the length of the runs.
	Hours      int
	Aggregates []Aggregate
	// Publishers holds the feed metrics of every publisher key of every
	// run, by configuration and seed.
	Publishers [][][]PublisherFeed
	// Runs counts the runs, Cached those taken from the cache; Wall is how
	// long the others took together.
	Runs, Cached int
	Wall         time.Duration
}

// Aggregate is a configuration's metrics over the seeds.
type Aggregate struct {
	Config
	// Hours holds the estimate of every metric in both windows of hour h.
	Hours [][numWindows][numMetrics]Estimate
	// Corroboration holds, per support bucket and local or not, the
	// attackers per seed and the share of them banned; Banned and
	// Attackers are the totals over the seeds.
	Corroboration [supportBuckets][2]CorroborationEstimate
	// FalseBansByClass estimates the false bans per run by the class of
	// their victim.
	FalseBansByClass map[Class]Estimate
	// Feeds are the estimates of the feed metrics of each role.
	Feeds map[Role]FeedEstimate
}

// CorroborationEstimate is one group of attackers of Corroboration.
type CorroborationEstimate struct {
	Attackers, Share      Estimate
	TotalAttackers, Total int
}

// FeedEstimate are the estimates of a role's feed metrics.
type FeedEstimate struct {
	Volume, Exclusive, LatencyMinutes, Bound, Accuracy Estimate
}

// End returns the estimates of the cumulative window at the end.
func (a *Aggregate) End() [numMetrics]Estimate {
	return a.Hours[len(a.Hours)-1][WindowCumulative]
}

// Execute runs every configuration of sc for every seed, the runs spread
// over the workers, and aggregates their metrics.
func Execute(ctx context.Context, sc Scenario, opts ExecOptions) (*Report, error) {
	if opts.Seeds < 1 {
		return nil, fmt.Errorf("%d seeds, want at least 1", opts.Seeds)
	}
	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	traces := make([]*Trace, opts.Seeds)
	for i := range traces {
		if opts.Trace != nil {
			traces[i] = opts.Trace.ShiftedTo(worldStart)
		} else {
			traces[i] = GenerateWorld(sc.World, uint64(i+1), worldStart) // #nosec G115 -- i >= 0.
		}
	}
	rep := &Report{Scenario: sc, Seeds: opts.Seeds, Trace: "a synthetic world per seed (ADR 0034)", Hours: traces[0].Hours}
	var replayed *Trace
	if opts.Trace != nil {
		rep.Trace, replayed = opts.Trace.Source, traces[0]
	}
	cache, err := newRunCache(opts.Cache, sc, replayed)
	if err != nil {
		return nil, err
	}
	type job struct{ config, seed int }
	jobs := make(chan job)
	metrics := make([][]*Metrics, len(sc.Configs))
	for i := range metrics {
		metrics[i] = make([]*Metrics, opts.Seeds)
	}
	began := time.Now()
	var mu sync.Mutex
	var errs []error
	done := 0
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for j := range jobs {
				c := sc.Configs[j.config]
				spec := RunSpec{Model: c.Model, Fraction: c.Fraction, Profile: c.Profile, Seed: uint64(j.seed + 1)} // #nosec G115 -- seed >= 0.
				started := time.Now()
				m, cached := cache.load(spec)
				var err error
				if !cached {
					var res *Result
					if res, err = Run(ctx, traces[j.seed], spec, sc.Models); err == nil {
						m = Measure(res)
						err = cache.store(m)
					}
				}
				mu.Lock()
				if err != nil {
					errs = append(errs, err)
				}
				metrics[j.config][j.seed] = m
				if cached {
					rep.Cached++
				}
				done++
				if opts.Progress != nil {
					opts.Progress(done, len(sc.Configs)*opts.Seeds, spec, time.Since(started))
				}
				mu.Unlock()
			}
		})
	}
feed:
	for seed := range opts.Seeds {
		for config := range sc.Configs {
			select {
			case jobs <- job{config, seed}:
			case <-ctx.Done():
				break feed
			}
		}
	}
	close(jobs)
	wg.Wait()
	if err := errors.Join(append(errs, ctx.Err())...); err != nil {
		return nil, err
	}
	rep.Runs, rep.Wall = len(sc.Configs)*opts.Seeds, time.Since(began)
	for i, c := range sc.Configs {
		rep.Aggregates = append(rep.Aggregates, aggregate(c, metrics[i], rep.Hours))
		perSeed := make([][]PublisherFeed, opts.Seeds)
		for seed, m := range metrics[i] {
			perSeed[seed] = m.Publishers
		}
		rep.Publishers = append(rep.Publishers, perSeed)
	}
	return rep, nil
}

// aggregate estimates the metrics of config c over its runs.
func aggregate(c Config, runs []*Metrics, hours int) Aggregate {
	a := Aggregate{Config: c, Hours: make([][numWindows][numMetrics]Estimate, hours), FalseBansByClass: map[Class]Estimate{},
		Feeds: map[Role]FeedEstimate{}}
	xs := make([]float64, len(runs))
	for h := range hours {
		for w := range numWindows {
			for i := range numMetrics {
				for s, m := range runs {
					xs[s] = m.Hours[h][w][i]
				}
				a.Hours[h][w][i] = estimate(xs)
			}
		}
	}
	for b := range supportBuckets {
		for l := range 2 {
			attackers, shares := make([]float64, len(runs)), make([]float64, len(runs))
			ce := &a.Corroboration[b][l]
			for s, m := range runs {
				g := m.Corroboration[b][l]
				attackers[s], shares[s] = float64(g.Attackers), ratio(g.Banned, g.Attackers)
				ce.TotalAttackers += g.Attackers
				ce.Total += g.Banned
			}
			ce.Attackers, ce.Share = estimate(attackers), estimate(shares)
		}
	}
	for _, class := range benignClasses {
		for s, m := range runs {
			xs[s] = float64(m.FalseBansByClass[class])
		}
		a.FalseBansByClass[class] = estimate(xs)
	}
	var roles []Role
	for _, m := range runs {
		for r := range m.Feeds {
			if !slices.Contains(roles, r) {
				roles = append(roles, r)
			}
		}
	}
	for _, r := range roles {
		field := func(get func(Feed) float64) Estimate {
			for s, m := range runs {
				xs[s] = math.NaN()
				if f, ok := m.Feeds[r]; ok {
					xs[s] = get(f)
				}
			}
			return estimate(xs)
		}
		a.Feeds[r] = FeedEstimate{
			Volume:         field(func(f Feed) float64 { return f.Volume }),
			Exclusive:      field(func(f Feed) float64 { return f.Exclusive }),
			LatencyMinutes: field(func(f Feed) float64 { return f.LatencyMinutes }),
			Bound:          field(func(f Feed) float64 { return f.Bound }),
			Accuracy:       field(func(f Feed) float64 { return f.Accuracy }),
		}
	}
	return a
}
