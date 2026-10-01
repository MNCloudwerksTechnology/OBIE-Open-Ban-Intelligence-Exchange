package simtrust

import (
	"bytes"
	"net/netip"
	"testing"
	"time"
)

func TestWorldIsReproducible(t *testing.T) {
	p := DefaultWorld()
	p.Hours = 12
	a, b, c := GenerateWorld(p, 5, testStart), GenerateWorld(p, 5, testStart), GenerateWorld(p, 6, testStart)
	var wa, wb, wc bytes.Buffer
	for _, x := range []struct {
		t *Trace
		w *bytes.Buffer
	}{{a, &wa}, {b, &wb}, {c, &wc}} {
		if err := WriteTrace(x.w, x.t); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(wa.Bytes(), wb.Bytes()) {
		t.Error("one seed gave two worlds")
	}
	if bytes.Equal(wa.Bytes(), wc.Bytes()) {
		t.Error("two seeds gave one world")
	}
}

func TestDefaultWorld(t *testing.T) {
	p := DefaultWorld()
	w := GenerateWorld(p, 1, testStart)
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
	counts := map[Class]int{}
	for _, a := range w.Addresses {
		counts[a.Class]++
	}
	for class, want := range map[Class]int{ClassCDN: 300, ClassCrawler: 120, ClassCustomer: 2000, ClassNAT: 200} {
		if counts[class] != want {
			t.Errorf("%d %s addresses, want %d", counts[class], class, want)
		}
	}
	if got := len(w.Published); got != 18 {
		t.Errorf("%d published ranges, want 18", got)
	}
	for _, a := range w.Addresses {
		published := false
		for _, r := range w.Published {
			published = published || r.Prefix.Contains(a.Addr)
		}
		if want := a.Class == ClassCDN || a.Class == ClassCrawler; published != want {
			t.Errorf("%s address %s in a published range: %v, want %v", a.Class, a.Addr, published, want)
		}
	}
	if len(w.Operators) != 21 {
		t.Fatalf("%d operators, want 21", len(w.Operators))
	}

	// About 30 attackers an hour, 4.5 hits each on average (returns
	// included), 90 % of them banned: some 20,000 bans a week.
	if n := len(w.Observations); n < 12000 || n > 30000 {
		t.Errorf("%d bans in a week, want about 20,000", n)
	}
	operatorsOf := map[netip.Addr]map[int]bool{}
	for _, o := range w.Observations {
		if operatorsOf[o.Addr] == nil {
			operatorsOf[o.Addr] = map[int]bool{}
		}
		operatorsOf[o.Addr][o.Operator] = true
	}
	single, nat := 0, 0
	for addr, ops := range operatorsOf {
		if len(ops) == 1 {
			single++
		}
		if classOf(w, addr) == ClassNAT {
			nat++
		}
	}
	// Most attackers are seen by one operator only (Metcalf and Spring
	// 2016); NAT addresses carry some of them.
	if share := float64(single) / float64(len(operatorsOf)); share < 0.5 || share > 0.8 {
		t.Errorf("%.2f of the banned addresses were banned by one operator, want most", share)
	}
	if nat == 0 {
		t.Error("no shared NAT address was banned")
	}
	t.Logf("%d bans of %d addresses, %.2f by one operator, %d NAT addresses", len(w.Observations), len(operatorsOf),
		float64(single)/float64(len(operatorsOf)), nat)
}

func TestNoRebanWithinBantime(t *testing.T) {
	ops := []Operator{{Bantime: time.Hour}, {Bantime: time.Minute}}
	a := netip.MustParseAddr(victim)
	obs := []Observation{
		{testStart.Add(30 * time.Minute), 0, a},
		{testStart, 0, a},
		{testStart.Add(time.Hour), 0, a},
		{testStart, 1, a},
		{testStart.Add(30 * time.Second), 1, a},
	}
	got := withoutRebans(obs, ops)
	want := []Observation{{testStart, 0, a}, {testStart, 1, a}, {testStart.Add(time.Hour), 0, a}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if !got[i].At.Equal(want[i].At) || got[i].Operator != want[i].Operator {
			t.Errorf("observation %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func classOf(t *Trace, addr netip.Addr) Class {
	for _, a := range t.Addresses {
		if a.Addr == addr {
			return a.Class
		}
	}
	return ""
}
