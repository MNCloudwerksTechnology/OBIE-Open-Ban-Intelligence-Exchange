package daemon

import (
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/console"
	"github.com/MNCloudwerksTechnology/obie/internal/decision"
	"github.com/MNCloudwerksTechnology/obie/internal/enforce"
	"github.com/MNCloudwerksTechnology/obie/internal/mesh"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
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
