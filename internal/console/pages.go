package console

import (
	"bytes"
	"embed"
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"

	"golang.org/x/time/rate"
)

//go:embed templates/*.html
var templateFiles embed.FS

// signInTemplate is the sign-in page. It shows nothing about the node.
var signInTemplate = template.Must(template.ParseFS(templateFiles, "templates/signin.html"))

// maxSignInBody bounds the sign-in form.
const maxSignInBody = 4 << 10

// newSignInLimit limits sign-in attempts to one per second, five at once:
// plenty for a person, useless for guessing a 256-bit token.
func newSignInLimit() *rate.Limiter { return rate.NewLimiter(1, 5) }

// signInPage is the data of the sign-in page.
type signInPage struct {
	// Next is where to go after signing in.
	Next string
	// Error explains why the last attempt failed.
	Error string
	// Action is set when an action was not carried out because the
	// session ended (ADR 0026).
	Action bool
}

// render writes the page t with data and status code.
func (c *Console) render(w http.ResponseWriter, code int, t *template.Template, data any) {
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		c.log.Error("rendering a console page", "page", t.Name(), "error", err)
		http.Error(w, "internal error: the page could not be rendered; the obied log says why", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write(buf.Bytes())
}

// showSignIn serves the sign-in page, or sends a browser that is signed in
// on to where it was going.
func (c *Console) showSignIn(w http.ResponseWriter, r *http.Request) {
	next := safeNext(r.URL.Query().Get("next"))
	action := r.URL.Query().Get("reason") == "action"
	if crossSiteNavigation(r) {
		// A link on another site or port may not lead through sign-in to
		// an action's page, nor say that an action waits (ADR 0026).
		next, action = crossSiteNext(next), false
	}
	if c.signedIn(r) {
		http.Redirect(w, r, next, http.StatusSeeOther) // #nosec G710 -- safeNext allows only paths on the console.
		return
	}
	c.render(w, http.StatusOK, signInTemplate, signInPage{Next: next, Action: action})
}

// crossSiteNext returns next for a sign-in page reached from another site
// or port, or "/" if it would lead on to an action's page: directly,
// through dot segments (plain or percent-encoded, which browsers remove),
// or through the sign-in page again, which redirects once signed in.
func crossSiteNext(next string) string {
	u, err := url.Parse(next)
	if err != nil {
		return "/"
	}
	switch p := path.Clean("/" + u.Path); {
	case p == "/login", p == "/actions", strings.HasPrefix(p, "/actions/"):
		return "/"
	default:
		return next
	}
}

// signIn exchanges the token for a session.
func (c *Console) signIn(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSignInBody)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	page := signInPage{Next: safeNext(r.PostForm.Get("next"))}
	user := userAttrs(r)
	if !c.signInLimit.Allow() {
		c.warn("console sign-in refused: too many attempts", user...)
		page.Error = "Too many sign-in attempts. Wait a few seconds, then try again."
		c.render(w, http.StatusTooManyRequests, signInTemplate, page)
		return
	}
	if !c.creds.matches(strings.TrimSpace(r.PostForm.Get("token"))) {
		c.warn("console sign-in with a wrong token", user...)
		page.Error = "This is not the console's current token. Get it with: sudo obiectl console"
		c.render(w, http.StatusForbidden, signInTemplate, page)
		return
	}
	c.setSession(w, r)
	c.log.Info("console sign-in", user...)
	http.Redirect(w, r, page.Next, http.StatusSeeOther) // #nosec G710 -- safeNext allows only paths on the console.
}

// signOut ends the browser's session.
func (c *Console) signOut(w http.ResponseWriter, r *http.Request) {
	clearSession(w, r)
	c.log.Info("console sign-out", userAttrs(r)...)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// healthResponse is the JSON body of GET /api/health.
type healthResponse struct {
	Health
	// Mode is node.mode.
	Mode string `json:"mode"`
}

// serveHealth reports the node's health and mode, for the indicator on
// every page.
func (c *Console) serveHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{Health: nodeHealth(c.node.Status()), Mode: c.node.Mode()}, c.log)
}

// apiError is the JSON body of an API error.
type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, code int, v any, log *slog.Logger) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Debug("writing a console response", "error", err)
	}
}
