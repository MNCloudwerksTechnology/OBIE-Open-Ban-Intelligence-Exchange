package gossip

import (
	"context"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/MNCloudwerksTechnology/obie/internal/eventtrace"
)

// TestTraceOutcomesAreTheValidators: the trace's outcomes are the gossip
// outcomes, so the harness can join both.
func TestTraceOutcomesAreTheValidators(t *testing.T) {
	if eventtrace.Accepted != string(Accepted) || eventtrace.Duplicate != string(Duplicate) {
		t.Errorf("trace outcomes %q, %q differ from the validator's %q, %q", eventtrace.Accepted, eventtrace.Duplicate,
			Accepted, Duplicate)
	}
	if slices.Contains(Outcomes[:], Outcome(eventtrace.Published)) {
		t.Errorf("%q is a validator outcome too", eventtrace.Published)
	}
}

// openTrace opens a trace file in dir for node, closed when the test ends.
func openTrace(t *testing.T, dir string, node peer.ID) (*eventtrace.Writer, string) {
	t.Helper()
	path := filepath.Join(dir, node.String()+".jsonl")
	w, err := eventtrace.Open(path, node.String(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w, path
}

// readTrace returns "from outcome" of every line of the trace at path.
func readTrace(t *testing.T, path string) []string {
	t.Helper()
	recs, err := eventtrace.ReadFiles(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range recs {
		out = append(out, r.Event+" "+r.From+" "+r.Outcome)
	}
	return out
}

// TestValidatorAndTracerWriteTheTrace: the validator traces every outcome
// it decides, the tracer the copies GossipSub drops before validation;
// the node's own messages and OBIE's rejections are not traced twice.
func TestValidatorAndTracerWriteTheTrace(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	w, path := openTrace(t, t.TempDir(), selfPeer)
	v, _ := newValidator(t, newStore(t), now)
	v.trace = w
	tr := newTracer(selfPeer, w, func() time.Time { return now })
	ev := newPublisher(t).verdict(t, now, 3600)
	msg := func(data []byte, id string) *pubsub.Message {
		return &pubsub.Message{Message: &pb.Message{Data: data}, ID: id}
	}

	v.validate(context.Background(), peerA, msg(marshal(t, ev), ev.ID))
	v.validate(context.Background(), peerB, msg(marshal(t, ev), "")) // no ID from GossipSub: taken from the event
	v.validate(context.Background(), selfPeer, msg(marshal(t, ev), ev.ID))
	junk := msg([]byte("hello"), "")
	v.validate(context.Background(), peerA, junk)
	copyOf := fromPeer(peerB)
	copyOf.ID = ev.ID
	tr.DuplicateMessage(copyOf)
	tr.RejectMessage(copyOf, pubsub.RejectValidationQueueFull)
	tr.RejectMessage(copyOf, pubsub.RejectValidationFailed)
	tr.RejectMessage(copyOf, pubsub.RejectValidationIgnored)
	own := fromPeer(selfPeer)
	own.ID = ev.ID
	tr.DuplicateMessage(own)
	_ = w.Close()

	want := []string{
		ev.ID + " " + peerA.String() + " accepted",
		ev.ID + " " + peerB.String() + " duplicate",
		messageID(junk.Message) + " " + peerA.String() + " invalid_schema",
		ev.ID + " " + peerB.String() + " duplicate",
		ev.ID + " " + peerB.String() + " queue_full",
	}
	if got := readTrace(t, path); !slices.Equal(got, want) {
		t.Errorf("trace =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestTraceTimesTheArrival: the validator's line carries the time the copy
// arrived, at which the propagation delay is measured, not a later reading
// of the clock after the checks and the store.
func TestTraceTimesTheArrival(t *testing.T) {
	arrived := time.Now().UTC().Truncate(time.Second)
	w, path := openTrace(t, t.TempDir(), selfPeer)
	v, _ := newValidator(t, newStore(t), arrived)
	v.trace = w
	readings := 0
	v.now = func() time.Time { // a minute passes with every reading
		readings++
		return arrived.Add(time.Duration(readings-1) * time.Minute)
	}
	ev := newPublisher(t).verdict(t, arrived, 3600)
	v.validate(context.Background(), peerA, &pubsub.Message{Message: &pb.Message{Data: marshal(t, ev)}, ID: ev.ID})
	_ = w.Close()
	recs, err := eventtrace.ReadFiles(path)
	if err != nil || len(recs) != 1 || recs[0].Outcome != eventtrace.Accepted || !recs[0].At.Equal(arrived) {
		t.Errorf("trace = %+v, %v; want one accepted copy at %s", recs, err, arrived)
	}
}

// TestTraceShowsHops publishes on A in the line A–B–C, each node with a
// trace file: joined, the traces show the event reaching B in one hop and
// C in two, through B.
func TestTraceShowsHops(t *testing.T) {
	dir := t.TempDir()
	var paths []string
	traced := func(n *node, o *Options) {
		w, path := openTrace(t, dir, n.host.ID())
		o.Trace = w
		paths = append(paths, path)
	}
	a, b, c := newNode(t, traced), newNode(t, traced), newNode(t, traced)
	line(t, a, b, c)

	v := a.verdict(t, time.Now(), 3600)
	if err := a.gossip.Publish(context.Background(), v); err != nil {
		t.Fatal(err)
	}
	idA, idB, idC := a.host.ID().String(), b.host.ID().String(), c.host.ID().String()
	// The validator traces an event after it stored it, and the writers
	// flush every second.
	var s eventtrace.Spread
	waitFor(t, propagationDeadline, "the verdict's receipt on C in the traces", func() bool {
		recs, err := eventtrace.ReadFiles(paths...)
		if err != nil {
			return false // not written yet
		}
		spreads := eventtrace.Join(recs)
		i := slices.IndexFunc(spreads, func(s eventtrace.Spread) bool { return s.Event == v.ID })
		if i < 0 {
			return false
		}
		s = spreads[i]
		_, reached := s.Reached[idC]
		return reached && s.Origin != ""
	})
	if s.Origin != idA {
		t.Errorf("origin = %s, want A", s.Origin)
	}
	rb, rc := s.Reached[idB], s.Reached[idC]
	if rb.Hops != 1 || !slices.Equal(rb.Path, []string{idA, idB}) || rb.Delay < 0 {
		t.Errorf("B's receipt = %+v, want one hop from A", rb)
	}
	if rc.Hops != 2 || !slices.Equal(rc.Path, []string{idA, idB, idC}) || rc.Delay < rb.Delay {
		t.Errorf("C's receipt = %+v, want two hops through B, after B", rc)
	}
}
