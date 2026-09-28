package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"

	"github.com/MNCloudwerksTechnology/obie/internal/audit"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/decision"
	"github.com/MNCloudwerksTechnology/obie/internal/enforce"
	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
)

// trustSetter is the mesh's part in a reload.
type trustSetter interface {
	SetTrust(config.Trust) error
}

// reloader applies a new configuration to the running node: the trust
// weights, the decision settings, the allow-list, the mode and the web
// console. Everything else needs a restart.
type reloader struct {
	// running is the configuration in effect; only reload changes it.
	running *config.Config
	self    string
	load    func() (*config.Config, error)
	env     sovereignty.Env
	engine  *decision.Engine
	gate    *enforce.Gate
	mesh    trustSetter
	// audit is reopened on every reload; nil without audit log.
	audit *audit.Log
	// console applies the console settings; nil without console.
	console func(config.Console)
	log     *slog.Logger
}

// reload reopens the audit log, then reads the configuration and applies
// it. If the configuration or an allow-list file is invalid, it logs why
// and changes nothing.
func (r *reloader) reload(ctx context.Context) error {
	_ = r.audit.Reopen() // a failure is logged and keeps the open file
	r.log.Info("reloading the configuration")
	next, err := r.load()
	if err != nil {
		return r.reject(err)
	}
	// The mesh keeps its listen addresses and bootstrap peers until a
	// restart, so the allow-list protects those, not the new ones.
	allowCfg := *next
	allowCfg.Mesh = r.running.Mesh
	allow, err := sovereignty.Build(ctx, &allowCfg, r.env, r.log)
	if err != nil {
		return r.reject(fmt.Errorf("allow-list: %w", err))
	}
	// SetTrust is the only step that can fail; it comes first.
	if err := r.mesh.SetTrust(next.Trust); err != nil {
		return r.reject(err)
	}
	r.engine.Reload(decision.NewPolicy(r.self, next.Trust, next.Decision), allow)
	r.gate.SetMode(next.Node.Mode)
	// The console never fails a reload: it logs why it cannot serve.
	if r.console != nil {
		r.console(next.Console)
	}

	if keys := restartKeys(r.running, next); len(keys) > 0 {
		r.log.Warn("configuration changes that need a restart were not applied", "keys", keys)
	}
	r.running.Node.Mode = next.Node.Mode
	r.running.Trust, r.running.Decision, r.running.Allowlist = next.Trust, next.Decision, next.Allowlist
	r.running.Console = next.Console
	r.log.Info("configuration reloaded", "mode", next.Node.Mode, "allowlist_entries", len(allow.Entries()),
		"publishers", len(next.Trust.Publishers), "console_enabled", next.Console.Enabled)
	return nil
}

func (r *reloader) reject(err error) error {
	r.log.Error("configuration reload rejected; the running configuration is kept", "error", err)
	return err
}

// restartKeys names the sections of next that differ from running in
// settings a reload does not apply.
func restartKeys(running, next *config.Config) []string {
	var keys []string
	differs := func(key string, a, b any) {
		if !reflect.DeepEqual(a, b) {
			keys = append(keys, key)
		}
	}
	differs("node.state_dir", running.Node.StateDir, next.Node.StateDir)
	differs("node.shutdown_timeout", running.Node.ShutdownTimeout, next.Node.ShutdownTimeout)
	differs("admin", running.Admin, next.Admin)
	differs("mesh.listen", running.Mesh.Listen, next.Mesh.Listen)
	differs("mesh.bootstrap", running.Mesh.Bootstrap, next.Mesh.Bootstrap)
	differs("mesh.rate_limit", running.Mesh.RateLimit, next.Mesh.RateLimit)
	differs("store", running.Store, next.Store)
	differs("enforce", running.Enforce, next.Enforce)
	differs("metrics", running.Metrics, next.Metrics)
	differs("audit", running.Audit, next.Audit)
	differs("log", running.Log, next.Log)
	return keys
}
