package cli

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// memOverrides is an in-memory admin.Overrides.
type memOverrides struct {
	mu   sync.Mutex
	list map[string]admin.OverrideResponse
}

func (m *memOverrides) List() ([]admin.OverrideResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []admin.OverrideResponse
	for _, o := range m.list {
		out = append(out, o)
	}
	return out, nil
}

func (m *memOverrides) Set(_ context.Context, ind obieproto.Indicator, action string, ttl time.Duration, note string) (admin.OverrideResponse, error) {
	o := admin.OverrideResponse{Indicator: ind, Action: action, Note: note, CreatedAt: explainAt}
	if ttl > 0 {
		end := explainAt.Add(ttl)
		o.ExpiresAt = &end
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.list[ind.Key()] = o
	return o, nil
}

func (m *memOverrides) get(key string) (admin.OverrideResponse, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.list[key]
	return o, ok
}

func (m *memOverrides) Delete(_ context.Context, ind obieproto.Indicator) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.list[ind.Key()]
	delete(m.list, ind.Key())
	return ok, nil
}

// startFakeAdmin serves the admin API with in-memory overrides; every
// indicator explains as allowed by the built-in list if it is private, else
// as the override says.
func startFakeAdmin(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "obie")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "obie.sock")
	overrides := &memOverrides{list: map[string]admin.OverrideResponse{}}
	info := admin.Info{
		Mode:      func() string { return "observe" },
		Status:    func() []lifecycle.Status { return nil },
		Overrides: overrides,
		Explain: func(ind obieproto.Indicator) (admin.DecisionResponse, error) {
			d := admin.DecisionResponse{Indicator: ind, State: admin.StateNone, Reason: "no active verdicts"}
			if strings.HasPrefix(ind.Value, "10.") {
				d.State, d.Reason = admin.StateAllowed, "allow-listed: built-in range 10.0.0.0/8 (private (RFC 1918)); verdicts: no active verdicts"
			} else if o, ok := overrides.get(ind.Key()); ok && o.Action == admin.ActionForceBlock {
				d.State, d.Reason = admin.StateBlock, "operator force-block override on "+ind.Key()+"; verdicts: no active verdicts"
			}
			return d, nil
		},
	}
	s := admin.New(socket, "obie-no-such-group", info, slog.New(slog.DiscardHandler))
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	return socket
}

func TestOverrideCommands(t *testing.T) {
	socket := startFakeAdmin(t)
	ctl := func(wantCode int, args ...string) (string, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := RunCtl(append([]string{"--socket", socket}, args...), &stdout, &stderr); code != wantCode {
			t.Fatalf("obiectl %v: exit code = %d, want %d; stderr %q", args, code, wantCode, stderr.String())
		}
		return stdout.String(), stderr.String()
	}

	out, stderr := ctl(ExitOK, "block", "185.0.0.1", "--ttl", "2h", "--note", "ssh abuse")
	want := "Override set: force_block on ipv4:185.0.0.1, until 2026-09-28T14:00:00Z (note: \"ssh abuse\").\n" +
		"Decision now: block — operator force-block override on ipv4:185.0.0.1; verdicts: no active verdicts\n"
	if out != want || stderr != "" {
		t.Errorf("block:\n%s%s\nwant\n%s", out, stderr, want)
	}
	// A force-block on a protected address is stored, with a warning.
	out, stderr = ctl(ExitOK, "block", "--note", "oops", "10.0.0.5")
	if !strings.Contains(out, "Decision now: allowed") ||
		!strings.HasPrefix(stderr, "obiectl block: warning: the force-block does not take effect: allow-listed: built-in range 10.0.0.0/8 (private (RFC 1918)); verdicts: no active verdicts\n") ||
		!strings.Contains(stderr, "\n  Why:  protected addresses") ||
		!strings.HasSuffix(stderr, "\n  Next: the override is kept but has no effect; remove it: sudo obiectl unoverride 10.0.0.5\n") {
		t.Errorf("block of a private address:\n%s%s", out, stderr)
	}
	out, _ = ctl(ExitOK, "allow", "185.0.0.0/24")
	if !strings.HasPrefix(out, "Override set: force_allow on cidr:185.0.0.0/24, until removed.\n") {
		t.Errorf("allow:\n%s", out)
	}
	out, _ = ctl(ExitOK, "allow", "--json", "2a01::1")
	if !strings.Contains(out, `"action": "force_allow"`) {
		t.Errorf("allow --json:\n%s", out)
	}

	out, _ = ctl(ExitOK, "overrides")
	for _, want := range []string{"INDICATOR          ACTION       EXPIRES               CREATED               NOTE\n",
		"ipv4:185.0.0.1     force_block  2026-09-28T14:00:00Z  2026-09-28T12:00:00Z  ssh abuse\n",
		"cidr:185.0.0.0/24  force_allow  never                 2026-09-28T12:00:00Z  -\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("overrides lacks %q:\n%s", want, out)
		}
	}
	if out, _ = ctl(ExitOK, "overrides", "--json"); !strings.Contains(out, `"overrides": [`) {
		t.Errorf("overrides --json:\n%s", out)
	}

	out, _ = ctl(ExitOK, "unoverride", "185.0.0.1")
	if out != "Override on 185.0.0.1 removed.\nDecision now: none — no active verdicts\n" {
		t.Errorf("unoverride:\n%s", out)
	}
	_, stderr = ctl(ExitFailure, "unoverride", "185.0.0.1")
	if !strings.Contains(stderr, "no override on 185.0.0.1") {
		t.Errorf("second unoverride: %q", stderr)
	}
	if out, _ = ctl(ExitOK, "unoverride", "--json", "10.0.0.5"); !strings.Contains(out, `"state": "allowed"`) {
		t.Errorf("unoverride --json:\n%s", out)
	}
	_, stderr = ctl(ExitUsage, "block", "example.org")
	if !strings.Contains(stderr, `"example.org" is not an IP address or range`) || !strings.Contains(stderr, "Next: give an IPv4 or IPv6 address") {
		t.Errorf("block of a name: %q", stderr)
	}
}

func TestOverrideUsage(t *testing.T) {
	for name, tc := range map[string]struct {
		args   []string
		code   int
		stderr string
	}{
		"allow without argument":  {[]string{"allow"}, ExitUsage, "missing the address or range"},
		"block with two":          {[]string{"block", "185.0.0.1", "185.0.0.2"}, ExitUsage, "expects one address or range, got 2 arguments"},
		"block bad ttl":           {[]string{"block", "185.0.0.1", "--ttl", "soon"}, ExitUsage, `invalid --ttl "soon"`},
		"block zero ttl":          {[]string{"block", "185.0.0.1", "--ttl", "0s"}, ExitUsage, "positive duration"},
		"block unknown flag":      {[]string{"block", "185.0.0.1", "--mode", "enforce"}, ExitUsage, "unknown flag --mode"},
		"overrides argument":      {[]string{"overrides", "all"}, ExitUsage, `unexpected argument "all"`},
		"allow not running":       {[]string{"allow", "185.0.0.1"}, ExitFailure, "obied is not running"},
		"overrides not running":   {[]string{"overrides"}, ExitFailure, "obied is not running"},
		"unoverride not running":  {[]string{"unoverride", "185.0.0.1"}, ExitFailure, "obied is not running"},
		"unoverride two":          {[]string{"unoverride", "a", "b"}, ExitUsage, "expects one address or range, got 2 arguments: a b"},
		"unoverride unknown flag": {[]string{"unoverride", "--ttl", "1h", "185.0.0.1"}, ExitUsage, "unknown flag --ttl"},
	} {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			args := append([]string{"--socket", "/nonexistent/obie.sock"}, tc.args...)
			if code := RunCtl(args, &stdout, &stderr); code != tc.code || !strings.Contains(stderr.String(), tc.stderr) {
				t.Errorf("exit code = %d, stderr %q; want %d containing %q", code, stderr.String(), tc.code, tc.stderr)
			}
		})
	}
}

func TestOverrideWriteErrors(t *testing.T) {
	socket := startFakeAdmin(t)
	for _, args := range [][]string{{"allow", "185.0.0.9"}, {"overrides"}, {"unoverride", "185.0.0.9"}} {
		var stderr bytes.Buffer
		if code := RunCtl(append([]string{"--socket", socket}, args...), failingWriter{}, &stderr); code != ExitIOError {
			t.Errorf("%v: exit code = %d, stderr %q", args, code, stderr.String())
		}
	}
	var out bytes.Buffer
	if err := writeOverridesTable(&out, nil); err != nil || out.String() != "No overrides.\n" {
		t.Errorf("empty table: %q, %v", out.String(), err)
	}
}

func TestSovereigntyText(t *testing.T) {
	until := explainAt.Add(time.Hour)
	d := explained
	d.State, d.ExpiresAt = admin.StateAllowed, nil
	d.Reason = "operator force-allow override on cidr:203.0.113.0/24; verdicts: consensus"
	d.Sovereignty = &admin.SovereigntyResponse{Applied: true, Effect: "allow", Rule: "force_allow", Source: "override",
		Match: "cidr:203.0.113.0/24", OverrideNote: "partner", ExpiresAt: &until, Note: "operator force-allow override on cidr:203.0.113.0/24"}
	var out bytes.Buffer
	if err := writeExplanation(&out, &d); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Decision:              allowed\n",
		"Allow-list/overrides:  operator force-allow override on cidr:203.0.113.0/24\n", "Override note:         partner\n"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("explanation lacks %q:\n%s", want, out.String())
		}
	}
	if got := sovereigntyText(&admin.SovereigntyResponse{Applied: true, Note: "no allow-list entry or override applies"}); got != "none apply" {
		t.Errorf("no rule: %q", got)
	}
	if got := sovereigntyText(&admin.SovereigntyResponse{Note: "old daemon"}); got != "not applied (old daemon)" {
		t.Errorf("not applied: %q", got)
	}
}
