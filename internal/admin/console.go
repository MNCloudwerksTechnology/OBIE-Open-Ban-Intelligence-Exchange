package admin

import (
	"context"
	"log/slog"
	"net/http"
)

// Endpoints of the web console (ADR 0019).
const (
	// ConsolePath reports the console and the token to sign in with.
	ConsolePath = "/v1/console"
	// ConsoleTokenPath replaces the console's token.
	ConsoleTokenPath = "/v1/console/token" // #nosec G101 -- a URL path, not a credential.
)

// ConsoleResponse is the JSON body of GET /v1/console and
// POST /v1/console/token.
type ConsoleResponse struct {
	// Enabled is console.enabled.
	Enabled bool `json:"enabled"`
	// Listen is console.listen.
	Listen string `json:"listen"`
	// URL is where the console serves; omitted while it does not.
	URL string `json:"url,omitempty"`
	// Error is why the enabled console is not serving.
	Error string `json:"error,omitempty"`
	// Token is the credential to sign in with. obied keeps it in memory
	// only; a restart replaces it.
	Token string `json:"token"`
}

// ConsoleService reports the web console and replaces its token.
type ConsoleService interface {
	Console() ConsoleResponse
	// RotateConsoleToken replaces the token, which ends every console
	// session, and reports the console with the new token.
	RotateConsoleToken() ConsoleResponse
}

// handleConsole registers the console endpoints on mux.
func handleConsole(mux *http.ServeMux, info Info, log *slog.Logger) {
	unavailable := func(w http.ResponseWriter) bool {
		if info.Console == nil {
			http.Error(w, "the web console is not available", http.StatusServiceUnavailable)
			return true
		}
		return false
	}
	mux.HandleFunc("GET "+ConsolePath, func(w http.ResponseWriter, _ *http.Request) {
		if !unavailable(w) {
			writeJSON(w, info.Console.Console(), log)
		}
	})
	mux.HandleFunc("POST "+ConsoleTokenPath, func(w http.ResponseWriter, _ *http.Request) {
		if !unavailable(w) {
			writeJSON(w, info.Console.RotateConsoleToken(), log)
		}
	})
}

// Console fetches the web console's state and token.
func (c *Client) Console(ctx context.Context) (*ConsoleResponse, error) {
	var resp ConsoleResponse
	if err := c.get(ctx, ConsolePath, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// RotateConsoleToken replaces the web console's token and returns the
// console's state with the new one.
func (c *Client) RotateConsoleToken(ctx context.Context) (*ConsoleResponse, error) {
	var resp ConsoleResponse
	if err := c.post(ctx, ConsoleTokenPath, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
