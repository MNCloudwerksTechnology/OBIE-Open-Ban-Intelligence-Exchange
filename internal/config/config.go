// Package config defines, loads and validates the obied node configuration.
//
// The configuration is one YAML file. Every key is optional and falls back to
// the default in Default; unknown keys, wrong types and invalid values are
// reported as Problems naming the offending key path.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// DefaultPath is the configuration file read when --config is not given.
const DefaultPath = "/etc/obie/obie.yaml"

// PathFlag returns the flag that selects the configuration file at path,
// for a command shown to the operator: nothing for DefaultPath, else
// " --config" and the path as one shell word.
func PathFlag(path string) string {
	if path == DefaultPath {
		return ""
	}
	return " --config " + QuotePath(path)
}

// QuotePath returns path as one POSIX shell word: as it is when the shell
// leaves each of its characters alone, else in single quotes.
func QuotePath(path string) string {
	const plain = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789@%+=:,./_-"
	if path != "" && !strings.ContainsFunc(path, func(r rune) bool { return !strings.ContainsRune(plain, r) }) {
		return path
	}
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}

// Mode selects whether decisions are only recorded or also enforced.
type Mode string

// Node modes.
const (
	ModeObserve Mode = "observe"
	ModeEnforce Mode = "enforce"
)

// Backend selects the enforcement backend.
type Backend string

// Enforcement backends.
const (
	BackendDryRun   Backend = "dryrun"
	BackendNFTables Backend = "nftables"
)

// Config is the complete node configuration.
type Config struct {
	Node      Node      `yaml:"node"`
	Admin     Admin     `yaml:"admin"`
	Mesh      Mesh      `yaml:"mesh"`
	Store     Store     `yaml:"store"`
	Trust     Trust     `yaml:"trust"`
	Decision  Decision  `yaml:"decision"`
	Allowlist Allowlist `yaml:"allowlist"`
	Enforce   Enforce   `yaml:"enforce"`
	Metrics   Metrics   `yaml:"metrics"`
	Console   Console   `yaml:"console"`
	Audit     Audit     `yaml:"audit"`
	Log       Log       `yaml:"log"`
}

// Node holds process-wide node settings.
type Node struct {
	StateDir string `yaml:"state_dir"`
	Mode     Mode   `yaml:"mode"`
	// ShutdownTimeout bounds the graceful shutdown on SIGTERM/SIGINT.
	ShutdownTimeout Duration `yaml:"shutdown_timeout"`
}

// Admin configures the local admin API used by obiectl.
type Admin struct {
	Socket      string `yaml:"socket"`
	SocketGroup string `yaml:"socket_group"`
}

// Mesh configures the libp2p mesh.
type Mesh struct {
	// Listen are the multiaddrs the node listens on (without /p2p).
	Listen []string `yaml:"listen"`
	// Bootstrap are static peers to dial; each ends in /p2p/<peer-id>.
	Bootstrap []string `yaml:"bootstrap"`
	// RateLimit bounds the events the node accepts from the mesh.
	RateLimit RateLimit `yaml:"rate_limit"`
}

// RateLimit configures the token buckets that bound the events accepted
// from the mesh. Events beyond a limit are dropped without penalizing the
// forwarding peer.
type RateLimit struct {
	// Publisher bounds the events of each publisher.
	Publisher TokenBucket `yaml:"publisher"`
	// Peer bounds the events each directly connected peer forwards. It must
	// exceed Publisher: a peer relays the events of every publisher, up to
	// Publisher each.
	Peer TokenBucket `yaml:"peer"`
}

// TokenBucket is a rate limit: EventsPerSecond on average, at most Burst at
// once.
type TokenBucket struct {
	EventsPerSecond float64 `yaml:"events_per_second"`
	Burst           int     `yaml:"burst"`
}

// Store bounds the local event store.
type Store struct {
	// MaxIndicators caps the verdicts held: one per publisher and
	// indicator. Beyond it the verdict expiring first is evicted; this
	// node's own verdicts never are.
	MaxIndicators int `yaml:"max_indicators"`
}

// Trust assigns trust weights to verdict publishers.
type Trust struct {
	Publishers    []Publisher `yaml:"publishers"`
	DefaultWeight float64     `yaml:"default_weight"`
	LocalWeight   float64     `yaml:"local_weight"`
}

// Publisher is a remote node whose verdicts are trusted with Weight.
type Publisher struct {
	PeerID string  `yaml:"peer_id,required"`
	Name   string  `yaml:"name,required"`
	Weight float64 `yaml:"weight,required"`
}

// Decision configures the weighted-consensus decision.
type Decision struct {
	Threshold  float64  `yaml:"threshold"`
	Quorum     int      `yaml:"quorum"`
	MaxTTL     Duration `yaml:"max_ttl"`
	DefaultTTL Duration `yaml:"default_ttl"`
	// LocalAutoblock lets this node's own ban verdicts block on their own,
	// without threshold and quorum.
	LocalAutoblock bool `yaml:"local_autoblock"`
}

// Allowlist lists networks that are never blocked, in addition to the
// built-in ranges, the node's own addresses and its bootstrap peers.
type Allowlist struct {
	CIDRs []string `yaml:"cidrs"`
	// Files are absolute paths of files with one IP address or CIDR range
	// per line; they are read at start and on every reload (SIGHUP).
	Files []string `yaml:"files"`
}

// Enforce configures the enforcement backend.
type Enforce struct {
	Backend           Backend  `yaml:"backend"`
	MaxEntries        int      `yaml:"max_entries"`
	ReconcileInterval Duration `yaml:"reconcile_interval"`
	NFTables          NFTables `yaml:"nftables"`
}

// NFTables configures the nftables backend (enforce.backend nftables).
type NFTables struct {
	// Forward also drops blocked sources in a forward chain, for routers
	// and container hosts.
	Forward bool `yaml:"forward"`
	// TeardownOnStop makes `obied teardown-firewall --on-stop` (the
	// systemd unit's ExecStopPost) remove the table; by default the
	// blocks stay until their timeout.
	TeardownOnStop bool `yaml:"teardown_on_stop"`
}

// Metrics configures the Prometheus and health endpoint listener.
type Metrics struct {
	Listen string `yaml:"listen"`
}

// Console configures the local web console (ADR 0019).
type Console struct {
	// Enabled serves the console; it is off unless the operator switches
	// it on.
	Enabled bool `yaml:"enabled"`
	// Listen is the loopback ip:port the console listens on; it is never
	// reachable from another host.
	Listen string `yaml:"listen"`
	// Actions lets the console carry out the operator actions of obiectl —
	// allow, block, unoverride, report, revoke — after a confirmation; off,
	// the console is read-only (ADR 0026).
	Actions bool `yaml:"actions"`
}

// Audit configures the JSON decision audit log.
type Audit struct {
	// Path is the audit log file; empty disables the audit log.
	Path string `yaml:"path"`
}

// Log configures process logging.
type Log struct {
	Level string `yaml:"level"`
}

// Default returns the configuration used for every key the file omits.
func Default() Config {
	return Config{
		Node:  Node{StateDir: "/var/lib/obie", Mode: ModeObserve, ShutdownTimeout: Duration(10 * time.Second)},
		Admin: Admin{Socket: "/run/obie/obie.sock", SocketGroup: "obie"},
		Mesh: Mesh{
			Listen: []string{
				"/ip4/0.0.0.0/tcp/4001",
				"/ip4/0.0.0.0/udp/4001/quic-v1",
				"/ip6/::/tcp/4001",
				"/ip6/::/udp/4001/quic-v1",
			},
			Bootstrap: []string{},
			RateLimit: RateLimit{
				Publisher: TokenBucket{EventsPerSecond: 10, Burst: 50},
				// A peer relays the events of many publishers.
				Peer: TokenBucket{EventsPerSecond: 50, Burst: 250},
			},
		},
		Store:     Store{MaxIndicators: 1_000_000},
		Trust:     Trust{Publishers: []Publisher{}, DefaultWeight: 0, LocalWeight: 1.0},
		Decision:  Decision{Threshold: 1.8, Quorum: 2, MaxTTL: Duration(30 * day), DefaultTTL: Duration(7 * day), LocalAutoblock: true},
		Allowlist: Allowlist{CIDRs: []string{}, Files: []string{}},
		Enforce:   Enforce{Backend: BackendDryRun, MaxEntries: 100000, ReconcileInterval: Duration(10 * time.Second)},
		Metrics:   Metrics{Listen: "127.0.0.1:9464"},
		Console:   Console{Enabled: false, Listen: "127.0.0.1:9465", Actions: true},
		Audit:     Audit{Path: ""},
		Log:       Log{Level: "info"},
	}
}

// Load reads the file at path, decodes it over Default and validates it.
// Configuration mistakes are returned as *Error listing every problem.
func Load(path string) (*Config, error) {
	f, err := LoadFile(path)
	if err != nil {
		return nil, err
	}
	return f.Config, nil
}

// File is a configuration file as obied read it.
type File struct {
	// Path is where the file was read.
	Path string
	// Config is the configuration the file sets over Default.
	Config *Config
	// set records the key paths the file sets.
	set lineMap
}

// Sets reports whether the file sets key rather than leaving it to its
// default. A nil File sets nothing.
func (f *File) Sets(key string) bool {
	if f == nil {
		return false
	}
	_, ok := f.set[key]
	return ok
}

// LoadFile reads, decodes and validates the file at path like Load, and
// also returns which keys it sets.
func LoadFile(path string) (*File, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- the operator chooses the config path.
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg, lines, err := parse(data)
	if err != nil {
		return nil, err
	}
	return &File{Path: path, Config: cfg, set: lines}, nil
}

// Parse decodes YAML data over Default and validates the result.
func Parse(data []byte) (*Config, error) {
	cfg, _, err := parse(data)
	return cfg, err
}

// parse is Parse, also returning the file line of every key path the data
// sets.
func parse(data []byte) (*Config, lineMap, error) {
	cfg := Default()
	lines, decodeProblems, err := decode(data, &cfg)
	if err != nil {
		return nil, nil, err
	}
	// Values that failed to decode keep their defaults, so validation still
	// runs and every problem is reported in one pass.
	if err := cfg.validate(lines, decodeProblems); err != nil {
		return nil, nil, err
	}
	return &cfg, lines, nil
}
