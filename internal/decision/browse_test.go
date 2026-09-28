package decision

import (
	"fmt"
	"math/rand/v2"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// keptEngine returns an engine, not started, that keeps the decisions ds as
// if it had evaluated them.
func keptEngine(ds ...Decision) *Engine {
	e := New(nil, testPolicy(), discardLogger(), Options{})
	for _, d := range ds {
		e.apply(d.Indicator.Key(), d, CauseStartup)
	}
	return e
}

// indicator returns the indicator of an address or network.
func indicator(s string) obieproto.Indicator {
	ind := obieproto.Indicator{Kind: obieproto.KindCIDR, Value: s}
	if !strings.Contains(s, "/") {
		ind.Kind = obieproto.KindIPv4
		if strings.Contains(s, ":") {
			ind.Kind = obieproto.KindIPv6
		}
	}
	if err := ind.Normalize(); err != nil {
		panic(err)
	}
	return ind
}

// kept is the decision on s evaluated at t0+at from verdicts, in state if
// set; a block set so expires with its last verdict.
func kept(s string, at time.Duration, state State, verdicts ...*obieproto.Event) Decision {
	ind := indicator(s)
	for _, v := range verdicts {
		v.Indicator = ind
	}
	d := Evaluate(ind, verdicts, testPolicy(), t0.Add(at))
	switch state {
	case StateBlock:
		d.State = state
		for _, v := range verdicts {
			d.ExpiresAt = later(d.ExpiresAt, v.ExpiresAt())
		}
	case StateAllowed, StateNone:
		d.State, d.ExpiresAt = state, time.Time{}
	}
	return d
}

// about is a ban verdict by publisher with confidence, about reason over
// protocol, issued an hour before t0 for ttl.
func about(publisher, reason, protocol string, confidence float64, ttl time.Duration) *obieproto.Event {
	v := verdict(publisher, obieproto.ActionBan, confidence, t0.Add(-time.Hour), ttl)
	v.Evidence.Reason, v.Protocol = reason, protocol
	return v
}

// browseSet is a set of decisions on IPv4 and IPv6 addresses and networks.
func browseSet() []Decision {
	return []Decision{
		kept("203.0.113.7", 6*time.Second, "", about(pubA, "password_bruteforce", "ssh", 1, 3*time.Hour),
			about(pubB, "password_bruteforce", "ssh", 1, 2*time.Hour), about(pubC, "port_scan", "tcp", 0.5, 2*time.Hour)),
		kept("203.0.113.8", 5*time.Second, "", about(pubA, "port_scan", "tcp", 0.9, 2*time.Hour)),
		kept("203.0.113.0/24", 4*time.Second, StateBlock, about(pubC, "password_bruteforce", "ssh", 1, 90*time.Minute)),
		kept("2001:db8::1", 3*time.Second, StateBlock, about(pubB, "password_bruteforce", "ssh", 1, 4*time.Hour)),
		kept("2001:db8::/48", 2*time.Second, StateAllowed, about(pubA, "password_bruteforce", "ssh", 1, 2*time.Hour)),
		kept("198.51.100.9", time.Second, StateAllowed, about(pubD, "http_probe", "http", 1, 2*time.Hour)),
	}
}

// values returns the indicator values of a page.
func values(p BrowsePage) []string {
	out := make([]string, len(p.Items))
	for i, it := range p.Items {
		out[i] = it.Decision.Indicator.Value
	}
	return out
}

func TestBrowseFilters(t *testing.T) {
	e := keptEngine(browseSet()...)
	all := map[State]int{StateBlock: 3, StateNone: 1, StateAllowed: 2}
	for _, tc := range []struct {
		name   string
		q      Query
		want   []string
		states map[State]int
	}{
		{"everything, last decided first", Query{},
			[]string{"203.0.113.7", "203.0.113.8", "203.0.113.0/24", "2001:db8::1", "2001:db8::/48", "198.51.100.9"}, all},
		{"a state; the state counts ignore it", Query{State: StateBlock},
			[]string{"203.0.113.7", "203.0.113.0/24", "2001:db8::1"}, all},
		{"a category", Query{Category: "port_scan/tcp"}, []string{"203.0.113.7", "203.0.113.8"},
			map[State]int{StateBlock: 1, StateNone: 1}},
		{"a publisher", Query{Publisher: pubC}, []string{"203.0.113.7", "203.0.113.0/24"}, map[State]int{StateBlock: 2}},
		{"a category and a publisher", Query{Category: "port_scan/tcp", Publisher: pubB}, []string{"203.0.113.7"},
			map[State]int{StateBlock: 1}},
		{"an address finds the network around it", Query{Overlapping: netip.MustParsePrefix("203.0.113.7/32")},
			[]string{"203.0.113.7", "203.0.113.0/24"}, map[State]int{StateBlock: 2}},
		{"an address the node has no decision on, inside a network", Query{Overlapping: netip.MustParsePrefix("203.0.113.99/32")},
			[]string{"203.0.113.0/24"}, map[State]int{StateBlock: 1}},
		{"a network finds what is inside it and around it", Query{Overlapping: netip.MustParsePrefix("203.0.113.0/25")},
			[]string{"203.0.113.7", "203.0.113.8", "203.0.113.0/24"}, map[State]int{StateBlock: 2, StateNone: 1}},
		{"an IPv6 address", Query{Overlapping: netip.MustParsePrefix("2001:db8::1/128")},
			[]string{"2001:db8::1", "2001:db8::/48"}, map[State]int{StateBlock: 1, StateAllowed: 1}},
		{"an address nothing covers", Query{Overlapping: netip.MustParsePrefix("192.0.2.1/32")}, nil, map[State]int{}},
		{"a predicate on the range", Query{Where: func(p netip.Prefix) bool { return p.Addr().Is6() }},
			[]string{"2001:db8::1", "2001:db8::/48"}, map[State]int{StateBlock: 1, StateAllowed: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := e.Browse(tc.q)
			if got := values(p); !slices.Equal(got, tc.want) {
				t.Errorf("Browse = %v, want %v", got, tc.want)
			}
			if p.Total != len(tc.want) || p.Offset != 0 {
				t.Errorf("Total, Offset = %d, %d, want %d, 0", p.Total, p.Offset, len(tc.want))
			}
			for _, s := range States {
				if p.States[s] != tc.states[s] {
					t.Errorf("States = %v, want %v", p.States, tc.states)
					break
				}
			}
		})
	}
}

func TestBrowseSorts(t *testing.T) {
	e := keptEngine(browseSet()...)
	for _, tc := range []struct {
		sort Sort
		want []string
	}{
		{SortAddress, []string{"198.51.100.9", "203.0.113.0/24", "203.0.113.7", "203.0.113.8", "2001:db8::/48", "2001:db8::1"}},
		// Ties are ordered by key: cidr, then ipv4, then ipv6.
		{SortState, []string{"203.0.113.0/24", "203.0.113.7", "2001:db8::1", "2001:db8::/48", "198.51.100.9", "203.0.113.8"}},
		{SortScore, []string{"203.0.113.7", "2001:db8::/48", "203.0.113.0/24", "2001:db8::1", "203.0.113.8", "198.51.100.9"}},
		{SortPublishers, []string{"203.0.113.7", "2001:db8::/48", "203.0.113.0/24", "203.0.113.8", "2001:db8::1", "198.51.100.9"}},
		// Only blocks expire; the others follow.
		{SortExpires, []string{"203.0.113.0/24", "203.0.113.7", "2001:db8::1", "2001:db8::/48", "198.51.100.9", "203.0.113.8"}},
		{"unknown", []string{"203.0.113.7", "203.0.113.8", "203.0.113.0/24", "2001:db8::1", "2001:db8::/48", "198.51.100.9"}},
	} {
		if got := values(e.Browse(Query{Sort: tc.sort})); !slices.Equal(got, tc.want) {
			t.Errorf("sort %s = %v, want %v", tc.sort, got, tc.want)
		}
	}
}

func TestBrowseItems(t *testing.T) {
	e := keptEngine(browseSet()...)
	p := e.Browse(Query{Limit: 1})
	if len(p.Items) != 1 || p.Total != 6 || p.Generation != e.Generation() || p.Generation == 0 {
		t.Fatalf("page = %+v", p)
	}
	it := p.Items[0]
	if it.Decision.Indicator.Value != "203.0.113.7" || it.Decision.Publishers != nil || it.Verdicts != 3 ||
		!slices.Equal(it.Categories, []string{"password_bruteforce/ssh", "port_scan/tcp"}) ||
		it.Cursor != fmt.Sprintf("decided:%d,ipv4:203.0.113.7", t0.Add(6*time.Second).UnixNano()) {
		t.Errorf("item = %+v", it)
	}
}

// TestBrowsePages: pages follow each other by cursor in both directions,
// the last page is found directly, and a cursor of another order or a
// broken one gives the first page.
func TestBrowsePages(t *testing.T) {
	e := keptEngine(browseSet()...)
	q := Query{Sort: SortAddress, Limit: 2}
	first := e.Browse(q)
	q.After = first.Items[1].Cursor
	second := e.Browse(q)
	q.After = second.Items[1].Cursor
	third := e.Browse(q)
	q.After = ""
	q.Before = third.Items[0].Cursor
	back := e.Browse(q)
	q.Before, q.Last = "", true
	last := e.Browse(q)
	for _, tc := range []struct {
		name   string
		p      BrowsePage
		want   []string
		offset int
	}{
		{"first", first, []string{"198.51.100.9", "203.0.113.0/24"}, 0},
		{"second", second, []string{"203.0.113.7", "203.0.113.8"}, 2},
		{"third", third, []string{"2001:db8::/48", "2001:db8::1"}, 4},
		{"before the third", back, []string{"203.0.113.7", "203.0.113.8"}, 2},
		{"last", last, []string{"2001:db8::/48", "2001:db8::1"}, 4},
	} {
		if got := values(tc.p); !slices.Equal(got, tc.want) || tc.p.Offset != tc.offset || tc.p.Total != 6 {
			t.Errorf("%s page = %v at %d of %d, want %v at %d of 6", tc.name, got, tc.p.Offset, tc.p.Total, tc.want, tc.offset)
		}
	}
	for _, cursor := range []string{first.Items[1].Cursor, "address:", "address:,", "decided:x,ipv4:203.0.113.7", "nonsense"} {
		p := e.Browse(Query{Sort: SortDecided, Limit: 2, After: cursor})
		if got := values(p); p.Offset != 0 || !slices.Equal(got, []string{"203.0.113.7", "203.0.113.8"}) {
			t.Errorf("after %q = %v at %d, want the first page", cursor, got, p.Offset)
		}
	}
	// A cursor holds its place when its decision is gone.
	e.apply("cidr:203.0.113.0/24", Decision{State: StateNone}, CauseRefresh)
	q = Query{Sort: SortAddress, Limit: 2, After: first.Items[1].Cursor}
	if got := values(e.Browse(q)); !slices.Equal(got, []string{"203.0.113.7", "203.0.113.8"}) {
		t.Errorf("after a removed decision = %v", got)
	}
}

// TestBrowsePagesMatchSorting: walking the pages forward and backward in
// every order yields exactly the decisions sorted in full.
func TestBrowsePagesMatchSorting(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2)) // #nosec G404 -- reproducible test data.
	states := []State{StateBlock, StateNone, StateAllowed}
	var ds []Decision
	for i := range 300 {
		addr := fmt.Sprintf("198.51.%d.%d", rng.IntN(4), rng.IntN(256))
		if i%10 == 0 {
			addr = fmt.Sprintf("2001:db8::%x", rng.IntN(1<<16))
		}
		if i%15 == 0 {
			addr = fmt.Sprintf("198.51.%d.0/24", rng.IntN(4))
		}
		d := kept(addr, time.Duration(rng.IntN(20))*time.Second, states[rng.IntN(3)],
			about(pubA, "password_bruteforce", "ssh", float64(rng.IntN(4))/4, time.Duration(1+rng.IntN(3))*time.Hour))
		ds = append(ds, d)
	}
	e := keptEngine(ds...)
	for _, order := range Sorts {
		t.Run(string(order), func(t *testing.T) {
			var all []sortKey
			for i := range e.kept.items {
				all = append(all, order.keyOf(&e.kept.items[i]))
			}
			slices.SortFunc(all, func(a, b sortKey) int { return order.compare(&a, &b) })
			want := make([]string, len(all))
			for i, k := range all {
				want[i] = k.key
			}
			var forward []string
			for q := (Query{Sort: order, Limit: 7}); ; {
				p := e.Browse(q)
				if p.Offset != len(forward) || p.Total != len(all) {
					t.Fatalf("page at %d of %d, want at %d of %d", p.Offset, p.Total, len(forward), len(all))
				}
				forward = append(forward, keys(p)...)
				if p.Offset+len(p.Items) >= p.Total {
					break
				}
				q.After = p.Items[len(p.Items)-1].Cursor
			}
			var backward []string
			for q := (Query{Sort: order, Limit: 7, Last: true}); ; {
				p := e.Browse(q)
				if p.Offset != len(all)-len(backward)-len(p.Items) {
					t.Fatalf("page at %d, want at %d", p.Offset, len(all)-len(backward)-len(p.Items))
				}
				backward = append(keys(p), backward...)
				if p.Offset == 0 {
					break
				}
				q.Before, q.Last = p.Items[0].Cursor, false
			}
			if !slices.Equal(forward, want) || !slices.Equal(backward, want) {
				t.Errorf("pages forward %v\nbackward %v\nwant %v", forward, backward, want)
			}
		})
	}
}

func keys(p BrowsePage) []string {
	out := make([]string, len(p.Items))
	for i, it := range p.Items {
		out[i] = it.Decision.Indicator.Key()
	}
	return out
}

func TestCoveringKeys(t *testing.T) {
	keys, ok := coveringKeys(netip.MustParsePrefix("203.0.113.7/32"))
	if !ok || len(keys) != 17 || keys[0] != "ipv4:203.0.113.7" || keys[1] != "cidr:203.0.113.6/31" ||
		keys[16] != "cidr:203.0.0.0/16" {
		t.Errorf("coveringKeys(IPv4) = %v, %v", keys, ok)
	}
	keys, ok = coveringKeys(netip.MustParsePrefix("2001:db8::1/128"))
	if !ok || len(keys) != 97 || keys[0] != "ipv6:2001:db8::1" || keys[96] != "cidr:2001:db8::/32" {
		t.Errorf("coveringKeys(IPv6) = %d keys, %v", len(keys), ok)
	}
	if _, ok := coveringKeys(netip.MustParsePrefix("203.0.113.0/24")); ok {
		t.Error("coveringKeys of a network")
	}
	if _, ok := coveringKeys(netip.Prefix{}); ok {
		t.Error("coveringKeys of nothing")
	}
}

// millionDecisions is an engine keeping 1,000,000 decisions: a tenth of
// them networks, a fifth IPv6, a third blocks, over four categories and
// eight publishers.
func millionDecisions(b *testing.B) *Engine {
	b.Helper()
	e := New(nil, testPolicy(), discardLogger(), Options{})
	publishers := []string{pubA, pubB, pubC, pubD, self, unlisted, "12D3KooWPublisherEeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		"12D3KooWPublisherFfffffffffffffffffffffffffffffffff"}
	reasons := [][2]string{{"password_bruteforce", "ssh"}, {"port_scan", "tcp"}, {"http_probe", "http"}, {"spam", "smtp"}}
	for i := range 1_000_000 {
		var s string
		switch {
		case i%10 == 0:
			s = fmt.Sprintf("10.%d.%d.0/24", i>>16&0xff, i>>8&0xff)
		case i%5 == 1:
			s = fmt.Sprintf("2001:db8::%x:%x", i>>16, i&0xffff)
		default:
			s = fmt.Sprintf("11.%d.%d.%d", i>>16&0xff, i>>8&0xff, i&0xff)
		}
		ind := indicator(s)
		state := []State{StateBlock, StateNone, StateAllowed}[i%3]
		d := Decision{Indicator: ind, State: state, Score: float64(i%17) / 8, Threshold: 1.8, Contributors: i % 4, Quorum: 2,
			EvaluatedAt: t0.Add(time.Duration(i) * time.Millisecond), ExpiresAt: t0.Add(time.Duration(i%1000) * time.Minute)}
		for j := range 1 + i%3 {
			r := reasons[(i+j)%len(reasons)]
			d.Publishers = append(d.Publishers, Contribution{PeerID: publishers[(i+j)%len(publishers)], Reason: r[0], Protocol: r[1],
				Contributes: j%2 == 0})
		}
		e.apply(ind.Key(), d, CauseStartup)
	}
	return e
}

// BenchmarkBrowse reads one page of 50 of 1,000,000 kept decisions
// (performance.md).
func BenchmarkBrowse(b *testing.B) {
	e := millionDecisions(b)
	deep := e.Browse(Query{Sort: SortAddress, Limit: 1, After: fmt.Sprintf("address:,ipv4:11.%d.0.0", 8)})
	for _, bc := range []struct {
		name string
		q    Query
	}{
		{"first page", Query{}},
		{"by address", Query{Sort: SortAddress}},
		{"deep page by address", Query{Sort: SortAddress, After: deep.Items[0].Cursor}},
		{"last page by score", Query{Sort: SortScore, Last: true}},
		{"blocks", Query{State: StateBlock}},
		{"category and publisher", Query{Category: "port_scan/tcp", Publisher: pubB}},
		{"address", Query{Overlapping: netip.MustParsePrefix("11.3.2.1/32")}},
		{"network", Query{Overlapping: netip.MustParsePrefix("11.3.0.0/16")}},
		{"predicate on the range", Query{Where: func(p netip.Prefix) bool { return p.Addr().Is4() }}},
	} {
		b.Run(bc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if p := e.Browse(bc.q); p.Total == 0 && bc.name != "address" {
					b.Fatal("empty page")
				}
			}
		})
	}
}

// BenchmarkPublishMetrics is the engine's own pass over 1,000,000 kept
// decisions after every evaluation, for comparison with BenchmarkBrowse.
func BenchmarkPublishMetrics(b *testing.B) {
	e := millionDecisions(b)
	for b.Loop() {
		e.publishMetrics()
	}
}
