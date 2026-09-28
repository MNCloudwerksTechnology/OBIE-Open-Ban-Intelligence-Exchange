package console

import (
	"net/http"
	"strings"
)

// routes returns the console's handler: the request guards in front of its
// pages. Every response carries the security headers; a request passes the
// host check, then the local-user check, then the cross-site check.
func (c *Console) routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /assets/", assets())
	mux.HandleFunc("GET /login", c.showSignIn)
	mux.HandleFunc("POST /login", c.signIn)
	mux.HandleFunc("POST /logout", c.signOut)
	mux.Handle("GET /api/health", c.requireAPI(http.HandlerFunc(c.serveHealth)))
	c.pages = c.views()
	for _, v := range c.pages {
		pattern := "GET " + v.Path
		if strings.HasSuffix(v.Path, "/") {
			pattern += "{$}" // exactly the path, not the subtree
		}
		mux.Handle(pattern, c.requirePage(c.serveView(v)))
	}
	mux.Handle("/", c.requirePage(http.HandlerFunc(c.notFound)))
	return securityHeaders(c.hostGuard(c.localUser(c.fetchGuard(mux))))
}
