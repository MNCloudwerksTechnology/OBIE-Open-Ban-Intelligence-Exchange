package sovereignty

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "allow.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func fakeEnv() Env {
	return Env{
		InterfaceAddrs: func() ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("185.0.0.10"), netip.MustParseAddr("2a01::10")}, nil
		},
		LookupIP: func(_ context.Context, network, host string) ([]netip.Addr, error) {
			if host == "seed.example.org" && network == "ip4" {
				return []netip.Addr{netip.MustParseAddr("185.0.0.30")}, nil
			}
			return nil, errors.New("no such host")
		},
	}
}

const peerID = "12D3KooWGzBX6MWMMz3kHmFfyT3vJxFoy4xQF8NbXN7xBAFhGyvd"

func TestBuild(t *testing.T) {
	file := writeFile(t, "# management\n185.0.1.0/24\n\n  185.0.2.7  # jump host\n2a01:1::/48\n")
	cfg := config.Default()
	cfg.Allowlist.CIDRs = []string{"185.0.3.0/24"}
	cfg.Allowlist.Files = []string{file}
	cfg.Mesh.Listen = []string{"/ip4/0.0.0.0/tcp/4001", "/ip6/2a01::99/udp/4001/quic-v1"}
	cfg.Mesh.Bootstrap = []string{
		"/ip4/185.0.0.20/tcp/4001/p2p/" + peerID,
		"/dns4/seed.example.org/tcp/4001/p2p/" + peerID,
		"/dns6/unknown.example.org/tcp/4001/p2p/" + peerID,
	}
	a, err := Build(context.Background(), &cfg, fakeEnv(), discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		addr   string
		source Source
		label  string
	}{
		{"185.0.3.9", SourceConfig, ""},
		{"185.0.1.1", SourceFile, file + ":2"},
		{"185.0.2.7", SourceFile, file + ":4"},
		{"2a01:1::1", SourceFile, file + ":5"},
		{"185.0.0.10", SourceSelf, "interface address, /ip4/0.0.0.0/tcp/4001"},
		{"2a01::99", SourceSelf, "/ip6/2a01::99/udp/4001/quic-v1"},
		{"185.0.0.20", SourceBootstrap, cfg.Mesh.Bootstrap[0]},
		{"185.0.0.30", SourceBootstrap, cfg.Mesh.Bootstrap[1]},
		{"127.0.0.1", SourceBuiltin, "loopback"},
	}
	for _, tt := range tests {
		e, ok := a.Match(netip.MustParsePrefix(addrPrefix(tt.addr)))
		if !ok || e.Source != tt.source || e.Label != tt.label {
			t.Errorf("%s: %+v, %v; want %s %q", tt.addr, e, ok, tt.source, tt.label)
		}
	}
	// The IPv6 interface address is not listed: only 0.0.0.0 was unspecified.
	for _, s := range []string{"2a01::10", "185.0.2.8", "185.0.0.31"} {
		if e, ok := a.Match(netip.MustParsePrefix(addrPrefix(s))); ok {
			t.Errorf("%s allow-listed by %+v", s, e)
		}
	}
}

func TestBuildErrors(t *testing.T) {
	cfg := config.Default()
	cfg.Allowlist.Files = []string{filepath.Join(t.TempDir(), "missing.txt")}
	if _, err := Build(context.Background(), &cfg, fakeEnv(), discardLogger()); err == nil || !strings.Contains(err.Error(), "missing.txt") {
		t.Errorf("missing file: %v", err)
	}
	bad := writeFile(t, "185.0.1.0/24\n185.0.1.0/33\n")
	cfg.Allowlist.Files = []string{bad}
	if _, err := Build(context.Background(), &cfg, fakeEnv(), discardLogger()); err == nil || !strings.Contains(err.Error(), bad+":2: invalid CIDR") {
		t.Errorf("bad line: %v", err)
	}
	long := writeFile(t, strings.Repeat("#", maxFileLine+1)+"\n")
	if _, err := ReadFiles([]string{long}); err == nil {
		t.Error("overlong line accepted")
	}
	cfg = config.Default()
	cfg.Allowlist.CIDRs = []string{"10.0.0.1/8"}
	if _, err := Build(context.Background(), &cfg, fakeEnv(), discardLogger()); err == nil {
		t.Error("invalid allowlist.cidrs accepted")
	}
	// Failing interface lookup is logged, not fatal.
	cfg = config.Default()
	env := fakeEnv()
	env.InterfaceAddrs = func() ([]netip.Addr, error) { return nil, errors.New("boom") }
	if _, err := Build(context.Background(), &cfg, env, discardLogger()); err != nil {
		t.Errorf("interface failure: %v", err)
	}
}

func TestBuildDefaultEnv(t *testing.T) {
	cfg := config.Default()
	a, err := Build(context.Background(), &cfg, Env{}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Entries()) < len(Builtin()) {
		t.Errorf("entries = %d", len(a.Entries()))
	}
}
