package gossip

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/MNCloudwerksTechnology/obie/internal/eventtrace"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// validator is the topic validator: it decides whether a message is
// stored and relayed.
type validator struct {
	self    peer.ID
	store   store.Store
	metrics Metrics
	// trace records every message's outcome; nil for none.
	trace *eventtrace.Writer
	log   *slog.Logger
	now   func() time.Time
	// receive are the obieproto.Receive options besides the clock.
	receive    []obieproto.Option
	publishers *limiter
	peers      *limiter
	// observe, if set, sees the message ID and outcome of every checked
	// message (Testing.Observe).
	observe func(id string, from peer.ID, outcome Outcome)
}

// validate is the pubsub.ValidatorEx of the topic; from is the peer that
// forwarded the message.
func (v *validator) validate(_ context.Context, from peer.ID, msg *pubsub.Message) pubsub.ValidationResult {
	if from == v.self {
		// Publish already checked and stored the node's own event.
		return pubsub.ValidationAccept
	}
	now := v.now()
	outcome, result := v.check(from, msg.GetData(), now)
	receivedTotal.WithLabelValues(string(outcome)).Inc()
	v.metrics.Observe(from, outcome)
	id := idOf(msg)
	v.trace.Write(id, from.String(), now, string(outcome))
	if v.observe != nil {
		v.observe(id, from, outcome)
	}
	return result
}

// idOf returns the message ID of msg: the event ID of an event.
func idOf(msg *pubsub.Message) string {
	if msg.ID != "" {
		return msg.ID
	}
	return messageID(msg.Message)
}

// check runs the checks in order — size, format and field rules,
// signature, clock, duplicate, rate limits — on data received at now, and
// stores an accepted event.
func (v *validator) check(from peer.ID, data []byte, now time.Time) (Outcome, pubsub.ValidationResult) {
	if len(data) > obieproto.MaxEventSize {
		v.drop(from, TooLarge, nil)
		return TooLarge, pubsub.ValidationReject
	}
	ev, err := obieproto.Receive(data, v.receiveOptions(now)...)
	if err != nil {
		outcome, result := classify(err, data, v.receiveOptions(now.Add(-obieproto.MaxClockSkew)))
		v.drop(from, outcome, err)
		return outcome, result
	}

	// On a store error the event goes on: Put deduplicates again.
	seen, err := v.store.Seen(ev.ID)
	if err != nil {
		v.log.Error("duplicate check failed", "event", ev.ID, "err", err)
	}
	if seen {
		return Duplicate, pubsub.ValidationIgnore
	}
	// An event dropped for one limit does not count against the other: a
	// relay is not charged for a flooding publisher's excess.
	if !takeBoth(v.publishers, ev.Publisher.PeerID, v.peers, from.String(), now) {
		v.log.Debug("rate limit exceeded; ignoring event", "event", ev.ID, "publisher", ev.Publisher.PeerID,
			"peer", from.String())
		return RateLimited, pubsub.ValidationIgnore
	}

	// The event is valid whatever the store makes of it (e.g. a verdict
	// older than the publisher's current one): it is relayed either way.
	if _, err := v.store.Put(ev); err != nil && !errors.Is(err, store.ErrClosed) {
		v.log.Error("storing received event failed", "event", ev.ID, "err", err)
	}
	observeDelay(createdAt(ev), now)
	return Accepted, pubsub.ValidationAccept
}

// receiveOptions returns the obieproto.Receive options with the clock at
// now.
func (v *validator) receiveOptions(now time.Time) []obieproto.Option {
	return append(slices.Clip(v.receive), obieproto.WithClock(func() time.Time { return now }))
}

// classify maps an obieproto.Receive error to an outcome. Invalid events
// are rejected, which penalizes the forwarding peer. Clock failures are
// ignored, since an honest peer whose clock differs a little may forward
// them, unless the event expired so long ago that no clock within
// MaxClockSkew would have accepted it; lenient are the Receive options with
// the clock MaxClockSkew behind.
func classify(err error, data []byte, lenient []obieproto.Option) (Outcome, pubsub.ValidationResult) {
	switch {
	case errors.Is(err, obieproto.ErrTooLarge):
		return TooLarge, pubsub.ValidationReject
	case errors.Is(err, obieproto.ErrInvalidSignature), errors.Is(err, obieproto.ErrPublisherMismatch):
		return InvalidSignature, pubsub.ValidationReject
	case errors.Is(err, obieproto.ErrClockSkew):
		return Expired, pubsub.ValidationIgnore
	case errors.Is(err, obieproto.ErrExpired):
		if _, err := obieproto.Receive(data, lenient...); errors.Is(err, obieproto.ErrExpired) {
			return Expired, pubsub.ValidationReject
		}
		return Expired, pubsub.ValidationIgnore
	default:
		return InvalidSchema, pubsub.ValidationReject
	}
}

func (v *validator) drop(from peer.ID, outcome Outcome, err error) {
	v.log.Debug("dropping message", "peer", from.String(), "outcome", string(outcome), "err", err)
}
