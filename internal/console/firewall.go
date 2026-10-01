package console

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Sizes of the firewall view (ADR 0022).
const (
	// entriesPageSize is how many applied entries a page lists.
	entriesPageSize = 50
	// differencesShown is how many rows each kind of difference lists.
	differencesShown = 20
	// entriesTimeout bounds listing the backend's entries. The listing
	// first waits for a pass in progress, which the reconciler bounds
	// itself (enforce.PassTimeout).
	entriesTimeout = 10 * time.Second
)

// firewallPage is the data of the firewall view. Its summary refreshes;
// the differences and the entries are read when the page opens.
type firewallPage struct {
	Fragment string
	Summary  firewallSummary
	// ReadAt is when the entries were read; EntriesErr says why they could
	// not be.
	ReadAt     timestamp
	EntriesErr string
	// Observe explains that nothing is applied, by design, in observe
	// mode; the differences are not compared then.
	Observe string
	// Missing are the decided blocks the firewall does not apply; Stray
	// the entries without a decided block on their range; Expiry the
	// entries whose expiry differs from the decided one.
	Missing differences
	Stray   differences
	Expiry  differences
	// Same is set when no difference was found.
	Same    bool
	Entries []entryRow
	Pager   pager
	// Blocks links to the decided blocks in the decisions list.
	Blocks string
}

// firewallSummary is the refreshing summary of the firewall view.
type firewallSummary struct {
	ReadAt  timestamp
	Summary summary
	Facts   []fact
}

// fact is one line of a list of facts.
type fact struct {
	Label, Value string
	// Note says more; Href links to where it is detailed.
	Note, Href string
}

// differences is one kind of difference between the decided blocks and
// the applied entries.
type differences struct {
	// Count counts them; Rows lists the first ones and More links to all
	// of them, if they are listed elsewhere.
	Count int
	Rows  []differenceRow
	More  string
}

// differenceRow is one difference.
type differenceRow struct {
	Href, Address string
	// Label and Note say what differs.
	Label, Note string
}

// entryRow is an entry the backend applies.
type entryRow struct {
	Href, Address string
	Expires       timestamp
	// Decision names the decision on the entry's range and Note how it
	// matches the entry; Differs is set if it does not.
	Decision, Note string
	Differs        bool
}

// firewallInput is what the firewall view is built from.
type firewallInput struct {
	now      time.Time
	mode     string
	firewall Firewall
	// listing is what the backend applies, read at now; entriesErr why it
	// could not be read.
	listing    FirewallListing
	entriesErr error
	page       int
}

// observing reports whether nothing is applied by design: the node is in
// observe mode, or its last pass was.
func observing(mode string, fw *Firewall) bool {
	return mode == modeObserve || (fw.Pass != nil && fw.Pass.Mode == modeObserve)
}

// buildFirewall builds the firewall view. It compares every entry with
// its decision, but formats only the differences shown and the page of
// entries shown.
func buildFirewall(in firewallInput) firewallPage {
	fw := &in.firewall
	p := firewallPage{
		Fragment: "/api/enforcement",
		Summary:  buildFirewallSummary(in.now, in.mode, fw),
		ReadAt:   stamp(in.now),
		Blocks:   decisionsQuery{state: StateBlock, sort: sortDecided}.href("/decisions"),
	}
	if in.entriesErr != nil {
		p.EntriesErr = in.entriesErr.Error()
	}
	entries := in.listing.Entries
	shown, pg := pageOf(len(entries), in.page)
	p.Pager = pg
	for i := range entries {
		e := &entries[i]
		kind := entryDifference(e, fw.ExpiryTolerance)
		switch kind {
		case differsStray:
			p.Stray.Count++
			if len(p.Stray.Rows) < differencesShown {
				row := newEntryRow(e, kind)
				p.Stray.Rows = append(p.Stray.Rows, differenceRow{Href: row.Href, Address: row.Address, Label: row.Decision, Note: row.Note})
			}
		case differsExpiry:
			p.Expiry.Count++
			if len(p.Expiry.Rows) < differencesShown {
				row := newEntryRow(e, kind)
				p.Expiry.Rows = append(p.Expiry.Rows, differenceRow{Href: row.Href, Address: row.Address,
					Label: "Applied until " + row.Expires.Text, Note: row.Note})
			}
		}
		if i >= shown.first && i < shown.end {
			p.Entries = append(p.Entries, newEntryRow(e, kind))
		}
	}
	if observing(in.mode, fw) {
		p.Observe = "Observe mode: the firewall applies nothing, by design. The node decides and logs every block, " +
			"but none reaches the firewall, so the decided blocks and the entries are not compared. Set node.mode to " +
			"enforce to apply them."
		return p
	}
	missing := &in.listing.Missing
	p.Missing.Count = missing.Total
	for i := range missing.Items {
		it := &missing.Items[i]
		_, label, note := firewallCell(it, fw, in.now)
		p.Missing.Rows = append(p.Missing.Rows, differenceRow{Href: decisionHref(it.Range), Address: rangeText(it.Range),
			Label: label, Note: note})
	}
	if p.Missing.Count > len(p.Missing.Rows) {
		p.Missing.More = decisionsQuery{state: StateBlock, firewall: FirewallNotApplied, sort: sortDecided}.href("/decisions")
	}
	p.Same = p.Missing.Count == 0 && p.Stray.Count == 0 && p.Expiry.Count == 0 && p.EntriesErr == ""
	return p
}

// How an entry differs from the decision on its range.
const (
	differsNot = iota
	// differsStray: no decided block stands behind the entry.
	differsStray
	// differsExpiry: the entry expires at another time than decided.
	differsExpiry
)

// entryDifference says how the entry e differs from the decision on its
// range, expiries within tolerance aside.
func entryDifference(e *FirewallEntry, tolerance time.Duration) int {
	switch {
	case e.State != StateBlock:
		return differsStray
	case e.Expires.Sub(e.ExpiresAt).Abs() > tolerance:
		return differsExpiry
	default:
		return differsNot
	}
}

// newEntryRow describes the entry e, which differs from its decision as
// kind says.
func newEntryRow(e *FirewallEntry, kind int) entryRow {
	row := entryRow{Href: decisionHref(e.Range), Address: rangeText(e.Range), Expires: stamp(e.Expires), Differs: kind != differsNot}
	switch kind {
	case differsStray:
		row.Decision, row.Note = strayText(e)
	case differsExpiry:
		row.Decision, row.Note = "Block", "decided until "+stamp(e.ExpiresAt).Text
	default:
		row.Decision, row.Note = "Block", "until the same time"
	}
	return row
}

// strayText says what the node decided on the range of an entry that no
// decided block stands behind.
func strayText(e *FirewallEntry) (decision, note string) {
	switch {
	case e.Key == "":
		return "No decision", "the node holds no decision on this range: an entry left behind, or added by hand; the next pass removes it"
	case e.State == StateAllowed:
		return "Allowed", "the node allows this range; the next pass removes the entry"
	default:
		return stateLabel(e.State), "no longer a block; the next pass removes the entry"
	}
}

// span is the part of a list a page shows: from first up to end.
type span struct{ first, end int }

// pageOf returns the part of n entries that page n shows, and the pager
// around it.
func pageOf(n, page int) (span, pager) {
	pages := max((n+entriesPageSize-1)/entriesPageSize, 1)
	page = min(max(page, 1), pages)
	first := (page - 1) * entriesPageSize
	shown := span{first, min(first+entriesPageSize, n)}
	var pg pager
	switch {
	case n == 0:
	case pages > 1:
		pg.Text = fmt.Sprintf("Entries %s–%s of %s, page %d of %d", count(first+1), count(shown.end), count(n), page, pages)
	default:
		pg.Text = plural(n, "entry", "entries")
	}
	href := func(page int) string {
		if page == 1 {
			return "/enforcement#entries"
		}
		return "/enforcement?" + url.Values{"page": {strconv.Itoa(page)}}.Encode() + "#entries"
	}
	if page > 1 {
		pg.Prev = href(page - 1)
	}
	if page < pages {
		pg.Next = href(page + 1)
	}
	return shown, pg
}

// buildFirewallSummary sums up the firewall in the node mode mode.
func buildFirewallSummary(now time.Time, mode string, fw *Firewall) firewallSummary {
	f := &fw.Facts
	s := firewallSummary{ReadAt: stamp(now)}
	label, ok := modeLabels[mode]
	if !ok {
		label = mode
	}
	s.Facts = append(s.Facts, fact{Label: "Mode", Value: label, Note: "node.mode"},
		fact{Label: "Backend", Value: f.Backend, Note: "enforce.backend"},
		fact{Label: "Entry limit", Value: count(f.MaxEntries), Note: "enforce.max_entries"})
	pass := fw.Pass
	switch {
	case f.Failures > 0:
		s.Summary = summary{State: stateStopped, Title: "Enforcement is failing: " + failed(f.Failures) + ".",
			Text: fmt.Sprintf("%s The next attempt is in %s.", f.Err, humanDuration(f.RetryIn))}
		if pass != nil {
			s.Summary.Text += " The numbers below are those of the last successful pass."
		}
	case pass == nil:
		s.Summary = summary{State: stateWaiting, Title: "The firewall has not run an enforcement pass yet.",
			Text: "The first pass runs as soon as the decision engine and the firewall have started."}
	case mode == modeObserve || pass.Mode == modeObserve:
		s.Summary = summary{State: stateWaiting, Title: "Observe mode: the firewall applies nothing, by design.",
			Text: "The node decides and logs every block, but none reaches the firewall. Set node.mode to enforce to apply them."}
	case f.Refused+f.Capped > 0:
		s.Summary = summary{State: stateAttention, Title: fmt.Sprintf("%s applied; %s not.", plural(pass.Entries, "entry", "entries"),
			plural(f.Refused+f.Capped, "decided block is", "decided blocks are")),
			Text: "The allow-list refused some blocks right before apply, or enforce.max_entries is reached."}
	default:
		s.Summary = summary{State: stateOK, Title: fmt.Sprintf("%s applied for %s.", plural(pass.Entries, "entry", "entries"),
			plural(f.Blocks, "decided block", "decided blocks")), Text: "Every decided block is applied."}
	}
	if pass == nil {
		return s
	}
	s.Facts = append(s.Facts, fact{Label: "Last pass", Value: stamp(pass.At).Text, Note: "in " + pass.Mode + " mode"})
	if pass.Mode == modeObserve {
		return s
	}
	blocks := decisionsQuery{state: StateBlock, sort: sortDecided}
	notApplied := blocks
	notApplied.firewall = FirewallNotApplied
	s.Facts = append(s.Facts,
		fact{Label: "Entries applied", Value: count(pass.Entries), Note: "what the backend holds after the pass"},
		fact{Label: "Decided blocks", Value: count(f.Blocks), Note: "that the pass considered", Href: blocks.href("/decisions")},
		fact{Label: "Covered", Value: count(f.Covered), Note: "blocks without an entry of their own: a wider or the same range holds them"},
		fact{Label: "Refused", Value: count(f.Refused), Note: "blocks the allow-list refused right before apply",
			Href: notApplied.href("/decisions")},
		fact{Label: "Left out", Value: count(f.Capped), Note: "blocks over enforce.max_entries, the lowest scores",
			Href: notApplied.href("/decisions")},
		fact{Label: "Waiting", Value: count(pass.Deferred), Note: "additions that wait until an entry they overlap expires"})
	return s
}

// firewallContent reads the firewall's condition, the backend's entries
// and the decided blocks the firewall does not apply.
func (c *Console) firewallContent(r *http.Request) any {
	in := firewallInput{now: c.now(), mode: c.node.Mode()}
	in.page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	src := c.node.Decisions
	if src == nil {
		return buildFirewall(in)
	}
	in.firewall = src.Firewall()
	missing := differencesShown
	if observing(in.mode, &in.firewall) {
		missing = 0 // nothing is compared
	}
	ctx, cancel := context.WithTimeout(r.Context(), entriesTimeout)
	defer cancel()
	in.listing, in.entriesErr = src.FirewallEntries(ctx, missing)
	return buildFirewall(in)
}

// firewallSummaryContent reads the firewall's condition for the
// refreshing summary: cheap, without the entries.
func (c *Console) firewallSummaryContent(*http.Request) any {
	var fw Firewall
	if src := c.node.Decisions; src != nil {
		fw = src.Firewall()
	}
	return buildFirewallSummary(c.now(), c.node.Mode(), &fw)
}
