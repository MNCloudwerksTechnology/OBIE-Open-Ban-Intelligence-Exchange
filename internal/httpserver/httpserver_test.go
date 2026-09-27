package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func hello() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "hello") })
}

func TestServeAndStop(t *testing.T) {
	s := New("web", TCP("127.0.0.1:0"), hello(), discardLogger())
	if s.Name() != "web" || s.Addr() != nil {
		t.Fatalf("before Start: name %q addr %v", s.Name(), s.Addr())
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	url := "http://" + s.Addr().String() + "/"
	resp, err := http.Get(url) // #nosec G107 -- test server URL.
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "hello" {
		t.Errorf("body = %q", body)
	}
	if err := s.Ready(); err != nil {
		t.Errorf("Ready = %v", err)
	}
	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if _, err := http.Get(url); err == nil { // #nosec G107 -- test server URL.
		t.Error("server still answers after Stop")
	}
	if err := s.Ready(); err != nil {
		t.Errorf("Ready after graceful Stop = %v, want nil", err)
	}
}

func TestStartFailsWhenAddressInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	s := New("web", TCP(ln.Addr().String()), hello(), discardLogger())
	if err := s.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "address already in use") {
		t.Errorf("Start = %v, want address in use", err)
	}
	if err := s.Stop(context.Background()); err != nil {
		t.Errorf("Stop without a started server = %v", err)
	}
}

func TestNotReadyWhenServingFails(t *testing.T) {
	var ln net.Listener
	listen := func(context.Context) (net.Listener, error) {
		var err error
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		return ln, err
	}
	s := New("web", listen, hello(), discardLogger())
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Stop(context.Background()) }()
	_ = ln.Close() // Serve returns an error other than ErrServerClosed.
	deadline := time.Now().Add(5 * time.Second)
	for s.Ready() == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if err := s.Ready(); err == nil || !strings.Contains(err.Error(), "not serving") {
		t.Errorf("Ready = %v, want not serving", err)
	}
}

func TestConnContext(t *testing.T) {
	type key struct{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		addr, _ := r.Context().Value(key{}).(string)
		_, _ = io.WriteString(w, addr)
	})
	s := New("web", TCP("127.0.0.1:0"), handler, discardLogger(), ConnContext(func(ctx context.Context, c net.Conn) context.Context {
		return context.WithValue(ctx, key{}, c.LocalAddr().String())
	}))
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Stop(context.Background()) }()
	resp, err := http.Get("http://" + s.Addr().String() + "/") // #nosec G107 -- test server URL.
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != s.Addr().String() {
		t.Errorf("connection context value = %q, want %q", body, s.Addr().String())
	}
}
