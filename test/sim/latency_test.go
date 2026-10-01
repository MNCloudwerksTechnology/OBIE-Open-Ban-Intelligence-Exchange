package sim

import (
	"math"
	"testing"
	"time"
)

func TestRegionShares(t *testing.T) {
	var total float64
	for _, r := range regions {
		total += r.share
	}
	if math.Abs(total-1) > 1e-9 {
		t.Errorf("the region shares sum to %v, want 1", total)
	}
	const n = 200000
	count := make([]int, len(regions))
	for _, r := range placeNodes(testRNG(1), n) {
		count[r]++
	}
	for i, r := range regions {
		if got := float64(count[i]) / n; math.Abs(got-r.share) > 0.005 {
			t.Errorf("%s holds %.3f of the nodes, want %.2f", r.name, got, r.share)
		}
	}
}

func TestRTTMatrixSymmetric(t *testing.T) {
	if len(rttMillis) != len(regions) {
		t.Fatalf("%d rows of round-trip times for %d regions", len(rttMillis), len(regions))
	}
	for i := range rttMillis {
		if len(rttMillis[i]) != len(regions) || rttMillis[i][i] != 5 {
			t.Errorf("row %d: %v, want %d columns and 5 ms within the region", i, rttMillis[i], len(regions))
		}
		for j := range rttMillis[i] {
			if rttMillis[i][j] != rttMillis[j][i] {
				t.Errorf("RTT %s–%s is %v but %s–%s is %v", regions[i].name, regions[j].name, rttMillis[i][j],
					regions[j].name, regions[i].name, rttMillis[j][i])
			}
		}
	}
}

func TestLinkLatency(t *testing.T) {
	rng := testRNG(2)
	for range 1000 {
		// Frankfurt–Sydney: half of 290 ms plus up to 10 %.
		if d := linkLatency(rng, 0, 7); d < 145*time.Millisecond || d > 159500*time.Microsecond {
			t.Fatalf("Frankfurt–Sydney link latency %v, want 145–159.5 ms", d)
		}
		if d := linkLatency(rng, 1, 1); d < 2500*time.Microsecond || d > 2750*time.Microsecond {
			t.Fatalf("London–London link latency %v, want 2.5–2.75 ms", d)
		}
	}
}
