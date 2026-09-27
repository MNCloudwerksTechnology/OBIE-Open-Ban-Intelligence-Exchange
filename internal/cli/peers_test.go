package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/identity"
)

func TestWritePeersTable(t *testing.T) {
	since := time.Date(2026, 9, 27, 10, 0, 0, 0, time.FixedZone("CEST", 2*3600))
	peers := []admin.PeerResponse{
		{
			PeerID: "12D3KooWAAA", Name: "seed", Addresses: []string{"/ip4/192.0.2.1/tcp/4001", "/ip4/192.0.2.1/udp/4001/quic-v1"},
			ConnectedSince: since, LatencySeconds: 0.012345, TrustWeight: 0.8, Bootstrap: true,
		},
		{PeerID: "12D3KooWBBB", Addresses: []string{"/ip6/2001:db8::1/tcp/4001"}, ConnectedSince: since},
	}
	var out bytes.Buffer
	if err := writePeersTable(&out, peers); err != nil {
		t.Fatal(err)
	}
	want := `PEER ID      NAME  TRUST  BOOTSTRAP  CONNECTED SINCE       LATENCY  ADDRESSES
12D3KooWAAA  seed  0.8    yes        2026-09-27T08:00:00Z  12.3ms   /ip4/192.0.2.1/tcp/4001,/ip4/192.0.2.1/udp/4001/quic-v1
12D3KooWBBB  -     0      no         2026-09-27T08:00:00Z  -        /ip6/2001:db8::1/tcp/4001
`
	if out.String() != want {
		t.Errorf("table =\n%s\nwant\n%s", out.String(), want)
	}

	out.Reset()
	if err := writePeersTable(&out, nil); err != nil || out.String() != "No peers connected.\n" {
		t.Errorf("no peers: %q, %v", out.String(), err)
	}
}

func TestPeersUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := RunCtl([]string{"--socket", "/nonexistent.sock", "peers", "extra"}, &stdout, &stderr); code != ExitUsage {
		t.Errorf("exit code = %d, want %d (stderr %q)", code, ExitUsage, stderr.String())
	}
}

// TestObiectlPeersAgainstTwoDaemons starts two in-process daemons, B
// bootstrapping to A, and lists the peers of both through obiectl.
func TestObiectlPeersAgainstTwoDaemons(t *testing.T) {
	meshA := freeAddr(t)
	host, port, err := net.SplitHostPort(meshA)
	if err != nil {
		t.Fatal(err)
	}
	a := newTestNodeWith(t, "", fmt.Sprintf("mesh:\n  listen: [/ip4/%s/tcp/%s]\n", host, port))
	keyA, err := identity.Create(a.stateDir, false)
	if err != nil {
		t.Fatal(err)
	}
	b := newTestNodeWith(t, "", fmt.Sprintf(
		"mesh:\n  listen: [/ip4/127.0.0.1/tcp/0]\n  bootstrap: [/ip4/%s/tcp/%s/p2p/%s]\n"+
			"trust:\n  publishers:\n    - {peer_id: %s, name: alpha, weight: 0.6}\n  default_weight: 0.2\n",
		host, port, keyA.PeerID(), keyA.PeerID()))

	var stderrA, stderrB bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	run := func(stderr *bytes.Buffer) func(ctx context.Context, args []string) int {
		return func(ctx context.Context, args []string) int { return runDaemon(ctx, args, &bytes.Buffer{}, stderr) }
	}
	exitA := startDaemon(ctx, t, a, &stderrA, run(&stderrA))
	exitB := startDaemon(ctx, t, b, &stderrB, run(&stderrB))

	peersOf := func(n testNode) []admin.PeerResponse {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := RunCtl([]string{"--socket", n.socket, "peers", "--json"}, &stdout, &stderr); code != ExitOK {
			t.Fatalf("obiectl peers: exit %d, stderr %q", code, stderr.String())
		}
		var resp admin.PeersResponse
		if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
			t.Fatalf("output is not JSON: %v\n%s", err, stdout.String())
		}
		return resp.Peers
	}
	deadline := time.Now().Add(10 * time.Second)
	for len(peersOf(b)) == 0 || len(peersOf(a)) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("B did not connect to A:\nA: %s\nB: %s", stderrA.String(), stderrB.String())
		}
		time.Sleep(20 * time.Millisecond)
	}

	gotB := peersOf(b)
	if len(gotB) != 1 || gotB[0].PeerID != keyA.PeerID() || gotB[0].Name != "alpha" || gotB[0].TrustWeight != 0.6 ||
		!gotB[0].Bootstrap || len(gotB[0].Addresses) != 1 || gotB[0].Addresses[0] != "/ip4/"+host+"/tcp/"+port ||
		time.Since(gotB[0].ConnectedSince) > time.Minute {
		t.Errorf("B's peers = %+v", gotB)
	}
	if gotA := peersOf(a); len(gotA) != 1 || gotA[0].Name != "" || gotA[0].TrustWeight != 0 || gotA[0].Bootstrap {
		t.Errorf("A's peers = %+v, want B unnamed with A's default weight 0", gotA)
	}

	var table, stderr bytes.Buffer
	if code := RunCtl([]string{"--socket", b.socket, "peers"}, &table, &stderr); code != ExitOK ||
		!strings.Contains(table.String(), keyA.PeerID()+"  alpha  0.6    yes") {
		t.Errorf("obiectl peers: exit %d, stderr %q, table:\n%s", code, stderr.String(), table.String())
	}

	cancel()
	for name, exit := range map[string]<-chan int{"A": exitA, "B": exitB} {
		if code := waitExit(t, exit, &stderrA); code != ExitOK {
			t.Errorf("obied %s exit code = %d", name, code)
		}
	}
}
