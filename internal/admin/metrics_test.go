package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func requests(endpoint, code string) float64 {
	return testutil.ToFloat64(requestsTotal.WithLabelValues(endpoint, code))
}

func TestMetricsRegistered(t *testing.T) {
	if err := prometheus.Register(requestsTotal); err == nil {
		t.Error("obie_admin_requests_total was not registered")
	}
}

// TestCountRequests: requests are counted by endpoint pattern, never by
// the indicator in their path, and by status code.
func TestCountRequests(t *testing.T) {
	h := countRequests(Handler(testInfo(), discardLogger()))
	const explain = "GET /v1/decisions/{indicator...}"
	before := map[[2]string]float64{}
	for _, k := range [][2]string{{"GET /v1/status", "200"}, {explain, "200"}, {explain, "400"}, {unmatchedEndpoint, "404"}, {unmatchedEndpoint, "405"}} {
		before[k] = requests(k[0], k[1])
	}
	for _, r := range []struct{ method, path string }{
		{http.MethodGet, StatusPath},
		{http.MethodGet, DecisionsPath + "/198.51.100.7"},
		{http.MethodGet, DecisionsPath + "/198.51.100.8"},
		{http.MethodGet, DecisionsPath + "/not-an-address"},
		{http.MethodGet, "/v1/nope"},
		{http.MethodPost, StatusPath},
	} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(r.method, r.path, nil))
	}
	want := map[[2]string]float64{{"GET /v1/status", "200"}: 1, {explain, "200"}: 2, {explain, "400"}: 1,
		{unmatchedEndpoint, "404"}: 1, {unmatchedEndpoint, "405"}: 1}
	for k, n := range want {
		if got := requests(k[0], k[1]) - before[k]; got != n {
			t.Errorf("obie_admin_requests_total{endpoint=%q,code=%q} rose by %v, want %v", k[0], k[1], got, n)
		}
	}
}

// TestServerCountsRequests: the admin server counts the requests it serves.
func TestServerCountsRequests(t *testing.T) {
	path := socketPath(t)
	startServer(t, path, "obie-no-such-group", discardLogger())
	before := requests("GET /v1/status", "200")
	if _, err := NewClient(path).Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := requests("GET /v1/status", "200") - before; got != 1 {
		t.Errorf("obie_admin_requests_total rose by %v, want 1", got)
	}
}
