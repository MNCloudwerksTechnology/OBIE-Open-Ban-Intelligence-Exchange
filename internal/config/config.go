// Package config defines, loads and validates the obied node configuration.
//
// The configuration is one YAML file. Every key is optional and falls back to
// the default in Default; unknown keys, wrong types and invalid values are
// reported as Problems naming the offending key path.
package config

import (
	"fmt"
	"os"
	"time"
)

// DefaultPath is the configuration file read when --config is not given.
const DefaultPath = "/etc/obie/obie.yaml"

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
	Trust     Trust     `yaml:"trust"`
	Decision  Decision  `yaml:"decision"`
	Allowlist Allowlist `yaml:"allowlist"`
	Enforce   Enforce   `yaml:"enforce"`
	Metrics   Metrics   `yaml:"metrics"`
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
}

// Allowlist lists networks that are never blocked.
type Allowlist struct {
	CIDRs []string `yaml:"cidrs"`
}

// Enforce configures the enforcement backend.
type Enforce struct {
	Backend           Backend  `yaml:"backend"`
	MaxEntries        int      `yaml:"max_entries"`
	ReconcileInterval Duration `yaml:"reconcile_interval"`
}

// Metrics configures the Prometheus and health endpoint listener.
type Metrics struct {
	Listen string `yaml:"listen"`
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
		},
		Trust:     Trust{Publishers: []Publisher{}, DefaultWeight: 0, LocalWeight: 1.0},
		Decision:  Decision{Threshold: 1.8, Quorum: 2, MaxTTL: Duration(30 * day), DefaultTTL: Duration(7 * day)},
		Allowlist: Allowlist{CIDRs: []string{}},
		Enforce:   Enforce{Backend: BackendDryRun, MaxEntries: 100000, ReconcileInterval: Duration(10 * time.Second)},
		Metrics:   Metrics{Listen: "127.0.0.1:9464"},
		Audit:     Audit{Path: ""},
		Log:       Log{Level: "info"},
	}
}

// Load reads the file at path, decodes it over Default and validates it.
// Configuration mistakes are returned as *Error listing every problem.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- the operator chooses the config path.
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return Parse(data)
}

// Parse decodes YAML data over Default and validates the result.
func Parse(data []byte) (*Config, error) {
	cfg := Default()
	lines, decodeProblems, err := decode(data, &cfg)
	if err != nil {
		return nil, err
	}
	// Values that failed to decode keep their defaults, so validation still
	// runs and every problem is reported in one pass.
	if err := cfg.validate(lines, decodeProblems); err != nil {
		return nil, err
	}
	return &cfg, nil
}
