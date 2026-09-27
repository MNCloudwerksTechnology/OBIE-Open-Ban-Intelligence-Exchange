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

// readHeaderTimeout protects the local servers against slow-header clients.
const readHeaderTimeout = 5 * time.Second

// ListenFunc opens the listener the server accepts connections on.
type ListenFunc func(ctx context.Context) (net.Listener, error)

// TCP returns a ListenFunc listening on the TCP address addr.
func TCP(addr string) ListenFunc {
	return func(ctx context.Context) (net.Listener, error) {
		var lc net.ListenConfig
		return lc.Listen(ctx, "tcp", addr)
	}
}

// Server serves handler on the listener returned by listen. It implements
// lifecycle.Subsystem and lifecycle.ReadinessChecker.
type Server struct {
	name    string
	listen  ListenFunc
	handler http.Handler
	log     *slog.Logger

	mu       sync.Mutex
	srv      *http.Server
	addr     net.Addr
	serveErr error
	done     chan struct{}
}

// New returns a Server named name.
func New(name string, listen ListenFunc, handler http.Handler, log *slog.Logger) *Server {
	return &Server{name: name, listen: listen, handler: handler, log: log}
}

// Name returns the subsystem name.
func (s *Server) Name() string { return s.name }

// Start opens the listener and serves in the background.
func (s *Server) Start(ctx context.Context) error {
	ln, err := s.listen(ctx)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ErrorLog:          slog.NewLogLogger(s.log.Handler(), slog.LevelWarn),
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
