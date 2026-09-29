package console

import (
	"strings"

	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
)

// Health states of the node, as the console shows them.
const (
	HealthStarting = "starting"
	HealthReady    = "ready"
	HealthDegraded = "degraded"
	HealthStopping = "stopping"
)

var healthLabels = map[string]string{
	HealthStarting: "Starting",
	HealthReady:    "Ready",
	HealthDegraded: "Degraded",
	HealthStopping: "Shutting down",
}

// Health is the node's health derived from its subsystems.
type Health struct {
	// State is one of the Health* states.
	State string `json:"state"`
	// Label names the state for people.
	Label string `json:"label"`
	// Problems names each degraded subsystem and why ("enforce: …").
	Problems []string `json:"problems,omitempty"`
}

// degradedPrefix starts the detail of a subsystem that runs and is ready,
// but in a degraded way (e.g. the mesh without peers).
const degradedPrefix = "degraded"

// nodeHealth derives the node's health from the status of its subsystems:
// shutting down once one is stopping, stopped or failed; starting while
// one is pending or starting; degraded while a running one is not ready
// or reports a degraded detail; ready otherwise.
func nodeHealth(statuses []lifecycle.Status) Health {
	var stopping, starting bool
	var problems []string
	for _, s := range statuses {
		switch s.State {
		case lifecycle.StateStopping, lifecycle.StateStopped, lifecycle.StateFailed:
			stopping = true
		case lifecycle.StatePending, lifecycle.StateStarting:
			starting = true
		case lifecycle.StateRunning:
			if !s.Ready {
				problems = append(problems, s.Name+": "+notReadyReason(s))
			} else if reason, ok := degradedReason(s.Detail); ok {
				problems = append(problems, s.Name+": "+reason)
			}
		}
	}
	state := HealthReady
	switch {
	case stopping:
		state, problems = HealthStopping, nil
	case starting:
		state, problems = HealthStarting, nil
	case len(problems) > 0:
		state = HealthDegraded
	}
	return Health{State: state, Label: healthLabels[state], Problems: problems}
}

// notReadyReason is why the running subsystem s is not ready.
func notReadyReason(s lifecycle.Status) string {
	if s.Error == "" {
		return "not ready"
	}
	return s.Error
}

// degradedReason returns why a subsystem whose status detail is detail
// runs in a degraded way, and whether it does.
func degradedReason(detail string) (string, bool) {
	rest, ok := strings.CutPrefix(detail, degradedPrefix)
	if !ok {
		return "", false
	}
	if reason := strings.TrimLeft(rest, ": "); reason != "" {
		return reason, true
	}
	return degradedPrefix, true
}
