package console

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// secretBytes is the size of the token and of the session key: 256 bits.
const secretBytes = 32

// sessionLifetime bounds a session from its sign-in.
const sessionLifetime = 12 * time.Hour

// maxSessionCookie bounds the session cookie a request may present.
const maxSessionCookie = 128

// credentials hold the console's token and the key that signs its session
// cookies. Both live in obied's memory only — never on disk, in a log, in
// a page or in a URL — and are replaced together, so a new token ends
// every session.
type credentials struct {
	mu    sync.RWMutex
	token string
	key   []byte
}

func newCredentials() *credentials {
	c := &credentials{}
	c.rotate()
	return c
}

// rotate replaces the token and the session key and returns the new token.
func (c *credentials) rotate() string {
	token, key := make([]byte, secretBytes), make([]byte, secretBytes)
	_, _ = rand.Read(token) // crypto/rand.Read never fails; it crashes the program instead.
	_, _ = rand.Read(key)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token, c.key = base64.RawURLEncoding.EncodeToString(token), key
	return c.token
}

// current returns the token.
func (c *credentials) current() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token
}

// matches reports in constant time whether candidate is the token.
func (c *credentials) matches(candidate string) bool {
	want, got := sha256.Sum256([]byte(c.current())), sha256.Sum256([]byte(candidate))
	return subtle.ConstantTimeCompare(want[:], got[:]) == 1
}

// session returns the value of a session cookie that is valid until
// expires: the expiry in Unix seconds and its HMAC-SHA256 under the
// session key.
func (c *credentials) session(expires time.Time) string {
	exp := strconv.FormatInt(expires.Unix(), 10)
	return exp + "." + base64.RawURLEncoding.EncodeToString(c.mac(exp))
}

// validSession reports whether value is a session cookie signed with the
// current session key that has not expired at now.
func (c *credentials) validSession(value string, now time.Time) bool {
	exp, sig, ok := strings.Cut(value, ".")
	if !ok || len(value) > maxSessionCookie {
		return false
	}
	unix, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || !now.Before(time.Unix(unix, 0)) {
		return false
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	return err == nil && hmac.Equal(got, c.mac(exp))
}

func (c *credentials) mac(exp string) []byte {
	c.mu.RLock()
	defer c.mu.RUnlock()
	h := hmac.New(sha256.New, c.key)
	_, _ = h.Write([]byte("obie-console-session\x00" + exp))
	return h.Sum(nil)
}

// Token returns the console's current token, the credential a browser
// signs in with.
func (c *Console) Token() string { return c.creds.current() }

// RotateToken replaces the token and returns the new one. The old token
// stops working and every session ends.
func (c *Console) RotateToken() string {
	token := c.creds.rotate()
	c.log.Info("console token rotated; every console session was signed out")
	return token
}

// cookieName is the name of the session cookie for a console reached at
// host. Cookies are not isolated by port, so the port is part of the name:
// consoles of several nodes forwarded to one workstation do not sign each
// other out.
func cookieName(host string) string {
	port := "80"
	if _, p, err := net.SplitHostPort(host); err == nil && p != "" && len(p) <= 5 && strings.Trim(p, "0123456789") == "" {
		port = p
	}
	return "obie_console_" + port
}

// signedIn reports whether r carries a valid session cookie.
func (c *Console) signedIn(r *http.Request) bool {
	ck, err := r.Cookie(cookieName(r.Host))
	return err == nil && c.creds.validSession(ck.Value, c.now())
}

// setSession starts a session for the browser of r.
func (c *Console) setSession(w http.ResponseWriter, r *http.Request) {
	// #nosec G124 -- plain HTTP on loopback (ADR 0019): not Secure, see below.
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName(r.Host),
		Value:    c.creds.session(c.now().Add(sessionLifetime)),
		Path:     "/",
		HttpOnly: true,
		// Plain HTTP on loopback: Secure would keep some browsers from
		// storing the cookie at all.
		SameSite: http.SameSiteStrictMode,
	})
}

// clearSession ends the session of the browser of r.
func clearSession(w http.ResponseWriter, r *http.Request) {
	// #nosec G124 -- deletes the session cookie, which is not Secure (setSession).
	http.SetCookie(w, &http.Cookie{Name: cookieName(r.Host), Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteStrictMode})
}

// requirePage sends browsers without a session to the sign-in page, which
// returns them to the page they asked for.
func (c *Console) requirePage(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !c.signedIn(r) {
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireAPI answers 401 to requests without a session.
func (c *Console) requireAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !c.signedIn(r) {
			writeJSON(w, http.StatusUnauthorized, apiError{Error: "not signed in: sign in to the console again"}, c.log)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// safeNext returns next if it is a path on the console and "/" otherwise,
// so that signing in never leads to another site.
func safeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
		return "/"
	}
	for _, r := range next {
		// Browsers drop tabs and line breaks from URLs, which could turn
		// "/\t/evil.example" into "//evil.example".
		if r < 0x20 || r == 0x7f || r == '\\' {
			return "/"
		}
	}
	if u, err := url.Parse(next); err != nil || u.Scheme != "" || u.Host != "" {
		return "/"
	}
	return next
}

// userAttrs returns the log attributes naming the local user of r's
// connection; none if it is not known.
func userAttrs(r *http.Request) []any {
	u, ok := r.Context().Value(connKey{}).(*connUser)
	if !ok || u.err != nil {
		return nil
	}
	return []any{"uid", u.cred.UID}
}
