package config

import (
	"fmt"
	"math"
	"net"
	"net/netip"
	"path/filepath"
	"strconv"

	ma "github.com/multiformats/go-multiaddr"

	"github.com/MNCloudwerksTechnology/obie/internal/logging"
)

// Validate checks every value of c and reports all problems as *Error.
func (c *Config) Validate() error {
	return c.validate(nil, nil)
}

// validator records problems, locating them through the decoded lines. It
// adds nothing for a path that already has a decoding problem.
type validator struct {
	problems problems
	lines    lineMap
	reported map[string]bool
}

func (v *validator) addf(path, format string, args ...any) {
	if !v.reported[path] {
		v.problems.addf(path, v.lines[path], format, args...)
	}
}

// validate checks c after decoding produced lines and decodeProblems, and
// reports those problems together with its own.
func (c *Config) validate(lines lineMap, decodeProblems problems) error {
	v := &validator{lines: lines, problems: decodeProblems, reported: make(map[string]bool, len(decodeProblems))}
	for _, p := range decodeProblems {
		v.reported[p.Path] = true
	}

	v.absPath("node.state_dir", c.Node.StateDir, false)
	v.oneOf("node.mode", string(c.Node.Mode), string(ModeObserve), string(ModeEnforce))

	v.absPath("admin.socket", c.Admin.Socket, false)
	v.nonEmpty("admin.socket_group", c.Admin.SocketGroup)

	for i, addr := range c.Mesh.Listen {
		v.listenAddr(fmt.Sprintf("mesh.listen[%d]", i), addr)
	}
	for i, addr := range c.Mesh.Bootstrap {
		v.bootstrapAddr(fmt.Sprintf("mesh.bootstrap[%d]", i), addr)
	}

	v.publishers(c.Trust.Publishers)
	v.weight("trust.default_weight", c.Trust.DefaultWeight)
	v.weight("trust.local_weight", c.Trust.LocalWeight)

	if !(c.Decision.Threshold > 0) || math.IsInf(c.Decision.Threshold, 0) {
		v.addf("decision.threshold", "must be a number greater than 0, got %v", c.Decision.Threshold)
	}
	if c.Decision.Quorum < 1 {
		v.addf("decision.quorum", "must be at least 1, got %d", c.Decision.Quorum)
	}
	maxOK := v.positive("decision.max_ttl", c.Decision.MaxTTL)
	defaultOK := v.positive("decision.default_ttl", c.Decision.DefaultTTL)
	if maxOK && defaultOK && c.Decision.DefaultTTL > c.Decision.MaxTTL {
		v.addf("decision.default_ttl", "must not exceed decision.max_ttl (%s), got %s", c.Decision.MaxTTL, c.Decision.DefaultTTL)
	}

	for i, cidr := range c.Allowlist.CIDRs {
		v.cidr(fmt.Sprintf("allowlist.cidrs[%d]", i), cidr)
	}

	v.oneOf("enforce.backend", string(c.Enforce.Backend), string(BackendDryRun), string(BackendNFTables))
	if c.Enforce.MaxEntries < 1 {
		v.addf("enforce.max_entries", "must be at least 1, got %d", c.Enforce.MaxEntries)
	}
	v.positive("enforce.reconcile_interval", c.Enforce.ReconcileInterval)

	v.hostPort("metrics.listen", c.Metrics.Listen)
	v.absPath("audit.path", c.Audit.Path, true)

	if _, err := logging.ParseLevel(c.Log.Level); err != nil {
		v.addf("log.level", "%v", err)
	}
	return v.problems.err()
}

func (v *validator) nonEmpty(path, value string) bool {
	if value == "" {
		v.addf(path, "must not be empty")
		return false
	}
	return true
}

// absPath requires an absolute path; optional paths may also be empty.
func (v *validator) absPath(path, value string, optional bool) {
	if optional && value == "" {
		return
	}
	if v.nonEmpty(path, value) && !filepath.IsAbs(value) {
		v.addf(path, "must be an absolute path, got %q", value)
	}
}

func (v *validator) oneOf(path, value string, allowed ...string) {
	for _, a := range allowed {
		if value == a {
			return
		}
	}
	v.addf(path, "must be one of %q, got %q", allowed, value)
}

func (v *validator) weight(path string, w float64) {
	if !(w >= 0 && w <= 1) {
		v.addf(path, "must be between 0 and 1, got %v", w)
	}
}

func (v *validator) positive(path string, d Duration) bool {
	if d <= 0 {
		v.addf(path, "must be greater than 0, got %s", d)
		return false
	}
	return true
}

func (v *validator) publishers(pubs []Publisher) {
	firstIndex := make(map[string]int, len(pubs))
	for i, p := range pubs {
		prefix := fmt.Sprintf("trust.publishers[%d]", i)
		if v.peerID(prefix+".peer_id", p.PeerID) {
			if first, dup := firstIndex[p.PeerID]; dup {
				v.addf(prefix+".peer_id", "duplicate publisher %s (already listed as trust.publishers[%d])", p.PeerID, first)
			} else {
				firstIndex[p.PeerID] = i
			}
		}
		v.nonEmpty(prefix+".name", p.Name)
		v.weight(prefix+".weight", p.Weight)
	}
}

func (v *validator) peerID(path, id string) bool {
	if !v.nonEmpty(path, id) {
		return false
	}
	if _, err := ma.NewComponent("p2p", id); err != nil {
		v.addf(path, "invalid peer ID %q: %v", id, err)
		return false
	}
	return true
}

// listenAddr requires a multiaddr without a /p2p component: a node listens
// under its own identity.
func (v *validator) listenAddr(path, addr string) {
	m, err := ma.NewMultiaddr(addr)
	if err != nil {
		v.addf(path, "invalid multiaddr: %v", err)
		return
	}
	if _, err := m.ValueForProtocol(ma.P_P2P); err == nil {
		v.addf(path, "listen address %q must not contain /p2p", addr)
	}
}

// bootstrapAddr requires a multiaddr ending in /p2p/<peer-id> so the peer
// can be authenticated when dialed.
func (v *validator) bootstrapAddr(path, addr string) {
	m, err := ma.NewMultiaddr(addr)
	if err != nil {
		v.addf(path, "invalid multiaddr: %v", err)
		return
	}
	transport, last := ma.SplitLast(m)
	if last == nil || last.Code() != ma.P_P2P || len(transport) == 0 {
		v.addf(path, "bootstrap address %q must be a transport address ending in /p2p/<peer-id>", addr)
	}
}

func (v *validator) cidr(path, value string) {
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		v.addf(path, "invalid CIDR %q: want e.g. 192.0.2.0/24 or 2001:db8::/32", value)
		return
	}
	if masked := prefix.Masked(); masked != prefix {
		v.addf(path, "CIDR %q has host bits set; did you mean %s?", value, masked)
	}
}

func (v *validator) hostPort(path, value string) {
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		v.addf(path, "must be host:port, got %q", value)
		return
	}
	if host != "" {
		if _, err := netip.ParseAddr(host); err != nil {
			v.addf(path, "host %q must be an IP address", host)
		}
	}
	if n, err := strconv.ParseUint(port, 10, 16); err != nil || n == 0 {
		v.addf(path, "port %q must be a number between 1 and 65535", port)
	}
}
