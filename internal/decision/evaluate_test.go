package decision

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

const (
	self = "12D3KooWSelfSelfSelfSelfSelfSelfSelfSelfSelfSelfSe"
	pubA = "12D3KooWPublisherAaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	pubB = "12D3KooWPublisherBbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	pubC = "12D3KooWPublisherCccccccccccccccccccccccccccccccc"
	pubD = "12D3KooWPublisherDddddddddddddddddddddddddddddddd"
	// unlisted is a publisher not in trust.publishers.
	unlisted = "12D3KooWUnlistedUuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuu"
)

var t0 = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

var idCounter atomic.Uint64

// newID returns a unique, increasing UUIDv7-shaped event ID.
func newID() string {
	return fmt.Sprintf("01900000-0000-7000-8000-%012x", idCounter.Add(1))
}

func ipv4(value string) obieproto.Indicator {
	return obieproto.Indicator{Kind: obieproto.KindIPv4, Value: value, Scope: "/32"}
}

var target = ipv4("203.0.113.7")

// verdict is a verdict by publisher on target, issued at issued.
func verdict(publisher, action string, confidence float64, issued time.Time, ttl time.Duration) *obieproto.Event {
	return verdictOn(target, publisher, action, confidence, issued, ttl)
}

func verdictOn(ind obieproto.Indicator, publisher, action string, confidence float64, issued time.Time, ttl time.Duration) *obieproto.Event {
	return &obieproto.Event{
		ID:        newID(),
		Spec:      obieproto.Spec,
		Type:      obieproto.TypeVerdict,
		IssuedAt:  obieproto.NewTimestamp(issued),
		Indicator: ind,
		Protocol:  "ssh",
		Evidence:  &obieproto.Evidence{Events: 10, Reason: "password_bruteforce"},
		Verdict:   &obieproto.Verdict{SuggestedAction: action, Confidence: confidence, TTLSeconds: int64(ttl / time.Second)},
		Publisher: obieproto.Publisher{PeerID: publisher},
	}
}

func ban(publisher string, confidence float64) *obieproto.Event {
	return verdict(publisher, obieproto.ActionBan, confidence, t0.Add(-time.Hour), 24*time.Hour)
}

func watch(publisher string, confidence float64) *obieproto.Event {
	return verdict(publisher, obieproto.ActionWatch, confidence, t0.Add(-time.Hour), 24*time.Hour)
}

// testPolicy is the default configuration of node self with publishers A, B
// and C at weight 1, D at weight 0.
func testPolicy() Policy {
	cfg := config.Default()
	cfg.Trust.Publishers = []config.Publisher{
		{PeerID: pubA, Name: "alpha", Weight: 1},
		{PeerID: pubB, Name: "bravo", Weight: 1},
		{PeerID: pubC, Name: "charlie", Weight: 1},
		{PeerID: pubD, Name: "delta", Weight: 0},
	}
	return NewPolicy(self, cfg.Trust, cfg.Decision)
}

func TestEvaluate(t *testing.T) {
	type policyFn func(*Policy)
	withWeights := func(w map[string]float64) policyFn {
		return func(p *Policy) {
			for id, weight := range w {
				p.Weights[id] = weight
			}
		}
	}
	tests := []struct {
		name     string
		policy   policyFn
		verdicts []*obieproto.Event
		state    State
		score    float64
		count    int
		auto     bool
	}{
		// Single peer never blocks under the defaults.
		{"no verdicts", nil, nil, StateNone, 0, 0, false},
		{"single fully trusted peer", nil, []*obieproto.Event{ban(pubA, 1)}, StateNone, 1, 1, false},
		{"single unlisted peer", nil, []*obieproto.Event{ban(unlisted, 1)}, StateNone, 0, 0, false},
		{"duplicate verdicts of one peer count once", withWeights(map[string]float64{pubA: 1}),
			[]*obieproto.Event{ban(pubA, 1), ban(pubA, 1)}, StateNone, 1, 1, false},
		{"two unlisted peers under default weight 0", nil, []*obieproto.Event{ban(unlisted, 1), ban(pubD, 1)}, StateNone, 0, 0, false},

		// Threshold boundary (quorum 2 met).
		{"threshold reached exactly", nil, []*obieproto.Event{ban(pubA, 0.9), ban(pubB, 0.9)}, StateBlock, 1.8, 2, false},
		{"threshold reached despite float rounding", func(p *Policy) { p.Threshold = 1.8; p.Quorum = 3 },
			[]*obieproto.Event{ban(pubA, 0.6), ban(pubB, 0.6), ban(pubC, 0.6)}, StateBlock, 1.8, 3, false},
		{"threshold missed by 0.01", nil, []*obieproto.Event{ban(pubA, 0.9), ban(pubB, 0.89)}, StateNone, 1.79, 2, false},
		{"threshold missed with weights", withWeights(map[string]float64{pubA: 0.5}),
			[]*obieproto.Event{ban(pubA, 1), ban(pubB, 1)}, StateNone, 1.5, 2, false},

		// Quorum boundary (threshold met).
		{"quorum 3 missed", func(p *Policy) { p.Quorum = 3; p.Threshold = 1 },
			[]*obieproto.Event{ban(pubA, 1), ban(pubB, 1)}, StateNone, 2, 2, false},
		{"quorum 3 reached", func(p *Policy) { p.Quorum = 3; p.Threshold = 1 },
			[]*obieproto.Event{ban(pubA, 1), ban(pubB, 1), ban(pubC, 1)}, StateBlock, 3, 3, false},
		{"quorum 1", func(p *Policy) { p.Quorum = 1; p.Threshold = 0.5 },
			[]*obieproto.Event{ban(pubA, 0.5)}, StateBlock, 0.5, 1, false},

		// Zero-weight publishers never count, not even towards the quorum.
		{"zero-weight publisher does not reach quorum", func(p *Policy) { p.Threshold = 1 },
			[]*obieproto.Event{ban(pubA, 1), ban(pubD, 1)}, StateNone, 1, 1, false},
		{"zero-weight publishers ignored with many", func(p *Policy) { p.Threshold = 0.1 },
			[]*obieproto.Event{ban(pubD, 1), ban(unlisted, 1), ban(pubA, 1)}, StateNone, 1, 1, false},
		{"default weight counts unlisted peers", func(p *Policy) { p.DefaultWeight = 0.9 },
			[]*obieproto.Event{ban(unlisted, 1), ban(pubA, 0.9)}, StateBlock, 1.8, 2, false},

		// Watch verdicts are reported only.
		{"watch verdicts never contribute", nil, []*obieproto.Event{watch(pubA, 1), watch(pubB, 1), watch(pubC, 1)}, StateNone, 0, 0, false},
		{"watch does not complete quorum", nil, []*obieproto.Event{ban(pubA, 1), watch(pubB, 1)}, StateNone, 1, 1, false},
		{"local watch does not autoblock", nil, []*obieproto.Event{watch(self, 1)}, StateNone, 0, 0, false},

		// Expired verdicts do not contribute.
		{"expired verdict ignored", nil, []*obieproto.Event{ban(pubA, 1),
			verdict(pubB, obieproto.ActionBan, 1, t0.Add(-2*time.Hour), time.Hour)}, StateNone, 1, 1, false},
		{"verdict expiring exactly now ignored", nil, []*obieproto.Event{ban(pubA, 1),
			verdict(pubB, obieproto.ActionBan, 1, t0.Add(-time.Hour), time.Hour)}, StateNone, 1, 1, false},

		// Local autoblock.
		{"local autoblock on", nil, []*obieproto.Event{ban(self, 0.3)}, StateBlock, 0.3, 1, true},
		{"local autoblock off", func(p *Policy) { p.LocalAutoblock = false }, []*obieproto.Event{ban(self, 1)}, StateNone, 1, 1, false},
		{"local autoblock off, local counts towards consensus", func(p *Policy) { p.LocalAutoblock = false },
			[]*obieproto.Event{ban(self, 0.9), ban(pubA, 0.9)}, StateBlock, 1.8, 2, false},
		{"local autoblock needs local weight > 0", func(p *Policy) { p.LocalWeight = 0 },
			[]*obieproto.Event{ban(self, 1)}, StateNone, 0, 0, false},
		{"consensus wins over autoblock", nil, []*obieproto.Event{ban(self, 1), ban(pubA, 1)}, StateBlock, 2, 2, false},
		{"local weight overrides a listing of self", withWeights(map[string]float64{self: 0}),
			[]*obieproto.Event{ban(self, 1), ban(pubA, 0.8)}, StateBlock, 1.8, 2, false},

		// Only the newest verdict of a publisher counts.
		{"one verdict per publisher", nil, []*obieproto.Event{ban(pubA, 1),
			verdict(pubA, obieproto.ActionBan, 1, t0.Add(-30*time.Minute), time.Hour)}, StateNone, 1, 1, false},
		{"newer watch replaces ban", nil, []*obieproto.Event{ban(pubA, 1), ban(pubB, 1),
			verdict(pubA, obieproto.ActionWatch, 1, t0.Add(-30*time.Minute), time.Hour)}, StateNone, 1, 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := testPolicy()
			if tt.policy != nil {
				tt.policy(&p)
			}
			d := Evaluate(target, tt.verdicts, p, t0)
			if d.State != tt.state || !closeTo(d.Score, tt.score) || d.Contributors != tt.count || d.Autoblock != tt.auto {
				t.Errorf("Evaluate = %s score %v count %d autoblock %v (%s), want %s score %v count %d autoblock %v",
					d.State, d.Score, d.Contributors, d.Autoblock, d.Reason, tt.state, tt.score, tt.count, tt.auto)
			}
			if (d.State == StateBlock) == d.ExpiresAt.IsZero() {
				t.Errorf("ExpiresAt = %v for state %s", d.ExpiresAt, d.State)
			}
			if d.Threshold != p.Threshold || d.Quorum != p.Quorum || !d.EvaluatedAt.Equal(t0) || d.Reason == "" {
				t.Errorf("decision = %+v", d)
			}
		})
	}
}

func closeTo(a, b float64) bool {
	const eps = 1e-9
	return a-b < eps && b-a < eps
}

func TestEvaluateExpiry(t *testing.T) {
	issued := t0.Add(-time.Hour)
	tests := []struct {
		name     string
		maxTTL   time.Duration
		verdicts []*obieproto.Event
		want     time.Time
	}{
		{"latest contributing expiry", 30 * 24 * time.Hour, []*obieproto.Event{
			verdict(pubA, obieproto.ActionBan, 1, issued, 2*time.Hour),
			verdict(pubB, obieproto.ActionBan, 1, issued, 5*time.Hour),
		}, issued.Add(5 * time.Hour)},
		{"non-contributing verdicts do not extend", 30 * 24 * time.Hour, []*obieproto.Event{
			verdict(pubA, obieproto.ActionBan, 1, issued, 2*time.Hour),
			verdict(pubB, obieproto.ActionBan, 1, issued, 3*time.Hour),
			verdict(pubC, obieproto.ActionWatch, 1, issued, 20*time.Hour),
			verdict(pubD, obieproto.ActionBan, 1, issued, 20*time.Hour),
		}, issued.Add(3 * time.Hour)},
		{"capped at max_ttl from now", 90 * time.Minute, []*obieproto.Event{
			verdict(pubA, obieproto.ActionBan, 1, issued, 2*time.Hour),
			verdict(pubB, obieproto.ActionBan, 1, issued, 5*time.Hour),
		}, t0.Add(90 * time.Minute)},
		{"autoblock uses the local verdict", 30 * 24 * time.Hour, []*obieproto.Event{
			verdict(self, obieproto.ActionBan, 1, issued, 2*time.Hour),
			verdict(pubA, obieproto.ActionBan, 0.1, issued, 10*time.Hour),
		}, issued.Add(2 * time.Hour)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := testPolicy()
			p.MaxTTL = tt.maxTTL
			if tt.name != "autoblock uses the local verdict" {
				p.Threshold = 1
			}
			d := Evaluate(target, tt.verdicts, p, t0)
			if d.State != StateBlock || !d.ExpiresAt.Equal(tt.want) {
				t.Errorf("Evaluate = %s until %v (%s), want block until %v", d.State, d.ExpiresAt, d.Reason, tt.want)
			}
		})
	}
}

func TestEvaluateExplains(t *testing.T) {
	p := testPolicy()
	a := verdict(pubA, obieproto.ActionBan, 0.9, t0.Add(-time.Hour), 2*time.Hour)
	b := watch(pubB, 0.5)
	d := Evaluate(target, []*obieproto.Event{b, ban(pubD, 1), a}, p, t0)
	if len(d.Publishers) != 3 {
		t.Fatalf("publishers = %+v", d.Publishers)
	}
	want := Contribution{
		PeerID: pubA, Name: "alpha", EventID: a.ID, Action: obieproto.ActionBan, Weight: 1, Confidence: 0.9, Score: 0.9,
		IssuedAt: t0.Add(-time.Hour), ExpiresAt: t0.Add(time.Hour), Contributes: true,
	}
	if !reflect.DeepEqual(d.Publishers[0], want) {
		t.Errorf("publisher A = %+v, want %+v", d.Publishers[0], want)
	}
	if c := d.Publishers[1]; c.PeerID != pubB || c.Contributes || c.Score != 0 || c.Action != obieproto.ActionWatch {
		t.Errorf("watch publisher = %+v", c)
	}
	if c := d.Publishers[2]; c.PeerID != pubD || c.Contributes || c.Weight != 0 || c.Name != "delta" {
		t.Errorf("zero-weight publisher = %+v", c)
	}
	if want := "below consensus: score 0.9 < threshold 1.8, 1 < quorum 2"; d.Reason != want {
		t.Errorf("reason = %q, want %q", d.Reason, want)
	}

	local := Evaluate(target, []*obieproto.Event{ban(self, 1)}, p, t0)
	if !local.Publishers[0].Local || !strings.HasPrefix(local.Reason, "local autoblock") {
		t.Errorf("local decision = %+v", local)
	}
	if got := Evaluate(target, nil, p, t0).Reason; got != "no active verdicts" {
		t.Errorf("reason without verdicts = %q", got)
	}
}

// TestEvaluateDeterministic checks that the order of the verdicts does not
// change the decision.
func TestEvaluateDeterministic(t *testing.T) {
	p := testPolicy()
	p.Quorum = 3
	verdicts := []*obieproto.Event{ban(pubA, 0.6), ban(pubB, 0.6), ban(pubC, 0.6), watch(self, 1), ban(pubD, 1)}
	want := Evaluate(target, verdicts, p, t0)
	for range 50 {
		shuffled := append([]*obieproto.Event(nil), verdicts...)
		rand.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		if got := Evaluate(target, shuffled, p, t0); !reflect.DeepEqual(got, want) {
			t.Fatalf("Evaluate depends on order:\n%+v\nwant\n%+v", got, want)
		}
	}
}

func TestNewPolicy(t *testing.T) {
	cfg := config.Default()
	cfg.Trust.Publishers = []config.Publisher{{PeerID: pubA, Name: "alpha", Weight: 0.7}}
	cfg.Trust.DefaultWeight = 0.1
	cfg.Trust.LocalWeight = 0.9
	p := NewPolicy(self, cfg.Trust, cfg.Decision)
	for id, want := range map[string]float64{self: 0.9, pubA: 0.7, unlisted: 0.1} {
		if got := p.weight(id); got != want {
			t.Errorf("weight(%s) = %v, want %v", id, got, want)
		}
	}
	if p.Threshold != 1.8 || p.Quorum != 2 || p.MaxTTL != 30*24*time.Hour || !p.LocalAutoblock || p.Names[pubA] != "alpha" {
		t.Errorf("policy = %+v", p)
	}
}
