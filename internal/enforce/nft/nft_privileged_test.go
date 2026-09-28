//go:build privileged && linux

// Integration tests against the kernel. They run in a fresh network
// namespace: TestMain re-executes the test binary under `unshare -rn`
// (or `unshare -n` as root) and skips everything if that is impossible,
// so the host's firewall is never touched. See CONTRIBUTING.md.
package nft

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"slices"
	"testing"
	"time"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"

	"github.com/MNCloudwerksTechnology/obie/internal/enforce"
)

// netnsEnv marks the re-executed test binary running in its own namespace.
const netnsEnv = "OBIE_TEST_NETNS"

var isolated = os.Getenv(netnsEnv) == "1"

func TestMain(m *testing.M) {
	if !isolated {
		if code, ok := reexecIsolated(); ok {
			os.Exit(code)
		}
	}
	os.Exit(m.Run())
}

// reexecIsolated runs the test binary again in a new network namespace and
// returns its exit code; ok is false if no namespace can be created.
func reexecIsolated() (code int, ok bool) {
	for _, flags := range [][]string{{"-rn"}, {"-n"}} {
		if exec.Command("unshare", append(flags, "true")...).Run() != nil { // #nosec G204 -- fixed arguments.
			continue
		}
		args := append(append(flags, "--"), os.Args...)
		cmd := exec.Command("unshare", args...) // #nosec G204 G702 -- re-executes this test binary.
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		cmd.Env = append(os.Environ(), netnsEnv+"=1")
		err := cmd.Run()
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode(), true
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "re-exec in a network namespace: %v\n", err)
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// requireNetns skips tb outside a private network namespace.
func requireNetns(tb testing.TB) {
	tb.Helper()
	if !isolated {
		tb.Skip("cannot create a network namespace (unshare -rn / unshare -n); not touching the host firewall")
	}
}

func newBackend(tb testing.TB, forward bool) *Backend {
	tb.Helper()
	requireNetns(tb)
	b := New(Options{Forward: forward}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	tb.Cleanup(func() { _ = b.Teardown(context.Background()) })
	return b
}

func entry(prefix string, ttl time.Duration) enforce.Entry {
	return enforce.Entry{Prefix: netip.MustParsePrefix(prefix), Expires: time.Now().Add(ttl)}
}

// sample is 3 IPv4 addresses (two adjacent), 2 IPv6 addresses and a CIDR.
func sample() []enforce.Entry {
	return []enforce.Entry{
		entry("192.0.2.1/32", time.Hour),
		entry("192.0.2.2/32", 2*time.Hour),
		entry("198.51.100.7/32", 3*time.Hour),
		entry("2001:db8::1/128", time.Hour),
		entry("2001:db8::2:1/128", 30*time.Minute),
		entry("203.0.113.0/24", 90*time.Minute),
	}
}

// assertEntries checks got against want: the same prefixes, expiries
// within enforce.ExpiryTolerance.
func assertEntries(t *testing.T, got, want []enforce.Entry) {
	t.Helper()
	want = slices.Clone(want)
	slices.SortFunc(want, func(a, b enforce.Entry) int {
		if c := a.Prefix.Addr().Compare(b.Prefix.Addr()); c != 0 {
			return c
		}
		return a.Prefix.Bits() - b.Prefix.Bits()
	})
	if len(got) != len(want) {
		t.Fatalf("listed %d entries %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range got {
		if got[i].Prefix != want[i].Prefix || got[i].Expires.Sub(want[i].Expires).Abs() > enforce.ExpiryTolerance {
			t.Errorf("entry %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func list(t *testing.T, b *Backend) []enforce.Entry {
	t.Helper()
	got, err := b.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	return got
}

func TestSetupCreatesTheTable(t *testing.T) {
	b := newBackend(t, true)
	if err := b.Setup(context.Background()); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	conn := &nftables.Conn{}
	tbl, err := findTable(conn)
	if err != nil || tbl == nil {
		t.Fatalf("table inet obie: %v %v", tbl, err)
	}
	sets, err := conn.GetSets(tbl)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sets {
		if !s.Interval || !s.HasTimeout {
			t.Errorf("set %s: interval %v timeout %v, want both", s.Name, s.Interval, s.HasTimeout)
		}
	}
	for _, name := range []string{ChainInput, ChainForward} {
		c, err := conn.ListChain(tbl, name)
		if err != nil {
			t.Fatalf("chain %s: %v", name, err)
		}
		if c.Type != nftables.ChainTypeFilter || *c.Priority != Priority || *c.Policy != nftables.ChainPolicyAccept {
			t.Errorf("chain %s = %+v", name, c)
		}
		rules, err := conn.GetRules(tbl, c)
		if err != nil || len(rules) != 2 {
			t.Fatalf("rules of %s: %d %v", name, len(rules), err)
		}
	}
	ok, err := b.matches(conn, tbl)
	if err != nil || !ok {
		t.Errorf("matches = %v %v, want true", ok, err)
	}
}

func TestApplyListRemove(t *testing.T) {
	b := newBackend(t, false)
	ctx := context.Background()
	if err := b.Setup(ctx); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	add := sample()
	if err := b.Apply(ctx, add, nil); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	assertEntries(t, list(t, b), add)
	elems, err := (&nftables.Conn{}).GetSetElements(newSet(table(), families[0]))
	if err != nil {
		t.Fatal(err)
	}
	for _, el := range elems {
		if a, _ := netip.AddrFromSlice(el.Key); !el.IntervalEnd && el.Comment != netip.PrefixFrom(a, 32).String() && el.Comment != "203.0.113.0/24" {
			t.Errorf("start element %v has comment %q, want its prefix", a, el.Comment)
		}
	}

	// Replace one with a new expiry, remove two, in one call.
	moved := entry("192.0.2.2/32", 5*time.Hour)
	if err := b.Apply(ctx, []enforce.Entry{moved}, []enforce.Entry{add[1], add[3], add[5]}); err != nil {
		t.Fatalf("Apply replace/remove: %v", err)
	}
	assertEntries(t, list(t, b), []enforce.Entry{add[0], moved, add[2], add[4]})

	if err := b.Apply(ctx, nil, []enforce.Entry{add[0], moved, add[2], add[4]}); err != nil {
		t.Fatalf("Apply remove all: %v", err)
	}
	assertEntries(t, list(t, b), nil)
}

func TestSetupIsIdempotentAndKeepsEntries(t *testing.T) {
	b := newBackend(t, false)
	ctx := context.Background()
	if err := b.Setup(ctx); err != nil {
		t.Fatal(err)
	}
	if err := b.Apply(ctx, sample(), nil); err != nil {
		t.Fatal(err)
	}
	if err := New(Options{}, b.log).Setup(ctx); err != nil {
		t.Fatalf("second Setup: %v", err)
	}
	assertEntries(t, list(t, b), sample())
}

func TestSetupReplacesADifferentStructure(t *testing.T) {
	b := newBackend(t, false)
	ctx := context.Background()
	// A stale table inet obie: a set without timeouts and a foreign chain.
	conn := &nftables.Conn{}
	tbl := conn.AddTable(table())
	if err := conn.AddSet(&nftables.Set{Table: tbl, Name: SetV4, KeyType: nftables.TypeIPAddr}, nil); err != nil {
		t.Fatal(err)
	}
	conn.AddChain(&nftables.Chain{Name: "stale", Table: tbl})
	if err := conn.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := b.Setup(ctx); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	ok, err := b.matches(conn, table())
	if err != nil || !ok {
		t.Fatalf("after Setup matches = %v %v", ok, err)
	}

	// Enabling the forward chain replaces the table too.
	fwd := New(Options{Forward: true}, b.log)
	if err := fwd.Setup(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ListChain(table(), ChainForward); err != nil {
		t.Errorf("forward chain: %v", err)
	}
}

func TestListDetectsDriftAndSetupHeals(t *testing.T) {
	b := newBackend(t, false)
	ctx := context.Background()
	if err := b.Setup(ctx); err != nil {
		t.Fatal(err)
	}
	conn := &nftables.Conn{}
	conn.FlushChain(&nftables.Chain{Name: ChainInput, Table: table()})
	if err := conn.Flush(); err != nil {
		t.Fatal(err)
	}
	if _, err := b.List(ctx); !errors.Is(err, ErrDrift) {
		t.Fatalf("List after a flushed chain: %v, want ErrDrift", err)
	}
	if err := b.Setup(ctx); err != nil {
		t.Fatal(err)
	}
	list(t, b)

	if err := b.Teardown(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := b.List(ctx); !errors.Is(err, ErrDrift) {
		t.Fatalf("List without the table: %v, want ErrDrift", err)
	}
}

func TestUnrelatedTableIsUntouched(t *testing.T) {
	b := newBackend(t, true)
	ctx := context.Background()
	conn := &nftables.Conn{}
	other := conn.AddTable(&nftables.Table{Name: "other", Family: nftables.TableFamilyINet})
	set := &nftables.Set{Table: other, Name: SetV4, KeyType: nftables.TypeIPAddr}
	if err := conn.AddSet(set, []nftables.SetElement{{Key: netip.MustParseAddr("192.0.2.9").AsSlice()}}); err != nil {
		t.Fatal(err)
	}
	accept := nftables.ChainPolicyAccept
	chain := conn.AddChain(&nftables.Chain{Name: ChainInput, Table: other, Type: nftables.ChainTypeFilter,
		Hooknum: nftables.ChainHookInput, Priority: nftables.ChainPriorityFilter, Policy: &accept})
	conn.AddRule(&nftables.Rule{Table: other, Chain: chain, Exprs: []expr.Any{&expr.Counter{}}})
	if err := conn.Flush(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.DelTable(other); _ = conn.Flush() })

	if err := b.Setup(ctx); err != nil {
		t.Fatal(err)
	}
	if err := b.Apply(ctx, sample(), nil); err != nil {
		t.Fatal(err)
	}
	if err := New(Options{}, b.log).Setup(ctx); err != nil { // replaces inet obie
		t.Fatal(err)
	}
	if err := b.Teardown(ctx); err != nil {
		t.Fatal(err)
	}

	elems, err := conn.GetSetElements(set)
	if err != nil || len(elems) != 1 {
		t.Errorf("elements of inet other %s: %v %v", SetV4, elems, err)
	}
	rules, err := conn.GetRules(other, chain)
	if err != nil || len(rules) != 1 {
		t.Errorf("rules of inet other input: %d %v", len(rules), err)
	}
}

func TestTeardown(t *testing.T) {
	b := newBackend(t, false)
	ctx := context.Background()
	if err := b.Setup(ctx); err != nil {
		t.Fatal(err)
	}
	for range 2 { // a missing table is no error
		if err := b.Teardown(ctx); err != nil {
			t.Fatalf("Teardown: %v", err)
		}
	}
	if tbl, err := findTable(&nftables.Conn{}); err != nil || tbl != nil {
		t.Errorf("table after Teardown: %v %v", tbl, err)
	}
}

// TestKernelExpiresEntries checks that the timeout reaches the kernel.
func TestKernelExpiresEntries(t *testing.T) {
	b := newBackend(t, false)
	ctx := context.Background()
	if err := b.Setup(ctx); err != nil {
		t.Fatal(err)
	}
	if err := b.Apply(ctx, []enforce.Entry{entry("192.0.2.1/32", 1500*time.Millisecond), entry("2001:db8::/64", time.Hour)}, nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
	assertEntries(t, list(t, b), []enforce.Entry{entry("2001:db8::/64", time.Hour)})

	// A range across the expired one fits.
	wide := entry("192.0.2.0/24", time.Hour)
	if err := b.Apply(ctx, []enforce.Entry{wide}, nil); err != nil {
		t.Fatal(err)
	}
	assertEntries(t, list(t, b), []enforce.Entry{wide, entry("2001:db8::/64", time.Hour)})
}

// TestBlocksTraffic sends UDP over the loopback from a blocked source and
// checks that it is dropped and counted.
func TestBlocksTraffic(t *testing.T) {
	b := newBackend(t, false)
	if err := exec.Command("ip", "link", "set", "lo", "up").Run(); err != nil {
		t.Skipf("cannot bring up lo: %v", err)
	}
	ctx := context.Background()
	if err := b.Setup(ctx); err != nil {
		t.Fatal(err)
	}
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pc.Close() }()
	send := func() bool {
		c, err := net.Dial("udp4", pc.LocalAddr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = c.Close() }()
		if _, err := c.Write([]byte("x")); err != nil {
			t.Fatal(err)
		}
		_ = pc.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		_, _, err = pc.ReadFrom(make([]byte, 8))
		return err == nil
	}
	if !send() {
		t.Fatal("packet lost before blocking")
	}
	if err := b.Apply(ctx, []enforce.Entry{entry("127.0.0.0/8", time.Hour)}, nil); err != nil {
		t.Fatal(err)
	}
	if send() {
		t.Fatal("packet from a blocked source delivered")
	}
	conn := &nftables.Conn{}
	rules, err := conn.GetRules(table(), &nftables.Chain{Name: ChainInput, Table: table()})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range rules[0].Exprs {
		if c, ok := e.(*expr.Counter); ok && c.Packets == 0 {
			t.Error("ip saddr @obie_v4 counter is 0")
		}
	}
}

// entries100k returns 100,000 distinct IPv4 addresses, every other one of
// 10.0.0.0/8, and a few IPv6 ranges.
func entries100k() []enforce.Entry {
	out := make([]enforce.Entry, 0, 100_000)
	exp := time.Now().Add(time.Hour)
	for i := range 99_000 {
		a := netip.AddrFrom4([4]byte{10, byte(i >> 15), byte(i >> 7), byte(i<<1) & 0xfe})
		out = append(out, enforce.Entry{Prefix: netip.PrefixFrom(a, 32), Expires: exp})
	}
	for i := range 1_000 {
		a := netip.AddrFrom16([16]byte{0x20, 0x01, 0x0d, 0xb8, byte(i >> 8), byte(i)})
		out = append(out, enforce.Entry{Prefix: netip.PrefixFrom(a, 48), Expires: exp})
	}
	return out
}

func TestApply100kUnder5s(t *testing.T) {
	b := newBackend(t, false)
	ctx := context.Background()
	if err := b.Setup(ctx); err != nil {
		t.Fatal(err)
	}
	entries := entries100k()
	start := time.Now()
	if err := b.Apply(ctx, entries, nil); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	took := time.Since(start)
	t.Logf("applied %d entries in %v", len(entries), took)
	if took >= 5*time.Second {
		t.Errorf("applying 100k entries took %v, want < 5s", took)
	}
	if got := list(t, b); len(got) != len(entries) {
		t.Errorf("listed %d entries, want %d", len(got), len(entries))
	}
}

func BenchmarkApply100k(b *testing.B) {
	be := newBackend(b, false)
	ctx := context.Background()
	entries := entries100k()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		if err := be.Teardown(ctx); err != nil {
			b.Fatal(err)
		}
		if err := be.Setup(ctx); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if err := be.Apply(ctx, entries, nil); err != nil {
			b.Fatal(err)
		}
	}
}
