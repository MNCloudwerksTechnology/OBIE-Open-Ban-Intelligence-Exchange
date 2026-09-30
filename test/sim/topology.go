package sim

import (
	"fmt"
	"math/rand/v2"
	"slices"
)

// graph says which peers each node dials: its mesh.bootstrap list, or the
// honest nodes a Sybil targets. A link joins two nodes if either dials the
// other.
type graph struct {
	// dials are the nodes each node dials.
	dials [][]int
	// hubs are the static-bootstrap graph's hubs; none in other graphs.
	hubs []int
}

func newGraph(n int) *graph { return &graph{dials: make([][]int, n)} }

// links returns every pair of nodes that one of them dials, once, the
// lower index first, in a stable order.
func (g *graph) links() [][2]int {
	seen := map[[2]int]bool{}
	var out [][2]int
	for a, targets := range g.dials {
		for _, b := range targets {
			p := [2]int{min(a, b), max(a, b)}
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// degree returns the number of distinct nodes node i has a link with.
func (g *graph) degrees() []int {
	deg := make([]int, len(g.dials))
	for _, l := range g.links() {
		deg[l[0]]++
		deg[l[1]]++
	}
	return deg
}

// randomRegular returns a random d-regular graph on n nodes (n·d even,
// d < n), built with the configuration model and repaired by edge
// switches; the dialer of each edge is chosen at random.
func randomRegular(rng *rand.Rand, n, d int) (*graph, error) {
	if d >= n || n*d%2 != 0 {
		return nil, fmt.Errorf("no %d-regular graph on %d nodes", d, n)
	}
	stubs := make([]int, 0, n*d)
	for i := range n {
		for range d {
			stubs = append(stubs, i)
		}
	}
	rng.Shuffle(len(stubs), func(i, j int) { stubs[i], stubs[j] = stubs[j], stubs[i] })
	edges := make([][2]int, 0, n*d/2)
	for i := 0; i < len(stubs); i += 2 {
		edges = append(edges, [2]int{stubs[i], stubs[i+1]})
	}
	if err := repair(rng, edges); err != nil {
		return nil, err
	}
	g := newGraph(n)
	for _, e := range edges {
		if rng.IntN(2) == 0 {
			e[0], e[1] = e[1], e[0]
		}
		g.dials[e[0]] = append(g.dials[e[0]], e[1])
	}
	return g, nil
}

// repair removes self-loops and repeated edges from the multigraph edges
// by switching each bad edge with a random other one, keeping every
// node's degree.
func repair(rng *rand.Rand, edges [][2]int) error {
	count := map[[2]int]int{}
	key := func(e [2]int) [2]int { return [2]int{min(e[0], e[1]), max(e[0], e[1])} }
	for _, e := range edges {
		count[key(e)]++
	}
	bad := func(e [2]int) bool { return e[0] == e[1] || count[key(e)] > 1 }
	for round := 0; round < 100; round++ {
		clean := true
		for i := range edges {
			if !bad(edges[i]) {
				continue
			}
			clean = false
			for range 1000 {
				j := rng.IntN(len(edges))
				if j == i {
					continue
				}
				a, b, c, dd := edges[i][0], edges[i][1], edges[j][0], edges[j][1]
				e1, e2 := [2]int{a, c}, [2]int{b, dd}
				if e1[0] == e1[1] || e2[0] == e2[1] || count[key(e1)] > 0 || count[key(e2)] > 0 || key(e1) == key(e2) {
					continue
				}
				count[key(edges[i])]--
				count[key(edges[j])]--
				edges[i], edges[j] = e1, e2
				count[key(e1)]++
				count[key(e2)]++
				break
			}
		}
		if clean {
			return nil
		}
	}
	return fmt.Errorf("could not repair the random regular graph")
}

// staticBootstrap returns today's static-bootstrap graph on n nodes (ADR
// 0033): the first k nodes are hubs, every other node dials perNode
// random hubs, and every hub dials perHub random other hubs.
func staticBootstrap(rng *rand.Rand, n, k, perNode, perHub int) (*graph, error) {
	if k < 2 || k > n || perNode > k || perHub >= k {
		return nil, fmt.Errorf("no static-bootstrap graph with %d nodes, %d hubs, %d hubs per node and %d per hub",
			n, k, perNode, perHub)
	}
	g := newGraph(n)
	for h := range k {
		g.hubs = append(g.hubs, h)
		g.dials[h] = pick(rng, k, perHub, h)
	}
	for i := k; i < n; i++ {
		g.dials[i] = pick(rng, k, perNode, -1)
	}
	return g, nil
}

// pick returns m distinct random numbers in [0, n) other than except,
// sorted.
func pick(rng *rand.Rand, n, m, except int) []int {
	chosen := map[int]bool{}
	for len(chosen) < m {
		if x := rng.IntN(n); x != except {
			chosen[x] = true
		}
	}
	out := make([]int, 0, m)
	for x := range chosen {
		out = append(out, x)
	}
	slices.Sort(out)
	return out
}
