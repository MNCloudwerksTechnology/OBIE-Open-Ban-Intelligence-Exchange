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
