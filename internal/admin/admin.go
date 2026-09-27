// Package admin implements the local admin API of obied: HTTP/JSON on a Unix
// socket. It holds the server (a lifecycle subsystem), the wire types and the
// client used by obiectl.
package admin

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/httpserver"
	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
)

// Name is the subsystem name of the admin API server.
const Name = "admin"

// Endpoints of the admin API.
const (
	// StatusPath reports the node status.
	StatusPath = "/v1/status"
	// IdentityPath reports the node identity.
	IdentityPath = "/v1/identity"
)

// StatusResponse is the JSON body of GET /v1/status.
type StatusResponse struct {
	Version       string    `json:"version"`
	Mode          string    `json:"mode"`
	StartedAt     time.Time `json:"started_at"`
	UptimeSeconds float64   `json:"uptime_seconds"`
	// Ready is true when every subsystem is ready.
	Ready      bool                       `json:"ready"`
	Subsystems map[string]SubsystemStatus `json:"subsystems"`
}

// Uptime returns the uptime as a duration.
func (s *StatusResponse) Uptime() time.Duration {
	return time.Duration(s.UptimeSeconds * float64(time.Second))
}

// SubsystemStatus is the status of one subsystem.
type SubsystemStatus struct {
	State lifecycle.State `json:"state"`
	Ready bool            `json:"ready"`
	Error string          `json:"error,omitempty"`
}

// IdentityResponse is the JSON body of GET /v1/identity. It never carries
// the private key.
type IdentityResponse struct {
	PeerID string `json:"peer_id"`
	// Fingerprint is the public key fingerprint (identity.Fingerprint).
	Fingerprint string `json:"fingerprint"`
}

// Info is what the admin API reports about the node.
type Info struct {
	Version   string
	Mode      string
	StartedAt time.Time
	Identity  IdentityResponse
	// Status reports the current status of every subsystem.
	Status func() []lifecycle.Status
	// Now returns the current time; time.Now when nil.
	Now func() time.Time
}

// New returns the admin API subsystem serving on the Unix socket at path,
// owned by group if that group exists (see ListenUnix).
func New(path, group string, info Info, log *slog.Logger) *httpserver.Server {
	return httpserver.New(Name, ListenUnix(path, group, log), Handler(info, log), log)
}

// Handler returns the admin API endpoints.
func Handler(info Info, log *slog.Logger) http.Handler {
	now := info.Now
	if now == nil {
		now = time.Now
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+StatusPath, func(w http.ResponseWriter, _ *http.Request) {
		statuses := info.Status()
		resp := StatusResponse{
			Version:       info.Version,
			Mode:          info.Mode,
			StartedAt:     info.StartedAt.UTC(),
			UptimeSeconds: now().Sub(info.StartedAt).Seconds(),
			Ready:         len(lifecycle.NotReady(statuses)) == 0,
			Subsystems:    make(map[string]SubsystemStatus, len(statuses)),
		}
		for _, s := range statuses {
			resp.Subsystems[s.Name] = SubsystemStatus{State: s.State, Ready: s.Ready, Error: s.Error}
		}
		writeJSON(w, resp, log)
	})
	mux.HandleFunc("GET "+IdentityPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, info.Identity, log)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, v any, log *slog.Logger) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Debug("writing response", "error", err)
	}
}
