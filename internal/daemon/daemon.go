// Package daemon wires the subsystems of obied together and runs them for
// the lifetime of the process.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"path/filepath"
	"time"

	ma "github.com/multiformats/go-multiaddr"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/audit"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/console"
	"github.com/MNCloudwerksTechnology/obie/internal/decision"
	"github.com/MNCloudwerksTechnology/obie/internal/enforce"
	"github.com/MNCloudwerksTechnology/obie/internal/enforce/nft"
	"github.com/MNCloudwerksTechnology/obie/internal/identity"
	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
	"github.com/MNCloudwerksTechnology/obie/internal/logging"
	"github.com/MNCloudwerksTechnology/obie/internal/mesh"
	"github.com/MNCloudwerksTechnology/obie/internal/ops"
	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
	"github.com/MNCloudwerksTechnology/obie/internal/statedir"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/internal/verdicts"
	"github.com/MNCloudwerksTechnology/obie/internal/version"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// Logger names.
const (
	// Component is the logger name of the daemon itself.
	Component            = "obied"
	sovereigntyComponent = "allowlist"
	enforceComponent     = "enforce"
	reloadComponent      = "reload"
)

// waitForShutdown blocks until ctx is canceled, reloading the configuration
// with rl whenever reload fires (the audit log is reopened first).
func waitForShutdown(ctx context.Context, reload <-chan struct{}, rl *reloader) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-reload:
			_ = rl.reload(ctx) // a failure is logged and keeps the running configuration
		}
	}
}

// Options configures Run beyond the configuration file.
type Options struct {
	// Reload receives a value whenever the configuration is to be reloaded
	// (SIGHUP); nil never reloads.
	Reload <-chan struct{}
	// LoadConfig reads the configuration file again; required with Reload.
	LoadConfig func() (*config.Config, error)
	// Env is how the allow-list learns the host's addresses; the zero
	// value uses the real host.
	Env sovereignty.Env
	// Testing holds the hooks of in-process multi-node tests; production
	// leaves it zero.
	Testing Testing
}

// Testing holds the hooks that let tests run several complete nodes in one
// process. None of them is reachable from the configuration file.
type Testing struct {
	// AllowDocumentationRanges lets documentation addresses (e.g.
	// 203.0.113.0/24) be reported, relayed, decided on and enforced, so
	// that tests never block a real host.
	AllowDocumentationRanges bool
	// MetricsListen replaces metrics.listen, e.g. with "127.0.0.1:0" for a
	// port chosen by the OS, which the configuration refuses.
	MetricsListen string
	// ConsoleListen replaces console.listen, at start and on every reload,
	// e.g. with "127.0.0.1:0".
	ConsoleListen string
	// Console, if set, is called with the node's web console before it
	// starts, e.g. to read the address it listens on.
	Console func(*console.Console)
	// NFTablesNetNS is a file descriptor of the network namespace the
	// nftables backend programs; 0 for the process's own.
	NFTablesNetNS int
	// Started is called once every subsystem runs, with the addresses they
	// bound.
	Started func(Endpoints)
	// Store, if set, is called with the node's store before it opens, e.g.
	// to subscribe to its changes or to read its cache use.
	Store func(*store.DB)
}

// Endpoints are the addresses a running node bound.
type Endpoints struct {
	// Mesh are the multiaddrs the mesh listens on, with the bound ports
	// and without /p2p.
	Mesh []string
	// Metrics is the host:port of the ops endpoints (/metrics).
	Metrics string
}

// Run checks the format of node.state_dir (ADR 0017), loads the node
// identity from it, generating it on the first start, builds the
// allow-list, then starts the subsystems configured by cfg and blocks
// until ctx is canceled, reloading the configuration
// whenever opts.Reload fires; then it shuts the subsystems down in reverse
// order within node.shutdown_timeout. It returns nil after a clean
// shutdown, also when ctx is canceled during startup, and an error when a
// subsystem fails to start or to stop in time, and when the state
// directory (e.g. one of a newer format), the identity or the allow-list
// cannot be loaded. A failed reload keeps the running
// configuration.
func Run(ctx context.Context, cfg *config.Config, logs *logging.Factory, opts Options) error {
	log := logs.Logger(Component)
	startedAt := time.Now()

	if err := prepareStateDir(cfg.Node.StateDir, log); err != nil {
		return err
	}
	id, err := loadIdentity(cfg.Node.StateDir, log)
	if err != nil {
		return err
	}
	env := opts.Env
	env.OmitDocumentationRanges = env.OmitDocumentationRanges || opts.Testing.AllowDocumentationRanges
	allow, err := sovereignty.Build(ctx, cfg, env, logs.Logger(sovereigntyComponent))
	if err != nil {
		return fmt.Errorf("allow-list: %w", err)
	}
	log.Info("allow-list loaded", "entries", len(allow.Entries()))

	db := store.New(filepath.Join(cfg.Node.StateDir, "db"), logs.Logger(store.Name), store.Options{
		MaxIndicators: cfg.Store.MaxIndicators,
		Self:          id.PeerID(),
	})
	if opts.Testing.Store != nil {
		opts.Testing.Store(db)
	}
	// go-libp2p's own logs join ours; below warn they are too chatty.
	mesh.UseLogHandler(logs.Logger("libp2p").Handler(), slog.LevelWarn)
	m, err := mesh.New(id, mesh.Options{
		Listen:    cfg.Mesh.Listen,
		Bootstrap: cfg.Mesh.Bootstrap,
		Trust:     cfg.Trust,
		UserAgent: "obied/" + version.Version,
		Store:     db,
		RateLimit: cfg.Mesh.RateLimit,

		AllowDocumentationRanges: opts.Testing.AllowDocumentationRanges,
	}, logs.Logger(mesh.Name))
	if err != nil {
		return fmt.Errorf("mesh: %w", err)
	}

	mgr := lifecycle.New(logs.Logger("lifecycle"), lifecycle.Options{StopTimeout: cfg.Node.ShutdownTimeout.Std()})
	// The web console starts first and stops last, so that it can show the
	// node starting and shutting down; it never fails to start (ADR 0019).
	var gate *enforce.Gate
	con := console.New(consoleConfig(cfg.Console, opts.Testing), console.Options{
		Group: cfg.Admin.SocketGroup,
		Node: console.Node{Version: version.Version, PeerID: id.PeerID(), Status: mgr.Status,
			Mode: func() string { return string(gate.Mode()) }},
	}, logs.Logger(console.Name))
	if opts.Testing.Console != nil {
		opts.Testing.Console(con)
	}
	mgr.Register(con)
	// The store starts next and stops before it: every other subsystem may
	// use it.
	mgr.Register(db)
	engine := decision.New(db, decision.NewPolicy(id.PeerID(), cfg.Trust, cfg.Decision), logs.Logger(decision.Name),
		decision.Options{Allowlist: allow})
	// The gate is the only path to enforcement; it subscribes before the
	// engine starts, so it sees the initial blocks. The reconciler applies
	// them; it only notifies it once the engine runs.
	backend, err := newEnforcer(cfg.Enforce, opts.Testing.NFTablesNetNS, logs.Logger(enforceComponent))
	if err != nil {
		return err
	}
	var reconciler *enforce.Reconciler
	gate = enforce.NewGate(cfg.Node.Mode, func() { reconciler.Trigger() }, logs.Logger(enforceComponent))
	reconciler = enforce.NewReconciler(gate, backend, enforce.Options{
		Backend:    string(cfg.Enforce.Backend),
		MaxEntries: cfg.Enforce.MaxEntries,
		Interval:   cfg.Enforce.ReconcileInterval.Std(),
		Allowlist:  engine.Allowlist,
	}, logs.Logger(enforceComponent))
	engine.Subscribe(gate.Handle)
	// The audit log opens before the engine starts and closes after
	// everything that writes to it has stopped.
	auditLog := newAuditLog(cfg.Audit.Path, gate, logs.Logger(audit.Name))
	if auditLog != nil {
		mgr.Register(auditLog)
		subscribeAudit(engine, auditLog)
	}
	mgr.Register(engine)
	mgr.Register(reconciler)
	metricsListen := cfg.Metrics.Listen
	if opts.Testing.MetricsListen != "" {
		metricsListen = opts.Testing.MetricsListen
	}
	opsServer := ops.New(metricsListen, mgr.Status, logs.Logger(ops.Name))
	mgr.Register(opsServer)
	mgr.Register(m)
	allowlist, err := parsePrefixes(cfg.Allowlist.CIDRs)
	if err != nil {
		return fmt.Errorf("allowlist: %w", err)
	}
	reporter := verdicts.New(verdicts.Options{
		Store:      db,
		Publisher:  m,
		Signer:     id,
		DefaultTTL: cfg.Decision.DefaultTTL.Std(),
		MaxTTL:     cfg.Decision.MaxTTL.Std(),
		Allowlist:  allowlist,

		AllowDocumentationRanges: opts.Testing.AllowDocumentationRanges,
	}, logs.Logger(verdicts.Name))
	mgr.Register(admin.New(cfg.Admin.Socket, cfg.Admin.SocketGroup, admin.Info{
		Version:   version.Version,
		Mode:      func() string { return string(gate.Mode()) },
		StartedAt: startedAt,
		Identity:  admin.NewIdentityResponse(id),
		Status:    mgr.Status,
		Peers:     func() []admin.PeerResponse { return peerResponses(m.Peers()) },
		Explain: func(ind obieproto.Indicator) (admin.DecisionResponse, error) {
			d, err := engine.Explain(ind)
			if err != nil {
				return admin.DecisionResponse{}, err
			}
			return explanationResponse(d), nil
		},
		Decisions: func(state string) []admin.DecisionResponse {
			ds := engine.Decisions(decision.State(state))
			out := make([]admin.DecisionResponse, len(ds))
			for i := range ds {
				out[i] = decisionResponse(&ds[i])
			}
			return out
		},
		Overrides: storeOverrides{store: db, now: time.Now, audit: auditLog},
		Enforced: func(ctx context.Context) ([]admin.EnforcedEntry, error) {
			return enforcedEntries(ctx, reconciler)
		},
		Verdicts: auditedVerdicts{VerdictService: reporter, audit: auditLog},
		Console:  consoleService{console: con},
	}, logs.Logger(admin.Name)))

	if err := mgr.Start(ctx); err != nil {
		var startErr *lifecycle.StartError
		if ctx.Err() != nil && errors.As(err, &startErr) && errors.Is(startErr.Err, context.Canceled) {
			log.Info("shutdown requested during startup")
			if startErr.Rollback != nil {
				return fmt.Errorf("shutdown: %w", startErr.Rollback)
			}
			return nil
		}
		return fmt.Errorf("startup failed: %w", err)
	}
	log.Info("obied started", "version", version.Version, "mode", cfg.Node.Mode, "peer_id", id.PeerID(),
		"admin_socket", cfg.Admin.Socket, "metrics_listen", opsServer.Addr().String(), "audit_path", cfg.Audit.Path,
		"console", con.Detail())

	if opts.Testing.Started != nil {
		opts.Testing.Started(Endpoints{Mesh: multiaddrStrings(m.ListenAddrs()), Metrics: opsServer.Addr().String()})
	}

	rl := &reloader{running: cfg, self: id.PeerID(), load: opts.LoadConfig, env: env,
		engine: engine, gate: gate, mesh: m, audit: auditLog, log: logs.Logger(reloadComponent),
		console: func(c config.Console) { con.Apply(consoleConfig(c, opts.Testing)) }}
	waitForShutdown(ctx, opts.Reload, rl)
	log.Info("shutdown requested", "timeout", cfg.Node.ShutdownTimeout.String())
	if err := mgr.Stop(context.WithoutCancel(ctx)); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	log.Info("shutdown complete")
	return nil
}

// prepareStateDir checks the format of stateDir, stamping a new or legacy
// directory, and refuses one written by a newer obied (ADR 0017).
func prepareStateDir(stateDir string, log *slog.Logger) error {
	previous, err := statedir.Prepare(stateDir, version.Version)
	if err != nil {
		return fmt.Errorf("state directory: %w", err)
	}
	if previous == 0 {
		log.Info("state directory format recorded", "state_dir", stateDir, "format", statedir.Version)
	}
	return nil
}

// loadIdentity loads the node key from stateDir, or generates it on the
// first start.
func loadIdentity(stateDir string, log *slog.Logger) (identity.Identity, error) {
	key, created, err := identity.LoadOrCreate(stateDir)
	if err != nil {
		return nil, fmt.Errorf("node identity: %w", err)
	}
	msg := "node identity loaded"
	if created {
		msg = "node identity generated"
	}
	log.Info(msg, "peer_id", key.PeerID(), "fingerprint", identity.Fingerprint(key.PublicKey()),
		"key_file", identity.Path(stateDir))
	return key, nil
}

// newEnforcer returns the enforcement backend configured in
// enforce.backend.
func newEnforcer(cfg config.Enforce, netns int, log *slog.Logger) (enforce.Enforcer, error) {
	switch cfg.Backend {
	case config.BackendDryRun:
		return enforce.NewDryRun(log), nil
	case config.BackendNFTables:
		return nft.New(nft.Options{Forward: cfg.NFTables.Forward, NetNS: netns}, log), nil
	default:
		return nil, fmt.Errorf("enforce.backend %q is not available in this build; use %q", cfg.Backend, config.BackendDryRun)
	}
}

// enforcedEntries lists the entries the backend applies as admin API wire
// types.
func enforcedEntries(ctx context.Context, rec *enforce.Reconciler) ([]admin.EnforcedEntry, error) {
	entries, err := rec.Entries(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]admin.EnforcedEntry, len(entries))
	for i, e := range entries {
		out[i] = admin.EnforcedEntry{Prefix: e.Prefix.String(), ExpiresAt: e.Expires.UTC()}
	}
	return out, nil
}

// multiaddrStrings returns the text form of addrs.
func multiaddrStrings(addrs []ma.Multiaddr) []string {
	out := make([]string, len(addrs))
	for i, a := range addrs {
		out[i] = a.String()
	}
	return out
}

// parsePrefixes parses CIDR ranges that configuration validation accepted.
func parsePrefixes(cidrs []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, len(cidrs))
	for i, c := range cidrs {
		p, err := netip.ParsePrefix(c)
		if err != nil {
			return nil, err
		}
		out[i] = p
	}
	return out, nil
}

// peerResponses converts the mesh's peer view into admin API wire types,
// keeping the admin package free of go-libp2p types.
func peerResponses(peers []mesh.Peer) []admin.PeerResponse {
	out := make([]admin.PeerResponse, len(peers))
	for i, p := range peers {
		out[i] = admin.PeerResponse{
			PeerID:         p.ID,
			Name:           p.Name,
			Addresses:      p.Addrs,
			ConnectedSince: p.ConnectedSince.UTC(),
			LatencySeconds: p.Latency.Seconds(),
			TrustWeight:    p.TrustWeight,
			Bootstrap:      p.Bootstrap,
		}
	}
	return out
}

// decisionResponse converts a decision into its admin API summary.
func decisionResponse(d *decision.Decision) admin.DecisionResponse {
	resp := admin.DecisionResponse{
		Indicator:      d.Indicator,
		State:          string(d.State),
		Score:          d.Score,
		Threshold:      d.Threshold,
		Contributors:   d.Contributors,
		Quorum:         d.Quorum,
		LocalAutoblock: d.Autoblock,
		Reason:         d.Reason,
		EvaluatedAt:    d.EvaluatedAt.UTC(),
	}
	if !d.ExpiresAt.IsZero() {
		expires := d.ExpiresAt.UTC()
		resp.ExpiresAt = &expires
	}
	return resp
}

// explanationResponse converts a decision into its admin API explanation
// with every publisher's contribution.
func explanationResponse(d decision.Decision) admin.DecisionResponse {
	resp := decisionResponse(&d)
	resp.Publishers = make([]admin.ContributionResponse, len(d.Publishers))
	for i, c := range d.Publishers {
		resp.Publishers[i] = admin.ContributionResponse{
			PeerID:      c.PeerID,
			Name:        c.Name,
			Local:       c.Local,
			EventID:     c.EventID,
			Action:      c.Action,
			Weight:      c.Weight,
			Confidence:  c.Confidence,
			Score:       c.Score,
			Contributes: c.Contributes,
			IssuedAt:    c.IssuedAt.UTC(),
			ExpiresAt:   c.ExpiresAt.UTC(),
		}
	}
	resp.Sovereignty = sovereigntyResponse(&d)
	return resp
}
