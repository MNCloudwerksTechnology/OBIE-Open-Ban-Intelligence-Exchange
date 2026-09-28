package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// fakeOverrides keeps overrides in memory; indicator 203.0.113.66 fails.
type fakeOverrides struct {
	mu   sync.Mutex
	byID map[string]OverrideResponse
}

func newFakeOverrides() *fakeOverrides { return &fakeOverrides{byID: map[string]OverrideResponse{}} }

var errStore = errors.New("store is not open")

func (f *fakeOverrides) List() ([]OverrideResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]OverrideResponse, 0, len(f.byID))
	for _, o := range f.byID {
		out = append(out, o)
	}
	slices.SortFunc(out, func(a, b OverrideResponse) int { return strings.Compare(a.Indicator.Key(), b.Indicator.Key()) })
	return out, nil
}

func (f *fakeOverrides) Set(ind obieproto.Indicator, action string, ttl time.Duration, note string) (OverrideResponse, error) {
	if ind.Value == failingIndicator {
		return OverrideResponse{}, errStore
	}
	if len(note) > 10 {
		return OverrideResponse{}, fmt.Errorf("%w: note longer than 10 bytes", ErrInvalid)
	}
	o := OverrideResponse{Indicator: ind, Action: action, Note: note, CreatedAt: started}
	if ttl > 0 {
		end := started.Add(ttl)
		o.ExpiresAt = &end
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byID[ind.Key()] = o
	return o, nil
}

func (f *fakeOverrides) Delete(ind obieproto.Indicator) (bool, error) {
	if ind.Value == failingIndicator {
		return false, errStore
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.byID[ind.Key()]
	delete(f.byID, ind.Key())
	return ok, nil
}

func TestOverrideHandlers(t *testing.T) {
	tests := []struct {
		name, method, path, body string
		info                     func(*Info)
		code                     int
		want                     string
	}{
		{name: "set", method: http.MethodPost, path: OverridesPath,
			body: `{"indicator":"185.0.0.1","action":"force_block","ttl_seconds":3600,"note":"abuse"}`, code: http.StatusOK,
			want: `{"override":{"indicator":{"kind":"ipv4","value":"185.0.0.1","scope":"/32"},"action":"force_block","note":"abuse",` +
				`"created_at":"2026-09-27T10:00:00Z","expires_at":"2026-09-27T11:00:00Z"},"decision":{"indicator":{"kind":"ipv4","value":"185.0.0.1"`},
		{name: "set cidr without ttl", method: http.MethodPost, path: OverridesPath,
			body: `{"indicator":"185.0.0.0/24","action":"force_allow"}`, code: http.StatusOK,
			want: `{"override":{"indicator":{"kind":"cidr","value":"185.0.0.0/24","scope":"/24"},"action":"force_allow","created_at":"2026-09-27T10:00:00Z"},`},
		{name: "set without engine", method: http.MethodPost, path: OverridesPath, info: func(i *Info) { i.Explain = nil },
			body: `{"indicator":"185.0.0.1","action":"force_allow"}`, code: http.StatusOK, want: `"created_at":"2026-09-27T10:00:00Z"}}`},
		{name: "set, explain fails", method: http.MethodPost, path: OverridesPath,
			info: func(i *Info) {
				i.Explain = func(obieproto.Indicator) (DecisionResponse, error) { return DecisionResponse{}, errStore }
			},
			body: `{"indicator":"185.0.0.1","action":"force_allow"}`, code: http.StatusOK, want: `"created_at":"2026-09-27T10:00:00Z"}}`},
		{name: "ineffective force-block", method: http.MethodPost, path: OverridesPath,
			info: func(i *Info) {
				i.Explain = func(ind obieproto.Indicator) (DecisionResponse, error) {
					return DecisionResponse{Indicator: ind, State: StateAllowed, Reason: "allow-listed: built-in range 10.0.0.0/8"}, nil
				}
			},
			body: `{"indicator":"10.0.0.1","action":"force_block"}`, code: http.StatusOK,
			want: `"warning":"the force-block does not take effect: allow-listed: built-in range 10.0.0.0/8"`},
		{name: "bad action", method: http.MethodPost, path: OverridesPath, body: `{"indicator":"185.0.0.1","action":"ban"}`,
			code: http.StatusBadRequest, want: `invalid action "ban": want force_allow or force_block`},
		{name: "negative ttl", method: http.MethodPost, path: OverridesPath, body: `{"indicator":"185.0.0.1","action":"force_allow","ttl_seconds":-1}`,
			code: http.StatusBadRequest, want: "must be between 0 and"},
		{name: "overflowing ttl", method: http.MethodPost, path: OverridesPath, body: `{"indicator":"185.0.0.1","action":"force_allow","ttl_seconds":9300000000}`,
			code: http.StatusBadRequest, want: "must be between 0 and 9223372036"},
		{name: "unknown field", method: http.MethodPost, path: OverridesPath, body: `{"indicator":"185.0.0.1","action":"force_allow","mode":"enforce"}`,
			code: http.StatusBadRequest, want: "invalid request body"},
		{name: "bad indicator", method: http.MethodPost, path: OverridesPath, body: `{"indicator":"example.org","action":"force_allow"}`,
			code: http.StatusBadRequest, want: "invalid indicator"},
		{name: "rejected note", method: http.MethodPost, path: OverridesPath, body: `{"indicator":"185.0.0.1","action":"force_allow","note":"far too long"}`,
			code: http.StatusBadRequest, want: "invalid override: note longer"},
		{name: "store failure", method: http.MethodPost, path: OverridesPath, body: `{"indicator":"` + failingIndicator + `","action":"force_allow"}`,
			code: http.StatusInternalServerError, want: "see the obied log"},
		{name: "unavailable", method: http.MethodPost, path: OverridesPath, info: func(i *Info) { i.Overrides = nil },
			body: `{}`, code: http.StatusServiceUnavailable, want: "not available"},
		{name: "list empty", method: http.MethodGet, path: OverridesPath, code: http.StatusOK, want: `{"overrides":[]}` + "\n"},
		{name: "list unavailable", method: http.MethodGet, path: OverridesPath, info: func(i *Info) { i.Overrides = nil },
			code: http.StatusServiceUnavailable, want: "not available"},
		{name: "delete missing", method: http.MethodDelete, path: OverridesPath + "/185.0.0.1", code: http.StatusNotFound,
			want: "no override: ipv4:185.0.0.1"},
		{name: "delete bad", method: http.MethodDelete, path: OverridesPath + "/nope", code: http.StatusBadRequest, want: "invalid indicator"},
		{name: "delete failure", method: http.MethodDelete, path: OverridesPath + "/" + failingIndicator, code: http.StatusInternalServerError,
			want: "see the obied log"},
		{name: "delete unavailable", method: http.MethodDelete, path: OverridesPath + "/185.0.0.1", info: func(i *Info) { i.Overrides = nil },
			code: http.StatusServiceUnavailable, want: "not available"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := testInfo()
			if tt.info != nil {
				tt.info(&info)
			}
			rec := httptest.NewRecorder()
			Handler(info, discardLogger()).ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body)))
			if rec.Code != tt.code || !strings.Contains(rec.Body.String(), tt.want) {
				t.Errorf("%s %s = %d\n%s\nwant %d containing\n%s", tt.method, tt.path, rec.Code, rec.Body.String(), tt.code, tt.want)
			}
		})
	}
}

func TestClientOverridesOverSocket(t *testing.T) {
	path := socketPath(t)
	startServer(t, path, "obie-no-such-group", discardLogger())
	client := NewClient(path)
	ctx := context.Background()

	res, err := client.SetOverride(ctx, OverrideRequest{Indicator: "185.0.0.1", Action: ActionForceBlock, TTLSeconds: 60, Note: "abuse"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Override == nil || res.Override.Indicator.Key() != "ipv4:185.0.0.1" || res.Decision == nil || res.Decision.Indicator.Key() != "ipv4:185.0.0.1" {
		t.Errorf("SetOverride = %+v", res)
	}
	if _, err := client.SetOverride(ctx, OverrideRequest{Indicator: "2a01::/48", Action: ActionForceAllow}); err != nil {
		t.Fatal(err)
	}
	list, err := client.Overrides(ctx)
	if err != nil || len(list.Overrides) != 2 || list.Overrides[0].Indicator.Key() != "cidr:2a01::/48" || list.Overrides[1].ExpiresAt == nil {
		t.Errorf("Overrides = %+v, %v", list, err)
	}
	if _, err := client.SetOverride(ctx, OverrideRequest{Indicator: "185.0.0.1", Action: "nope"}); err == nil || !strings.Contains(err.Error(), "400 Bad Request: invalid action") {
		t.Errorf("SetOverride(bad) = %v", err)
	}
	var status *StatusError
	if _, err := client.SetOverride(ctx, OverrideRequest{Indicator: "x", Action: ActionForceAllow}); !errors.As(err, &status) || status.Code != http.StatusBadRequest {
		t.Errorf("SetOverride(bad indicator) = %v", err)
	}
	if _, err := client.SetOverride(ctx, OverrideRequest{Action: ActionForceAllow}); err == nil {
		t.Error("SetOverride without indicator succeeded")
	}

	if res, err := client.DeleteOverride(ctx, "185.0.0.1"); err != nil || res.Override != nil || res.Decision == nil {
		t.Errorf("DeleteOverride = %+v, %v", res, err)
	}
	if _, err := client.DeleteOverride(ctx, "185.0.0.1"); !errors.Is(err, ErrNoOverride) {
		t.Errorf("second DeleteOverride = %v", err)
	}
	if _, err := client.DeleteOverride(ctx, "nope"); err == nil || errors.Is(err, ErrNoOverride) {
		t.Errorf("DeleteOverride(bad) = %v", err)
	}
	if _, err := client.DeleteOverride(ctx, " "); err == nil {
		t.Error("DeleteOverride without indicator succeeded")
	}
	if _, err := NewClient(path + ".missing").Overrides(ctx); !errors.Is(err, ErrDaemonNotRunning) {
		t.Errorf("Overrides without daemon = %v", err)
	}
}
