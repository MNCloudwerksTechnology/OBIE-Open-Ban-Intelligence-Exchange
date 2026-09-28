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

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/decision"
	"github.com/MNCloudwerksTechnology/obie/internal/enforce"
	"github.com/MNCloudwerksTechnology/obie/internal/identity"
	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
	"github.com/MNCloudwerksTechnology/obie/internal/logging"
	"github.com/MNCloudwerksTechnology/obie/internal/mesh"
	"github.com/MNCloudwerksTechnology/obie/internal/ops"
	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
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
// with rl whenever reload fires.
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
}

// Run loads the node identity from node.state_dir, generating it on the
// first start, builds the allow-list, then starts the subsystems configured
// by cfg and blocks until ctx is canceled, reloading the configuration
// whenever opts.Reload fires; then it shuts the subsystems down in reverse
// order within node.shutdown_timeout. It returns nil after a clean
// shutdown, also when ctx is canceled during startup, and an error when a
// subsystem fails to start or to stop in time, and when the identity or the
// allow-list cannot be loaded. A failed reload keeps the running
// configuration.
func Run(ctx context.Context, cfg *config.Config, logs *logging.Factory, opts Options) error {
	log := logs.Logger(Component)
	startedAt := time.Now()

	id, err := loadIdentity(cfg.Node.StateDir, log)
	if err != nil {
		return err
	}
	allow, err := sovereignty.Build(ctx, cfg, opts.Env, logs.Logger(sovereigntyComponent))
	if err != nil {
		return fmt.Errorf("allow-list: %w", err)
	}
	log.Info("allow-list loaded", "entries", len(allow.Entries()))

	db := store.New(filepath.Join(cfg.Node.StateDir, "db"), logs.Logger(store.Name), store.Options{})
	// go-libp2p's own logs join ours; below warn they are too chatty.
	mesh.UseLogHandler(logs.Logger("libp2p").Handler(), slog.LevelWarn)
	m, err := mesh.New(id, mesh.Options{
		Listen:    cfg.Mesh.Listen,
		Bootstrap: cfg.Mesh.Bootstrap,
		Trust:     cfg.Trust,
		UserAgent: "obied/" + version.Version,
		Store:     db,
		RateLimit: cfg.Mesh.RateLimit,
	}, logs.Logger(mesh.Name))
	if err != nil {
		return fmt.Errorf("mesh: %w", err)
	}

	mgr := lifecycle.New(logs.Logger("lifecycle"), lifecycle.Options{StopTimeout: cfg.Node.ShutdownTimeout.Std()})
	// The store starts first and stops last: every other subsystem may use it.
	mgr.Register(db)
	engine := decision.New(db, decision.NewPolicy(id.PeerID(), cfg.Trust, cfg.Decision), logs.Logger(decision.Name),
		decision.Options{Allowlist: allow})
	// The gate is the only path to enforcement; it subscribes before the
	// engine starts, so it sees the initial blocks.
	gate := enforce.NewGate(cfg.Node.Mode, nil, logs.Logger(enforceComponent))
	engine.Subscribe(gate.Handle)
	mgr.Register(engine)
	mgr.Register(ops.New(cfg.Metrics.Listen, mgr.Status, logs.Logger(ops.Name)))
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
		Overrides: storeOverrides{store: db, now: time.Now},
		Verdicts:  reporter,
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
		"admin_socket", cfg.Admin.Socket, "metrics_listen", cfg.Metrics.Listen)

	rl := &reloader{running: cfg, self: id.PeerID(), load: opts.LoadConfig, env: opts.Env,
		engine: engine, gate: gate, mesh: m, log: logs.Logger(reloadComponent)}
	waitForShutdown(ctx, opts.Reload, rl)
	log.Info("shutdown requested", "timeout", cfg.Node.ShutdownTimeout.String())
	if err := mgr.Stop(context.WithoutCancel(ctx)); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	log.Info("shutdown complete")
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
