package sim

import (
	"math/rand/v2"
	"slices"
	"testing"
)

func testRNG(seed uint64) *rand.Rand { return rand.New(rand.NewPCG(seed, 1)) } // #nosec G404 -- a test.

func TestRandomRegular(t *testing.T) {
	for seed := range uint64(5) {
		g, err := randomRegular(testRNG(seed), 200, 20)
		if err != nil {
			t.Fatal(err)
		}
		dialed := 0
		for a, targets := range g.dials {
			dialed += len(targets)
			if slices.Contains(targets, a) {
				t.Errorf("seed %d: node %d dials itself", seed, a)
			}
		}
		if links := g.links(); dialed != len(links) || len(links) != 200*20/2 {
			t.Errorf("seed %d: %d dials and %d links, want %d of each (no edge twice)", seed, dialed, len(links), 200*20/2)
		}
		for i, d := range g.degrees() {
			if d != 20 {
				t.Fatalf("seed %d: node %d has degree %d, want 20", seed, i, d)
			}
		}
		if len(g.hubs) != 0 {
			t.Errorf("seed %d: a random regular graph has hubs %v", seed, g.hubs)
		}
	}
}

func TestRandomRegularImpossible(t *testing.T) {
	for _, tc := range [][2]int{{5, 3}, {10, 10}, {3, 5}} {
		if _, err := randomRegular(testRNG(1), tc[0], tc[1]); err == nil {
			t.Errorf("randomRegular(%d, %d) succeeded, want an error", tc[0], tc[1])
		}
	}
}

func TestStaticBootstrap(t *testing.T) {
	const n, k = 300, 6
	g, err := staticBootstrap(testRNG(7), n, k, 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(g.hubs, []int{0, 1, 2, 3, 4, 5}) {
		t.Errorf("hubs = %v, want the first %d nodes", g.hubs, k)
	}
	for i, targets := range g.dials {
		want := 2
		if i < k {
			want = 3
		}
		if len(targets) != want {
			t.Errorf("node %d dials %d peers, want %d", i, len(targets), want)
		}
		for _, h := range targets {
			if h >= k || h == i {
				t.Errorf("node %d dials %d, want another hub", i, h)
			}
		}
	}
	// Nodes that share a hub are not connected (ADR 0007): every link has
	// a hub at one end.
	for _, l := range g.links() {
		if l[0] >= k && l[1] >= k {
			t.Errorf("link %v joins two nodes that are not hubs", l)
		}
	}
}

func TestStaticBootstrapImpossible(t *testing.T) {
	for _, tc := range [][4]int{{10, 1, 1, 0}, {10, 3, 4, 1}, {10, 3, 2, 3}, {3, 5, 2, 2}} {
		if _, err := staticBootstrap(testRNG(1), tc[0], tc[1], tc[2], tc[3]); err == nil {
			t.Errorf("staticBootstrap%v succeeded, want an error", tc)
		}
	}
}

func TestPick(t *testing.T) {
	rng := testRNG(3)
	for range 100 {
		got := pick(rng, 10, 4, 5)
		if len(got) != 4 || !slices.IsSorted(got) || slices.Contains(got, 5) || len(slices.Compact(slices.Clone(got))) != 4 {
			t.Fatalf("pick(10, 4, except 5) = %v, want 4 distinct sorted numbers without 5", got)
		}
	}
}
