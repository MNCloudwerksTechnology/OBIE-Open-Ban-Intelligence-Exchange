package console

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
)

var ruleNow = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

// testOverrides are two overrides in effect, one of them a force-block a
// protected entry beats, and a force-allow.
func testOverrides() []Override {
	return []Override{
		{Range: netip.MustParsePrefix("203.0.113.7/32"), Action: ruleForceBlock, Note: "scanner", CreatedAt: ruleNow.Add(-2 * time.Hour),
			ExpiresAt: ruleNow.Add(22 * time.Hour)},
		{Range: netip.MustParsePrefix("198.51.100.0/24"), Action: ruleForceAllow, Note: "partner", CreatedAt: ruleNow.Add(-time.Hour)},
		{Range: netip.MustParsePrefix("192.168.1.10/32"), Action: ruleForceBlock, CreatedAt: ruleNow.Add(-3 * time.Hour),
			Overruled: &Ruling{Effect: "allow", Rule: ruleAllowlist, Source: "builtin", Protected: true, Match: "192.168.0.0/16",
				Label: "private (RFC 1918)"}},
	}
}

func TestParseOverridesQuery(t *testing.T) {
	q := parseOverridesQuery(url.Values{"kind": {"block"}, "address": {" 203.0.113.0/24 "}, "state": {"expired"}})
	if q != (overridesQuery{kind: kindBlock, address: "203.0.113.0/24", state: stateExpired}) {
		t.Errorf("query = %+v", q)
	}
	if got := q.href(); got != "/overrides?address=203.0.113.0%2F24&kind=block&state=expired" {
		t.Errorf("href = %s", got)
	}
	if q := parseOverridesQuery(url.Values{"kind": {"maybe"}, "state": {"gone"}}); q != (overridesQuery{}) || q.href() != "/overrides" {
		t.Errorf("unknown values = %+v", q)
	}
}

// TestBuildOverrides: the overrides in effect, the most recent first, with
// note, set and end times, and why a force-block has no effect (AC1).
func TestBuildOverrides(t *testing.T) {
	p := buildOverrides(overridesInput{now: ruleNow, active: testOverrides(), retention: 7 * 24 * time.Hour,
		expired: []Override{{Range: netip.MustParsePrefix("203.0.113.9/32"), Action: ruleForceBlock, ExpiresAt: ruleNow.Add(-time.Hour)}}})
	if p.Heading != "Overrides in effect" || p.Err != "" || p.Empty != "" || p.More != 0 || p.Retention != "7 days" {
		t.Errorf("page = %+v", p)
	}
	var addrs []string
	for _, r := range p.Rows {
		addrs = append(addrs, r.Address)
	}
	if want := []string{"198.51.100.0/24", "203.0.113.7", "192.168.1.10"}; !slices.Equal(addrs, want) {
		t.Errorf("rows = %v, want %v (the most recently set first)", addrs, want)
	}
	allow, block, beaten := p.Rows[0], p.Rows[1], p.Rows[2]
	if allow.Kind != kindAllow || allow.KindLabel != "Always allow" || !allow.Never || allow.Note != "partner" ||
		allow.DecisionHref != "/decisions/198.51.100.0/24" || allow.NoEffect != "" {
		t.Errorf("force-allow row = %+v", allow)
	}
	if block.Kind != kindBlock || block.Never || block.Ends.Text != "2026-09-29 10:00:00 UTC" || block.Set.Text != "2026-09-28 10:00:00 UTC" ||
		block.NoEffect != "" {
		t.Errorf("force-block row = %+v", block)
	}
	if beaten.NoEffect != "No effect: it is protected by the built-in range 192.168.0.0/16 (Private), "+
		"which not even an override blocks." {
		t.Errorf("overruled force-block: %q", beaten.NoEffect)
	}
	if !reflect.DeepEqual(p.States, []peerFilter{{Label: "In effect", Href: "/overrides", Count: 3, Current: true},
		{Label: "Expired", Href: "/overrides?state=expired", Count: 1}}) {
		t.Errorf("states = %+v", p.States)
	}
	if p.Clear != "" || !p.Kinds[0].Selected {
		t.Errorf("unfiltered: clear %q, kinds %+v", p.Clear, p.Kinds)
	}
}

// TestBuildOverridesFilters: the kind and an overlapping address or
// network narrow both tabs; the expired tab explains itself (AC1).
func TestBuildOverridesFilters(t *testing.T) {
	expired := []Override{
		{Range: netip.MustParsePrefix("203.0.113.9/32"), Action: ruleForceBlock, ExpiresAt: ruleNow.Add(-2 * time.Hour)},
		{Range: netip.MustParsePrefix("203.0.113.0/24"), Action: ruleForceAllow, ExpiresAt: ruleNow.Add(-time.Hour)},
		{Range: netip.MustParsePrefix("203.0.113.10/32"), Action: ruleForceBlock, ExpiresAt: ruleNow.Add(-time.Minute),
			// Expired: whatever beat it no longer matters.
			Overruled: &Ruling{Rule: ruleForceAllow, Match: "cidr:203.0.113.0/24"}},
	}
	q := overridesQuery{kind: kindBlock, address: "203.0.113.0/24", state: stateExpired}
	p := buildOverrides(overridesInput{now: ruleNow, query: q, active: testOverrides(), expired: expired,
		search: netip.MustParsePrefix("203.0.113.0/24"), retention: 7 * 24 * time.Hour})
	if p.Heading != "Expired always-block overrides on or around 203.0.113.0/24" || p.Clear != "/overrides?state=expired" {
		t.Errorf("heading %q, clear %q", p.Heading, p.Clear)
	}
	if len(p.Rows) != 2 || p.Rows[0].Address != "203.0.113.10" || p.Rows[1].Address != "203.0.113.9" || p.Rows[0].NoEffect != "" {
		t.Errorf("rows = %+v (the most recently expired first)", p.Rows)
	}
	if p.States[0].Count != 1 || p.States[1].Count != 2 || !p.States[1].Current ||
		p.States[0].Href != "/overrides?address=203.0.113.0%2F24&kind=block" {
		t.Errorf("states = %+v", p.States)
	}
	if !strings.Contains(p.StateNote, "keeps them for 7 days after their expiry") {
		t.Errorf("state note = %q", p.StateNote)
	}
	if !p.Kinds[2].Selected || p.Kinds[0].Selected {
		t.Errorf("kinds = %+v", p.Kinds)
	}

	p = buildOverrides(overridesInput{now: ruleNow, query: overridesQuery{kind: kindAllow, address: "192.0.2.1"},
		active: testOverrides(), search: netip.MustParsePrefix("192.0.2.1/32")})
	if len(p.Rows) != 0 || p.Empty != "No override matches this view." {
		t.Errorf("no match: rows %+v, empty %q", p.Rows, p.Empty)
	}
}

func TestBuildOverridesEmptyAndFailing(t *testing.T) {
	for _, tc := range []struct {
		in   overridesInput
		want string
	}{
		{overridesInput{}, "You have set no override: the verdicts and the allow-list decide. Set one with sudo obiectl allow or sudo obiectl block."},
		{overridesInput{query: overridesQuery{state: stateExpired}, retention: 7 * 24 * time.Hour}, "No override expired in the last 7 days."},
		{overridesInput{query: overridesQuery{state: stateExpired}}, "No expired override is kept."},
	} {
		if p := buildOverrides(tc.in); p.Empty != tc.want {
			t.Errorf("empty = %q, want %q", p.Empty, tc.want)
		}
	}
	p := buildOverrides(overridesInput{err: errors.New("store closed"), addressErr: errors.New("nope")})
	if p.Err != "store closed" || p.Empty != "" || p.AddressErr != "nope" {
		t.Errorf("failing: %+v", p)
	}
}

func TestBuildOverridesCapsTheList(t *testing.T) {
	var many []Override
	base := netip.MustParseAddr("100.0.0.0")
	for range maxRuleRows + 5 {
		many = append(many, Override{Range: netip.PrefixFrom(base, 32), Action: ruleForceAllow, CreatedAt: ruleNow})
		base = base.Next()
	}
	p := buildOverrides(overridesInput{active: many})
	if len(p.Rows) != maxRuleRows || p.More != 5 || p.States[0].Count != maxRuleRows+5 {
		t.Errorf("%d rows, %d more, count %d", len(p.Rows), p.More, p.States[0].Count)
	}
}

func TestNoEffectText(t *testing.T) {
	for _, tc := range []struct {
		r    Ruling
		want string
	}{
		{Ruling{Rule: ruleForceAllow, Match: "cidr:198.51.100.0/24"}, "No effect: the always-allow override on 198.51.100.0/24 covers it and wins."},
		{Ruling{Rule: ruleAllowlist, Protected: true, Source: "bootstrap", Match: "192.0.2.4/32"},
			"No effect: it is protected by a bootstrap peer's address 192.0.2.4/32, which not even an override blocks."},
		{Ruling{Rule: "other", Reason: "something else"}, "No effect: something else."},
	} {
		if got := noEffectText(&tc.r); got != tc.want {
			t.Errorf("noEffectText(%+v) = %q, want %q", tc.r, got, tc.want)
		}
	}
}

func TestPartNotice(t *testing.T) {
	notice := func(state lifecycle.State) string {
		return partNotice([]lifecycle.Status{{Name: partMesh}, {Name: partStore, State: state}}, partStore, "The store", "x appear", "x wait")
	}
	for state, want := range map[lifecycle.State]string{
		lifecycle.StateRunning:  "",
		lifecycle.StatePending:  "The store is starting: x appear.",
		lifecycle.StateStarting: "The store is starting: x appear.",
		lifecycle.StateStopped:  "The store is not running: x wait.",
		lifecycle.StateFailed:   "The store is not running: x wait.",
	} {
		if got := notice(state); got != want {
			t.Errorf("%s: %q, want %q", state, got, want)
		}
	}
	if got := partNotice(nil, partStore, "The store", "", ""); got != "" {
		t.Errorf("no status: %q", got)
	}
}

// testAllowlist is an allow-list with an entry of every origin, two files
// (one changed, one with rejected lines) and a warning.
func testAllowlist() Allowlist {
	e := func(cidr, source, label string) AllowEntry {
		return AllowEntry{Range: netip.MustParsePrefix(cidr), Source: source, Label: label}
	}
	return Allowlist{
		Entries: []AllowEntry{
			e("0.0.0.0/8", "builtin", "unspecified / this network"),
			e("::/128", "builtin", "unspecified"),
			e("127.0.0.0/8", "builtin", "loopback"),
			e("::1/128", "builtin", "loopback"),
			e("10.0.0.0/8", "builtin", "private (RFC 1918)"),
			e("192.168.0.0/16", "builtin", "private (RFC 1918)"),
			e("192.0.2.0/24", "builtin", "documentation (TEST-NET-1)"),
			e("2001:db8::/32", "builtin", "documentation"),
			e("185.0.9.1/32", "self", "/ip4/0.0.0.0/tcp/4001"),
			e("185.0.8.1/32", "bootstrap", "/dns4/peer.example/tcp/4001/p2p/12D3"),
			e("185.0.3.0/24", "config", ""),
			e("185.0.1.0/24", "file", "/etc/obie/allow.txt:2"),
			e("185.0.2.7/32", "file", "/etc/obie/allow.txt:4"),
			e("185.0.4.0/24", "file", "/etc/obie/partners.txt:1"),
		},
		LoadedAt: ruleNow.Add(-time.Hour),
		Files: []AllowFile{
			{Path: "/etc/obie/allow.txt", Loaded: 2, Changed: true, Entries: 3},
			{Path: "/etc/obie/partners.txt", Loaded: 1, Entries: 1, RejectedLines: 3,
				Rejected: []RejectedLine{{Line: 2, Text: "185.0.5.0/33", Err: "invalid CIDR"}, {Line: 3, Text: "nope", Err: "invalid address"}}},
		},
		Warnings: []AllowWarning{{Source: "bootstrap", Subject: "/dns4/gone.example/tcp/4001/p2p/12D3", Err: "no such host"},
			{Source: "self", Subject: "/ip6/::/tcp/4001", Err: "permission denied"}},
	}
}

// TestBuildAllowlist: every entry grouped by origin, the built-in ones by
// class; per file the entries loaded, when, and its state on disk (AC2).
func TestBuildAllowlist(t *testing.T) {
	p := buildAllowlist(allowlistInput{now: ruleNow, allow: testAllowlist()})
	if p.Total != 14 || p.LoadedAt.Text != "2026-09-28 11:00:00 UTC" || p.Lookup != nil || p.Err != "" {
		t.Errorf("page = %+v", p)
	}
	var titles []string
	for _, g := range p.Groups {
		titles = append(titles, g.Title)
	}
	if want := []string{"Built-in ranges", "This node's addresses", "Bootstrap peers", "Configured networks",
		"File /etc/obie/allow.txt", "File /etc/obie/partners.txt"}; !slices.Equal(titles, want) {
		t.Fatalf("groups = %v, want %v", titles, want)
	}
	builtin := p.Groups[0]
	if !builtin.Protected || len(builtin.Rows) != 0 {
		t.Errorf("built-in group = %+v", builtin)
	}
	var classes []string
	for _, c := range builtin.Classes {
		var ranges []string
		for _, r := range c.Ranges {
			ranges = append(ranges, strings.TrimSpace(r.Range+" "+r.Label))
		}
		classes = append(classes, c.Class+": "+strings.Join(ranges, ", "))
	}
	if want := []string{"Unspecified: 0.0.0.0/8 this network, ::", "Loopback: 127.0.0.0/8, ::1", "Private: 10.0.0.0/8 RFC 1918, 192.168.0.0/16 RFC 1918",
		"Documentation: 192.0.2.0/24 TEST-NET-1, 2001:db8::/32"}; !slices.Equal(classes, want) {
		t.Errorf("classes = %q\nwant      %q", classes, want)
	}
	if g := p.Groups[1]; !g.Protected || !reflect.DeepEqual(g.Rows, []allowRow{{Range: "185.0.9.1", Label: "/ip4/0.0.0.0/tcp/4001"}}) {
		t.Errorf("own addresses = %+v", g)
	}
	if g := p.Groups[2]; !g.Protected || g.Rows[0].Label != "/dns4/peer.example/tcp/4001/p2p/12D3" {
		t.Errorf("bootstrap = %+v", g)
	}
	if g := p.Groups[3]; g.Protected || !reflect.DeepEqual(g.Rows, []allowRow{{Range: "185.0.3.0/24"}}) {
		t.Errorf("configured networks = %+v", g)
	}
	allow := p.Groups[4]
	if allow.Protected || !reflect.DeepEqual(allow.Rows, []allowRow{{Range: "185.0.1.0/24", Label: "line 2"}, {Range: "185.0.2.7", Label: "line 4"}}) {
		t.Errorf("file group = %+v", allow)
	}
	if f := allow.File; f.Loaded != "2 entries loaded at 2026-09-28 11:00:00 UTC" || f.State != "changed" ||
		f.Status != "It changed since it was loaded and now holds 3 entries: they are not active until a reload applies them." {
		t.Errorf("changed file = %+v", f)
	}
	partners := p.Groups[5].File
	if partners.State != "warning" || len(partners.Rejected) != 2 || partners.MoreRejected != 1 ||
		partners.Status != "It now holds 3 lines the node rejects. The entries loaded stay in effect, but a reload would be "+
			"rejected, and obied would not start until the file is fixed." {
		t.Errorf("file with rejected lines = %+v", partners)
	}
	if len(p.Warnings) != 2 || !strings.HasPrefix(p.Warnings[0], "The bootstrap peer /dns4/gone.example/tcp/4001/p2p/12D3 did not resolve: no such host.") ||
		!strings.Contains(p.Warnings[1], "/ip6/::/tcp/4001 could not be listed: permission denied") {
		t.Errorf("warnings = %q", p.Warnings)
	}
}

// TestBuildAllowlistFileStates: a missing or unreadable file is a warning
// (edge case 3); an unchanged one says so; empty groups explain themselves.
func TestBuildAllowlistFileStates(t *testing.T) {
	a := Allowlist{LoadedAt: ruleNow, Files: []AllowFile{
		{Path: "/etc/obie/gone.txt", Loaded: 4, Err: "open /etc/obie/gone.txt: no such file or directory"},
		{Path: "/etc/obie/same.txt"},
	}}
	p := buildAllowlist(allowlistInput{allow: a})
	gone, same := p.Groups[4], p.Groups[5]
	if gone.File.State != "warning" || gone.File.Status != "It cannot be read now: open /etc/obie/gone.txt: no such file or directory. "+
		"The entries loaded stay in effect, but a reload would be rejected, and obied would not start until it can be read again." {
		t.Errorf("missing file = %+v", gone.File)
	}
	if same.File.State != "ok" || same.File.Status != "Unchanged since it was loaded." || same.Empty == "" || same.ID != "file-2" {
		t.Errorf("unchanged file = %+v", same)
	}
	for _, g := range p.Groups[:4] {
		if g.Empty == "" {
			t.Errorf("empty group %s without explanation", g.Title)
		}
	}
	if p := buildAllowlist(allowlistInput{err: errNoRules}); p.Err == "" || p.Groups != nil {
		t.Errorf("without rules: %+v", p)
	}
}

func TestBuildAllowlistCapsAGroup(t *testing.T) {
	var a Allowlist
	base := netip.MustParseAddr("100.0.0.0")
	for range maxRuleRows + 2 {
		a.Entries = append(a.Entries, AllowEntry{Range: netip.PrefixFrom(base, 32), Source: "config"})
		base = base.Next()
	}
	g := buildAllowlist(allowlistInput{allow: a}).Groups[3]
	if len(g.Rows) != maxRuleRows || g.More != 2 {
		t.Errorf("%d rows, %d more", len(g.Rows), g.More)
	}
}

// TestLookupNamesTheRule: the lookup names the rule that decides, with the
// matching entry, and the other entries that overlap (AC5).
func TestLookupNamesTheRule(t *testing.T) {
	cfgEntry := AllowEntry{Range: netip.MustParsePrefix("185.0.3.0/24"), Source: "config"}
	fileEntry := AllowEntry{Range: netip.MustParsePrefix("185.0.3.7/32"), Source: "file", Label: "/etc/obie/allow.txt:3"}
	for _, tc := range []struct {
		name   string
		pr     Protection
		state  string
		answer string
		others []allowRow
	}{
		{"protected", Protection{Range: netip.MustParsePrefix("192.168.1.10/32"), Ruling: Ruling{Effect: "allow", Rule: ruleAllowlist,
			Source: "builtin", Protected: true, Match: "192.168.0.0/16", Label: "private (RFC 1918)"},
			Overlapping: []AllowEntry{{Range: netip.MustParsePrefix("192.168.0.0/16"), Source: "builtin", Label: "private (RFC 1918)"}}},
			lookupProtected, "Yes: 192.168.1.10 is protected by the built-in range 192.168.0.0/16 (Private). It is never blocked, " +
				"not even by an always-block override.", nil},
		{"operator entry", Protection{Range: netip.MustParsePrefix("185.0.3.7/32"), Decidable: true, Ruling: Ruling{Effect: "allow",
			Rule: ruleAllowlist, Source: "config", Match: "185.0.3.0/24"}, Overlapping: []AllowEntry{cfgEntry, fileEntry}},
			lookupAllowed, "Yes: the allow-list entry 185.0.3.0/24 (allowlist.cidrs) covers 185.0.3.7, so it is never blocked, " +
				"whatever the verdicts. Only an always-block override on it, or on a network around it, would overrule the entry.",
			[]allowRow{{Range: "185.0.3.7", Label: "allowlist.files: /etc/obie/allow.txt:3"}}},
		{"force-allow", Protection{Range: netip.MustParsePrefix("198.51.100.4/32"), Decidable: true, Ruling: Ruling{Effect: "allow",
			Rule: ruleForceAllow, Source: "override", Match: "cidr:198.51.100.0/24"}},
			lookupAllowed, "Yes, by your always-allow override on 198.51.100.0/24: 198.51.100.4 is not blocked, whatever the verdicts, " +
				"until you remove the override.", nil},
		{"force-block", Protection{Range: netip.MustParsePrefix("185.0.3.7/32"), Decidable: true, Ruling: Ruling{Effect: "block",
			Rule: ruleForceBlock, Source: "override", Match: "ipv4:185.0.3.7", ExpiresAt: ruleNow}, Overlapping: []AllowEntry{cfgEntry}},
			lookupBlocked, "No: your always-block override blocks 185.0.3.7, until 2026-09-28 12:00:00 UTC. It overrules the " +
				"allow-list entries that cover it.", []allowRow{{Range: "185.0.3.0/24", Label: "allowlist.cidrs"}}},
		{"force-block around it", Protection{Range: netip.MustParsePrefix("185.0.3.7/32"), Decidable: true, Ruling: Ruling{Effect: "block",
			Rule: ruleForceBlock, Source: "override", Match: "cidr:185.0.3.0/25"}, Overlapping: []AllowEntry{cfgEntry}},
			lookupBlocked, "No: your always-block override on 185.0.3.0/25 blocks 185.0.3.7, until you remove the override. It " +
				"overrules the allow-list entries that cover it.", []allowRow{{Range: "185.0.3.0/24", Label: "allowlist.cidrs"}}},
		{"none", Protection{Range: netip.MustParsePrefix("203.0.113.7/32"), Decidable: true},
			lookupNone, "No: no allow-list entry or override covers 203.0.113.7. The verdicts decide whether it is blocked.", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := newLookupView(&tc.pr)
			if v.State != tc.state || v.Answer != tc.answer || !reflect.DeepEqual(v.Others, tc.others) {
				t.Errorf("lookup = %+v\nwant state %s, answer %q, others %+v", v, tc.state, tc.answer, tc.others)
			}
			if (v.DecisionHref != "") != tc.pr.Decidable {
				t.Errorf("decision link %q for decidable %v", v.DecisionHref, tc.pr.Decidable)
			}
		})
	}
	p := buildAllowlist(allowlistInput{address: "10.0.0.0/8", protectionErr: errors.New("store closed")})
	if p.AddressErr != "The lookup failed: store closed" || p.Lookup != nil {
		t.Errorf("failed lookup: %+v", p)
	}
}

// TestLookupCapsTheOthers: a lookup of a wide network lists at most
// maxRuleRows of the entries that overlap it, and counts the rest.
func TestLookupCapsTheOthers(t *testing.T) {
	pr := Protection{Range: netip.MustParsePrefix("0.0.0.0/0")}
	base := netip.MustParseAddr("100.0.0.0")
	for range maxRuleRows + 3 {
		pr.Overlapping = append(pr.Overlapping, AllowEntry{Range: netip.PrefixFrom(base, 32), Source: "file"})
		base = base.Next()
	}
	if v := newLookupView(&pr); len(v.Others) != maxRuleRows || v.MoreOthers != 3 {
		t.Errorf("%d others, %d more", len(v.Others), v.MoreOthers)
	}
}

// testSettings are settings of three sections: one set by the file, one
// default, one changed on disk for a reload and one for a restart, a list
// and a secret.
func testSettings() []Setting {
	return []Setting{
		{Key: "node.mode", Section: "node", Summary: "Observe or enforce.", Applied: appliedReload, Value: SettingValue{Text: "observe"},
			Disk: &SettingValue{Text: "enforce"}},
		{Key: "node.state_dir", Section: "node", Summary: "State.", Applied: appliedRestart, Value: SettingValue{Text: "/var/lib/obie"}, Default: true},
		{Key: "mesh.bootstrap", Section: "mesh", Summary: "Peers.", Applied: appliedRestart, Value: SettingValue{List: true},
			Default: true, Disk: &SettingValue{List: true, Items: []string{"/ip4/192.0.2.1/tcp/4001/p2p/12D3"}}},
		{Key: "log.token", Section: "log", Summary: "A secret.", Applied: appliedRestart, Value: SettingValue{Secret: true, IsSet: true}},
	}
}

// TestBuildConfiguration: every setting by section with its value, the
// defaults marked, when a change takes effect, and what changed on disk
// (AC3, AC4, edge case 1).
func TestBuildConfiguration(t *testing.T) {
	loaded := ruleNow.Add(-time.Hour)
	p := buildConfiguration(configurationInput{now: ruleNow, cfg: Configuration{Path: "/etc/obie/obie.yaml",
		Load: ConfigFacts{LoadedAt: loaded}, Settings: testSettings()}})
	if p.Path != "/etc/obie/obie.yaml" || p.LoadedAt.Text != "2026-09-28 11:00:00 UTC" || p.LoadedBy != "at start" || p.Defaults != 2 {
		t.Errorf("page = %+v", p)
	}
	if p.Reload != (statusLine{State: "none", Text: "The configuration was not reloaded since obied started."}) {
		t.Errorf("reload = %+v", p.Reload)
	}
	if p.Disk != (statusLine{State: "changed", Text: "The file on disk changed since it was loaded: 2 settings differ from the " +
		"running configuration and are not active yet."}) || !slices.Equal(p.Pending, []string{"node.mode"}) ||
		!slices.Equal(p.Restart, []string{"mesh.bootstrap"}) {
		t.Errorf("disk = %+v, pending %v, restart %v", p.Disk, p.Pending, p.Restart)
	}
	var sections []string
	for _, s := range p.Sections {
		sections = append(sections, fmt.Sprintf("%s:%d", s.Name, len(s.Rows)))
		if s.Text == "" {
			t.Errorf("section %s without text", s.Name)
		}
	}
	if !slices.Equal(sections, []string{"node:2", "mesh:1", "log:1"}) {
		t.Errorf("sections = %v", sections)
	}
	mode, stateDir := p.Sections[0].Rows[0], p.Sections[0].Rows[1]
	if mode.Value.Text != "observe" || mode.Default || mode.AppliedText != "Reload" || mode.Disk.Text != "enforce" ||
		mode.Pending != "Not active until a reload." {
		t.Errorf("node.mode = %+v", mode)
	}
	if !stateDir.Default || stateDir.Applied != appliedRestart || stateDir.AppliedText != "Restart" || stateDir.Disk != nil || stateDir.Pending != "" {
		t.Errorf("node.state_dir = %+v", stateDir)
	}
	bootstrap := p.Sections[1].Rows[0]
	if !bootstrap.Value.List || !bootstrap.Value.Empty || bootstrap.Disk.Empty || len(bootstrap.Disk.Items) != 1 ||
		bootstrap.Pending != "Waits for a restart." {
		t.Errorf("mesh.bootstrap = %+v", bootstrap)
	}
	if secret := p.Sections[2].Rows[0].Value; !reflect.DeepEqual(secret, valueView{Secret: true, Text: "Set, not shown"}) {
		t.Errorf("secret = %+v", secret)
	}
}

// TestBuildConfigurationLoads: a successful and a rejected reload, a file
// that cannot be loaded, one that matches, and a node without a file
// (AC4, edge case 2).
func TestBuildConfigurationLoads(t *testing.T) {
	loaded := ruleNow.Add(-time.Hour)
	unchanged := []Setting{{Key: "node.mode", Section: "node", Applied: appliedReload, Value: SettingValue{Text: "observe"}}}
	p := buildConfiguration(configurationInput{cfg: Configuration{Path: "/etc/obie/obie.yaml", Settings: unchanged,
		Load: ConfigFacts{LoadedAt: loaded, Reloaded: true}}})
	if p.LoadedBy != "by a reload" || p.Reload != (statusLine{State: "ok", Text: "The last reload, at 2026-09-28 11:00:00 UTC, succeeded."}) ||
		p.Disk != (statusLine{State: "ok", Text: "The file on disk matches the running configuration."}) {
		t.Errorf("after a reload: %+v / %+v", p.Reload, p.Disk)
	}
	p = buildConfiguration(configurationInput{cfg: Configuration{Path: "/etc/obie/obie.yaml", Settings: unchanged,
		DiskErr: "invalid configuration: decision.quorum: must be at least 1",
		Load:    ConfigFacts{LoadedAt: loaded, RejectedAt: ruleNow, Rejected: "invalid configuration: decision.quorum: must be at least 1"}}})
	if p.Reload != (statusLine{State: "warning", Text: "The last reload, at 2026-09-28 12:00:00 UTC, was rejected: invalid configuration: " +
		"decision.quorum: must be at least 1. The configuration loaded at 2026-09-28 11:00:00 UTC is still active."}) {
		t.Errorf("rejected reload = %+v", p.Reload)
	}
	if p.Disk != (statusLine{State: "warning", Text: "The file on disk cannot be loaded now: invalid configuration: decision.quorum: " +
		"must be at least 1. A reload would be rejected, and the running configuration kept."}) {
		t.Errorf("invalid file = %+v", p.Disk)
	}
	// One setting waits for a reload; or the file on disk only differs in
	// one a restart applies, which a reload leaves waiting.
	pending := []Setting{{Key: "node.mode", Section: "node", Applied: appliedReload, Value: SettingValue{Text: "observe"},
		Disk: &SettingValue{Text: "enforce"}}}
	if p := buildConfiguration(configurationInput{cfg: Configuration{Path: "/etc/obie/obie.yaml", Settings: pending}}); p.Disk !=
		(statusLine{State: "changed", Text: "The file on disk changed since it was loaded: 1 setting differs from the running " +
			"configuration and is not active yet."}) {
		t.Errorf("one pending = %+v", p.Disk)
	}
	restart := []Setting{{Key: "log.level", Section: "log", Applied: appliedRestart, Value: SettingValue{Text: "info"},
		Disk: &SettingValue{Text: "debug"}}}
	if p := buildConfiguration(configurationInput{cfg: Configuration{Path: "/etc/obie/obie.yaml", Settings: restart,
		Load: ConfigFacts{LoadedAt: loaded, Reloaded: true}}}); p.Disk != (statusLine{State: "changed",
		Text: "The file on disk differs from the running configuration in 1 setting that only a restart of obied applies."}) {
		t.Errorf("restart only = %+v", p.Disk)
	}
	if p := buildConfiguration(configurationInput{cfg: Configuration{Settings: unchanged}}); p.Disk.State != "none" {
		t.Errorf("without a file: %+v", p.Disk)
	}
	if p := buildConfiguration(configurationInput{err: errNoRules}); p.Err == "" || p.Sections != nil {
		t.Errorf("without rules: %+v", p)
	}
}

// TestSecretsAreNeverShown: a secret's value is never shown, even if a
// source passed it (AC3).
func TestSecretsAreNeverShown(t *testing.T) {
	for _, v := range []SettingValue{{Secret: true, IsSet: true, Text: "hunter2"}, {Secret: true, List: true, Items: []string{"hunter2"}}} {
		got := newValueView(&v)
		if !got.Secret || strings.Contains(fmt.Sprint(got), "hunter2") {
			t.Errorf("secret shown: %+v", got)
		}
	}
	if got := newValueView(&SettingValue{Secret: true}); got.Text != "Not set" {
		t.Errorf("unset secret = %+v", got)
	}
}
