// Package lifecycle starts and stops the subsystems of the node daemon.
//
// Subsystems are started in registration order, each under its own start
// timeout, and stopped in reverse order under one shared shutdown deadline.
// If a subsystem fails to start, the ones already started are stopped again.
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Subsystem is a long-running part of the daemon.
//
// Start acquires the subsystem's resources and returns once it is serving;
// background work must outlive ctx, which only bounds the start itself. If
// Start fails it must release whatever it acquired. Stop releases everything
// and returns when done or when ctx expires.
type Subsystem interface {
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// ReadinessChecker is implemented by subsystems that can be running but not
// (or no longer) ready, e.g. because a background loop failed. Ready returns
// nil when the subsystem is ready and the reason otherwise.
type ReadinessChecker interface {
	Ready() error
}

// State is the lifecycle state of a subsystem.
type State string

// Subsystem states.
const (
	StatePending  State = "pending"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateFailed   State = "failed"
	StateStopping State = "stopping"
	StateStopped  State = "stopped"
)

// Status is a point-in-time report of one subsystem.
type Status struct {
	Name  string `json:"name"`
	State State  `json:"state"`
	Ready bool   `json:"ready"`
	// Error is the start/stop failure or the reason the subsystem is not
	// ready; empty otherwise.
	Error string `json:"error,omitempty"`
}

// Default timeouts used when Options leaves them zero.
const (
	DefaultStartTimeout = 30 * time.Second
	DefaultStopTimeout  = 10 * time.Second
)

// Options configures a Manager.
type Options struct {
	// StartTimeout bounds the Start of each subsystem.
	StartTimeout time.Duration
	// StopTimeout bounds the whole shutdown, shared by all subsystems.
	StopTimeout time.Duration
}

type entry struct {
	sub   Subsystem
	state State
	err   error
}

// Manager owns the ordered set of subsystems. It is safe for concurrent use;
// Status may be called while Start or Stop run.
type Manager struct {
	log  *slog.Logger
	opts Options

	mu      sync.Mutex
	entries []*entry
	started bool
}

// New returns a Manager logging to log.
func New(log *slog.Logger, opts Options) *Manager {
	if opts.StartTimeout <= 0 {
		opts.StartTimeout = DefaultStartTimeout
	}
	if opts.StopTimeout <= 0 {
		opts.StopTimeout = DefaultStopTimeout
	}
	return &Manager{log: log, opts: opts}
}

// Register appends s to the start order. It panics when called after Start
// or with a duplicate name, both of which are programming errors.
func (m *Manager) Register(s Subsystem) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		panic("lifecycle: Register called after Start")
	}
	for _, e := range m.entries {
		if e.sub.Name() == s.Name() {
			panic(fmt.Sprintf("lifecycle: subsystem %q registered twice", s.Name()))
		}
	}
	m.entries = append(m.entries, &entry{sub: s, state: StatePending})
}

// StartError reports the subsystem that failed to start.
type StartError struct {
	Subsystem string
	Err       error
}

func (e *StartError) Error() string {
	return fmt.Sprintf("start subsystem %s: %v", e.Subsystem, e.Err)
}

func (e *StartError) Unwrap() error { return e.Err }

// Start starts every registered subsystem in order. On the first failure, or
// when ctx is canceled in between, it stops the subsystems already started
// and returns a *StartError (joined with any stop errors). Start may only be
// called once.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return errors.New("lifecycle: Start called twice")
	}
	m.started = true
	entries := append([]*entry(nil), m.entries...)
	m.mu.Unlock()

	for _, e := range entries {
		name := e.sub.Name()
		err := ctx.Err()
		if err == nil {
			m.setState(e, StateStarting, nil)
			m.log.Info("starting subsystem", "subsystem", name)
			err = m.startOne(ctx, e.sub)
		}
		if err != nil {
			m.setState(e, StateFailed, err)
			m.log.Error("subsystem failed to start; stopping started subsystems", "subsystem", name, "error", err)
			startErr := &StartError{Subsystem: name, Err: err}
			if stopErr := m.Stop(context.WithoutCancel(ctx)); stopErr != nil {
				return errors.Join(startErr, stopErr)
			}
			return startErr
		}
		m.setState(e, StateRunning, nil)
		m.log.Info("subsystem started", "subsystem", name)
	}
	return nil
}

func (m *Manager) startOne(ctx context.Context, s Subsystem) error {
	ctx, cancel := context.WithTimeout(ctx, m.opts.StartTimeout)
	defer cancel()
	return s.Start(ctx)
}

// Stop stops every running subsystem in reverse start order. All of them
// share one deadline: the earlier of ctx's and StopTimeout. A failing
// subsystem does not prevent the others from being stopped; all failures are
// returned joined. Subsystems that never started are skipped.
func (m *Manager) Stop(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, m.opts.StopTimeout)
	defer cancel()

	m.mu.Lock()
	entries := append([]*entry(nil), m.entries...)
	m.mu.Unlock()

	var errs []error
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if m.state(e) != StateRunning {
			continue
		}
		name := e.sub.Name()
		m.setState(e, StateStopping, nil)
		m.log.Info("stopping subsystem", "subsystem", name)
		if err := e.sub.Stop(ctx); err != nil {
			m.setState(e, StateFailed, err)
			m.log.Error("subsystem failed to stop", "subsystem", name, "error", err)
			errs = append(errs, fmt.Errorf("stop subsystem %s: %w", name, err))
			continue
		}
		m.setState(e, StateStopped, nil)
		m.log.Info("subsystem stopped", "subsystem", name)
	}
	return errors.Join(errs...)
}

// Status reports every subsystem in start order. A subsystem is ready when
// it is running and, if it implements ReadinessChecker, reports ready.
func (m *Manager) Status() []Status {
	m.mu.Lock()
	type snapshot struct {
		sub   Subsystem
		state State
		err   error
	}
	snaps := make([]snapshot, len(m.entries))
	for i, e := range m.entries {
		snaps[i] = snapshot{sub: e.sub, state: e.state, err: e.err}
	}
	m.mu.Unlock()

	// Ready is called without holding the lock: it may take its own locks.
	out := make([]Status, len(snaps))
	for i, s := range snaps {
		st := Status{Name: s.sub.Name(), State: s.state}
		if s.err != nil {
			st.Error = s.err.Error()
		}
		if s.state == StateRunning {
			st.Ready = true
			if rc, ok := s.sub.(ReadinessChecker); ok {
				if err := rc.Ready(); err != nil {
					st.Ready = false
					st.Error = err.Error()
				}
			}
		}
		out[i] = st
	}
	return out
}

// NotReady returns the statuses of the subsystems that are not ready; an
// empty result means the node is ready.
func NotReady(statuses []Status) []Status {
	var out []Status
	for _, s := range statuses {
		if !s.Ready {
			out = append(out, s)
		}
	}
	return out
}

func (m *Manager) setState(e *entry, s State, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e.state = s
	e.err = err
}

func (m *Manager) state(e *entry) State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return e.state
}
