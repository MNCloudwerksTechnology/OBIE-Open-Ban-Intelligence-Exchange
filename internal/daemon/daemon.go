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
	"github.com/MNCloudwerksTechnology/obie/internal/identity"
	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
	"github.com/MNCloudwerksTechnology/obie/internal/logging"
	"github.com/MNCloudwerksTechnology/obie/internal/mesh"
	"github.com/MNCloudwerksTechnology/obie/internal/ops"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/internal/version"
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
	mgr.Register(ops.New(cfg.Metrics.Listen, mgr.Status, logs.Logger(ops.Name)))
	mgr.Register(m)
	mgr.Register(admin.New(cfg.Admin.Socket, cfg.Admin.SocketGroup, admin.Info{
		Version:   version.Version,
		Mode:      string(cfg.Node.Mode),
		StartedAt: startedAt,
		Identity:  admin.NewIdentityResponse(id),
		Status:    mgr.Status,
		Peers:     func() []admin.PeerResponse { return peerResponses(m.Peers()) },
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
