package lifecycle

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// recorder collects the order of Start/Stop calls across fake subsystems.
type recorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *recorder) add(call string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call)
}

func (r *recorder) get() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

type fake struct {
	name     string
	rec      *recorder
	startErr error
	stopErr  error
	// blockStart / blockStop make Start / Stop wait for ctx to expire.
	blockStart bool
	blockStop  bool
	readyErr   error
}

func (f *fake) Name() string { return f.name }

func (f *fake) Start(ctx context.Context) error {
	f.rec.add("start " + f.name)
	if f.blockStart {
		<-ctx.Done()
		return ctx.Err()
	}
	return f.startErr
}

func (f *fake) Stop(ctx context.Context) error {
	f.rec.add("stop " + f.name)
	if f.blockStop {
		<-ctx.Done()
		return ctx.Err()
	}
	return f.stopErr
}

type readyFake struct{ *fake }

func (f readyFake) Ready() error { return f.readyErr }

func newManager(opts Options, subs ...Subsystem) *Manager {
	m := New(slog.New(slog.NewTextHandler(io.Discard, nil)), opts)
	for _, s := range subs {
		m.Register(s)
	}
	return m
}

func states(m *Manager) map[string]State {
	out := map[string]State{}
	for _, s := range m.Status() {
		out[s.Name] = s.State
	}
	return out
}

func TestStartInOrderStopInReverse(t *testing.T) {
	rec := &recorder{}
	m := newManager(Options{}, &fake{name: "a", rec: rec}, &fake{name: "b", rec: rec}, &fake{name: "c", rec: rec})

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	for _, s := range m.Status() {
		if s.State != StateRunning || !s.Ready {
			t.Errorf("after Start: %+v, want running and ready", s)
		}
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	want := []string{"start a", "start b", "start c", "stop c", "stop b", "stop a"}
	if got := rec.get(); !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
	if got := states(m); !reflect.DeepEqual(got, map[string]State{"a": StateStopped, "b": StateStopped, "c": StateStopped}) {
		t.Errorf("states after Stop = %v", got)
	}
}

func TestStartFailureStopsStartedSubsystems(t *testing.T) {
	rec := &recorder{}
	boom := errors.New("address already in use")
	m := newManager(Options{}, &fake{name: "a", rec: rec}, &fake{name: "b", rec: rec},
		&fake{name: "c", rec: rec, startErr: boom}, &fake{name: "d", rec: rec})

	err := m.Start(context.Background())
	var startErr *StartError
	if !errors.As(err, &startErr) || startErr.Subsystem != "c" || !errors.Is(err, boom) {
		t.Fatalf("Start error = %v, want StartError for c wrapping %v", err, boom)
	}
	if !strings.Contains(err.Error(), "start subsystem c: address already in use") {
		t.Errorf("error message = %q", err)
	}
	// c failed and cleans up after itself; d is never started.
	want := []string{"start a", "start b", "start c", "stop b", "stop a"}
	if got := rec.get(); !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
	wantStates := map[string]State{"a": StateStopped, "b": StateStopped, "c": StateFailed, "d": StatePending}
	if got := states(m); !reflect.DeepEqual(got, wantStates) {
		t.Errorf("states = %v, want %v", got, wantStates)
	}
}

func TestStartFailureReportsRollbackErrors(t *testing.T) {
	rec := &recorder{}
	stopBoom := errors.New("stuck")
	m := newManager(Options{}, &fake{name: "a", rec: rec, stopErr: stopBoom}, &fake{name: "b", rec: rec, startErr: errors.New("boom")})

	err := m.Start(context.Background())
	var startErr *StartError
	if !errors.As(err, &startErr) || !errors.Is(startErr.Rollback, stopBoom) || !errors.Is(err, stopBoom) {
		t.Fatalf("Start error = %v, want StartError carrying the stop error", err)
	}
	if want := "start subsystem b: boom (stopping started subsystems: stop subsystem a: stuck)"; err.Error() != want {
		t.Errorf("error message = %q, want %q", err, want)
	}
}

func TestStartTimeout(t *testing.T) {
	rec := &recorder{}
	m := newManager(Options{StartTimeout: 20 * time.Millisecond},
		&fake{name: "a", rec: rec}, &fake{name: "slow", rec: rec, blockStart: true})

	err := m.Start(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Start error = %v, want deadline exceeded", err)
	}
	if got, want := rec.get(), []string{"start a", "start slow", "stop a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
}

func TestStartCanceledBetweenSubsystems(t *testing.T) {
	rec := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	canceler := &cancelOnStart{fake: &fake{name: "a", rec: rec}, cancel: cancel}
	m := newManager(Options{}, canceler, &fake{name: "b", rec: rec})

	err := m.Start(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Start error = %v, want canceled", err)
	}
	if got, want := rec.get(), []string{"start a", "stop a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
}

type cancelOnStart struct {
	*fake
	cancel context.CancelFunc
}

func (c *cancelOnStart) Start(ctx context.Context) error {
	err := c.fake.Start(ctx)
	c.cancel()
	return err
}

func TestStopContinuesAfterFailureAndSharesDeadline(t *testing.T) {
	rec := &recorder{}
	m := newManager(Options{StopTimeout: 50 * time.Millisecond},
		&fake{name: "a", rec: rec},
		&fake{name: "b", rec: rec, stopErr: errors.New("flush failed")},
		&fake{name: "c", rec: rec, blockStop: true})
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	begin := time.Now()
	err := m.Stop(context.Background())
	if elapsed := time.Since(begin); elapsed > time.Second {
		t.Errorf("Stop took %v, want it bounded by StopTimeout", elapsed)
	}
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "stop subsystem b: flush failed") {
		t.Errorf("Stop error = %v, want both failures", err)
	}
	if got, want := rec.get(), []string{"start a", "start b", "start c", "stop c", "stop b", "stop a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
	wantStates := map[string]State{"a": StateStopped, "b": StateFailed, "c": StateFailed}
	if got := states(m); !reflect.DeepEqual(got, wantStates) {
		t.Errorf("states = %v, want %v", got, wantStates)
	}
}

func TestStopIsIdempotent(t *testing.T) {
	rec := &recorder{}
	m := newManager(Options{}, &fake{name: "a", rec: rec})
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := m.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := rec.get(), []string{"start a", "stop a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
}

func TestStatusReadiness(t *testing.T) {
	rec := &recorder{}
	degraded := readyFake{&fake{name: "b", rec: rec, readyErr: errors.New("serve loop exited")}}
	m := newManager(Options{}, &fake{name: "a", rec: rec}, degraded)

	before := NotReady(m.Status())
	if len(before) != 2 || before[0].State != StatePending {
		t.Errorf("before Start NotReady = %+v, want both pending", before)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []Status{{Name: "b", State: StateRunning, Ready: false, Error: "serve loop exited"}}
	if got := NotReady(m.Status()); !reflect.DeepEqual(got, want) {
		t.Errorf("NotReady = %+v, want %+v", got, want)
	}
	degraded.readyErr = nil
	if got := NotReady(m.Status()); len(got) != 0 {
		t.Errorf("NotReady = %+v, want none", got)
	}
}

type detailFake struct {
	*fake
	detail string
}

func (f detailFake) Detail() string { return f.detail }

func TestStatusDetail(t *testing.T) {
	rec := &recorder{}
	m := newManager(Options{}, &fake{name: "a", rec: rec}, detailFake{&fake{name: "b", rec: rec}, "degraded: 0 peers"})
	if got := m.Status()[1].Detail; got != "" {
		t.Errorf("detail before Start = %q, want none", got)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []Status{
		{Name: "a", State: StateRunning, Ready: true},
		{Name: "b", State: StateRunning, Ready: true, Detail: "degraded: 0 peers"},
	}
	if got := m.Status(); !reflect.DeepEqual(got, want) {
		t.Errorf("Status = %+v, want %+v", got, want)
	}
}

func TestMisuse(t *testing.T) {
	rec := &recorder{}
	m := newManager(Options{}, &fake{name: "a", rec: rec})
	assertPanics(t, "duplicate name", func() { m.Register(&fake{name: "a", rec: rec}) })
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertPanics(t, "register after start", func() { m.Register(&fake{name: "b", rec: rec}) })
	if err := m.Start(context.Background()); err == nil {
		t.Error("second Start succeeded, want an error")
	}
}

func assertPanics(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: no panic", name)
		}
	}()
	f()
}
