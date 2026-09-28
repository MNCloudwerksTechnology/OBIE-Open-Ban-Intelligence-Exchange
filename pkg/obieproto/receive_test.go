package obieproto

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTopic(t *testing.T) {
	if Topic != "obie/0.1/verdicts" || !strings.HasPrefix(Topic, Spec+"/") {
		t.Errorf("Topic = %q, want obie/0.1/verdicts", Topic)
	}
}

func TestReceive(t *testing.T) {
	signedJSON := func(e *Event) []byte { return mustMarshal(t, signedBy(t, e, testSeedA)) }
	shortVerdict := func() *Event { e := validVerdict(); e.Verdict.TTLSeconds = 60; return e }
	tampered := []byte(strings.Replace(string(signedJSON(validVerdict())), `"events":47`, `"events":48`, 1))
	tests := []struct {
		name    string
		data    []byte
		now     time.Time
		wantErr error
	}{
		{name: "signed verdict", data: signedJSON(validVerdict()), now: testNow},
		{name: "signed revocation", data: signedJSON(validRevoke()), now: testNow},
		{name: "verdict one second before expiry", data: signedJSON(shortVerdict()), now: testNow.Add(59 * time.Second)},
		{name: "expired verdict", data: signedJSON(shortVerdict()), now: testNow.Add(60 * time.Second), wantErr: ErrExpired},
		{name: "expired revocation", data: signedJSON(validRevoke()), now: testNow.Add(MaxTTLSeconds * time.Second), wantErr: ErrExpired},
		{name: "unsigned", data: mustMarshal(t, func() *Event {
			e := validVerdict()
			e.Publisher.PeerID, e.Publisher.Signature = testPeerIDA, ""
			return e
		}()), now: testNow, wantErr: ErrInvalidSignature},
		{name: "tampered", data: tampered, now: testNow, wantErr: ErrInvalidSignature},
		{name: "fails decoding", data: []byte(`{"spec":"obie/0.2"}`), now: testNow, wantErr: ErrUnsupportedSpec},
		{name: "future issued_at", data: signedJSON(validVerdict()), now: testNow.Add(-MaxClockSkew - time.Second), wantErr: ErrClockSkew},
		// Clock checks run after the signature: a forgery is invalid whatever its timestamps.
		{name: "tampered future issued_at", data: tampered, now: testNow.Add(-MaxClockSkew - time.Second), wantErr: ErrInvalidSignature},
		{name: "tampered expired verdict", data: tampered, now: testNow.Add(MaxTTLSeconds * time.Second), wantErr: ErrInvalidSignature},
		{name: "documentation range", data: signedJSON(func() *Event {
			e := validVerdict()
			e.Indicator = Indicator{Kind: KindIPv4, Value: "203.0.113.7", Scope: "/32"}
			return e
		}()), now: testNow, wantErr: ErrNonPublicIndicator},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := Receive(tt.data, WithClock(func() time.Time { return tt.now }), AllowDocumentationRanges())
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Receive() error = %v, want %v", err, tt.wantErr)
			}
			if (e == nil) == (err == nil) {
				t.Errorf("Receive() = %v, %v; want exactly one of event and error", e, err)
			}
		})
	}
}

func TestReceiveDocumentationRanges(t *testing.T) {
	e := validVerdict()
	e.Indicator = Indicator{Kind: KindIPv4, Value: "203.0.113.7", Scope: "/32"}
	data := mustMarshal(t, signedBy(t, e, testSeedA))
	clock := WithClock(func() time.Time { return testNow })
	if _, err := Receive(data, clock, ReceiveDocumentationRanges()); err != nil {
		t.Fatalf("Receive() with ReceiveDocumentationRanges = %v, want the event", err)
	}
	if _, err := Decode(data, clock, ReceiveDocumentationRanges()); !errors.Is(err, ErrNonPublicIndicator) {
		t.Errorf("Decode() with ReceiveDocumentationRanges = %v, want %v: the option only affects Receive", err, ErrNonPublicIndicator)
	}
}

// TestReceiveVectors checks that a relay accepts exactly the vectors that
// are both protocol-valid and correctly signed.
func TestReceiveVectors(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(vectorDir, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no test vectors: %v", err)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			v := readVector(t, file)
			var e Event
			if err := json.Unmarshal(v.Event, &e); err != nil {
				t.Fatal(err)
			}
			_, err := Receive(v.Event, WithClock(func() time.Time { return e.IssuedAt.Time }))
			if want := v.ProtocolValid && v.Valid; (err == nil) != want {
				t.Errorf("Receive() error = %v, want accepted = %v", err, want)
			}
		})
	}
}
