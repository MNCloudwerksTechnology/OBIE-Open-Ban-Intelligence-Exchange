package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/console"
	"github.com/MNCloudwerksTechnology/obie/internal/decision"
	"github.com/MNCloudwerksTechnology/obie/internal/gossip"
	"github.com/MNCloudwerksTechnology/obie/internal/identity"
	"github.com/MNCloudwerksTechnology/obie/internal/mesh"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// TestConsolePeerConversion: the console gets the mesh's peers and their
// events in its own types.
func TestConsolePeerConversion(t *testing.T) {
	at := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	k := mesh.KnownPeer{
		Peer: mesh.Peer{ID: self, Name: "alpha", Addrs: []string{"/ip4/192.0.2.1/tcp/4001"}, ConnectedSince: at,
			Latency: time.Millisecond, TrustWeight: 0.7, Bootstrap: true},
		Connected: true, Publisher: true, LastSeen: at.Add(time.Minute), DialError: "refused", DialFailedAt: at.Add(2 * time.Minute),
		Events: map[gossip.Outcome]int{gossip.Accepted: 5, gossip.Duplicate: 2, gossip.InvalidSignature: 1, gossip.RateLimited: 3},
	}
	want := console.Peer{ID: self, Name: "alpha", Bootstrap: true, Publisher: true, Connected: true,
		Addrs: []string{"/ip4/192.0.2.1/tcp/4001"}, ConnectedSince: at, Latency: time.Millisecond, LastSeen: at.Add(time.Minute),
		DialError: "refused", DialFailedAt: at.Add(2 * time.Minute), Weight: 0.7,
		Events: console.EventCounts{Accepted: 5, Duplicates: 2, Rejected: map[string]int{"invalid_signature": 1, "rate_limited": 3}}}
	if got := consolePeer(&k); !reflect.DeepEqual(got, want) {
		t.Errorf("consolePeer = %+v\nwant          %+v", got, want)
	}
	if got := eventCounts(nil); got.Accepted != 0 || got.Rejected != nil {
		t.Errorf("eventCounts(nil) = %+v", got)
	}
}

// TestConsolePeers reads the peers view's data from a mesh, an engine and
// a store: the configured peers, the verdict counts per publisher and one
// publisher's verdicts, page by page.
func TestConsolePeers(t *testing.T) {
	boot, err := identity.Create(filepath.Join(t.TempDir(), "boot"), false)
	if err != nil {
		t.Fatal(err)
	}
	selfKey, err := identity.Create(filepath.Join(t.TempDir(), "self"), false)
	if err != nil {
		t.Fatal(err)
	}
	st := newStore(t)
	trust := config.Trust{Publishers: []config.Publisher{{PeerID: boot.PeerID(), Name: "boot", Weight: 0.5}}, DefaultWeight: 0.1}
	m, err := mesh.New(selfKey, mesh.Options{Store: st, Trust: trust,
		Bootstrap: []string{"/ip4/192.0.2.1/tcp/4001/p2p/" + boot.PeerID()}}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	engine := decision.New(st, decision.NewPolicy(selfKey.PeerID(), trust, config.Default().Decision), slog.New(slog.DiscardHandler), decision.Options{})
	now := time.Now().UTC().Truncate(time.Second)
	for i := range 3 {
		putVerdict(t, st, boot.PeerID(), fmt.Sprintf("85.10.20.%d", i), now)
	}
	putVerdict(t, st, "12D3KooWOtherPublisher", "85.10.20.9", now)
	if err := engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Stop(context.Background()) })

	p := &consolePeers{mesh: m, engine: engine, store: st, now: time.Now}
	set := p.read()
	if len(set.Peers) != 1 || set.Peers[0].ID != boot.PeerID() || !set.Peers[0].Bootstrap || !set.Peers[0].Publisher ||
		set.Peers[0].Connected || set.Peers[0].Weight != 0.5 || set.DefaultWeight != 0.1 || set.EventWindow != time.Hour {
		t.Errorf("PeerSet = %+v", set)
	}
	if want := map[string]console.VerdictCount{boot.PeerID(): {Held: 3, Counting: 3}, "12D3KooWOtherPublisher": {Held: 1, Counting: 1}}; !maps.Equal(set.Verdicts, want) {
		t.Errorf("Verdicts = %v, want %v", set.Verdicts, want)
	}

	page, err := p.verdicts(boot.PeerID(), "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Verdicts) != 2 || page.Next != "ipv4:85.10.20.1" {
		t.Fatalf("first page = %+v", page)
	}
	v := page.Verdicts[0]
	if want := (console.Verdict{Key: "ipv4:85.10.20.0", Address: "85.10.20.0", Action: "ban", Confidence: 0.9,
		Reason: "password_bruteforce", Protocol: "ssh", ExpiresAt: now.Add(time.Hour)}); !reflect.DeepEqual(v, want) {
		t.Errorf("verdict = %+v\nwant      %+v", v, want)
	}
	page, err = p.verdicts(boot.PeerID(), page.Next, 2)
	if err != nil || len(page.Verdicts) != 1 || page.Verdicts[0].Address != "85.10.20.2" || page.Next != "" {
		t.Errorf("second page = %+v, %v", page, err)
	}

	if err := st.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.verdicts(boot.PeerID(), "", 2); err == nil {
		t.Error("reading the verdicts of a stopped store succeeded")
	}
}

// verdictSeq numbers the events of putVerdict.
var verdictSeq atomic.Int64

// putVerdict stores a ban verdict of publisher on the IPv4 address addr,
// issued at issued for an hour; the store does not check signatures.
func putVerdict(t *testing.T, st *store.DB, publisher, addr string, issued time.Time) {
	t.Helper()
	ev := &obieproto.Event{
		ID:        fmt.Sprintf("0199a1b2-c3d4-7e5f-8a6b-%012d", verdictSeq.Add(1)),
		Spec:      obieproto.Spec,
		Type:      obieproto.TypeVerdict,
		IssuedAt:  obieproto.NewTimestamp(issued),
		Indicator: obieproto.Indicator{Kind: obieproto.KindIPv4, Value: addr, Scope: "/32"},
		Protocol:  "ssh",
		Evidence:  &obieproto.Evidence{Events: 3, Reason: "password_bruteforce"},
		Verdict:   &obieproto.Verdict{SuggestedAction: obieproto.ActionBan, Confidence: 0.9, TTLSeconds: 3600},
		Publisher: obieproto.Publisher{PeerID: publisher},
	}
	if ok, err := st.Put(ev); err != nil || !ok {
		t.Fatalf("Put(%s) = %v, %v", addr, ok, err)
	}
}
