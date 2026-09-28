package e2e

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/daemon"
	"github.com/MNCloudwerksTechnology/obie/internal/identity"
	"github.com/MNCloudwerksTechnology/obie/internal/logging"
)

// Timing bounds of the acceptance criteria.
const (
	// propagationBound is how long an event may take to reach every node.
	propagationBound = 5 * time.Second
	// enforcementBound is how long a decision may take to reach the
	// enforcement backend.
	enforcementBound = 10 * time.Second
	// quietPeriod is how long a node must keep not blocking after the
	// verdict that must not block reached it: longer than the reconciler's
	// debounce (250 ms) with a wide margin.
	quietPeriod = 1500 * time.Millisecond
	// suiteBound is the time the whole end-to-end test may take.
	suiteBound = 60 * time.Second

	// startBound bounds the start and the stop of one node.
	startBound = 30 * time.Second
	// meshBound bounds the connection of the nodes to their peers.
	meshBound = 10 * time.Second
	// pollInterval is how often the polling helpers check.
	pollInterval = 20 * time.Millisecond
	// requestTimeout bounds one admin API or metrics request.
	requestTimeout = 5 * time.Second
)

// Decision settings of every node: two publishers at confidence 0.95
// (score 1.9) block, two at 0.8 (1.6) do not, one alone never does.
const (
	threshold = 1.8
	quorum    = 2
	trusted   = 1.0
)

// allowlisted is allowlist.cidrs of every node.
const allowlisted = "198.51.100.0/28"

// node is one obied of the test cluster: a complete daemon.Run with its
// own configuration file, state directory, admin socket and log file.
type node struct {
	name   string
	dir    string
	config string
	logs   string
	peerID string
	client *admin.Client
	// hooks are the node's test hooks besides those every node gets.
	hooks daemon.Testing

	endpoints daemon.Endpoints
	cancel    context.CancelFunc
	done      chan error
	logFile   *os.File
}

// clusterOptions configures a cluster.
type clusterOptions struct {
	// backend is enforce.backend of every node.
	backend config.Backend
	// hooks returns extra test hooks of the named node; nil for none.
	hooks func(name string) daemon.Testing
}

// cluster is a set of running nodes. The first three names trust each
// other with weight 1.0; any further node is trusted by nobody (the
// default weight 0) and trusts nobody.
type cluster struct {
	nodes  []*node
	byName map[string]*node
	opts   clusterOptions
	warmUp int
}

// newCluster starts a node per name in order, each bootstrapping to the
// nodes started before it, waits until every node is connected to every
// other and until events published on each node reach all others.
func newCluster(t *testing.T, opts clusterOptions, names ...string) *cluster {
	t.Helper()
	c := &cluster{byName: map[string]*node{}, opts: opts}
	for _, name := range names {
		n := newNode(t, name)
		if opts.hooks != nil {
			n.hooks = opts.hooks(name)
		}
		c.nodes = append(c.nodes, n)
		c.byName[name] = n
	}
	t.Cleanup(func() { c.stopAll(t) })
	for i, n := range c.nodes {
		c.writeConfig(t, n, c.nodes[:i])
		n.start(t)
	}
	for _, n := range c.nodes {
		c.awaitPeers(t, n, len(c.nodes)-1)
	}
	for _, n := range c.nodes {
		c.awaitMesh(t, n, c.others(n)...)
	}
	return c
}

// newNode creates the node's directory and identity key, like `obied
// keygen`, so that the peer IDs are known before any configuration is
// written.
func newNode(t *testing.T, name string) *node {
	t.Helper()
	// Unix socket paths are limited to about 100 bytes; t.TempDir can exceed that.
	dir, err := os.MkdirTemp("", "obie-e2e-"+name+"-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("node %s kept in %s for inspection", name, dir)
			return
		}
		_ = os.RemoveAll(dir)
	})
	key, err := identity.Create(filepath.Join(dir, "state"), false)
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "obie.sock")
	return &node{
		name: name, dir: dir, peerID: key.PeerID(),
		config: filepath.Join(dir, "obie.yaml"), logs: filepath.Join(dir, "obied.log"),
		client: admin.NewClient(socket),
	}
}

// trustedNames are the nodes that trust each other.
func (c *cluster) trustedNames() []string {
	return []string{c.nodes[0].name, c.nodes[1].name, c.nodes[2].name}
}

// node returns the node called name.
func (c *cluster) node(name string) *node { return c.byName[name] }

// others returns every node but n.
func (c *cluster) others(n *node) []*node {
	var out []*node
	for _, o := range c.nodes {
		if o != n {
			out = append(out, o)
		}
	}
	return out
}

// writeConfig writes the configuration file of n, bootstrapping to the
// running nodes in bootstrap.
func (c *cluster) writeConfig(t *testing.T, n *node, bootstrap []*node) {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "node:\n  state_dir: %s\n  mode: enforce\n  shutdown_timeout: 10s\n", filepath.Join(n.dir, "state"))
	fmt.Fprintf(&b, "admin:\n  socket: %s\n  socket_group: obie-e2e-no-such-group\n", filepath.Join(n.dir, "obie.sock"))
	b.WriteString("mesh:\n  listen: [/ip4/127.0.0.1/tcp/0]\n  bootstrap:\n")
	for _, peer := range bootstrap {
		for _, addr := range peer.endpoints.Mesh {
			fmt.Fprintf(&b, "    - %s/p2p/%s\n", addr, peer.peerID)
		}
	}
	b.WriteString("trust:\n  default_weight: 0\n  local_weight: 1.0\n  publishers:\n")
	if trusts := c.trustedNames(); slices.Contains(trusts, n.name) {
		for _, name := range trusts {
			if peer := c.node(name); peer != n {
				fmt.Fprintf(&b, "    - {peer_id: %s, name: %s, weight: %.1f}\n", peer.peerID, peer.name, trusted)
			}
		}
	}
	fmt.Fprintf(&b, "decision:\n  threshold: %.1f\n  quorum: %d\n  local_autoblock: true\n", threshold, quorum)
	fmt.Fprintf(&b, "allowlist:\n  cidrs: [%s]\n", allowlisted)
	fmt.Fprintf(&b, "enforce:\n  backend: %s\n", c.opts.backend)
	// metrics.listen needs a fixed port; the test hook replaces it with
	// one the OS chooses.
	b.WriteString("metrics:\n  listen: 127.0.0.1:9464\n")
	fmt.Fprintf(&b, "audit:\n  path: %s\n", filepath.Join(n.dir, "audit.log"))
	b.WriteString("log:\n  level: debug\n")
	if err := os.WriteFile(n.config, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

// start loads the node's configuration file and runs obied until stop,
// returning once every subsystem runs.
func (n *node) start(t *testing.T) {
	t.Helper()
	cfg, err := config.Load(n.config)
	if err != nil {
		t.Fatalf("node %s: %v", n.name, err)
	}
	n.logFile, err = os.OpenFile(n.logs, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	level, err := logging.ParseLevel(cfg.Log.Level)
	if err != nil {
		t.Fatal(err)
	}
	hooks := n.hooks
	hooks.AllowDocumentationRanges = true
	hooks.MetricsListen = "127.0.0.1:0"
	started := make(chan daemon.Endpoints, 1)
	hooks.Started = func(e daemon.Endpoints) { started <- e }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	n.cancel, n.done = cancel, done
	logs := logging.New(n.logFile, level)
	go func() {
		done <- daemon.Run(ctx, cfg, logs, daemon.Options{
			LoadConfig: func() (*config.Config, error) { return config.Load(n.config) },
			Testing:    hooks,
		})
	}()
	select {
	case n.endpoints = <-started:
		t.Logf("node %s (%s) started: mesh %v, metrics %s", n.name, n.peerID, n.endpoints.Mesh, n.endpoints.Metrics)
	case err := <-n.done:
		n.cancel = nil
		t.Fatalf("node %s stopped during startup: %v\n%s", n.name, err, n.logTail())
	case <-time.After(startBound):
		t.Fatalf("node %s did not start within %s\n%s", n.name, startBound, n.logTail())
	}
}

// stop shuts the node down and checks that it stopped cleanly.
func (n *node) stop(t *testing.T) {
	t.Helper()
	if n.cancel == nil {
		return
	}
	n.cancel()
	n.cancel = nil
	select {
	case err := <-n.done:
		if err != nil {
			t.Errorf("node %s: shutdown: %v", n.name, err)
		}
	case <-time.After(startBound):
		t.Errorf("node %s did not stop within %s", n.name, startBound)
	}
	_ = n.logFile.Close()
}

// stopAll stops every node, quoting the end of their logs if t failed.
func (c *cluster) stopAll(t *testing.T) {
	for _, n := range slices.Backward(c.nodes) {
		n.stop(t)
	}
	if t.Failed() {
		for _, n := range c.nodes {
			t.Logf("node %s (%s):\n%s", n.name, n.peerID, n.logTail())
		}
	}
}

// logTail returns the last lines of the node's log.
func (n *node) logTail() string {
	const lines = 40
	data, err := os.ReadFile(n.logs)
	if err != nil {
		return err.Error()
	}
	all := strings.Split(strings.TrimSpace(string(data)), "\n")
	return strings.Join(all[max(0, len(all)-lines):], "\n")
}

// awaitPeers waits until n is connected to at least want peers.
func (c *cluster) awaitPeers(t *testing.T, n *node, want int) {
	t.Helper()
	within(t, meshBound, fmt.Sprintf("node %s connected to %d peers", n.name, want), func(ctx context.Context) error {
		resp, err := n.client.Peers(ctx)
		if err != nil {
			return err
		}
		if len(resp.Peers) < want {
			return fmt.Errorf("%d peers", len(resp.Peers))
		}
		return nil
	})
}

// awaitMesh waits until events of from reach every node in to. GossipSub
// forwards only to peers whose subscription it has seen; an event published
// before may never arrive. Each probe is a watch verdict on a fresh
// documentation address, which blocks nowhere.
func (c *cluster) awaitMesh(t *testing.T, from *node, to ...*node) {
	t.Helper()
	const probes, probeBound = 10, time.Second
	var err error
	for range probes {
		c.warmUp++
		ip := fmt.Sprintf("192.0.2.%d", c.warmUp)
		from.report(t, ip, 0.1, "watch")
		err = poll(probeBound, func(ctx context.Context) error {
			for _, n := range to {
				if err := n.hasVerdict(ctx, ip, from, "watch"); err != nil {
					return err
				}
			}
			return nil
		})
		if err == nil {
			return
		}
	}
	t.Fatalf("events of node %s do not reach the others: %v", from.name, err)
}

// within polls check until it succeeds and fails t unless that happens
// within bound. It logs and returns how long it took.
func within(t *testing.T, bound time.Duration, what string, check func(ctx context.Context) error) time.Duration {
	t.Helper()
	start := time.Now()
	if err := poll(bound, check); err != nil {
		t.Fatalf("%s: not within %s: %v", what, bound, err)
	}
	took := time.Since(start)
	t.Logf("%s after %s (bound %s)", what, took.Round(time.Millisecond), bound)
	return took
}

// holds checks that check keeps succeeding for d and fails t otherwise.
func holds(t *testing.T, d time.Duration, what string, check func(ctx context.Context) error) {
	t.Helper()
	deadline := time.Now().Add(d)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		if err := call(check); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		if time.Now().After(deadline) {
			return
		}
		<-ticker.C
	}
}

// poll calls check every pollInterval until it succeeds or bound elapsed;
// it returns the last error then.
func poll(bound time.Duration, check func(ctx context.Context) error) error {
	deadline := time.Now().Add(bound)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		err := call(check)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		<-ticker.C
	}
}

// call runs check with a request timeout.
func call(check func(ctx context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	return check(ctx)
}

// report sends a report on ip to the node's admin API, like
// `obiectl report`, and fails t if it is not accepted.
func (n *node) report(t *testing.T, ip string, confidence float64, action string) *admin.ReportResponse {
	t.Helper()
	var resp *admin.ReportResponse
	err := call(func(ctx context.Context) error {
		var err error
		resp, err = n.client.Report(ctx, reportRequest(ip, confidence, action))
		return err
	})
	if err != nil {
		t.Fatalf("node %s: report %s: %v", n.name, ip, err)
	}
	return resp
}

func reportRequest(ip string, confidence float64, action string) admin.ReportRequest {
	return admin.ReportRequest{
		IP: ip, Protocol: "ssh", Reason: "password_bruteforce", Events: 12,
		EvidenceLines: []string{"sshd[4711]: Failed password for root from " + ip + " port 52814 ssh2"},
		Confidence:    &confidence, Action: action,
	}
}

// revoke revokes the node's verdict on ip.
func (n *node) revoke(t *testing.T, ip string) {
	t.Helper()
	err := call(func(ctx context.Context) error {
		resp, err := n.client.Revoke(ctx, admin.RevocationRequest{Indicator: ip, Reason: "false_positive"})
		if err == nil && len(resp.Revocations) != 1 {
			err = fmt.Errorf("%d revocations, want 1", len(resp.Revocations))
		}
		return err
	})
	if err != nil {
		t.Fatalf("node %s: revoke %s: %v", n.name, ip, err)
	}
}

// explain returns the node's explained decision on ip.
func (n *node) explain(ctx context.Context, ip string) (*admin.DecisionResponse, error) {
	return n.client.Explain(ctx, ip)
}

// contribution returns the verdict of publisher in d.
func contribution(d *admin.DecisionResponse, publisher *node) (admin.ContributionResponse, bool) {
	for _, c := range d.Publishers {
		if c.PeerID == publisher.peerID {
			return c, true
		}
	}
	return admin.ContributionResponse{}, false
}

// hasVerdict checks that the node holds an active verdict of publisher
// on ip with the action.
func (n *node) hasVerdict(ctx context.Context, ip string, publisher *node, action string) error {
	d, err := n.explain(ctx, ip)
	if err != nil {
		return err
	}
	c, ok := contribution(d, publisher)
	if !ok || c.Action != action {
		return fmt.Errorf("node %s holds no %s verdict of %s on %s", n.name, action, publisher.name, ip)
	}
	return nil
}

// enforced reports whether the node's enforcement backend applies a block
// of ip.
func (n *node) enforced(ctx context.Context, ip string) (bool, error) {
	resp, err := n.client.Enforced(ctx)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(resp.Entries, func(e admin.EnforcedEntry) bool { return e.Prefix == ip+"/32" }), nil
}

// blocks checks that the node decided to block ip and its backend applies
// the block.
func (n *node) blocks(ctx context.Context, ip string) error {
	d, err := n.explain(ctx, ip)
	if err != nil {
		return err
	}
	if d.State != admin.StateBlock {
		return fmt.Errorf("node %s: decision on %s is %s (score %.2f, %d contributors): %s", n.name, ip, d.State, d.Score, d.Contributors, d.Reason)
	}
	on, err := n.enforced(ctx, ip)
	if err != nil {
		return err
	}
	if !on {
		return fmt.Errorf("node %s decided to block %s but the backend does not apply it", n.name, ip)
	}
	return nil
}

// notBlocking checks that the node neither decided to block ip nor
// applies a block of it.
func (n *node) notBlocking(ctx context.Context, ip string) error {
	d, err := n.explain(ctx, ip)
	if err != nil {
		return err
	}
	if d.State == admin.StateBlock {
		return fmt.Errorf("node %s blocks %s (score %.2f, %d contributors): %s", n.name, ip, d.Score, d.Contributors, d.Reason)
	}
	on, err := n.enforced(ctx, ip)
	if err != nil {
		return err
	}
	if on {
		return fmt.Errorf("node %s's backend applies a block of %s", n.name, ip)
	}
	return nil
}

// metric returns the value of the sample name (with labels, e.g.
// `obie_events_received_total{outcome="invalid_signature"}`) on the node's
// /metrics. The nodes share one process and so one Prometheus registry:
// counters add up over all nodes.
func (n *node) metric(ctx context.Context, sample string) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+n.endpoints.Metrics+"/metrics", nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("GET /metrics: %s", resp.Status)
	}
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if value, ok := strings.CutPrefix(sc.Text(), sample+" "); ok {
			return strconv.ParseFloat(value, 64)
		}
	}
	if err := sc.Err(); err != nil {
		return 0, err
	}
	return 0, errors.New("no sample " + sample)
}

// near reports whether a and b are equal up to float rounding.
func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }
