package setup

import (
	"bytes"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
)

const testPath = "/etc/obie/obie.yaml"

// federated returns answers with two peers, an allow-list entry and
// enforce mode.
func federated(t *testing.T) Answers {
	t.Helper()
	a := Answers{StateDir: "/var/lib/obie", AuditLog: "", Mode: config.ModeEnforce}
	for _, spec := range []string{
		"/dns4/obie.friend.example/tcp/4001/p2p/" + friendID + ",name=friend,weight=1",
		"/ip6/2001:db8::20/udp/4001/quic-v1/p2p/" + partnerID + ",weight=0.55",
	} {
		p, err := ParsePeer(spec)
		if err != nil {
			t.Fatal(err)
		}
		if err := a.AddPeer(p); err != nil {
			t.Fatal(err)
		}
	}
	a.AddAllow(netip.MustParsePrefix("198.51.100.0/24"))
	a.AddAllow(netip.MustParsePrefix("2001:db8::7/128"))
	return a
}

// TestRenderSetsTheAnswers checks that the rendered file is a valid
// configuration with exactly the answered keys changed from the defaults.
func TestRenderSetsTheAnswers(t *testing.T) {
	for name, a := range map[string]Answers{"defaults": Defaults(), "federated": federated(t)} {
		data, err := Render(a, testPath)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		cfg, err := config.Parse(data)
		if err != nil {
			t.Fatalf("%s: %v\n%s", name, err, data)
		}
		want := config.Default()
		want.Node.StateDir, want.Node.Mode, want.Audit.Path = a.StateDir, a.Mode, a.AuditLog
		want.Mesh.Bootstrap = []string{}
		want.Trust.Publishers = []config.Publisher{}
		want.Allowlist.CIDRs = []string{}
		for _, p := range a.Peers {
			want.Mesh.Bootstrap = append(want.Mesh.Bootstrap, p.Address)
			want.Trust.Publishers = append(want.Trust.Publishers, config.Publisher{PeerID: p.PeerID, Name: p.Name, Weight: p.Weight})
		}
		for _, p := range a.Allow {
			want.Allowlist.CIDRs = append(want.Allowlist.CIDRs, p.String())
		}
		if a.Mode == config.ModeEnforce {
			want.Enforce.Backend = config.BackendNFTables
		}
		if !reflect.DeepEqual(*cfg, want) {
			t.Errorf("%s: rendered configuration =\n%+v\nwant\n%+v\n%s", name, *cfg, want, data)
		}
	}
}

// TestRenderIsCommentedAndStable checks that the file explains itself, names
// its path and the next commands, and that the same answers give the same
// bytes.
func TestRenderIsCommentedAndStable(t *testing.T) {
	a := federated(t)
	first, err := Render(a, testPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(federated(t), testPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Error("the same answers render different files")
	}
	text := string(first)
	for _, want := range []string{
		"# OBIE node configuration, written by `obied setup`: /etc/obie/obie.yaml\n",
		"sudo obied --config /etc/obie/obie.yaml --check-config",
		"sudo obied self-check",
		ReferenceURL,
		"  # enforce: block what the node decides",
		"  backend: nftables\n",
		"    - \"198.51.100.0/24\"\n",
		"      weight: 0.55\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("rendered file lacks %q:\n%s", want, text)
		}
	}
	for _, section := range []string{"node", "mesh", "trust", "allowlist", "enforce", "audit"} {
		i := strings.Index(text, "\n"+section+":\n")
		if i < 0 || !strings.HasPrefix(text[i+len(section)+3:], "  #") {
			t.Errorf("section %s is missing or not explained by a comment", section)
		}
	}

	observe, err := Render(Defaults(), testPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(observe), "\nenforce:") || !strings.Contains(string(observe), "enforce.backend: nftables, then restart") {
		t.Errorf("observe mode renders the enforce section or does not say how to enforce:\n%s", observe)
	}
}

// TestRenderQuotesAnswers checks that no answer can add keys or comments.
func TestRenderQuotesAnswers(t *testing.T) {
	a := federated(t)
	a.Peers[0].Name = `x" # comment` + "\u2028" + `admin: {socket: /tmp/evil}`
	data, err := Render(a, testPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Trust.Publishers[0].Name != a.Peers[0].Name || cfg.Admin.Socket != config.Default().Admin.Socket {
		t.Errorf("name = %q, admin.socket = %q", cfg.Trust.Publishers[0].Name, cfg.Admin.Socket)
	}
}

func TestRenderRejectsInvalidAnswers(t *testing.T) {
	a := Defaults()
	a.StateDir = "relative"
	if _, err := Render(a, testPath); err == nil || !strings.Contains(err.Error(), "node.state_dir") {
		t.Errorf("Render with a relative state directory: %v", err)
	}
}
