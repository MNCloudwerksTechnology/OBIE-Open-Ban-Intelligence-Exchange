package gossip

import (
	"context"
	"math"
	"testing"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// histogram returns the sample count and sum of h.
func histogram(t *testing.T, h prometheus.Histogram) (count uint64, sum float64) {
	t.Helper()
	var m dto.Metric
	if err := h.Write(&m); err != nil {
		t.Fatal(err)
	}
	return m.GetHistogram().GetSampleCount(), m.GetHistogram().GetSampleSum()
}

func TestMetricsRegistered(t *testing.T) {
	for _, c := range []prometheus.Collector{receivedTotal, publishedTotal, propagationDelay} {
		if err := prometheus.Register(c); err == nil {
			t.Errorf("collector was not registered")
		}
	}
	if n := testutil.CollectAndCount(receivedTotal, "obie_events_received_total"); n != len(Outcomes) {
		t.Errorf("obie_events_received_total has %d series, want one per outcome (%d)", n, len(Outcomes))
	}
	if n := testutil.CollectAndCount(publishedTotal, "obie_events_published_total"); n != 2 {
		t.Errorf("obie_events_published_total has %d series, want verdict and revoke", n)
	}
}

// TestValidateUpdatesMetrics: every received message is counted by
// outcome; accepted ones record the delay since issued_at.
func TestValidateUpdatesMetrics(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	v, _ := newValidator(t, newStore(t), now)
	p := newPublisher(t)
	ev := p.verdict(t, now.Add(-3*time.Second), 3600)
	accepted := testutil.ToFloat64(receivedTotal.WithLabelValues(string(Accepted)))
	duplicate := testutil.ToFloat64(receivedTotal.WithLabelValues(string(Duplicate)))
	invalid := testutil.ToFloat64(receivedTotal.WithLabelValues(string(InvalidSchema)))
	count, sum := histogram(t, propagationDelay)

	msg := func(data []byte) *pubsub.Message { return &pubsub.Message{Message: &pb.Message{Data: data}} }
	v.validate(context.Background(), peerA, msg(marshal(t, ev)))
	v.validate(context.Background(), peerB, msg(marshal(t, ev)))
	v.validate(context.Background(), peerA, msg([]byte("hello")))

	for _, c := range []struct {
		outcome Outcome
		before  float64
	}{{Accepted, accepted}, {Duplicate, duplicate}, {InvalidSchema, invalid}} {
		if got := testutil.ToFloat64(receivedTotal.WithLabelValues(string(c.outcome))) - c.before; got != 1 {
			t.Errorf("obie_events_received_total{outcome=%q} rose by %v, want 1", c.outcome, got)
		}
	}
	gotCount, gotSum := histogram(t, propagationDelay)
	if gotCount-count != 1 || math.Abs(gotSum-sum-3) > 1e-9 {
		t.Errorf("obie_propagation_delay_seconds: %d samples summing to %v, want one of 3s", gotCount-count, gotSum-sum)
	}
}

func TestObserveDelayClampsClockSkew(t *testing.T) {
	now := time.Now()
	count, sum := histogram(t, propagationDelay)
	observeDelay(now.Add(time.Minute), now)
	if gotCount, gotSum := histogram(t, propagationDelay); gotCount-count != 1 || gotSum != sum {
		t.Errorf("a future issued_at recorded %d samples summing to %v, want one of 0s", gotCount-count, gotSum-sum)
	}
}

// TestPublishCountsPublishedEvents: an event counts once it is sent to
// the mesh; one held for want of a peer counts when a peer joins.
func TestPublishCountsPublishedEvents(t *testing.T) {
	n := newNode(t)
	before := testutil.ToFloat64(publishedTotal.WithLabelValues("verdict"))
	if err := n.gossip.Publish(context.Background(), n.verdict(t, time.Now(), 3600)); err != nil {
		t.Fatal(err)
	}
	unsigned := n.verdict(t, time.Now(), 3600)
	unsigned.Publisher.Signature = ""
	_ = n.gossip.Publish(context.Background(), unsigned)
	if got := testutil.ToFloat64(publishedTotal.WithLabelValues("verdict")) - before; got != 0 {
		t.Errorf("obie_events_published_total{type=\"verdict\"} rose by %v without a peer, want 0", got)
	}
	peer := newNode(t)
	connect(t, n.host, peer.host, n.gossip.topic, peer.gossip.topic)
	waitFor(t, propagationDeadline, "the held verdict to count", func() bool {
		return testutil.ToFloat64(publishedTotal.WithLabelValues("verdict"))-before == 1
	})
}

// TestCreatedAt: the id's millisecond time refines issued_at only within
// the second issued_at names (ADR 0032).
func TestCreatedAt(t *testing.T) {
	issued := time.Date(2026, 9, 30, 12, 0, 7, 0, time.UTC)
	for _, c := range []struct {
		name string
		id   string
		want time.Time
	}{
		{"within the second", obieproto.NewID(issued.Add(345 * time.Millisecond)), issued.Add(345 * time.Millisecond)},
		{"at the second", obieproto.NewID(issued), issued},
		{"its last millisecond", obieproto.NewID(issued.Add(999 * time.Millisecond)), issued.Add(999 * time.Millisecond)},
		{"a second later", obieproto.NewID(issued.Add(time.Second)), issued},
		{"earlier", obieproto.NewID(issued.Add(-time.Millisecond)), issued},
		{"not a UUIDv7", "0199a1b2-c3d4-4e5f-8a6b-000000000001", issued},
	} {
		ev := &obieproto.Event{ID: c.id, IssuedAt: obieproto.NewTimestamp(issued)}
		if got := createdAt(ev); !got.Equal(c.want) {
			t.Errorf("%s: createdAt = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestValidateRecordsSubSecondDelay: an event created 1.25 s before its
// receipt records 1.25 s, not the 2 s since its whole-second issued_at.
func TestValidateRecordsSubSecondDelay(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second).Add(500 * time.Millisecond)
	v, _ := newValidator(t, newStore(t), now)
	p := newPublisher(t)
	created := now.Add(-1250 * time.Millisecond)
	ev := p.verdict(t, created, 3600)
	ev.ID, ev.Publisher.Signature = obieproto.NewID(created), ""
	p.sign(t, ev)
	count, sum := histogram(t, propagationDelay)

	msg := &pubsub.Message{Message: &pb.Message{Data: marshal(t, ev)}}
	if got := v.validate(context.Background(), peerA, msg); got != pubsub.ValidationAccept {
		t.Fatalf("validate = %v, want accept", got)
	}
	gotCount, gotSum := histogram(t, propagationDelay)
	if gotCount-count != 1 || math.Abs(gotSum-sum-1.25) > 1e-9 {
		t.Errorf("obie_propagation_delay_seconds: %d samples summing to %v, want one of 1.25s", gotCount-count, gotSum-sum)
	}
}
