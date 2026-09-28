package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/mesh"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// ipMeshDown is reported while node A has lost its mesh.
const ipMeshDown = "203.0.113.50"

// TestMeshDownKeepsLocalProtection stops every peer of node A: A stays
// ready, reports locally and blocks what it reports on its own (local
// autoblock), because a node must never depend on the mesh to protect
// itself.
func TestMeshDownKeepsLocalProtection(t *testing.T) {
	c := newCluster(t, clusterOptions{backend: config.BackendDryRun}, "A", "B", "C")
	a := c.node("A")
	c.node("B").stop(t)
	c.node("C").stop(t)
	within(t, meshBound, "A notices its peers are gone", func(ctx context.Context) error {
		resp, err := a.client.Peers(ctx)
		if err == nil && len(resp.Peers) > 0 {
			err = fmt.Errorf("%d peers still connected", len(resp.Peers))
		}
		return err
	})

	resp := a.report(t, ipMeshDown, 0.8, obieproto.ActionBan)
	if resp.Event.Indicator.Value != ipMeshDown {
		t.Fatalf("report = %+v", resp)
	}
	within(t, enforcementBound, "A blocks its own report without peers", func(ctx context.Context) error {
		return a.blocks(ctx, ipMeshDown)
	})
	within(t, requestTimeout, "A is ready with the mesh degraded", func(ctx context.Context) error {
		st, err := a.client.Status(ctx)
		if err != nil {
			return err
		}
		m := st.Subsystems[mesh.Name]
		if !st.Ready || !m.Ready || !strings.HasPrefix(m.Detail, "degraded") {
			return fmt.Errorf("status ready %v, mesh %+v", st.Ready, m)
		}
		return nil
	})

	a.revoke(t, ipMeshDown)
	within(t, enforcementBound, "A unblocks after revoking without peers", func(ctx context.Context) error {
		return a.notBlocking(ctx, ipMeshDown)
	})
}
