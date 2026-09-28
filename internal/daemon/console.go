package daemon

import (
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/console"
	"github.com/MNCloudwerksTechnology/obie/internal/decision"
	"github.com/MNCloudwerksTechnology/obie/internal/enforce"
	"github.com/MNCloudwerksTechnology/obie/internal/gossip"
	"github.com/MNCloudwerksTechnology/obie/internal/mesh"
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
