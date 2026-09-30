package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/console"
	"github.com/MNCloudwerksTechnology/obie/internal/decision"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// newEvent returns an event of publisher on the address or network s,
// issued at issued for ttl: a ban verdict about password_bruteforce over
// ssh, 3 events, no log hash.
func newEvent(t *testing.T, publisher, s string, issued time.Time, ttl time.Duration) *obieproto.Event {
	t.Helper()
	ind, err := indicatorOfRange(netip.MustParsePrefix(s))
	if err != nil {
		t.Fatal(err)
	}
	return &obieproto.Event{
		ID:        fmt.Sprintf("0199a1b2-c3d4-7e5f-8a6b-%012d", verdictSeq.Add(1)),
		Spec:      obieproto.Spec,
		Type:      obieproto.TypeVerdict,
		IssuedAt:  obieproto.NewTimestamp(issued),
		Indicator: ind,
		Protocol:  "ssh",
		Evidence:  &obieproto.Evidence{Events: 3, Reason: "password_bruteforce"},
		Verdict:   &obieproto.Verdict{SuggestedAction: obieproto.ActionBan, Confidence: 0.9, TTLSeconds: int64(ttl / time.Second)},
		Publisher: obieproto.Publisher{PeerID: publisher},
	}
}

func put(t *testing.T, st *store.DB, ev *obieproto.Event) {
	t.Helper()
	if ok, err := st.Put(ev); err != nil || !ok {
		t.Fatalf("Put(%s %s) = %v, %v", ev.Type, ev.ID, ok, err)
	}
}

// verdictsFixture runs a store and an engine holding this node's own
// verdict with evidence and alpha's on 85.10.20.1, alpha's port scan on
// 85.10.20.2, a watch verdict of bravo (weight 0) on 85.10.20.3, this
// node's revoked verdict on 85.10.20.5 and alpha's expired one on
// 85.10.20.4.
func verdictsFixture(t *testing.T) (*consoleVerdicts, map[string]*obieproto.Event) {
	t.Helper()
	st := newStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	evs := map[string]*obieproto.Event{
		"mine":    newEvent(t, self, "85.10.20.1/32", now, 24*time.Hour),
		"alpha":   newEvent(t, pubAlpha, "85.10.20.1/32", now, 24*time.Hour),
		"scan":    newEvent(t, pubAlpha, "85.10.20.2/32", now, 24*time.Hour),
		"watch":   newEvent(t, pubBravo, "85.10.20.3/32", now, 24*time.Hour),
		"expired": newEvent(t, pubAlpha, "85.10.20.4/32", now, time.Hour),
		"revoked": newEvent(t, self, "85.10.20.5/32", now, 24*time.Hour),
	}
	evs["mine"].Evidence.LogHash, evs["mine"].Evidence.Events = "sha256:"+fmt.Sprintf("%064d", 7), 12
	evs["scan"].Protocol, evs["scan"].Evidence.Reason = "tcp", "port_scan"
	evs["watch"].Verdict.SuggestedAction = obieproto.ActionWatch
	for _, ev := range evs {
		put(t, st, ev)
	}
	r := &obieproto.Event{ID: fmt.Sprintf("0199a1b2-c3d4-7e5f-8a6b-%012d", verdictSeq.Add(1)), Spec: obieproto.Spec,
		Type: obieproto.TypeRevoke, IssuedAt: obieproto.NewTimestamp(now), Indicator: evs["revoked"].Indicator,
		Revokes: evs["revoked"].ID, Reason: "false_positive", Publisher: obieproto.Publisher{PeerID: self}}
	put(t, st, r)
	evs["revocation"] = r
	if err := st.Sweep(now.Add(2 * time.Hour)); err != nil {
		t.Fatal(err)
	}

	trust := config.Default().Trust
	trust.Publishers = []config.Publisher{{PeerID: pubAlpha, Name: "alpha", Weight: 1}, {PeerID: pubBravo, Weight: 0}}
	engine := decision.New(st, decision.NewPolicy(self, trust, config.Default().Decision), slog.New(slog.DiscardHandler),
		decision.Options{})
	if err := engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Stop(context.Background()) })
	return &consoleVerdicts{engine: engine, store: st, now: time.Now}, evs
}

// verdictsOf reads the verdicts q selects.
func verdictsOf(t *testing.T, v *consoleVerdicts, q console.VerdictQuery) console.VerdictList {
	t.Helper()
	if q.Limit == 0 {
		q.Limit = 50
	}
	l, err := v.Verdicts(q)
	if err != nil {
		t.Fatalf("Verdicts(%+v): %v", q, err)
	}
	return l
}

// ids returns the event IDs of a page of verdicts.
func ids(l console.VerdictList) []string {
	var out []string
	for _, it := range l.Items {
		out = append(out, it.EventID)
	}
	return out
}

// TestConsoleVerdicts: the verdicts view reads the engine's active
// verdicts with their events and the store's ended ones, filtered, paged
// and counted by state, with weights and decisions (ADR 0023).
func TestConsoleVerdicts(t *testing.T) {
	v, evs := verdictsFixture(t)
	all := verdictsOf(t, v, console.VerdictQuery{State: console.VerdictActive})
	// By key, then by publisher: alpha's peer ID sorts before this node's.
	if want := []string{evs["alpha"].ID, evs["mine"].ID, evs["scan"].ID, evs["watch"].ID}; !slices.Equal(ids(all), want) {
		t.Fatalf("active verdicts = %v, want %v", ids(all), want)
	}
	states := map[string]int{console.VerdictActive: 4, console.VerdictRevoked: 1, console.VerdictExpired: 1}
	if all.Total != 4 || all.Offset != 0 || all.Next != "" || !reflect.DeepEqual(all.States, states) {
		t.Errorf("counts = total %d, offset %d, next %q, states %v", all.Total, all.Offset, all.Next, all.States)
	}
	decided, ok := v.engine.Decision("ipv4:85.10.20.1")
	if !ok {
		t.Fatal("no decision on 85.10.20.1")
	}
	mine := evs["mine"]
	want := console.VerdictItem{Range: netip.MustParsePrefix("85.10.20.1/32"), EventID: mine.ID, Publisher: self, Local: true,
		Weight: config.Default().Trust.LocalWeight, Action: "ban", Confidence: 0.9, Reason: "password_bruteforce", Protocol: "ssh",
		Events: 12, LogHash: mine.Evidence.LogHash, IssuedAt: mine.IssuedAt.Time, ExpiresAt: mine.ExpiresAt(),
		State: console.VerdictActive, Counts: true, Decision: string(decided.State), Cursor: "ipv4:85.10.20.1," + self}
	if !reflect.DeepEqual(all.Items[1], want) {
		t.Errorf("this node's verdict = %+v\nwant %+v", all.Items[1], want)
	}
	if w := all.Items[3]; w.Publisher != pubBravo || w.Weight != 0 || w.Counts || w.Action != "watch" || w.Local {
		t.Errorf("bravo's watch verdict = %+v", w)
	}

	first := verdictsOf(t, v, console.VerdictQuery{State: console.VerdictActive, Limit: 3})
	second := verdictsOf(t, v, console.VerdictQuery{State: console.VerdictActive, Limit: 3, After: first.Next})
	if len(first.Items) != 3 || first.Next != first.Items[2].Cursor || !slices.Equal(ids(second), []string{evs["watch"].ID}) ||
		second.Offset != 3 || second.Next != "" {
		t.Errorf("pages = %v (next %q), %v at %d", ids(first), first.Next, ids(second), second.Offset)
	}

	for _, tc := range []struct {
		name   string
		q      console.VerdictQuery
		want   []string
		states map[string]int
	}{
		{"this node's", console.VerdictQuery{State: console.VerdictActive, Publisher: self}, []string{evs["mine"].ID},
			map[string]int{console.VerdictActive: 1, console.VerdictRevoked: 1, console.VerdictExpired: 0}},
		{"received", console.VerdictQuery{State: console.VerdictActive, Except: self},
			[]string{evs["alpha"].ID, evs["scan"].ID, evs["watch"].ID},
			map[string]int{console.VerdictActive: 3, console.VerdictRevoked: 0, console.VerdictExpired: 1}},
		{"a reason", console.VerdictQuery{State: console.VerdictActive, Category: "port_scan/tcp"}, []string{evs["scan"].ID},
			map[string]int{console.VerdictActive: 1, console.VerdictRevoked: 0, console.VerdictExpired: 0}},
		{"an address", console.VerdictQuery{State: console.VerdictActive, Range: netip.MustParsePrefix("85.10.20.1/32")},
			[]string{evs["alpha"].ID, evs["mine"].ID},
			map[string]int{console.VerdictActive: 2, console.VerdictRevoked: 0, console.VerdictExpired: 0}},
		{"an expired one's address", console.VerdictQuery{State: console.VerdictExpired, Range: netip.MustParsePrefix("85.10.20.4/32")},
			[]string{evs["expired"].ID}, map[string]int{console.VerdictActive: 0, console.VerdictRevoked: 0, console.VerdictExpired: 1}},
	} {
		l := verdictsOf(t, v, tc.q)
		if !slices.Equal(ids(l), tc.want) || !reflect.DeepEqual(l.States, tc.states) {
			t.Errorf("%s: %v, states %v; want %v, %v", tc.name, ids(l), l.States, tc.want, tc.states)
		}
	}

	revoked := verdictsOf(t, v, console.VerdictQuery{State: console.VerdictRevoked, Publisher: self})
	rev := evs["revocation"]
	if len(revoked.Items) != 1 || revoked.Total != 1 {
		t.Fatalf("revoked = %+v", revoked)
	}
	r := revoked.Items[0]
	if r.EventID != evs["revoked"].ID || r.State != console.VerdictRevoked || r.Decision != "" || !r.Local ||
		!reflect.DeepEqual(r.Revocation, &console.VerdictRevocation{ID: rev.ID, Reason: "false_positive", At: rev.IssuedAt.Time}) {
		t.Errorf("revoked verdict = %+v (revocation %+v)", r, r.Revocation)
	}
	expired := verdictsOf(t, v, console.VerdictQuery{State: console.VerdictExpired})
	if len(expired.Items) != 1 || expired.Items[0].EventID != evs["expired"].ID || expired.Items[0].State != console.VerdictExpired ||
		expired.Items[0].Weight != 1 || expired.Items[0].Cursor == "" {
		t.Errorf("expired = %+v", expired)
	}
	if _, err := v.Verdicts(console.VerdictQuery{State: console.VerdictActive, Range: netip.MustParsePrefix("10.0.0.0/8")}); !errors.Is(err, console.ErrNoIndicator) {
		t.Errorf("Verdicts on a /8 = %v, want ErrNoIndicator", err)
	}

	totals, err := v.Totals()
	if err != nil {
		t.Fatal(err)
	}
	wantTotals := console.VerdictTotals{ByPublisher: map[string]console.VerdictCounts{
		self:     {Active: 1, Counting: 1, Revoked: 1},
		pubAlpha: {Active: 2, Counting: 2, Expired: 1},
		pubBravo: {Active: 1},
	}, Retention: store.DefaultEndedRetention, EndedMax: store.DefaultMaxIndicators / 10,
		EndedFull: map[string]bool{console.VerdictRevoked: false, console.VerdictExpired: false}}
	if !reflect.DeepEqual(totals, wantTotals) {
		t.Errorf("totals = %+v\nwant %+v", totals, wantTotals)
	}
	if c := v.Categories(); c["port_scan/tcp"] != 1 || c["password_bruteforce/ssh"] != 2 {
		t.Errorf("categories = %v", c)
	}

	if err := v.store.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Verdicts(console.VerdictQuery{State: console.VerdictActive, Limit: 50}); err == nil {
		t.Error("reading the verdicts of a stopped store succeeded")
	}
	if _, err := v.Totals(); err == nil {
		t.Error("counting the verdicts of a stopped store succeeded")
	}
}
