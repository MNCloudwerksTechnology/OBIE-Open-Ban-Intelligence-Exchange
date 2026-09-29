// Package decision turns the verdicts a node holds into one deterministic,
// explainable decision per indicator: a trust-weighted score over the
// distinct publishers' active ban verdicts, a quorum of publishers and a
// threshold, plus local autoblock, overruled by the operator's allow-list
// and overrides. The Engine keeps the decision of every indicator with
// active verdicts or a force-block override up to date from the store's
// change notifications and streams block changes to subscribers. See
// ADR 0011 and ADR 0013.
package decision

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// State is the outcome of a decision.
type State string

// Decision states.
const (
	StateBlock State = "block"
	StateNone  State = "none"
	// StateAllowed: the allow-list or a force-allow override keeps the
	// indicator from being blocked, whatever its verdicts.
	StateAllowed State = "allowed"
)

// scoreTolerance absorbs float rounding when comparing the score with the
// threshold, so that e.g. 0.6 + 0.6 + 0.6 reaches 1.8.
const scoreTolerance = 1e-9

// Policy is what the operator configured for deciding.
type Policy struct {
	// Self is this node's peer ID; its verdicts are local.
	Self string
	// Names and Weights of the publishers listed in trust.publishers.
	Names   map[string]string
	Weights map[string]float64
	// DefaultWeight applies to publishers not listed.
	DefaultWeight float64
	// LocalWeight applies to Self.
	LocalWeight float64

	Threshold      float64
	Quorum         int
	MaxTTL         time.Duration
	LocalAutoblock bool
}

// NewPolicy builds the policy of the node with peer ID self from its
// configuration.
func NewPolicy(self string, trust config.Trust, dec config.Decision) Policy {
	p := Policy{
		Self:           self,
		Names:          make(map[string]string, len(trust.Publishers)),
		Weights:        make(map[string]float64, len(trust.Publishers)),
		DefaultWeight:  trust.DefaultWeight,
		LocalWeight:    trust.LocalWeight,
		Threshold:      dec.Threshold,
		Quorum:         dec.Quorum,
		MaxTTL:         dec.MaxTTL.Std(),
		LocalAutoblock: dec.LocalAutoblock,
	}
	for _, pub := range trust.Publishers {
		p.Names[pub.PeerID] = pub.Name
		p.Weights[pub.PeerID] = pub.Weight
	}
	return p
}

// Weight returns the trust weight of the publisher with peerID:
// trust.local_weight for this node, its trust.publishers weight, else
// trust.default_weight.
func (p *Policy) Weight(peerID string) float64 {
	if peerID == p.Self {
		return p.LocalWeight
	}
	if w, ok := p.Weights[peerID]; ok {
		return w
	}
	return p.DefaultWeight
}

// Contribution is one publisher's verdict as seen by a decision.
type Contribution struct {
	PeerID string
	// Name is the publisher's name in trust.publishers; empty if unlisted.
	Name string
	// Local is set for this node's own verdicts.
	Local   bool
	EventID string
	Action  string
	// Reason is the verdict's evidence reason, e.g. "password_bruteforce";
	// Protocol names the attacked service, e.g. "ssh".
	Reason, Protocol string
	Weight           float64
	Confidence       float64
	// Score is Weight × Confidence if the verdict contributes, else 0.
	Score     float64
	IssuedAt  time.Time
	ExpiresAt time.Time
	// Contributes is set for ban verdicts of publishers with weight > 0.
	Contributes bool
}

// Decision is the decision on one indicator and why it was taken.
type Decision struct {
	Indicator obieproto.Indicator
	State     State
	// Score is the sum of the contributing verdicts' scores.
	Score     float64
	Threshold float64
	// Contributors counts the contributing publishers.
	Contributors int
	Quorum       int
	// Autoblock is set when this node's own verdict alone decided the block.
	Autoblock bool
	// ExpiresAt is when a block ends; zero for StateNone.
	ExpiresAt time.Time
	// Publishers lists every active verdict, contributing or not, by
	// publisher.
	Publishers []Contribution
	// Sovereignty is the effect of the allow-list and the overrides;
	// its Effect is sovereignty.EffectNone if they do not apply.
	Sovereignty sovereignty.Ruling
	// Reason explains the state in one line.
	Reason      string
	EvaluatedAt time.Time
}

// Rules are the operator's rules that overrule the verdicts. The zero
// value applies none.
type Rules struct {
	Allowlist *sovereignty.Allowlist
	Overrides *sovereignty.Overrides
}

// Decide decides on ind like Evaluate and then applies the operator's
// rules (sovereignty.Judge): an allowed indicator is StateAllowed, a
// force-blocked one StateBlock until the override ends, capped at
// p.MaxTTL from now.
func Decide(ind obieproto.Indicator, verdicts []*obieproto.Event, p Policy, r Rules, now time.Time) Decision {
	d := Evaluate(ind, verdicts, p, now)
	d.Sovereignty = sovereignty.Judge(ind, r.Allowlist, r.Overrides, now)
	switch d.Sovereignty.Effect {
	case sovereignty.EffectAllow:
		d.State, d.ExpiresAt, d.Autoblock = StateAllowed, time.Time{}, false
	case sovereignty.EffectBlock:
		d.State, d.Autoblock = StateBlock, false
		d.ExpiresAt = now.Add(p.MaxTTL)
		if end := d.Sovereignty.ExpiresAt; !end.IsZero() && end.Before(d.ExpiresAt) {
			d.ExpiresAt = end
		}
	default:
		return d
	}
	d.Reason = d.Sovereignty.Reason + "; verdicts: " + d.Reason
	return d
}

// Evaluate decides on ind from its active verdicts at now under policy p.
// Verdicts that are not active at now are ignored; of several verdicts of
// one publisher only the newest counts.
func Evaluate(ind obieproto.Indicator, verdicts []*obieproto.Event, p Policy, now time.Time) Decision {
	d := Decision{Indicator: ind, State: StateNone, Threshold: p.Threshold, Quorum: p.Quorum, EvaluatedAt: now}
	var consensusExpiry, localExpiry time.Time
	localBan := false
	for _, v := range latestPerPublisher(verdicts, now) {
		c := contribution(v, &p)
		if c.Contributes {
			d.Score += c.Score
			d.Contributors++
			consensusExpiry = later(consensusExpiry, c.ExpiresAt)
			if c.Local {
				localBan = true
				localExpiry = later(localExpiry, c.ExpiresAt)
			}
		}
		d.Publishers = append(d.Publishers, c)
	}

	consensus := d.Score >= p.Threshold-scoreTolerance && d.Contributors >= p.Quorum
	switch {
	case consensus:
		d.State, d.ExpiresAt = StateBlock, consensusExpiry
	case p.LocalAutoblock && localBan:
		d.State, d.ExpiresAt, d.Autoblock = StateBlock, localExpiry, true
	}
	if d.State == StateBlock {
		if limit := now.Add(p.MaxTTL); d.ExpiresAt.After(limit) {
			d.ExpiresAt = limit
		}
	}
	d.Reason = reason(&d, len(d.Publishers))
	return d
}

// latestPerPublisher returns the newest active verdict of each publisher,
// ordered by publisher, so that the score is summed in a fixed order.
func latestPerPublisher(verdicts []*obieproto.Event, now time.Time) []*obieproto.Event {
	latest := make(map[string]*obieproto.Event, len(verdicts))
	for _, v := range verdicts {
		if v == nil || v.Type != obieproto.TypeVerdict || v.Verdict == nil || v.Expired(now) {
			continue
		}
		cur, ok := latest[v.Publisher.PeerID]
		if !ok || v.IssuedAt.After(cur.IssuedAt.Time) || (v.IssuedAt.Equal(cur.IssuedAt.Time) && v.ID > cur.ID) {
			latest[v.Publisher.PeerID] = v
		}
	}
	out := make([]*obieproto.Event, 0, len(latest))
	for _, v := range latest {
		out = append(out, v)
	}
	slices.SortFunc(out, func(a, b *obieproto.Event) int { return strings.Compare(a.Publisher.PeerID, b.Publisher.PeerID) })
	return out
}

func contribution(v *obieproto.Event, p *Policy) Contribution {
	c := Contribution{
		PeerID:     v.Publisher.PeerID,
		Name:       p.Names[v.Publisher.PeerID],
		Local:      v.Publisher.PeerID == p.Self,
		EventID:    v.ID,
		Action:     v.Verdict.SuggestedAction,
		Protocol:   v.Protocol,
		Weight:     p.Weight(v.Publisher.PeerID),
		Confidence: v.Verdict.Confidence,
		IssuedAt:   v.IssuedAt.UTC(),
		ExpiresAt:  v.ExpiresAt().UTC(),
	}
	if v.Evidence != nil {
		c.Reason = v.Evidence.Reason
	}
	c.Contributes = c.Action == obieproto.ActionBan && c.Weight > 0
	if c.Contributes {
		c.Score = c.Weight * c.Confidence
	}
	return c
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// reason summarizes why d has its state; active is the number of active
// verdicts.
func reason(d *Decision, active int) string {
	if active == 0 {
		return "no active verdicts"
	}
	tally := fmt.Sprintf("score %s %s threshold %s, %d %s quorum %d",
		formatFloat(d.Score), relation(d.Score >= d.Threshold-scoreTolerance), formatFloat(d.Threshold),
		d.Contributors, relation(d.Contributors >= d.Quorum), d.Quorum)
	switch {
	case d.Autoblock:
		return "local autoblock: this node's own ban verdict (" + tally + ")"
	case d.State == StateBlock:
		return "consensus: " + tally
	default:
		return "below consensus: " + tally
	}
}

func relation(reached bool) string {
	if reached {
		return ">="
	}
	return "<"
}

// formatFloat prints a score or weight without float noise.
func formatFloat(f float64) string {
	return fmt.Sprintf("%.4g", f)
}
