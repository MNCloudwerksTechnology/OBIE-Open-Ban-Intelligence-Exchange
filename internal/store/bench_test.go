package store

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// BenchmarkInsertList100k inserts 100k verdicts on distinct indicators into
// an on-disk store and lists them all, 1000 per page. It reports the Go heap
// the open store holds afterwards, without the benchmark's own events
// (store-heap-MiB), and the Go memory obtained from the OS by the whole
// process (sys-MiB), an upper bound for the resident size.
func BenchmarkInsertList100k(b *testing.B) {
	const n = 100_000
	clk := newClock()
	events := make([]*obieproto.Event, n)
	for i := range events {
		events[i] = verdict(pubA, ipv4(ipv4Value(i)), clk.Now(), 24*time.Hour)
	}
	heapInuse := func() uint64 {
		runtime.GC()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		return ms.HeapInuse
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		baseline := heapInuse()
		b.StartTimer()
		db := New(filepath.Join(b.TempDir(), "db"), discardLogger(), Options{Now: clk.Now})
		if err := db.Start(context.Background()); err != nil {
			b.Fatal(err)
		}
		for _, ev := range events {
			if ok, err := db.Put(ev); err != nil || !ok {
				b.Fatalf("Put = %v, %v", ok, err)
			}
		}
		listed := 0
		page := Page{Limit: MaxPageLimit}
		for {
			res, err := db.ListIndicators(clk.Now(), Filter{}, page)
			if err != nil {
				b.Fatal(err)
			}
			listed += len(res.Items)
			if res.Next == "" {
				break
			}
			page.After = res.Next
		}
		if listed != n {
			b.Fatalf("listed %d indicators, want %d", listed, n)
		}

		b.StopTimer()
		inuse := heapInuse()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		b.ReportMetric(float64(inuse-min(inuse, baseline))/(1<<20), "store-heap-MiB")
		b.ReportMetric(float64(ms.Sys)/(1<<20), "sys-MiB")
		if err := db.Stop(context.Background()); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}

// BenchmarkPublisherVerdicts100k lists the 10 verdicts of one publisher in
// an on-disk store that holds 100k verdicts of another: the worst case of
// PublisherVerdicts, a walk over every verdict key (ADR 0021).
func BenchmarkPublisherVerdicts100k(b *testing.B) {
	const n = 100_000
	clk := newClock()
	db := startDB(b, New(filepath.Join(b.TempDir(), "db"), discardLogger(), Options{Now: clk.Now}))
	for i := range n {
		if ok, err := db.Put(verdict(pubA, ipv4(ipv4Value(i)), clk.Now(), 24*time.Hour)); err != nil || !ok {
			b.Fatalf("Put = %v, %v", ok, err)
		}
	}
	for i := range 10 {
		if ok, err := db.Put(verdict(pubB, ipv4(ipv4Value(i*9973)), clk.Now(), 24*time.Hour)); err != nil || !ok {
			b.Fatalf("Put = %v, %v", ok, err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		res, err := db.PublisherVerdicts(pubB, clk.Now(), Page{Limit: 50})
		if err != nil || len(res.Verdicts) != 10 {
			b.Fatalf("PublisherVerdicts = %d verdicts, %v", len(res.Verdicts), err)
		}
	}
}

// BenchmarkEnded100k reads an on-disk store that keeps 100k expired
// verdicts, about what 700k indicators with the default 7-day lifetime
// leave in a day (ADR 0023): the first page of a rare category, whose
// walk passes every ended verdict's key, and the counts by publisher.
func BenchmarkEnded100k(b *testing.B) {
	const n = 100_000
	clk := newClock()
	db := startDB(b, New(filepath.Join(b.TempDir(), "db"), discardLogger(), Options{Now: clk.Now}))
	for i := range n {
		ev := verdict(pubA, ipv4(ipv4Value(i)), clk.Now(), time.Hour)
		if i%10_000 == 0 {
			ev.Evidence.Reason = "port_scan"
		}
		if ok, err := db.Put(ev); err != nil || !ok {
			b.Fatalf("Put = %v, %v", ok, err)
		}
	}
	clk.Advance(time.Hour)
	if err := db.Sweep(clk.Now()); err != nil {
		b.Fatal(err)
	}
	b.Run("page", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			res, err := db.EndedVerdicts(EndedFilter{State: EndedExpired, Category: "port_scan/ssh"}, Page{Limit: 50})
			if err != nil || len(res.Verdicts) != 10 || res.Total != 10 {
				b.Fatalf("EndedVerdicts = %d verdicts of %d, %v", len(res.Verdicts), res.Total, err)
			}
		}
	})
	b.Run("counts", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			counts, err := db.EndedCounts()
			if err != nil || counts[pubA].Expired != n {
				b.Fatalf("EndedCounts = %v, %v", counts, err)
			}
		}
	})
}
