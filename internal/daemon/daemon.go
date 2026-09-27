// Package daemon wires the subsystems of obied together and runs them for
// the lifetime of the process.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/identity"
	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
	"github.com/MNCloudwerksTechnology/obie/internal/logging"
	"github.com/MNCloudwerksTechnology/obie/internal/ops"
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

	mgr := lifecycle.New(logs.Logger("lifecycle"), lifecycle.Options{StopTimeout: cfg.Node.ShutdownTimeout.Std()})
	mgr.Register(ops.New(cfg.Metrics.Listen, mgr.Status, logs.Logger(ops.Name)))
	mgr.Register(admin.New(cfg.Admin.Socket, cfg.Admin.SocketGroup, admin.Info{
		Version:   version.Version,
		Mode:      string(cfg.Node.Mode),
		StartedAt: startedAt,
		Identity:  admin.IdentityResponse{PeerID: id.PeerID(), Fingerprint: identity.Fingerprint(id.PublicKey())},
		Status:    mgr.Status,
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
