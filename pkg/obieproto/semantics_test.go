package obieproto

import (
	"testing"
	"time"
)

func TestSupersedes(t *testing.T) {
	later := func(e *Event) { e.IssuedAt = NewTimestamp(testNow.Add(time.Second)) }
	tests := []struct {
		name   string
		mutate func(a *Event)
		want   bool
	}{
		{name: "issued later", mutate: later, want: true},
		{name: "issued earlier", mutate: func(a *Event) { a.IssuedAt = NewTimestamp(testNow.Add(-time.Second)) }},
		{name: "same second, greater id", mutate: func(a *Event) { a.ID = testOtherID }, want: true},
		{name: "same second, smaller id", mutate: func(a *Event) { a.ID = "01923e4a-7b2c-7def-8a12-3456789abcdd" }},
		{name: "same event", mutate: func(*Event) {}},
		{name: "later, lower confidence", mutate: func(a *Event) { later(a); a.Verdict.Confidence = 0.1 }, want: true},
		{name: "other publisher", mutate: func(a *Event) { later(a); a.Publisher.PeerID = testPeerIDB }},
		{name: "other indicator", mutate: func(a *Event) { later(a); a.Indicator.Value = "85.10.20.31" }},
		{name: "revocation", mutate: func(a *Event) { later(a); a.Type = TypeRevoke }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, b := validVerdict(), validVerdict()
			tt.mutate(a)
			if got := a.Supersedes(b); got != tt.want {
				t.Errorf("Supersedes() = %v, want %v", got, tt.want)
			}
			if tt.want && b.Supersedes(a) {
				t.Error("Supersedes() holds in both directions")
			}
		})
	}
	var nilEvent *Event
	if nilEvent.Supersedes(validVerdict()) || validVerdict().Supersedes(nil) {
		t.Error("Supersedes() with nil = true")
	}
}

func TestWithdraws(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(r, v *Event)
		want   bool
	}{
		{name: "own verdict", mutate: func(*Event, *Event) {}, want: true},
		{name: "other verdict id", mutate: func(r, _ *Event) { r.Revokes = "01923e4a-7b2c-7def-8a12-3456789abcdd" }},
		{name: "other publisher", mutate: func(r, _ *Event) { r.Publisher.PeerID = testPeerIDB }},
		{name: "other indicator", mutate: func(r, _ *Event) { r.Indicator.Value = "85.10.20.31" }},
		{name: "target is a revocation", mutate: func(_, v *Event) { v.Type = TypeRevoke }},
		{name: "revoker is a verdict", mutate: func(r, _ *Event) { r.Type = TypeVerdict }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, v := validRevoke(), validVerdict()
			tt.mutate(r, v)
			if got := r.Withdraws(v); got != tt.want {
				t.Errorf("Withdraws() = %v, want %v", got, tt.want)
			}
		})
	}
	var nilEvent *Event
	if nilEvent.Withdraws(validVerdict()) || validRevoke().Withdraws(nil) {
		t.Error("Withdraws() with nil = true")
	}
}
