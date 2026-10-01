package obieproto

import (
	"fmt"
	"slices"
)

// Receive is the complete check a node applies to a message received on
// [Topic] before it acts on the event or forwards it; data is the message
// payload. Messages carry no GossipSub author: the event's signature is the
// only authentication of its publisher.
//
// The event must pass [Decode] with opts and must carry a valid signature
// ([Verify]); then, at the clock of opts, its issued_at must not lie more
// than [MaxClockSkew] ahead ([ErrClockSkew]) and it must not have expired
// ([ErrExpired]). The checks against the local clock come after the
// signature, so a clock error is only ever reported for an authentic event:
// forged or altered events are always invalid, whatever their timestamps.
// Any error means the message is dropped and not forwarded.
// [AllowDocumentationRanges] has no effect: the mesh never accepts them,
// except in tests that pass [ReceiveDocumentationRanges].
func Receive(data []byte, opts ...Option) (*Event, error) {
	opts = append(slices.Clip(opts), func(o *options) {
		o.allowDocumentation = o.receiveDocumentation
		o.skipClockSkew = true
	})
	e, err := Decode(data, opts...)
	if err != nil {
		return nil, err
	}
	if err := Verify(e); err != nil {
		return nil, err
	}
	now := newOptions(opts).now()
	if err := e.checkClockSkew(now); err != nil {
		return nil, err
	}
	if e.Expired(now) {
		return nil, &FieldError{Field: "issued_at", Err: ErrExpired,
			Detail: fmt.Sprintf("expired at %s", e.ExpiresAt().UTC().Format(TimestampLayout))}
	}
	return e, nil
}
