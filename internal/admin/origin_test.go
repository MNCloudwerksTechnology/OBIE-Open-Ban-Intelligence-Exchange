package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/audit"
	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/internal/verdicts"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// origins records the origin of every action it serves.
type origins struct {
	mu   sync.Mutex
	seen []audit.Origin
}

func (o *origins) add(ctx context.Context) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.seen = append(o.seen, audit.OriginOf(ctx))
}

func (o *origins) all() []audit.Origin {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]audit.Origin(nil), o.seen...)
}

// originOverrides is fakeOverrides recording the origins of changes.
type originOverrides struct {
	*fakeOverrides
	origins *origins
}

func (f originOverrides) Set(ctx context.Context, ind obieproto.Indicator, action string, ttl time.Duration, note string) (OverrideResponse, error) {
	f.origins.add(ctx)
	return f.fakeOverrides.Set(ctx, ind, action, ttl, note)
}

func (f originOverrides) Delete(ctx context.Context, ind obieproto.Indicator) (bool, error) {
	f.origins.add(ctx)
	return f.fakeOverrides.Delete(ctx, ind)
}

// originVerdicts reports and revokes without publishing, recording the
// origins.
type originVerdicts struct{ origins *origins }

func (originVerdicts) PeerID() string { return testIdentity.PeerID }

func (v originVerdicts) Report(ctx context.Context, r verdicts.Report) (verdicts.Result, error) {
	v.origins.add(ctx)
	return verdicts.Result{Event: &obieproto.Event{ID: "e1", Indicator: r.Indicator}}, nil
}

func (v originVerdicts) Revoke(ctx context.Context, _ verdicts.Revocation) ([]*obieproto.Event, error) {
	v.origins.add(ctx)
	return nil, nil
}

func (originVerdicts) List(string, store.Page) (store.IndicatorPage, error) {
	return store.IndicatorPage{}, nil
}

func (originVerdicts) Lookup(obieproto.Indicator) ([]*obieproto.Event, error) { return nil, nil }

// TestActionsCarryTheirOrigin: every operator action through the admin
// socket reaches its service with the origin admin-api and the UID of the
// connecting process, for the audit trail (ADR 0026).
func TestActionsCarryTheirOrigin(t *testing.T) {
	seen := &origins{}
	info := testInfo(lifecycle.Status{Name: "admin", State: lifecycle.StateRunning, Ready: true})
	info.Overrides = originOverrides{newFakeOverrides(), seen}
	info.Verdicts = originVerdicts{seen}
	path := socketPath(t)
	s := New(path, "obie-no-such-group", info, discardLogger())
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	client, ctx := NewClient(path), context.Background()
	if _, err := client.SetOverride(ctx, OverrideRequest{Indicator: "185.0.0.1", Action: ActionForceBlock}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.DeleteOverride(ctx, "185.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Report(ctx, ReportRequest{IP: "185.0.0.1", Protocol: "ssh", Reason: "password_bruteforce", Events: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Revoke(ctx, RevocationRequest{Indicator: "185.0.0.1", Reason: "false_positive"}); err != nil {
		t.Fatal(err)
	}

	uid := strconv.Itoa(os.Getuid())
	got := seen.all()
	if len(got) != 4 {
		t.Fatalf("origins = %+v, want 4", got)
	}
	for i, o := range got {
		if o.Via != audit.OriginAdminAPI || o.UserID != uid {
			t.Errorf("origin %d = %+v, want %s by uid %s", i, o, audit.OriginAdminAPI, uid)
		}
	}
}

// TestOriginWithoutPeerCredentials: a request whose peer is not known
// still names the admin API, without a user.
func TestOriginWithoutPeerCredentials(t *testing.T) {
	seen := &origins{}
	info := testInfo()
	info.Overrides = originOverrides{newFakeOverrides(), seen}
	rec := httptest.NewRecorder()
	Handler(info, discardLogger()).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, OverridesPath,
		strings.NewReader(`{"indicator":"185.0.0.1","action":"force_allow"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body)
	}
	if got := seen.all(); len(got) != 1 || got[0] != (audit.Origin{Via: audit.OriginAdminAPI}) {
		t.Errorf("origins = %+v", got)
	}
}
