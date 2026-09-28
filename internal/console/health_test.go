package console

import (
	"reflect"
	"testing"

	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
)

func TestNodeHealth(t *testing.T) {
	running := func(name string) lifecycle.Status {
		return lifecycle.Status{Name: name, State: lifecycle.StateRunning, Ready: true}
	}
	with := func(s lifecycle.Status, f func(*lifecycle.Status)) lifecycle.Status { f(&s); return s }
	for name, tc := range map[string]struct {
		statuses []lifecycle.Status
		want     Health
	}{
		"all ready": {[]lifecycle.Status{running("console"), running("store"), running("mesh")},
			Health{State: HealthReady, Label: "Ready"}},
		"starting": {[]lifecycle.Status{running("console"), {Name: "store", State: lifecycle.StateStarting}, {Name: "mesh", State: lifecycle.StatePending}},
			Health{State: HealthStarting, Label: "Starting"}},
		"not ready": {[]lifecycle.Status{running("console"), with(running("enforce"), func(s *lifecycle.Status) {
			s.Ready, s.Error = false, "enforcement failed 2 time(s)"
		})}, Health{State: HealthDegraded, Label: "Degraded", Problems: []string{"enforce: enforcement failed 2 time(s)"}}},
		"not ready without reason": {[]lifecycle.Status{with(running("ops"), func(s *lifecycle.Status) { s.Ready = false })},
			Health{State: HealthDegraded, Label: "Degraded", Problems: []string{"ops: not ready"}}},
		"degraded detail": {[]lifecycle.Status{running("console"), with(running("mesh"), func(s *lifecycle.Status) {
			s.Detail = "degraded: 0 peers connected (0/2 bootstrap peers)"
		}), with(running("decision"), func(s *lifecycle.Status) { s.Detail = "3 blocked of 9 indicators" })},
			Health{State: HealthDegraded, Label: "Degraded", Problems: []string{"mesh: 0 peers connected (0/2 bootstrap peers)"}}},
		"shutting down": {[]lifecycle.Status{running("console"), {Name: "store", State: lifecycle.StateRunning, Ready: true},
			{Name: "mesh", State: lifecycle.StateStopping}, {Name: "admin", State: lifecycle.StateStopped}},
			Health{State: HealthStopping, Label: "Shutting down"}},
		"failed wins over starting": {[]lifecycle.Status{running("console"), {Name: "ops", State: lifecycle.StateFailed, Error: "bind"},
			{Name: "mesh", State: lifecycle.StatePending}}, Health{State: HealthStopping, Label: "Shutting down"}},
	} {
		t.Run(name, func(t *testing.T) {
			if got := nodeHealth(tc.statuses); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("nodeHealth = %+v, want %+v", got, tc.want)
			}
		})
	}
}
