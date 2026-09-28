package daemon

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/audit"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/console"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// actions returns the actions of entries, in their order.
func actions(entries []console.ActivityEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Action
	}
	return out
}

// TestAuditReloads: a reload is recorded with the settings it changed and
// those waiting for a restart, a mode switch as a mode change; a rejected
// reload is not recorded.
func TestAuditReloads(t *testing.T) {
	f := newReloadFixture(t)
	log := newAuditLog("", func() config.Mode { return f.gate.Mode() }, slog.New(slog.DiscardHandler))
	f.rl.audit = log
	activity := consoleActivity{log: log}

	f.next.Node.Mode = config.ModeEnforce
	f.next.Decision.Threshold = 2.5
	f.next.Log.Level = "debug"
	if err := f.rl.reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.err = errors.New("broken file")
	if err := f.rl.reload(context.Background()); err == nil {
		t.Fatal("reload with a broken file succeeded")
	}
	f.err = nil
	if err := f.rl.reload(context.Background()); err != nil {
		t.Fatal(err)
	}

	p, err := activity.Timeline(console.ActivityFilter{}, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := actions(p.Entries), []string{"config-reloaded", "config-reloaded", "mode-changed"}; !slices.Equal(got, want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	unchanged, reload, mode := p.Entries[0], p.Entries[1], p.Entries[2]
	if len(unchanged.Settings) != 0 || !slices.Equal(unchanged.RestartSettings, []string{"log.level"}) ||
		unchanged.Reason != "configuration reloaded: no setting changed; log.level waits for a restart" {
		t.Errorf("second reload = %+v", unchanged)
	}
	if !slices.Equal(reload.Settings, []string{"node.mode", "decision.threshold"}) || reload.Mode != "enforce" {
		t.Errorf("first reload = %+v", reload)
	}
	if mode.PreviousMode != "observe" || mode.Mode != "enforce" || mode.Range.IsValid() {
		t.Errorf("mode change = %+v", mode)
	}
}

// TestConsoleActivityTimeline: the timeline reads the audit log file,
// converted for the console, filtered by action and range, continues
// where a page ended, and the live feed continues after the page.
func TestConsoleActivityTimeline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	log := newAuditLog(path, func() config.Mode { return config.ModeEnforce }, slog.New(slog.DiscardHandler))
	if err := log.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Stop(context.Background()) })
	activity := consoleActivity{log: log}
	expires := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	log.Write(audit.OverrideSet(&store.Override{Indicator: ipv4Indicator("198.18.0.1"), Action: store.ForceBlock,
		Note: "scanner", ExpiresAt: expires}))
	log.Write(audit.OverrideRemoved(obieproto.Indicator{Kind: obieproto.KindCIDR, Value: "198.18.4.0/24", Scope: "/24"},
		store.ForceAllow))
	log.Write(audit.PeerConnection(audit.Peer{ID: "12D3KooWB", Name: "beta"}, true))

	p, err := activity.Timeline(console.ActivityFilter{}, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if p.Path != path || p.Memory || p.FileErr != "" || p.Live != 3 || p.Kept != audit.MemoryEntries || p.Older == "" {
		t.Fatalf("page = %+v", p)
	}
	if got := actions(p.Entries); !slices.Equal(got, []string{"peer-connected", "override-removed"}) {
		t.Fatalf("first page = %v", got)
	}
	peer, removed := p.Entries[0], p.Entries[1]
	if peer.PeerID != "12D3KooWB" || peer.PeerName != "beta" || peer.Range.IsValid() || peer.Time.IsZero() {
		t.Errorf("peer entry = %+v", peer)
	}
	if removed.Range != netip.MustParsePrefix("198.18.4.0/24") || removed.Rule != "force_allow" {
		t.Errorf("removed override = %+v", removed)
	}
	older, err := activity.Timeline(console.ActivityFilter{}, p.Older, 2)
	if err != nil || len(older.Entries) != 1 || older.Older != "" {
		t.Fatalf("older page = %+v, %v", older, err)
	}
	if set := older.Entries[0]; set.Range != netip.MustParsePrefix("198.18.0.1/32") || set.Note != "scanner" ||
		!set.ExpiresAt.Equal(expires) || set.Mode != "enforce" {
		t.Errorf("override set = %+v", set)
	}

	for _, tc := range []struct {
		name string
		f    console.ActivityFilter
		want []string
	}{
		{"action", console.ActivityFilter{Actions: []string{"override-set", "override-removed"}}, []string{"override-removed", "override-set"}},
		{"address in a network", console.ActivityFilter{Range: netip.MustParsePrefix("198.18.4.7/32")}, []string{"override-removed"}},
		{"network around an address", console.ActivityFilter{Range: netip.MustParsePrefix("198.18.0.0/16")}, []string{"override-removed", "override-set"}},
		{"both", console.ActivityFilter{Actions: []string{"override-set"}, Range: netip.MustParsePrefix("198.18.4.0/24")}, nil},
	} {
		p, err := activity.Timeline(tc.f, "", 10)
		if got := actions(p.Entries); err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v, %v; want %v", tc.name, got, err, tc.want)
		}
	}

	if _, err := activity.Timeline(console.ActivityFilter{}, "nonsense", 10); err == nil {
		t.Error("a bad position was accepted")
	}

	for range 3 {
		log.Write(audit.PeerConnection(audit.Peer{ID: "12D3KooWB"}, false))
	}
	log.Write(audit.ModeChanged("enforce", "observe"))
	b := activity.Live(console.ActivityFilter{}, p.Live, 2)
	if got := actions(b.Entries); !slices.Equal(got, []string{"mode-changed", "peer-disconnected"}) || b.Next != 7 ||
		b.More["peer-disconnected"] != 2 || len(b.More) != 1 || b.From.IsZero() {
		t.Errorf("live = %v, next %d, more %v, from %v", got, b.Next, b.More, b.From)
	}
	if b := activity.Live(console.ActivityFilter{Actions: []string{"mode-changed"}}, p.Live, 2); len(b.Entries) != 1 || b.More != nil {
		t.Errorf("filtered live = %+v", b)
	}
}

// TestConsoleActivityWithoutFile: without audit.path the timeline comes
// from memory and names no file error; a file obied cannot read names one.
func TestConsoleActivityWithoutFile(t *testing.T) {
	log := newAuditLog("", func() config.Mode { return config.ModeObserve }, slog.New(slog.DiscardHandler))
	log.Write(audit.ModeChanged("enforce", "observe"))
	p, err := consoleActivity{log: log}.Timeline(console.ActivityFilter{}, "", 10)
	if err != nil || !p.Memory || p.FileErr != "" || p.Path != "" || len(p.Entries) != 1 {
		t.Errorf("page = %+v, %v", p, err)
	}

	stopped := newAuditLog(filepath.Join(t.TempDir(), "audit.jsonl"), func() config.Mode { return config.ModeObserve },
		slog.New(slog.DiscardHandler))
	p, err = consoleActivity{log: stopped}.Timeline(console.ActivityFilter{}, "", 10)
	if err != nil || !p.Memory || !strings.Contains(p.FileErr, "not open") {
		t.Errorf("page of a closed file = %+v, %v", p, err)
	}
}

func ipv4Indicator(value string) obieproto.Indicator {
	return obieproto.Indicator{Kind: obieproto.KindIPv4, Value: value, Scope: "/32"}
}
