package decision

import (
	"encoding/hex"
	"unique"
)

// Contributor is a verdict that counts in a block: its publisher, its
// event ID, and the trust weight and confidence it counted with
// (ADR 0032).
type Contributor struct {
	PeerID, EventID    string
	Weight, Confidence float64
}

// keptContributor is a Contributor of a kept block, kept compactly so that
// the block change stream can name it when the block ends: the publisher
// interned and the event ID as the 16 bytes of its UUID.
type keptContributor struct {
	publisher          unique.Handle[string]
	eventID            [16]byte
	weight, confidence float64
}

// keptContributorsOf returns the contributions that count, compactly, in
// their order; nil if none does.
func keptContributorsOf(contributions []Contribution) []keptContributor {
	n := 0
	for i := range contributions {
		if contributions[i].Contributes {
			n++
		}
	}
	if n == 0 {
		return nil
	}
	out := make([]keptContributor, 0, n)
	for i := range contributions {
		c := &contributions[i]
		if c.Contributes {
			out = append(out, keptContributor{publisher: unique.Make(c.PeerID), eventID: uuidBytes(c.EventID),
				weight: c.Weight, confidence: c.Confidence})
		}
	}
	return out
}

// contributorsOf returns the kept contributors in their order; an empty,
// non-nil list if there are none.
func contributorsOf(kept []keptContributor) []Contributor {
	out := make([]Contributor, len(kept))
	for i, k := range kept {
		out[i] = Contributor{PeerID: k.publisher.Value(), EventID: uuidString(k.eventID), Weight: k.weight,
			Confidence: k.confidence}
	}
	return out
}

// uuidBytes returns the 16 bytes of a UUID in canonical form, as every
// event ID is ([ENV-2]); the zero UUID if id is not one.
func uuidBytes(id string) [16]byte {
	var b [16]byte
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return [16]byte{}
	}
	hexDigits := id[0:8] + id[9:13] + id[14:18] + id[19:23] + id[24:36]
	if _, err := hex.Decode(b[:], []byte(hexDigits)); err != nil {
		return [16]byte{}
	}
	return b
}

// uuidString formats the 16 bytes of a UUID in canonical lower-case form;
// empty for the zero UUID, which no event ID is.
func uuidString(b [16]byte) string {
	if b == ([16]byte{}) {
		return ""
	}
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
