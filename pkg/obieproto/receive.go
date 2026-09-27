package obieproto

import "fmt"

// Receive is the complete check a node applies to a message received on
// [Topic] before it acts on the event or forwards it. data is the message
// payload and from the peer ID of the message's author (the GossipSub "from"
// field, not the peer that forwarded it).
//
// The event must pass [Decode] with opts, must have been published by its
// own publisher (from equals publisher.peer_id, else [ErrPublisherMismatch]),
// must not have expired at the clock of opts ([ErrExpired]) and must carry a
// valid signature ([Verify]). The cheap checks run before the signature is
// verified. Any error means the message is dropped and not forwarded.
// [AllowDocumentationRanges] has no effect: the mesh never accepts them.
func Receive(data []byte, from string, opts ...Option) (*Event, error) {
	opts = append(opts, func(o *options) { o.allowDocumentation = false })
	e, err := Decode(data, opts...)
	if err != nil {
		return nil, err
	}
	if from != e.Publisher.PeerID {
		return nil, &FieldError{Field: "publisher.peer_id", Err: ErrPublisherMismatch,
			Detail: fmt.Sprintf("message author %q is not the publisher", from)}
	}
	if now := newOptions(opts).now(); e.Expired(now) {
		return nil, &FieldError{Field: "issued_at", Err: ErrExpired,
			Detail: fmt.Sprintf("expired at %s", e.ExpiresAt().UTC().Format(TimestampLayout))}
	}
	if err := Verify(e); err != nil {
		return nil, err
	}
	return e, nil
}
