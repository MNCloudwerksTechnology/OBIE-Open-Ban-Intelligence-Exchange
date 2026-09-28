// Package httpserver runs an HTTP server as a lifecycle subsystem.
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

// Timeouts bound every phase of a request, so that no client — slow,
// stalled or hostile — can hold a connection or its goroutine for long
// (ADR 0017).
type Timeouts struct {
	// ReadHeader bounds reading the request headers.
	ReadHeader time.Duration
	// Read bounds reading the whole request, body included.
	Read time.Duration
	// Write bounds the time from the end of the request headers to the
	// end of the response.
	Write time.Duration
	// Idle bounds how long a keep-alive connection waits for the next
	// request.
	Idle time.Duration
}

// DefaultTimeouts are the timeouts of every server. The endpoints answer
// from memory or the local store: a request that takes longer is stuck.
var DefaultTimeouts = Timeouts{
	ReadHeader: 5 * time.Second,
	Read:       10 * time.Second,
	Write:      30 * time.Second,
	Idle:       60 * time.Second,
}

// MaxHeaderBytes bounds the request headers; the endpoints need a few
// hundred bytes.
const MaxHeaderBytes = 16 << 10

// ListenFunc opens the listener the server accepts connections on.
type ListenFunc func(ctx context.Context) (net.Listener, error)

// TCP returns a ListenFunc listening on the TCP address addr.
func TCP(addr string) ListenFunc {
	return func(ctx context.Context) (net.Listener, error) {
		var lc net.ListenConfig
		return lc.Listen(ctx, "tcp", addr)
	}
}

// Option configures the http.Server of a Server.
type Option func(*http.Server)

// WithTimeouts replaces DefaultTimeouts.
func WithTimeouts(t Timeouts) Option {
	return func(srv *http.Server) {
		srv.ReadHeaderTimeout, srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout = t.ReadHeader, t.Read, t.Write, t.Idle
	}
}

// ConnContext sets http.Server.ConnContext: fn derives the context of every
// request on a connection, e.g. to attach the peer's credentials.
func ConnContext(fn func(ctx context.Context, c net.Conn) context.Context) Option {
	return func(srv *http.Server) { srv.ConnContext = fn }
}

// Server serves handler on the listener returned by listen. It implements
// lifecycle.Subsystem and lifecycle.ReadinessChecker.
type Server struct {
	name    string
	listen  ListenFunc
	handler http.Handler
	log     *slog.Logger
	opts    []Option

	mu       sync.Mutex
	srv      *http.Server
	addr     net.Addr
	serveErr error
	done     chan struct{}
}

// New returns a Server named name.
func New(name string, listen ListenFunc, handler http.Handler, log *slog.Logger, opts ...Option) *Server {
	return &Server{name: name, listen: listen, handler: handler, log: log, opts: opts}
}

// Name returns the subsystem name.
func (s *Server) Name() string { return s.name }

// Start opens the listener and serves in the background.
func (s *Server) Start(ctx context.Context) error {
	ln, err := s.listen(ctx)
	if err != nil {
		return err
	}
	d := DefaultTimeouts
	srv := &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: d.ReadHeader,
		ReadTimeout:       d.Read,
		WriteTimeout:      d.Write,
		IdleTimeout:       d.Idle,
		MaxHeaderBytes:    MaxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(s.log.Handler(), slog.LevelWarn),
	}
	for _, opt := range s.opts {
		opt(srv)
	}
	done := make(chan struct{})

	s.mu.Lock()
	s.srv, s.addr, s.done = srv, ln.Addr(), done
	s.mu.Unlock()

	go func() {
		defer close(done)
		if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
			s.log.Error("HTTP server stopped serving", "addr", ln.Addr().String(), "error", err)
			s.mu.Lock()
			s.serveErr = err
			s.mu.Unlock()
		}
	}()
	s.log.Info("HTTP server listening", "addr", ln.Addr().String())
	return nil
}

// Stop shuts the server down gracefully, closing it forcibly when ctx
// expires first.
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	srv, done := s.srv, s.done
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	err := srv.Shutdown(ctx)
	if err != nil {
		_ = srv.Close()
		err = fmt.Errorf("graceful shutdown: %w", err)
	}
	<-done
	return err
}

// Ready reports an error once the server has stopped serving unexpectedly.
func (s *Server) Ready() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.serveErr != nil {
		return fmt.Errorf("not serving: %w", s.serveErr)
	}
	return nil
}

// Addr returns the listen address; nil before Start.
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}
