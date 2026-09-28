//go:build soak

package e2e

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"math"
	"net/netip"
	"os"
	"runtime"
	"runtime/pprof"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/daemon"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// The load of the soak test; `make soak` runs the defaults.
var (
	soakDuration = flag.Duration("soak.duration", 30*time.Minute, "how long TestSoak sends events")
	soakRate     = flag.Float64("soak.rate", 50, "events per second TestSoak sends, spread over the nodes")
	soakHeap     = flag.String("soak.heapprofile", "", "file TestSoak writes a heap profile to at the end of the load")
)

// Bounds of the soak test's acceptance criteria.
const (
	// soakPropagationP99 bounds the 99th percentile of the time from a
	// report on one node until its verdict is stored on another.
	soakPropagationP99 = 2 * time.Second
	// soakMemoryGrowth bounds the growth of the live heap after warm-up.
	soakMemoryGrowth = 0.20
	// soakGoroutineGrowth bounds the growth of the goroutines after
	// warm-up; goleak in TestMain checks that none outlive the nodes.
	soakGoroutineGrowth = 0.05
	// soakSampleInterval is how often memory and goroutines are sampled.
	soakSampleInterval = 30 * time.Second
	// soakReadings is how many readings of the heap make one sample,
	// soakReadingGap apart.
	soakReadings   = 3
	soakReadingGap = 2 * time.Second
	// soakDrain is how long events may take to arrive after the last one
	// was sent.
	soakDrain = 10 * time.Second
	// soakReporters bounds the reports in flight at once.
	soakReporters = 32
	// soakTTL is the lifetime of the soak test's verdicts. Once the first
	// expire, the store sweep removes as many as arrive, so the node holds
	// a steady working set (about rate × (soakTTL + the sweep interval))
	// and any memory it does not give back shows as growth. How far a
	// flood of long-lived verdicts can grow a node is store.max_indicators'
	// job, tested in internal/store.
	soakTTL = 2 * time.Minute
	// soakBanEvery makes every n-th report a ban, which the reporting node
	// blocks on its own (local autoblock); the others are watches.
	soakBanEvery = 10
)

// soakFirstIP is the first attacking address of the soak test; the i-th
// report is on soakFirstIP + i. 11.0.0.0/8 is public, so every address
// takes the full path through validation and decision.
var soakFirstIP = netip.MustParseAddr("11.0.0.0")

// rateLimited counts the events a node dropped at a rate limit.
const rateLimited = `obie_events_received_total{outcome="rate_limited"}`

// TestSoak runs three nodes that trust each other under a steady load of
// unique reports with a short lifetime (soakTTL), spread evenly over the
// nodes, and checks that every
// event reaches every other node with a 99th percentile below 2 s, that
// the live heap without Badger's bounded caches does not grow by more than
// 20 % after warm-up (the first sixth of the run; see sampler.check), and — through TestMain — that no goroutine outlives
// the nodes. Run it with `make soak`.
func TestSoak(t *testing.T) {
	duration, rate := *soakDuration, *soakRate
	arr := newArrivals(int(rate*duration.Seconds()) * 2)
	var stores storeSet
	c := newCluster(t, clusterOptions{
		backend: config.BackendDryRun,
		// Each node publishes rate/3 events per second, above the default
		// of 10 per publisher.
		rateLimit: config.RateLimit{
			Publisher: config.TokenBucket{EventsPerSecond: rate, Burst: int(5 * rate)},
			Peer:      config.TokenBucket{EventsPerSecond: 2 * rate, Burst: int(10 * rate)},
		},
		logLevel: "info",
		hooks: func(name string) daemon.Testing {
			return daemon.Testing{Store: func(db *store.DB) {
				stores.add(db)
				db.Subscribe(func(ch store.Change) { arr.arrived(name, ch) })
			}}
		},
	}, "A", "B", "C")
	t.Logf("soak: %d nodes, %g events/s for %s", len(c.nodes), rate, duration)

	start := time.Now()
	mem := newSampler(start, stores.cacheBytes)
	ctx, stopSampling := context.WithCancel(context.Background())
	sampled := make(chan struct{})
	go func() { defer close(sampled); mem.run(ctx, soakSampleInterval) }()

	sent, failed := sendLoad(c, arr, rate, start.Add(duration))
	arr.drain(soakDrain)
	stopSampling()
	<-sampled
	mem.sample()
	if *soakHeap != "" {
		writeHeapProfile(t, *soakHeap)
	}

	for _, n := range c.nodes {
		if v, err := n.metricNow(rateLimited); err == nil && v > 0 {
			t.Logf("soak: %s = %v", rateLimited, v)
		}
	}
	lat, missing := arr.result()
	t.Logf("soak: sent %d reports in %s, %d failed; %d deliveries, %d missing",
		sent, time.Since(start).Round(time.Second), len(failed), len(lat), missing)
	if len(failed) > 0 {
		t.Errorf("%d reports failed, the first: %v", len(failed), failed[0])
	}
	if missing > 0 {
		t.Errorf("%d events did not reach every other node within %s of the last report", missing, soakDrain)
	}
	if len(lat) > 0 {
		p50, p99, p100 := percentile(lat, 0.50), percentile(lat, 0.99), percentile(lat, 1)
		t.Logf("soak: propagation p50 %s, p99 %s, max %s (bound p99 %s)",
			p50.Round(time.Millisecond), p99.Round(time.Millisecond), p100.Round(time.Millisecond), soakPropagationP99)
		if p99 >= soakPropagationP99 {
			t.Errorf("propagation p99 = %s, want below %s", p99, soakPropagationP99)
		}
	}
	mem.check(t, duration/6, duration/6)
}

// sendLoad reports a unique address every 1/rate seconds until end,
// round-robin on the nodes of c, and returns how many reports it sent
// and the errors of those that failed.
func sendLoad(c *cluster, arr *arrivals, rate float64, end time.Time) (sent int, failed []error) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, soakReporters)
	ticker := time.NewTicker(time.Duration(float64(time.Second) / rate))
	defer ticker.Stop()
	for ; time.Now().Before(end); sent++ {
		<-ticker.C
		n := c.nodes[sent%len(c.nodes)]
		ip := soakIP(sent)
		confidence, action := 0.5, obieproto.ActionWatch
		if sent%soakBanEvery == 0 {
			confidence, action = 0.95, obieproto.ActionBan
		}
		key := obieproto.Indicator{Kind: obieproto.KindIPv4, Value: ip}.Key()
		receivers := make(map[string]bool, len(c.nodes)-1)
		for _, o := range c.others(n) {
			receivers[o.name] = true
		}
		arr.expect(key, receivers)
		slots <- struct{}{}
		wg.Go(func() {
			defer func() { <-slots }()
			err := call(func(ctx context.Context) error {
				req := reportRequest(ip, confidence, action)
				req.TTL = admin.TTL(soakTTL)
				_, err := n.client.Report(ctx, req)
				return err
			})
			if err != nil {
				arr.forget(key)
				mu.Lock()
				failed = append(failed, fmt.Errorf("node %s: report %s: %w", n.name, ip, err))
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return sent, failed
}

// soakIP returns the i-th attacking address of the soak test.
func soakIP(i int) string {
	b := soakFirstIP.As4()
	binary.BigEndian.PutUint32(b[:], binary.BigEndian.Uint32(b[:])+uint32(i)) // #nosec G115 -- i is far below 2^24.
	return netip.AddrFrom4(b).String()
}

// arrivals times the way of every event from its report to the store of
// each other node.
type arrivals struct {
	mu sync.Mutex
	// pending are the events still missing on some node, by indicator key.
	pending   map[string]*pendingEvent
	latencies []time.Duration
}

// pendingEvent is a reported event and the nodes it has not reached yet.
type pendingEvent struct {
	sent      time.Time
	receivers map[string]bool
}

// newArrivals returns arrivals with room for deliveries latencies, so that
// recording them does not grow the heap under measurement.
func newArrivals(deliveries int) *arrivals {
	return &arrivals{pending: map[string]*pendingEvent{}, latencies: make([]time.Duration, 0, deliveries)}
}

// expect records that the event on key is sent now, to receivers.
func (a *arrivals) expect(key string, receivers map[string]bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pending[key] = &pendingEvent{sent: time.Now(), receivers: receivers}
}

// forget drops the event on key, whose report failed.
func (a *arrivals) forget(key string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.pending, key)
}

// arrived records a change of node's store.
func (a *arrivals) arrived(node string, ch store.Change) {
	if ch.Reason != store.ReasonVerdict {
		return
	}
	now := time.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	p := a.pending[ch.Key]
	if p == nil || !p.receivers[node] {
		return
	}
	delete(p.receivers, node)
	a.latencies = append(a.latencies, now.Sub(p.sent))
	if len(p.receivers) == 0 {
		delete(a.pending, ch.Key)
	}
}

// drain waits until every event arrived everywhere, at most for bound.
func (a *arrivals) drain(bound time.Duration) {
	_ = poll(bound, func(context.Context) error {
		a.mu.Lock()
		defer a.mu.Unlock()
		if len(a.pending) > 0 {
			return fmt.Errorf("%d events pending", len(a.pending))
		}
		return nil
	})
}

// result returns the latencies of all deliveries and how many are missing.
func (a *arrivals) result() (latencies []time.Duration, missing int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, p := range a.pending {
		missing += len(p.receivers)
	}
	return slices.Clone(a.latencies), missing
}

// percentile returns the q-quantile (0 < q ≤ 1) of d, sorting it.
func percentile(d []time.Duration, q float64) time.Duration {
	slices.Sort(d)
	i := int(q*float64(len(d))+0.5) - 1
	return d[min(max(i, 0), len(d)-1)]
}

// storeSet are the stores of the nodes.
type storeSet struct {
	mu  sync.Mutex
	dbs []*store.DB
}

func (s *storeSet) add(db *store.DB) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dbs = append(s.dbs, db)
}

// cacheBytes returns the bytes held in the Badger caches of all stores.
func (s *storeSet) cacheBytes() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for _, db := range s.dbs {
		n += db.CacheBytes()
	}
	return uint64(max(n, 0))
}

// memSample is the memory and the goroutines at one point of the run.
type memSample struct {
	at time.Duration
	// heap is the live heap without Badger's caches: the smallest of
	// soakReadings readings. peak is the largest live heap read,
	// caches included.
	heap, peak uint64
	// cache is what Badger's caches held at the smallest reading.
	cache      uint64
	goroutines int
}

// sampler records memSamples of the whole process: the three nodes and
// the test.
type sampler struct {
	start time.Time
	// cache returns the bytes held in Badger's caches.
	cache   func() uint64
	mu      sync.Mutex
	samples []memSample
}

func newSampler(start time.Time, cache func() uint64) *sampler {
	return &sampler{start: start, cache: cache}
}

// run samples every interval until ctx is canceled.
func (s *sampler) run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sample()
		}
	}
}

// sample reads the live heap after a garbage collection soakReadings
// times, soakReadingGap apart, and records the smallest reading less what
// Badger's block and index caches held then. The caches fill up to their
// configured sizes and empty as compaction deletes tables, so with them
// the heap is a sawtooth; a Badger memtable flush holds the table it
// builds (about 60 MiB per node) for a moment, which the smallest reading
// leaves out. What remains grows only if something leaks.
func (s *sampler) sample() {
	m := memSample{at: time.Since(s.start), heap: math.MaxUint64}
	for i := range soakReadings {
		if i > 0 {
			time.Sleep(soakReadingGap)
		}
		runtime.GC()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		cache := s.cache()
		if heap := ms.HeapAlloc - min(cache, ms.HeapAlloc); heap < m.heap {
			m.heap, m.cache = heap, cache
		}
		m.peak = max(m.peak, ms.HeapAlloc)
	}
	m.goroutines = runtime.NumGoroutine()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.samples = append(s.samples, m)
}

// check logs the samples and fails t if, from the window right after
// warmUp to the last window, the median live heap without caches grew by
// more than soakMemoryGrowth, or the largest number of goroutines by more
// than soakGoroutineGrowth.
func (s *sampler) check(t *testing.T, warmUp, window time.Duration) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	end := s.samples[len(s.samples)-1].at
	var first, last []memSample
	var peak memSample
	for _, m := range s.samples {
		t.Logf("soak: %8s  heap %6.1f MiB + caches %5.1f MiB (peak %6.1f MiB)  %4d goroutines",
			m.at.Round(time.Second), mib(m.heap), mib(m.cache), mib(m.peak), m.goroutines)
		if m.at >= warmUp && m.at < warmUp+window {
			first = append(first, m)
		}
		if m.at > end-window && m.at >= warmUp+window {
			last = append(last, m)
		}
		if m.peak > peak.peak {
			peak = m
		}
	}
	if len(first) == 0 || len(last) == 0 {
		t.Errorf("the run is too short for a warm-up of %s and two windows of %s", warmUp, window)
		return
	}
	base, final := medianHeap(first), medianHeap(last)
	growth := float64(final)/float64(base) - 1
	t.Logf("soak: heap without caches, median %.1f MiB after warm-up (%s–%s), %.1f MiB in the last %s: %+.1f %% (bound %.0f %%); peak with caches %.1f MiB at %s",
		mib(base), warmUp, warmUp+window, mib(final), window, 100*growth, 100*soakMemoryGrowth, mib(peak.peak), peak.at.Round(time.Second))
	if growth > soakMemoryGrowth {
		t.Errorf("live heap grew by %.1f %% after warm-up, want at most %.0f %%", 100*growth, 100*soakMemoryGrowth)
	}
	g0, g1 := maxGoroutines(first), maxGoroutines(last)
	if float64(g1) > float64(g0)*(1+soakGoroutineGrowth) {
		t.Errorf("goroutines grew from %d after warm-up to %d at the end", g0, g1)
	}
}

// medianHeap returns the median heap of samples.
func medianHeap(samples []memSample) uint64 {
	heaps := make([]uint64, len(samples))
	for i, m := range samples {
		heaps[i] = m.heap
	}
	slices.Sort(heaps)
	return heaps[len(heaps)/2]
}

// maxGoroutines returns the largest number of goroutines in samples.
func maxGoroutines(samples []memSample) int {
	n := 0
	for _, m := range samples {
		n = max(n, m.goroutines)
	}
	return n
}

// mib converts bytes to MiB.
func mib(b uint64) float64 { return float64(b) / (1 << 20) }

// writeHeapProfile writes a heap profile of the running nodes to path,
// for `go tool pprof`.
func writeHeapProfile(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path) // #nosec G304 -- a path the operator of the test chose.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := pprof.WriteHeapProfile(f); err != nil {
		t.Fatal(err)
	}
	t.Logf("soak: heap profile written to %s", path)
}

// metricNow reads a sample of n's /metrics with a request timeout.
func (n *node) metricNow(sample string) (float64, error) {
	var v float64
	err := call(func(ctx context.Context) error {
		var err error
		v, err = n.metric(ctx, sample)
		return err
	})
	return v, err
}
