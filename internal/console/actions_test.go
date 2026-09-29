package console

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
)

// fakeActions is an ActionSource over an address's override and this
// node's verdict, as the daemon's would keep them.
type fakeActions struct {
	mu       sync.Mutex
	override *Override
	verdict  *OwnVerdict
	peers    int
	mode     string
	// now and after are the decisions Review reports.
	now, after ActionDecision
	// reviewErr and doErr fail Review and Do.
	reviewErr, doErr error
	done             []ActionRequest
	actors           []Actor
	ids              int
}

func newFakeActions() *fakeActions {
	return &fakeActions{peers: 3, mode: "observe",
		now:   ActionDecision{State: StateNone, Reason: "below consensus: score 0.8 < threshold 1.8"},
		after: ActionDecision{State: StateBlock, Rule: "force_block", Reason: "operator force-block override"}}
}

func (f *fakeActions) Review(req ActionRequest) (ActionReview, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reviewErr != nil {
		return ActionReview{}, f.reviewErr
	}
	p, err := parseRange(req.Address)
	if err != nil {
		return ActionReview{}, &ActionError{Status: http.StatusBadRequest, Message: "invalid indicator " + fmt.Sprintf("%q", req.Address)}
	}
	r := ActionReview{Range: p, Override: f.override, Verdict: f.verdict, Now: f.now, After: f.after, Peers: f.peers, Mode: f.mode}
	switch req.Kind {
	case ActionUnoverride:
		if f.override == nil {
			return ActionReview{}, &ActionError{Status: http.StatusNotFound, Message: "no override: ipv4:" + p.Addr().String()}
		}
	case ActionRevoke:
		if f.verdict == nil {
			return ActionReview{}, &ActionError{Status: http.StatusNotFound,
				Message: "not found: this node has no active verdict on ipv4:" + p.Addr().String()}
		}
	case ActionReport:
		pl := &PlannedVerdict{Action: req.Report.Action, Confidence: 0.8, TTL: 7 * 24 * time.Hour}
		if req.Report.Confidence != nil {
			pl.Confidence = *req.Report.Confidence
		}
		pl.Refreshes = f.verdict != nil
		r.Planned = pl
	}
	return r, nil
}

func (f *fakeActions) Do(_ context.Context, req ActionRequest, actor Actor) (ActionOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.doErr != nil {
		return ActionOutcome{}, f.doErr
	}
	p, _ := parseRange(req.Address)
	f.done, f.actors = append(f.done, req), append(f.actors, actor)
	f.ids++
	out := ActionOutcome{Range: p, Decision: f.after, Peers: f.peers, Held: f.peers == 0}
	switch req.Kind {
	case ActionAllow, ActionBlock:
		action := "force_allow"
		if req.Kind == ActionBlock {
			action = "force_block"
		}
		f.override = &Override{Range: p, Action: action, Note: req.Note, CreatedAt: time.Now()}
		if req.TTL > 0 {
			f.override.ExpiresAt = time.Now().Add(req.TTL)
		}
		out.Override, out.Held = f.override, false
	case ActionUnoverride:
		f.override, out.Held = nil, false
	case ActionReport:
		f.verdict = &OwnVerdict{EventID: fmt.Sprintf("0199a1b2-c3d4-7e5f-8a6b-%012d", f.ids), Action: "ban",
			Protocol: req.Report.Protocol, Reason: req.Report.Reason, IssuedAt: time.Now()}
		out.EventIDs = []string{f.verdict.EventID}
	case ActionRevoke:
		f.verdict = nil
	}
	return out, nil
}

func (f *fakeActions) calls() ([]ActionRequest, []Actor) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ActionRequest(nil), f.done...), append([]Actor(nil), f.actors...)
}

func (f *fakeActions) set(fn func(*fakeActions)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

// actionConsole starts a console whose node carries out actions through
// src, with console.actions set to on, and signs a browser in.
func actionConsole(t *testing.T, src ActionSource, on bool) (*Console, *browser) {
	t.Helper()
	c := newConsole(t, config.Console{Enabled: true, Listen: "127.0.0.1:0", Actions: on}, &syncBuffer{})
	c.node.Actions = src
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	b := newBrowser(t, "http://"+c.Addr().String())
	if resp, _ := b.signIn(c.Token(), "/"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("sign-in = %d", resp.StatusCode)
	}
	return c, b
}

var hiddenInput = regexp.MustCompile(`<input type="hidden" name="([^"]+)" value="([^"]*)">`)

// postForm posts form from a console page and returns the status, where
// it leads and the body.
func postForm(b *browser, path string, form url.Values) (int, string, string) {
	b.t.Helper()
	resp, body := b.do(http.MethodPost, path, form, nil)
	return resp.StatusCode, resp.Header.Get("Location"), body
}

// confirmForm returns the fields the confirmation on page posts.
func confirmForm(t *testing.T, page string) url.Values {
	t.Helper()
	_, form, ok := strings.Cut(page, `class="confirm-form"`)
	if !ok {
		t.Fatalf("no confirmation on the page:\n%s", page)
	}
	v := url.Values{}
	for _, m := range hiddenInput.FindAllStringSubmatch(form, -1) {
		v.Set(m[1], html.UnescapeString(m[2]))
	}
	return v
}

// TestActionFormThenConfirmation: an action with details shows its form,
// then what it would do, before anything is carried out (AC1, AC2).
func TestActionFormThenConfirmation(t *testing.T) {
	src := newFakeActions()
	_, b := actionConsole(t, src, true)

	resp, page := b.get("/actions/block?address=203.0.113.7&return=%2Fdecisions%2F203.0.113.7")
	if resp.StatusCode != http.StatusOK || !strings.Contains(page, `name="ttl"`) || !strings.Contains(page, `name="note"`) ||
		strings.Contains(page, "confirm-form") || !strings.Contains(page, "<h1>Always block 203.0.113.7</h1>") {
		t.Fatalf("form = %d:\n%s", resp.StatusCode, page)
	}
	resp, page = b.get("/actions/block?review=1&address=203.0.113.7&ttl=36h&note=scanner+%3Cb%3E&return=%2Fdecisions%2F203.0.113.7")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("confirmation = %d:\n%s", resp.StatusCode, page)
	}
	for _, want := range []string{
		"203.0.113.7 will be blocked by this node whatever its score, until ",
		"(for 1 day 12 hours)",
		"It is not blocked now; it will be blocked on this node only. In observe mode the node only logs blocks",
		"Nothing is published",
		"scanner &lt;b&gt;",
		`<button type="submit" class="button">Always block 203.0.113.7</button>`,
		`href="/actions/block?address=203.0.113.7&amp;edit=1&amp;note=scanner&#43;%3Cb%3E&amp;return=%2Fdecisions%2F203.0.113.7&amp;ttl=36h"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("confirmation lacks %q:\n%s", want, page)
		}
	}
	if done, _ := src.calls(); len(done) != 0 {
		t.Errorf("reviewing carried out %+v", done)
	}
	// B1: an action's page offers no link to share it.
	if strings.Contains(page, "data-share") {
		t.Error("the confirmation offers a link to share it")
	}
	if _, view := b.get("/overrides"); !strings.Contains(view, "data-share") {
		t.Error("a view lost its share link")
	}
	form := confirmForm(t, page)
	if form.Get("address") != "203.0.113.7" || form.Get("ttl") != "36h" || form.Get("note") != "scanner <b>" ||
		form.Get("return") != "/decisions/203.0.113.7" || form.Get("state") == "" {
		t.Errorf("confirmation posts %v", form)
	}

	// Actions without details are reviewed at once.
	src.set(func(f *fakeActions) {
		f.override = &Override{Action: "force_block", Note: "scanner", CreatedAt: time.Now()}
	})
	if resp, page := b.get("/actions/unoverride?address=203.0.113.7"); resp.StatusCode != http.StatusOK ||
		!strings.Contains(page, "The always-block override on") || !strings.Contains(page, "will be removed on this node only") {
		t.Errorf("unoverride = %d:\n%s", resp.StatusCode, page)
	}
}

// TestActionCarriedOut: confirming carries the action out for the
// connection's local user and returns to the page the operator came from,
// which shows what was done (AC4, AC6).
func TestActionCarriedOut(t *testing.T) {
	src := newFakeActions()
	_, b := actionConsole(t, src, true)
	_, page := b.get("/actions/block?review=1&address=203.0.113.7&ttl=36h&note=scanner&return=%2Foverrides")
	resp, _ := b.do(http.MethodPost, "/actions/block", confirmForm(t, page), nil)
	loc := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(loc, "/overrides?done=") {
		t.Fatalf("POST = %d, Location %q", resp.StatusCode, loc)
	}
	done, actors := src.calls()
	if len(done) != 1 || done[0].Kind != ActionBlock || done[0].Address != "203.0.113.7" || done[0].TTL != 36*time.Hour ||
		done[0].Note != "scanner" {
		t.Fatalf("carried out %+v", done)
	}
	if uid := uint32(os.Getuid()); !actors[0].Known || actors[0].UID != uid { // #nosec G115 -- a UID fits in 32 bits.
		t.Errorf("actor = %+v, want uid %d", actors[0], uid)
	}
	resp, page = b.get(loc)
	if resp.StatusCode != http.StatusOK || !strings.Contains(page, `class="outcome" data-level="ok" role="status"`) ||
		!strings.Contains(page, "203.0.113.7 is always blocked on this node, until ") ||
		!strings.Contains(page, "Decision now: block — operator force-block override.") {
		t.Errorf("returned page = %d:\n%s", resp.StatusCode, page)
	}
	// An unknown outcome shows nothing; a link cannot put words here.
	if _, page := b.get("/overrides?done=I+was+hacked"); strings.Contains(page, `class="outcome"`) {
		t.Error("an unknown outcome ID shows a notice")
	}

	// Without a return path, the browser returns to the decision.
	_, page = b.get("/actions/allow?review=1&address=203.0.113.9")
	if resp, _ := b.do(http.MethodPost, "/actions/allow", confirmForm(t, page), nil); !strings.HasPrefix(resp.Header.Get("Location"), "/decisions/203.0.113.9?done=") {
		t.Errorf("POST without return: Location %q", resp.Header.Get("Location"))
	}
}

// TestActionSecondTabSeesTheCurrentState: a confirmation shown before
// another tab acted on the same address is not carried out over it; it is
// shown again with the current state (edge case: two tabs).
func TestActionSecondTabSeesTheCurrentState(t *testing.T) {
	src := newFakeActions()
	_, b := actionConsole(t, src, true)
	_, first := b.get("/actions/block?review=1&address=203.0.113.7")
	_, second := b.get("/actions/allow?review=1&address=203.0.113.7&note=partner")

	if resp, _ := b.do(http.MethodPost, "/actions/allow", confirmForm(t, second), nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("second tab's POST = %d", resp.StatusCode)
	}
	resp, page := b.do(http.MethodPost, "/actions/block", confirmForm(t, first), nil)
	if resp.StatusCode != http.StatusConflict || !strings.Contains(page, "Nothing was changed.") ||
		!strings.Contains(page, "It replaces the always-allow override on 203.0.113.7") ||
		!strings.Contains(page, "with the note “partner”") {
		t.Fatalf("stale POST = %d:\n%s", resp.StatusCode, page)
	}
	if done, _ := src.calls(); len(done) != 1 || done[0].Kind != ActionAllow {
		t.Fatalf("carried out %+v, want only the second tab's allow", done)
	}
	// Confirmed again with the current state, it is carried out.
	if resp, _ := b.do(http.MethodPost, "/actions/block", confirmForm(t, page), nil); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("confirmed again = %d", resp.StatusCode)
	}
	if done, _ := src.calls(); len(done) != 2 || done[1].Kind != ActionBlock {
		t.Errorf("carried out %+v", done)
	}
}

// TestActionAfterTheSessionEnded: an action posted after the session ended
// is not carried out; signing in again leads back to its confirmation
// (edge case: expired credential).
func TestActionAfterTheSessionEnded(t *testing.T) {
	src := newFakeActions()
	c, b := actionConsole(t, src, true)
	_, page := b.get("/actions/report?review=1&address=198.51.100.7&protocol=ssh&reason=password_bruteforce&events=12")
	form := confirmForm(t, page)
	c.RotateToken() // as obiectl console --rotate does; an expiry is alike

	resp, _ := b.do(http.MethodPost, "/actions/report", form, nil)
	loc := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(loc, "/login?reason=action&next=") {
		t.Fatalf("POST without session = %d, Location %q", resp.StatusCode, loc)
	}
	if done, _ := src.calls(); len(done) != 0 {
		t.Fatalf("carried out without a session: %+v", done)
	}
	u, _ := url.Parse(loc)
	next := u.Query().Get("next")
	// The action waits on the node: the URL names it, and carries none of
	// its details.
	if !strings.HasPrefix(next, "/actions/report?pending=") || strings.Contains(next, "events") || strings.Contains(next, "198.51") {
		t.Errorf("next = %q", next)
	}
	if _, page := b.get(loc); !strings.Contains(page, "Your session ended before the action was carried out, so nothing was changed.") {
		t.Errorf("sign-in page lacks the explanation:\n%s", page)
	}
	resp, _ = b.signIn(c.Token(), next)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != next {
		t.Fatalf("sign-in = %d, Location %q; want %q", resp.StatusCode, resp.Header.Get("Location"), next)
	}
	if resp, page := b.get(next); resp.StatusCode != http.StatusOK || !strings.Contains(page, "confirm-form") ||
		confirmForm(t, page).Get("events") != "12" || confirmForm(t, page).Get("address") != "198.51.100.7" {
		t.Errorf("back at the confirmation = %d:\n%s", resp.StatusCode, page)
	}
	// A pending action no longer kept asks to enter it again.
	if resp, page := b.get("/actions/report?pending=gone"); resp.StatusCode != http.StatusOK ||
		!strings.Contains(page, "The action that waited for you to sign in is no longer kept") || strings.Contains(page, "confirm-form") {
		t.Errorf("a lost pending action = %d:\n%s", resp.StatusCode, page)
	}
	// A long note in another script survives signing in (it waits on the
	// node, not in the sign-in form).
	note := strings.Repeat("ü", 500)
	_, page = b.get("/actions/block?review=1&address=198.51.100.9&note=" + url.QueryEscape(note))
	c.RotateToken()
	_, loc2, _ := postForm(b, "/actions/block", confirmForm(t, page))
	u2, _ := url.Parse(loc2)
	if resp, _ := b.signIn(c.Token(), u2.Query().Get("next")); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("sign-in with a long note pending = %d", resp.StatusCode)
	}
	if _, page := b.get(u2.Query().Get("next")); confirmForm(t, page).Get("note") != note {
		t.Error("the pending note did not survive signing in")
	}
	// And an expired session is refused alike.
	now := time.Now()
	c.now = func() time.Time { return now.Add(sessionLifetime + time.Minute) }
	if resp, _ := b.do(http.MethodPost, "/actions/report", form, nil); !strings.HasPrefix(resp.Header.Get("Location"), "/login?reason=action") {
		t.Errorf("POST after the session expired: Location %q", resp.Header.Get("Location"))
	}
	if done, _ := src.calls(); len(done) != 0 {
		t.Errorf("carried out without a session: %+v", done)
	}
}

// TestActionsSwitchedOff: with console.actions off, or no action source,
// the console is read-only: action pages and posts are refused and the
// footer says so (AC5).
func TestActionsSwitchedOff(t *testing.T) {
	for name, tc := range map[string]struct {
		src  ActionSource
		on   bool
		want string
	}{
		"console.actions false": {newFakeActions(), false, "console.actions: false"},
		"no action source":      {nil, true, "The node passes the console no actions."},
	} {
		t.Run(name, func(t *testing.T) {
			_, b := actionConsole(t, tc.src, tc.on)
			resp, page := b.get("/actions/allow?address=203.0.113.7&review=1")
			if resp.StatusCode != http.StatusForbidden || !strings.Contains(page, tc.want) || strings.Contains(page, "confirm-form") {
				t.Errorf("GET = %d:\n%s", resp.StatusCode, page)
			}
			resp, _ = b.do(http.MethodPost, "/actions/allow", url.Values{"address": {"203.0.113.7"}, "state": {"x"}}, nil)
			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("POST = %d", resp.StatusCode)
			}
			if _, page := b.get("/"); !strings.Contains(page, "Read-only view of this node") {
				t.Error("the footer does not say the console is read-only")
			}
			if f, ok := tc.src.(*fakeActions); ok {
				if done, _ := f.calls(); len(done) != 0 {
					t.Errorf("carried out %+v", done)
				}
			}
		})
	}
	_, b := actionConsole(t, newFakeActions(), true)
	if _, page := b.get("/"); !strings.Contains(page, "it acts only after you confirm") {
		t.Error("the footer does not say actions are confirmed")
	}
}

// TestActionRefusals: what the node refuses and invalid input are
// explained, with the admin API's status, and nothing is carried out
// (AC3).
func TestActionRefusals(t *testing.T) {
	src := newFakeActions()
	_, b := actionConsole(t, src, true)
	refused := "refused: 85.20.1.1 overlaps the allow-listed network 85.20.0.0/16 (allowlist.cidrs); allow-listed addresses are never reported"
	src.set(func(f *fakeActions) {
		f.reviewErr = &ActionError{Status: http.StatusUnprocessableEntity, Message: refused}
	})
	resp, page := b.get("/actions/report?review=1&address=85.20.1.1&protocol=ssh&reason=password_bruteforce")
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(page, "<strong>Not carried out.</strong> "+refused) ||
		strings.Contains(page, "confirm-form") || !strings.Contains(page, `value="password_bruteforce"`) {
		t.Errorf("refused report = %d:\n%s", resp.StatusCode, page)
	}
	resp, _ = b.do(http.MethodPost, "/actions/report", url.Values{"address": {"85.20.1.1"}, "protocol": {"ssh"},
		"reason": {"password_bruteforce"}, "events": {"1"}, "state": {"x"}}, nil)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("refused report posted = %d", resp.StatusCode)
	}
	src.set(func(f *fakeActions) { f.reviewErr = nil })

	for path, want := range map[string]string{
		"/actions/block?review=1&address=203.0.113.7&ttl=soon":                        `invalid end &#34;soon&#34;: want a positive duration`,
		"/actions/report?review=1&address=203.0.113.7&protocol=ssh&events=many":       `events: &#34;many&#34; is not a whole number`,
		"/actions/report?review=1&address=203.0.113.7&confidence=high&events=1":       `confidence: &#34;high&#34; is not a number from 0 to 1`,
		"/actions/allow?review=1&address=nonsense":                                    `invalid indicator &#34;nonsense&#34;`,
		"/actions/unoverride?address=203.0.113.7":                                     `no override: ipv4:203.0.113.7`,
		"/actions/revoke?address=203.0.113.7":                                         `this node has no active verdict on ipv4:203.0.113.7`,
		"/actions/report?review=1&address=203.0.113.7&protocol=ssh&ttl=forever":       `lifetime: `,
		"/actions/block?review=1&address=203.0.113.7&ttl=-2h":                         `invalid end &#34;-2h&#34;`,
		"/actions/allow?review=1&address=203.0.113.7&note=" + strings.Repeat("x", 10): "confirm-form",
	} {
		resp, page := b.get(path)
		if !strings.Contains(page, want) {
			t.Errorf("GET %s = %d, lacks %q:\n%s", path, resp.StatusCode, want, page)
		}
	}
	src.set(func(f *fakeActions) { f.doErr = errors.New("store is closed") })
	_, page = b.get("/actions/allow?review=1&address=203.0.113.7")
	resp, page = b.do(http.MethodPost, "/actions/allow", confirmForm(t, page), nil)
	if resp.StatusCode != http.StatusInternalServerError || !strings.Contains(page, "the obied log says why") ||
		strings.Contains(page, "store is closed") {
		t.Errorf("failed action = %d:\n%s", resp.StatusCode, page)
	}
	if done, _ := src.calls(); len(done) != 0 {
		t.Errorf("carried out %+v", done)
	}
}

// TestActionPagesRefuseOtherSites: a link on another site or another port
// of this host cannot open a prefilled confirmation, and a post from
// there is refused before anything is read (ADR 0026).
func TestActionPagesRefuseOtherSites(t *testing.T) {
	src := newFakeActions()
	_, b := actionConsole(t, src, true)
	for _, site := range []string{"cross-site", "same-site"} {
		resp, page := b.do(http.MethodGet, "/actions/allow?review=1&address=203.0.113.7", nil,
			map[string]string{"Sec-Fetch-Site": site, "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document"})
		if resp.StatusCode != http.StatusForbidden || strings.Contains(page, "confirm-form") {
			t.Errorf("%s navigation = %d:\n%s", site, resp.StatusCode, page)
		}
		resp, _ = b.do(http.MethodPost, "/actions/allow", url.Values{"address": {"203.0.113.7"}}, map[string]string{"Sec-Fetch-Site": site})
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s POST = %d", site, resp.StatusCode)
		}
	}
	// Without a session (a cross-site navigation carries no SameSite=Strict
	// cookie), it is refused before the sign-in, which would lead on to it.
	stranger := newBrowser(t, b.base)
	resp, _ := stranger.do(http.MethodGet, "/actions/block?review=1&address=203.0.113.7&note=trust+me", nil,
		map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document"})
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-site navigation without session = %d, Location %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	// The sign-in page reached from another site forgets an action page as
	// its next and says no action waits.
	_, page := stranger.do(http.MethodGet, "/login?reason=action&next="+url.QueryEscape("/actions/block?review=1&address=203.0.113.7"), nil,
		map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document"})
	if !strings.Contains(page, `name="next" value="/"`) || strings.Contains(page, "Your session ended") {
		t.Errorf("sign-in page reached cross-site:\n%s", page)
	}
	// Nor through the sign-in page again, nor through dot segments; a link
	// to a view still returns there.
	for next, want := range map[string]string{
		"/login?next=" + url.QueryEscape("/actions/block?review=1&address=203.0.113.7"): "/",
		"/./actions/block?review=1&address=203.0.113.7":                                 "/",
		"/%2e/actions/block?review=1":                                                   "/",
		"/decisions/../actions/allow?review=1":                                          "/",
		"/decisions/203.0.113.7":                                                        "/decisions/203.0.113.7",
	} {
		_, page := stranger.do(http.MethodGet, "/login?next="+url.QueryEscape(next), nil,
			map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document"})
		if !strings.Contains(page, `name="next" value="`+want+`"`) {
			t.Errorf("sign-in page reached cross-site with next %q keeps another next than %q:\n%s", next, want, page)
		}
	}
	_, page = stranger.do(http.MethodGet, "/login?reason=action&next="+url.QueryEscape("/actions/block?pending=x"), nil,
		map[string]string{"Sec-Fetch-Site": "same-origin"})
	if !strings.Contains(page, `name="next" value="/actions/block?pending=x"`) || !strings.Contains(page, "Your session ended") {
		t.Errorf("sign-in page reached from the console:\n%s", page)
	}
	// Typed into the address bar, it opens.
	if resp, _ := b.do(http.MethodGet, "/actions/allow?address=203.0.113.7", nil, map[string]string{"Sec-Fetch-Site": "none"}); resp.StatusCode != http.StatusOK {
		t.Errorf("address bar = %d", resp.StatusCode)
	}
	if resp, _ := b.get("/actions/nosuchaction"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown action = %d", resp.StatusCode)
	}
	if done, _ := src.calls(); len(done) != 0 {
		t.Errorf("carried out %+v", done)
	}
}

// TestReportAndRevokeConsequences: a report or revocation says to how many
// peers it goes, or that it waits for one, and the outcome says whether it
// was sent (edge case: mesh unreachable).
func TestReportAndRevokeConsequences(t *testing.T) {
	src := newFakeActions()
	src.after = ActionDecision{State: StateBlock, Autoblock: true, Reason: "local autoblock"}
	_, b := actionConsole(t, src, true)
	report := "/actions/report?review=1&address=198.51.100.7&protocol=ssh&reason=password_bruteforce&events=12"
	if _, page := b.get(report); !strings.Contains(page, "This will publish a signed ban verdict on 198.51.100.7 (ssh password_bruteforce, 12 events, confidence 0.8, for 7 days) to the 3 peers connected now") ||
		!strings.Contains(page, "This node will block 198.51.100.7 on its own report (decision.local_autoblock).") {
		t.Errorf("report with peers:\n%s", page)
	}

	src.set(func(f *fakeActions) { f.peers = 0 })
	_, page := b.get(report)
	if !strings.Contains(page, "No peer is reachable now. This node stores it and counts it at once, and sends it as soon as a peer is reachable") {
		t.Errorf("report without peers:\n%s", page)
	}
	resp, _ := b.do(http.MethodPost, "/actions/report", confirmForm(t, page), nil)
	_, page = b.get(resp.Header.Get("Location"))
	if !strings.Contains(page, `data-level="warning"`) || !strings.Contains(page, "Your verdict on 198.51.100.7 is stored and counts on this node.") ||
		!strings.Contains(page, "it will be sent as soon as one is") {
		t.Errorf("held report outcome:\n%s", page)
	}

	src.set(func(f *fakeActions) {
		f.peers, f.now, f.after = 2, f.after, ActionDecision{State: StateNone, Reason: "no active verdicts"}
	})
	_, page = b.get("/actions/revoke?address=198.51.100.7")
	for _, want := range []string{"This will publish a signed revocation of your ban verdict on 198.51.100.7 (ssh password_bruteforce, issued ",
		"to the 2 peers connected now: they stop counting it.", "198.51.100.7 will be unblocked on this node: no active verdicts.",
		`name="reason" value="false_positive"`} {
		if !strings.Contains(page, want) {
			t.Errorf("revocation lacks %q:\n%s", want, page)
		}
	}
	resp, _ = b.do(http.MethodPost, "/actions/revoke", confirmForm(t, page), nil)
	if _, page = b.get(resp.Header.Get("Location")); !strings.Contains(page, "Your verdict on 198.51.100.7 was revoked; the revocation was published to 2 peers.") {
		t.Errorf("revocation outcome:\n%s", page)
	}
	if done, _ := src.calls(); len(done) != 2 || done[1].Reason != "false_positive" || done[0].Report.Events != 12 {
		t.Errorf("carried out %+v", done)
	}
}

// TestBlockWithoutEffectWarns: an always-block that a protected address
// beats says so before it is set, as obiectl block warns after.
func TestBlockWithoutEffectWarns(t *testing.T) {
	w := consequenceWriter{review: &ActionReview{Range: netip.MustParsePrefix("10.0.0.7/32"), Mode: "enforce",
		Now:   ActionDecision{State: StateAllowed, Reason: "allow-listed: built-in private range 10.0.0.0/8 (protected)"},
		After: ActionDecision{State: StateAllowed, Reason: "allow-listed: built-in private range 10.0.0.0/8 (protected)"}},
		req: ActionRequest{Kind: ActionBlock}, now: time.Now(), addr: "10.0.0.7"}
	got := w.block()
	if len(got) < 2 || got[1].Level != "warning" ||
		got[1].Text != "The always-block will not take effect: allow-listed: built-in private range 10.0.0.0/8 (protected). It is set all the same, as obiectl block does, and takes effect once that no longer applies." {
		t.Errorf("block consequences = %+v", got)
	}
	// Unblocking in enforce mode names the firewall.
	w.review.Now, w.review.After = ActionDecision{State: StateBlock}, ActionDecision{State: StateAllowed, Reason: "operator force-allow"}
	if got := w.allow(); !strings.Contains(got[1].Text, "it will be unblocked on this node only. Its firewall entry is removed within seconds.") {
		t.Errorf("allow consequences = %+v", got)
	}
}

func TestSpanText(t *testing.T) {
	for d, want := range map[time.Duration]string{
		7 * 24 * time.Hour: "7 days", 36 * time.Hour: "1 day 12 hours", 90 * time.Minute: "1 hour 30 minutes",
		time.Hour: "1 hour", 30 * time.Second: "1 minute", 0: "0 minutes",
	} {
		if got := spanText(d); got != want {
			t.Errorf("spanText(%s) = %q, want %q", d, got, want)
		}
	}
}

// TestCoalescedReportWithoutVerdict: the words of a coalesced report do
// not need the verdict it is coalesced into.
func TestCoalescedReportWithoutVerdict(t *testing.T) {
	w := consequenceWriter{review: &ActionReview{Range: netip.MustParsePrefix("198.51.100.7/32"),
		Planned: &PlannedVerdict{Coalesced: true}}, req: ActionRequest{Kind: ActionReport, Report: ReportDetails{Events: 2}},
		now: time.Now(), addr: "198.51.100.7"}
	if got := w.report(); len(got) == 0 || !strings.Contains(got[0].Text, "2 events are added to the next refresh of your verdict.") {
		t.Errorf("coalesced report = %+v", got)
	}
}

// TestStateToken: the fingerprint changes with the override and this
// node's verdict, and only with them.
func TestStateToken(t *testing.T) {
	base := ActionReview{Range: netip.MustParsePrefix("203.0.113.7/32"), Peers: 3}
	t0 := stateToken(&base)
	moved := base
	moved.Peers, moved.Now = 0, ActionDecision{State: StateBlock}
	if stateToken(&moved) != t0 {
		t.Error("the token changed with the peers or the decision")
	}
	for name, mutate := range map[string]func(*ActionReview){
		"override":      func(r *ActionReview) { r.Override = &Override{Action: "force_allow"} },
		"verdict":       func(r *ActionReview) { r.Verdict = &OwnVerdict{EventID: "e1"} },
		"another range": func(r *ActionReview) { r.Range = netip.MustParsePrefix("203.0.113.8/32") },
	} {
		r := base
		mutate(&r)
		if stateToken(&r) == t0 {
			t.Errorf("%s: the token did not change", name)
		}
	}
	// A report that would refresh the verdict is not one that would only
	// be added to its next refresh (after the coalescing window passed).
	coalesced, refreshed := base, base
	coalesced.Planned = &PlannedVerdict{Coalesced: true}
	refreshed.Planned = &PlannedVerdict{Refreshes: true}
	if stateToken(&coalesced) == stateToken(&refreshed) {
		t.Error("the token ignores whether a report is coalesced")
	}
	a, b := base, base
	a.Override = &Override{Action: "force_allow", Note: "a"}
	b.Override = &Override{Action: "force_allow", Note: "b"}
	if stateToken(&a) == stateToken(&b) {
		t.Error("the token ignores the override's note")
	}
}

func TestCrossSiteNext(t *testing.T) {
	for next, want := range map[string]string{
		"/":                           "/",
		"/decisions?state=block":      "/decisions?state=block",
		"/actions/block?review=1":     "/",
		"/actions":                    "/",
		"/login?next=%2Factions%2Fx":  "/",
		"/./actions/x":                "/",
		"/%2E/actions/x":              "/",
		"/verdicts/../actions/revoke": "/",
		"/actionsX":                   "/actionsX",
		"%zz":                         "/",
	} {
		if got := crossSiteNext(next); got != want {
			t.Errorf("crossSiteNext(%q) = %q, want %q", next, got, want)
		}
	}
}

// TestMemoIsBounded: the node keeps the last outcomes and pending actions
// for a while.
func TestMemoIsBounded(t *testing.T) {
	o, now := newMemo[actionNotice](maxOutcomes, outcomeLifetime), time.Now()
	first := o.put(actionNotice{Title: "first"}, now)
	var last string
	for i := range maxOutcomes {
		last = o.put(actionNotice{Title: fmt.Sprint(i)}, now)
	}
	if _, ok := o.get(first, now); ok {
		t.Error("the oldest value is kept over the limit")
	}
	if n, ok := o.get(last, now); !ok || n.Title != fmt.Sprint(maxOutcomes-1) {
		t.Errorf("last value = %+v, %v", n, ok)
	}
	if _, ok := o.get(last, now.Add(outcomeLifetime+time.Second)); ok {
		t.Error("a value is kept past its lifetime")
	}
	if _, ok := o.get("", now); ok {
		t.Error("the empty ID names a value")
	}
	if len(o.byID) != maxOutcomes || len(o.order) != maxOutcomes {
		t.Errorf("kept %d/%d values, want %d", len(o.byID), len(o.order), maxOutcomes)
	}
}
