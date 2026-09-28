package daemon

import (
	"context"
	"log/slog"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/audit"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/decision"
	"github.com/MNCloudwerksTechnology/obie/internal/mesh"
	"github.com/MNCloudwerksTechnology/obie/internal/verdicts"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// newAuditLog returns the audit trail, writing to the file at path
// (audit.path) unless it is empty and recording the mode that mode
// returns. It keeps the last records in memory either way (ADR 0025).
func newAuditLog(path string, mode func() config.Mode, log *slog.Logger) *audit.Log {
	return audit.New(path, audit.Options{Mode: func() string { return string(mode()) }}, log)
}

// auditConnections records peers connecting and disconnecting.
func auditConnections(log *audit.Log) func(mesh.Connection) {
	return func(c mesh.Connection) {
		log.Write(audit.PeerConnection(audit.Peer{ID: c.ID, Name: c.Name, Bootstrap: c.Bootstrap, Publisher: c.Publisher},
			c.Connected))
	}
}

// subscribeAudit records the engine's block changes and allow-listings.
// The blocks built at startup are no change and are not recorded.
func subscribeAudit(engine *decision.Engine, log *audit.Log) {
	engine.Subscribe(func(c decision.Change) {
		if isChange(c.Cause) {
			log.Write(audit.BlockChange(c))
		}
	})
	engine.SubscribeTransitions(func(t decision.Transition) {
		if t.Decision.State == decision.StateAllowed && isChange(t.Cause) {
			log.Write(audit.Allowed(t))
		}
	})
}

// isChange reports whether a decision with cause changed while the node
// runs, rather than being rebuilt at startup.
func isChange(cause string) bool {
	return cause != decision.CauseStartup && cause != decision.CauseSnapshot
}

// auditedVerdicts records the verdicts and revocations this node issues.
type auditedVerdicts struct {
	admin.VerdictService
	audit *audit.Log
}

func (v auditedVerdicts) Report(ctx context.Context, r verdicts.Report) (verdicts.Result, error) {
	res, err := v.VerdictService.Report(ctx, r)
	if err == nil && !res.Coalesced {
		v.audit.Write(audit.LocalReport(res.Event))
	}
	return res, err
}

func (v auditedVerdicts) Revoke(ctx context.Context, r verdicts.Revocation) ([]*obieproto.Event, error) {
	revocations, err := v.VerdictService.Revoke(ctx, r)
	// Revocations issued before a failure took effect too.
	for _, ev := range revocations {
		v.audit.Write(audit.Revocation(ev))
	}
	return revocations, err
}
