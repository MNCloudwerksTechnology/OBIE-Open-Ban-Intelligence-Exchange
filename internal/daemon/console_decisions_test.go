package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/console"
	"github.com/MNCloudwerksTechnology/obie/internal/decision"
	"github.com/MNCloudwerksTechnology/obie/internal/enforce"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// Publishers of the console decision test.
const (
	pubAlpha = "12D3KooWAa1phaPeerAa1phaPeerAa1phaPeerAa1phaPeerAa1ph"
	pubBravo = "12D3KooWBravoPeerBravoPeerBravoPeerBravoPeerBravoPee"
)

// putBan stores a ban verdict of publisher on the address or network s,
// issued at issued for an hour.
func putBan(t *testing.T, st *store.DB, publisher, s string, issued time.Time) {
	t.Helper()
	ind, err := indicatorOfRange(netip.MustParsePrefix(s))
	if err != nil {
		t.Fatal(err)
	}
	ev := &obieproto.Event{
		ID:        fmt.Sprintf("0199a1b2-c3d4-7e5f-8a6b-%012d", verdictSeq.Add(1)),
		Spec:      obieproto.Spec,
		Type:      obieproto.TypeVerdict,
		IssuedAt:  obieproto.NewTimestamp(issued),
		Indicator: ind,
		Protocol:  "ssh",
		Evidence:  &obieproto.Evidence{Events: 3, Reason: "password_bruteforce"},
		Verdict:   &obieproto.Verdict{SuggestedAction: obieproto.ActionBan, Confidence: 0.9, TTLSeconds: 3600},
		Publisher: obieproto.Publisher{PeerID: publisher},
	}
	if ok, err := st.Put(ev); err != nil || !ok {
		t.Fatalf("Put(%s) = %v, %v", s, ok, err)
	}
}

// decisionsFixture runs a store, an engine, a gate and a reconciler over
// the dry-run backend in enforce mode: blocks on an IPv4 address, an IPv4
// network and an IPv6 address, and decisions below consensus on an
// address outside and one inside the blocked network.
func decisionsFixture(t *testing.T) *consoleDecisions {
	t.Helper()
	st := newStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	for _, s := range []string{"85.10.20.1/32", "85.10.30.0/24", "2a01:4f8::1/128"} {
		putBan(t, st, pubAlpha, s, now)
		putBan(t, st, pubBravo, s, now)
	}
	putBan(t, st, pubAlpha, "85.10.20.2/32", now)
	putBan(t, st, pubAlpha, "85.10.30.7/32", now)

	trust := config.Trust{Publishers: []config.Publisher{{PeerID: pubAlpha, Name: "alpha", Weight: 1}, {PeerID: pubBravo, Weight: 1}}}
	dec := config.Default().Decision
	dec.Threshold, dec.Quorum = 1.5, 2
	log := slog.New(slog.DiscardHandler)
	engine := decision.New(st, decision.NewPolicy(self, trust, dec), log, decision.Options{})
	gate := enforce.NewGate(config.ModeEnforce, nil, log)
	engine.Subscribe(gate.Handle)
	if err := engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Stop(context.Background()) })
	cfg := config.Enforce{Backend: config.BackendDryRun, MaxEntries: 100, ReconcileInterval: config.Duration(time.Hour)}
	rec := enforce.NewReconciler(gate, enforce.NewDryRun(log), enforce.Options{Backend: "dryrun", MaxEntries: cfg.MaxEntries,
		Interval: time.Hour}, log)
	if err := rec.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	return &consoleDecisions{engine: engine, reconciler: rec, enforce: cfg}
}

func ranges(items []console.DecisionItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Range.String())
	}
	return out
}

// TestConsoleDecisions: the console's decisions list reads the engine's
// decisions with the firewall's last pass, filtered, searched, sorted and
// paged (ADR 0022).
func TestConsoleDecisions(t *testing.T) {
	d := decisionsFixture(t)
	page := d.Decisions(console.DecisionQuery{Sort: "address", Limit: 50})
	if got, want := ranges(page.Items), []string{"85.10.20.1/32", "85.10.20.2/32", "85.10.30.0/24", "85.10.30.7/32", "2a01:4f8::1/128"}; !slices.Equal(got, want) {
		t.Fatalf("decisions by address = %v, want %v", got, want)
	}
	if page.Total != 5 || page.Offset != 0 || !maps.Equal(page.States, map[string]int{"block": 3, "none": 2}) ||
		page.Generation != d.Generation() || page.Generation == 0 {
		t.Errorf("page = %+v", page)
	}
	block, none, inside := page.Items[0], page.Items[1], page.Items[3]
	if block.Key != "ipv4:85.10.20.1" || block.State != "block" || block.Score != 1.8 || block.Contributors != 2 || block.Quorum != 2 ||
		!slices.Equal(block.Categories, []string{"password_bruteforce/ssh"}) || block.Verdicts != 2 || block.Cursor == "" ||
		block.ExpiresAt.IsZero() || block.DecidedAt.IsZero() {
		t.Errorf("block = %+v", block)
	}
	if !block.Firewall.Applied || block.Firewall.Entry != block.Range {
		t.Errorf("block's firewall = %+v, want its own entry", block.Firewall)
	}
	if none.State != "none" || none.Firewall != (console.Coverage{}) {
		t.Errorf("decision below consensus = %+v", none)
	}
	if !inside.Firewall.Applied || inside.Firewall.Entry != netip.MustParsePrefix("85.10.30.0/24") {
		t.Errorf("address inside the blocked network = %+v, want the network's entry", inside.Firewall)
	}

	for _, tc := range []struct {
		name string
		q    console.DecisionQuery
		want []string
	}{
		{"not applied", console.DecisionQuery{Sort: "address", Firewall: console.FirewallNotApplied}, []string{"85.10.20.2/32"}},
		{"applied", console.DecisionQuery{Sort: "address", Firewall: console.FirewallApplied},
			[]string{"85.10.20.1/32", "85.10.30.0/24", "85.10.30.7/32", "2a01:4f8::1/128"}},
		{"a publisher", console.DecisionQuery{Sort: "address", Publisher: pubBravo}, []string{"85.10.20.1/32", "85.10.30.0/24", "2a01:4f8::1/128"}},
		{"a state", console.DecisionQuery{Sort: "address", State: "none"}, []string{"85.10.20.2/32", "85.10.30.7/32"}},
		{"an address finds its network", console.DecisionQuery{Sort: "address", Search: netip.MustParsePrefix("85.10.30.200/32")},
			[]string{"85.10.30.0/24"}},
		{"a network finds what is inside", console.DecisionQuery{Sort: "address", Search: netip.MustParsePrefix("85.10.0.0/16")},
			[]string{"85.10.20.1/32", "85.10.20.2/32", "85.10.30.0/24", "85.10.30.7/32"}},
		{"a category", console.DecisionQuery{Sort: "address", Category: "port_scan/tcp"}, nil},
	} {
		if got := ranges(d.Decisions(tc.q).Items); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	next := d.Decisions(console.DecisionQuery{Sort: "address", Limit: 2, After: page.Items[1].Cursor})
	if got := ranges(next.Items); next.Offset != 2 || !slices.Equal(got, []string{"85.10.30.0/24", "85.10.30.7/32"}) {
		t.Errorf("second page = %v at %d", got, next.Offset)
	}
	if got := d.Categories(); !maps.Equal(got, map[string]int{"password_bruteforce/ssh": 5}) {
		t.Errorf("Categories = %v", got)
	}
}

// TestConsoleExplain: the console's explanation is the engine's, with the
// kept decision, the networks around it and the firewall's last pass.
func TestConsoleExplain(t *testing.T) {
	d := decisionsFixture(t)
	ex, err := d.Explain(netip.MustParsePrefix("85.10.30.7/32"))
	if err != nil {
		t.Fatal(err)
	}
	if ex.Key != "ipv4:85.10.30.7" || ex.State != "none" || ex.Score != 0.9 || ex.Threshold != 1.5 || ex.Quorum != 2 ||
		!ex.Kept || ex.KeptState != "none" || ex.KeptAt.IsZero() || ex.Ruling.Rule != "" || len(ex.Verdicts) != 1 {
		t.Fatalf("explanation = %+v", ex)
	}
	v := ex.Verdicts[0]
	if v.PeerID != pubAlpha || v.Name != "alpha" || !v.Listed || v.Local || v.Weight != 1 || v.Confidence != 0.9 ||
		v.Reason != "password_bruteforce" || v.Protocol != "ssh" || !v.Contributes || v.ExpiresAt.IsZero() {
		t.Errorf("verdict = %+v", v)
	}
	if got := ranges(ex.Around); !slices.Equal(got, []string{"85.10.30.0/24"}) || ex.Around[0].State != "block" {
		t.Errorf("around = %+v", ex.Around)
	}
	if !ex.Firewall.Applied || ex.Firewall.Entry != netip.MustParsePrefix("85.10.30.0/24") {
		t.Errorf("firewall = %+v", ex.Firewall)
	}

	unknown, err := d.Explain(netip.MustParsePrefix("85.10.99.1/32"))
	if err != nil || unknown.State != "none" || unknown.Kept || len(unknown.Verdicts) != 0 || unknown.Firewall.Applied {
		t.Errorf("unknown address = %+v, %v", unknown, err)
	}
	for _, p := range []string{"10.0.0.0/8", "2a01::/16"} {
		if _, err := d.Explain(netip.MustParsePrefix(p)); !errors.Is(err, console.ErrNoIndicator) {
			t.Errorf("Explain(%s) = %v, want ErrNoIndicator", p, err)
		}
	}
	v6, err := d.Explain(netip.MustParsePrefix("2a01:4f8::1/128"))
	if err != nil || v6.State != "block" || !v6.Firewall.Applied || v6.Firewall.Entry != netip.MustParsePrefix("2a01:4f8::1/128") {
		t.Errorf("IPv6 block = %+v, %v", v6, err)
	}
}

// TestConsoleFirewall: the firewall view reads the reconciler's last pass
// and the backend's entries with the decision on each.
func TestConsoleFirewall(t *testing.T) {
	d := decisionsFixture(t)
	fw := d.Firewall()
	if fw.Pass == nil || fw.Pass.Mode != "enforce" || fw.Pass.Entries != 3 || fw.Pass.Seq != 1 || fw.Pass.At.IsZero() ||
		fw.Facts.Backend != "dryrun" || fw.Facts.Blocks != 3 || fw.Facts.Applied != 3 || fw.ExpiryTolerance != enforce.ExpiryTolerance {
		t.Errorf("firewall = %+v, pass %+v", fw, fw.Pass)
	}
	entries, err := d.FirewallEntries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, fmt.Sprintf("%s %s %s %v", e.Range, e.Key, e.State, e.Expires.Equal(e.ExpiresAt)))
	}
	if want := []string{"85.10.20.1/32 ipv4:85.10.20.1 block true", "85.10.30.0/24 cidr:85.10.30.0/24 block true",
		"2a01:4f8::1/128 ipv6:2a01:4f8::1 block true"}; !slices.Equal(got, want) {
		t.Errorf("entries = %v, want %v", got, want)
	}
}

func TestIndicatorOfRange(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"203.0.113.7/32", "ipv4:203.0.113.7"},
		{"2001:db8::1/128", "ipv6:2001:db8::1"},
		{"198.51.100.0/24", "cidr:198.51.100.0/24"},
		{"198.51.100.9/24", "cidr:198.51.100.0/24"},
	} {
		if ind, err := indicatorOfRange(netip.MustParsePrefix(tc.in)); err != nil || ind.Key() != tc.want {
			t.Errorf("indicatorOfRange(%s) = %v, %v", tc.in, ind.Key(), err)
		}
	}
	if _, err := indicatorOfRange(netip.MustParsePrefix("10.0.0.0/8")); err == nil {
		t.Error("a /8 is an indicator")
	}
}
