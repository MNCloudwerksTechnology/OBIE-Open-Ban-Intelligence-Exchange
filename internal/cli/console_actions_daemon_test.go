package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
)

// confirmField matches a field the confirmation posts.
var confirmField = regexp.MustCompile(`<input type="hidden" name="([^"]+)" value="([^"]*)">`)

// confirmation returns the fields the confirmation on page posts.
func confirmation(t *testing.T, page string) url.Values {
	t.Helper()
	_, form, ok := strings.Cut(page, `class="confirm-form"`)
	if !ok {
		t.Fatalf("no confirmation on the page:\n%s", page)
	}
	v := url.Values{}
	for _, m := range confirmField.FindAllStringSubmatch(form, -1) {
		v.Set(m[1], html.UnescapeString(m[2]))
	}
	return v
}

// post posts form from a console page and returns the status and where it
// leads.
func (b *consoleBrowser) post(path string, form url.Values) (code int, location, body string) {
	b.t.Helper()
	req, err := http.NewRequest(http.MethodPost, b.base+path, strings.NewReader(form.Encode()))
	if err != nil {
		b.t.Fatal(err)
	}
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := b.client.Do(req)
	if err != nil {
		b.t.Fatalf("POST %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Get("Location"), string(data)
}

// auditRecord is what the test reads of an audit log line.
type auditRecord struct {
	Event struct{ Action string }
	User  *struct{ ID, Name string }
	Obie  struct {
		Indicator, Origin, Note string
	}
}

// auditRecords returns the records of the audit log at path.
func auditRecords(t *testing.T, path string) []auditRecord {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- the test's own audit log.
	if err != nil {
		t.Fatal(err)
	}
	var out []auditRecord
	for line := range strings.Lines(string(data)) {
		var r auditRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("audit line %q: %v", line, err)
		}
		out = append(out, r)
	}
	return out
}

// TestConsoleActionsEndToEnd runs obied with the console, its actions and
// the audit log on, and no peer: it blocks, allows, removes, reports and
// revokes from the console, each after its confirmation, and checks the
// views, obiectl and the audit log; then two tabs, an ended session and
// the actions switched off by a reload (WP-1689).
func TestConsoleActionsEndToEnd(t *testing.T) {
	addr := freeAddr(t)
	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
	sections := "mesh:\n  listen: [/ip4/127.0.0.1/tcp/0]\nallowlist:\n  cidrs: [185.0.3.0/24]\n" +
		"console:\n  enabled: true\n  listen: " + addr + "\naudit:\n  path: " + auditPath + "\n"
	n := newTestNodeWith(t, "  mode: observe\n", sections)
	var logs syncBuffer
	reload := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exit := startDaemon(ctx, t, n, &logs.buf, func(ctx context.Context, args []string) int {
		return runDaemonWith(ctx, reload, args, &bytes.Buffer{}, &logs)
	})
	ctl := func(args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := RunCtl(append([]string{"--socket", n.socket}, args...), &stdout, &stderr); code != ExitOK {
			t.Fatalf("obiectl %s = %d: %s", strings.Join(args, " "), code, stderr.String())
		}
		return stdout.String()
	}
	browser := newConsoleBrowser(t, addr)
	eventually(t, "the console serves", func() bool { return listening(addr) })
	signIn := func() {
		t.Helper()
		if code, _ := browser.do(http.MethodPost, "/login", url.Values{"token": {consoleOf(t, n).Token}, "next": {"/"}}); code != http.StatusSeeOther {
			t.Fatalf("sign-in = %d", code)
		}
	}
	signIn()
	get := func(path string, want int) string {
		t.Helper()
		code, body := browser.do(http.MethodGet, path, nil)
		if code != want {
			t.Fatalf("GET %s = %d, want %d: %s", path, code, want, body)
		}
		return body
	}
	wantIn := func(what, page string, want ...string) {
		t.Helper()
		for _, w := range want {
			if !strings.Contains(page, w) {
				t.Errorf("%s lacks %q:\n%s", what, w, page)
			}
		}
	}
	// act reviews the action at review, confirms it and returns the page
	// the browser returns to.
	act := func(review string) string {
		t.Helper()
		path, _, _ := strings.Cut(review, "?")
		code, location, body := browser.post(path, confirmation(t, get(review, http.StatusOK)))
		if code != http.StatusSeeOther || !strings.Contains(location, "done=") {
			t.Fatalf("POST %s = %d, Location %q: %s", path, code, location, body)
		}
		return get(location, http.StatusOK)
	}

	// AC1, AC2, AC6: always block from the decision view, with expiry and
	// note, confirmed; the decision and the lists show it at once.
	wantIn("the decision view", get("/decisions/85.10.30.1", http.StatusOK),
		`href="/actions/block?address=85.10.30.1&amp;return=%2Fdecisions%2F85.10.30.1">Always block…</a>`)
	review := "/actions/block?review=1&address=85.10.30.1&ttl=2h&note=scanner&return=%2Fdecisions%2F85.10.30.1"
	wantIn("the block's confirmation", get(review, http.StatusOK),
		"85.10.30.1 will be blocked by this node whatever its score, until ",
		"(for 2 hours).",
		"In observe mode the node only logs blocks: nothing reaches the firewall until node.mode is enforce.",
		"Nothing is published")
	if strings.Contains(ctl("overrides"), "85.10.30.1") {
		t.Fatal("the confirmation set the override")
	}
	wantIn("the decision after the block", act(review),
		`<div class="outcome" data-level="ok" role="status">`,
		"85.10.30.1 is always blocked on this node, until ",
		"Blocked by the operator&#39;s force-block until ")
	wantIn("the blocks", get("/decisions?state=block", http.StatusOK), `href="/decisions/85.10.30.1"`)
	var overrides admin.OverridesResponse
	if err := json.Unmarshal([]byte(ctl("overrides", "--json")), &overrides); err != nil {
		t.Fatal(err)
	}
	if len(overrides.Overrides) != 1 || overrides.Overrides[0].Action != admin.ActionForceBlock ||
		overrides.Overrides[0].Note != "scanner" || overrides.Overrides[0].ExpiresAt == nil {
		t.Errorf("obiectl overrides = %+v", overrides)
	}

	// Remove it again from the overrides view.
	wantIn("the overrides view", get("/overrides", http.StatusOK),
		`<a href="/actions/unoverride?address=85.10.30.1&amp;return=%2Foverrides">Remove`)
	wantIn("the overrides after the removal", act("/actions/unoverride?address=85.10.30.1&return=%2Foverrides"),
		"The override on 85.10.30.1 was removed.", "You have set no override")

	// AC3: a report on an allow-listed address is refused with the reason.
	wantIn("the refused report", get("/actions/report?review=1&address=185.0.3.7&protocol=ssh&reason=password_bruteforce",
		http.StatusUnprocessableEntity),
		"<strong>Not carried out.</strong> refused: ipv4:185.0.3.7 overlaps the allow-listed network 185.0.3.0/24 (allowlist.cidrs)")
	wantIn("an invalid expiry", get("/actions/allow?review=1&address=85.10.30.2&ttl=soon", http.StatusBadRequest),
		"invalid end &#34;soon&#34;")

	// Edge case: no peer is reachable, so the report is stored and held.
	review = "/actions/report?review=1&address=85.10.30.3&protocol=ssh&reason=password_bruteforce&events=5"
	wantIn("the report's confirmation", get(review, http.StatusOK),
		"No peer is reachable now. This node stores it and counts it at once, and sends it as soon as a peer is reachable",
		"This node will block 85.10.30.3 on its own report (decision.local_autoblock).")
	wantIn("the decision after the report", act(review),
		`<div class="outcome" data-level="warning" role="status">`,
		"Your verdict on 85.10.30.3 is stored and counts on this node.",
		"Blocked by this node&#39;s own verdict (local autoblock)")
	var show admin.IndicatorResponse
	if err := json.Unmarshal([]byte(ctl("show", "--json", "85.10.30.3")), &show); err != nil {
		t.Fatal(err)
	}
	if len(show.Verdicts) != 1 || !show.Verdicts[0].Local || show.Verdicts[0].Event.Evidence.Events != 5 {
		t.Fatalf("obiectl show = %+v", show)
	}

	// Revoke it from the verdicts view.
	wantIn("the verdicts view", get("/verdicts?from=mine", http.StatusOK),
		`<a class="row-action" href="/actions/revoke?address=85.10.30.3&amp;return=%2Fverdicts%3Ffrom%3Dmine">Revoke`)
	wantIn("the verdicts after the revocation", act("/actions/revoke?address=85.10.30.3&return=%2Fverdicts%3Ffrom%3Dmine"),
		"Your verdict on 85.10.30.3 is revoked on this node.")
	if err := json.Unmarshal([]byte(ctl("show", "--json", "85.10.30.3")), &show); err != nil || len(show.Verdicts) != 0 {
		t.Errorf("obiectl show after the revocation = %+v, %v", show, err)
	}

	// Edge case: two tabs act on one address; the second sees the first's
	// change and changes nothing.
	first := get("/actions/block?review=1&address=85.10.30.4", http.StatusOK)
	act("/actions/allow?review=1&address=85.10.30.4&note=partner")
	code, _, body := browser.post("/actions/block", confirmation(t, first))
	if code != http.StatusConflict {
		t.Fatalf("stale confirmation = %d:\n%s", code, body)
	}
	wantIn("the stale confirmation", body, "Nothing was changed.", "It replaces the always-allow override on 85.10.30.4")
	if out := ctl("overrides"); !strings.Contains(out, "force_allow") || strings.Contains(out, "force_block") {
		t.Errorf("obiectl overrides after the stale confirmation:\n%s", out)
	}

	// AC4: the audit log records the console's actions as the console's,
	// with the user; obiectl's as the admin API's.
	ctl("block", "85.10.30.5")
	uid := strconv.Itoa(os.Getuid())
	var console, api []string
	for _, r := range auditRecords(t, auditPath) {
		switch r.Obie.Origin {
		case "console":
			console = append(console, r.Event.Action+" "+r.Obie.Indicator)
			if r.User == nil || r.User.ID != uid {
				t.Errorf("console record %+v lacks uid %s", r, uid)
			}
		case "admin-api":
			api = append(api, r.Event.Action+" "+r.Obie.Indicator)
		}
	}
	if got, want := strings.Join(console, ", "), "override-set ipv4:85.10.30.1, override-removed ipv4:85.10.30.1, "+
		"local-report ipv4:85.10.30.3, revocation ipv4:85.10.30.3, override-set ipv4:85.10.30.4"; got != want {
		t.Errorf("console records = %s\nwant %s", got, want)
	}
	if got := strings.Join(api, ", "); got != "override-set ipv4:85.10.30.5" {
		t.Errorf("admin API records = %s", got)
	}
	wantIn("the activity timeline", get("/activity?kind=overrides", http.StatusOK),
		"By "+userName(uid)+" in the console", " with obiectl or another client of the admin socket")

	// Edge case: the session ends before the confirmation is posted; nothing
	// is carried out, and signing in again leads back to the confirmation.
	pending := get("/actions/unoverride?address=85.10.30.5", http.StatusOK)
	ctl("console", "--rotate")
	code, location, _ := browser.post("/actions/unoverride", confirmation(t, pending))
	if code != http.StatusSeeOther || !strings.HasPrefix(location, "/login?reason=action&next=") {
		t.Fatalf("POST after the rotation = %d, Location %q", code, location)
	}
	if !strings.Contains(ctl("overrides"), "85.10.30.5") {
		t.Error("the override was removed without a session")
	}
	wantIn("the sign-in page", get(location, http.StatusOK), "Your session ended before the action was carried out")
	signIn()
	next, _ := url.Parse(location)
	wantIn("the confirmation after signing in", get(next.Query().Get("next"), http.StatusOK), "Remove the override on 85.10.30.5")

	// AC5: console.actions: false, applied by a reload, makes it read-only.
	config, err := os.ReadFile(n.config)
	if err != nil {
		t.Fatal(err)
	}
	readOnly := strings.Replace(string(config), "  enabled: true\n", "  enabled: true\n  actions: false\n", 1)
	if err := os.WriteFile(n.config, []byte(readOnly), 0o600); err != nil { // #nosec G703 -- the test's own configuration file.
		t.Fatal(err)
	}
	reload <- struct{}{}
	eventually(t, "the reload switches the actions off", func() bool {
		code, _ := browser.do(http.MethodGet, "/actions/allow?address=85.10.30.6", nil)
		return code == http.StatusForbidden
	})
	page := get("/decisions/85.10.30.5", http.StatusOK)
	if strings.Contains(page, "/actions/") || !strings.Contains(page, "The console is read-only here") {
		t.Errorf("the read-only decision view:\n%s", page)
	}
	if code, _, _ := browser.post("/actions/unoverride", url.Values{"address": {"85.10.30.5"}}); code != http.StatusForbidden {
		t.Errorf("POST with the actions off = %d", code)
	}

	cancel()
	if code := waitExit(t, exit, &logs.buf); code != ExitOK {
		t.Fatalf("exit code = %d:\n%s", code, logs.String())
	}
}

// userName returns the name of the user uid, or "uid N" as the console
// names a user without one.
func userName(uid string) string {
	if u, err := user.LookupId(uid); err == nil {
		return u.Username + " (uid " + uid + ")"
	}
	return "uid " + uid
}
