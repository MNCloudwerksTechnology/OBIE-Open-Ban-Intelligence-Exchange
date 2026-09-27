// Package daemon wires the subsystems of obied together and runs them for
// the lifetime of the process.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
	"github.com/MNCloudwerksTechnology/obie/internal/logging"
	"github.com/MNCloudwerksTechnology/obie/internal/ops"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/internal/version"
)

// Component is the logger name of the daemon itself.
const Component = "obied"

// Run starts the subsystems configured by cfg and blocks until ctx is
// canceled, then shuts them down in reverse order within
// node.shutdown_timeout. It returns nil after a clean shutdown, also when ctx
// is canceled during startup, and an error when a subsystem fails to start
// or to stop in time.
func Run(ctx context.Context, cfg *config.Config, logs *logging.Factory) error {
	log := logs.Logger(Component)
	startedAt := time.Now()

	mgr := lifecycle.New(logs.Logger("lifecycle"), lifecycle.Options{StopTimeout: cfg.Node.ShutdownTimeout.Std()})
	// The store starts first and stops last: every other subsystem may use it.
	mgr.Register(store.New(filepath.Join(cfg.Node.StateDir, "db"), logs.Logger(store.Name), store.Options{}))
	mgr.Register(ops.New(cfg.Metrics.Listen, mgr.Status, logs.Logger(ops.Name)))
	mgr.Register(admin.New(cfg.Admin.Socket, cfg.Admin.SocketGroup, admin.Info{
		Version:   version.Version,
		Mode:      string(cfg.Node.Mode),
		StartedAt: startedAt,
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
	log.Info("obied started", "version", version.Version, "mode", cfg.Node.Mode,
		"admin_socket", cfg.Admin.Socket, "metrics_listen", cfg.Metrics.Listen)

	<-ctx.Done()
	log.Info("shutdown requested", "timeout", cfg.Node.ShutdownTimeout.String())
	if err := mgr.Stop(context.WithoutCancel(ctx)); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	log.Info("shutdown complete")
	return nil
}
