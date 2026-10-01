package sim

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math"
	"math/rand/v2"
	"net/netip"
	"time"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// event is an honest event of a run: a verdict or a revocation that the
// run measures.
type event struct {
	ev        *obieproto.Event
	publisher int32
	// at is when it is published, since the run started.
	at time.Duration
	// chosen: the preempters forge it (C-preempt).
	chosen bool
}

// Verdict lifetimes: honest verdicts live a week, junk verdicts as long as
// the protocol allows, so that the store evicts honest ones first.
const (
	honestTTL = 7 * 24 * 3600
	junkTTL   = obieproto.MaxTTLSeconds
)

// asn is the publishers' disclosed autonomous system number.
var asn = uint32(64512)

// mitre are the techniques a verdict names; with them a verdict is about
// 1 KB, the size of the traffic model (ADR 0035).
var mitre = []string{"T1110", "T1110.001", "T1110.002", "T1110.003", "T1110.004", "T1021.001", "T1021.002",
	"T1021.004", "T1078", "T1078.001", "T1078.003", "T1133", "T1595", "T1595.001", "T1595.002", "T1592.002",
	"T1590.005", "T1046", "T1190", "T1071.001", "T1105", "T1059.004", "T1543.002", "T1136.001", "T1098.004",
	"T1003.008", "T1489", "T1498", "T1499", "T1499.002", "T1583.005", "T1584.005"}

// indicator returns the n-th IPv6 address in 2001:db8::/32 of the run
// with the given salt, unique per event.
func indicator(salt uint32, n uint64) obieproto.Indicator {
	var b [16]byte
	b[0], b[1], b[2], b[3] = 0x20, 0x01, 0x0d, 0xb8
	binary.BigEndian.PutUint32(b[4:8], salt)
	binary.BigEndian.PutUint64(b[8:16], n+1)
	return obieproto.Indicator{Kind: obieproto.KindIPv6, Value: netip.AddrFrom16(b).String(), Scope: "/128"}
}

// newVerdict returns a signed ban verdict of about 1 KB by id, issued at
// t, with a TTL of ttl seconds on the n-th indicator of salt.
func newVerdict(id *simIdentity, t time.Time, ttl int64, salt uint32, n uint64) *obieproto.Event {
	sum := sha256.Sum256(binary.BigEndian.AppendUint64(nil, n))
	return sign(&obieproto.Event{
		ID:        obieproto.NewID(t),
		Spec:      obieproto.Spec,
		Type:      obieproto.TypeVerdict,
		IssuedAt:  obieproto.NewTimestamp(t),
		Indicator: indicator(salt, n),
		Protocol:  "ssh",
		Evidence: &obieproto.Evidence{Events: 47, Reason: "password_bruteforce",
			LogHash: "sha256:" + hex.EncodeToString(sum[:])},
		Verdict:   &obieproto.Verdict{SuggestedAction: obieproto.ActionBan, Confidence: 0.9, TTLSeconds: ttl},
		MITRE:     mitre,
		Publisher: obieproto.Publisher{PeerID: id.PeerID(), ASN: &asn},
	}, id.key)
}

// newRevocation returns id's signed revocation of its verdict v, issued
// at t.
func newRevocation(id *simIdentity, t time.Time, v *obieproto.Event) *obieproto.Event {
	return sign(&obieproto.Event{
		ID:        obieproto.NewID(t),
		Spec:      obieproto.Spec,
		Type:      obieproto.TypeRevoke,
		IssuedAt:  obieproto.NewTimestamp(t),
		Indicator: v.Indicator,
		Revokes:   v.ID,
		Reason:    "false_positive",
		Publisher: obieproto.Publisher{PeerID: id.PeerID(), ASN: &asn},
	}, id.key)
}

// poisson returns the arrival times of a Poisson process with the rate
// per second in [from, to).
func poisson(rng *rand.Rand, from, to time.Duration, rate float64) []time.Duration {
	var out []time.Duration
	for t := from; ; {
		gap := -math.Log(1-rng.Float64()) / rate
		t += time.Duration(gap * float64(time.Second))
		if t >= to {
			return out
		}
		out = append(out, t)
	}
}

// uniform returns rate·(to−from) evenly spaced times in [from, to).
func uniform(from, to time.Duration, rate float64) []time.Duration {
	n := int(rate * (to - from).Seconds())
	out := make([]time.Duration, n)
	for i := range out {
		out[i] = from + time.Duration(float64(i)/rate*float64(time.Second))
	}
	return out
}

// marshalEvent encodes ev as it goes on the wire.
func marshalEvent(ev *obieproto.Event) ([]byte, error) { return json.Marshal(ev) }
