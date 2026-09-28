package admin

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// EnforcedPath lists the entries the enforcement backend applies.
const EnforcedPath = "/v1/enforced"

// EnforcedResponse is the JSON body of GET /v1/enforced.
type EnforcedResponse struct {
	// Mode is the current node.mode; in observe mode nothing is applied.
	Mode string `json:"mode"`
	// Entries are the applied entries, ordered by prefix.
	Entries []EnforcedEntry `json:"entries"`
}

// EnforcedEntry is one entry applied by the enforcement backend.
type EnforcedEntry struct {
	// Prefix is the blocked address or range in CIDR notation.
	Prefix string `json:"prefix"`
	// ExpiresAt is when the backend drops the entry on its own.
	ExpiresAt time.Time `json:"expires_at"`
}

// handleEnforced registers the enforcement endpoint on mux.
func handleEnforced(mux *http.ServeMux, info Info, log *slog.Logger) {
	mux.HandleFunc("GET "+EnforcedPath, func(w http.ResponseWriter, r *http.Request) {
		if info.Enforced == nil {
			http.Error(w, "the enforcement backend is not available", http.StatusServiceUnavailable)
			return
		}
		entries, err := info.Enforced(r.Context())
		if err != nil {
			log.Error("listing the enforced entries failed", "error", err)
			http.Error(w, "listing the enforced entries failed; see the obied log", http.StatusInternalServerError)
			return
		}
		resp := EnforcedResponse{Mode: info.Mode(), Entries: append([]EnforcedEntry{}, entries...)}
		writeJSON(w, resp, log)
	})
}

// Enforced fetches the entries the enforcement backend applies.
func (c *Client) Enforced(ctx context.Context) (*EnforcedResponse, error) {
	var resp EnforcedResponse
	if err := c.get(ctx, EnforcedPath, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
