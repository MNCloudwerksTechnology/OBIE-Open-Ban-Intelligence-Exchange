package console

import (
	"net/http"
	"net/url"
)

// verdictsPageSize is how many verdicts a peer's page lists at once.
const verdictsPageSize = 50

// peerPage is the data of a peer's page.
type peerPage struct {
	// Fragment is the path of the refreshing region.
	Fragment string
	// Title names the page.
	Title  string
	ReadAt timestamp
	// Notice explains that the mesh does not run; empty while it does.
	Notice string
	// Gone is set once the node knows the peer no longer; only the
	// refreshing region of an open page shows it.
	Gone bool
	Peer peerView
	// Verdicts lists the verdicts the node holds from the peer; nil for the
	// refreshing region alone.
	Verdicts *verdictList
}

// verdictList is a page of the verdicts the node holds from a peer. It is
// read when the page opens, not with every refresh (ADR 0021).
type verdictList struct {
	ReadAt timestamp
	Rows   []verdictRow
	// Err says why the verdicts could not be read.
	Err string
	// Next links to the following page and First back to the first one;
	// empty if there is none.
	Next, First string
	// All links to all the peer's verdicts in the verdict view; while that
	// view does not exist, AllCommand names the obiectl command that lists
	// them and ShowCommand the one that shows all verdicts on an address.
	All, AllCommand, ShowCommand string
}

// verdictRow is one verdict of a peer.
type verdictRow struct {
	Address string
	// Href links to the address in the verdict view; empty while that view
	// does not exist.
	Href               string
	Action, Confidence string
	Reason             string
	Issued, Expires    timestamp
	// Counting says whether the verdict counts in decisions; Counts says
	// so, and if not, why.
	Counting bool
	Counts   string
}

// peerContent reads the peer r names and returns the page's title, its
// data and whether the node knows the peer. With region set it reads only
// the data of the refreshing region.
func (c *Console) peerContent(r *http.Request, region bool) (string, any, bool) {
	id := r.PathValue("id")
	set := c.peerSet()
	entry, known := findPeer(set, id, c.node.PeerID)
	p := peerPage{
		Fragment: "/api/peers/" + url.PathEscape(id),
		ReadAt:   stamp(c.now()),
		Notice:   meshNotice(c.node.Status()),
		Gone:     !known,
		Peer:     newPeerView(&entry, set.EventWindow),
	}
	p.Title = "Peer " + p.Peer.ShortID
	if p.Peer.Named {
		p.Title = "Peer " + p.Peer.Title
	}
	if known && !region {
		p.Verdicts = c.peerVerdicts(&entry, r.URL.Query().Get("after"))
	}
	return p.Title, p, known
}

// findPeer returns the peer id with the verdicts the node holds from it,
// and whether the node knows it: configured, connected, or holding active
// verdicts of it. This node is not a peer.
func findPeer(set PeerSet, id, self string) (peerEntry, bool) {
	e := peerEntry{Peer: Peer{ID: id, Weight: set.DefaultWeight}, verdicts: set.Verdicts[id]}
	if id == self {
		return e, false
	}
	for _, p := range set.Peers {
		if p.ID == id {
			e.Peer = p
			return e, true
		}
	}
	return e, e.verdicts.Held > 0
}

// peerVerdicts reads the page of the verdicts held from peer e that
// starts after the indicator key after.
func (c *Console) peerVerdicts(e *peerEntry, after string) *verdictList {
	l := &verdictList{ReadAt: stamp(c.now())}
	l.All, l.AllCommand = c.detailLink("/verdicts?publisher="+url.QueryEscape(e.ID), "obiectl indicators --publisher "+e.ID)
	_, l.ShowCommand = c.detailLink("/verdicts", "obiectl show <address>")
	if c.node.PeerVerdicts == nil {
		return l
	}
	page, err := c.node.PeerVerdicts(e.ID, after, verdictsPageSize)
	if err != nil {
		l.Err = err.Error()
		return l
	}
	for _, v := range page.Verdicts {
		l.Rows = append(l.Rows, c.verdictRow(&v, e.Weight))
	}
	base := "/peers/" + url.PathEscape(e.ID)
	if page.Next != "" {
		l.Next = base + "?after=" + url.QueryEscape(page.Next)
	}
	if after != "" {
		l.First = base
	}
	return l
}

// verdictRow describes verdict v of a publisher with trust weight w. It
// counts in decisions like the decision engine counts it: a ban verdict
// of a publisher whose weight is above 0.
func (c *Console) verdictRow(v *Verdict, w float64) verdictRow {
	row := verdictRow{
		Address:    v.Address,
		Action:     v.Action,
		Confidence: weight(v.Confidence),
		Reason:     v.Reason,
		Issued:     stamp(v.IssuedAt),
		Expires:    stamp(v.ExpiresAt),
	}
	if v.Protocol != "" {
		row.Reason += " (" + v.Protocol + ")"
	}
	row.Href, _ = c.detailLink("/verdicts?address="+url.QueryEscape(v.Address), "")
	switch {
	case v.Action != "ban":
		row.Counts = "No: a " + v.Action + " verdict"
	case !(w > 0):
		row.Counts = "No: weight 0"
	default:
		row.Counting, row.Counts = true, "Yes"
	}
	return row
}
