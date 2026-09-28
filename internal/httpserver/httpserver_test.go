package httpserver

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
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
		_, _ = io.WriteString(w, addr) // #nosec G705 -- a test server echoing its own address.
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

func TestDefaultLimits(t *testing.T) {
	var got *http.Server
	s := New("web", TCP("127.0.0.1:0"), hello(), discardLogger(), func(srv *http.Server) { got = srv })
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Stop(context.Background()) }()
	want := DefaultTimeouts
	if got.ReadHeaderTimeout != want.ReadHeader || got.ReadTimeout != want.Read || got.WriteTimeout != want.Write ||
		got.IdleTimeout != want.Idle || got.MaxHeaderBytes != MaxHeaderBytes {
		t.Errorf("server limits = header %v read %v write %v idle %v max header %d, want %+v and %d",
			got.ReadHeaderTimeout, got.ReadTimeout, got.WriteTimeout, got.IdleTimeout, got.MaxHeaderBytes, want, MaxHeaderBytes)
	}
	for name, d := range map[string]time.Duration{"read header": want.ReadHeader, "read": want.Read, "write": want.Write, "idle": want.Idle} {
		if d <= 0 {
			t.Errorf("default %s timeout %v disables the timeout", name, d)
		}
	}
}

// testTimeouts are short timeouts for the slow-client tests.
var testTimeouts = Timeouts{
	ReadHeader: 200 * time.Millisecond,
	Read:       300 * time.Millisecond,
	Write:      300 * time.Millisecond,
	Idle:       300 * time.Millisecond,
}

// closedWithin reports whether the server closes conn within d, after
// the client sent send.
func closedWithin(t *testing.T, conn net.Conn, send string, d time.Duration) bool {
	t.Helper()
	if _, err := io.WriteString(conn, send); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(d)); err != nil {
		t.Fatal(err)
	}
	// Whatever the server answers (e.g. 408), it must then close.
	_, err := io.Copy(io.Discard, conn)
	var netErr net.Error
	return err == nil || !errors.As(err, &netErr) || !netErr.Timeout()
}

func TestTimeoutsCutSlowClients(t *testing.T) {
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			time.Sleep(2 * testTimeouts.Write)
		}
		_, _ = io.WriteString(w, "hello")
	})
	s := New("web", TCP("127.0.0.1:0"), slow, discardLogger(), WithTimeouts(testTimeouts))
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Stop(context.Background()) }()

	tests := []struct {
		name, send string
	}{
		{"headers never finish", "GET / HTTP/1.1\r\nHost: x\r\n"},
		{"body never finishes", "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: 1000\r\n\r\npartial"},
		{"idle keep-alive connection", "GET / HTTP/1.1\r\nHost: x\r\n\r\n"},
		{"handler slower than the write timeout", "GET /slow HTTP/1.1\r\nHost: x\r\n\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, err := net.Dial("tcp", s.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.Close() }()
			if !closedWithin(t, conn, tt.send, 3*time.Second) {
				t.Errorf("server kept the connection open for 3s; timeouts %+v", testTimeouts)
			}
		})
	}
}

func TestMaxHeaderBytes(t *testing.T) {
	s := New("web", TCP("127.0.0.1:0"), hello(), discardLogger())
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Stop(context.Background()) }()
	req, err := http.NewRequest(http.MethodGet, "http://"+s.Addr().String()+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	// net/http grants 4 KiB of slack beyond MaxHeaderBytes.
	req.Header.Set("X-Filler", strings.Repeat("a", 2*MaxHeaderBytes))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusRequestHeaderFieldsTooLarge {
		t.Errorf("oversized headers: status %d, want %d", resp.StatusCode, http.StatusRequestHeaderFieldsTooLarge)
	}
}

// TestEveryServerIsBounded checks that no production code outside this
// package serves HTTP, so that every server gets the limits above.
func TestEveryServerIsBounded(t *testing.T) {
	root := filepath.Join("..", "..")
	forbidden := []string{"http.Server{", "http.ListenAndServe", "http.Serve("}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && (d.Name() == "website" || d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".")) && path != root:
			return filepath.SkipDir
		case d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go"):
			return nil
		case filepath.Base(filepath.Dir(path)) == "httpserver":
			return nil
		}
		data, err := os.ReadFile(path) // #nosec G304,G122 -- walking this repository's sources.
		if err != nil {
			return err
		}
		for _, f := range forbidden {
			if strings.Contains(string(data), f) {
				t.Errorf("%s uses %s; serve HTTP through internal/httpserver", path, f)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
