package console

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
)

// syncBuffer collects log output written by the servers' goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

var testNode = Node{
	Version:     "v0.1.0",
	PeerID:      "12D3KooWGzBX6MWMMz3kHmFfyT3vJxFoy4xQF8NbXN7xBAFhGyvd",
	Fingerprint: "SHA256:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU",
	StartedAt:   time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC),
	Mode:        func() string { return "observe" },
	Status: func() []lifecycle.Status {
		return []lifecycle.Status{{Name: Name, State: lifecycle.StateRunning, Ready: true}}
	},
	Facts: func() Facts {
		return Facts{Config: ConfigFacts{LoadedAt: time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)}}
	},
}

// newConsole returns a console for cfg that logs into logs; it is stopped
// when the test ends.
func newConsole(t *testing.T, cfg config.Console, logs *syncBuffer) *Console {
	t.Helper()
	c := New(cfg, Options{Group: "obie-test-no-such-group", Node: testNode}, slog.New(slog.NewJSONHandler(logs, nil)))
	t.Cleanup(func() { _ = c.Stop(context.Background()) })
	return c
}

// freeAddr returns a loopback address with a port that was free a moment
// ago.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().String()
}

// get requests path from the console at addr as a browser on this host
// would, without keeping the connection.
func get(t *testing.T, addr, path string) (*http.Response, string) {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get("http://" + addr + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body bytes.Buffer
	_, _ = body.ReadFrom(resp.Body)
	return resp, body.String()
}

// refused reports whether nothing listens on addr.
func refused(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, time.Second)
	if err == nil {
		_ = c.Close()
	}
	return err != nil
}

func TestDisabledByDefault(t *testing.T) {
	c := newConsole(t, config.Default().Console, &syncBuffer{})
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start = %v", err)
	}
	if c.Addr() != nil || c.Detail() != "disabled" {
		t.Errorf("default console: addr %v, detail %q; want not serving and disabled", c.Addr(), c.Detail())
	}
	if s := c.State(); s.Enabled || s.URL != "" || s.Listen != "127.0.0.1:9465" {
		t.Errorf("State = %+v", s)
	}
}

func TestStartServes(t *testing.T) {
	logs := &syncBuffer{}
	c := newConsole(t, config.Console{Enabled: true, Listen: "127.0.0.1:0"}, logs)
	if c.Detail() != "not started" {
		t.Errorf("Detail before Start = %q", c.Detail())
	}
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start = %v", err)
	}
	addr := c.Addr()
	if addr == nil {
		t.Fatalf("not serving: %s", c.Detail())
	}
	want := "http://" + addr.String() + "/"
	if c.Detail() != "serving at "+want || c.State().URL != want {
		t.Errorf("Detail = %q, State = %+v; want serving at %s", c.Detail(), c.State(), want)
	}
	if !strings.Contains(logs.String(), `"msg":"console serving","url":"`+want+`"`) {
		t.Errorf("no serving log line:\n%s", logs)
	}
	resp, _ := get(t, addr.String(), "/no-such-page")
	if resp.Header.Get("Content-Security-Policy") != contentSecurityPolicy {
		t.Errorf("response without the security headers: %v", resp.Header)
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop = %v", err)
	}
	if !refused(addr.String()) {
		t.Error("still listening after Stop")
	}
}

func TestPortInUseKeepsRunning(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	logs := &syncBuffer{}
	cfg := config.Console{Enabled: true, Listen: taken.Addr().String()}
	c := newConsole(t, cfg, logs)
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start = %v, want nil: the console never stops the node", err)
	}
	if d := c.Detail(); !strings.HasPrefix(d, "not serving: ") || !strings.Contains(d, "address already in use") {
		t.Errorf("Detail = %q, want the bind error", d)
	}
	if s := c.State(); s.Err == nil || s.URL != "" {
		t.Errorf("State = %+v, want the error and no URL", s)
	}
	if !strings.Contains(logs.String(), `"level":"ERROR","msg":"console not started; the node runs without it"`) ||
		!strings.Contains(logs.String(), "address already in use") || !strings.Contains(logs.String(), "choose another console.listen") {
		t.Errorf("no log line explaining why:\n%s", logs)
	}

	// Once the port is free, a reload with the same settings retries.
	_ = taken.Close()
	c.Apply(cfg)
	if c.Addr() == nil || c.Addr().String() != cfg.Listen {
		t.Errorf("after the port was freed: addr %v, detail %q", c.Addr(), c.Detail())
	}
}

func TestApplySwitchesAndMoves(t *testing.T) {
	c := newConsole(t, config.Console{Listen: "127.0.0.1:9465"}, &syncBuffer{})
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	a, b := freeAddr(t), freeAddr(t)

	c.Apply(config.Console{Enabled: true, Listen: a})
	if c.Addr() == nil || c.Addr().String() != a {
		t.Fatalf("switched on: addr %v, detail %q; want %s", c.Addr(), c.Detail(), a)
	}
	if resp, _ := get(t, a, "/"); resp.StatusCode == 0 {
		t.Fatal("no answer")
	}
	c.Apply(config.Console{Enabled: true, Listen: a}) // unchanged: keeps serving
	if c.Addr() == nil || c.Addr().String() != a {
		t.Fatalf("unchanged: addr %v", c.Addr())
	}

	c.Apply(config.Console{Enabled: true, Listen: b})
	if c.Addr() == nil || c.Addr().String() != b || !refused(a) {
		t.Fatalf("moved: addr %v, old address refused %v", c.Addr(), refused(a))
	}

	c.Apply(config.Console{Enabled: false, Listen: b})
	if c.Addr() != nil || c.Detail() != "disabled" || !refused(b) {
		t.Fatalf("switched off: addr %v, detail %q, refused %v", c.Addr(), c.Detail(), refused(b))
	}
}

// TestApplySwitchesActions: a reload switches the actions on and off
// without moving or restarting the console (ADR 0026).
func TestApplySwitchesActions(t *testing.T) {
	a := freeAddr(t)
	c := newConsole(t, config.Console{Enabled: true, Listen: a}, &syncBuffer{})
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	served := c.Addr()
	if c.actionsOn() {
		t.Error("actions on without console.actions")
	}
	c.Apply(config.Console{Enabled: true, Listen: a, Actions: true})
	if !c.actionsOn() || c.Addr() != served {
		t.Errorf("switched on: actions %v, addr %v (was %v)", c.actionsOn(), c.Addr(), served)
	}
	c.Apply(config.Console{Enabled: true, Listen: a})
	if c.actionsOn() || c.Addr() != served {
		t.Errorf("switched off: actions %v, addr %v (was %v)", c.actionsOn(), c.Addr(), served)
	}
}

func TestApplyOutsideStartAndStop(t *testing.T) {
	c := newConsole(t, config.Console{Listen: "127.0.0.1:9465"}, &syncBuffer{})
	c.Apply(config.Console{Enabled: true, Listen: "127.0.0.1:0"})
	if c.Addr() != nil {
		t.Fatal("serving before Start")
	}
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.Addr() == nil {
		t.Fatalf("Start did not use the applied configuration: %s", c.Detail())
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.Apply(config.Console{Enabled: true, Listen: "127.0.0.1:0"})
	if c.Addr() != nil {
		t.Error("serving after Stop")
	}
}

func TestLoopbackListenerRefusesOtherAddresses(t *testing.T) {
	if ln, err := loopbackListener("0.0.0.0:0")(context.Background()); err == nil {
		_ = ln.Close()
		t.Error("listening on 0.0.0.0: no error")
	} else if !strings.Contains(err.Error(), "not a loopback address") {
		t.Errorf("error = %v", err)
	}
	ln, err := loopbackListener("127.0.0.1:0")(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = ln.Close()
}

func TestApplyKeepsServingWhenAMoveFails(t *testing.T) {
	logs := &syncBuffer{}
	a := freeAddr(t)
	c := newConsole(t, config.Console{Enabled: true, Listen: a}, logs)
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	b := taken.Addr().String()

	c.Apply(config.Console{Enabled: true, Listen: b})
	want := "serving at http://" + a + "/, not at " + b + ": "
	if c.Addr() == nil || c.Addr().String() != a || !strings.HasPrefix(c.Detail(), want) ||
		!strings.Contains(c.Detail(), "address already in use") || refused(a) {
		t.Fatalf("after a failed move: addr %v, detail %q; want still serving at %s", c.Addr(), c.Detail(), a)
	}
	if !strings.Contains(logs.String(), "console not moved; it keeps serving at its old address") {
		t.Errorf("failed move not logged:\n%s", logs)
	}

	// Undoing the move keeps the console where it is, without an error.
	c.Apply(config.Console{Enabled: true, Listen: a})
	if c.Detail() != "serving at http://"+a+"/" {
		t.Errorf("after undoing the move: detail %q", c.Detail())
	}

	// Once the port is free, the move succeeds and the old address closes.
	c.Apply(config.Console{Enabled: true, Listen: b})
	_ = taken.Close()
	c.Apply(config.Console{Enabled: true, Listen: b})
	if c.Addr() == nil || c.Addr().String() != b || c.Detail() != "serving at http://"+b+"/" || !refused(a) {
		t.Errorf("after the port was freed: addr %v, detail %q, old refused %v", c.Addr(), c.Detail(), refused(a))
	}
}
