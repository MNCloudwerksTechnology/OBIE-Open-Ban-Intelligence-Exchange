package console

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
)

// startupGrace is how long a node without peers or data counts as just
// started: until then the overview explains what will appear instead of
// raising conditions (ADR 0020).
const startupGrace = 2 * time.Minute

// The subsystems whose numbers the overview shows, by lifecycle name. The
// console imports none of their packages.
const (
	partMesh     = "mesh"
	partStore    = "store"
	partDecision = "decision"
	partEnforce  = "enforce"
	partAdmin    = "admin"
)

var (
	// partOrder lists the parts the overview shows first; the others follow
	// in the order the node starts them.
	partOrder = []string{partMesh, partStore, partDecision, partEnforce, partAdmin}
	// partTitles name the parts for people.
	partTitles = map[string]string{
		partMesh: "Mesh", partStore: "Store", partDecision: "Decision engine", partEnforce: "Enforcement",
		partAdmin: "Admin interface", "ops": "Metrics endpoint", "audit": "Audit log", Name: "Web console",
	}
	// partPhrases name the parts inside a sentence.
	partPhrases = map[string]string{
		partMesh: "the mesh", partStore: "the store", partDecision: "the decision engine", partEnforce: "enforcement",
	}
)

// States of a key number, a part and the summary, for the stylesheet.
const (
	stateEmpty     = "empty"
	stateWaiting   = "waiting"
	stateError     = "error"
	stateReady     = "ready"
	stateWarning   = "warning"
	stateStopped   = "stopped"
	stateOK        = "ok"
	stateAttention = "attention"
)

// overviewPage is the data of the overview's refreshing region.
type overviewPage struct {
	// ReadAt is when the data was read.
	ReadAt     timestamp
	Summary    summary
	Conditions []condition
	// Starting explains what will appear on a node that has just started
	// and still lacks peers or data; nil otherwise.
	Starting *starting
	Numbers  []keyNumber
	// Activity is the last entries of the activity timeline; nil without
	// one (ADR 0025).
	Activity *recentActivity
	Parts    []part
	Node     nodeFacts
}

// timestamp is a moment for a <time> element.
type timestamp struct {
	ISO, Text string
}

// summary answers "is my node healthy?" in one line.
type summary struct {
	// State is stateOK, stateAttention, stateWaiting or stateStopped.
	State, Title, Text string
}

// condition is something that needs the operator's attention.
type condition struct {
	// Warning conditions need action; the others only inform.
	Warning bool
	// Title says what is wrong, Next what to do about it.
	Title, Next string
	// Command helps with the next step; empty if none does.
	Command string
}

// starting is the empty state of a node that has just started.
type starting struct {
	Uptime string
	Items  []startingItem
}

// startingItem says when something will appear.
type startingItem struct {
	What, When string
}

// keyNumber is one of the overview's numbers.
type keyNumber struct {
	Label, Value, Note string
	// Href links to the view that details the number; while that view
	// does not exist, Command names the obiectl command that shows it.
	Href, Command string
	// State is "" for a number, stateEmpty for none yet, stateWaiting
	// while its part does not run and stateError if it could not be read.
	State string
}

// part is the readiness of one part of the node.
type part struct {
	Title string
	// State is stateReady, stateWaiting, stateWarning or stateStopped;
	// Label names it.
	State, Label, Detail string
}

// nodeFacts identify the node and its mode.
type nodeFacts struct {
	PeerID, Fingerprint, Version, Uptime string
	StartedAt, ConfigLoadedAt            timestamp
	// ConfigLoadedBy says how the running configuration was loaded;
	// ConfigHref links to the configuration view, empty without it.
	ConfigLoadedBy, ConfigHref string
	ModeLabel, ModeText        string
}

// overviewInput is what the overview is built from.
type overviewInput struct {
	now time.Time
	// node holds the version, the identity and the start time.
	node     Node
	mode     string
	statuses []lifecycle.Status
	facts    Facts
	// link returns the link to the view at path, or the obiectl command
	// that shows the same while that view does not exist.
	link func(path, command string) (href, cmd string)
}

// overview builds an overviewPage.
type overview struct {
	overviewInput
	status map[string]lifecycle.Status
	uptime time.Duration
	// fresh is set while the node has just started and still lacks peers
	// or data.
	fresh bool
}

// buildOverview turns the node's statuses and facts into the overview.
func buildOverview(in overviewInput) overviewPage {
	o := &overview{overviewInput: in, status: map[string]lifecycle.Status{}, uptime: max(in.now.Sub(in.node.StartedAt), 0)}
	for _, s := range in.statuses {
		o.status[s.Name] = s
	}
	o.fresh = o.uptime < startupGrace && (in.facts.Peers.Connected == 0 || o.noData())
	conds := o.conditions()
	p := overviewPage{
		ReadAt:     stamp(in.now),
		Summary:    o.summary(conds),
		Conditions: conds,
		Numbers:    o.numbers(),
		Parts:      o.parts(),
		Node:       o.nodeFacts(),
	}
	if o.fresh {
		p.Starting = o.starting()
	}
	return p
}

// noData reports whether the node holds no indicator and no verdict yet.
func (o *overview) noData() bool {
	return o.facts.Decisions.Indicators == 0 && o.facts.Store.VerdictRecords == 0
}

// running reports whether the subsystem name runs.
func (o *overview) running(name string) bool {
	return o.status[name].State == lifecycle.StateRunning
}

// summary sums the node's health and the conditions up.
func (o *overview) summary(conds []condition) summary {
	warnings := 0
	for _, c := range conds {
		if c.Warning {
			warnings++
		}
	}
	notes := len(conds) - warnings
	switch h := nodeHealth(o.statuses); {
	case h.State == HealthStopping:
		return summary{stateStopped, "Shutting down", "obied is stopping its parts; the numbers below are the last ones read."}
	case h.State == HealthStarting:
		return summary{stateWaiting, "Starting", "Some parts of the node are still starting; their numbers appear once they run."}
	case warnings == 1:
		return summary{stateAttention, "Needs attention", "1 condition below needs your attention."}
	case warnings > 1:
		return summary{stateAttention, "Needs attention", strconv.Itoa(warnings) + " conditions below need your attention."}
	case o.fresh:
		return summary{stateWaiting, "Just started", "Every part of the node runs; peers and data are still arriving."}
	case notes > 0:
		return summary{stateOK, "Healthy", "Every part of the node is ready and nothing needs action now; see the note below."}
	default:
		return summary{stateOK, "Healthy", "Every part of the node is ready and nothing needs your attention."}
	}
}

// parts returns the readiness of every part: the main ones first.
func (o *overview) parts() []part {
	out := make([]part, 0, len(o.statuses))
	for _, name := range partOrder {
		if s, ok := o.status[name]; ok {
			out = append(out, o.partOf(s))
		}
	}
	for _, s := range o.statuses {
		if !slices.Contains(partOrder, s.Name) {
			out = append(out, o.partOf(s))
		}
	}
	return out
}

// partOf describes the readiness of the subsystem with status s; a mesh
// without peers on a node that has just started is waiting for them, not
// degraded.
func (o *overview) partOf(s lifecycle.Status) part {
	p := partOf(s)
	if o.fresh && s.Name == partMesh && p.Label == "Degraded" && o.facts.Peers.Connected == 0 {
		p.State, p.Label = stateWaiting, "Waiting for peers"
	}
	return p
}

// partOf describes the readiness of the subsystem with status s.
func partOf(s lifecycle.Status) part {
	p := part{Title: partTitle(s.Name), Detail: s.Detail}
	switch s.State {
	case lifecycle.StatePending:
		p.State, p.Label = stateWaiting, "Waiting to start"
	case lifecycle.StateStarting:
		p.State, p.Label = stateWaiting, "Starting"
	case lifecycle.StateRunning:
		if reason, degraded := degradedReason(s.Detail); !s.Ready {
			p.State, p.Label, p.Detail = stateWarning, "Not ready", notReadyReason(s)
		} else if degraded {
			p.State, p.Label, p.Detail = stateWarning, "Degraded", reason
		} else {
			p.State, p.Label = stateReady, "Ready"
		}
	case lifecycle.StateStopping:
		p.State, p.Label = stateStopped, "Stopping"
	case lifecycle.StateStopped:
		p.State, p.Label = stateStopped, "Stopped"
	default:
		p.State, p.Label, p.Detail = stateWarning, "Failed", s.Error
	}
	return p
}

func partTitle(name string) string {
	if t, ok := partTitles[name]; ok {
		return t
	}
	return name
}

// numbers returns the key numbers.
func (o *overview) numbers() []keyNumber {
	d := o.facts.Decisions
	indicators := o.number(partDecision, "Indicators held", d.Indicators,
		"with "+plural(d.Verdicts, "active verdict", "active verdicts"), "/verdicts", "obiectl indicators")
	return []keyNumber{
		o.peersNumber(),
		o.arriving(indicators, d.Indicators, "verdicts from peers and local reports appear here"),
		o.decisionNumber("block", d.Block, "score and quorum reached, or force-blocked"),
		o.decisionNumber("none", d.None, "held, but below the threshold or the quorum"),
		o.decisionNumber("allowed", d.Allowed, "protected by the allow-list or a force-allow"),
		o.entriesNumber(),
		o.overridesNumber(),
	}
}

// number is the key number n of the part owner, or why it is not shown.
func (o *overview) number(owner, label string, n int, note, path, command string) keyNumber {
	k := keyNumber{Label: label}
	k.Href, k.Command = o.link(path, command)
	if value, why, waiting := o.waiting(owner); waiting {
		k.Value, k.Note, k.State = value, why, stateWaiting
		return k
	}
	k.Value, k.Note = count(n), note
	return k
}

// arriving shows the zero number k, of data that arrives over time, as
// none yet with note while the node has just started.
func (o *overview) arriving(k keyNumber, n int, note string) keyNumber {
	if k.State == "" && n == 0 && o.fresh {
		k.Value, k.Note, k.State = "None yet", note, stateEmpty
	}
	return k
}

// waiting returns what a number of the part owner shows while it does not
// run, and whether it does not.
func (o *overview) waiting(owner string) (value, note string, waiting bool) {
	s, ok := o.status[owner]
	phrase := partPhrases[owner]
	switch {
	case !ok:
		return "Not available", partTitle(owner) + " is not part of this node", true
	case s.State == lifecycle.StateRunning:
		return "", "", false
	case s.State == lifecycle.StatePending || s.State == lifecycle.StateStarting:
		return "Waiting", "for " + phrase + " to start", true
	default:
		return "Stopped", phrase + " is not running", true
	}
}

func (o *overview) peersNumber() keyNumber {
	p := o.facts.Peers
	note, empty := "no peer is configured in mesh.bootstrap", "no peer is configured in mesh.bootstrap"
	if p.Configured > 0 {
		note = fmt.Sprintf("%d of %d configured connected", p.Bootstrap, p.Configured)
		empty = "connecting to " + plural(p.Configured, "configured peer", "configured peers")
	}
	return o.arriving(o.number(partMesh, "Peers connected", p.Connected, note, "/peers", "obiectl peers"), p.Connected, empty)
}

func (o *overview) decisionNumber(state string, n int, note string) keyNumber {
	k := o.number(partDecision, "Decisions: "+state, n, note, "/decisions?state="+state, "obiectl decisions --state "+state)
	return o.arriving(k, n, note)
}

func (o *overview) entriesNumber() keyNumber {
	e := o.facts.Enforce
	k := o.number(partEnforce, "Firewall entries", e.Applied, entriesNote(e), "/enforcement", "obiectl enforced")
	switch {
	case k.State != "":
	case e.Mode == "":
		k.Value, k.Note, k.State = "Waiting", "for the first enforcement pass", stateWaiting
	case e.Mode != "enforce":
		k.Value, k.Note = "None", "observe mode applies no block"
	default:
		k = o.arriving(k, e.Applied, "blocks are applied within seconds of being decided")
	}
	return k
}

// entriesNote says who applies the entries and how the decided blocks
// came to them.
func entriesNote(e EnforceFacts) string {
	var note string
	switch e.Backend {
	case "dryrun":
		note = "kept by the dry-run backend, which blocks nothing"
	case "":
		note = "applied by the enforcement backend"
	default:
		note = "applied by " + e.Backend
	}
	var why []string
	if e.Covered > 0 {
		why = append(why, plural(e.Covered, "shares an entry with another block", "share an entry with another block"))
	}
	if e.Refused > 0 {
		why = append(why, count(e.Refused)+" refused by the allow-list")
	}
	if e.Capped > 0 {
		why = append(why, count(e.Capped)+" over enforce.max_entries")
	}
	if len(why) > 0 {
		note += " for " + plural(e.Blocks, "decided block", "decided blocks") + ": " + strings.Join(why, ", ")
	}
	return note
}

func (o *overview) overridesNumber() keyNumber {
	s := o.facts.Store
	k := o.number(partStore, "Active overrides", s.Overrides, "force-allows and force-blocks you set", "/overrides", "obiectl overrides")
	if k.State == "" && s.OverridesErr != nil {
		k.Value, k.Note, k.State = "Not available", "reading them failed: "+s.OverridesErr.Error(), stateError
	}
	return k
}

// nodeFacts identify the node and explain its mode.
func (o *overview) nodeFacts() nodeFacts {
	n := nodeFacts{
		PeerID:         o.node.PeerID,
		Fingerprint:    o.node.Fingerprint,
		Version:        o.node.Version,
		Uptime:         humanDuration(o.uptime),
		StartedAt:      stamp(o.node.StartedAt),
		ConfigLoadedAt: stamp(o.facts.Config.LoadedAt),
		ConfigLoadedBy: "at start",
	}
	if o.facts.Config.Reloaded {
		n.ConfigLoadedBy = "by a reload"
	}
	n.ConfigHref, _ = o.link("/configuration", "")
	n.ModeLabel, n.ModeText = modeExplained(o.mode, o.facts.Enforce.Backend)
	return n
}

// modeExplained names node.mode and says in plain words what it means
// with the enforcement backend.
func modeExplained(mode, backend string) (label, text string) {
	switch {
	case mode == "observe":
		return "Observe", "The node decides and shows what it would block, but blocks nothing: the firewall is left alone. " +
			"Switch node.mode to enforce once the decisions look right."
	case mode != "enforce":
		return mode, ""
	case backend == "dryrun":
		return "Enforce", "The node applies what it decides to block to the dry-run backend, which only logs it: " +
			"the firewall is left alone."
	case backend == "":
		return "Enforce", "The node blocks what it decides to block, through the enforcement backend, until each decision expires."
	default:
		return "Enforce", "The node blocks what it decides to block: the " + backend +
			" backend applies each block to the firewall until the decision expires."
	}
}

// starting says what will appear on a node that has just started, and
// when.
func (o *overview) starting() *starting {
	s := &starting{Uptime: humanDuration(o.uptime)}
	if p := o.facts.Peers; p.Connected == 0 && p.Configured > 0 {
		s.Items = append(s.Items, startingItem{"Peers", fmt.Sprintf("The mesh is dialing the %s now; they usually "+
			"connect within a minute. If none has connected after %d minutes, this page says so.",
			plural(p.Configured, "configured peer", "configured peers"), int(startupGrace.Minutes()))})
	} else if p.Connected == 0 {
		s.Items = append(s.Items, startingItem{"Peers", "None will connect by themselves: no peer is configured in " +
			"mesh.bootstrap. Other nodes that list this one can still connect to it."})
	}
	if o.noData() {
		blocks := "The firewall applies each block within seconds of the decision."
		if o.mode != "enforce" {
			blocks = "In observe mode they are shown, not applied: the firewall blocks nothing."
		}
		s.Items = append(s.Items,
			startingItem{"Verdicts", "They arrive as peers relay them, and when this node reports an attack itself " +
				"(obiectl report, or the Fail2Ban action on every ban)."},
			startingItem{"Decisions", "Each indicator is decided within a second of its verdicts arriving; it is " +
				"blocked once enough trusted publishers agree (decision.threshold and decision.quorum)."},
			startingItem{"Blocks", blocks})
	}
	return s
}

// conditions returns what needs the operator's attention: warnings first,
// then notes.
func (o *overview) conditions() []condition {
	var all []condition
	all = append(all, o.meshConditions()...)
	all = append(all, o.enforceConditions()...)
	all = append(all, o.partConditions()...)
	all = append(all, o.configConditions()...)
	out := make([]condition, 0, len(all))
	for _, warning := range []bool{true, false} {
		for _, c := range all {
			if c.Warning == warning {
				out = append(out, c)
			}
		}
	}
	return out
}

// meshConditions are the missing peers and events, after the startup
// grace.
func (o *overview) meshConditions() []condition {
	if o.uptime < startupGrace {
		return nil
	}
	var out []condition
	p := o.facts.Peers
	switch {
	case !o.running(partMesh) || p.Connected > 0:
	case p.Configured == 0:
		out = append(out, condition{Warning: true,
			Title: "No peer is configured, so this node hears only its own reports.",
			Next:  "Add the peers of your mesh to mesh.bootstrap and restart obied; the federation guide explains how."})
	default:
		title := fmt.Sprintf("No peer is connected: none of the %s configured peers answers.", count(p.Configured))
		if p.Configured == 1 {
			title = "No peer is connected: the configured peer does not answer."
		}
		out = append(out, condition{Warning: true,
			Title:   title,
			Next:    "Check that the peers run and that their mesh port is reachable from this host; the obied log names the failed dials.",
			Command: "obiectl peers"})
	}
	if s := o.facts.Store; o.running(partStore) && s.VerdictRecords == 0 && s.EventsAccepted == 0 {
		out = append(out, condition{Warning: true,
			Title: "No event received: this node holds no verdict and has accepted none since obied started.",
			Next: "Connect it to peers that publish verdicts, or report attacks on this host yourself; " +
				"the Fail2Ban action reports every ban.",
			Command: "obiectl report --help"})
	}
	return out
}

// enforceConditions are the ways the firewall may not apply what the node
// decided.
func (o *overview) enforceConditions() []condition {
	if !o.running(partEnforce) {
		return nil
	}
	e := o.facts.Enforce
	var out []condition
	if o.mode == "enforce" && e.Backend == "dryrun" {
		out = append(out, condition{Warning: true,
			Title: "Enforce mode, but nothing is applied to the firewall: the dry-run backend only logs the blocks.",
			Next:  "Set enforce.backend to nftables and restart obied."})
	}
	switch {
	case e.Failures > 0 && e.Mode == "observe":
		out = append(out, condition{Warning: true,
			Title: fmt.Sprintf("The firewall may still apply blocks of an earlier enforce run: withdrawing them %s: %s",
				failed(e.Failures), e.Err),
			Next:    "The obied log says why. obied retries in " + humanDuration(e.RetryIn) + ".",
			Command: "obiectl enforced"})
	case e.Failures > 0:
		title := "Decided blocks and applied entries may differ"
		if e.Applied == 0 && o.facts.Decisions.Block > 0 {
			title = "Enforce mode, but nothing is applied"
		}
		out = append(out, condition{Warning: true,
			Title: fmt.Sprintf("%s: enforcement %s: %s", title, failed(e.Failures), e.Err),
			Next: "The obied log says why; check that obied may change the firewall. obied retries in " +
				humanDuration(e.RetryIn) + ".",
			Command: "obiectl enforced"})
	}
	if e.Mode == "enforce" && e.Refused > 0 {
		title := fmt.Sprintf("%s not applied: the allow-list refuses them right before apply.",
			plural(e.Refused, "decided block is", "decided blocks are"))
		if e.Applied == 0 {
			title = "Enforce mode, but nothing is applied: the allow-list refuses every decided block right before apply."
		}
		out = append(out, condition{Warning: true, Title: title,
			Next: "Explain a blocked address to see which allow-list entry protects it; if it should be blocked, " +
				"remove it from the allow-list and reload obied.",
			Command: "obiectl explain <address>"})
	}
	if e.Mode == "enforce" && e.Capped > 0 {
		out = append(out, condition{Warning: true,
			Title: fmt.Sprintf("%s not applied: the firewall holds at most %s (enforce.max_entries), and the "+
				"lowest-score blocks are left out.", plural(e.Capped, "decided block is", "decided blocks are"),
				plural(e.MaxEntries, "entry", "entries")),
			Next: "Raise enforce.max_entries and restart obied, if the host can hold more entries."})
	}
	return out
}

// partConditions are the running parts that are not ready or degraded,
// unless another condition or the startup explains them.
func (o *overview) partConditions() []condition {
	var out []condition
	for _, s := range o.statuses {
		if s.State != lifecycle.StateRunning || o.explained(s) {
			continue
		}
		reason, degraded := degradedReason(s.Detail)
		what := "is degraded"
		switch {
		case !s.Ready:
			reason, what = notReadyReason(s), "is not ready"
		case !degraded:
			continue
		}
		out = append(out, condition{Warning: true,
			Title:   fmt.Sprintf("%s %s: %s", partTitle(s.Name), what, reason),
			Next:    "The obied log says why; obiectl status shows every part of the node.",
			Command: "obiectl status"})
	}
	return out
}

// explained reports whether another condition, or the startup, already
// explains why the running subsystem s is not ready or degraded.
func (o *overview) explained(s lifecycle.Status) bool {
	switch s.Name {
	case partMesh:
		return s.Ready && o.facts.Peers.Connected == 0 // no peer yet, or the missing-peer condition
	case partEnforce:
		return o.facts.Enforce.Failures > 0
	}
	return false
}

// configConditions are a rejected reload and changes waiting for a
// restart.
func (o *overview) configConditions() []condition {
	c := o.facts.Config
	var out []condition
	if c.Rejected != "" {
		out = append(out, condition{Warning: true,
			Title: fmt.Sprintf("The configuration reload at %s was rejected: %s", stamp(c.RejectedAt).Text, c.Rejected),
			Next: fmt.Sprintf("The node keeps the configuration loaded at %s. Fix the file, check it, then reload obied again.",
				stamp(c.LoadedAt).Text),
			Command: "obied --check-config"})
	}
	if len(c.RestartKeys) > 0 {
		out = append(out, condition{
			Title:   "Changes to " + strings.Join(c.RestartKeys, ", ") + " wait for a restart: a reload does not apply them.",
			Next:    "Restart obied when it suits you.",
			Command: "sudo systemctl restart obied"})
	}
	return out
}

// stamp formats t in UTC, like obiectl; zero stays empty.
func stamp(t time.Time) timestamp {
	if t.IsZero() {
		return timestamp{}
	}
	u := t.UTC()
	return timestamp{ISO: u.Format(time.RFC3339), Text: u.Format("2006-01-02 15:04:05 UTC")}
}

// humanDuration formats d for people, to the second below an hour.
func humanDuration(d time.Duration) string {
	d = max(d, 0).Round(time.Second)
	h, m, s := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d s", s)
	case d < time.Hour:
		return fmt.Sprintf("%d min %d s", m, s)
	case h < 24:
		return fmt.Sprintf("%d h %d min", h, m)
	default:
		return fmt.Sprintf("%d d %d h", h/24, h%24)
	}
}

// count formats n with thousands separators.
func count(n int) string {
	digits := strconv.Itoa(n)
	sign := ""
	if n < 0 {
		sign, digits = "-", digits[1:]
	}
	for i := len(digits) - 3; i > 0; i -= 3 {
		digits = digits[:i] + "," + digits[i:]
	}
	return sign + digits
}

// plural returns n with the noun one or many.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return count(n) + " " + many
}

// failed says how often something failed in a row.
func failed(n int) string {
	if n == 1 {
		return "failed"
	}
	return "failed " + count(n) + " times in a row"
}
