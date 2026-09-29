package setup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
)

// ReferenceURL is the configuration reference, which describes every key.
const ReferenceURL = "https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange/blob/main/documentation/operations/configuration.md"

// Render returns the configuration file for a, to be written at path: the
// answered keys with comments that explain them, every other key left to
// its default. The same answers always render the same bytes. The result
// is checked with config.Parse, so it is a configuration obied accepts.
func Render(a Answers, path string) ([]byte, error) {
	// The mode is written unquoted, as the example writes it, so it must be
	// one of the modes; the path appears in comments, so it must not end
	// them.
	if a.Mode != config.ModeObserve && a.Mode != config.ModeEnforce {
		return nil, fmt.Errorf("the answers do not make a valid configuration: node.mode must be %s or %s, got %q",
			config.ModeObserve, config.ModeEnforce, a.Mode)
	}
	if err := checkPathText(path); err != nil {
		return nil, err
	}
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	w("# OBIE node configuration, written by `obied setup`: %s\n", path)
	w("#\n")
	w("# It sets only what the setup assistant asked about; every other key keeps\n")
	w("# its default. /etc/obie/obie.yaml.example and the configuration reference\n")
	w("# describe every key:\n")
	w("# %s\n", ReferenceURL)
	w("#\n")
	w("# After a change, check the file, restart the node and check the node:\n")
	w("#\n")
	w("#   sudo obied --config %s --check-config\n", config.QuotePath(path))
	w("#   sudo systemctl restart obied\n")
	w("#   sudo obied self-check%s\n", config.PathFlag(path))

	w("\nnode:\n")
	w("  # The node's identity key (its name on the network: back it up) and its\n")
	w("  # database of verdicts live here.\n")
	w("  state_dir: %s\n", quote(a.StateDir))
	if a.Mode == config.ModeEnforce {
		w("  # enforce: block what the node decides, through its own nftables table.\n")
		w("  # observe (recommended to start with) only shows what it would block.\n")
	} else {
		w("  # observe: decide and show what would be blocked, but block nothing.\n")
		w("  # To block, set enforce here and enforce.backend: nftables, then restart.\n")
	}
	w("  mode: %s\n", a.Mode)

	w("\nmesh:\n")
	w("  # Peers the node connects to at start; each address ends in the peer's ID.\n")
	if len(a.Peers) == 0 {
		w("  # None: the node works on its own and acts only on what this server detects.\n")
		w("  bootstrap: []\n")
	} else {
		w("  bootstrap:\n")
		for _, p := range a.Peers {
			w("    - %s\n", quote(p.Address))
		}
	}

	w("\ntrust:\n")
	w("  # How much the node trusts each peer's verdicts, from 0 (not at all) to 1\n")
	w("  # (fully). With the default threshold (1.8) and quorum (2), a peer alone never\n")
	w("  # gets an address blocked; see documentation/operations/federation.md.\n")
	if len(a.Peers) == 0 {
		w("  publishers: []\n")
	} else {
		w("  publishers:\n")
		for _, p := range a.Peers {
			w("    - peer_id: %s\n", quote(p.PeerID))
			w("      name: %s\n", quote(p.Name))
			w("      weight: %s\n", strconv.FormatFloat(p.Weight, 'f', -1, 64))
		}
	}

	w("\nallowlist:\n")
	w("  # Networks that are never blocked, besides the private ranges, this server's\n")
	w("  # own addresses and its peers, which are always protected. Keep the networks\n")
	w("  # you administer this server from here, so that OBIE cannot lock you out.\n")
	if len(a.Allow) == 0 {
		w("  cidrs: []\n")
	} else {
		w("  cidrs:\n")
		for _, p := range a.Allow {
			w("    - %s\n", quote(p.String()))
		}
	}

	if a.Mode == config.ModeEnforce {
		w("\nenforce:\n")
		w("  # Block through the nftables table inet obie, and nothing else; the\n")
		w("  # shipped service grants the one capability it needs (CAP_NET_ADMIN).\n")
		w("  backend: %s\n", config.BackendNFTables)
	}

	w("\naudit:\n")
	w("  # Every decision, override and report as one JSON line, for you or your\n")
	w("  # SIEM; empty: no audit log. The node's own log goes to the journal:\n")
	w("  # journalctl -u obied\n")
	w("  path: %s\n", quote(a.AuditLog))

	data := []byte(b.String())
	if _, err := config.Parse(data); err != nil {
		return nil, fmt.Errorf("the answers do not make a valid configuration: %w", err)
	}
	return data, nil
}

// checkPathText refuses a configuration path with a control character or
// a YAML line break, which would end the comments that name it.
func checkPathText(path string) error {
	if strings.ContainsFunc(path, func(r rune) bool { return r < ' ' || r == 0x7f || r == 0x85 || r == 0x2028 || r == 0x2029 }) {
		return fmt.Errorf("the configuration path %q holds a line break or another control character", path)
	}
	return nil
}

// quote writes s as a YAML double-quoted scalar: JSON strings are valid
// YAML, so no answer can break out of its value.
func quote(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // Encoding a string cannot fail.
	return strings.TrimSuffix(buf.String(), "\n")
}
