package config

import (
	"math"
	"testing"
	"time"
)

func TestValidateRules(t *testing.T) {
	pub := func(id, name string, w float64) Publisher { return Publisher{PeerID: id, Name: name, Weight: w} }
	tests := []struct {
		name   string
		mutate func(*Config)
		path   string // empty: the config must be valid
		msg    string
	}{
		// node
		{"state_dir empty", func(c *Config) { c.Node.StateDir = "" }, "node.state_dir", "must not be empty"},
		{"state_dir relative", func(c *Config) { c.Node.StateDir = "var/lib/obie" }, "node.state_dir", "absolute path"},
		{"mode enforce", func(c *Config) { c.Node.Mode = ModeEnforce }, "", ""},
		{"mode unknown", func(c *Config) { c.Node.Mode = "block" }, "node.mode", `must be one of ["observe" "enforce"], got "block"`},
		{"shutdown_timeout zero", func(c *Config) { c.Node.ShutdownTimeout = 0 }, "node.shutdown_timeout", "greater than 0"},
		{"mode wrong case", func(c *Config) { c.Node.Mode = "Observe" }, "node.mode", "must be one of"},

		// admin
		{"socket empty", func(c *Config) { c.Admin.Socket = "" }, "admin.socket", "must not be empty"},
		{"socket relative", func(c *Config) { c.Admin.Socket = "obie.sock" }, "admin.socket", "absolute path"},
		{"socket_group empty", func(c *Config) { c.Admin.SocketGroup = "" }, "admin.socket_group", "must not be empty"},

		// mesh.listen
		{"listen empty list", func(c *Config) { c.Mesh.Listen = []string{} }, "", ""},
		{"listen invalid multiaddr", func(c *Config) { c.Mesh.Listen = []string{"0.0.0.0:4001"} }, "mesh.listen[0]", "invalid multiaddr"},
		{"listen unknown protocol", func(c *Config) { c.Mesh.Listen = []string{"/ip4/0.0.0.0/xtp/4001"} }, "mesh.listen[0]", "invalid multiaddr"},
		{"listen bad port", func(c *Config) { c.Mesh.Listen = []string{"/ip4/0.0.0.0/tcp/70000"} }, "mesh.listen[0]", "invalid multiaddr"},
		{"listen with p2p", func(c *Config) { c.Mesh.Listen = []string{"/ip4/0.0.0.0/tcp/4001/p2p/" + peerA} }, "mesh.listen[0]", "must not contain /p2p"},
		{"listen second entry", func(c *Config) { c.Mesh.Listen = []string{"/ip4/0.0.0.0/tcp/4001", "/ip4/1.2.3/tcp/1"} }, "mesh.listen[1]", "invalid multiaddr"},

		// mesh.bootstrap
		{"bootstrap ip4", func(c *Config) { c.Mesh.Bootstrap = []string{"/ip4/192.0.2.1/tcp/4001/p2p/" + peerA} }, "", ""},
		{"bootstrap dns quic", func(c *Config) {
			c.Mesh.Bootstrap = []string{"/dns4/seed.example.org/udp/4001/quic-v1/p2p/" + peerB}
		}, "", ""},
		{"bootstrap without peer id", func(c *Config) { c.Mesh.Bootstrap = []string{"/ip4/192.0.2.1/tcp/4001"} }, "mesh.bootstrap[0]", "ending in /p2p/<peer-id>"},
		{"bootstrap only peer id", func(c *Config) { c.Mesh.Bootstrap = []string{"/p2p/" + peerA} }, "mesh.bootstrap[0]", "transport address ending in /p2p/<peer-id>"},
		{"bootstrap p2p not last", func(c *Config) {
			c.Mesh.Bootstrap = []string{"/ip4/192.0.2.1/tcp/4001/p2p/" + peerA + "/p2p-circuit"}
		}, "mesh.bootstrap[0]", "ending in /p2p/<peer-id>"},
		{"bootstrap invalid peer id", func(c *Config) { c.Mesh.Bootstrap = []string{"/ip4/192.0.2.1/tcp/4001/p2p/not-a-peer"} }, "mesh.bootstrap[0]", "invalid multiaddr"},
		{"bootstrap garbage", func(c *Config) { c.Mesh.Bootstrap = []string{"seed.example.org:4001"} }, "mesh.bootstrap[0]", "invalid multiaddr"},

		// mesh.rate_limit
		{"rate limit fractional rate", func(c *Config) { c.Mesh.RateLimit.Publisher.EventsPerSecond = 0.5 }, "", ""},
		{"rate limit burst 1", func(c *Config) { c.Mesh.RateLimit.Peer.Burst = 1 }, "", ""},
		{"publisher rate zero", func(c *Config) { c.Mesh.RateLimit.Publisher.EventsPerSecond = 0 }, "mesh.rate_limit.publisher.events_per_second", "greater than 0"},
		{"peer rate negative", func(c *Config) { c.Mesh.RateLimit.Peer.EventsPerSecond = -1 }, "mesh.rate_limit.peer.events_per_second", "greater than 0"},
		{"peer rate NaN", func(c *Config) { c.Mesh.RateLimit.Peer.EventsPerSecond = math.NaN() }, "mesh.rate_limit.peer.events_per_second", "greater than 0"},
		{"publisher rate infinite", func(c *Config) {
			c.Mesh.RateLimit.Publisher.EventsPerSecond = math.Inf(1)
		}, "mesh.rate_limit.publisher.events_per_second", "greater than 0"},
		{"publisher burst zero", func(c *Config) { c.Mesh.RateLimit.Publisher.Burst = 0 }, "mesh.rate_limit.publisher.burst", "at least 1"},
		{"peer burst negative", func(c *Config) { c.Mesh.RateLimit.Peer.Burst = -5 }, "mesh.rate_limit.peer.burst", "at least 1"},

		// trust.publishers
		{"publishers valid", func(c *Config) {
			c.Trust.Publishers = []Publisher{pub(peerA, "a", 0), pub(peerB, "b", 1)}
		}, "", ""},
		{"publisher weight above 1", func(c *Config) { c.Trust.Publishers = []Publisher{pub(peerA, "a", 1.01)} }, "trust.publishers[0].weight", "between 0 and 1"},
		{"publisher weight negative", func(c *Config) { c.Trust.Publishers = []Publisher{pub(peerA, "a", -0.1)} }, "trust.publishers[0].weight", "between 0 and 1"},
		{"publisher weight NaN", func(c *Config) { c.Trust.Publishers = []Publisher{pub(peerA, "a", math.NaN())} }, "trust.publishers[0].weight", "between 0 and 1"},
		{"publisher name empty", func(c *Config) { c.Trust.Publishers = []Publisher{pub(peerA, "", 1)} }, "trust.publishers[0].name", "must not be empty"},
		{"publisher peer id empty", func(c *Config) { c.Trust.Publishers = []Publisher{pub("", "a", 1)} }, "trust.publishers[0].peer_id", "must not be empty"},
		{"publisher peer id invalid", func(c *Config) { c.Trust.Publishers = []Publisher{pub("12D3Koo!", "a", 1)} }, "trust.publishers[0].peer_id", "invalid peer ID"},
		{"publisher peer id is a multiaddr", func(c *Config) {
			c.Trust.Publishers = []Publisher{pub("/p2p/"+peerA, "a", 1)}
		}, "trust.publishers[0].peer_id", "invalid peer ID"},
		{"publisher duplicate", func(c *Config) {
			c.Trust.Publishers = []Publisher{pub(peerA, "a", 1), pub(peerB, "b", 1), pub(peerA, "c", 0.5)}
		}, "trust.publishers[2].peer_id", "duplicate publisher " + peerA + " (already listed as trust.publishers[0])"},

		// trust weights
		{"default_weight 1", func(c *Config) { c.Trust.DefaultWeight = 1 }, "", ""},
		{"default_weight negative", func(c *Config) { c.Trust.DefaultWeight = -1 }, "trust.default_weight", "between 0 and 1"},
		{"default_weight above 1", func(c *Config) { c.Trust.DefaultWeight = 2 }, "trust.default_weight", "between 0 and 1"},
		{"local_weight 0", func(c *Config) { c.Trust.LocalWeight = 0 }, "", ""},
		{"local_weight above 1", func(c *Config) { c.Trust.LocalWeight = 1.5 }, "trust.local_weight", "between 0 and 1"},
		{"local_weight infinite", func(c *Config) { c.Trust.LocalWeight = math.Inf(1) }, "trust.local_weight", "between 0 and 1"},

		// decision
		{"threshold small positive", func(c *Config) { c.Decision.Threshold = 0.01 }, "", ""},
		{"threshold zero", func(c *Config) { c.Decision.Threshold = 0 }, "decision.threshold", "greater than 0"},
		{"threshold negative", func(c *Config) { c.Decision.Threshold = -1 }, "decision.threshold", "greater than 0"},
		{"threshold NaN", func(c *Config) { c.Decision.Threshold = math.NaN() }, "decision.threshold", "greater than 0"},
		{"threshold infinite", func(c *Config) { c.Decision.Threshold = math.Inf(1) }, "decision.threshold", "greater than 0"},
		{"quorum 1", func(c *Config) { c.Decision.Quorum = 1 }, "", ""},
		{"quorum 0", func(c *Config) { c.Decision.Quorum = 0 }, "decision.quorum", "at least 1, got 0"},
		{"quorum negative", func(c *Config) { c.Decision.Quorum = -3 }, "decision.quorum", "at least 1"},
		{"max_ttl zero", func(c *Config) { c.Decision.MaxTTL = 0 }, "decision.max_ttl", "greater than 0"},
		{"default_ttl negative", func(c *Config) { c.Decision.DefaultTTL = Duration(-time.Hour) }, "decision.default_ttl", "greater than 0"},
		{"ttl equal", func(c *Config) { c.Decision.DefaultTTL = c.Decision.MaxTTL }, "", ""},
		{"ttl default above max", func(c *Config) { c.Decision.DefaultTTL = Duration(31 * day) }, "decision.default_ttl", "must not exceed decision.max_ttl (30d), got 31d"},

		// allowlist
		{"cidrs valid", func(c *Config) { c.Allowlist.CIDRs = []string{"10.0.0.0/8", "192.0.2.1/32", "2001:db8::/32"} }, "", ""},
		{"cidr without mask", func(c *Config) { c.Allowlist.CIDRs = []string{"10.0.0.1"} }, "allowlist.cidrs[0]", "invalid CIDR"},
		{"cidr bad mask", func(c *Config) { c.Allowlist.CIDRs = []string{"10.0.0.0/33"} }, "allowlist.cidrs[0]", "invalid CIDR"},
		{"cidr bad address", func(c *Config) { c.Allowlist.CIDRs = []string{"10.0.0.0/8", "300.0.0.0/8"} }, "allowlist.cidrs[1]", "invalid CIDR"},
		{"cidr host bits", func(c *Config) { c.Allowlist.CIDRs = []string{"10.1.2.3/8"} }, "allowlist.cidrs[0]", "did you mean 10.0.0.0/8?"},
		{"files valid", func(c *Config) { c.Allowlist.Files = []string{"/etc/obie/allow.txt"} }, "", ""},
		{"files relative", func(c *Config) { c.Allowlist.Files = []string{"/etc/obie/a.txt", "allow.txt"} }, "allowlist.files[1]", "absolute path"},
		{"files empty", func(c *Config) { c.Allowlist.Files = []string{""} }, "allowlist.files[0]", "must not be empty"},

		// enforce
		{"backend nftables", func(c *Config) { c.Enforce.Backend = BackendNFTables }, "", ""},
		{"backend unknown", func(c *Config) { c.Enforce.Backend = "iptables" }, "enforce.backend", `must be one of ["dryrun" "nftables"], got "iptables"`},
		{"max_entries 1", func(c *Config) { c.Enforce.MaxEntries = 1 }, "", ""},
		{"max_indicators 1", func(c *Config) { c.Store.MaxIndicators = 1 }, "", ""},
		{"max_indicators 0", func(c *Config) { c.Store.MaxIndicators = 0 }, "store.max_indicators", "at least 1"},
		{"ended_retention 1h", func(c *Config) { c.Store.EndedRetention = Duration(time.Hour) }, "", ""},
		{"ended_retention 365d", func(c *Config) { c.Store.EndedRetention = Duration(365 * day) }, "", ""},
		{"ended_retention 59m", func(c *Config) { c.Store.EndedRetention = Duration(59 * time.Minute) }, "store.ended_retention",
			"must lie between 1h and 365d, got 59m0s"},
		{"ended_retention 366d", func(c *Config) { c.Store.EndedRetention = Duration(366 * day) }, "store.ended_retention",
			"must lie between 1h and 365d, got 366d"},
		{"ended_retention 0", func(c *Config) { c.Store.EndedRetention = 0 }, "store.ended_retention", "got 0s"},
		{"max_entries 0", func(c *Config) { c.Enforce.MaxEntries = 0 }, "enforce.max_entries", "at least 1"},
		{"reconcile_interval zero", func(c *Config) { c.Enforce.ReconcileInterval = 0 }, "enforce.reconcile_interval", "greater than 0"},

		// metrics
		{"metrics ipv6", func(c *Config) { c.Metrics.Listen = "[::1]:9464" }, "", ""},
		{"metrics all interfaces", func(c *Config) { c.Metrics.Listen = ":9464" }, "", ""},
		{"metrics no port", func(c *Config) { c.Metrics.Listen = "127.0.0.1" }, "metrics.listen", "must be host:port"},
		{"metrics empty", func(c *Config) { c.Metrics.Listen = "" }, "metrics.listen", "must be host:port"},
		{"metrics hostname", func(c *Config) { c.Metrics.Listen = "localhost:9464" }, "metrics.listen", "must be an IP address"},
		{"metrics port zero", func(c *Config) { c.Metrics.Listen = "127.0.0.1:0" }, "metrics.listen", "between 1 and 65535"},
		{"metrics port too big", func(c *Config) { c.Metrics.Listen = "127.0.0.1:65536" }, "metrics.listen", "between 1 and 65535"},
		{"metrics port name", func(c *Config) { c.Metrics.Listen = "127.0.0.1:http" }, "metrics.listen", "between 1 and 65535"},

		// console
		{"console enabled", func(c *Config) { c.Console.Enabled = true }, "", ""},
		{"console loopback range", func(c *Config) { c.Console.Listen = "127.0.0.2:8080" }, "", ""},
		{"console ipv6 loopback", func(c *Config) { c.Console.Listen = "[::1]:9465" }, "", ""},
		{"console all interfaces", func(c *Config) { c.Console.Listen = ":9465" }, "console.listen",
			`":9465" listens on every interface and would expose the console to the network; use a loopback address such as 127.0.0.1:9465 and reach it from another machine through an SSH port forward instead: ssh -L 9465:127.0.0.1:9465 <this host>`},
		{"console unspecified", func(c *Config) { c.Console.Listen = "0.0.0.0:9465" }, "console.listen",
			"0.0.0.0 is not a loopback address and would expose the console to the network"},
		{"console unspecified ipv6", func(c *Config) { c.Console.Listen = "[::]:9465" }, "console.listen", ":: is not a loopback address"},
		{"console interface address", func(c *Config) { c.Console.Listen = "192.0.2.10:8443" }, "console.listen",
			"192.0.2.10 is not a loopback address and would expose the console to the network; the console is only reachable from this host: use 127.0.0.1 or ::1 and reach it from another machine through an SSH port forward instead: ssh -L 8443:127.0.0.1:8443 <this host>"},
		{"console hostname", func(c *Config) { c.Console.Listen = "localhost:9465" }, "console.listen", `host "localhost" must be a loopback IP address (127.0.0.1 or ::1), not a name`},
		{"console no port", func(c *Config) { c.Console.Listen = "127.0.0.1" }, "console.listen", `must be a loopback ip:port such as 127.0.0.1:9465, got "127.0.0.1"`},
		{"console empty", func(c *Config) { c.Console.Listen = "" }, "console.listen", "must be a loopback ip:port"},
		{"console port zero", func(c *Config) { c.Console.Listen = "127.0.0.1:0" }, "console.listen", "between 1 and 65535"},
		{"console port name", func(c *Config) { c.Console.Listen = "127.0.0.1:https" }, "console.listen", "between 1 and 65535"},

		// audit
		{"audit file", func(c *Config) { c.Audit.Path = "/var/log/obie/audit.jsonl" }, "", ""},
		{"audit relative", func(c *Config) { c.Audit.Path = "audit.jsonl" }, "audit.path", "absolute path"},

		// log
		{"level debug", func(c *Config) { c.Log.Level = "debug" }, "", ""},
		{"level unknown", func(c *Config) { c.Log.Level = "verbose" }, "log.level", "unknown log level"},
		{"level empty", func(c *Config) { c.Log.Level = "" }, "log.level", "unknown log level"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			tt.mutate(&cfg)
			err := cfg.Validate()
			if tt.path == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want valid", err)
				}
				return
			}
			wantProblem(t, err, tt.path, tt.msg)
			if n := len(problemsOf(t, err)); n != 1 {
				t.Errorf("got %d problems, want exactly 1: %v", n, err)
			}
		})
	}
}

func TestValidateTTLOrderingSkippedWhenInvalid(t *testing.T) {
	cfg := Default()
	cfg.Decision.MaxTTL = 0
	ps := problemsOf(t, cfg.Validate())
	if len(ps) != 1 || ps[0].Path != "decision.max_ttl" {
		t.Errorf("problems = %v, want only decision.max_ttl", ps)
	}
}

func TestValidateCollectsAllProblems(t *testing.T) {
	cfg := Default()
	cfg.Node.Mode = "x"
	cfg.Decision.Quorum = 0
	cfg.Allowlist.CIDRs = []string{"nope"}
	if n := len(problemsOf(t, cfg.Validate())); n != 3 {
		t.Errorf("got %d problems, want 3", n)
	}
}
