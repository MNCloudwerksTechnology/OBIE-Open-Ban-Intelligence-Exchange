package selfcheck

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	ma "github.com/multiformats/go-multiaddr"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
)

// Timeouts of the calls to the node and of the peer reachability test.
const (
	callTimeout = 5 * time.Second
	dialTimeout = 3 * time.Second
)

// NodeClient is what the checks ask a running node; *admin.Client
// implements it.
type NodeClient interface {
	Status(ctx context.Context) (*admin.StatusResponse, error)
	Identity(ctx context.Context) (*admin.IdentityResponse, error)
	Peers(ctx context.Context) (*admin.PeersResponse, error)
	Explain(ctx context.Context, indicator string) (*admin.DecisionResponse, error)
	Indicators(ctx context.Context, q admin.IndicatorsQuery) (*admin.IndicatorsResponse, error)
}

var _ NodeClient = (*admin.Client)(nil)

// queryNode asks the node for its status, over the socket of the
// configuration, or the default one if the configuration is unusable.
func (r *run) queryNode() {
	socket := config.Default().Admin.Socket
	if r.cfg != nil {
		socket = r.cfg.Admin.Socket
	}
	r.socket = socket
	r.node = r.env.Node(socket)
	ctx, cancel := context.WithTimeout(r.ctx, callTimeout)
	defer cancel()
	r.status, r.statusErr = r.node.Status(ctx)
}

// running reports whether the node answered.
func (r *run) running() bool { return r.statusErr == nil }

// deniedByNode reports whether err means that this user may not use the
// admin socket: the kernel refused the connection, or obied the user.
func deniedByNode(err error) bool {
	var apiErr *admin.APIError
	return errors.Is(err, fs.ErrPermission) || errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden
}

// checkNode: the node runs and every subsystem is ready. Before the first
// start, a stopped node is expected.
func (r *run) checkNode() Check {
	const id, name = "node", "Node"
	s, err := r.status, r.statusErr
	switch {
	case err == nil && s.Ready:
		findings := []finding{ok(fmt.Sprintf("obied %s is running and ready in %s mode, up %s",
			s.Version, s.Mode, s.Uptime().Truncate(time.Second)))}
		if s.Version != r.env.Version {
			findings = append(findings, warn(fmt.Sprintf("obied %s is running, but the installed obied is %s", s.Version, r.env.Version),
				"restart the node to run the installed version: sudo systemctl restart obied"))
		}
		return newCheck(id, name, findings...)
	case err == nil:
		c := newCheck(id, name, problem("the node is running, but not ready: "+strings.Join(notReady(s), ", "),
			"see what the node says: sudo obiectl status; sudo journalctl -u obied -n 50"))
		for _, sub := range notReady(s) {
			if e := s.Subsystems[sub].Error; e != "" {
				c.Details = append(c.Details, sub+": "+e)
			}
		}
		return c
	case errors.Is(err, admin.ErrDaemonNotRunning):
		if started, known := r.startedIfConfigured(); known && !started {
			return newCheck(id, name, warn("the node has not been started yet", "start it: sudo systemctl enable --now obied"))
		}
		return newCheck(id, name, problem(fmt.Sprintf("the node is not running: nothing answers on %s", r.socket),
			"start it: sudo systemctl start obied; if it stops again, see why: sudo journalctl -u obied -n 20"))
	case deniedByNode(err):
		return newCheck(id, name, warn(fmt.Sprintf("cannot ask the node as user %s: %v", r.me(), err), asRoot))
	default:
		return newCheck(id, name, problem(fmt.Sprintf("cannot ask the node: %v", err),
			"check that it runs: sudo systemctl status obied; sudo journalctl -u obied -n 20"))
	}
}

// startedIfConfigured is started, unknown without a configuration.
func (r *run) startedIfConfigured() (started, known bool) {
	if r.cfg == nil {
		return false, false
	}
	return r.started()
}

// notReady lists the subsystems that are not ready, sorted.
func notReady(s *admin.StatusResponse) []string {
	var names []string
	for name, sub := range s.Subsystems {
		if !sub.Ready {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// Next steps of the peer check.
const (
	nextReach  = "check that the peer's node runs and that TCP and UDP port 4001 are open between both servers; see documentation/operations/troubleshooting.md#no-peers"
	nextPeerID = "check the peer ID at the end of the address: it must be the one sudo obiectl identity shows on that node; " +
		"the node redials every few minutes, and sudo systemctl restart obied dials at once"
)

// peerProbe is what the peer check found out about a bootstrap peer.
type peerProbe struct {
	addr, peerID, label string
	// tested is false for an address without a TCP port; err is why the
	// peer did not answer.
	tested bool
	err    error
}

// checkPeers: peers are configured, answer at their address and, while the
// node runs, are connected. A node without peers is a warning: it may be
// on purpose.
func (r *run) checkPeers() Check {
	const id, name = "peers", "Peers"
	if r.cfg == nil {
		return notChecked(id, name)
	}
	bootstrap, publishers := r.cfg.Mesh.Bootstrap, r.cfg.Trust.Publishers
	if len(bootstrap) == 0 && len(publishers) == 0 {
		return newCheck(id, name, warn("stand-alone node: no peers are configured, so the node acts only on what this server detects",
			"if that is what you want, there is nothing to do; to exchange verdicts with other nodes, run sudo obied setup again "+
				"or see documentation/operations/federation.md"))
	}
	connected := map[string]bool{}
	if r.running() {
		ctx, cancel := context.WithTimeout(r.ctx, callTimeout)
		peers, err := r.node.Peers(ctx)
		cancel()
		if err != nil {
			return newCheck(id, name, warn(fmt.Sprintf("cannot list the node's peers: %v", err), "list them yourself: sudo obiectl peers"))
		}
		for _, p := range peers.Peers {
			connected[p.PeerID] = true
		}
	}
	if len(bootstrap) == 0 {
		return newCheck(id, name, r.publishersOnly(publishers, connected))
	}
	probes := r.probePeers(bootstrap, publishers)
	findings := []finding{r.peersSummary(probes, connected)}
	for _, p := range probes {
		findings = append(findings, r.peerFinding(p, connected[p.peerID]))
	}
	return newCheck(id, name, findings...)
}

// publishersOnly judges trusted peers when mesh.bootstrap lists none: they
// have to connect to this node.
func (r *run) publishersOnly(publishers []config.Publisher, connected map[string]bool) finding {
	if !r.running() {
		return ok(fmt.Sprintf("%s trusted, none in mesh.bootstrap: they connect to this node, which shows once it runs",
			plural(len(publishers), "peer")))
	}
	n := 0
	for _, p := range publishers {
		if connected[p.PeerID] {
			n++
		}
	}
	if n == 0 {
		return warn(fmt.Sprintf("none of the %s is connected, and mesh.bootstrap lists none to dial", plural(len(publishers), "trusted peer")),
			"add their addresses to mesh.bootstrap and restart, or ask their operators to list this node")
	}
	return ok(fmt.Sprintf("%d of %s connected", n, plural(len(publishers), "trusted peer")))
}

// probePeers tests every bootstrap peer's address at once.
func (r *run) probePeers(bootstrap []string, publishers []config.Publisher) []peerProbe {
	names := make(map[string]string, len(publishers))
	for _, p := range publishers {
		names[p.PeerID] = p.Name
	}
	probes := make([]peerProbe, len(bootstrap))
	var wg sync.WaitGroup
	for i, addr := range bootstrap {
		p := peerProbe{addr: addr}
		if m, err := ma.NewMultiaddr(addr); err == nil {
			if _, last := ma.SplitLast(m); last != nil {
				p.peerID = last.Value()
			}
		}
		p.label = names[p.peerID]
		if p.label == "" {
			p.label = p.peerID
		}
		probes[i] = p
		wg.Add(1)
		go func() {
			defer wg.Done()
			probes[i].tested, probes[i].err = r.reach(addr)
		}()
	}
	wg.Wait()
	return probes
}

// reach connects to the TCP address of a peer's multiaddr; tested is false
// if it has none (QUIC only).
func (r *run) reach(addr string) (tested bool, err error) {
	m, err := ma.NewMultiaddr(addr)
	if err != nil {
		return true, err
	}
	network, host, port := "tcp", "", ""
	for _, c := range m {
		switch c.Code() {
		case ma.P_IP4, ma.P_IP6, ma.P_DNS:
			host = c.Value()
		case ma.P_DNS4:
			network, host = "tcp4", c.Value()
		case ma.P_DNS6:
			network, host = "tcp6", c.Value()
		case ma.P_TCP:
			port = c.Value()
		}
	}
	if host == "" || port == "" {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(r.ctx, dialTimeout)
	defer cancel()
	conn, err := r.env.Dial(ctx, network, net.JoinHostPort(host, port))
	if err != nil {
		return true, err
	}
	_ = conn.Close()
	return true, nil
}

// peersSummary sums the bootstrap peers up, with the check's worst status.
func (r *run) peersSummary(probes []peerProbe, connected map[string]bool) finding {
	if !r.running() {
		for _, p := range probes {
			if p.err != nil {
				return warn(fmt.Sprintf("not every peer in mesh.bootstrap answers (%d configured)", len(probes)), nextReach)
			}
		}
		return ok(fmt.Sprintf("%s in mesh.bootstrap; whether they connect shows once the node runs", plural(len(probes), "peer")))
	}
	n := 0
	for _, p := range probes {
		if connected[p.peerID] {
			n++
		}
	}
	text := fmt.Sprintf("%d of %s in mesh.bootstrap connected; %d connected in all", n, plural(len(probes), "peer"), len(connected))
	switch n {
	case len(probes):
		return ok(text)
	case 0:
		return problem(text, nextReach)
	default:
		return warn(text, nextReach)
	}
}

// peerFinding describes one bootstrap peer.
func (r *run) peerFinding(p peerProbe, connected bool) finding {
	answer := "answers at " + p.addr
	switch {
	case !p.tested:
		answer = "has no TCP port to test at " + p.addr
	case p.err != nil:
		answer = fmt.Sprintf("does not answer at %s: %v", p.addr, p.err)
	}
	switch {
	case r.running() && connected:
		return ok(p.label + " is connected")
	case r.running() && p.err == nil:
		return warn(fmt.Sprintf("%s is not connected, although it %s", p.label, answer), nextPeerID)
	case r.running():
		return warn(fmt.Sprintf("%s is not connected and %s", p.label, answer), nextReach)
	case p.err != nil:
		return warn(fmt.Sprintf("%s %s", p.label, answer), nextReach)
	default:
		return ok(fmt.Sprintf("%s %s", p.label, answer))
	}
}
