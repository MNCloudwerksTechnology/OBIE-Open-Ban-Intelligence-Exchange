package decision

import (
	"reflect"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// contributor is the Contributor of verdict v at weight.
func contributor(v *obieproto.Event, weight float64) Contributor {
	return Contributor{PeerID: v.Publisher.PeerID, EventID: v.ID, Weight: weight, Confidence: v.Verdict.Confidence}
}

// wantContributors checks the contributors of the only change in changes.
func wantContributors(t *testing.T, step string, changes []Change, want ...Contributor) {
	t.Helper()
	if len(changes) != 1 {
		t.Errorf("%s: %d changes, want 1", step, len(changes))
		return
	}
	if got := changes[0].Contributors; got == nil || len(got) != len(want) || (len(want) > 0 && !reflect.DeepEqual(got, want)) {
		t.Errorf("%s: contributors = %+v, want %+v", step, got, want)
	}
}

// TestEngineStreamNamesContributors: every block change names the
// verdicts that count in the block — a removal those of the block that
// ended — and a new verdict of a contributor is an update even if score
// and expiry stay (ADR 0032).
func TestEngineStreamNamesContributors(t *testing.T) {
	p := testPolicy()
	p.Threshold, p.Weights[pubB] = 1.5, 0.8
	f := newFixture(t, p)
	f.start(t)
	ind := ipv4("203.0.113.40")

	a := f.put(t, ind, pubA, 1, 2*time.Hour)
	b := f.put(t, ind, pubB, 0.9, time.Hour)
	f.engine.processDirty()
	changes := f.rec.take()
	wantChanges(t, changes, "added/verdict")
	wantContributors(t, "added", changes, contributor(a, 1), contributor(b, 0.8))

	// A publisher without weight holds a verdict that does not count.
	f.put(t, ind, pubD, 1, time.Hour)
	f.engine.processDirty()
	wantChanges(t, f.rec.take())

	// A's new verdict with the same confidence and expiry changes only
	// the verdict that counts.
	f.clock.Advance(time.Second)
	a2 := f.put(t, ind, pubA, 1, 2*time.Hour-time.Second)
	f.engine.processDirty()
	changes = f.rec.take()
	wantChanges(t, changes, "updated/verdict")
	wantContributors(t, "a new verdict of A", changes, contributor(a2, 1), contributor(b, 0.8))

	// Revoking A's verdict ends the block: the removal names A and B.
	f.revoke(t, a2)
	f.engine.processDirty()
	changes = f.rec.take()
	wantChanges(t, changes, "removed/revoke")
	wantContributors(t, "removed", changes, contributor(a2, 1), contributor(b, 0.8))
	if d := changes[0].Decision; d.State != StateNone || d.Contributors != 1 {
		t.Errorf("removed decision = %+v, want no block with B's verdict left", d)
	}
}

// TestSubscribeSnapshotNamesContributors: a subscriber gets the existing
// blocks with their contributors.
func TestSubscribeSnapshotNamesContributors(t *testing.T) {
	f := newFixture(t, testPolicy())
	ind := ipv4("203.0.113.41")
	v := f.put(t, ind, self, 0.7, time.Hour) // local autoblock
	f.start(t)
	wantChanges(t, f.rec.take(), "added/startup")

	late := newRecorder()
	f.engine.Subscribe(late.record)
	changes := late.take()
	wantChanges(t, changes, "added/snapshot")
	wantContributors(t, "snapshot", changes, contributor(v, 1))
}

// TestForceBlockHasNoContributors: a force-block without verdicts names an
// empty list of contributors, not none.
func TestForceBlockHasNoContributors(t *testing.T) {
	f := newFixture(t, testPolicy())
	f.start(t)
	ind := ipv4("203.0.113.42")
	if err := f.store.SetOverride(store.Override{Indicator: ind, Action: store.ForceBlock}); err != nil {
		t.Fatal(err)
	}
	f.engine.processDirty()
	changes := f.rec.take()
	wantChanges(t, changes, "added/override")
	wantContributors(t, "force-block", changes)
}

func TestUUIDBytes(t *testing.T) {
	id := obieproto.NewID(time.Now())
	if got := uuidString(uuidBytes(id)); got != id {
		t.Errorf("round trip of %q = %q", id, got)
	}
	for _, bad := range []string{"", "not-a-uuid", "01900000-0000-7000-8000-00000000000g", "0190000000007000800000000000000001"} {
		if b := uuidBytes(bad); b != ([16]byte{}) || uuidString(b) != "" {
			t.Errorf("uuidBytes(%q) = %x, want the zero UUID, formatted empty", bad, b)
		}
	}
}
