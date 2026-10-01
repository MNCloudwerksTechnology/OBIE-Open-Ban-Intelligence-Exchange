package sovereignty

import (
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

func indicator(t *testing.T, s string) obieproto.Indicator {
	t.Helper()
	p := netip.MustParsePrefix(s)
	ind := obieproto.Indicator{Kind: obieproto.KindCIDR, Value: p.String()}
	if p.IsSingleIP() {
		ind = obieproto.Indicator{Kind: obieproto.KindIPv4, Value: p.Addr().String()}
		if p.Addr().Is6() {
			ind.Kind = obieproto.KindIPv6
		}
	}
	if err := ind.Normalize(); err != nil {
		t.Fatal(err)
	}
	return ind
}

var now = time.Date(2029, 1, 1, 0, 0, 0, 0, time.UTC)

func override(t *testing.T, s string, action store.Action) store.Override {
	return store.Override{Indicator: indicator(t, s), Action: action, Note: "n"}
}

// TestJudgePrecedence is the override precedence matrix: for an address
// covered by each combination of a protected entry, an operator entry, a
// force-allow and a force-block override, the first rule in the order
// force-allow > protected > force-block > operator allow-list > none wins.
func TestJudgePrecedence(t *testing.T) {
	const (
		protectedAddr = "10.1.2.3/32"   // built-in
		selfAddr      = "185.0.0.1/32"  // own address
		bootAddr      = "185.0.0.2/32"  // bootstrap peer
		operatorAddr  = "198.18.0.5/32" // allowlist.cidrs
		publicAddr    = "185.9.9.9/32"  // nothing
	)
	allow := NewAllowlist(append(Builtin(),
		Entry{Prefix: netip.MustParsePrefix(selfAddr), Source: SourceSelf},
		Entry{Prefix: netip.MustParsePrefix(bootAddr), Source: SourceBootstrap},
		Entry{Prefix: netip.MustParsePrefix("198.18.0.0/24"), Source: SourceConfig},
	)...)
	type want struct {
		effect Effect
		rule   Rule
		source Source
	}
	var (
		forceAllow = want{EffectAllow, RuleForceAllow, SourceOverride}
		forceBlock = want{EffectBlock, RuleForceBlock, SourceOverride}
		none       = want{}
	)
	listed := func(src Source) want { return want{EffectAllow, RuleAllowlist, src} }
	tests := []struct {
		name  string
		addr  string
		allow bool // force-allow override on the address
		block bool // force-block override on the address
		want  want
	}{
		{"built-in only", protectedAddr, false, false, listed(SourceBuiltin)},
		{"built-in + force-block", protectedAddr, false, true, listed(SourceBuiltin)},
		{"built-in + force-allow", protectedAddr, true, false, forceAllow},
		{"self + force-block", selfAddr, false, true, listed(SourceSelf)},
		{"bootstrap + force-block", bootAddr, false, true, listed(SourceBootstrap)},
		{"operator only", operatorAddr, false, false, listed(SourceConfig)},
		{"operator + force-block", operatorAddr, false, true, forceBlock},
		{"operator + force-allow", operatorAddr, true, false, forceAllow},
		{"public only", publicAddr, false, false, none},
		{"public + force-block", publicAddr, false, true, forceBlock},
		{"public + force-allow", publicAddr, true, false, forceAllow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var overrides []store.Override
			if tt.allow {
				overrides = append(overrides, override(t, tt.addr, store.ForceAllow))
			}
			if tt.block {
				overrides = append(overrides, override(t, tt.addr, store.ForceBlock))
			}
			r := Judge(indicator(t, tt.addr), allow, NewOverrides(overrides), now)
			if got := (want{r.Effect, r.Rule, r.Source}); got != tt.want {
				t.Errorf("Judge = %+v, want %+v", got, tt.want)
			}
			if r.Effect != EffectNone && r.Reason == "" {
				t.Error("ruling without reason")
			}
		})
	}
}

// TestJudgeOverrideScope: a force-allow covers everything its range
// overlaps; a force-block only its own indicator.
func TestJudgeOverrideScope(t *testing.T) {
	expires := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	fa := override(t, "185.20.0.0/24", store.ForceAllow)
	fa.ExpiresAt = expires
	ov := NewOverrides([]store.Override{fa, override(t, "185.30.0.0/24", store.ForceBlock)})
	allow := NewAllowlist(Builtin()...)

	r := Judge(indicator(t, "185.20.0.9/32"), allow, ov, now)
	if r.Effect != EffectAllow || r.Match != "cidr:185.20.0.0/24" || !r.ExpiresAt.Equal(expires) || r.Note != "n" {
		t.Errorf("address in force-allowed range: %+v", r)
	}
	if !strings.Contains(r.Reason, "force-allow override on cidr:185.20.0.0/24 until 2030-01-02T03:04:05Z") || !strings.Contains(r.Reason, `note: "n"`) {
		t.Errorf("reason = %q", r.Reason)
	}
	if r := Judge(indicator(t, "185.20.0.0/16"), allow, ov, now); r.Effect != EffectAllow {
		t.Errorf("range containing a force-allowed range: %+v", r)
	}
	if r := Judge(indicator(t, "185.30.0.9/32"), allow, ov, now); r.Effect != EffectNone {
		t.Errorf("address in force-blocked range: %+v", r)
	}
	if r := Judge(indicator(t, "185.30.0.0/24"), allow, ov, now); r.Effect != EffectBlock {
		t.Errorf("force-blocked range: %+v", r)
	}
	// A force-block on a range overlapping the built-in list is overruled.
	ov = NewOverrides([]store.Override{override(t, "10.0.0.0/16", store.ForceBlock)})
	if r := Judge(indicator(t, "10.0.0.0/16"), allow, ov, now); r.Effect != EffectAllow || r.Source != SourceBuiltin {
		t.Errorf("force-block on private range: %+v", r)
	}
	// Expired overrides are ignored.
	if r := Judge(indicator(t, "185.20.0.9/32"), allow, NewOverrides([]store.Override{fa}), expires); r.Effect != EffectNone {
		t.Errorf("expired force-allow: %+v", r)
	}
	fb := override(t, "185.30.0.0/24", store.ForceBlock)
	fb.ExpiresAt = expires
	if r := Judge(indicator(t, "185.30.0.0/24"), allow, NewOverrides([]store.Override{fb}), expires.Add(time.Second)); r.Effect != EffectNone {
		t.Errorf("expired force-block: %+v", r)
	}
	// Without allow-list and overrides nothing applies.
	if r := Judge(indicator(t, "10.0.0.1/32"), nil, nil, now); r.Effect != EffectNone {
		t.Errorf("nil rules: %+v", r)
	}
}

func TestOverridesChanged(t *testing.T) {
	a := override(t, "185.20.0.0/24", store.ForceAllow)
	b := override(t, "185.30.0.0/24", store.ForceBlock)
	old := NewOverrides([]store.Override{a, b})

	if got := old.Changed(NewOverrides([]store.Override{a, b})); len(got) != 0 {
		t.Errorf("unchanged: %v", got)
	}
	// Force-block changes need no re-judging of other indicators.
	if got := old.Changed(NewOverrides([]store.Override{a})); len(got) != 0 {
		t.Errorf("force-block removed: %v", got)
	}
	if got := old.Changed(NewOverrides([]store.Override{b})); len(got) != 1 || got[0].String() != "185.20.0.0/24" {
		t.Errorf("force-allow removed: %v", got)
	}
	c := override(t, "185.40.0.1/32", store.ForceAllow)
	if got := old.Changed(NewOverrides([]store.Override{a, b, c})); len(got) != 1 || got[0].String() != "185.40.0.1/32" {
		t.Errorf("force-allow added: %v", got)
	}
	a2 := a
	a2.Note = "changed"
	if got := old.Changed(NewOverrides([]store.Override{a2, b})); len(got) != 2 {
		t.Errorf("force-allow changed: %v", got)
	}
	var nilOv *Overrides
	if got := nilOv.Changed(old); len(got) != 1 {
		t.Errorf("from nil: %v", got)
	}
	if old.Len() != 2 || nilOv.Len() != 0 || len(old.ForceBlocks()) != 1 {
		t.Errorf("Len/ForceBlocks = %d %d %v", old.Len(), nilOv.Len(), old.ForceBlocks())
	}
}

func TestPrefixOf(t *testing.T) {
	for _, tt := range []struct {
		ind  obieproto.Indicator
		want string
	}{
		{obieproto.Indicator{Kind: obieproto.KindIPv4, Value: "185.1.2.3"}, "185.1.2.3/32"},
		{obieproto.Indicator{Kind: obieproto.KindIPv6, Value: "2a00::1"}, "2a00::1/128"},
		{obieproto.Indicator{Kind: obieproto.KindCIDR, Value: "185.1.2.0/24"}, "185.1.2.0/24"},
	} {
		if p, err := PrefixOf(tt.ind); err != nil || p.String() != tt.want {
			t.Errorf("PrefixOf(%v) = %v, %v", tt.ind, p, err)
		}
	}
	if _, err := PrefixOf(obieproto.Indicator{Kind: "asn", Value: "1"}); err == nil {
		t.Error("PrefixOf(asn) succeeded")
	}
}
