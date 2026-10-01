package setup

import (
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
)

// Peer IDs of the documentation's examples.
const (
	friendID  = "12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf"
	partnerID = "12D3KooWGzBX6MWMMz3kHmFfyT3vJxFoy4xQF8NbXN7xBAFhGyvd"
)

func TestParsePeer(t *testing.T) {
	tests := []struct {
		spec string
		want Peer
	}{
		{"/dns4/obie.friend.example/tcp/4001/p2p/" + friendID,
			Peer{Address: "/dns4/obie.friend.example/tcp/4001/p2p/" + friendID, PeerID: friendID, Name: "obie.friend.example", Weight: DefaultWeight}},
		{" /ip4/198.51.100.20/tcp/4001/p2p/" + friendID + ",name=friend,weight=1 ",
			Peer{Address: "/ip4/198.51.100.20/tcp/4001/p2p/" + friendID, PeerID: friendID, Name: "friend", Weight: 1}},
		{"/ip6/2001:db8::20/udp/4001/quic-v1/p2p/" + partnerID + ",weight=0",
			Peer{Address: "/ip6/2001:db8::20/udp/4001/quic-v1/p2p/" + partnerID, PeerID: partnerID, Name: "2001:db8::20", Weight: 0}},
	}
	for _, tt := range tests {
		got, err := ParsePeer(tt.spec)
		if err != nil || got != tt.want {
			t.Errorf("ParsePeer(%q) = %+v, %v; want %+v", tt.spec, got, err, tt.want)
		}
	}
}

func TestParsePeerErrors(t *testing.T) {
	tests := []struct{ spec, want string }{
		{"obie.friend.example:4001", "is not a peer address"},
		{"/dns4/obie.friend.example/tcp/4001", "does not end in /p2p/<peer ID>"},
		{"/p2p/" + friendID, "does not end in /p2p/<peer ID> after a network address"},
		{"/ip4/198.51.100.20/tcp/4001/p2p/" + friendID + ",weight=2", "is not a trust weight"},
		{"/ip4/198.51.100.20/tcp/4001/p2p/" + friendID + ",name=", "is not a peer name"},
		{"/ip4/198.51.100.20/tcp/4001/p2p/" + friendID + ",trust=1", "unknown peer setting"},
	}
	for _, tt := range tests {
		if _, err := ParsePeer(tt.spec); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("ParsePeer(%q) error = %v, want %q", tt.spec, err, tt.want)
		}
	}
}

func TestParseAnswers(t *testing.T) {
	if got, err := ParseStateDir(" /srv/obie/ "); err != nil || got != "/srv/obie" {
		t.Errorf("ParseStateDir = %q, %v", got, err)
	}
	if _, err := ParseStateDir("var/lib/obie"); err == nil || !strings.Contains(err.Error(), "not an absolute path") {
		t.Errorf("ParseStateDir(relative) error = %v", err)
	}
	for in, want := range map[string]string{"none": "", " NONE ": "", "/var/log/obie/a.jsonl": "/var/log/obie/a.jsonl"} {
		if got, err := ParseAuditLog(in); err != nil || got != want {
			t.Errorf("ParseAuditLog(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "audit.jsonl", "/var/log/obie/"} {
		if _, err := ParseAuditLog(in); err == nil {
			t.Errorf("ParseAuditLog(%q) accepted", in)
		}
	}
	for in, want := range map[string]config.Mode{"observe": config.ModeObserve, " Enforce": config.ModeEnforce} {
		if got, err := ParseMode(in); err != nil || got != want {
			t.Errorf("ParseMode(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := ParseMode("block"); err == nil {
		t.Error("ParseMode(block) accepted")
	}
	for _, in := range []string{"-0.1", "1.5", "NaN", "high", ""} {
		if _, err := ParseWeight(in); err == nil {
			t.Errorf("ParseWeight(%q) accepted", in)
		}
	}
	for in, want := range map[string]string{"85.10.0.7": "85.10.0.7/32", "2001:db8::7": "2001:db8::7/128", "198.51.100.0/24": "198.51.100.0/24"} {
		if got, err := ParseAllow(in); err != nil || got.String() != want {
			t.Errorf("ParseAllow(%q) = %v, %v; want %s", in, got, err, want)
		}
	}
	if _, err := ParseAllow("198.51.100.7/24"); err == nil {
		t.Error("ParseAllow accepted a range with host bits")
	}
	if _, err := ParseName("two\nlines"); err == nil {
		t.Error("ParseName accepted a line break")
	}
}

func TestAddPeerAndAllow(t *testing.T) {
	a := Defaults()
	p, err := ParsePeerAddress("/dns4/obie.friend.example/tcp/4001/p2p/" + friendID)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.AddPeer(p); err != nil {
		t.Fatal(err)
	}
	p.Address = "/ip4/198.51.100.20/tcp/4001/p2p/" + friendID
	if err := a.AddPeer(p); !errors.Is(err, ErrDuplicatePeer) {
		t.Errorf("second peer with the same ID: %v, want ErrDuplicatePeer", err)
	}
	a.AddAllow(netip.MustParsePrefix("85.10.0.7/32"))
	a.AddAllow(netip.MustParsePrefix("85.10.0.7/32"))
	if len(a.Peers) != 1 || len(a.Allow) != 1 {
		t.Errorf("answers = %+v, want one peer and one network", a)
	}
}

func TestNotes(t *testing.T) {
	if notes := Defaults().Notes(); len(notes) != 0 {
		t.Errorf("default answers need notes: %q", notes)
	}
	if notes := (Answers{StateDir: ServiceStateDir}).Notes(); len(notes) != 0 {
		t.Errorf("no audit log needs notes: %q", notes)
	}
	a := Answers{StateDir: "/srv/obie", AuditLog: "/srv/log/audit.jsonl"}
	notes := a.Notes()
	if len(notes) != 2 || !strings.Contains(notes[0], "ReadWritePaths=/srv/obie") ||
		!strings.Contains(notes[1], "sudo install -d -o obie -g obie -m 0750 /srv/log") {
		t.Errorf("notes = %q", notes)
	}
}

func TestProtects(t *testing.T) {
	a := Defaults()
	p, err := ParsePeerAddress("/ip4/85.10.0.20/tcp/4001/p2p/" + friendID)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.AddPeer(p); err != nil {
		t.Fatal(err)
	}
	a.AddAllow(netip.MustParsePrefix("85.10.1.0/24"))
	for addr, want := range map[string]bool{
		"10.1.2.3":   true,  // private, built in
		"85.10.1.99": true,  // allow-list entry
		"85.10.0.20": true,  // a peer's address
		"85.10.0.7":  false, // nothing covers it
	} {
		if got := a.Protects(netip.MustParseAddr(addr)); got != want {
			t.Errorf("Protects(%s) = %v, want %v", addr, got, want)
		}
	}
}
