package decision

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// cursors returns the cursors of a page of verdicts.
func cursors(p VerdictPage) []string {
	out := make([]string, len(p.Verdicts))
	for i, v := range p.Verdicts {
		out[i] = v.Cursor
	}
	return out
}

func TestVerdictsFilters(t *testing.T) {
	e := keptEngine(browseSet()...)
	all := []string{
		"cidr:2001:db8::/48," + pubA, "cidr:203.0.113.0/24," + pubC, "ipv4:198.51.100.9," + pubD,
		"ipv4:203.0.113.7," + pubA, "ipv4:203.0.113.7," + pubB, "ipv4:203.0.113.7," + pubC,
		"ipv4:203.0.113.8," + pubA, "ipv6:2001:db8::1," + pubB,
	}
	for _, tc := range []struct {
		name string
		q    VerdictQuery
		want []string
	}{
		{"every verdict, by key and publisher", VerdictQuery{}, all},
		{"a publisher", VerdictQuery{Publisher: pubA},
			[]string{"cidr:2001:db8::/48," + pubA, "ipv4:203.0.113.7," + pubA, "ipv4:203.0.113.8," + pubA}},
		{"every publisher but one", VerdictQuery{Except: pubA}, slices.DeleteFunc(slices.Clone(all),
			func(c string) bool { return strings.HasSuffix(c, pubA) })},
		{"a category", VerdictQuery{Category: "port_scan/tcp"},
			[]string{"ipv4:203.0.113.7," + pubC, "ipv4:203.0.113.8," + pubA}},
		{"an indicator", VerdictQuery{Key: "ipv4:203.0.113.7"}, all[3:6]},
		{"an indicator, a category and every publisher but one",
			VerdictQuery{Key: "ipv4:203.0.113.7", Category: "password_bruteforce/ssh", Except: pubB},
			[]string{"ipv4:203.0.113.7," + pubA}},
		{"an indicator the node holds nothing on", VerdictQuery{Key: "ipv4:192.0.2.1"}, nil},
		{"an unknown publisher", VerdictQuery{Publisher: unlisted}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := e.Verdicts(tc.q)
			if got := cursors(p); !slices.Equal(got, tc.want) {
				t.Errorf("Verdicts = %v, want %v", got, tc.want)
			}
			if p.Total != len(tc.want) || p.Offset != 0 {
				t.Errorf("Total, Offset = %d, %d, want %d, 0", p.Total, p.Offset, len(tc.want))
			}
		})
	}
}

func TestVerdictsItems(t *testing.T) {
	e := keptEngine(browseSet()...)
	p := e.Verdicts(VerdictQuery{Key: "ipv4:198.51.100.9"})
	want := ActiveVerdict{Key: "ipv4:198.51.100.9", Publisher: pubD, Category: "http_probe/http", Counts: false,
		State: StateAllowed, Cursor: "ipv4:198.51.100.9," + pubD}
	if len(p.Verdicts) != 1 || p.Verdicts[0] != want {
		t.Errorf("verdict of a publisher with weight 0 = %+v, want %+v", p.Verdicts, want)
	}
	p = e.Verdicts(VerdictQuery{Key: "cidr:203.0.113.0/24"})
	want = ActiveVerdict{Key: "cidr:203.0.113.0/24", Publisher: pubC, Category: "password_bruteforce/ssh", Counts: true,
		State: StateBlock, Cursor: "cidr:203.0.113.0/24," + pubC}
	if len(p.Verdicts) != 1 || p.Verdicts[0] != want {
		t.Errorf("counting verdict = %+v, want %+v", p.Verdicts, want)
	}
}

// TestVerdictsPages: pages follow each other by cursor; a cursor of an
// ended verdict (with its category) works too, and a cursor holds its place
// when its verdict is gone.
func TestVerdictsPages(t *testing.T) {
	e := keptEngine(browseSet()...)
	first := e.Verdicts(VerdictQuery{Limit: 3})
	second := e.Verdicts(VerdictQuery{Limit: 3, After: first.Verdicts[2].Cursor})
	third := e.Verdicts(VerdictQuery{Limit: 3, After: second.Verdicts[2].Cursor})
	for _, tc := range []struct {
		name   string
		p      VerdictPage
		n, off int
	}{{"first", first, 3, 0}, {"second", second, 3, 3}, {"third", third, 2, 6}} {
		if len(tc.p.Verdicts) != tc.n || tc.p.Offset != tc.off || tc.p.Total != 8 {
			t.Errorf("%s page = %v at %d of %d, want %d at %d of 8", tc.name, cursors(tc.p), tc.p.Offset, tc.p.Total, tc.n, tc.off)
		}
	}
	if got := cursors(second); !slices.Equal(got, []string{"ipv4:203.0.113.7," + pubA, "ipv4:203.0.113.7," + pubB,
		"ipv4:203.0.113.7," + pubC}) {
		t.Errorf("second page = %v", got)
	}
	ended := e.Verdicts(VerdictQuery{Limit: 3, After: "ipv4:203.0.113.7," + pubA + ",password_bruteforce/ssh"})
	if got := cursors(ended); len(got) != 3 || got[0] != "ipv4:203.0.113.7,"+pubB || ended.Offset != 4 {
		t.Errorf("after an ended verdict's cursor = %v at %d", got, ended.Offset)
	}
	e.apply("ipv4:203.0.113.7", Decision{State: StateNone}, CauseRefresh)
	if got := cursors(e.Verdicts(VerdictQuery{Limit: 1, After: first.Verdicts[2].Cursor})); !slices.Equal(got,
		[]string{"ipv4:203.0.113.8," + pubA}) {
		t.Errorf("after a removed decision = %v", got)
	}
}

// TestVerdictsPagesMatchSorting: walking the pages yields exactly the
// verdicts sorted in full, under every filter.
func TestVerdictsPagesMatchSorting(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4)) // #nosec G404 -- reproducible test data.
	publishers := []string{pubA, pubB, pubC, pubD, self}
	var ds []Decision
	for i := range 300 {
		addr := fmt.Sprintf("198.51.%d.%d", i/256, i%256)
		if i%10 == 0 {
			addr = fmt.Sprintf("2001:db8::%x", i)
		}
		var vs []*obieproto.Event
		for _, p := range publishers {
			if rng.IntN(3) == 0 {
				vs = append(vs, about(p, []string{"password_bruteforce", "port_scan"}[rng.IntN(2)], "ssh", 1, 2*time.Hour))
			}
		}
		if len(vs) > 0 {
			ds = append(ds, kept(addr, 0, "", vs...))
		}
	}
	e := keptEngine(ds...)
	for _, q := range []VerdictQuery{{}, {Publisher: pubB}, {Except: self}, {Category: "port_scan/ssh"}} {
		want := cursors(e.Verdicts(VerdictQuery{Publisher: q.Publisher, Except: q.Except, Category: q.Category,
			Limit: MaxBrowseLimit}))
		if !slices.IsSorted(want) || len(want) == 0 {
			t.Fatalf("%+v: all on one page = %v", q, want)
		}
		var got []string
		for q.Limit = 7; ; {
			p := e.Verdicts(q)
			if p.Offset != len(got) || p.Total != len(want) {
				t.Fatalf("%+v: page at %d of %d, want at %d of %d", q, p.Offset, p.Total, len(got), len(want))
			}
			got = append(got, cursors(p)...)
			if p.Offset+len(p.Verdicts) >= p.Total {
				break
			}
			q.After = p.Verdicts[len(p.Verdicts)-1].Cursor
		}
		if !slices.Equal(got, want) {
			t.Errorf("%+v: pages %v, want %v", q, got, want)
		}
	}
}

// BenchmarkVerdicts reads one page of 50 of the 2,000,000 active verdicts
// of 1,000,000 kept decisions (performance.md).
func BenchmarkVerdicts(b *testing.B) {
	e := millionDecisions(b)
	for _, bc := range []struct {
		name string
		q    VerdictQuery
	}{
		{"first page", VerdictQuery{}},
		{"deep page", VerdictQuery{After: "ipv4:11.8.0.0"}},
		{"a publisher", VerdictQuery{Publisher: pubB}},
		{"received", VerdictQuery{Except: self}},
		{"a category", VerdictQuery{Category: "port_scan/tcp"}},
		{"an indicator", VerdictQuery{Key: "ipv4:11.3.2.2"}},
	} {
		b.Run(bc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if p := e.Verdicts(bc.q); p.Total == 0 {
					b.Fatal("empty page")
				}
			}
		})
	}
}
