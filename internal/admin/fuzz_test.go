package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/identity"
	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/internal/verdicts"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// fuzzOverrides accepts every override without storing it.
type fuzzOverrides struct{}

func (fuzzOverrides) List() ([]OverrideResponse, error) { return nil, nil }

func (fuzzOverrides) Set(_ context.Context, ind obieproto.Indicator, action string, _ time.Duration, note string) (OverrideResponse, error) {
	return OverrideResponse{Indicator: ind, Action: action, Note: note, CreatedAt: started}, nil
}

func (fuzzOverrides) Delete(context.Context, obieproto.Indicator) (bool, error) { return false, nil }

// newFuzzHandler returns the admin API backed by the real verdict service
// on an in-memory store; nothing it is asked may fail internally.
func newFuzzHandler(tb testing.TB) http.Handler {
	tb.Helper()
	key, err := identity.Create(filepath.Join(tb.TempDir(), "state"), false)
	if err != nil {
		tb.Fatal(err)
	}
	db := store.NewMemory(discardLogger(), store.Options{MaxIndicators: 10000, Self: key.PeerID()})
	if err := db.Start(context.Background()); err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = db.Stop(context.Background()) })
	info := testInfo(lifecycle.Status{Name: "admin", State: lifecycle.StateRunning, Ready: true})
	info.Explain = func(ind obieproto.Indicator) (DecisionResponse, error) {
		d := testBlocked
		d.Indicator = ind
		return d, nil
	}
	info.Overrides = fuzzOverrides{}
	info.Enforced = func(context.Context) ([]EnforcedEntry, error) { return nil, nil }
	info.Console = &fakeConsole{}
	info.Verdicts = verdicts.New(verdicts.Options{
		Store: db, Publisher: &storePublisher{db: db}, Signer: key,
		DefaultTTL: 7 * 24 * time.Hour, MaxTTL: 30 * 24 * time.Hour,
		Allowlist: []netip.Prefix{netip.MustParsePrefix("85.20.0.0/16")},
	}, discardLogger())
	return Handler(info, discardLogger())
}

// fuzzRequests are the requests FuzzRequests sends: a body for the POST
// endpoints, a path or query suffix for the others.
var fuzzRequests = []struct {
	method, path string
	body, query  bool
}{
	{method: http.MethodPost, path: ReportsPath, body: true},
	{method: http.MethodPost, path: RevocationsPath, body: true},
	{method: http.MethodPost, path: OverridesPath, body: true},
	{method: http.MethodDelete, path: OverridesPath + "/"},
	{method: http.MethodGet, path: DecisionsPath + "/"},
	{method: http.MethodGet, path: DecisionsPath, query: true},
	{method: http.MethodGet, path: IndicatorsPath + "/"},
	{method: http.MethodGet, path: IndicatorsPath, query: true},
	{method: http.MethodPost, path: ConsoleTokenPath, body: true},
}

// FuzzRequests checks that the admin API never panics and never fails
// internally (500) on an arbitrary request body, path or query, whatever
// endpoint it goes to, and that every successful response is JSON.
func FuzzRequests(f *testing.F) {
	h := newFuzzHandler(f)
	seeds := []string{
		sshBody,
		`{"cidr":"85.10.0.0/24","protocol":"http","reason":"scan","events":1,"confidence":1,"ttl":"36h","action":"watch","mitre":["T1110"]}`,
		`{"ip":"85.10.0.7","protocol":"ssh","reason":"x","events":1,"evidence_lines":["line 1","line 2"],"ttl":3600}`,
		`{"indicator":"85.10.0.7","reason":"false_positive"}`,
		`{"event_id":"01900000-0000-7000-8000-000000000001","reason":"false_positive"}`,
		`{"indicator":"185.0.0.1","action":"force_block","ttl_seconds":3600,"note":"abuse"}`,
		`{"ip":"85.10.0.7","ttl":1e300}`,
		"ipv4:203.0.113.7", "2001:db8::/48", "state=block", "publisher=12D3KooWJ1TsijH7H5F74hfAD5XishQz3sxrmAtVY37GtNd9CqYf&limit=2&after=x",
		`{}`, `[]`, `null`,
	}
	for i := range fuzzRequests {
		for _, s := range seeds {
			f.Add(uint8(i), []byte(s))
		}
	}
	f.Fuzz(func(t *testing.T, which uint8, data []byte) {
		r := fuzzRequests[int(which)%len(fuzzRequests)]
		req := httptest.NewRequest(r.method, "/", bytes.NewReader(nil))
		req.URL = &url.URL{Path: r.path}
		switch {
		case r.body:
			req = httptest.NewRequest(r.method, r.path, bytes.NewReader(data))
		case r.query:
			req.URL.RawQuery = string(data)
		default:
			req.URL.Path += string(data)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code >= http.StatusInternalServerError {
			t.Fatalf("%s %s: %d %s", req.Method, req.URL, rec.Code, rec.Body.String())
		}
		if rec.Code < 300 && !json.Valid(rec.Body.Bytes()) {
			t.Fatalf("%s %s: %d with a body that is not JSON: %q", req.Method, req.URL, rec.Code, rec.Body.String())
		}
	})
}
