package console

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// allowlistPage is the data of the allow-list view: the lookup, then every
// entry the node never blocks, grouped by origin (ADR 0024). It is read
// when the page opens.
type allowlistPage struct {
	ReadAt timestamp
	// Err says why the allow-list could not be read.
	Err string
	// LoadedAt is when the running configuration built it; Total counts
	// its entries.
	LoadedAt timestamp
	Total    int
	// Address is the address as typed into the lookup; AddressErr says why
	// it is none, and Lookup answers it.
	Address, AddressErr string
	Lookup              *lookupView
	// Warnings are the addresses it should hold but could not determine.
	Warnings []string
	Groups   []allowGroup
}

// lookupView answers "Is this address protected?".
type lookupView struct {
	Address string
	// State is for the stylesheet: protected, allowed, blocked or none;
	// Answer says it in one line.
	State, Answer string
	Ruling        rulingView
	// Others are the other allow-list entries that overlap it, up to
	// maxRuleRows; MoreOthers counts those not listed.
	Others     []allowRow
	MoreOthers int
	// DecisionHref links to its decision; empty if the node cannot decide
	// on it.
	DecisionHref string
}

// allowGroup is the entries of one origin.
type allowGroup struct {
	// ID anchors the group; Title names it and Text says what it holds.
	ID, Title, Text string
	// Protected is set for the entries not even a force-block overrules.
	Protected bool
	// Classes are the built-in entries, by class; Rows the others.
	Classes []classRow
	Rows    []allowRow
	// More counts the entries not listed; Empty explains an empty group.
	More  int
	Empty string
	// File is the state of an allow-list file; nil for other groups.
	File *fileView
}

// classRow is a class of built-in ranges.
type classRow struct {
	Class  string
	Ranges []allowRow
}

// allowRow is an allow-list entry.
type allowRow struct {
	// Range is the address or network; Label says where it comes from.
	Range, Label string
}

// fileView is an allow-list file: what was loaded, and what it holds now.
type fileView struct {
	Path string
	// Loaded says how many entries were loaded, and when.
	Loaded string
	// State is ok, changed or warning for the stylesheet; Status says it.
	State, Status string
	// Rejected are the lines the node rejects now; MoreRejected counts
	// those not listed.
	Rejected     []RejectedLine
	MoreRejected int
}

// Lookup states.
const (
	lookupProtected = "protected"
	lookupAllowed   = "allowed"
	lookupBlocked   = "blocked"
	lookupNone      = "none"
)

// allowlistInput is what the allow-list view is built from.
type allowlistInput struct {
	now   time.Time
	allow Allowlist
	err   error
	// address is the lookup as typed; addressErr says why it is none, and
	// protection answers it, or protectionErr says why it could not.
	address       string
	addressErr    error
	protection    *Protection
	protectionErr error
}

// allowGroups are the groups of the allow-list by source, in the order of
// their precedence; the files follow them.
var allowGroups = []struct{ source, id, title, text string }{
	{"builtin", "builtin", "Built-in ranges", "Special-purpose address space that is never reachable from the public " +
		"internet, and so is never blocked on any node."},
	{"self", "self", "This node's addresses", "The addresses this node listens on in mesh.listen; for an unspecified " +
		"listen address, the addresses of every network interface."},
	{"bootstrap", "bootstrap", "Bootstrap peers", "The addresses of the peers in mesh.bootstrap, with their DNS names " +
		"resolved, so the node never cuts itself off from the mesh."},
	{"config", "cidrs", "Configured networks", "The addresses and networks in allowlist.cidrs."},
}

// buildAllowlist builds the allow-list view.
func buildAllowlist(in allowlistInput) allowlistPage {
	p := allowlistPage{ReadAt: stamp(in.now), Address: in.address, LoadedAt: stamp(in.allow.LoadedAt),
		Total: len(in.allow.Entries)}
	if in.err != nil {
		p.Err = in.err.Error()
		return p
	}
	if in.addressErr != nil {
		p.AddressErr = in.addressErr.Error()
	}
	if in.protectionErr != nil {
		p.AddressErr = "The lookup failed: " + in.protectionErr.Error()
	}
	if in.protection != nil {
		p.Lookup = newLookupView(in.protection)
	}
	for _, w := range in.allow.Warnings {
		p.Warnings = append(p.Warnings, warningText(w))
	}
	for _, g := range allowGroups {
		group := allowGroup{ID: g.id, Title: g.title, Text: g.text, Protected: sourceProtected(g.source)}
		var entries []AllowEntry
		for _, e := range in.allow.Entries {
			if e.Source == g.source {
				entries = append(entries, e)
			}
		}
		if g.source == "builtin" {
			group.Classes = builtinClasses(entries)
		} else {
			group.Rows, group.More = allowRows(entries, "")
		}
		if len(entries) == 0 {
			group.Empty = emptyGroup(g.source)
		}
		p.Groups = append(p.Groups, group)
	}
	for i := range in.allow.Files {
		p.Groups = append(p.Groups, fileGroup(&in.allow.Files[i], in.allow.Entries, i, stamp(in.allow.LoadedAt)))
	}
	return p
}

// sourceProtected reports whether entries of source are protected: not
// even a force-block overrules them.
func sourceProtected(source string) bool {
	return source == "builtin" || source == "self" || source == "bootstrap"
}

// builtinClasses groups the built-in entries by their class, e.g.
// "Loopback", each range with the rest of its label.
func builtinClasses(entries []AllowEntry) []classRow {
	var out []classRow
	for _, e := range entries {
		class, detail := classOf(e.Label)
		if n := len(out); n == 0 || out[n-1].Class != class {
			out = append(out, classRow{Class: class})
		}
		out[len(out)-1].Ranges = append(out[len(out)-1].Ranges, allowRow{Range: rangeText(e.Range), Label: detail})
	}
	return out
}

// classOf splits a built-in entry's label, e.g. "private (RFC 1918)", into
// its class and the rest.
func classOf(label string) (class, detail string) {
	class, detail, _ = strings.Cut(label, " (")
	detail = strings.TrimSuffix(detail, ")")
	if c, d, ok := strings.Cut(class, " / "); ok {
		class, detail = c, d
	}
	if class != "" {
		class = strings.ToUpper(class[:1]) + class[1:]
	}
	return class, detail
}

// allowRows lists up to maxRuleRows of entries, each labeled where it
// comes from; the prefix of a file entry's label is left out. It returns
// how many are not listed.
func allowRows(entries []AllowEntry, filePrefix string) ([]allowRow, int) {
	more := 0
	if len(entries) > maxRuleRows {
		more, entries = len(entries)-maxRuleRows, entries[:maxRuleRows]
	}
	rows := make([]allowRow, len(entries))
	for i, e := range entries {
		rows[i] = allowRow{Range: rangeText(e.Range), Label: entryLabel(e, filePrefix)}
	}
	return rows, more
}

// entryLabel says where the entry e comes from.
func entryLabel(e AllowEntry, filePrefix string) string {
	switch {
	case e.Source == "file" && filePrefix != "" && strings.HasPrefix(e.Label, filePrefix):
		return "line " + strings.TrimPrefix(e.Label, filePrefix)
	default:
		return e.Label
	}
}

// emptyGroup explains why the group of source holds no entry.
func emptyGroup(source string) string {
	switch source {
	case "self":
		return "None: this node listens on no address it could list."
	case "bootstrap":
		return "None: no peer is configured in mesh.bootstrap."
	case "config":
		return "None: allowlist.cidrs lists no network."
	default:
		return "None."
	}
}

// fileGroup is the group of the i-th allow-list file f: the entries loaded
// from it, and its state on disk.
func fileGroup(f *AllowFile, entries []AllowEntry, i int, loadedAt timestamp) allowGroup {
	g := allowGroup{ID: "file-" + strconv.Itoa(i+1), Title: "File " + f.Path,
		Text: "Addresses and networks in this file of allowlist.files, read again on every reload."}
	prefix := f.Path + ":"
	var mine []AllowEntry
	for _, e := range entries {
		if e.Source == "file" && strings.HasPrefix(e.Label, prefix) {
			mine = append(mine, e)
		}
	}
	g.Rows, g.More = allowRows(mine, prefix)
	if len(mine) == 0 {
		g.Empty = "None: the file held no entry when it was loaded."
	}
	v := &fileView{Path: f.Path, Loaded: plural(f.Loaded, "entry", "entries") + " loaded"}
	if loadedAt.Text != "" {
		v.Loaded += " at " + loadedAt.Text
	}
	const keeps = "The entries loaded stay in effect, but a reload would be rejected, and obied would not start"
	switch {
	case f.Err != "":
		v.State, v.Status = "warning", "It cannot be read now: "+f.Err+". "+keeps+" until it can be read again."
	case f.RejectedLines > 0:
		v.State = "warning"
		v.Status = fmt.Sprintf("It now holds %s the node rejects. %s until the file is fixed.",
			plural(f.RejectedLines, "line", "lines"), keeps)
		v.Rejected, v.MoreRejected = f.Rejected, f.RejectedLines-len(f.Rejected)
	case f.Changed:
		v.State, v.Status = "changed", fmt.Sprintf("It changed since it was loaded and now holds %s: they are not "+
			"active until a reload applies them.", plural(f.Entries, "entry", "entries"))
	default:
		v.State, v.Status = "ok", "Unchanged since it was loaded."
	}
	g.File = v
	return g
}

// warningText says which address the allow-list could not determine.
func warningText(w AllowWarning) string {
	if w.Source == "bootstrap" {
		return fmt.Sprintf("The bootstrap peer %s did not resolve: %s. Its addresses are not protected until a reload "+
			"resolves them.", w.Subject, w.Err)
	}
	return fmt.Sprintf("The interface addresses for %s could not be listed: %s. This node's own addresses are not "+
		"protected; list its public addresses in allowlist.cidrs.", w.Subject, w.Err)
}

// newLookupView answers "Is this address protected?" for pr.
func newLookupView(pr *Protection) *lookupView {
	r := &pr.Ruling
	addr := rangeText(pr.Range)
	v := &lookupView{Address: addr, Ruling: newRulingView(r)}
	if pr.Decidable {
		v.DecisionHref = decisionHref(pr.Range)
	}
	switch {
	case r.Rule == ruleAllowlist && r.Protected:
		v.State, v.Answer = lookupProtected, fmt.Sprintf("Yes: %s is protected by %s. It is never blocked, "+
			"not even by an always-block override.", addr, protectingText(r))
	case r.Rule == ruleAllowlist:
		v.State, v.Answer = lookupAllowed, fmt.Sprintf("Yes: the allow-list entry %s (%s) covers %s, so it is never "+
			"blocked, whatever the verdicts. Only an always-block override on it, or on a network around it, would "+
			"overrule the entry.",
			r.Match, sourceText(r.Source), addr)
	case r.Rule == ruleForceAllow:
		v.State, v.Answer = lookupAllowed, fmt.Sprintf("Yes, by your always-allow override on %s: %s is not blocked, "+
			"whatever the verdicts, %s.", matchText(r.Match), addr, untilText(r.ExpiresAt))
	case r.Rule == ruleForceBlock:
		on := ""
		if m := matchText(r.Match); m != addr {
			on = " on " + m
		}
		v.State, v.Answer = lookupBlocked, fmt.Sprintf("No: your always-block override%s blocks %s, %s.", on, addr,
			untilText(r.ExpiresAt))
		if len(pr.Overlapping) > 0 {
			v.Answer += " It overrules the allow-list entries that cover it."
		}
	default:
		v.State, v.Answer = lookupNone, fmt.Sprintf("No: no allow-list entry or override covers %s. The verdicts "+
			"decide whether it is blocked.", addr)
	}
	for _, e := range pr.Overlapping {
		if r.Rule == ruleAllowlist && e.Range.String() == r.Match && e.Label == r.Label {
			continue // the entry that decides
		}
		if len(v.Others) == maxRuleRows {
			v.MoreOthers++
			continue
		}
		label := sourceText(e.Source)
		if l := entryLabel(e, ""); l != "" {
			label += ": " + l
		}
		v.Others = append(v.Others, allowRow{Range: rangeText(e.Range), Label: label})
	}
	return v
}

// protectingText names the protected allow-list entry of the ruling r, e.g.
// "the built-in range 10.0.0.0/8 (Private)", with the class the allow-list
// view lists a built-in range under.
func protectingText(r *Ruling) string {
	if r.Source != "builtin" {
		return sourceText(r.Source) + " " + r.Match
	}
	s := "the built-in range " + r.Match
	if class, _ := classOf(r.Label); class != "" {
		s += " (" + class + ")"
	}
	return s
}

// untilText says until when an override lasts.
func untilText(expires time.Time) string {
	if expires.IsZero() {
		return "until you remove the override"
	}
	return "until " + stamp(expires).Text
}

// allowlistContent reads the allow-list, and answers the lookup if the
// request asks it.
func (c *Console) allowlistContent(r *http.Request) any {
	address := bounded(strings.TrimSpace(r.URL.Query().Get("address")))
	in := allowlistInput{now: c.now(), address: address}
	src := c.node.Rules
	if src == nil {
		in.err = errNoRules
		return buildAllowlist(in)
	}
	in.allow = src.Allowlist()
	if address != "" {
		p, err := parseRange(address)
		if err != nil {
			in.addressErr = err
		} else if pr, err := src.Protection(p); err != nil {
			in.protectionErr = err
		} else {
			in.protection = &pr
		}
	}
	return buildAllowlist(in)
}
