package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

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
// console, exactly the settings config.Settings marks reload. Everything
// else needs a restart.
type reloader struct {
	// running is the configuration in effect, with the keys its file set;
	// only reload replaces it, never changing the one it replaces.
	running *config.File
	self    string
	load    func() (*config.File, error)
	env     sovereignty.Env
	engine  *decision.Engine
	gate    *enforce.Gate
	mesh    trustSetter
	// audit is reopened on every reload and records reloads and mode
	// changes; nil records nothing.
	audit *audit.Log
	// console applies the console settings; nil without console.
	console func(config.Console)
	// loads records the outcome of every reload; nil records nothing.
	loads *configLoads
	log   *slog.Logger
}

// reload reopens the audit log, then reads the configuration, applies it
// and records it in the audit log. If the configuration or an allow-list
// file is invalid, it logs why and changes nothing.
func (r *reloader) reload(ctx context.Context) error {
	_ = r.audit.Reopen() // a failure is logged and keeps the open file
	r.log.Info("reloading the configuration")
	file, err := r.load()
	if err != nil {
		return r.reject(err)
	}
	next := file.Config
	// The mesh keeps its listen addresses and bootstrap peers until a
	// restart, so the allow-list protects those, not the new ones.
	allowCfg := *next
	allowCfg.Mesh = r.running.Config.Mesh
	allow, err := sovereignty.Build(ctx, &allowCfg, r.env, r.log)
	if err != nil {
		return r.reject(fmt.Errorf("allow-list: %w", err))
	}
	// SetTrust is the only step that can fail; it comes first.
	if err := r.mesh.SetTrust(next.Trust); err != nil {
		return r.reject(err)
	}
	r.engine.Reload(decision.NewPolicy(r.self, next.Trust, next.Decision), allow)
	if mode := r.gate.Mode(); mode != next.Node.Mode {
		r.gate.SetMode(next.Node.Mode)
		r.audit.Write(audit.ModeChanged(string(mode), string(next.Node.Mode)))
	}
	// The console never fails a reload: it logs why it cannot serve.
	if r.console != nil {
		r.console(next.Console)
	}

	keys := config.ChangedOnRestart(r.running.Config, next)
	if len(keys) > 0 {
		r.log.Warn("configuration changes that need a restart were not applied", "keys", keys,
			"next", "restart the node to apply them: sudo systemctl restart obied")
	}
	applied := config.ChangedOnReload(r.running.Config, next)
	r.running = r.running.Reload(file)
	r.loads.reloaded(r.running, keys)
	r.audit.Write(audit.ConfigReloaded(file.Path, applied, keys))
	r.log.Info("configuration reloaded", "mode", next.Node.Mode, "allowlist_entries", len(allow.Entries()),
		"publishers", len(next.Trust.Publishers), "console_enabled", next.Console.Enabled)
	return nil
}

func (r *reloader) reject(err error) error {
	r.log.Error("configuration reload rejected; the running configuration is kept", "error", err,
		"next", "sudo obied --check-config names every problem; fix them, then reload again: sudo systemctl reload obied")
	r.loads.rejected(err)
	return err
}

// configLoads records the running configuration, when it was loaded and
// how the last reload went, for the web console; a nil *configLoads
// records nothing. Reloads and console requests run concurrently.
type configLoads struct {
	now func() time.Time
	mu  sync.Mutex
	rec loadRecord
}

// loadRecord is what configLoads recorded.
type loadRecord struct {
	// File is the running configuration, with the keys its file set and
	// the file's path (ADR 0024); nil for a nil *configLoads. It never
	// changes.
	File *config.File
	// LoadedAt is when the running configuration was loaded: at start,
	// or by the last successful reload if Reloaded.
	LoadedAt time.Time
	Reloaded bool
	// RejectedAt and Rejected are the time and the error of the last
	// rejected reload; zero once a later reload succeeds.
	RejectedAt time.Time
	Rejected   error
	// RestartKeys name the settings in which the configuration file, as
	// of the last successful reload, differs from the running
	// configuration and which only a restart applies.
	RestartKeys []string
}

// newConfigLoads records the configuration file loaded at start, at
// loadedAt.
func newConfigLoads(file *config.File, loadedAt time.Time, now func() time.Time) *configLoads {
	return &configLoads{now: now, rec: loadRecord{File: file, LoadedAt: loadedAt}}
}

// reloaded records a successful reload that left running in effect and
// restartKeys unapplied.
func (l *configLoads) reloaded(running *config.File, restartKeys []string) {
	if l == nil {
		return
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rec = loadRecord{File: running, LoadedAt: now, Reloaded: true, RestartKeys: slices.Clone(restartKeys)}
}

// rejected records a reload rejected with err; the running configuration
// and its restart keys stay.
func (l *configLoads) rejected(err error) {
	if l == nil {
		return
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rec.RejectedAt, l.rec.Rejected = now, err
}

// record returns what was recorded; nothing for a nil *configLoads.
func (l *configLoads) record() loadRecord {
	if l == nil {
		return loadRecord{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	rec := l.rec
	rec.RestartKeys = slices.Clone(rec.RestartKeys)
	return rec
}
