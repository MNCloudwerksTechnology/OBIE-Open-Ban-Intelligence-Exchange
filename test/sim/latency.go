package sim

import (
	"math/rand/v2"
	"time"
)

// region is where a node runs, with the share of the nodes placed there.
type region struct {
	name  string
	share float64
}

// regions are the eight regions nodes are placed in (ADR 0035), weighted
// towards Europe, where OBIE's first operators run.
var regions = []region{
	{"Frankfurt", 0.30},
	{"London", 0.15},
	{"N. Virginia", 0.20},
	{"Oregon", 0.10},
	{"Tokyo", 0.08},
	{"Singapore", 0.10},
	{"São Paulo", 0.04},
	{"Sydney", 0.03},
}

// rttMillis are the round-trip times between the regions in milliseconds,
// in the order of regions: rounded medians of public inter-region
// measurements, illustrative rather than measured by this project.
var rttMillis = [][]float64{
	//  FRA  LON  IAD  PDX  NRT  SIN  GRU  SYD
	{5, 14, 90, 150, 225, 160, 205, 290},   // Frankfurt
	{14, 5, 76, 140, 210, 170, 185, 265},   // London
	{90, 76, 5, 70, 145, 215, 115, 200},    // N. Virginia
	{150, 140, 70, 5, 100, 165, 175, 140},  // Oregon
	{225, 210, 145, 100, 5, 70, 255, 105},  // Tokyo
	{160, 170, 215, 165, 70, 5, 325, 92},   // Singapore
	{205, 185, 115, 175, 255, 325, 5, 310}, // São Paulo
	{290, 265, 200, 140, 105, 92, 310, 5},  // Sydney
}

// maxLinkJitter is the largest random addition to a link's latency, as a
// share of it; it is drawn once per link.
const maxLinkJitter = 0.1

// placeNodes returns the region of each of n nodes, drawn with the regions'
// shares.
func placeNodes(rng *rand.Rand, n int) []int {
	out := make([]int, n)
	for i := range out {
		x := rng.Float64()
		out[i] = len(regions) - 1
		for r, reg := range regions {
			if x < reg.share {
				out[i] = r
				break
			}
			x -= reg.share
		}
	}
	return out
}

// linkLatency returns the one-way latency of a link between nodes in the
// regions a and b: half their round-trip time plus up to maxLinkJitter of
// it, rounded to the microsecond.
func linkLatency(rng *rand.Rand, a, b int) time.Duration {
	oneWay := rttMillis[a][b] / 2 * (1 + maxLinkJitter*rng.Float64())
	return time.Duration(oneWay * float64(time.Millisecond)).Round(time.Microsecond)
}
