package selfcheck

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/identity"
)

// createKey creates the node key in stateDir and returns its peer ID.
func createKey(t *testing.T, stateDir string) string {
	t.Helper()
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	key, err := identity.Create(stateDir, false)
	if err != nil {
		t.Fatal(err)
	}
	return key.PeerID()
}

// listenUnix listens on a Unix socket at path until the test ends.
func listenUnix(t *testing.T, path string) net.Listener {
	t.Helper()
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

// fakeNode is a node's admin API as the checks see it.
type fakeNode struct {
	status        *admin.StatusResponse
	statusErr     error
	identity      admin.IdentityResponse
	peers         []admin.PeerResponse
	peersErr      error
	allowed       map[string]string // address -> sovereignty note of an allowed decision
	explainErr    error
	indicators    int
	indicatorsErr error
}

func (n *fakeNode) Status(context.Context) (*admin.StatusResponse, error) {
	return n.status, n.statusErr
}

func (n *fakeNode) Identity(context.Context) (*admin.IdentityResponse, error) {
	return &n.identity, nil
}

func (n *fakeNode) Peers(context.Context) (*admin.PeersResponse, error) {
	return &admin.PeersResponse{Peers: n.peers}, n.peersErr
}

func (n *fakeNode) Explain(_ context.Context, indicator string) (*admin.DecisionResponse, error) {
	if n.explainErr != nil {
		return nil, n.explainErr
	}
	if note, ok := n.allowed[indicator]; ok {
		return &admin.DecisionResponse{State: admin.StateAllowed, Reason: note, Sovereignty: &admin.SovereigntyResponse{Note: note}}, nil
	}
	return &admin.DecisionResponse{State: admin.StateNone, Reason: "no active verdicts"}, nil
}

func (n *fakeNode) Indicators(_ context.Context, q admin.IndicatorsQuery) (*admin.IndicatorsResponse, error) {
	if n.indicatorsErr != nil {
		return nil, n.indicatorsErr
	}
	resp := &admin.IndicatorsResponse{}
	if q.Publisher == n.identity.PeerID {
		for range n.indicators {
			resp.Indicators = append(resp.Indicators, admin.IndicatorResponse{})
		}
	}
	return resp, nil
}

// readyStatus is the status of a running, ready node.
func readyStatus(version, mode string) *admin.StatusResponse {
	return &admin.StatusResponse{Version: version, Mode: mode, UptimeSeconds: 3725.4, Ready: true,
		Subsystems: map[string]admin.SubsystemStatus{"mesh": {State: "running", Ready: true}, "enforce": {State: "running", Ready: true}}}
}

// fakeDialer answers for the addresses in up and refuses the others.
type fakeDialer struct{ up map[string]bool }

func (d fakeDialer) dial(_ context.Context, network, address string) (net.Conn, error) {
	if d.up[address] {
		client, server := net.Pipe()
		_ = server.Close()
		return client, nil
	}
	return nil, &net.OpError{Op: "dial", Net: network, Err: errors.New("connection refused")}
}
