// Package setup is the core of the first-run assistant, `obied setup`: the
// answers an operator gives, the short commented configuration file they
// render to, and writing that file without ever replacing one the operator
// did not agree to replace (ADR 0027). Asking the questions is left to the
// command line, so that the interactive and the non-interactive assistant
// share everything else.
package setup

import (
	"errors"
	"fmt"
	"net/netip"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	ma "github.com/multiformats/go-multiaddr"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
)

// Directories the shipped systemd unit lets obied write to
// (StateDirectory and LogsDirectory); ProtectSystem=strict makes every
// other path read-only for it.
const (
	ServiceStateDir = "/var/lib/obie"
	ServiceLogsDir  = "/var/log/obie"
)

// DefaultAuditLog is the audit log the assistant suggests: in the unit's
// logs directory, so that the shipped service can write it.
const DefaultAuditLog = ServiceLogsDir + "/audit.jsonl"

// DefaultWeight is the trust weight the assistant suggests for a peer. With
// the default threshold (1.8) and quorum (2), a peer of this weight never
// gets an address blocked on its own (documentation/operations/federation.md).
const DefaultWeight = 0.8

// Answers are an operator's answers to the questions of the assistant.
type Answers struct {
	// StateDir is node.state_dir.
	StateDir string
	// AuditLog is audit.path; empty means no audit log.
	AuditLog string
	// Peers are the nodes to connect to and trust: mesh.bootstrap and
	// trust.publishers.
	Peers []Peer
	// Mode is node.mode; enforce also selects the nftables backend.
	Mode config.Mode
	// Allow are the networks never to block: allowlist.cidrs.
	Allow []netip.Prefix
}

// Peer is a node this node connects to and whose verdicts it trusts.
type Peer struct {
	// Address is the peer's multiaddr, ending in /p2p/<PeerID>.
	Address string
	PeerID  string
	Name    string
	Weight  float64
}

// Defaults returns the answers the assistant suggests: the default state
// directory, an audit log, no peers, observe mode and no extra allow-list
// entries.
func Defaults() Answers {
	return Answers{StateDir: config.Default().Node.StateDir, AuditLog: DefaultAuditLog, Mode: config.ModeObserve}
}

// ParseStateDir parses the state directory answer: an absolute path.
func ParseStateDir(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !filepath.IsAbs(s) {
		return "", fmt.Errorf("%q is not an absolute path; give one such as %s", s, ServiceStateDir)
	}
	return filepath.Clean(s), nil
}

// ParseAuditLog parses the audit log answer: an absolute file path, or
// "none" for no audit log (returned as "").
func ParseAuditLog(s string) (string, error) {
	s = strings.TrimSpace(s)
	if strings.EqualFold(s, "none") {
		return "", nil
	}
	if !filepath.IsAbs(s) || strings.HasSuffix(s, "/") {
		return "", fmt.Errorf("%q is not an absolute file path; give one such as %s, or none", s, DefaultAuditLog)
	}
	return filepath.Clean(s), nil
}

// ParseMode parses the mode answer: observe or enforce.
func ParseMode(s string) (config.Mode, error) {
	switch mode := config.Mode(strings.ToLower(strings.TrimSpace(s))); mode {
	case config.ModeObserve, config.ModeEnforce:
		return mode, nil
	}
	return "", fmt.Errorf("%q is not a mode; give observe or enforce", s)
}

// ParseWeight parses a trust weight: a number from 0 to 1.
func ParseWeight(s string) (float64, error) {
	w, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || !(w >= 0 && w <= 1) {
		return 0, fmt.Errorf("%q is not a trust weight; give a number from 0 (no trust) to 1 (full trust), such as 0.8", s)
	}
	return w, nil
}

// ParseAllow parses a network never to block: an IP address or a CIDR
// range without host bits. A single address becomes a /32 or /128 range.
func ParseAllow(s string) (netip.Prefix, error) {
	return sovereignty.ParseEntry(s)
}

// ParsePeerAddress parses a peer's address: a multiaddr that ends in
// /p2p/<peer ID>, so that the connection is authenticated. The peer is
// named after the address's host and gets DefaultWeight.
func ParsePeerAddress(s string) (Peer, error) {
	s = strings.TrimSpace(s)
	const want = "a peer address is a multiaddr that ends in the peer's ID, such as " +
		"/dns4/obie.example.org/tcp/4001/p2p/12D3KooW..."
	m, err := ma.NewMultiaddr(s)
	if err != nil {
		return Peer{}, fmt.Errorf("%q is not a peer address (%w); %s", s, err, want)
	}
	transport, last := ma.SplitLast(m)
	if last == nil || last.Code() != ma.P_P2P || len(transport) == 0 {
		return Peer{}, fmt.Errorf("%q does not end in /p2p/<peer ID> after a network address; %s", s, want)
	}
	return Peer{Address: m.String(), PeerID: last.Value(), Name: hostOf(transport), Weight: DefaultWeight}, nil
}

// hostOf returns the IP address or DNS name of a transport multiaddr.
func hostOf(m ma.Multiaddr) string {
	for _, c := range m {
		switch c.Code() {
		case ma.P_IP4, ma.P_IP6, ma.P_DNS, ma.P_DNS4, ma.P_DNS6:
			return c.Value()
		}
	}
	return m.String()
}

// ParsePeer parses a peer given on the command line:
// "<address>[,name=<name>][,weight=<weight>]".
func ParsePeer(spec string) (Peer, error) {
	fields := strings.Split(spec, ",")
	p, err := ParsePeerAddress(fields[0])
	if err != nil {
		return Peer{}, err
	}
	for _, field := range fields[1:] {
		key, value, _ := strings.Cut(field, "=")
		switch strings.TrimSpace(key) {
		case "name":
			if p.Name, err = ParseName(value); err != nil {
				return Peer{}, err
			}
		case "weight":
			if p.Weight, err = ParseWeight(value); err != nil {
				return Peer{}, err
			}
		default:
			return Peer{}, fmt.Errorf("%q: unknown peer setting %q; give <address>[,name=<name>][,weight=<0..1>]", spec, field)
		}
	}
	return p, nil
}

// ParseName parses a peer's name: any text on one line.
func ParseName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsFunc(s, func(r rune) bool { return r < ' ' || r == 0x7f }) {
		return "", fmt.Errorf("%q is not a peer name; give a short name on one line, such as friend", s)
	}
	return s, nil
}

// ErrDuplicatePeer means a peer with the same peer ID was already added.
var ErrDuplicatePeer = errors.New("peer already added")

// AddPeer adds p, refusing a second peer with the same peer ID.
func (a *Answers) AddPeer(p Peer) error {
	if slices.ContainsFunc(a.Peers, func(q Peer) bool { return q.PeerID == p.PeerID }) {
		return fmt.Errorf("%w: peer ID %s", ErrDuplicatePeer, p.PeerID)
	}
	a.Peers = append(a.Peers, p)
	return nil
}

// AddAllow adds a network never to block; a network already listed is
// not added again.
func (a *Answers) AddAllow(p netip.Prefix) {
	if !slices.Contains(a.Allow, p) {
		a.Allow = append(a.Allow, p)
	}
}

// Notes are what the operator must do besides writing the file for the
// shipped systemd unit to work with these answers: it lets obied write
// only to ServiceStateDir and ServiceLogsDir.
func (a Answers) Notes() []string {
	var notes []string
	if a.StateDir != ServiceStateDir {
		notes = append(notes, fmt.Sprintf(
			"The shipped service lets obied write only to %s. For the state directory %s, create it and allow it:\n"+
				"  sudo install -d -o obie -g obie -m 0700 %s\n"+
				"  sudo systemctl edit obied    # add: [Service] ReadWritePaths=%s",
			ServiceStateDir, a.StateDir, a.StateDir, a.StateDir))
	}
	if dir := filepath.Dir(a.AuditLog); a.AuditLog != "" && dir != ServiceLogsDir {
		notes = append(notes, fmt.Sprintf(
			"The shipped service lets obied write logs only to %s. For the audit log %s, create its directory and allow it:\n"+
				"  sudo install -d -o obie -g obie -m 0750 %s\n"+
				"  sudo systemctl edit obied    # add: [Service] ReadWritePaths=%s",
			ServiceLogsDir, a.AuditLog, dir, dir))
	}
	return notes
}

// Protects reports whether a node configured with these answers never
// blocks addr: a built-in range, an allow-list entry or the literal IP
// address of a peer covers it. The node's own interface addresses and the
// DNS names of peers are left out, as they are only known at the start.
func (a Answers) Protects(addr netip.Addr) bool {
	entries := sovereignty.Builtin()
	for _, p := range a.Allow {
		entries = append(entries, sovereignty.Entry{Prefix: p, Source: sovereignty.SourceConfig})
	}
	for _, p := range a.Peers {
		if m, err := ma.NewMultiaddr(p.Address); err == nil {
			if ip, err := netip.ParseAddr(hostOf(m)); err == nil {
				entries = append(entries, sovereignty.Entry{Prefix: netip.PrefixFrom(ip, ip.BitLen()), Source: sovereignty.SourceBootstrap})
			}
		}
	}
	_, ok := sovereignty.NewAllowlist(entries...).Match(netip.PrefixFrom(addr, addr.BitLen()))
	return ok
}
