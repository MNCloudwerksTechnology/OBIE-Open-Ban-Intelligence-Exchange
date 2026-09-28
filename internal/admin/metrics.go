package admin

import (
	"net/http"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
)

// unmatchedEndpoint is the endpoint label of requests that reached no
// endpoint: refused ones, unknown paths and wrong methods.
const unmatchedEndpoint = "unmatched"

var requestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
	Namespace: "obie",
	Name:      "admin_requests_total",
	Help:      "Admin API requests, by endpoint (method and path pattern) and HTTP status code.",
}, []string{"endpoint", "code"})

func init() {
	prometheus.MustRegister(requestsTotal)
}

// statusRecorder remembers the status code written through it.
type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.code = code
	s.ResponseWriter.WriteHeader(code)
}

// countRequests counts every request by the pattern of the endpoint that
// served it, never by its path, which may hold an indicator.
func countRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(rec, r)
		requestsTotal.WithLabelValues(endpointLabel(r.Pattern), strconv.Itoa(rec.code)).Inc()
	})
}

// endpointLabel returns the endpoint label of the ServeMux pattern that
// served a request, e.g. "GET /v1/decisions/{indicator...}";
// unmatchedEndpoint for none.
func endpointLabel(pattern string) string {
	if pattern == "" {
		return unmatchedEndpoint
	}
	return pattern
}
