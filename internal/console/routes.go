package console

import (
	"net/http"
)

// routes returns the console's handler: the request guards in front of its
// pages. Every response carries the security headers; a request passes the
// host check, then the local-user check, then the cross-site check.
func (c *Console) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", c.showSignIn)
	mux.HandleFunc("POST /login", c.signIn)
	mux.HandleFunc("POST /logout", c.signOut)
	mux.Handle("GET /api/health", c.requireAPI(http.HandlerFunc(c.serveHealth)))
	mux.Handle("/", c.requirePage(http.NotFoundHandler()))
	return securityHeaders(c.hostGuard(c.localUser(c.fetchGuard(mux))))
}
