package sim

import (
	"crypto/ed25519"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

func TestPoisson(t *testing.T) {
	const rate, from, to = 10.0, 30 * time.Second, 1030 * time.Second
	times := poisson(testRNG(4), from, to, rate)
	// 10,000 arrivals expected; the standard deviation is 100.
	if n := float64(len(times)); math.Abs(n-10000) > 400 {
		t.Errorf("%v arrivals at %v/s over %v, want about 10,000", n, rate, to-from)
	}
	if !slices.IsSorted(times) || times[0] < from || times[len(times)-1] >= to {
		t.Errorf("arrivals not sorted within [%v, %v): first %v, last %v", from, to, times[0], times[len(times)-1])
	}
}

func TestUniform(t *testing.T) {
	times := uniform(10*time.Second, 12*time.Second, 5)
	want := []time.Duration{10 * time.Second, 10200 * time.Millisecond, 10400 * time.Millisecond,
		10600 * time.Millisecond, 10800 * time.Millisecond, 11 * time.Second, 11200 * time.Millisecond,
		11400 * time.Millisecond, 11600 * time.Millisecond, 11800 * time.Millisecond}
	if !slices.Equal(times, want) {
		t.Errorf("uniform(10 s, 12 s, 5/s) = %v, want %v", times, want)
	}
}

func testIdentity(t *testing.T, b byte) *simIdentity {
	t.Helper()
	var seed [ed25519.SeedSize]byte
	seed[0] = b
	id, err := newSimIdentity(seed)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// TestVerdictIsValid: a node's validator accepts a run's verdicts and
// revocations; a verdict is about 1 KB and names an address of its own.
func TestVerdictIsValid(t *testing.T) {
	id := testIdentity(t, 1)
	now := time.Now()
	seen := map[string]bool{}
	for n := range uint64(20) {
		v := newVerdict(id, now, honestTTL, 42, n)
		data, err := marshalEvent(v)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) < 900 || len(data) > 1200 {
			t.Errorf("verdict %d is %d bytes, want about 1 KB", n, len(data))
		}
		got, err := obieproto.Receive(data, obieproto.ReceiveDocumentationRanges())
		if err != nil {
			t.Fatalf("verdict %d: %v", n, err)
		}
		if got.ID != v.ID || eventID(data) != v.ID {
			t.Errorf("verdict %d received as %q, message ID %q; want %q", n, got.ID, eventID(data), v.ID)
		}
		if seen[v.Indicator.Value] {
			t.Errorf("verdict %d repeats indicator %s", n, v.Indicator.Value)
		}
		seen[v.Indicator.Value] = true
	}
	v := newVerdict(id, now, honestTTL, 42, 0)
	r := newRevocation(id, now.Add(time.Second), v)
	data, err := marshalEvent(r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := obieproto.Receive(data, obieproto.ReceiveDocumentationRanges()); err != nil {
		t.Errorf("revocation: %v", err)
	}
	if r.Revokes != v.ID || r.Indicator != v.Indicator {
		t.Errorf("revocation revokes %q on %v, want %q on %v", r.Revokes, r.Indicator, v.ID, v.Indicator)
	}
}

func TestEventIDOfJunk(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("{"), []byte(`{"id":""}`), make([]byte, obieproto.MaxEventSize+1)} {
		if got := eventID(data); got != "junk" {
			t.Errorf("eventID(%q…) = %q, want junk", data[:min(len(data), 10)], got)
		}
	}
}
