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
	// Mode returns the current node.mode.
	Mode func() string
	// Status reports the status of every subsystem.
	Status func() []lifecycle.Status
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
	log     *slog.Logger
	node    Node
	policy  peercred.Policy
	lookup  func(net.Conn) (peercred.Cred, error)
	creds   *credentials
	now     func() time.Time
	handler http.Handler
	// signInLimit bounds sign-in attempts.
	signInLimit *rate.Limiter

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
	if err != nil {
		log.Warn("admin socket group not found; only root and obied's own user may use the console",
			"group", opts.Group, "error", err)
	}
	c := &Console{log: log, node: opts.Node, policy: policy, lookup: peercred.LoopbackTCP, creds: newCredentials(),
		now: time.Now, signInLimit: newSignInLimit(), cfg: cfg}
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
		c.serve(cfg)
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
// An unchanged configuration only retries a console that failed to
// listen. Before Start and after Stop it only records cfg.
func (c *Console) Apply(cfg config.Console) {
	c.applyMu.Lock()
	defer c.applyMu.Unlock()
	c.mu.Lock()
	prev, serving, running := c.cfg, c.server != nil, c.running
	c.cfg = cfg
	if !cfg.Enabled {
		c.err = nil
	}
	c.mu.Unlock()
	if !running || (cfg == prev && (serving || !cfg.Enabled)) {
		return
	}
	if serving {
		ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
		err := c.stopServer(ctx)
		cancel()
		if err != nil {
			c.log.Warn("stopping the console", "error", err)
		}
		c.log.Info("console stopped", "enabled", cfg.Enabled)
	}
	if cfg.Enabled {
		c.serve(cfg)
	}
}

// serve starts a server for cfg; a failure is logged and kept for Detail.
// The caller holds applyMu.
func (c *Console) serve(cfg config.Console) {
	srv := httpserver.New(Name, loopbackListener(cfg.Listen), c.handler, c.log, httpserver.ConnContext(withConn))
	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	err := srv.Start(ctx)
	cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.server, c.err = nil, err
		c.log.Error("console not started; the node runs without it", "listen", cfg.Listen, "error", err)
		return
	}
	c.server, c.err = srv, nil
	c.log.Info("console serving", "url", consoleURL(srv.Addr()))
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
	// Err is why the enabled console is not serving.
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
			s.Err = serveErr
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
	case s.URL != "":
		return "serving at " + s.URL
	case s.Err != nil:
		return "not serving: " + s.Err.Error()
	default:
		return "not started"
	}
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
