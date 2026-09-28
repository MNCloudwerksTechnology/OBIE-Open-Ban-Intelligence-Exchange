package console

import (
	"errors"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// activityNow is when the test entries end.
var activityNow = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

// fakeActivity is an ActivitySource with fixed answers that records what
// it was asked.
type fakeActivity struct {
	page    ActivityPage
	pageErr error
	batch   ActivityBatch

	mu      sync.Mutex
	filters []ActivityFilter
	befores []string
	limits  []int
	afters  []uint64
}

func (f *fakeActivity) Timeline(filter ActivityFilter, before string, limit int) (ActivityPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.filters, f.befores, f.limits = append(f.filters, filter), append(f.befores, before), append(f.limits, limit)
	p := f.page
	p.Entries = p.Entries[:min(limit, len(p.Entries))]
	return p, f.pageErr
}

func (f *fakeActivity) Live(filter ActivityFilter, after uint64, limit int) ActivityBatch {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.filters, f.afters, f.limits = append(f.filters, filter), append(f.afters, after), append(f.limits, limit)
	return f.batch
}

// last returns the last filter, before, limit and after asked for.
func (f *fakeActivity) last() (ActivityFilter, string, int, uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var before string
	var after uint64
	if len(f.befores) > 0 {
		before = f.befores[len(f.befores)-1]
	}
	if len(f.afters) > 0 {
		after = f.afters[len(f.afters)-1]
	}
	return f.filters[len(f.filters)-1], before, f.limits[len(f.limits)-1], after
}

const peerLong = "12D3KooWLongPeerIdentifierXYZ"

// testActivity returns an entry of every kind, newest first, one minute
// apart.
func testActivity() []ActivityEntry {
	at := func(minutes int) time.Time { return activityNow.Add(-time.Duration(minutes) * time.Minute) }
	addr := netip.MustParsePrefix("203.0.113.7/32")
	return []ActivityEntry{
		{Time: at(0), Action: actionModeChanged, Mode: "enforce", PreviousMode: "observe",
			Reason: "node.mode changed from observe to enforce: blocks are applied to the firewall"},
		{Time: at(1), Action: actionConfigReloaded, Mode: "observe", Settings: []string{"node.mode"},
			Reason: "configuration reloaded from /etc/obie/obie.yaml: node.mode changed"},
		{Time: at(2), Action: actionPeerConnected, PeerID: idAlpha, PeerName: "alpha", Reason: "bootstrap peer alpha (" + idAlpha + ") connected"},
		{Time: at(3), Action: actionPeerDisconnected, PeerID: peerLong, Reason: "peer " + peerLong + " disconnected"},
		{Time: at(4), Action: actionRevocation, Range: addr, Rule: "revocation", Reason: "false_positive"},
		{Time: at(5), Action: actionLocalReport, Range: addr, Rule: "local_report", ExpiresAt: activityNow.Add(24 * time.Hour),
			Reason: "ban verdict issued: ssh password_bruteforce, 47 events, confidence 0.8"},
		{Time: at(6), Action: actionOverrideRemoved, Range: netip.MustParsePrefix("198.51.100.0/24"), Rule: ruleForceAllow,
			Reason: "operator override removed"},
		{Time: at(7), Action: actionOverrideSet, Range: netip.MustParsePrefix("192.0.2.99/32"), Rule: ruleForceBlock,
			Note: "scanner", ExpiresAt: activityNow.Add(time.Hour), Reason: "operator force_block override set"},
		{Time: at(8), Action: actionAllowed, Range: netip.MustParsePrefix("198.51.100.20/32"), Rule: ruleAllowlist, Cause: "verdict",
			Reason: "allow-listed: allowlist.cidrs entry 198.51.100.20/32"},
		{Time: at(9), Action: actionBlockRemoved, Range: addr, Rule: "consensus", Cause: "revoke",
			Reason: "below consensus: score 0.9 < threshold 1.8, 1 < quorum 2"},
		{Time: at(10), Action: actionBlockUpdated, Range: netip.MustParsePrefix("2001:db8:4::/48"), Rule: "local_autoblock",
			Cause: "refresh", Mode: "observe", Reason: "local autoblock: this node's own ban verdict"},
		{Time: at(11), Action: actionBlockAdded, Range: addr, Rule: "consensus", Cause: "verdict", Mode: "enforce",
			ExpiresAt: activityNow.Add(24 * time.Hour), Reason: "consensus: score 1.8 >= threshold 1.8, 2 >= quorum 2"},
	}
}

// activityNode makes c show the test entries from the audit log file.
func activityNode(c *Console) *fakeActivity {
	src := &fakeActivity{page: ActivityPage{Entries: testActivity(), Live: 42, Path: "/var/log/obie/audit.jsonl",
		Kept: 10000}}
	c.node.Activity = src
	return src
}

// TestActivityPage: every kind of activity, newest first, with what
// happened, what it is about and links to the decision, verdicts, peer or
// setting (AC1, AC3); the page follows new entries (AC2) from the audit
// log file (AC5).
func TestActivityPage(t *testing.T) {
	c, b := signedInBrowser(t)
	src := activityNode(c)
	resp, page := b.get("/activity")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /activity = %d", resp.StatusCode)
	}
	wantAll(t, "the activity view", page,
		"<title>Activity · OBIE console</title>",
		`<a href="/activity" aria-current="page">Activity</a>`,
		`<h2 id="list-heading">All activity</h2>`,
		`<p class="callout">From the audit log file /var/log/obie/audit.jsonl: the records a SIEM reads from it, also those from before obied last started.`,
		`<form class="filter-form" method="get" action="/activity" role="search" aria-label="Filter activity">`,
		`<option value="" selected>All activity</option>`,
		`<option value="peers">Peers connecting or disconnecting</option>`,
		`<div class="live" data-live-feed="/api/activity?after=42" data-max-rows="500" hidden>`,
		`<button type="button" class="button button-secondary" data-live-toggle aria-pressed="false">Pause live updates</button>`,
		`<th scope="row"><time datetime="2026-09-29T12:00:00Z"><span class="day">2026-09-29</span> 12:00:00 UTC</time></th>`,
		// Mode change and reload link to the settings.
		`<span class="activity-kind" data-kind="mode">Mode changed to Enforce</span>`,
		`<a href="/configuration#section-node">node.mode</a>`,
		`<span class="activity-kind" data-kind="reloads">Configuration reloaded</span>`,
		`<a href="/configuration">Configuration</a>`,
		// Peers link to the peer, by name or short ID.
		`<span class="activity-kind" data-kind="peers">Peer connected</span>`,
		`<a href="/peers/`+idAlpha+`" title="`+idAlpha+`">alpha</a>`,
		`<a class="mono" href="/peers/`+peerLong+`" title="`+peerLong+`">12D3KooW…ierXYZ</a>`,
		// Reports and revocations link to the decision and the verdicts.
		`<span class="activity-kind" data-kind="revocations">Verdict revoked</span>`,
		`<a class="activity-link" href="/verdicts?address=203.0.113.7&amp;from=mine&amp;state=revoked">Its revoked verdicts on 203.0.113.7</a>`,
		`<span class="activity-kind" data-kind="reports">Reported by this node</span>`,
		`<a class="activity-link" href="/verdicts?address=203.0.113.7&amp;from=mine">This node&#39;s verdicts on 203.0.113.7</a>`,
		// Overrides link to the decision and the overrides.
		`<span class="activity-kind" data-kind="overrides">Always allow removed</span>`,
		`<a class="activity-link" href="/overrides?address=198.51.100.0%2F24">Overrides on 198.51.100.0/24</a>`,
		`<span class="activity-kind" data-kind="overrides">Always block set</span>`,
		`<span class="cell-note">Until 2026-09-29 13:00:00 UTC · Note: scanner</span>`,
		// Decisions link to their explanation.
		`<span class="activity-kind" data-kind="allowlist">Spared by the allow-list</span>`,
		`<a class="mono" href="/decisions/198.51.100.20">198.51.100.20</a>`,
		`<span class="activity-kind" data-kind="blocks">Block removed</span>`,
		`<span class="cell-note">No end · Cause: refresh · Observe mode: not applied to the firewall</span>`,
		`<a class="mono" href="/decisions/2001:db8:4::/48">2001:db8:4::/48</a>`,
		`<span class="activity-kind" data-kind="blocks">Block added</span>`,
		`consensus: score 1.8 &gt;= threshold 1.8, 2 &gt;= quorum 2<span class="cell-note">Until 2026-09-30 12:00:00 UTC · Cause: verdict</span>`,
		`<p class="note">This is the first entry of the current audit log file.</p>`,
	)
	if strings.Index(page, "Mode changed to Enforce") > strings.Index(page, "Block added") {
		t.Error("the entries are not newest first")
	}
	if strings.Contains(page, "data-refresh") || strings.Contains(page, `rel="next"`) || strings.Contains(page, "Newest entries") {
		t.Error("the first page refreshes itself, or offers pages that do not exist")
	}
	if f, before, limit, _ := src.last(); len(f.Actions) != 0 || f.Range.IsValid() || before != "" || limit != activityPageSize {
		t.Errorf("asked for %+v before %q limit %d", f, before, limit)
	}
}

// TestActivityPageFilters: the kind and address filters narrow the list
// and the live feed; older pages continue with the same filters and do
// not follow new entries (AC3).
func TestActivityPageFilters(t *testing.T) {
	c, b := signedInBrowser(t)
	src := activityNode(c)
	src.page.Older = "f1-5000"
	_, page := b.get("/activity?kind=overrides&address=198.51.100.0/24")
	wantAll(t, "the filtered view", page,
		`<h2 id="list-heading">Overrides set or removed on or around 198.51.100.0/24</h2>`,
		`<option value="overrides" selected>Overrides set or removed</option>`,
		`value="198.51.100.0/24"`,
		`<a href="/activity">Clear filters</a>`,
		`data-live-feed="/api/activity?address=198.51.100.0%2F24&amp;after=42&amp;kind=overrides"`,
		`<a href="/activity?address=198.51.100.0%2F24&amp;before=f1-5000&amp;kind=overrides" rel="next">Older entries</a>`)
	f, before, _, _ := src.last()
	if !slices.Equal(f.Actions, []string{actionOverrideSet, actionOverrideRemoved}) ||
		f.Range != netip.MustParsePrefix("198.51.100.0/24") || before != "" {
		t.Errorf("asked for %+v before %q", f, before)
	}

	src.page.Older, src.page.Searched, src.page.SearchedTo = "f1-100", true, activityNow.Add(-72*time.Hour)
	_, page = b.get("/activity?kind=peers&before=f1-5000")
	wantAll(t, "an older page", page,
		`<p class="note">This is an older page, which does not follow new entries: <a href="/activity?kind=peers">the newest entries</a>.</p>`,
		`<a href="/activity?kind=peers">Newest entries</a>`,
		`<p class="note">This page searched the audit log back to 2026-09-26 12:00:00 UTC and found no more entries for this view.</p>`,
		`<a href="/activity?before=f1-100&amp;kind=peers" rel="next">Search further back</a>`)
	if strings.Contains(page, "data-live-feed") {
		t.Error("an older page follows new entries")
	}
	if _, before, _, _ := src.last(); before != "f1-5000" {
		t.Errorf("before = %q", before)
	}

	_, page = b.get("/activity?address=nonsense&kind=unknown")
	wantAll(t, "a bad filter", page,
		`<p class="callout" data-level="warning">&#34;nonsense&#34; is not an IP address or network; the list is not narrowed to it</p>`,
		`<option value="" selected>All activity</option>`,
		`data-live-feed="/api/activity?after=42"`)
	if f, _, _, _ := src.last(); f.Range.IsValid() || len(f.Actions) != 0 {
		t.Errorf("a bad filter narrowed the list: %+v", f)
	}

	src.pageErr = errors.New(`"x" is not a position in the audit trail`)
	_, page = b.get("/activity?before=x")
	wantAll(t, "a bad position", page,
		`<p class="callout" data-level="warning">The activity could not be read: &#34;x&#34; is not a position in the audit trail</p>`)
}

// TestActivitySources: the page says where its history comes from and,
// when the audit log is off or cannot be read, which history is missing
// and that live updates still work.
func TestActivitySources(t *testing.T) {
	started := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		page    ActivityPage
		warning bool
		want    []string
	}{
		{"file", ActivityPage{Path: "/a.jsonl", Kept: 10000}, false,
			[]string{"From the audit log file /a.jsonl: the records a SIEM reads from it, also those from before obied last started. Only the current file is read"}},
		{"off", ActivityPage{Memory: true, Kept: 10000}, true, []string{
			"The audit log is off: audit.path is not set. So this timeline cannot show what happened before obied started at 2026-09-28 09:00:00 UTC, keeps at most the last 10,000 entries in memory and loses them at the next restart, and no SIEM receives them. Live updates work. To keep the history, set audit.path and restart obied."}},
		{"unreadable", ActivityPage{Path: "/a.jsonl", Memory: true, FileErr: "obied may write the audit log file but not read it", Kept: 10000}, true,
			[]string{"The audit log file /a.jsonl cannot be read here: obied may write the audit log file but not read it. So this timeline cannot show what happened before obied started at 2026-09-28 09:00:00 UTC; it shows the entries kept in memory since, at most the last 10,000 entries. Live updates work."}},
		{"reopened", ActivityPage{Path: "/a.jsonl", FileErr: "the audit log file was reopened since this page was read", Kept: 10000}, false,
			[]string{"From the audit log file /a.jsonl. The audit log file was reopened since this page was read, so this page starts again at the newest entries."}},
	} {
		p := buildActivity(activityInput{page: tc.page, startedAt: started})
		if p.Warning != tc.warning || (p.SettingHref != "") != tc.warning {
			t.Errorf("%s: warning %v, setting %q", tc.name, p.Warning, p.SettingHref)
		}
		for _, w := range tc.want {
			if !strings.Contains(p.Source, w) {
				t.Errorf("%s: source %q lacks %q", tc.name, p.Source, w)
			}
		}
	}

	c, b := signedInBrowser(t)
	activityNode(c).page = ActivityPage{Memory: true, Kept: 10000, Live: 3}
	_, page := b.get("/activity")
	wantAll(t, "the view without audit log", page,
		`<p class="callout" data-level="warning">The audit log is off`,
		` <a href="/configuration#section-audit">The audit log setting</a></p>`,
		`<p class="empty" data-activity-empty>Nothing has happened since obied started at 2026-09-28 09:00:00 UTC.</p>`,
		`<div class="table-scroll" hidden data-activity-table>`,
		`data-live-feed="/api/activity?after=3"`)
}

// TestActivityEnds: the page says where the list ends, why it is empty
// and which lines it skipped.
func TestActivityEnds(t *testing.T) {
	started := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	one := testActivity()[:1]
	for _, tc := range []struct {
		name  string
		in    activityInput
		end   string
		empty string
	}{
		{"memory start", activityInput{page: ActivityPage{Entries: one, Memory: true}}, "This is the first entry since obied started.", ""},
		{"forgotten", activityInput{page: ActivityPage{Entries: one, Memory: true, Forgotten: true}},
			"Older entries since obied started are no longer kept in memory.", ""},
		{"more pages", activityInput{page: ActivityPage{Entries: one, Older: "f1-10"}}, "", ""},
		{"empty file", activityInput{page: ActivityPage{Path: "/a"}}, "", "The audit log holds no activity yet."},
		{"empty filter", activityInput{query: activityQuery{kind: "mode"}, page: ActivityPage{Path: "/a"}}, "", "No activity matches this view."},
		{"empty search", activityInput{query: activityQuery{kind: "mode"}, page: ActivityPage{Path: "/a", Searched: true, Older: "f1-9"}}, "",
			"No activity matches this view in the part of the audit log searched so far."},
		{"empty older", activityInput{query: activityQuery{before: "f1-9"}, page: ActivityPage{Path: "/a"}}, "", "No older activity."},
	} {
		tc.in.startedAt = started
		p := buildActivity(tc.in)
		if p.End != tc.end || p.Empty != tc.empty {
			t.Errorf("%s: end %q, empty %q; want %q, %q", tc.name, p.End, p.Empty, tc.end, tc.empty)
		}
	}
	p := buildActivity(activityInput{page: ActivityPage{Entries: one, Skipped: 2}})
	if p.Skipped != "2 lines of the audit log file on this page are no OBIE audit record and were skipped." {
		t.Errorf("skipped = %q", p.Skipped)
	}
}

// TestActivityLive: the live feed answers with the rows after a point
// under the page's filters, and summarizes a burst instead of listing it
// (AC2, bursts).
func TestActivityLive(t *testing.T) {
	c, b := signedInBrowser(t)
	src := activityNode(c)
	src.batch = ActivityBatch{Entries: testActivity()[9:11], Next: 1300,
		More: map[string]int{actionBlockAdded: 1200, actionBlockRemoved: 34}, From: activityNow.Add(-2 * time.Second),
		To: activityNow.Add(-time.Second), Lost: 12}
	resp, body := b.get("/api/activity?after=42&kind=blocks&address=203.0.113.0/24")
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("GET /api/activity = %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	wantAll(t, "the live feed", body,
		`<div data-next="1300">`,
		`<span class="activity-kind" data-kind="blocks">Block removed</span>`,
		`<span class="activity-kind" data-kind="blocks">Block updated</span>`,
		`<tr class="burst" data-burst><td colspan="4"><p>Busy: to keep this page responsive, 1,234 more entries from 11:59:58 to 11:59:59 are summarized here instead of listed one by one: 1,200 blocks added, 34 blocks removed. 12 entries were no longer kept in memory when this page asked for them. <a href="/activity?address=203.0.113.0%2F24&amp;kind=blocks">Reload the page to list every entry</a></p></td></tr>`)
	if strings.Contains(body, "<nav") || strings.Contains(body, "<h1") {
		t.Errorf("the live feed renders more than rows:\n%s", body)
	}
	f, _, limit, after := src.last()
	if after != 42 || limit != liveLimit || !slices.Equal(f.Actions, activityKinds[0].actions) ||
		f.Range != netip.MustParsePrefix("203.0.113.0/24") {
		t.Errorf("asked for %+v after %d limit %d", f, after, limit)
	}

	src.batch = ActivityBatch{Next: 1300}
	if _, body := b.get("/api/activity?after=1300"); strings.Contains(body, "<tr") || !strings.Contains(body, `data-next="1300"`) {
		t.Errorf("nothing new:\n%s", body)
	}
	for _, path := range []string{"/api/activity", "/api/activity?after=x", "/api/activity?after=1&address=nonsense"} {
		if resp, _ := b.get(path); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("GET %s = %d, want 400", path, resp.StatusCode)
		}
	}
}

// TestActivityLiveNeedsASession: the live feed answers 401 without a
// session, like every endpoint under /api/.
func TestActivityLiveNeedsASession(t *testing.T) {
	_, b := startConsole(t, &syncBuffer{})
	if resp, _ := b.get("/api/activity?after=0"); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /api/activity signed out = %d, want 401", resp.StatusCode)
	}
}

func TestBurstText(t *testing.T) {
	for _, tc := range []struct {
		b    ActivityBatch
		want string
	}{
		{ActivityBatch{}, ""},
		{ActivityBatch{More: map[string]int{actionPeerConnected: 1}, From: activityNow, To: activityNow},
			"Busy: to keep this page responsive, 1 more entry from 12:00:00 to 12:00:00 is summarized here instead of listed one by one: 1 peers connected."},
		{ActivityBatch{More: map[string]int{"future-action": 2, actionBlockAdded: 2}, From: activityNow, To: activityNow},
			"Busy: to keep this page responsive, 4 more entries from 12:00:00 to 12:00:00 are summarized here instead of listed one by one: 2 blocks added, 2 future-action."},
		{ActivityBatch{Lost: 1}, "1 entry was no longer kept in memory when this page asked for it."},
	} {
		if got := burstText(&tc.b); got != tc.want {
			t.Errorf("burstText(%+v) = %q\nwant %q", tc.b, got, tc.want)
		}
	}
}

// TestOverviewRecentActivity: the overview shows the last entries and
// links to the timeline (AC4).
func TestOverviewRecentActivity(t *testing.T) {
	c, b := signedInBrowser(t)
	overviewNode(c)
	src := activityNode(c)
	_, page := b.get("/")
	wantAll(t, "the overview", page,
		`<h2 id="activity-heading">Recent activity</h2>`,
		`<span class="activity-kind" data-kind="mode">Mode changed to Enforce</span>`,
		`<span class="activity-kind" data-kind="revocations">Verdict revoked</span>`,
		`<p class="section-link"><a href="/activity">All activity, live</a></p>`)
	if strings.Contains(page, "Reported by this node") {
		t.Error("the overview shows more than the last 5 entries")
	}
	if _, _, limit, _ := src.last(); limit != recentEntries {
		t.Errorf("limit = %d", limit)
	}
	if _, fragment := b.get("/api/overview"); !strings.Contains(fragment, "Recent activity") {
		t.Error("the overview's region lacks the recent activity")
	}

	src.page = ActivityPage{Entries: testActivity()[:1], Memory: true}
	_, page = b.get("/")
	wantAll(t, "the overview without audit log", page,
		`<p class="note">The audit log is off: only what happened since obied started is shown.</p>`)
	src.page = ActivityPage{Memory: true}
	_, page = b.get("/")
	wantAll(t, "the overview without activity", page, `<p class="note">Nothing has happened since obied started.</p>`)

	c.node.Activity = nil
	if _, page = b.get("/"); strings.Contains(page, "Recent activity") {
		t.Error("the overview shows activity without a source")
	}
}

// TestActivityWithoutSource: a node that passes no audit trail says so.
func TestActivityWithoutSource(t *testing.T) {
	_, b := signedInBrowser(t)
	_, page := b.get("/activity")
	wantAll(t, "the view without source", page,
		`<p class="callout" data-level="warning">The activity could not be read: the node passes the console no audit trail</p>`)
	if resp, body := b.get("/api/activity?after=7"); resp.StatusCode != http.StatusOK || !strings.Contains(body, `data-next="7"`) {
		t.Errorf("live feed without source = %d %s", resp.StatusCode, body)
	}
}
