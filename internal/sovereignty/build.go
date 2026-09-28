package sovereignty

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"slices"
	"strings"
	"time"

	ma "github.com/multiformats/go-multiaddr"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
)

// ResolveTimeout bounds resolving the DNS names of the bootstrap peers.
const ResolveTimeout = 5 * time.Second

// maxFileLine bounds a line of an allow-list file.
const maxFileLine = 4096

// Env is how Build learns about the host. Nil fields use the real host.
type Env struct {
	// InterfaceAddrs lists the addresses of the host's network interfaces.
	InterfaceAddrs func() ([]netip.Addr, error)
	// LookupIP resolves host to addresses of network "ip", "ip4" or "ip6".
	LookupIP func(ctx context.Context, network, host string) ([]netip.Addr, error)
	// OmitDocumentationRanges leaves the documentation ranges out of the
	// built-in entries, so that multi-node tests can block documentation
	// addresses. Production nodes never set it.
	OmitDocumentationRanges bool
}

func (e Env) withDefaults() Env {
	if e.InterfaceAddrs == nil {
		e.InterfaceAddrs = interfaceAddrs
	}
	if e.LookupIP == nil {
		e.LookupIP = net.DefaultResolver.LookupNetIP
	}
	return e
}

func interfaceAddrs() ([]netip.Addr, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	out := make([]netip.Addr, 0, len(addrs))
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok {
			if addr, ok := netip.AddrFromSlice(n.IP); ok {
				out = append(out, addr.Unmap())
			}
		}
	}
	return out, nil
}

// Build returns the effective allow-list of cfg: the built-in ranges,
// allowlist.cidrs, the node's own addresses (from mesh.listen, with every
// interface address for an unspecified listen address), the IPs of the
// mesh.bootstrap peers and the entries of allowlist.files. A file that
// cannot be read or holds an invalid line is an error. Interface addresses
// and bootstrap names that cannot be determined are logged and skipped.
func Build(ctx context.Context, cfg *config.Config, env Env, log *slog.Logger) (*Allowlist, error) {
	env = env.withDefaults()
	entries := Builtin()
	if env.OmitDocumentationRanges {
		entries = slices.Clone(builtin)
	}
	for _, s := range cfg.Allowlist.CIDRs {
		p, err := ParseEntry(s)
		if err != nil {
			return nil, fmt.Errorf("allowlist.cidrs: %w", err)
		}
		entries = append(entries, Entry{Prefix: p, Source: SourceConfig})
	}
	files, err := ReadFiles(cfg.Allowlist.Files)
	if err != nil {
		return nil, err
	}
	entries = append(entries, files...)
	entries = append(entries, selfEntries(cfg.Mesh.Listen, env, log)...)
	entries = append(entries, bootstrapEntries(ctx, cfg.Mesh.Bootstrap, env, log)...)
	return NewAllowlist(entries...), nil
}

// ReadFiles reads the allow-list files at paths: one IP address or CIDR
// range per line, blank lines and "#" comments ignored.
func ReadFiles(paths []string) ([]Entry, error) {
	var out []Entry
	for _, path := range paths {
		entries, err := readFile(path)
		if err != nil {
			return nil, err
		}
		out = append(out, entries...)
	}
	return out, nil
}

func readFile(path string) ([]Entry, error) {
	f, err := os.Open(path) // #nosec G304 -- the operator configures the allow-list files.
	if err != nil {
		return nil, fmt.Errorf("allow-list file: %w", err)
	}
	defer func() { _ = f.Close() }()
	return parseFile(f, path)
}

// parseFile parses the allow-list file at path from r: one IP address or
// CIDR range per line, blank lines and "#" comments ignored.
func parseFile(r io.Reader, path string) ([]Entry, error) {
	var out []Entry
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 256), maxFileLine)
	for n := 1; sc.Scan(); n++ {
		line, _, _ := strings.Cut(sc.Text(), "#")
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		p, err := ParseEntry(line)
		if err != nil {
			return nil, fmt.Errorf("allow-list file %s:%d: %w", path, n, err)
		}
		out = append(out, Entry{Prefix: p, Source: SourceFile, Label: fmt.Sprintf("%s:%d", path, n)})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("allow-list file %s: %w", path, err)
	}
	return out, nil
}

// selfEntries returns the node's own addresses from its listen multiaddrs.
func selfEntries(listen []string, env Env, log *slog.Logger) []Entry {
	var out []Entry
	var ifaces []netip.Addr
	ifacesRead := false
	for _, s := range listen {
		for _, addr := range multiaddrIPs(s) {
			if !addr.IsUnspecified() {
				out = append(out, hostEntry(addr, SourceSelf, s))
				continue
			}
			if !ifacesRead {
				ifacesRead = true
				var err error
				if ifaces, err = env.InterfaceAddrs(); err != nil {
					log.Warn("cannot list the interface addresses for the allow-list; list public addresses in allowlist.cidrs", "error", err)
				}
			}
			for _, a := range ifaces {
				if a.Is4() == addr.Is4() {
					out = append(out, hostEntry(a, SourceSelf, "interface address, "+s))
				}
			}
		}
	}
	return out
}

// bootstrapEntries returns the IPs of the bootstrap peers, resolving DNS
// names.
func bootstrapEntries(ctx context.Context, bootstrap []string, env Env, log *slog.Logger) []Entry {
	var out []Entry
	for _, s := range bootstrap {
		for _, addr := range multiaddrIPs(s) {
			out = append(out, hostEntry(addr, SourceBootstrap, s))
		}
		network, host, ok := multiaddrDNS(s)
		if !ok {
			continue
		}
		rctx, cancel := context.WithTimeout(ctx, ResolveTimeout)
		addrs, err := env.LookupIP(rctx, network, host)
		cancel()
		if err != nil {
			log.Warn("cannot resolve a bootstrap peer for the allow-list; it stays unprotected until the next reload", "address", s, "error", err)
			continue
		}
		for _, a := range addrs {
			out = append(out, hostEntry(a.Unmap(), SourceBootstrap, s))
		}
	}
	return out
}

func hostEntry(addr netip.Addr, src Source, label string) Entry {
	return Entry{Prefix: netip.PrefixFrom(addr, addr.BitLen()), Source: src, Label: label}
}

// multiaddrIPs returns the /ip4 and /ip6 addresses of the multiaddr s;
// none if it does not parse (config validation reports that).
func multiaddrIPs(s string) []netip.Addr {
	m, err := ma.NewMultiaddr(s)
	if err != nil {
		return nil
	}
	var out []netip.Addr
	for _, c := range m {
		if c.Code() != ma.P_IP4 && c.Code() != ma.P_IP6 {
			continue
		}
		if addr, ok := netip.AddrFromSlice(c.RawValue()); ok {
			out = append(out, addr.Unmap())
		}
	}
	return out
}

// multiaddrDNS returns the lookup network and host name of a /dns, /dns4 or
// /dns6 multiaddr.
func multiaddrDNS(s string) (network, host string, ok bool) {
	m, err := ma.NewMultiaddr(s)
	if err != nil {
		return "", "", false
	}
	for _, c := range m {
		switch c.Code() {
		case ma.P_DNS:
			return "ip", c.Value(), true
		case ma.P_DNS4:
			return "ip4", c.Value(), true
		case ma.P_DNS6:
			return "ip6", c.Value(), true
		}
	}
	return "", "", false
}
