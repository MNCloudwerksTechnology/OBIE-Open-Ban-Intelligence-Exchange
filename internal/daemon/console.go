package daemon

import (
	"cmp"
	"context"
	"fmt"
	"net/netip"
	"slices"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/console"
	"github.com/MNCloudwerksTechnology/obie/internal/decision"
	"github.com/MNCloudwerksTechnology/obie/internal/enforce"
	"github.com/MNCloudwerksTechnology/obie/internal/gossip"
	"github.com/MNCloudwerksTechnology/obie/internal/mesh"
	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// consoleConfig returns cfg with the test hook Testing.ConsoleListen
// applied.
func consoleConfig(cfg config.Console, t Testing) config.Console {
	if t.ConsoleListen != "" {
		cfg.Listen = t.ConsoleListen
	}
	return cfg
}

// consoleService is the admin API's view of the web console.
type consoleService struct {
	console *console.Console
}

func (s consoleService) Console() admin.ConsoleResponse {
	return consoleResponse(s.console.State(), s.console.Token())
}

func (s consoleService) RotateConsoleToken() admin.ConsoleResponse {
	token := s.console.RotateToken()
	return consoleResponse(s.console.State(), token)
}

// consoleResponse converts the console's state into its admin API wire
// type.
func consoleResponse(st console.State, token string) admin.ConsoleResponse {
	resp := admin.ConsoleResponse{Enabled: st.Enabled, Listen: st.Listen, URL: st.URL, Token: token}
	if st.Err != nil {
		resp.Error = st.Err.Error()
	}
	return resp
}

// consoleFacts reads the node's numbers for the console's overview, with
// cheap reads only: counts the subsystems keep and the short list of
// overrides (ADR 0020). Every read works while its subsystem is stopped.
type consoleFacts struct {
	mesh       *mesh.Mesh
	engine     *decision.Engine
	reconciler *enforce.Reconciler
	store      *store.DB
	loads      *configLoads
	enforce    config.Enforce
	now        func() time.Time
}

func (f *consoleFacts) read() console.Facts {
	peers := f.mesh.PeerCounts()
	counts := f.engine.Counts()
	overrides, overridesErr := f.store.Overrides(f.now())
	return console.Facts{
		Peers: console.PeerFacts{Connected: peers.Connected, Bootstrap: peers.Bootstrap, Configured: peers.Configured},
		Decisions: console.DecisionFacts{
			Block:      counts.Decisions[decision.StateBlock],
			None:       counts.Decisions[decision.StateNone],
			Allowed:    counts.Decisions[decision.StateAllowed],
			Indicators: counts.Indicators,
			Verdicts:   counts.Verdicts,
		},
		Enforce: enforceFacts(f.reconciler.Status(), f.enforce),
		Store: console.StoreFacts{Overrides: len(overrides), OverridesErr: overridesErr,
			VerdictRecords: f.store.Verdicts(), EventsAccepted: f.store.Stats().Accepted},
		Config: configFacts(f.loads.record()),
	}
}

// enforceFacts converts the reconciler's status and the enforce settings
// cfg for the console.
func enforceFacts(st enforce.Status, cfg config.Enforce) console.EnforceFacts {
	e := console.EnforceFacts{
		Backend:    string(cfg.Backend),
		MaxEntries: cfg.MaxEntries,
		Mode:       string(st.Mode),
		Applied:    st.Applied,
		Blocks:     st.Blocks,
		Covered:    st.Covered,
		Refused:    st.SkippedBlocks[enforce.SkipAllowlist],
		Capped:     st.SkippedBlocks[enforce.SkipMaxEntries],
		Failures:   st.Failures,
		RetryIn:    st.RetryIn,
	}
	if st.Err != nil {
		e.Err = st.Err.Error()
	}
	return e
}

// configFacts converts the record of the configuration loads for the
// console.
func configFacts(rec loadRecord) console.ConfigFacts {
	c := console.ConfigFacts{LoadedAt: rec.LoadedAt, Reloaded: rec.Reloaded, RejectedAt: rec.RejectedAt, RestartKeys: rec.RestartKeys}
	if rec.Rejected != nil {
		c.Rejected = rec.Rejected.Error()
	}
	return c
}

// consolePeers reads the node's peers for the console's peers view: the
// mesh's known peers and the engine's verdict counts per publisher, both
// cheap, and on request one publisher's verdicts from the store
// (ADR 0021). Every read works while its subsystem is stopped.
type consolePeers struct {
	mesh   *mesh.Mesh
	engine *decision.Engine
	store  *store.DB
	now    func() time.Time
}

func (p *consolePeers) read() console.PeerSet {
	known := p.mesh.KnownPeers()
	counts := p.engine.PublisherCounts()
	set := console.PeerSet{
		Peers:         make([]console.Peer, len(known)),
		DefaultWeight: p.mesh.DefaultWeight(),
		Verdicts:      make(map[string]console.VerdictCount, len(counts)),
		EventWindow:   gossip.TallyWindow,
	}
	for i := range known {
		set.Peers[i] = consolePeer(&known[i])
	}
	for id, c := range counts {
		set.Verdicts[id] = console.VerdictCount{Held: c.Verdicts, Counting: c.Counting}
	}
	return set
}

// verdicts reads up to limit of the active verdicts held from the
// publisher id, after the indicator key after.
func (p *consolePeers) verdicts(id, after string, limit int) (console.VerdictPage, error) {
	page, err := p.store.PublisherVerdicts(id, p.now(), store.Page{After: after, Limit: limit})
	if err != nil {
		return console.VerdictPage{}, err
	}
	out := console.VerdictPage{Verdicts: make([]console.Verdict, len(page.Verdicts)), Next: page.Next}
	for i, ev := range page.Verdicts {
		out.Verdicts[i] = consoleVerdict(ev)
	}
	return out, nil
}

// consolePeer converts a peer the mesh knows for the console.
func consolePeer(k *mesh.KnownPeer) console.Peer {
	return console.Peer{
		ID:             k.ID,
		Name:           k.Name,
		Bootstrap:      k.Bootstrap,
		Publisher:      k.Publisher,
		Connected:      k.Connected,
		Addrs:          k.Addrs,
		ConnectedSince: k.ConnectedSince,
		Latency:        k.Latency,
		LastSeen:       k.LastSeen,
		DialError:      k.DialError,
		DialFailedAt:   k.DialFailedAt,
		Weight:         k.TrustWeight,
		Events:         eventCounts(k.Events),
	}
}

// eventCounts sorts the outcomes of a peer's events into accepted ones,
// duplicates and rejected ones by reason.
func eventCounts(outcomes map[gossip.Outcome]int) console.EventCounts {
	var e console.EventCounts
	for o, n := range outcomes {
		switch o {
		case gossip.Accepted:
			e.Accepted += n
		case gossip.Duplicate:
			e.Duplicates += n
		default:
			if e.Rejected == nil {
				e.Rejected = map[string]int{}
			}
			e.Rejected[string(o)] += n
		}
	}
	return e
}

// consoleVerdict converts a verdict event for the console.
func consoleVerdict(ev *obieproto.Event) console.Verdict {
	v := console.Verdict{Key: ev.Key(), Address: ev.Indicator.Value, Protocol: ev.Protocol, ExpiresAt: ev.ExpiresAt()}
	if ev.Verdict != nil {
		v.Action, v.Confidence = ev.Verdict.SuggestedAction, ev.Verdict.Confidence
	}
	if ev.Evidence != nil {
		v.Reason = ev.Evidence.Reason
	}
	return v
}

// consoleDecisions reads the decisions, explanations and the firewall for
// the console's decisions and firewall views, with cheap reads only: a
// page of the engine's decisions, one indicator's verdicts, the
// reconciler's snapshot, and on request the backend's entries (ADR 0022).
// Every read works while its subsystem is stopped. The engine and the
// reconciler are set before anything starts.
type consoleDecisions struct {
	engine     *decision.Engine
	reconciler *enforce.Reconciler
	enforce    config.Enforce
}

var _ console.DecisionSource = (*consoleDecisions)(nil)

// Decisions reads the page of the kept decisions q selects; the firewall
// filter asks the reconciler's last pass.
func (d *consoleDecisions) Decisions(q console.DecisionQuery) console.DecisionPage {
	snap := d.reconciler.Snapshot()
	dq := decision.Query{State: decision.State(q.State), Category: q.Category, Publisher: q.Publisher, Overlapping: q.Search,
		Sort: decision.Sort(q.Sort), After: q.After, Before: q.Before, Last: q.Last, Limit: q.Limit}
	switch q.Firewall {
	case console.FirewallApplied:
		dq.Where = snap.Applies
	case console.FirewallNotApplied:
		dq.Where = func(p netip.Prefix) bool { return !snap.Applies(p) }
	}
	page := d.engine.Browse(dq)
	out := console.DecisionPage{Items: make([]console.DecisionItem, len(page.Items)), Total: page.Total, Offset: page.Offset,
		States: make(map[string]int, len(page.States)), Generation: page.Generation}
	for s, n := range page.States {
		out.States[string(s)] = n
	}
	for i := range page.Items {
		it := &page.Items[i]
		out.Items[i] = consoleDecisionItem(&it.Decision, snap)
		out.Items[i].Categories, out.Items[i].Verdicts, out.Items[i].Cursor = it.Categories, it.Verdicts, it.Cursor
	}
	return out
}

// Generation is the engine's count of re-evaluations.
func (d *consoleDecisions) Generation() uint64 { return d.engine.Generation() }

// Categories counts the kept decisions by the categories of their
// verdicts.
func (d *consoleDecisions) Categories() map[string]int { return d.engine.Categories() }

// Explain re-evaluates the range p like obiectl explain, with the kept
// decisions on the networks around it and the firewall's last pass.
func (d *consoleDecisions) Explain(p netip.Prefix) (console.Explanation, error) {
	ind, err := indicatorOfRange(p)
	if err != nil {
		return console.Explanation{}, fmt.Errorf("%w: %w", console.ErrNoIndicator, err)
	}
	dec, err := d.engine.Explain(ind)
	if err != nil {
		return console.Explanation{}, err
	}
	snap := d.reconciler.Snapshot()
	policy := d.engine.Policy()
	ex := console.Explanation{
		Range: p, State: string(dec.State), Score: dec.Score, Threshold: dec.Threshold,
		Contributors: dec.Contributors, Quorum: dec.Quorum, Autoblock: dec.Autoblock, LocalAutoblock: policy.LocalAutoblock,
		Reason: dec.Reason, ExpiresAt: dec.ExpiresAt, EvaluatedAt: dec.EvaluatedAt,
		Verdicts: make([]console.Contribution, len(dec.Publishers)), Ruling: consoleRuling(&dec.Sovereignty),
		Firewall: consoleCoverage(snap.Lookup(p)),
	}
	for i, c := range dec.Publishers {
		_, listed := policy.Weights[c.PeerID]
		ex.Verdicts[i] = console.Contribution{PeerID: c.PeerID, Name: c.Name, Local: c.Local, Listed: listed && !c.Local,
			Action: c.Action, Reason: c.Reason, Protocol: c.Protocol, Weight: c.Weight, Confidence: c.Confidence,
			Score: c.Score, Contributes: c.Contributes, IssuedAt: c.IssuedAt, ExpiresAt: c.ExpiresAt}
	}
	if kept, ok := d.engine.Decision(ind.Key()); ok {
		ex.Kept, ex.KeptState, ex.KeptAt = true, string(kept.State), kept.EvaluatedAt
	}
	for _, around := range d.engine.Covering(p) {
		ex.Around = append(ex.Around, consoleDecisionItem(&around, snap))
	}
	return ex, nil
}

// Firewall reads the reconciler's condition and its last pass.
func (d *consoleDecisions) Firewall() console.Firewall {
	fw := console.Firewall{Facts: enforceFacts(d.reconciler.Status(), d.enforce), ExpiryTolerance: enforce.ExpiryTolerance}
	if snap := d.reconciler.Snapshot(); snap != nil {
		fw.Pass = &console.FirewallPass{Mode: string(snap.Mode), At: snap.At, Entries: len(snap.Entries),
			Deferred: len(snap.Deferred), Seq: snap.Seq}
	}
	return fw
}

// FirewallEntries lists the entries the backend applies right now, each
// with the kept decision on its range (one engine lock for all of them),
// and the first missing decided blocks that none of them holds: compared
// with what the backend lists, not with the last pass, so an entry lost
// since shows (ADR 0022).
func (d *consoleDecisions) FirewallEntries(ctx context.Context, missing int) (console.FirewallListing, error) {
	entries, err := d.reconciler.Entries(ctx)
	if err != nil {
		return console.FirewallListing{}, err
	}
	slices.SortFunc(entries, func(a, b enforce.Entry) int {
		return cmp.Or(a.Prefix.Addr().Compare(b.Prefix.Addr()), cmp.Compare(a.Prefix.Bits(), b.Prefix.Bits()))
	})
	listing := console.FirewallListing{Entries: make([]console.FirewallEntry, len(entries))}
	keys := make([]string, len(entries))
	for i, e := range entries {
		listing.Entries[i] = console.FirewallEntry{Range: e.Prefix, Expires: e.Expires}
		keys[i] = keyOfRange(e.Prefix)
	}
	d.engine.Lookup(keys, func(i int, dec *decision.Decision) {
		listing.Entries[i].Key, listing.Entries[i].State, listing.Entries[i].ExpiresAt = keys[i], string(dec.State), dec.ExpiresAt
	})
	if missing > 0 {
		held := enforce.NewEntryIndex(entries)
		page := d.engine.Browse(decision.Query{State: decision.StateBlock, Limit: missing,
			Where: func(p netip.Prefix) bool { return !held.Holds(p) }})
		snap := d.reconciler.Snapshot()
		listing.Missing = console.DecisionPage{Items: make([]console.DecisionItem, len(page.Items)), Total: page.Total,
			Generation: page.Generation}
		for i := range page.Items {
			it := consoleDecisionItem(&page.Items[i].Decision, snap)
			if it.Firewall.Applied {
				it.Firewall = console.Coverage{Gone: true}
			}
			listing.Missing.Items[i] = it
		}
	}
	return listing, nil
}

// keyOfRange returns the key of the indicator naming the range p, as the
// engine keeps it; a range no indicator may name has no decision.
func keyOfRange(p netip.Prefix) string {
	p = p.Masked()
	switch {
	case p.Bits() < p.Addr().BitLen():
		return obieproto.KindCIDR + ":" + p.String()
	case p.Addr().Is4():
		return obieproto.KindIPv4 + ":" + p.Addr().String()
	default:
		return obieproto.KindIPv6 + ":" + p.Addr().String()
	}
}

// indicatorOfRange returns the indicator naming the range p: an IPv4 or
// IPv6 address, or a CIDR range; it fails for a range no indicator may
// name, e.g. one broader than /16.
func indicatorOfRange(p netip.Prefix) (obieproto.Indicator, error) {
	ind := obieproto.Indicator{Kind: obieproto.KindCIDR, Value: p.Masked().String()}
	switch {
	case p.Bits() < p.Addr().BitLen():
	case p.Addr().Is4():
		ind = obieproto.Indicator{Kind: obieproto.KindIPv4, Value: p.Addr().String()}
	default:
		ind = obieproto.Indicator{Kind: obieproto.KindIPv6, Value: p.Addr().String()}
	}
	if err := ind.Normalize(); err != nil {
		return obieproto.Indicator{}, err
	}
	return ind, nil
}

// consoleDecisionItem converts a kept decision for the console, with how
// the firewall's last pass left its range.
func consoleDecisionItem(d *decision.Decision, snap *enforce.Snapshot) console.DecisionItem {
	p, _ := sovereignty.PrefixOf(d.Indicator)
	return console.DecisionItem{Range: p, State: string(d.State), Score: d.Score, Threshold: d.Threshold,
		Contributors: d.Contributors, Quorum: d.Quorum, Autoblock: d.Autoblock, Rule: string(d.Sovereignty.Rule),
		Protected: d.Sovereignty.Rule == sovereignty.RuleAllowlist && d.Sovereignty.Source.Protected(),
		DecidedAt: d.EvaluatedAt, ExpiresAt: d.ExpiresAt, Firewall: consoleCoverage(snap.Lookup(p))}
}

// consoleCoverage converts how the firewall's last pass left a range.
func consoleCoverage(c enforce.Coverage) console.Coverage {
	return console.Coverage{Applied: c.Applied, Entry: c.Entry.Prefix, EntryExpires: c.Entry.Expires, Skipped: c.Skipped,
		Within: c.Within, Deferred: c.Deferred}
}

// consoleRuling converts what the allow-list and the overrides do.
func consoleRuling(r *sovereignty.Ruling) console.Ruling {
	return console.Ruling{Effect: string(r.Effect), Rule: string(r.Rule), Source: string(r.Source), Protected: r.Source.Protected(),
		Match: r.Match, Label: r.Label, Note: r.Note, ExpiresAt: r.ExpiresAt, Reason: r.Reason}
}
