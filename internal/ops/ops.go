// Package ops serves the operational endpoints of the node: /healthz,
// /readyz and the Prometheus /metrics.
package ops

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/MNCloudwerksTechnology/obie/internal/httpserver"
	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
)

// Name is the subsystem name of the ops server.
const Name = "ops"

// StatusFunc reports the current status of every subsystem.
type StatusFunc func() []lifecycle.Status

// ReadyResponse is the JSON body of /readyz.
type ReadyResponse struct {
	Ready bool `json:"ready"`
	// NotReady lists the subsystems that are not ready; omitted when ready.
	NotReady []lifecycle.Status `json:"not_ready,omitempty"`
}

// New returns the ops server subsystem listening on the TCP address addr.
func New(addr string, status StatusFunc, log *slog.Logger) *httpserver.Server {
	return httpserver.New(Name, httpserver.TCP(addr), Handler(status, log), log)
}

// Handler returns the ops endpoints. Readiness is taken from status.
func Handler(status StatusFunc, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		notReady := lifecycle.NotReady(status())
		resp := ReadyResponse{Ready: len(notReady) == 0, NotReady: notReady}
		code := http.StatusOK
		if !resp.Ready {
			code = http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Debug("writing /readyz response", "error", err)
		}
	})
	mux.Handle("GET /metrics", promhttp.Handler())
	return mux
}
