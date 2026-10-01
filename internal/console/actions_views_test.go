package console

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestDecisionActions: the decision of an address leads to every action
// that applies to it, and back to it (AC1).
func TestDecisionActions(t *testing.T) {
	c, b := actionConsole(t, newFakeActions(), true)
	explainNode(c)
	rulesNode(c)
	c.now = func() time.Time { return decisionsNow }

	// 203.0.113.7 has an always-block and a verdict of this node.
	_, page := b.get("/decisions/203.0.113.7")
	back := url.QueryEscape("/decisions/203.0.113.7")
	wantAll(t, "the actions on 203.0.113.7", page,
		`<ul class="actions" aria-label="Act on this address">`,
		`<a class="button button-secondary" href="/actions/allow?address=203.0.113.7&amp;return=`+back+`">Always allow…</a>`,
		`<a class="button button-secondary" href="/actions/block?address=203.0.113.7&amp;return=`+back+`">Always block…</a>`,
		`<a class="button button-secondary" href="/actions/unoverride?address=203.0.113.7&amp;return=`+back+`">Remove the override…</a>`,
		`<a class="button button-secondary" href="/actions/report?address=203.0.113.7&amp;return=`+back+`">Report…</a>`,
		`<a class="button button-secondary" href="/actions/revoke?address=203.0.113.7&amp;return=`+back+`">Revoke my verdict…</a>`)

	// 198.51.100.200 has neither: no removal, no revocation.
	_, page = b.get("/decisions/198.51.100.200")
	if !strings.Contains(page, "Always block…") || strings.Contains(page, "Remove the override…") || strings.Contains(page, "Revoke my verdict…") {
		t.Errorf("actions on 198.51.100.200:\n%s", page)
	}
	// The refreshing region carries no actions.
	if _, region := b.get("/api/decisions/203.0.113.7"); strings.Contains(region, "/actions/") {
		t.Errorf("the refreshing region links to actions:\n%s", region)
	}
	// An outcome shown on the page is not passed on by the actions.
	if _, page := b.get("/decisions/203.0.113.7?done=abc"); strings.Contains(page, "done%3Dabc") {
		t.Error("the actions return to the outcome of an earlier action")
	}
}

// TestVerdictAndOverrideActions: the verdicts view offers to report an
// address and to revoke this node's own active verdicts; the overrides
// view to set and remove overrides (AC1).
func TestVerdictAndOverrideActions(t *testing.T) {
	c, b := actionConsole(t, newFakeActions(), true)
	verdictsNode(c)
	_, page := b.get("/verdicts?from=mine")
	back := url.QueryEscape("/verdicts?from=mine")
	wantAll(t, "the verdicts view", page,
		`<a class="button button-secondary" href="/actions/report?return=`+back+`">Report an address…</a>`,
		`<a class="row-action" href="/actions/revoke?address=203.0.113.7&amp;return=`+back+`">Revoke<span class="visually-hidden"> my verdict on 203.0.113.7</span>…</a>`)
	if n := strings.Count(page, `class="row-action"`); n != 1 {
		t.Errorf("%d rows offer to revoke; only this node's active verdict may", n)
	}

	rulesNode(c)
	_, page = b.get("/overrides")
	wantAll(t, "the overrides view", page,
		`Set and remove them here, each after a confirmation, or with`,
		`<a class="button button-secondary" href="/actions/allow?return=%2Foverrides">Always allow an address…</a>`,
		`<a class="button button-secondary" href="/actions/block?return=%2Foverrides">Always block an address…</a>`,
		`<th scope="col">Action</th>`,
		`<a href="/actions/unoverride?address=198.51.100.0%2F24&amp;return=%2Foverrides">Remove<span class="visually-hidden"> the override on 198.51.100.0/24</span>…</a>`)
	if _, page := b.get("/overrides?state=expired"); strings.Contains(page, "/actions/unoverride") || strings.Contains(page, `<th scope="col">Action</th>`) {
		t.Errorf("expired overrides offer to be removed:\n%s", page)
	}
}

// TestReadOnlyViewsOfferNoActions: with the actions switched off, the
// views offer none and say how to act instead (AC5).
func TestReadOnlyViewsOfferNoActions(t *testing.T) {
	c, b := actionConsole(t, newFakeActions(), false)
	explainNode(c)
	rulesNode(c)
	verdictsNode(c)
	for _, path := range []string{"/decisions/203.0.113.7", "/verdicts", "/overrides"} {
		resp, page := b.get(path)
		if resp.StatusCode != http.StatusOK || strings.Contains(page, "/actions/") || !strings.Contains(page, actionsOffNote) {
			t.Errorf("%s = %d, offers actions or does not say why not:\n%s", path, resp.StatusCode, page)
		}
	}
	if _, page := b.get("/overrides"); !strings.Contains(page, "The console only shows them") {
		t.Error("the overrides view does not say it only shows them")
	}
}

// TestActivityShowsWhoActed: an operator action in the timeline names the
// user and the door (AC4).
func TestActivityShowsWhoActed(t *testing.T) {
	for _, tc := range []struct {
		e    ActivityEntry
		want string
	}{
		{ActivityEntry{Action: actionOverrideSet, Origin: "console", UserID: "1000", UserName: "alice"}, "By alice (uid 1000) in the console"},
		{ActivityEntry{Action: actionLocalReport, Origin: "admin-api", UserID: "0"}, "By uid 0 with obiectl or another client of the admin socket"},
		{ActivityEntry{Action: actionRevocation, Origin: "console"}, "By an operator in the console"},
		{ActivityEntry{Action: actionBlockAdded}, ""},
	} {
		row := newActivityRow(&tc.e)
		var got string
		if len(row.Facts) > 0 && strings.HasPrefix(row.Facts[0], "By ") {
			got = row.Facts[0]
		}
		if got != tc.want {
			t.Errorf("%+v: who = %q, want %q", tc.e, got, tc.want)
		}
	}
}
