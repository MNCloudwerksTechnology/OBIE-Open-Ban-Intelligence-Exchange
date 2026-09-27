package gossip

// Outcome is what became of a message received on the topic.
type Outcome string

// Outcomes of a received message.
const (
	// Accepted: valid and new; stored and relayed.
	Accepted Outcome = "accepted"
	// InvalidSignature: the signature is missing, malformed or wrong.
	InvalidSignature Outcome = "invalid_signature"
	// InvalidSchema: the event violates the obie/0.1 format or field rules.
	InvalidSchema Outcome = "invalid_schema"
	// Expired: the event expired or is dated too far in the future.
	Expired Outcome = "expired"
	// Duplicate: the event is already stored.
	Duplicate Outcome = "duplicate"
	// RateLimited: the publisher or the forwarding peer exceeded its limit.
	RateLimited Outcome = "rate_limited"
	// TooLarge: the message exceeds obieproto.MaxEventSize.
	TooLarge Outcome = "too_large"
)

// Outcomes lists every Outcome, e.g. to initialize counters.
var Outcomes = []Outcome{Accepted, InvalidSignature, InvalidSchema, Expired, Duplicate, RateLimited, TooLarge}

// Metrics observes the outcome of every message received from a peer; the
// node's own publications are not observed. Observe is called concurrently
// on the validation path and must be fast.
type Metrics interface {
	Observe(Outcome)
}

type nopMetrics struct{}

func (nopMetrics) Observe(Outcome) {}
