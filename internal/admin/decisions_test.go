package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

var (
	blockedUntil = started.Add(time.Hour)
	testBlocked  = DecisionResponse{
		Indicator: obieproto.Indicator{Kind: obieproto.KindIPv4, Value: "203.0.113.7", Scope: "/32"},
		State:     StateBlock, Score: 1.8, Threshold: 1.8, Contributors: 2, Quorum: 2,
		ExpiresAt: &blockedUntil, Reason: "consensus: score 1.8 >= threshold 1.8, 2 >= quorum 2", EvaluatedAt: started,
	}
	testWatched = DecisionResponse{
		Indicator: obieproto.Indicator{Kind: obieproto.KindIPv4, Value: "203.0.113.8", Scope: "/32"},
		State:     StateNone, Score: 0.9, Threshold: 1.8, Contributors: 1, Quorum: 2,
		Reason: "below consensus: score 0.9 < threshold 1.8, 1 < quorum 2", EvaluatedAt: started,
	}
)

// failingIndicator makes testExplain fail.
const failingIndicator = "203.0.113.66"

// testExplain explains every indicator like testBlocked, with one
// publisher.
func testExplain(ind obieproto.Indicator) (DecisionResponse, error) {
	if ind.Value == failingIndicator {
		return DecisionResponse{}, errors.New("store is not open")
	}
	d := testBlocked
	d.Indicator = ind
	d.Publishers = []ContributionResponse{{
		PeerID: testPeers[0].PeerID, Name: "seed", EventID: "01900000-0000-7000-8000-000000000001", Action: obieproto.ActionBan,
		Weight: 1, Confidence: 0.9, Score: 0.9, Contributes: true, IssuedAt: started, ExpiresAt: blockedUntil,
	}}
	d.Sovereignty = &SovereigntyResponse{Applied: true, Note: "no allow-list entry or override applies"}
	return d, nil
}

func testDecisions(state string) []DecisionResponse {
	switch state {
	case StateBlock:
		return []DecisionResponse{testBlocked}
	case StateNone:
		return []DecisionResponse{testWatched}
	case StateAllowed:
		return nil
	}
	return []DecisionResponse{testBlocked, testWatched}
}

func TestParseIndicator(t *testing.T) {
	tests := []struct {
		in, key, scope string
		wantErr        bool
	}{
		{in: "203.0.113.7", key: "ipv4:203.0.113.7", scope: "/32"},
		{in: " 203.0.113.7 ", key: "ipv4:203.0.113.7", scope: "/32"},
		{in: "2001:DB8::1", key: "ipv6:2001:db8::1", scope: "/128"},
		{in: "198.51.100.0/24", key: "cidr:198.51.100.0/24", scope: "/24"},
		{in: "198.51.100.9/24", key: "cidr:198.51.100.0/24", scope: "/24"},
		{in: "198.51.100.9/32", key: "ipv4:198.51.100.9", scope: "/32"},
		{in: "2001:db8::/48", key: "cidr:2001:db8::/48", scope: "/48"},
		{in: "ipv4:203.0.113.7", key: "ipv4:203.0.113.7", scope: "/32"},
		{in: "ipv6:2001:db8::1", key: "ipv6:2001:db8::1", scope: "/128"},
		{in: "cidr:198.51.100.0/24", key: "cidr:198.51.100.0/24", scope: "/24"},
		{in: "", wantErr: true},
		{in: "example.org", wantErr: true},
		{in: "ipv4:2001:db8::1", wantErr: true},
		{in: "10.0.0.0/8", wantErr: true}, // broader than /16
		{in: "fe80::1%eth0", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			ind, err := ParseIndicator(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseIndicator(%q) = %+v, want an error", tt.in, ind)
				}
				return
			}
			if err != nil || ind.Key() != tt.key || ind.Scope != tt.scope {
				t.Errorf("ParseIndicator(%q) = %+v, %v; want %s %s", tt.in, ind, err, tt.key, tt.scope)
			}
		})
	}
}

func TestDecisionHandler(t *testing.T) {
	tests := []struct {
		name, path string
		info       func(*Info)
		code       int
		body       string
	}{
		{name: "address", path: "/v1/decisions/203.0.113.9", code: http.StatusOK, body: `"indicator":{"kind":"ipv4","value":"203.0.113.9","scope":"/32"},"state":"block"`},
		{name: "cidr", path: "/v1/decisions/198.51.100.0/24", code: http.StatusOK, body: `"value":"198.51.100.0/24"`},
		{name: "escaped cidr", path: "/v1/decisions/198.51.100.0%2F24", code: http.StatusOK, body: `"value":"198.51.100.0/24"`},
		{name: "ipv6", path: "/v1/decisions/2001:db8::1", code: http.StatusOK, body: `"value":"2001:db8::1"`},
		{name: "publishers and sovereignty", path: "/v1/decisions/203.0.113.9", code: http.StatusOK,
			body: `"publishers":[{"peer_id":"12D3KooWGzBX6MWMMz3kHmFfyT3vJxFoy4xQF8NbXN7xBAFhGyvd","name":"seed","local":false,` +
				`"event_id":"01900000-0000-7000-8000-000000000001","action":"ban","weight":1,"confidence":0.9,"score":0.9,"contributes":true,` +
				`"issued_at":"2026-09-27T10:00:00Z","expires_at":"2026-09-27T11:00:00Z"}],` +
				`"sovereignty":{"applied":true,"note":"no allow-list entry or override applies"}`},
		{name: "invalid indicator", path: "/v1/decisions/not-an-ip", code: http.StatusBadRequest, body: "invalid indicator"},
		{name: "internal range", path: "/v1/decisions/10.0.0.0/8", code: http.StatusBadRequest, body: "invalid indicator"},
		{name: "engine failure", path: "/v1/decisions/" + failingIndicator, code: http.StatusInternalServerError, body: "see the obied log"},
		{name: "no engine", path: "/v1/decisions/203.0.113.9", info: func(i *Info) { i.Explain = nil },
			code: http.StatusServiceUnavailable, body: "not available"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := testInfo()
			if tt.info != nil {
				tt.info(&info)
			}
			rec := httptest.NewRecorder()
			Handler(info, discardLogger()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if rec.Code != tt.code || !strings.Contains(rec.Body.String(), tt.body) {
				t.Errorf("GET %s = %d\n%s\nwant %d containing %s", tt.path, rec.Code, rec.Body.String(), tt.code, tt.body)
			}
		})
	}
}

func TestDecisionsHandler(t *testing.T) {
	const (
		blocked = `{"indicator":{"kind":"ipv4","value":"203.0.113.7","scope":"/32"},"state":"block","score":1.8,"threshold":1.8,` +
			`"contributors":2,"quorum":2,"local_autoblock":false,"expires_at":"2026-09-27T11:00:00Z",` +
			`"reason":"consensus: score 1.8 \u003e= threshold 1.8, 2 \u003e= quorum 2","evaluated_at":"2026-09-27T10:00:00Z"}`
		watched = `{"indicator":{"kind":"ipv4","value":"203.0.113.8","scope":"/32"},"state":"none","score":0.9,"threshold":1.8,` +
			`"contributors":1,"quorum":2,"local_autoblock":false,` +
			`"reason":"below consensus: score 0.9 \u003c threshold 1.8, 1 \u003c quorum 2","evaluated_at":"2026-09-27T10:00:00Z"}`
	)
	tests := []struct {
		name, query string
		info        func(*Info)
		code        int
		body        string
	}{
		{name: "all", code: http.StatusOK, body: `{"decisions":[` + blocked + `,` + watched + `]}` + "\n"},
		{name: "block", query: "?state=block", code: http.StatusOK, body: `{"decisions":[` + blocked + `]}` + "\n"},
		{name: "none", query: "?state=none", code: http.StatusOK, body: `{"decisions":[` + watched + `]}` + "\n"},
		{name: "allowed", query: "?state=allowed", code: http.StatusOK, body: `{"decisions":[]}` + "\n"},
		{name: "invalid state", query: "?state=blocked", code: http.StatusBadRequest, body: `invalid state "blocked": want block, none or allowed` + "\n"},
		{name: "no engine", info: func(i *Info) { i.Decisions = nil }, code: http.StatusOK, body: `{"decisions":[]}` + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := testInfo()
			if tt.info != nil {
				tt.info(&info)
			}
			rec := httptest.NewRecorder()
			Handler(info, discardLogger()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, DecisionsPath+tt.query, nil))
			if rec.Code != tt.code || rec.Body.String() != tt.body {
				t.Errorf("GET %s = %d\n%s\nwant %d\n%s", tt.query, rec.Code, rec.Body.String(), tt.code, tt.body)
			}
		})
	}
}

func TestClientDecisionsOverSocket(t *testing.T) {
	path := socketPath(t)
	startServer(t, path, "obie-no-such-group", discardLogger())
	client := NewClient(path)
	ctx := context.Background()

	for _, in := range []string{"203.0.113.9", "198.51.100.0/24", "2001:db8::1", "cidr:2001:db8::/48"} {
		got, err := client.Explain(ctx, in)
		if err != nil {
			t.Fatalf("Explain(%q): %v", in, err)
		}
		want, _ := ParseIndicator(in)
		if got.Indicator != want || len(got.Publishers) != 1 || got.Sovereignty == nil || !got.ExpiresAt.Equal(blockedUntil) {
			t.Errorf("Explain(%q) = %+v", in, got)
		}
	}
	if _, err := client.Explain(ctx, "bogus"); err == nil || !strings.Contains(err.Error(), "400 Bad Request: invalid indicator") {
		t.Errorf("Explain(bogus) error = %v", err)
	}
	if _, err := client.Explain(ctx, " "); err == nil {
		t.Error("Explain of an empty indicator succeeded")
	}

	list, err := client.Decisions(ctx, StateBlock)
	if err != nil {
		t.Fatal(err)
	}
	if want := []DecisionResponse{testBlocked}; !reflect.DeepEqual(list.Decisions, want) {
		t.Errorf("Decisions(block) = %+v, want %+v", list.Decisions, want)
	}
	if list, err := client.Decisions(ctx, ""); err != nil || len(list.Decisions) != 2 {
		t.Errorf("Decisions() = %+v, %v", list, err)
	}
}
