// Package console serves the node's local web console (ADR 0019): an
// opt-in, read-only view of the node for its operator. It listens on a
// loopback address only, serves only the local users the admin API admits,
// and only to browsers signed in with a token that obied keeps in memory.
//
// The console is a lifecycle subsystem that never fails: when it cannot
// listen, the node runs without it and the reason is logged and shown in
// its status detail. Apply switches it on, off or to another address while
// the node runs.
package console

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/httpserver"
	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
	"github.com/MNCloudwerksTechnology/obie/internal/peercred"
)

// Name is the subsystem name of the console.
const Name = "console"

// Bounds of starting and stopping the console's server on Apply.
const (
	startTimeout = 5 * time.Second
	stopTimeout  = 5 * time.Second
)

// Node is what the console shows about the node. The console runs longer
// than every other subsystem, so the functions must work while the others
// start or stop.
type Node struct {
	Version string
	PeerID  string
	// Fingerprint is the fingerprint of the node's public key.
	Fingerprint string
	// StartedAt is when obied started.
	StartedAt time.Time
	// Mode returns the current node.mode.
	Mode func() string
	// Status reports the status of every subsystem.
	Status func() []lifecycle.Status
	// Facts reads the node's numbers for the overview; nil reads none.
	Facts func() Facts
	// Peers reads the peers the node knows for the peers view; nil reads
	// none (ADR 0021).
	Peers func() PeerSet
	// PeerVerdicts reads up to limit of the active verdicts the node holds
	// from the publisher with peer ID id, after the indicator key after
	// ("" for the first page); nil reads none.
	PeerVerdicts func(id, after string, limit int) (VerdictPage, error)
	// Decisions reads the decisions, explanations and the firewall for
	// the decisions and firewall views; nil reads none (ADR 0022).
	Decisions DecisionSource
	// Verdicts reads the verdicts for the verdicts view; nil reads none
	// (ADR 0023).
	Verdicts VerdictSource
	// Rules reads the overrides, the allow-list and the configuration for
	// their views; nil reads none (ADR 0024).
	Rules RuleSource
	// Activity reads the audit trail for the activity timeline; nil reads
	// none (ADR 0025).
	Activity ActivitySource
	// Actions checks and carries out the operator's actions; nil carries
	// out none (ADR 0026).
	Actions ActionSource
}

// Options configures a Console.
type Options struct {
	// Group is admin.socket_group: root, obied's own user and the members
	// of this group may use the console.
	Group string
	Node  Node
}

// Console is the console subsystem. It implements lifecycle.Subsystem and
// lifecycle.DetailReporter. It is always ready: the node never depends on
// it.
type Console struct {
	log    *slog.Logger
	node   Node
	policy peercred.Policy
	// policyErr is why the policy admits no group; logged once the console
	// serves.
	policyErr error
	lookup    func(net.Conn) (peercred.Cred, error)
	creds     *credentials
	now       func() time.Time
	handler   http.Handler
	// pages are the views in navigation order.
	pages []view
	// signInLimit bounds sign-in attempts; warnLimit the warnings clients
	// can provoke.
	signInLimit, warnLimit *rate.Limiter
	// actionMu makes checking the state an action acts on and carrying it
	// out one step among all browser tabs; outcomes keeps what actions
	// did for the pages the browsers return to (ADR 0026).
	actionMu sync.Mutex
	outcomes *outcomes

	// applyMu serializes Start, Stop and Apply. It is held while a server
	// starts or stops, which may wait for requests in flight; those only
	// take mu.
	applyMu sync.Mutex

	mu      sync.Mutex
	cfg     config.Console
	server  *httpserver.Server // nil while not serving
	err     error              // why the enabled console is not serving
	running bool               // between Start and Stop
}

var _ lifecycle.DetailReporter = (*Console)(nil)

// New returns the console configured by cfg. It serves nothing until
// Start.
func New(cfg config.Console, opts Options, log *slog.Logger) *Console {
	policy, err := peercred.NewPolicy(opts.Group)
	c := &Console{log: log, node: opts.Node, policy: policy, policyErr: err, lookup: peercred.LoopbackTCP,
		creds: newCredentials(), now: time.Now, signInLimit: newSignInLimit(), warnLimit: rate.NewLimiter(1, 10), cfg: cfg,
		outcomes: newOutcomes()}
	c.pages = c.views()
	c.handler = c.routes()
	return c
}

// Name returns the subsystem name.
func (c *Console) Name() string { return Name }

// Start serves the console if it is enabled. It never fails: a console
// that cannot listen is logged and reported by Detail, and the node starts
// without it.
func (c *Console) Start(context.Context) error {
	c.applyMu.Lock()
	defer c.applyMu.Unlock()
	c.mu.Lock()
	c.running = true
	cfg := c.cfg
	c.mu.Unlock()
	if cfg.Enabled {
		_ = c.serve(cfg) // a failure is logged and shown by Detail
	}
	return nil
}

// Stop stops serving, waiting for requests in flight until ctx expires.
func (c *Console) Stop(ctx context.Context) error {
	c.applyMu.Lock()
	defer c.applyMu.Unlock()
	c.mu.Lock()
	c.running = false
	c.mu.Unlock()
	return c.stopServer(ctx)
}

// Apply switches the console to cfg — on, off or to another address —
// without touching anything else; it is how a reload reaches the console.
// A move starts the console at the new address before leaving the old
// one, so a move to a taken port keeps it where it was. An unchanged
// configuration retries a console that failed to listen or stopped
// serving. Before Start and after Stop it only records cfg.
func (c *Console) Apply(cfg config.Console) {
	c.applyMu.Lock()
	defer c.applyMu.Unlock()
	c.mu.Lock()
	prev, old, running, failed := c.cfg, c.server, c.running, c.err != nil
	c.cfg = cfg
	if !cfg.Enabled {
		c.err = nil
	}
	healthy := old != nil && old.Ready() == nil
	if healthy && old.Addr().String() == cfg.Listen {
		c.err = nil // already serving there, e.g. after a failed move is undone
	}
	c.mu.Unlock()
	switch {
	case !running:
		return
	case !cfg.Enabled:
		if old != nil {
			c.retire(old)
			c.log.Info("console stopped")
		}
		return
	case healthy && (old.Addr().String() == cfg.Listen || (cfg == prev && !failed)):
		return
	}
	if c.serve(cfg) && old != nil {
		c.retire(old)
	}
}

// retire stops a server that no longer serves the console. The caller
// holds applyMu.
func (c *Console) retire(srv *httpserver.Server) {
	c.mu.Lock()
	if c.server == srv {
		c.server = nil
	}
	c.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()
	if err := srv.Stop(ctx); err != nil {
		c.log.Warn("stopping the console", "error", err)
	}
}

// serve starts a server for cfg and reports whether it serves. A failure
// is logged and kept for Detail; the current server, if any, keeps
// serving. The caller holds applyMu.
func (c *Console) serve(cfg config.Console) bool {
	srv := httpserver.New(Name, loopbackListener(cfg.Listen), c.handler, c.log, httpserver.ConnContext(withConn))
	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	err := srv.Start(ctx)
	cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.err = err
		if c.server != nil {
			c.log.Error("console not moved; it keeps serving at its old address", "listen", cfg.Listen,
				"url", consoleURL(c.server.Addr()), "error", err)
		} else {
			c.log.Error("console not started; the node runs without it", "listen", cfg.Listen, "error", err)
		}
		return false
	}
	c.server, c.err = srv, nil
	c.log.Info("console serving", "url", consoleURL(srv.Addr()))
	if c.policyErr != nil {
		c.log.Warn("admin socket group not found; only root and obied's own user may use the console",
			"group", c.policy.Group, "error", c.policyErr)
		c.policyErr = nil // once
	}
	return true
}

// stopServer stops the current server, if any. The caller holds applyMu.
func (c *Console) stopServer(ctx context.Context) error {
	c.mu.Lock()
	srv := c.server
	c.server = nil
	c.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Stop(ctx)
}

// loopbackListener listens on addr and refuses to serve on anything but a
// loopback address, whatever the configuration allowed.
func loopbackListener(addr string) httpserver.ListenFunc {
	listen := httpserver.TCP(addr)
	return func(ctx context.Context) (net.Listener, error) {
		ln, err := listen(ctx)
		if err != nil {
			return nil, err
		}
		if a, ok := ln.Addr().(*net.TCPAddr); !ok || !a.IP.IsLoopback() {
			_ = ln.Close()
			return nil, fmt.Errorf("refusing to serve the console on %s, which is not a loopback address", ln.Addr())
		}
		return ln, nil
	}
}

// consoleURL returns the address of the console for a browser.
func consoleURL(addr net.Addr) string { return "http://" + addr.String() + "/" }

// State is the console's current condition.
type State struct {
	// Enabled is console.enabled.
	Enabled bool
	// Listen is console.listen.
	Listen string
	// URL is where the console serves; empty while it does not.
	URL string
	// Err is why the enabled console does not serve at Listen; with a URL,
	// it still serves at an earlier address.
	Err error
}

// State reports the console's current condition.
func (c *Console) State() State {
	c.mu.Lock()
	cfg, srv, err := c.cfg, c.server, c.err
	c.mu.Unlock()
	s := State{Enabled: cfg.Enabled, Listen: cfg.Listen, Err: err}
	if srv != nil {
		if serveErr := srv.Ready(); serveErr != nil {
			s.Err = errors.Unwrap(serveErr) // Ready says "not serving: …" itself
		} else {
			s.URL = consoleURL(srv.Addr())
		}
	}
	return s
}

// Detail summarizes the console for the node status.
func (c *Console) Detail() string {
	s := c.State()
	switch {
	case !s.Enabled:
		return "disabled"
	case s.URL != "" && s.Err != nil:
		return fmt.Sprintf("serving at %s, not at %s: %v", s.URL, s.Listen, s.Err)
	case s.URL != "":
		return "serving at " + s.URL
	case s.Err != nil:
		return "not serving: " + s.Err.Error()
	default:
		return "not started"
	}
}

// actionsOn reports whether console.actions lets the console carry out
// operator actions; a reload switches it through Apply (ADR 0026).
func (c *Console) actionsOn() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cfg.Actions
}

// Addr returns the address the console listens on; nil while it does not
// serve.
func (c *Console) Addr() net.Addr {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.server == nil {
		return nil
	}
	return c.server.Addr()
}
