package sovereignty

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// Effect is what the operator's rules do to an indicator.
type Effect string

// Effects.
const (
	// EffectNone: no rule applies; the trust-weighted decision stands.
	EffectNone Effect = ""
	// EffectAllow: the indicator is never blocked.
	EffectAllow Effect = "allow"
	// EffectBlock: the indicator is blocked whatever the verdicts say.
	EffectBlock Effect = "block"
)

// Rule names the rule that decided a Ruling.
type Rule string

// Rules, in precedence order.
const (
	RuleForceAllow Rule = "force_allow"
	RuleAllowlist  Rule = "allowlist"
	RuleForceBlock Rule = "force_block"
)

// Ruling is the effect of the allow-list and the overrides on an indicator.
type Ruling struct {
	Effect Effect
	Rule   Rule
	Source Source
	// Match is the allow-listed range or the override's indicator key.
	Match string
	// Label describes an allow-list entry.
	Label string
	// Note is the override's note.
	Note string
	// ExpiresAt is when the override ends; zero for allow-list entries and
	// overrides without TTL.
	ExpiresAt time.Time
	// Reason explains the ruling in one line.
	Reason string
}

// Overrides indexes the operator overrides in effect.
type Overrides struct {
	byKey map[string]store.Override
	// allows are the force-allow overrides with their ranges, by key.
	allows []allowOverride
}

type allowOverride struct {
	prefix netip.Prefix
	o      store.Override
}

// NewOverrides indexes overrides; overrides with an invalid indicator are
// skipped.
func NewOverrides(overrides []store.Override) *Overrides {
	ov := &Overrides{byKey: make(map[string]store.Override, len(overrides))}
	for _, o := range overrides {
		ov.byKey[o.Indicator.Key()] = o
		if o.Action != store.ForceAllow {
			continue
		}
		if p, err := PrefixOf(o.Indicator); err == nil {
			ov.allows = append(ov.allows, allowOverride{prefix: p, o: o})
		}
	}
	slices.SortFunc(ov.allows, func(a, b allowOverride) int {
		return strings.Compare(a.o.Indicator.Key(), b.o.Indicator.Key())
	})
	return ov
}

// Len returns the number of overrides.
func (ov *Overrides) Len() int {
	if ov == nil {
		return 0
	}
	return len(ov.byKey)
}

// Get returns the override of the indicator with key.
func (ov *Overrides) Get(key string) (store.Override, bool) {
	if ov == nil {
		return store.Override{}, false
	}
	o, ok := ov.byKey[key]
	return o, ok
}

// ForceBlocks returns the force-block overrides, by key.
func (ov *Overrides) ForceBlocks() []store.Override {
	if ov == nil {
		return nil
	}
	var out []store.Override
	for _, o := range ov.byKey {
		if o.Action == store.ForceBlock {
			out = append(out, o)
		}
	}
	slices.SortFunc(out, func(a, b store.Override) int { return strings.Compare(a.Indicator.Key(), b.Indicator.Key()) })
	return out
}

// Changed returns the ranges of the force-allow overrides that differ
// between ov and next: added, removed or changed ones. Indicators
// overlapping them must be re-judged.
func (ov *Overrides) Changed(next *Overrides) []netip.Prefix {
	var out []netip.Prefix
	diff := func(a, b *Overrides) {
		if a == nil {
			return
		}
		for _, x := range a.allows {
			if y, ok := b.Get(x.o.Indicator.Key()); !ok || !sameOverride(x.o, y) {
				out = append(out, x.prefix)
			}
		}
	}
	diff(ov, next)
	diff(next, ov)
	return out
}

func sameOverride(a, b store.Override) bool {
	return a.Action == b.Action && a.Note == b.Note && a.ExpiresAt.Equal(b.ExpiresAt)
}

// matchAllow returns the first force-allow override overlapping p.
func (ov *Overrides) matchAllow(p netip.Prefix) (store.Override, bool) {
	if ov == nil {
		return store.Override{}, false
	}
	for _, a := range ov.allows {
		if a.prefix.Overlaps(p) {
			return a.o, true
		}
	}
	return store.Override{}, false
}

// Judge applies the operator's rules to ind, first match wins:
//  1. a force-allow override overlapping ind allows it;
//  2. a built-in, own or bootstrap allow-list entry overlapping ind allows it;
//  3. a force-block override on ind itself blocks it;
//  4. an allowlist.cidrs or allowlist.files entry overlapping ind allows it.
//
// Otherwise the Ruling's Effect is EffectNone. An indicator that is not an
// address or CIDR range is judged by its overrides only.
func Judge(ind obieproto.Indicator, allow *Allowlist, ov *Overrides) Ruling {
	p, err := PrefixOf(ind)
	valid := err == nil
	if valid {
		if o, ok := ov.matchAllow(p); ok {
			return overrideRuling(EffectAllow, o)
		}
		if e, ok := allow.MatchProtected(p); ok {
			return entryRuling(e)
		}
	}
	if o, ok := ov.Get(ind.Key()); ok && o.Action == store.ForceBlock {
		return overrideRuling(EffectBlock, o)
	}
	if valid {
		if e, ok := allow.MatchOperator(p); ok {
			return entryRuling(e)
		}
	}
	return Ruling{}
}

func entryRuling(e Entry) Ruling {
	return Ruling{
		Effect: EffectAllow,
		Rule:   RuleAllowlist,
		Source: e.Source,
		Match:  e.Prefix.String(),
		Label:  e.Label,
		Reason: "allow-listed: " + e.describe(),
	}
}

func overrideRuling(effect Effect, o store.Override) Ruling {
	r := Ruling{
		Effect:    effect,
		Rule:      Rule(o.Action),
		Source:    SourceOverride,
		Match:     o.Indicator.Key(),
		Note:      o.Note,
		ExpiresAt: o.ExpiresAt,
	}
	verb := "force-allow"
	if effect == EffectBlock {
		verb = "force-block"
	}
	r.Reason = fmt.Sprintf("operator %s override on %s", verb, r.Match)
	if !o.ExpiresAt.IsZero() {
		r.Reason += " until " + o.ExpiresAt.UTC().Format(time.RFC3339)
	}
	if o.Note != "" {
		r.Reason += fmt.Sprintf(" (note: %q)", o.Note)
	}
	return r
}

// PrefixOf returns the address range of an address or CIDR indicator; a
// single address is a full-length prefix. IPv4-mapped IPv6 addresses stay
// IPv6, where the built-in ::ffff:0:0/96 entry covers them.
func PrefixOf(ind obieproto.Indicator) (netip.Prefix, error) {
	switch ind.Kind {
	case obieproto.KindIPv4, obieproto.KindIPv6:
		addr, err := netip.ParseAddr(ind.Value)
		if err != nil {
			return netip.Prefix{}, err
		}
		return netip.PrefixFrom(addr.WithZone(""), addr.BitLen()), nil
	case obieproto.KindCIDR:
		p, err := netip.ParsePrefix(ind.Value)
		if err != nil {
			return netip.Prefix{}, err
		}
		return p.Masked(), nil
	default:
		return netip.Prefix{}, fmt.Errorf("indicator kind %q has no address range", ind.Kind)
	}
}
