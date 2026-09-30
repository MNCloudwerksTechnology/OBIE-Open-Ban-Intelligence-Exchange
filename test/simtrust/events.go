package simtrust

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"net/netip"
	"time"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// Evidence of every simulated verdict: what the Fail2Ban action sends for
// an SSH jail (contrib/fail2ban/action.d/obie.conf).
const (
	verdictProtocol = "ssh"
	verdictReason   = "bruteforce"
	verdictEvents   = 5
	revokeReason    = "unbanned"
	mitreBruteForce = "T1110"
)

// Key is a publisher's Ed25519 key, derived from a seed so that a run is
// reproducible.
type Key struct {
	PeerID  string
	private ed25519.PrivateKey
}

// NewKey derives the key named label of the run with seed.
func NewKey(seed uint64, label string) Key {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], seed)
	sum := sha256.Sum256(append(b[:], label...))
	priv := ed25519.NewKeyFromSeed(sum[:])
	id, err := obieproto.PeerIDFromPublicKey(priv.Public().(ed25519.PublicKey))
	if err != nil {
		// An Ed25519 public key always has a peer ID.
		panic(fmt.Sprintf("peer ID of a new key: %v", err))
	}
	return Key{PeerID: id, private: priv}
}

// Sign signs ev with k, as its publisher would; the simulation itself
// never needs signatures (ADR 0034).
func (k Key) Sign(ev *obieproto.Event) error {
	return obieproto.Sign(ev, k.private)
}

// idSource makes event IDs: UUIDv7s carrying the event's time, with
// random bits from the run's generator.
type idSource struct {
	rng *rand.Rand
}

func (s idSource) next(t time.Time) string {
	var b [16]byte
	binary.BigEndian.PutUint64(b[8:], s.rng.Uint64())
	binary.BigEndian.PutUint64(b[0:8], uint64(t.UnixMilli())<<16|uint64(s.rng.Uint32()&0xffff)) // #nosec G115 -- simulated times lie after 1970.
	b[6] = 0x70 | b[6]&0x0f                                                                     // version 7
	b[8] = 0x80 | b[8]&0x3f                                                                     // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// indicatorOf returns the indicator of a single address.
func indicatorOf(addr netip.Addr) obieproto.Indicator {
	if addr.Is4() {
		return obieproto.Indicator{Kind: obieproto.KindIPv4, Value: addr.String(), Scope: "/32"}
	}
	return obieproto.Indicator{Kind: obieproto.KindIPv6, Value: addr.String(), Scope: "/128"}
}

// publisherOf returns the publisher field of peerID in asn; asn 0 is
// left out.
func publisherOf(peerID string, asn uint32) obieproto.Publisher {
	p := obieproto.Publisher{PeerID: peerID}
	if asn != 0 {
		p.ASN = &asn
	}
	return p
}

// newVerdict returns a ban verdict on addr, issued at at.
func newVerdict(id string, pub obieproto.Publisher, addr netip.Addr, at time.Time, confidence float64, ttl time.Duration) *obieproto.Event {
	return &obieproto.Event{
		ID:        id,
		Spec:      obieproto.Spec,
		Type:      obieproto.TypeVerdict,
		IssuedAt:  obieproto.NewTimestamp(at),
		Indicator: indicatorOf(addr),
		Protocol:  verdictProtocol,
		Evidence:  &obieproto.Evidence{Events: verdictEvents, Reason: verdictReason},
		Verdict: &obieproto.Verdict{SuggestedAction: obieproto.ActionBan, Confidence: confidence,
			TTLSeconds: int64(ttl / time.Second)},
		MITRE:     []string{mitreBruteForce},
		Publisher: pub,
	}
}

// newRevocation returns the revocation of verdict v by its publisher at at.
func newRevocation(id string, v *obieproto.Event, at time.Time) *obieproto.Event {
	return &obieproto.Event{
		ID:        id,
		Spec:      obieproto.Spec,
		Type:      obieproto.TypeRevoke,
		IssuedAt:  obieproto.NewTimestamp(at),
		Indicator: v.Indicator,
		Revokes:   v.ID,
		Reason:    revokeReason,
		Publisher: v.Publisher,
	}
}
