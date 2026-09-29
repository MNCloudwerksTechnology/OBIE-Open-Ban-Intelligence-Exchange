package selfcheck

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
)

// stampFormat marks the state directory as used by a started node.
func (h *testHost) stampFormat(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(h.stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.stateDir, "FORMAT"), []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCheckNode(t *testing.T) {
	h := newTestHost(t, "")
	assertCheck(t, h.run(t, "node"), Warning, "the node has not been started yet", "sudo systemctl enable --now obied")

	h.stampFormat(t)
	assertCheck(t, h.run(t, "node"), Problem, "the node is not running: nothing answers on "+h.socket,
		"sudo systemctl start obied; if it stops again, see why: sudo journalctl -u obied")

	h.node.status, h.node.statusErr = readyStatus("1.2.3", "observe"), nil
	assertCheck(t, h.run(t, "node"), OK, "obied 1.2.3 is running and ready in observe mode, up 1h2m5s")

	h.node.status = readyStatus("1.2.2", "observe")
	c := h.run(t, "node")
	assertCheck(t, c, Warning, "obied 1.2.2 is running, but the installed obied is 1.2.3", "sudo systemctl restart obied")
	if len(c.Details) != 1 || !strings.Contains(c.Details[0], "running and ready") {
		t.Errorf("details = %q", c.Details)
	}

	h.node.status = readyStatus("1.2.3", "enforce")
	h.node.status.Ready = false
	h.node.status.Subsystems["enforce"] = admin.SubsystemStatus{State: "running", Error: "nftables access denied"}
	c = h.run(t, "node")
	assertCheck(t, c, Problem, "the node is running, but not ready: enforce", "sudo obiectl status")
	if len(c.Details) != 1 || c.Details[0] != "enforce: nftables access denied" {
		t.Errorf("details = %q", c.Details)
	}

	for _, err := range []error{
		fmt.Errorf("%w on admin socket %s: run as root", os.ErrPermission, h.socket),
		&admin.APIError{StatusCode: http.StatusForbidden, Status: "403 Forbidden", Message: "not allowed"},
	} {
		h.node.status, h.node.statusErr = nil, err
		assertCheck(t, h.run(t, "node"), Warning, "cannot ask the node as user root", "sudo obied self-check")
	}
	h.node.statusErr = errors.New("connection reset")
	assertCheck(t, h.run(t, "node"), Problem, "cannot ask the node: connection reset", "sudo systemctl status obied")

	// Without a configuration, a stopped node cannot be told from one
	// that never started.
	if err := os.Remove(h.config); err != nil {
		t.Fatal(err)
	}
	h.node.statusErr = admin.ErrDaemonNotRunning
	assertCheck(t, h.run(t, "node"), Problem, "the node is not running", "sudo systemctl start obied")
}

const (
	friendID  = "12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf"
	partnerID = "12D3KooWGzBX6MWMMz3kHmFfyT3vJxFoy4xQF8NbXN7xBAFhGyvd"
)

// federatedConfig configures two bootstrap peers: the friend, trusted and
// named, at an IP address, and the partner at a DNS name.
var federatedConfig = `mesh:
  bootstrap:
    - /ip4/198.51.100.20/tcp/4001/p2p/` + friendID + `
    - /dns4/obie.partner.example/tcp/4001/p2p/` + partnerID + `
trust:
  publishers:
    - {peer_id: ` + friendID + `, name: friend, weight: 0.8}
`

func TestCheckPeersStandAlone(t *testing.T) {
	h := newTestHost(t, "")
	assertCheck(t, h.run(t, "peers"), Warning, "stand-alone node: no peers are configured", "if that is what you want, there is nothing to do")
}

func TestCheckPeersBeforeStart(t *testing.T) {
	h := newTestHost(t, federatedConfig)
	h.dialer.up["198.51.100.20:4001"] = true
	c := h.run(t, "peers")
	assertCheck(t, c, Warning, "not every peer in mesh.bootstrap answers (2 configured)", "TCP and UDP port 4001")
	if len(c.Details) != 2 || c.Details[0] != "friend answers at /ip4/198.51.100.20/tcp/4001/p2p/"+friendID ||
		!strings.HasPrefix(c.Details[1], partnerID+" does not answer at /dns4/obie.partner.example/tcp/4001/p2p/"+partnerID+": dial tcp4") {
		t.Errorf("details = %q", c.Details)
	}

	h.dialer.up["obie.partner.example:4001"] = true
	assertCheck(t, h.run(t, "peers"), OK, "2 peers in mesh.bootstrap; whether they connect shows once the node runs")

	h.writeConfig(t, "mesh:\n  bootstrap: [/ip6/2001:db8::20/udp/4001/quic-v1/p2p/"+friendID+"]\n")
	c = h.run(t, "peers")
	if c.Status != OK || len(c.Details) != 1 || !strings.Contains(c.Details[0], "has no TCP port to test") {
		t.Errorf("QUIC-only peer = %+v", c)
	}
}

func TestCheckPeersRunning(t *testing.T) {
	h := newTestHost(t, federatedConfig)
	h.node.status, h.node.statusErr = readyStatus("1.2.3", "observe"), nil
	h.dialer.up["198.51.100.20:4001"] = true
	h.node.peers = []admin.PeerResponse{{PeerID: friendID}, {PeerID: "12D3KooWOther"}}
	c := h.run(t, "peers")
	assertCheck(t, c, Warning, "1 of 2 peers in mesh.bootstrap connected; 2 connected in all", "TCP and UDP port 4001")
	if len(c.Details) != 2 || c.Details[0] != "friend is connected" || !strings.Contains(c.Details[1], "is not connected and does not answer") {
		t.Errorf("details = %q", c.Details)
	}

	h.node.peers = nil
	c = h.run(t, "peers")
	assertCheck(t, c, Problem, "0 of 2 peers in mesh.bootstrap connected", "TCP and UDP port 4001", "check the peer ID")
	if !strings.Contains(c.Details[0], "friend is not connected, although it answers at") {
		t.Errorf("details = %q", c.Details)
	}

	h.node.peers = []admin.PeerResponse{{PeerID: friendID}, {PeerID: partnerID}}
	assertCheck(t, h.run(t, "peers"), OK, "2 of 2 peers in mesh.bootstrap connected; 2 connected in all")

	h.node.peersErr = errors.New("boom")
	assertCheck(t, h.run(t, "peers"), Warning, "cannot list the node's peers: boom", "sudo obiectl peers")
}

func TestCheckPeersTrustedOnly(t *testing.T) {
	h := newTestHost(t, "trust:\n  publishers:\n    - {peer_id: "+friendID+", name: friend, weight: 1}\n")
	assertCheck(t, h.run(t, "peers"), OK, "1 peer trusted, none in mesh.bootstrap")
	h.node.status, h.node.statusErr = readyStatus("1.2.3", "observe"), nil
	assertCheck(t, h.run(t, "peers"), Warning, "none of the 1 trusted peer is connected", "add their addresses to mesh.bootstrap")
	h.node.peers = []admin.PeerResponse{{PeerID: friendID}}
	assertCheck(t, h.run(t, "peers"), OK, "1 of 1 trusted peer connected")
}

func TestCheckClock(t *testing.T) {
	h := newTestHost(t, "")
	assertCheck(t, h.run(t, "clock"), OK, "the clock is synchronized (estimated error 12ms)")
	h.clock = ClockState{Synced: false}
	assertCheck(t, h.run(t, "clock"), Warning, "the clock is not synchronized", "sudo timedatectl set-ntp true")
	h.clock = ClockState{Synced: true, MaxError: 20 * time.Second}
	assertCheck(t, h.run(t, "clock"), Warning, "the clock is not synchronized", "sudo timedatectl set-ntp true")
	h.env.Clock = func() (ClockState, error) { return ClockState{}, errors.New("not supported") }
	assertCheck(t, h.run(t, "clock"), Warning, "cannot tell whether the clock is synchronized: not supported", "timedatectl")
	h.env.Now = func() time.Time { return time.Unix(0, 0) }
	assertCheck(t, h.run(t, "clock"), Problem, "the clock shows 1970-01-01T00:00:00Z, which cannot be right", "set the time")
}

func TestKernelClock(t *testing.T) {
	state, err := KernelClock()
	if runtime.GOOS != "linux" {
		if err == nil {
			t.Error("KernelClock succeeded without Linux")
		}
		return
	}
	if err != nil || state.MaxError < 0 {
		t.Errorf("KernelClock = %+v, %v", state, err)
	}
}
