// Package daemon wires the subsystems of obied together and runs them for
// the lifetime of the process.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/decision"
	"github.com/MNCloudwerksTechnology/obie/internal/identity"
	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
	"github.com/MNCloudwerksTechnology/obie/internal/logging"
	"github.com/MNCloudwerksTechnology/obie/internal/mesh"
	"github.com/MNCloudwerksTechnology/obie/internal/ops"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/internal/version"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// Component is the logger name of the daemon itself.
const Component = "obied"

// Run loads the node identity from node.state_dir, generating it on the
// first start, then starts the subsystems configured by cfg and blocks until
// ctx is canceled, then shuts them down in reverse order within
// node.shutdown_timeout. It returns nil after a clean shutdown, also when ctx
// is canceled during startup, and an error when a subsystem fails to start
// or to stop in time, and when the identity cannot be loaded.
func Run(ctx context.Context, cfg *config.Config, logs *logging.Factory) error {
	log := logs.Logger(Component)
	startedAt := time.Now()

	id, err := loadIdentity(cfg.Node.StateDir, log)
	if err != nil {
		return err
	}

	// go-libp2p's own logs join ours; below warn they are too chatty.
	mesh.UseLogHandler(logs.Logger("libp2p").Handler(), slog.LevelWarn)
	m, err := mesh.New(id, mesh.Options{
		Listen:    cfg.Mesh.Listen,
		Bootstrap: cfg.Mesh.Bootstrap,
		Trust:     cfg.Trust,
		UserAgent: "obied/" + version.Version,
	}, logs.Logger(mesh.Name))
	if err != nil {
		return fmt.Errorf("mesh: %w", err)
	}

	mgr := lifecycle.New(logs.Logger("lifecycle"), lifecycle.Options{StopTimeout: cfg.Node.ShutdownTimeout.Std()})
	// The store starts first and stops last: every other subsystem may use it.
	st := store.New(filepath.Join(cfg.Node.StateDir, "db"), logs.Logger(store.Name), store.Options{})
	mgr.Register(st)
	engine := decision.New(st, decision.NewPolicy(id.PeerID(), cfg.Trust, cfg.Decision), logs.Logger(decision.Name), decision.Options{})
	mgr.Register(engine)
	mgr.Register(ops.New(cfg.Metrics.Listen, mgr.Status, logs.Logger(ops.Name)))
	mgr.Register(m)
	mgr.Register(admin.New(cfg.Admin.Socket, cfg.Admin.SocketGroup, admin.Info{
		Version:   version.Version,
		Mode:      string(cfg.Node.Mode),
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

	<-ctx.Done()
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

// sovereigntyPending is the explanation's sovereignty section until the
// allow-list and overrides are applied (WP #1660).
var sovereigntyPending = admin.SovereigntyResponse{
	Applied: false,
	Note:    "allow-list and operator overrides are not applied yet",
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
	sovereignty := sovereigntyPending
	resp.Sovereignty = &sovereignty
	return resp
}
