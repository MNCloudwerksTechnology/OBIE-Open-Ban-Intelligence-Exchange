package console

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
)

var overviewNow = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

// runningStatuses are the statuses of a node whose parts all run, in the
// order obied starts them.
func runningStatuses() []lifecycle.Status {
	var out []lifecycle.Status
	for _, name := range []string{Name, partStore, "audit", partDecision, partEnforce, "ops", partMesh, partAdmin} {
		s := lifecycle.Status{Name: name, State: lifecycle.StateRunning, Ready: true}
		if name == partMesh {
			s.Detail = "2 peers connected (2/3 bootstrap peers)"
		}
		out = append(out, s)
	}
	return out
}

// healthyInput is a node in enforce mode that has run for three hours,
// with peers, data and applied blocks.
func healthyInput() overviewInput {
	return overviewInput{
		now:      overviewNow,
		node:     Node{Version: "v0.1.0", PeerID: testNode.PeerID, Fingerprint: "SHA256:abc", StartedAt: overviewNow.Add(-3 * time.Hour)},
		mode:     "enforce",
		statuses: runningStatuses(),
		facts: Facts{
			Peers:     PeerFacts{Connected: 2, Bootstrap: 2, Configured: 3},
			Decisions: DecisionFacts{Block: 14, None: 1190, Allowed: 2, Indicators: 1206, Verdicts: 3410},
			Enforce:   EnforceFacts{Backend: "nftables", MaxEntries: 100000, Mode: "enforce", Applied: 12, Blocks: 14, Covered: 2},
			Store:     StoreFacts{Overrides: 3, VerdictRecords: 3500, EventsAccepted: 40},
			Config:    ConfigFacts{LoadedAt: overviewNow.Add(-3 * time.Hour)},
		},
		link: func(_, command string) (string, string) { return "", command },
	}
}

// setStatus replaces the status of the subsystem name.
func setStatus(in *overviewInput, s lifecycle.Status) {
	for i := range in.statuses {
		if in.statuses[i].Name == s.Name {
			in.statuses[i] = s
			return
		}
	}
	in.statuses = append(in.statuses, s)
}

func numberOf(t *testing.T, p overviewPage, label string) keyNumber {
	t.Helper()
	for _, k := range p.Numbers {
		if k.Label == label {
			return k
		}
	}
	t.Fatalf("no number %q in %+v", label, p.Numbers)
	return keyNumber{}
}

func titles(conds []condition) []string {
	out := make([]string, len(conds))
	for i, c := range conds {
		out[i] = c.Title
	}
	return out
}

// wantConditions checks that the conditions start with the given titles,
// in order, and that there are no others.
func wantConditions(t *testing.T, p overviewPage, prefixes ...string) {
	t.Helper()
	got := titles(p.Conditions)
	if len(got) != len(prefixes) {
		t.Fatalf("conditions %q, want %d starting with %q", got, len(prefixes), prefixes)
	}
	for i, prefix := range prefixes {
		if !strings.HasPrefix(got[i], prefix) {
			t.Errorf("condition %d = %q, want it to start with %q", i, got[i], prefix)
		}
	}
}

func TestOverviewHealthyNode(t *testing.T) {
	p := buildOverview(healthyInput())
	if p.Summary != (summary{stateOK, "Healthy", "Every part of the node is ready and nothing needs your attention."}) {
		t.Errorf("summary = %+v", p.Summary)
	}
	wantConditions(t, p)
	if p.Starting != nil {
		t.Errorf("a node running for hours shows the empty state: %+v", p.Starting)
	}
	if p.ReadAt != (timestamp{"2026-09-28T12:00:00Z", "2026-09-28 12:00:00 UTC"}) {
		t.Errorf("read at %+v", p.ReadAt)
	}

	for _, want := range []keyNumber{
		{Label: "Peers connected", Value: "2", Note: "2 of 3 configured connected", Command: "obiectl peers"},
		{Label: "Indicators held", Value: "1,206", Note: "with 3,410 active verdicts", Command: "obiectl indicators"},
		{Label: "Decisions: block", Value: "14", Note: "score and quorum reached, or force-blocked", Command: "obiectl decisions --state block"},
		{Label: "Decisions: none", Value: "1,190", Note: "held, but below the threshold or the quorum", Command: "obiectl decisions --state none"},
		{Label: "Decisions: allowed", Value: "2", Note: "protected by the allow-list or a force-allow", Command: "obiectl decisions --state allowed"},
		{Label: "Firewall entries", Value: "12", Note: "applied by nftables for 14 decided blocks: 2 share an entry with another block", Command: "obiectl enforced"},
		{Label: "Active overrides", Value: "3", Note: "force-allows and force-blocks you set", Command: "obiectl overrides"},
	} {
		if got := numberOf(t, p, want.Label); got != want {
			t.Errorf("number %+v\nwant   %+v", got, want)
		}
	}

	var parts []string
	for _, pt := range p.Parts {
		if pt.State != stateReady || pt.Label != "Ready" {
			t.Errorf("part %+v, want ready", pt)
		}
		parts = append(parts, pt.Title)
	}
	if got := strings.Join(parts, ", "); got != "Mesh, Store, Decision engine, Enforcement, Admin interface, Web console, Audit log, Metrics endpoint" {
		t.Errorf("parts = %s", got)
	}
	if p.Parts[0].Detail != "2 peers connected (2/3 bootstrap peers)" {
		t.Errorf("mesh detail = %q", p.Parts[0].Detail)
	}

	n := p.Node
	if n.PeerID != testNode.PeerID || n.Fingerprint != "SHA256:abc" || n.Version != "v0.1.0" || n.Uptime != "3 h 0 min" ||
		n.StartedAt.Text != "2026-09-28 09:00:00 UTC" || n.ConfigLoadedAt.Text != "2026-09-28 09:00:00 UTC" ||
		n.ConfigLoadedBy != "at start" || n.ModeLabel != "Enforce" ||
		n.ModeText != "The node blocks what it decides to block: the nftables backend applies each block to the firewall until the decision expires." {
		t.Errorf("node = %+v", n)
	}
}

func TestOverviewLinksBuiltViews(t *testing.T) {
	in := healthyInput()
	in.link = func(path, command string) (string, string) {
		if path == "/peers" || path == "/decisions?state=block" {
			return path, ""
		}
		return "", command
	}
	p := buildOverview(in)
	if k := numberOf(t, p, "Peers connected"); k.Href != "/peers" || k.Command != "" {
		t.Errorf("peers = %+v, want a link and no command", k)
	}
	if k := numberOf(t, p, "Decisions: block"); k.Href != "/decisions?state=block" || k.Command != "" {
		t.Errorf("blocks = %+v", k)
	}
	if k := numberOf(t, p, "Active overrides"); k.Href != "" || k.Command != "obiectl overrides" {
		t.Errorf("overrides = %+v, want the command", k)
	}
}

// justStarted is a node that started 30 s ago without peers or data.
func justStarted() overviewInput {
	in := healthyInput()
	in.node.StartedAt = overviewNow.Add(-30 * time.Second)
	in.facts.Peers = PeerFacts{Configured: 2}
	in.facts.Decisions = DecisionFacts{}
	in.facts.Enforce = EnforceFacts{Backend: "nftables", MaxEntries: 100000, Mode: "enforce"}
	in.facts.Store = StoreFacts{}
	setStatus(&in, lifecycle.Status{Name: partMesh, State: lifecycle.StateRunning, Ready: true,
		Detail: "degraded: 0 peers connected (0/2 bootstrap peers)"})
	return in
}

// TestOverviewJustStarted: a new node without peers or data explains what
// will appear and when, and shows no zeros that look like a failure.
func TestOverviewJustStarted(t *testing.T) {
	p := buildOverview(justStarted())
	if p.Summary.Title != "Just started" || p.Summary.State != stateWaiting {
		t.Errorf("summary = %+v", p.Summary)
	}
	wantConditions(t, p) // the degraded mesh is explained by the start
	if p.Starting == nil || p.Starting.Uptime != "30 s" {
		t.Fatalf("starting = %+v", p.Starting)
	}
	var what []string
	for _, it := range p.Starting.Items {
		what = append(what, it.What)
	}
	if got := strings.Join(what, ","); got != "Peers,Verdicts,Decisions,Blocks" {
		t.Errorf("starting items = %s", got)
	}
	if !strings.Contains(p.Starting.Items[0].When, "dialing the 2 configured peers now") ||
		!strings.Contains(p.Starting.Items[3].When, "within seconds") {
		t.Errorf("starting = %+v", p.Starting.Items)
	}
	for label, note := range map[string]string{
		"Peers connected":  "connecting to 2 configured peers",
		"Indicators held":  "verdicts from peers and local reports appear here",
		"Decisions: block": "score and quorum reached, or force-blocked",
		"Firewall entries": "blocks are applied within seconds of being decided",
	} {
		if k := numberOf(t, p, label); k.Value != "None yet" || k.State != stateEmpty || k.Note != note {
			t.Errorf("%s = %+v, want none yet: %s", label, k, note)
		}
	}
	if k := numberOf(t, p, "Active overrides"); k.Value != "0" || k.State != "" {
		t.Errorf("overrides = %+v: set by the operator, not arriving", k)
	}
	if p.Parts[0] != (part{Title: "Mesh", State: stateWaiting, Label: "Waiting for peers", Detail: "0 peers connected (0/2 bootstrap peers)"}) {
		t.Errorf("mesh = %+v, want waiting for peers rather than degraded", p.Parts[0])
	}

	// Without configured peers and in observe mode.
	in := justStarted()
	in.mode, in.facts.Peers.Configured = "observe", 0
	in.facts.Enforce.Mode = "observe"
	p = buildOverview(in)
	if !strings.Contains(p.Starting.Items[0].When, "no peer is configured in mesh.bootstrap") ||
		!strings.Contains(p.Starting.Items[3].When, "the firewall blocks nothing") {
		t.Errorf("starting = %+v", p.Starting.Items)
	}
	if k := numberOf(t, p, "Firewall entries"); k.Value != "None" || k.Note != "observe mode applies no block" {
		t.Errorf("entries in observe mode = %+v", k)
	}

	// Once peers and data arrived, the node no longer looks new.
	in = justStarted()
	in.facts.Peers = PeerFacts{Connected: 1, Bootstrap: 1, Configured: 2}
	in.facts.Store.VerdictRecords = 1
	setStatus(&in, lifecycle.Status{Name: partMesh, State: lifecycle.StateRunning, Ready: true})
	if p := buildOverview(in); p.Starting != nil || p.Summary.Title != "Healthy" {
		t.Errorf("node with peers and data: starting %+v, summary %+v", p.Starting, p.Summary)
	}
}

// TestOverviewAfterStartupGrace: the same empty node is a condition to act
// on once the grace is over.
func TestOverviewAfterStartupGrace(t *testing.T) {
	in := justStarted()
	in.node.StartedAt = overviewNow.Add(-3 * time.Minute)
	p := buildOverview(in)
	if p.Starting != nil {
		t.Errorf("starting = %+v", p.Starting)
	}
	wantConditions(t, p,
		"No peer is connected: none of the 2 configured peers answers.",
		"No event received: this node holds no verdict, and none arrived since obied started 3 min 0 s ago.")
	if c := p.Conditions[0]; !c.Warning || c.Command != "obiectl peers" || !strings.Contains(c.Next, "mesh port is reachable") {
		t.Errorf("no-peer condition = %+v", c)
	}
	if p.Summary != (summary{stateAttention, "Needs attention", "2 conditions below need your attention."}) {
		t.Errorf("summary = %+v", p.Summary)
	}
	if k := numberOf(t, p, "Peers connected"); k.Value != "0" || k.State != "" {
		t.Errorf("peers = %+v", k)
	}
	if p.Parts[0].Label != "Degraded" || p.Parts[0].State != stateWarning {
		t.Errorf("mesh after the grace = %+v, want degraded", p.Parts[0])
	}

	in.facts.Peers.Configured = 1
	if p := buildOverview(in); p.Conditions[0].Title != "No peer is connected: the configured peer does not answer." {
		t.Errorf("no-peer condition with one configured peer = %q", p.Conditions[0].Title)
	}

	in.facts.Peers.Configured = 0
	in.facts.Store.EventsAccepted = 1 // arrived and expired: the node did receive one
	p = buildOverview(in)
	wantConditions(t, p, "No peer is configured, so this node hears only its own reports.")
	if p.Summary.Text != "1 condition below needs your attention." {
		t.Errorf("summary = %+v", p.Summary)
	}
}

// TestOverviewPartsNotReady: a part that does not run yet is shown as
// such, its numbers wait for it, and the rest of the page still works.
func TestOverviewPartsNotReady(t *testing.T) {
	in := healthyInput()
	setStatus(&in, lifecycle.Status{Name: partStore, State: lifecycle.StateStarting})
	setStatus(&in, lifecycle.Status{Name: partDecision, State: lifecycle.StatePending})
	p := buildOverview(in)
	if p.Summary.Title != "Starting" {
		t.Errorf("summary = %+v", p.Summary)
	}
	if k := numberOf(t, p, "Active overrides"); k.Value != "Waiting" || k.Note != "for the store to start" || k.State != stateWaiting {
		t.Errorf("overrides = %+v", k)
	}
	if k := numberOf(t, p, "Decisions: block"); k.Value != "Waiting" || k.Note != "for the decision engine to start" {
		t.Errorf("blocks = %+v", k)
	}
	if k := numberOf(t, p, "Peers connected"); k.Value != "2" {
		t.Errorf("peers = %+v, want the mesh's numbers still shown", k)
	}
	if p.Parts[1] != (part{Title: "Store", State: stateWaiting, Label: "Starting"}) ||
		p.Parts[2] != (part{Title: "Decision engine", State: stateWaiting, Label: "Waiting to start"}) {
		t.Errorf("parts = %+v", p.Parts)
	}

	in = healthyInput()
	setStatus(&in, lifecycle.Status{Name: partEnforce, State: lifecycle.StateStopped})
	setStatus(&in, lifecycle.Status{Name: partStore, State: lifecycle.StateRunning, Error: "sweep expired verdicts: disk full"})
	setStatus(&in, lifecycle.Status{Name: partDecision, State: lifecycle.StateRunning, Ready: true, Detail: "degraded: stale"})
	in.statuses = in.statuses[1:] // no console, e.g. an older node
	p = buildOverview(in)
	if k := numberOf(t, p, "Firewall entries"); k.Value != "Stopped" || k.Note != "enforcement is not running" {
		t.Errorf("entries = %+v", k)
	}
	if k := numberOf(t, p, "Active overrides"); k.Value != "3" {
		t.Errorf("overrides of a running store that is not ready = %+v, want shown", k)
	}
	if p.Summary.Title != "Shutting down" {
		t.Errorf("summary = %+v", p.Summary)
	}
	wantConditions(t, p, "Store is not ready: sweep expired verdicts: disk full", "Decision engine is degraded: stale")
	if p.Parts[1] != (part{Title: "Store", State: stateWarning, Label: "Not ready", Detail: "sweep expired verdicts: disk full"}) ||
		p.Parts[2] != (part{Title: "Decision engine", State: stateWarning, Label: "Degraded", Detail: "stale"}) ||
		p.Parts[3] != (part{Title: "Enforcement", State: stateStopped, Label: "Stopped"}) {
		t.Errorf("parts = %+v", p.Parts)
	}

	in = healthyInput()
	var noMesh []lifecycle.Status
	for _, s := range in.statuses {
		if s.Name != partMesh {
			noMesh = append(noMesh, s)
		}
	}
	in.statuses = noMesh
	if k := numberOf(t, buildOverview(in), "Peers connected"); k.Value != "Not available" || k.State != stateWaiting {
		t.Errorf("peers without a mesh = %+v", k)
	}
}

func TestOverviewEnforcementConditions(t *testing.T) {
	for name, tc := range map[string]struct {
		mode    string
		enforce EnforceFacts
		want    []string
	}{
		"dry-run backend": {"enforce", EnforceFacts{Backend: "dryrun", Mode: "enforce", Applied: 14, Blocks: 14},
			[]string{"Enforce mode, but nothing is applied to the firewall: the dry-run backend only logs the blocks."}},
		"dry-run backend in observe mode": {"observe", EnforceFacts{Backend: "dryrun", Mode: "observe"}, nil},
		"failing, nothing applied": {"enforce", EnforceFacts{Backend: "nftables", Mode: "enforce", Failures: 3, Err: "netlink: operation not permitted", RetryIn: 4 * time.Second},
			[]string{"Enforce mode, but nothing is applied: enforcement failed 3 times in a row: netlink: operation not permitted"}},
		"failing": {"enforce", EnforceFacts{Backend: "nftables", Mode: "enforce", Applied: 12, Blocks: 14, Covered: 2, Failures: 1, Err: "timeout"},
			[]string{"Decided blocks and applied entries may differ: enforcement failed: timeout"}},
		"withdrawal failing": {"observe", EnforceFacts{Backend: "nftables", Mode: "observe", Failures: 2, Err: "no such table"},
			[]string{"The firewall may still apply blocks of an earlier enforce run: withdrawing them failed 2 times in a row: no such table"}},
		"refused": {"enforce", EnforceFacts{Backend: "nftables", Mode: "enforce", Applied: 12, Blocks: 14, Refused: 2},
			[]string{"2 decided blocks are not applied: the allow-list refuses them right before apply."}},
		"all refused": {"enforce", EnforceFacts{Backend: "nftables", Mode: "enforce", Blocks: 1, Refused: 1},
			[]string{"Enforce mode, but nothing is applied: the allow-list refuses every decided block right before apply."}},
		"capped": {"enforce", EnforceFacts{Backend: "nftables", MaxEntries: 10, Mode: "enforce", Applied: 10, Blocks: 11, Capped: 1},
			[]string{"1 decided block is not applied: the firewall holds at most 10 entries (enforce.max_entries), and the lowest-score blocks are left out."}},
	} {
		t.Run(name, func(t *testing.T) {
			in := healthyInput()
			in.mode, in.facts.Enforce = tc.mode, tc.enforce
			if tc.enforce.Failures > 0 {
				// Explained by the failure, not listed again.
				setStatus(&in, lifecycle.Status{Name: partEnforce, State: lifecycle.StateRunning, Error: "enforcement failed"})
			}
			p := buildOverview(in)
			wantConditions(t, p, tc.want...)
			for _, c := range p.Conditions {
				if !c.Warning || c.Next == "" {
					t.Errorf("condition %+v, want a warning with a next step", c)
				}
			}
		})
	}

	// While enforcement does not run, its last numbers raise nothing.
	in := healthyInput()
	in.facts.Enforce.Capped = 3
	setStatus(&in, lifecycle.Status{Name: partEnforce, State: lifecycle.StateStopping})
	if p := buildOverview(in); len(p.Conditions) != 0 {
		t.Errorf("conditions of a stopping enforcement: %q", titles(p.Conditions))
	}
}

func TestOverviewEntries(t *testing.T) {
	in := healthyInput()
	in.facts.Enforce = EnforceFacts{Backend: "nftables", Mode: "enforce", Applied: 10, Blocks: 14, Covered: 2, Refused: 1, Capped: 1}
	if k := numberOf(t, buildOverview(in), "Firewall entries"); k.Note != "applied by nftables for 14 decided blocks: "+
		"2 share an entry with another block, 1 refused by the allow-list, 1 over enforce.max_entries" {
		t.Errorf("entries = %+v", k)
	}
	in.facts.Enforce = EnforceFacts{Backend: "dryrun", Mode: "enforce", Applied: 1, Blocks: 1}
	if k := numberOf(t, buildOverview(in), "Firewall entries"); k.Note != "kept by the dry-run backend, which blocks nothing" {
		t.Errorf("dry-run entries = %+v", k)
	}
	in.facts.Enforce = EnforceFacts{Backend: "nftables"}
	if k := numberOf(t, buildOverview(in), "Firewall entries"); k.Value != "Waiting" || k.Note != "for the first enforcement pass" {
		t.Errorf("entries before the first pass = %+v", k)
	}
}

func TestOverviewConfigConditions(t *testing.T) {
	in := healthyInput()
	in.facts.Config = ConfigFacts{LoadedAt: overviewNow.Add(-time.Hour), Reloaded: true,
		RejectedAt: overviewNow.Add(-time.Minute), Rejected: "invalid configuration: decision.quorum must be at least 1",
		RestartKeys: []string{"mesh.bootstrap", "store"}}
	in.facts.Store.OverridesErr = errors.New("store closed")
	p := buildOverview(in)
	wantConditions(t, p,
		"The configuration reload at 2026-09-28 11:59:00 UTC was rejected: invalid configuration: decision.quorum must be at least 1",
		"Changes to mesh.bootstrap, store wait for a restart: a reload does not apply them.")
	if c := p.Conditions[0]; !c.Warning || c.Command != "obied --check-config" ||
		!strings.Contains(c.Next, "keeps the configuration loaded at 2026-09-28 11:00:00 UTC") {
		t.Errorf("rejected reload = %+v", c)
	}
	if c := p.Conditions[1]; c.Warning || c.Command != "sudo systemctl restart obied" {
		t.Errorf("restart = %+v, want a note", c)
	}
	if p.Summary.Title != "Needs attention" || p.Summary.Text != "1 condition below needs your attention." {
		t.Errorf("summary = %+v; notes do not count", p.Summary)
	}
	if p.Node.ConfigLoadedBy != "by a reload" || p.Node.ConfigLoadedAt.Text != "2026-09-28 11:00:00 UTC" {
		t.Errorf("node = %+v", p.Node)
	}
	if k := numberOf(t, p, "Active overrides"); k.Value != "Not available" || k.State != stateError || k.Note != "reading them failed: store closed" {
		t.Errorf("overrides = %+v", k)
	}

	// Warnings come before notes, whatever raised them.
	in.facts.Config.Rejected = ""
	in.facts.Peers.Connected = 0
	p = buildOverview(in)
	if len(p.Conditions) != 2 || !p.Conditions[0].Warning || p.Conditions[1].Warning {
		t.Errorf("conditions %q, want the warning first", titles(p.Conditions))
	}
}

func TestModeExplained(t *testing.T) {
	for _, tc := range []struct{ mode, backend, label, text string }{
		{"observe", "nftables", "Observe", "blocks nothing: the firewall is left alone"},
		{"enforce", "nftables", "Enforce", "the nftables backend applies each block to the firewall"},
		{"enforce", "dryrun", "Enforce", "dry-run backend, which only logs it"},
		{"enforce", "", "Enforce", "through the enforcement backend"},
		{"learn", "", "learn", ""},
	} {
		label, text := modeExplained(tc.mode, tc.backend)
		if label != tc.label || !strings.Contains(text, tc.text) || (tc.text == "" && text != "") {
			t.Errorf("modeExplained(%q, %q) = %q, %q", tc.mode, tc.backend, label, text)
		}
	}
}

func TestFormats(t *testing.T) {
	for n, want := range map[int]string{0: "0", 7: "7", 999: "999", 1000: "1,000", 1234567: "1,234,567", -1234: "-1,234", -12: "-12"} {
		if got := count(n); got != want {
			t.Errorf("count(%d) = %q, want %q", n, got, want)
		}
	}
	for d, want := range map[time.Duration]string{
		-time.Second: "0 s", 0: "0 s", 1500 * time.Millisecond: "2 s", 59 * time.Second: "59 s",
		61 * time.Second: "1 min 1 s", 3*time.Hour + 5*time.Minute + 9*time.Second: "3 h 5 min",
		50 * time.Hour: "2 d 2 h",
	} {
		if got := humanDuration(d); got != want {
			t.Errorf("humanDuration(%v) = %q, want %q", d, got, want)
		}
	}
	if plural(1, "peer", "peers") != "1 peer" || plural(0, "peer", "peers") != "0 peers" || plural(2000, "peer", "peers") != "2,000 peers" {
		t.Error("plural")
	}
	if failed(1) != "failed" || failed(3) != "failed 3 times in a row" {
		t.Error("failed")
	}
	if stamp(time.Time{}) != (timestamp{}) {
		t.Error("stamp of the zero time")
	}
	if got := stamp(time.Date(2026, 9, 28, 14, 0, 0, 0, time.FixedZone("CEST", 2*3600))); got.Text != "2026-09-28 12:00:00 UTC" {
		t.Errorf("stamp = %+v, want UTC", got)
	}
}
