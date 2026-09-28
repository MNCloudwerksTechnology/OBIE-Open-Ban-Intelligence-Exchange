package console

import "time"

// Facts are the node's numbers at one moment, which the overview shows
// (ADR 0020). The daemon reads them from the subsystems with cheap reads
// only. A number whose subsystem is not running is left at its zero
// value; the console tells it apart from a real zero by that subsystem's
// lifecycle state.
type Facts struct {
	Peers     PeerFacts
	Decisions DecisionFacts
	Enforce   EnforceFacts
	Store     StoreFacts
	Config    ConfigFacts
}

// PeerFacts count the mesh's peers.
type PeerFacts struct {
	// Connected counts the connected peers; Bootstrap those among them
	// that are configured.
	Connected, Bootstrap int
	// Configured counts the peers in mesh.bootstrap, without this node.
	Configured int
}

// DecisionFacts are the decision engine's numbers after its last
// evaluation pass.
type DecisionFacts struct {
	// Block, None and Allowed count the decisions by state.
	Block, None, Allowed int
	// Indicators counts the indicators with an active verdict; Verdicts
	// counts their active verdicts.
	Indicators, Verdicts int
}

// EnforceFacts are the enforcement settings and the condition after the
// last enforcement pass.
type EnforceFacts struct {
	// Backend is enforce.backend; MaxEntries is enforce.max_entries.
	Backend    string
	MaxEntries int
	// Mode is the node mode of the last pass; empty before the first one.
	Mode string
	// Applied counts the entries the backend applies after the last
	// successful pass.
	Applied int
	// Blocks counts the decided blocks that pass considered: Applied of
	// them have their own entry, Covered share one with another block,
	// Refused are refused by the allow-list right before apply, and
	// Capped are left out over enforce.max_entries.
	Blocks, Covered, Refused, Capped int
	// Failures counts the consecutive failed passes, Err is the last
	// failure and RetryIn the delay before the next attempt.
	Failures int
	Err      string
	RetryIn  time.Duration
}

// StoreFacts are the store's numbers.
type StoreFacts struct {
	// Overrides counts the operator overrides in effect; OverridesErr is
	// why they could not be read.
	Overrides    int
	OverridesErr error
	// VerdictRecords counts the verdicts the store holds, active or not;
	// EventsAccepted counts the events it accepted since obied started.
	VerdictRecords int64
	EventsAccepted uint64
}

// ConfigFacts tell when the running configuration was loaded and how the
// last reload went.
type ConfigFacts struct {
	// LoadedAt is when the running configuration was loaded: at start,
	// or by the last successful reload if Reloaded.
	LoadedAt time.Time
	Reloaded bool
	// RejectedAt and Rejected are the time and the error of the last
	// rejected reload, until a later reload succeeds.
	RejectedAt time.Time
	Rejected   string
	// RestartKeys name the changed settings that wait for a restart.
	RestartKeys []string
}
