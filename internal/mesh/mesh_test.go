package mesh

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/identity"
	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
)

// Compile-time checks of the lifecycle interfaces the daemon relies on.
var (
	_ lifecycle.Subsystem        = (*Mesh)(nil)
	_ lifecycle.ReadinessChecker = (*Mesh)(nil)
	_ lifecycle.DetailReporter   = (*Mesh)(nil)
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// newStore returns a started in-memory store that is stopped when the test
// ends.
func newStore(t *testing.T) *store.DB {
	t.Helper()
	db := store.NewMemory(discardLogger(), store.Options{})
	if err := db.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return db
}

func newIdentity(t *testing.T) *identity.Key {
	t.Helper()
	key, err := identity.Create(filepath.Join(t.TempDir(), "state"), false)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestHostKey(t *testing.T) {
	id := newIdentity(t)
	key, err := newHostKey(id)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := peer.IDFromPrivateKey(key)
	if err != nil || pid.String() != id.PeerID() {
		t.Fatalf("peer ID = %s, %v; want %s", pid, err, id.PeerID())
	}

	msg := []byte("hello")
	sig, err := key.Sign(msg)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := key.GetPublic().Verify(msg, sig); !ok || err != nil {
		t.Errorf("signature does not verify: %v", err)
	}

	raw1, err1 := key.Raw()
	raw2, err2 := key.Raw()
	if err1 != nil || err2 != nil || len(raw1) < 32 || !bytes.Equal(raw1, raw2) {
		t.Errorf("Raw() = %x, %v / %x, %v; want stable secret material", raw1, err1, raw2, err2)
	}
	if bytes.Contains(raw1, id.PublicKey()) {
		t.Error("Raw() contains the public key; it must not be the key encoding")
	}

	same, _ := newHostKey(id)
	other, _ := newHostKey(newIdentity(t))
	if !key.Equals(same) || key.Equals(other) || key.Equals(key.GetPublic()) {
		t.Error("Equals must compare private keys by their public key")
	}
}

func TestBackoff(t *testing.T) {
	b := newBackoff(time.Second, 5*time.Second)
	b.randN = func(n int64) int64 { return n - 1 } // upper end of the jitter range
	var got []time.Duration
	for range 5 {
		got = append(got, b.Next().Round(time.Millisecond))
	}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second, 5 * time.Second}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("delays = %v, want %v", got, want)
		}
	}

	b.Reset()
	b.randN = func(int64) int64 { return 0 } // lower end
	if d := b.Next(); d != 500*time.Millisecond {
		t.Errorf("after Reset, Next() = %v with minimal jitter, want 500ms", d)
	}

	b = newBackoff(time.Second, 5*time.Second)
	for range 100 {
		if d := b.Next(); d < 500*time.Millisecond || d >= 5*time.Second {
			t.Fatalf("jittered delay %v outside [0.5s, 5s)", d)
		}
	}
}

func TestNew(t *testing.T) {
	self := newIdentity(t)
	other := newIdentity(t)
	st := newStore(t)
	m, err := New(self, Options{Store: st, Bootstrap: []string{
		"/ip4/192.0.2.1/tcp/4001/p2p/" + other.PeerID(),
		"/ip4/192.0.2.1/udp/4001/quic-v1/p2p/" + other.PeerID(),
		"/ip4/192.0.2.2/tcp/4001/p2p/" + self.PeerID(),
	}}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if len(m.bootstrap) != 1 || m.bootstrap[0].ID.String() != other.PeerID() || len(m.bootstrap[0].Addrs) != 2 {
		t.Errorf("bootstrap = %v, want %s with both addresses and without this node", m.bootstrap, other.PeerID())
	}
	if got := m.PeerCounts(); got != (PeerCounts{Configured: 1}) {
		t.Errorf("PeerCounts() before Start = %+v, want 1 configured", got)
	}
	if m.opts.InitialBackoff != DefaultInitialBackoff || m.opts.MaxBackoff != DefaultMaxBackoff {
		t.Errorf("backoff = %v..%v, want the defaults", m.opts.InitialBackoff, m.opts.MaxBackoff)
	}

	for name, opts := range map[string]Options{
		"listen":    {Store: st, Listen: []string{"/ip4/1.2.3/tcp/1"}},
		"bootstrap": {Store: st, Bootstrap: []string{"/ip4/192.0.2.1/tcp/4001"}},
		"publisher": {Store: st, Trust: config.Trust{Publishers: []config.Publisher{{PeerID: "nope", Name: "x"}}}},
		"store":     {},
	} {
		if _, err := New(self, opts, discardLogger()); err == nil {
			t.Errorf("%s: New accepted an invalid value", name)
		}
	}
}

func TestStartFailsWhenNothingCanBeBound(t *testing.T) {
	// 192.0.2.0/24 (TEST-NET-1) is not assigned to any local interface.
	m, err := New(newIdentity(t), Options{Store: newStore(t), Listen: []string{"/ip4/192.0.2.1/tcp/0"}}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err == nil {
		_ = m.Stop(context.Background())
		t.Fatal("Start succeeded without any bound listen address")
	}
	if m.Ready() == nil {
		t.Error("Ready() = nil after a failed start")
	}
}

// TestStartWithoutListenAddresses checks that an empty mesh.listen keeps
// libp2p's default listen addresses.
func TestStartWithoutListenAddresses(t *testing.T) {
	m := startMesh(t, newIdentity(t), Options{})
	if err := m.Ready(); err != nil {
		t.Errorf("Ready() = %v; want the default listen addresses bound", err)
	}
}

func TestReadyWithoutPeers(t *testing.T) {
	m := startMesh(t, newIdentity(t), Options{Listen: []string{"/ip4/127.0.0.1/tcp/0"}})
	if err := m.Ready(); err != nil {
		t.Errorf("Ready() = %v; zero peers must not make the mesh unready", err)
	}
	if d := m.Detail(); !strings.HasPrefix(d, "degraded: 0 peers connected") {
		t.Errorf("Detail() = %q, want a degraded report", d)
	}
	if len(m.Peers()) != 0 {
		t.Errorf("Peers() = %v, want none", m.Peers())
	}
}

// startMesh starts a mesh, with a new store unless opts has one, and stops
// it at the end of the test.
func startMesh(t *testing.T, id identity.Identity, opts Options) *Mesh {
	t.Helper()
	if opts.Store == nil {
		opts.Store = newStore(t)
	}
	m, err := New(id, opts, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Stop(context.Background()); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	return m
}

// listenAddr returns m's bound listen address using protocol code.
func listenAddr(t *testing.T, m *Mesh, code int) string {
	t.Helper()
	for _, a := range m.ListenAddrs() {
		if _, err := a.ValueForProtocol(code); err == nil {
			return a.String()
		}
	}
	t.Fatalf("no listen address with protocol %d in %v", code, m.ListenAddrs())
	return ""
}

func peerIDs(m *Mesh) map[string]Peer {
	out := make(map[string]Peer)
	for _, p := range m.Peers() {
		out[p.ID] = p
	}
	return out
}

func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %v waiting for %s", timeout, what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestBootstrapMesh runs three in-process nodes: B dials A over TCP, C over
// QUIC. When A stops, B and C redial with backoff and reconnect once A is
// back on the same addresses with the same identity.
func TestBootstrapMesh(t *testing.T) {
	idA, idB, idC := newIdentity(t), newIdentity(t), newIdentity(t)
	const backoffMax = 400 * time.Millisecond
	tuning := func(o Options) Options {
		o.InitialBackoff, o.MaxBackoff = 50*time.Millisecond, backoffMax
		o.DialTimeout, o.PingInterval = time.Second, 100*time.Millisecond
		return o
	}

	a := startMesh(t, idA, tuning(Options{Listen: []string{"/ip4/127.0.0.1/tcp/0", "/ip4/127.0.0.1/udp/0/quic-v1"}}))
	tcpA, quicA := listenAddr(t, a, ma.P_TCP), listenAddr(t, a, ma.P_QUIC_V1)
	trust := config.Trust{
		Publishers:    []config.Publisher{{PeerID: idA.PeerID(), Name: "alpha", Weight: 0.7}},
		DefaultWeight: 0.1,
	}
	b := startMesh(t, idB, tuning(Options{
		Listen: []string{"/ip4/127.0.0.1/tcp/0"}, Bootstrap: []string{tcpA + "/p2p/" + idA.PeerID()}, Trust: trust,
	}))
	c := startMesh(t, idC, tuning(Options{
		Listen: []string{"/ip4/127.0.0.1/udp/0/quic-v1"}, Bootstrap: []string{quicA + "/p2p/" + idA.PeerID()}, Trust: trust,
	}))

	connected := func() bool {
		pa, pb, pc := peerIDs(a), peerIDs(b), peerIDs(c)
		_, ab := pa[idB.PeerID()]
		_, ac := pa[idC.PeerID()]
		_, ba := pb[idA.PeerID()]
		_, ca := pc[idA.PeerID()]
		return ab && ac && ba && ca
	}
	waitFor(t, 10*time.Second, "B and C to connect to A", connected)

	waitFor(t, 5*time.Second, "a latency measurement", func() bool { return peerIDs(b)[idA.PeerID()].Latency > 0 })
	got := peerIDs(b)[idA.PeerID()]
	if got.Name != "alpha" || got.TrustWeight != 0.7 || !got.Bootstrap || got.ConnectedSince.IsZero() ||
		len(got.Addrs) != 1 || got.Addrs[0] != tcpA {
		t.Errorf("B's view of A = %+v", got)
	}
	if got := peerIDs(a)[idB.PeerID()]; got.Name != "" || got.TrustWeight != 0 || got.Bootstrap {
		t.Errorf("A's view of B = %+v, want an unnamed non-bootstrap peer with A's default weight", got)
	}
	if d := b.Detail(); d != "1 peers connected (1/1 bootstrap peers)" {
		t.Errorf("B Detail() = %q", d)
	}
	if got := b.PeerCounts(); got != (PeerCounts{Connected: 1, Bootstrap: 1, Configured: 1}) {
		t.Errorf("B PeerCounts() = %+v", got)
	}
	if got := a.PeerCounts(); got != (PeerCounts{Connected: 2}) {
		t.Errorf("A PeerCounts() = %+v, want 2 connected, none configured", got)
	}
	// A reload replaces names and weights.
	if err := b.SetTrust(config.Trust{Publishers: []config.Publisher{{PeerID: idA.PeerID(), Name: "alpha2", Weight: 0.2}}}); err != nil {
		t.Fatal(err)
	}
	if got := peerIDs(b)[idA.PeerID()]; got.Name != "alpha2" || got.TrustWeight != 0.2 {
		t.Errorf("B's view of A after SetTrust = %+v", got)
	}
	if err := b.SetTrust(config.Trust{Publishers: []config.Publisher{{PeerID: "nope"}}}); err == nil {
		t.Error("SetTrust with an invalid peer ID succeeded")
	}
	if got := peerIDs(b)[idA.PeerID()]; got.Name != "alpha2" {
		t.Errorf("a failed SetTrust changed the trust: %+v", got)
	}

	if err := a.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "B and C to notice A is gone", func() bool {
		return len(b.Peers()) == 0 && len(c.Peers()) == 0
	})
	if d := b.Detail(); !strings.HasPrefix(d, "degraded:") || b.Ready() != nil {
		t.Errorf("B without peers: Detail() = %q, Ready() = %v; want degraded but ready", d, b.Ready())
	}
	if got := b.PeerCounts(); got != (PeerCounts{Configured: 1}) {
		t.Errorf("B PeerCounts() without peers = %+v", got)
	}
	// Let a few dials fail so that B and C back off to the cap.
	time.Sleep(2 * backoffMax)

	restarted := time.Now()
	startMesh(t, idA, tuning(Options{Listen: []string{tcpA, quicA}}))
	// Within one capped backoff (plus jitter-free slack for the dial).
	waitFor(t, backoffMax+3*time.Second, "B and C to reconnect to the restarted A", connected2(b, c, idA.PeerID()))
	t.Logf("reconnected %v after A restarted", time.Since(restarted).Round(time.Millisecond))
}

func connected2(b, c *Mesh, a string) func() bool {
	return func() bool {
		_, ba := peerIDs(b)[a]
		_, ca := peerIDs(c)[a]
		return ba && ca
	}
}
