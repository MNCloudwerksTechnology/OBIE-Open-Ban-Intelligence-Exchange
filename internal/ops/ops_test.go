package ops

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
	"github.com/MNCloudwerksTechnology/obie/internal/version"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func get(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestHealthz(t *testing.T) {
	h := Handler(func() []lifecycle.Status { return nil }, discardLogger())
	rec := get(t, h, http.MethodGet, "/healthz")
	if rec.Code != http.StatusOK || rec.Body.String() != "ok\n" {
		t.Errorf("GET /healthz = %d %q", rec.Code, rec.Body.String())
	}
	if rec := get(t, h, http.MethodPost, "/healthz"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /healthz = %d, want 405", rec.Code)
	}
}

func TestReadyz(t *testing.T) {
	running := lifecycle.Status{Name: "ops", State: lifecycle.StateRunning, Ready: true}
	pending := lifecycle.Status{Name: "admin", State: lifecycle.StatePending}
	tests := []struct {
		name     string
		statuses []lifecycle.Status
		wantCode int
		want     ReadyResponse
	}{
		{"all ready", []lifecycle.Status{running}, http.StatusOK, ReadyResponse{Ready: true}},
		{"one not ready", []lifecycle.Status{running, pending}, http.StatusServiceUnavailable,
			ReadyResponse{Ready: false, NotReady: []lifecycle.Status{pending}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := Handler(func() []lifecycle.Status { return tt.statuses }, discardLogger())
			rec := get(t, h, http.MethodGet, "/readyz")
			if rec.Code != tt.wantCode {
				t.Errorf("code = %d, want %d", rec.Code, tt.wantCode)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q", ct)
			}
			var got ReadyResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("body = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestReadyzListsNotReadySubsystemsAsJSON(t *testing.T) {
	h := Handler(func() []lifecycle.Status {
		return []lifecycle.Status{{Name: "admin", State: lifecycle.StateFailed, Error: "boom"}}
	}, discardLogger())
	body := get(t, h, http.MethodGet, "/readyz").Body.String()
	want := `{"ready":false,"not_ready":[{"name":"admin","state":"failed","ready":false,"error":"boom"}]}` + "\n"
	if body != want {
		t.Errorf("body = %s, want %s", body, want)
	}
}

func TestMetricsServesDefaultRegistry(t *testing.T) {
	h := Handler(func() []lifecycle.Status { return nil }, discardLogger())
	rec := get(t, h, http.MethodGet, "/metrics")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "go_goroutines") {
		t.Errorf("GET /metrics = %d, body lacks default Go collector metrics", rec.Code)
	}
}

func TestMetricsServesBuildInfo(t *testing.T) {
	h := Handler(func() []lifecycle.Status { return nil }, discardLogger())
	body := get(t, h, http.MethodGet, "/metrics").Body.String()
	if want := `obie_build_info{version="` + version.Version + `"} 1`; !strings.Contains(body, want) {
		t.Errorf("GET /metrics lacks %s", want)
	}
}

func TestServerSubsystem(t *testing.T) {
	s := New("127.0.0.1:0", func() []lifecycle.Status { return nil }, discardLogger())
	if s.Name() != Name {
		t.Errorf("Name = %q", s.Name())
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Stop(context.Background()) }()
	resp, err := http.Get("http://" + s.Addr().String() + "/healthz") // #nosec G107 -- test server URL.
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /healthz = %d", resp.StatusCode)
	}
}
