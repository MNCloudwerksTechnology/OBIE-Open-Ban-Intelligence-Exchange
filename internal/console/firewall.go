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
	// entriesTimeout bounds reading the backend's entries, which waits for
	// a pass in progress.
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
	// entries are the backend's entries, read at now; entriesErr why they
	// could not be.
	entries    []FirewallEntry
	entriesErr error
	// missing is the first page of the decided blocks the firewall does
	// not apply.
	missing DecisionPage
	page    int
}

// buildFirewall builds the firewall view.
func buildFirewall(in firewallInput) firewallPage {
	fw := &in.firewall
	notApplied := decisionsQuery{state: StateBlock, firewall: FirewallNotApplied, sort: sortDecided}.href("/decisions")
	p := firewallPage{
		Fragment: "/api/enforcement",
		Summary:  buildFirewallSummary(in.now, in.mode, fw),
		ReadAt:   stamp(in.now),
		Blocks:   decisionsQuery{state: StateBlock, sort: sortDecided}.href("/decisions"),
	}
	if in.entriesErr != nil {
		p.EntriesErr = in.entriesErr.Error()
	}
	for i := range in.entries {
		e := &in.entries[i]
		row := entryRow{Href: decisionHref(e.Range), Address: rangeText(e.Range), Expires: stamp(e.Expires)}
		switch {
		case e.State != StateBlock:
			row.Differs = true
			row.Decision, row.Note = strayText(e)
			p.Stray.add(differenceRow{Href: row.Href, Address: row.Address, Label: row.Decision, Note: row.Note})
		case e.Expires.Sub(e.ExpiresAt).Abs() > fw.ExpiryTolerance:
			row.Differs = true
			row.Decision = "Block"
			row.Note = "decided until " + stamp(e.ExpiresAt).Text
			p.Expiry.add(differenceRow{Href: row.Href, Address: row.Address,
				Label: "Applied until " + stamp(e.Expires).Text, Note: row.Note})
		default:
			row.Decision, row.Note = "Block", "until the same time"
		}
		p.Entries = append(p.Entries, row)
	}
	if in.mode == modeObserve || (fw.Pass != nil && fw.Pass.Mode == modeObserve) {
		p.Observe = "Observe mode: the firewall applies nothing, by design. The node decides and logs every block, " +
			"but none reaches the firewall, so the decided blocks and the entries are not compared. Set node.mode to " +
			"enforce to apply them."
	} else {
		p.Missing.Count = in.missing.Total
		for i := range in.missing.Items {
			it := &in.missing.Items[i]
			_, label, note := firewallCell(it, fw, in.now)
			p.Missing.Rows = append(p.Missing.Rows, differenceRow{Href: decisionHref(it.Range), Address: rangeText(it.Range),
				Label: label, Note: note})
		}
		if p.Missing.Count > len(p.Missing.Rows) {
			p.Missing.More = notApplied
		}
		p.Same = p.Missing.Count == 0 && p.Stray.Count == 0 && p.Expiry.Count == 0 && p.EntriesErr == ""
	}
	p.Entries, p.Pager = pageEntries(p.Entries, in.page)
	return p
}

// add counts d and lists it among the first ones.
func (d *differences) add(row differenceRow) {
	d.Count++
	if len(d.Rows) < differencesShown {
		d.Rows = append(d.Rows, row)
	}
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

// pageEntries returns page n of rows, and the pager around it.
func pageEntries(rows []entryRow, n int) ([]entryRow, pager) {
	pages := max((len(rows)+entriesPageSize-1)/entriesPageSize, 1)
	n = min(max(n, 1), pages)
	first := (n - 1) * entriesPageSize
	shown := rows[first:min(first+entriesPageSize, len(rows))]
	var pg pager
	switch {
	case len(rows) == 0:
	case pages > 1:
		pg.Text = fmt.Sprintf("Entries %s–%s of %s, page %d of %d", count(first+1), count(first+len(shown)), count(len(rows)), n, pages)
	default:
		pg.Text = plural(len(rows), "entry", "entries")
	}
	href := func(page int) string {
		if page == 1 {
			return "/enforcement#entries"
		}
		return "/enforcement?" + url.Values{"page": {strconv.Itoa(page)}}.Encode() + "#entries"
	}
	if n > 1 {
		pg.Prev = href(n - 1)
	}
	if n < pages {
		pg.Next = href(n + 1)
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
	ctx, cancel := context.WithTimeout(r.Context(), entriesTimeout)
	defer cancel()
	in.entries, in.entriesErr = src.FirewallEntries(ctx)
	in.missing = src.Decisions(DecisionQuery{State: StateBlock, Firewall: FirewallNotApplied, Limit: differencesShown})
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
