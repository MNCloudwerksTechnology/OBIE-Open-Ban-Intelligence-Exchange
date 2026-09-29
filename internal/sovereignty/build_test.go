package sovereignty

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
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
	content := "# management\n185.0.1.0/24\n\n  185.0.2.7  # jump host\n2a01:1::/48\n"
	file := writeFile(t, content)
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
	// It remembers what it loaded from the file, and the name it could not
	// resolve (ADR 0024).
	if got, want := a.Files(), []FileLoad{{Path: file, Entries: 3, Digest: sha256.Sum256([]byte(content))}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Files = %+v, want %+v", got, want)
	}
	w := a.Warnings()
	if len(w) != 1 || w[0].Source != SourceBootstrap || w[0].Subject != cfg.Mesh.Bootstrap[2] || w[0].Err == nil {
		t.Errorf("Warnings = %+v", w)
	}
}

func TestBuildOmitDocumentationRanges(t *testing.T) {
	cfg := config.Default()
	for _, omit := range []bool{false, true} {
		env := fakeEnv()
		env.OmitDocumentationRanges = omit
		a, err := Build(context.Background(), &cfg, env, discardLogger())
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range []string{"192.0.2.1/32", "198.51.100.7/32", "203.0.113.7/32", "2001:db8::1/128", "3fff::1/128"} {
			if _, ok := a.Match(netip.MustParsePrefix(s)); ok == omit {
				t.Errorf("omit %v: %s allow-listed = %v", omit, s, ok)
			}
		}
		if _, ok := a.Match(netip.MustParsePrefix("127.0.0.1/32")); !ok {
			t.Errorf("omit %v: loopback not allow-listed", omit)
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
	a, err := Build(context.Background(), &cfg, env, discardLogger())
	if err != nil {
		t.Fatalf("interface failure: %v", err)
	}
	if w := a.Warnings(); len(w) != 1 || w[0].Source != SourceSelf || w[0].Subject != cfg.Mesh.Listen[0] || w[0].Err.Error() != "boom" {
		t.Errorf("Warnings = %+v", w)
	}
}

// TestCheckFile: a file is read without loading it, with every line it
// rejects, so that the console can warn before a reload fails (ADR 0024).
func TestCheckFile(t *testing.T) {
	content := "185.0.1.0/24\n2a01:1::/48\n"
	clean := writeFile(t, content)
	if got := CheckFile(clean); got.Err != nil || got.Entries != 2 || got.RejectedLines != 0 ||
		got.Digest != sha256.Sum256([]byte(content)) || got.LoadErr(clean) != nil {
		t.Errorf("clean file: %+v", got)
	}
	missing := CheckFile(filepath.Join(t.TempDir(), "missing.txt"))
	if missing.Err == nil || !errors.Is(missing.Err, os.ErrNotExist) || !strings.Contains(missing.Err.Error(), "missing.txt") ||
		!errors.Is(missing.LoadErr("missing.txt"), missing.Err) {
		t.Errorf("missing file: %+v", missing)
	}
	bad := "185.0.1.0/24\n185.0.1.0/33 # typo\n" + strings.Repeat("nope\n", MaxRejected+3) + "185.0.2.7\n"
	badPath := writeFile(t, bad)
	got := CheckFile(badPath)
	if got.Err != nil || got.Entries != 2 || got.RejectedLines != MaxRejected+4 || len(got.Rejected) != MaxRejected {
		t.Fatalf("file with bad lines: %+v", got)
	}
	// Loading the file fails with its first rejected line, as a reload does.
	if _, err := ReadFiles([]string{badPath}); err == nil || got.LoadErr(badPath).Error() != err.Error() ||
		!strings.HasPrefix(err.Error(), "allow-list file "+badPath+":2: invalid CIDR") {
		t.Errorf("load error = %v, check says %v", err, got.LoadErr(badPath))
	}
	if r := got.Rejected[0]; r.Line != 2 || r.Text != "185.0.1.0/33" || !strings.Contains(r.Err.Error(), "invalid CIDR") {
		t.Errorf("first rejected line = %+v", r)
	}
	if r := got.Rejected[1]; r.Line != 3 || r.Text != "nope" {
		t.Errorf("second rejected line = %+v", r)
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
