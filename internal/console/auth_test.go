package console

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
)

// browser is an HTTP client with a cookie jar that does not follow
// redirects and sends what a browser sends for the console's own pages.
type browser struct {
	t      *testing.T
	base   string
	client *http.Client
}

// startConsole starts an enabled console on a free loopback port.
func startConsole(t *testing.T, logs *syncBuffer) (*Console, *browser) {
	t.Helper()
	c := newConsole(t, config.Console{Enabled: true, Listen: "127.0.0.1:0"}, logs)
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.Addr() == nil {
		t.Fatalf("console not serving: %s", c.Detail())
	}
	return c, newBrowser(t, "http://"+c.Addr().String())
}

func newBrowser(t *testing.T, base string) *browser {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	return &browser{t: t, base: base, client: &http.Client{Jar: jar, Transport: transport, Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// do sends a same-origin request and returns the response with its body.
func (b *browser) do(method, path string, form url.Values, headers map[string]string) (*http.Response, string) {
	b.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, b.base+path, body)
	if err != nil {
		b.t.Fatal(err)
	}
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", b.base)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := b.client.Do(req)
	if err != nil {
		b.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		b.t.Fatal(err)
	}
	return resp, string(data)
}

func (b *browser) get(path string) (*http.Response, string) {
	return b.do(http.MethodGet, path, nil, nil)
}

func (b *browser) signIn(token, next string) (*http.Response, string) {
	return b.do(http.MethodPost, "/login", url.Values{"token": {token}, "next": {next}}, nil)
}

// sessionCookie returns the browser's session cookie for the console.
func (b *browser) sessionCookie() *http.Cookie {
	u, _ := url.Parse(b.base)
	for _, ck := range b.client.Jar.Cookies(u) {
		if ck.Name == cookieName(u.Host) {
			return ck
		}
	}
	return nil
}

func TestSignInFlow(t *testing.T) {
	logs := &syncBuffer{}
	c, b := startConsole(t, logs)

	resp, _ := b.get("/")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login?next=%2F" {
		t.Fatalf("GET / signed out = %d %s, want a redirect to sign-in", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp, body := b.get("/api/health")
	if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(body, "not signed in") {
		t.Fatalf("GET /api/health signed out = %d %s, want 401", resp.StatusCode, body)
	}
	resp, body = b.get("/login?next=%2Fsome%2Fview%3Fq%3D1")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `name="next" value="/some/view?q=1"`) ||
		!strings.Contains(body, `type="password"`) {
		t.Fatalf("sign-in page = %d:\n%s", resp.StatusCode, body)
	}
	if strings.Contains(body, c.Token()) || strings.Contains(body, testNode.PeerID) {
		t.Error("the sign-in page shows the token or the node")
	}

	resp, _ = b.signIn(" "+c.Token()+"\n", "/some/view?q=1")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/some/view?q=1" {
		t.Fatalf("sign-in = %d %s, want a redirect to the page asked for", resp.StatusCode, resp.Header.Get("Location"))
	}
	setCookie := resp.Header.Get("Set-Cookie")
	for _, want := range []string{cookieName(c.Addr().String()) + "=", "Path=/", "HttpOnly", "SameSite=Strict"} {
		if !strings.Contains(setCookie, want) {
			t.Errorf("Set-Cookie %q lacks %q", setCookie, want)
		}
	}
	if strings.Contains(setCookie, c.Token()) || strings.Contains(setCookie, "Max-Age") || strings.Contains(setCookie, "Expires") {
		t.Errorf("Set-Cookie %q holds the token or outlives the browser session", setCookie)
	}
	if !strings.Contains(logs.String(), `"msg":"console sign-in"`) {
		t.Errorf("sign-in not logged:\n%s", logs)
	}

	resp, body = b.get("/api/health")
	var health healthResponse
	if resp.StatusCode != http.StatusOK || json.Unmarshal([]byte(body), &health) != nil ||
		health.State != HealthReady || health.Label != "Ready" || health.Mode != "observe" {
		t.Fatalf("GET /api/health = %d %s", resp.StatusCode, body)
	}
	if resp, _ := b.get("/login"); resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/" {
		t.Errorf("sign-in page while signed in = %d %s, want a redirect home", resp.StatusCode, resp.Header.Get("Location"))
	}

	resp, _ = b.do(http.MethodPost, "/logout", url.Values{}, nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login" || b.sessionCookie() != nil {
		t.Fatalf("sign-out = %d %s, cookie %v", resp.StatusCode, resp.Header.Get("Location"), b.sessionCookie())
	}
	if resp, _ := b.get("/api/health"); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /api/health after sign-out = %d, want 401", resp.StatusCode)
	}
}

func TestSignInRefusals(t *testing.T) {
	logs := &syncBuffer{}
	c, b := startConsole(t, logs)

	resp, body := b.signIn("not-the-token", "/")
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(body, "not the console&#39;s current token") ||
		!strings.Contains(body, "sudo obiectl console") || b.sessionCookie() != nil {
		t.Errorf("wrong token = %d, cookie %v:\n%s", resp.StatusCode, b.sessionCookie(), body)
	}
	if !strings.Contains(logs.String(), "console sign-in with a wrong token") || strings.Contains(logs.String(), "not-the-token") {
		t.Errorf("wrong token not logged, or logged with the attempt:\n%s", logs)
	}

	// A page on another site or another port of this host posts the right
	// token: refused before it is looked at.
	for _, h := range []map[string]string{
		{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"},
		{"Sec-Fetch-Site": "same-site", "Origin": "http://127.0.0.1:8080"},
		{"Sec-Fetch-Site": "", "Origin": "http://evil.example"},
	} {
		resp, _ := b.do(http.MethodPost, "/login", url.Values{"token": {c.Token()}}, h)
		if resp.StatusCode != http.StatusForbidden || b.sessionCookie() != nil {
			t.Errorf("forged sign-in %v = %d, cookie %v; want 403 and no session", h, resp.StatusCode, b.sessionCookie())
		}
	}

	// Guessing is rate-limited, and so is the right token while limited.
	limited := 0
	for range 10 {
		if resp, _ := b.signIn("guess", "/"); resp.StatusCode == http.StatusTooManyRequests {
			limited++
		}
	}
	if limited == 0 {
		t.Error("ten wrong tokens in a row were never rate-limited")
	}
	if resp, body := b.signIn(c.Token(), "/"); resp.StatusCode != http.StatusTooManyRequests || !strings.Contains(body, "Too many sign-in attempts") {
		t.Errorf("sign-in while limited = %d", resp.StatusCode)
	}

	resp, _ = b.do(http.MethodPost, "/login", url.Values{"token": {strings.Repeat("x", maxSignInBody)}}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("oversized sign-in = %d, want 400", resp.StatusCode)
	}
}

func TestRotateEndsSessions(t *testing.T) {
	logs := &syncBuffer{}
	c, b := startConsole(t, logs)
	old := c.Token()
	if resp, _ := b.signIn(old, "/"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("sign-in = %d", resp.StatusCode)
	}
	fresh := c.RotateToken()
	if fresh == old || fresh != c.Token() || len(fresh) != 43 {
		t.Fatalf("rotated token %q (old %q), want a new 256-bit token", fresh, old)
	}
	if !strings.Contains(logs.String(), "console token rotated; every console session was signed out") ||
		strings.Contains(logs.String(), fresh) || strings.Contains(logs.String(), old) {
		t.Errorf("rotation not logged, or a token logged:\n%s", logs)
	}
	if resp, _ := b.get("/api/health"); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("old session after rotation = %d, want 401", resp.StatusCode)
	}
	if resp, _ := b.signIn(old, "/"); resp.StatusCode != http.StatusForbidden {
		t.Errorf("old token after rotation = %d, want 403", resp.StatusCode)
	}
	if resp, _ := b.signIn(fresh, "/"); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("new token = %d, want a session", resp.StatusCode)
	}
	if resp, _ := b.get("/api/health"); resp.StatusCode != http.StatusOK {
		t.Errorf("new session = %d, want 200", resp.StatusCode)
	}
}

func TestSessionValidity(t *testing.T) {
	creds, other := newCredentials(), newCredentials()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	valid := creds.session(now.Add(sessionLifetime))
	exp, sig, _ := strings.Cut(valid, ".")
	for name, tc := range map[string]struct {
		value string
		at    time.Time
		want  bool
	}{
		"fresh":                {valid, now, true},
		"just before expiry":   {valid, now.Add(sessionLifetime - time.Second), true},
		"expired":              {valid, now.Add(sessionLifetime), false},
		"other console's key":  {other.session(now.Add(sessionLifetime)), now, false},
		"extended expiry":      {"9999999999." + sig, now, false},
		"tampered signature":   {exp + "." + strings.Repeat("A", len(sig)), now, false},
		"no signature":         {exp, now, false},
		"empty":                {"", now, false},
		"garbage":              {"a.b.c", now, false},
		"oversized":            {valid + strings.Repeat("A", maxSessionCookie), now, false},
		"signature not base64": {exp + ".!!!", now, false},
		"token instead":        {creds.current(), now, false},
	} {
		if got := creds.validSession(tc.value, tc.at); got != tc.want {
			t.Errorf("%s: validSession = %v, want %v", name, got, tc.want)
		}
	}
	if !creds.matches(creds.current()) || creds.matches("") || creds.matches(other.current()) {
		t.Error("matches accepts the wrong token or refuses the right one")
	}
}

func TestSessionExpiresInBrowser(t *testing.T) {
	c, b := startConsole(t, &syncBuffer{})
	now := time.Now()
	c.now = func() time.Time { return now }
	if resp, _ := b.signIn(c.Token(), "/"); resp.StatusCode != http.StatusSeeOther {
		t.Fatal("sign-in failed")
	}
	now = now.Add(sessionLifetime + time.Second)
	if resp, _ := b.get("/api/health"); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("session after %s = %d, want 401", sessionLifetime, resp.StatusCode)
	}
}

func TestHealthFollowsTheNode(t *testing.T) {
	c, b := startConsole(t, &syncBuffer{})
	mode, statuses := "observe", []lifecycle.Status{{Name: "console", State: lifecycle.StateRunning, Ready: true}}
	c.node.Mode = func() string { return mode }
	c.node.Status = func() []lifecycle.Status { return statuses }
	if resp, _ := b.signIn(c.Token(), "/"); resp.StatusCode != http.StatusSeeOther {
		t.Fatal("sign-in failed")
	}
	mode = "enforce"
	statuses = append(statuses, lifecycle.Status{Name: "enforce", State: lifecycle.StateRunning, Error: "nft: permission denied"})
	_, body := b.get("/api/health")
	var h healthResponse
	if err := json.Unmarshal([]byte(body), &h); err != nil || h.Mode != "enforce" || h.State != HealthDegraded ||
		len(h.Problems) != 1 || h.Problems[0] != "enforce: nft: permission denied" {
		t.Errorf("health after a change = %s", body)
	}
}

func TestSafeNext(t *testing.T) {
	for next, want := range map[string]string{
		"/":                      "/",
		"/decisions?state=block": "/decisions?state=block",
		"":                       "/",
		"decisions":              "/",
		"//evil.example/":        "/",
		"/\\evil.example":        "/",
		"/\t/evil.example":       "/",
		"/\n/evil.example":       "/",
		"https://evil.example":   "/",
		"/a\\b":                  "/",
		"/%2F%2Fevil.example":    "/%2F%2Fevil.example",
	} {
		if got := safeNext(next); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", next, got, want)
		}
	}
}

func TestCookieName(t *testing.T) {
	for host, want := range map[string]string{
		"127.0.0.1:9465":  "obie_console_9465",
		"localhost:10000": "obie_console_10000",
		"[::1]:9465":      "obie_console_9465",
		"127.0.0.1":       "obie_console_80",
		"127.0.0.1:x;y":   "obie_console_80",
	} {
		if got := cookieName(host); got != want {
			t.Errorf("cookieName(%q) = %q, want %q", host, got, want)
		}
	}
}
