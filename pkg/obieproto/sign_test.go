package obieproto

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// signedBy returns e with publisher.peer_id set to the peer ID of seedHex's
// key and signed with that key.
func signedBy(t testing.TB, e *Event, seedHex string) *Event {
	t.Helper()
	key := testKey(t, seedHex)
	peerID, err := PeerIDFromPublicKey(key.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	e.Publisher.PeerID = peerID
	if err := Sign(e, key); err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	return e
}

func TestSignVerifyRoundTrip(t *testing.T) {
	for name, build := range map[string]func() *Event{"verdict": validVerdict, "revoke": validRevoke} {
		t.Run(name, func(t *testing.T) {
			e := signedBy(t, build(), testSeedA)
			if !strings.HasPrefix(e.Publisher.Signature, "ed25519:") || strings.Contains(e.Publisher.Signature, "=") {
				t.Errorf("signature %q is not ed25519: + unpadded base64url", e.Publisher.Signature)
			}
			if err := Verify(e); err != nil {
				t.Errorf("Verify() error = %v", err)
			}
			if err := e.Validate(WithClock(func() time.Time { return testNow })); err != nil {
				t.Errorf("signed event fails Validate: %v", err)
			}
			// Sign replaces a previous signature and is deterministic.
			again := build()
			again.Publisher.Signature = "ed25519:stale"
			if signedBy(t, again, testSeedA).Publisher.Signature != e.Publisher.Signature {
				t.Error("signing the same event twice gave different signatures")
			}
		})
	}
}

func TestVerifyAfterWireRoundTrip(t *testing.T) {
	e := signedBy(t, validVerdict(), testSeedB)
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(data, WithClock(func() time.Time { return testNow }))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if err := Verify(decoded); err != nil {
		t.Errorf("Verify(decoded) error = %v", err)
	}
}

func TestCanonicalBytes(t *testing.T) {
	e := validRevoke()
	want := `{"id":"01923e4a-7b2c-7def-9a12-3456789abcdf","indicator":{"kind":"ipv4","scope":"/32","value":"85.10.20.30"},` +
		`"issued_at":"2026-01-05T01:50:00Z","publisher":{"peer_id":"` + testPeerID + `"},"reason":"false_positive",` +
		`"revokes":"01923e4a-7b2c-7def-8a12-3456789abcde","spec":"obie/0.1","type":"indicator.revoke"}`
	got, err := CanonicalBytes(e)
	if err != nil {
		t.Fatalf("CanonicalBytes() error = %v", err)
	}
	if string(got) != want {
		t.Errorf("CanonicalBytes() =\n%s\nwant\n%s", got, want)
	}
	e.Publisher.Signature = ""
	if unsigned, _ := CanonicalBytes(e); string(unsigned) != want {
		t.Error("CanonicalBytes() depends on publisher.signature")
	}
}

func TestSignRejects(t *testing.T) {
	keyA := testKey(t, testSeedA)
	tests := []struct {
		name    string
		event   func() *Event
		key     ed25519.PrivateKey
		wantErr error
	}{
		{
			name:    "peer ID of another key",
			event:   func() *Event { e := validVerdict(); e.Publisher.PeerID = testPeerIDB; return e },
			key:     keyA,
			wantErr: ErrPublisherMismatch,
		},
		{
			name:    "peer ID without embedded key",
			event:   func() *Event { e := validVerdict(); e.Publisher.PeerID = testPeerID + "x"; return e },
			key:     keyA,
			wantErr: ErrPublisherMismatch,
		},
		{
			name: "invalid UTF-8",
			event: func() *Event {
				e := validVerdict()
				e.Publisher.PeerID = testPeerIDA
				e.MITRE = []string{"T1110", "T\xff"}
				return e
			},
			key:     keyA,
			wantErr: ErrMalformed,
		},
		{
			name: "integer beyond 2^53-1",
			event: func() *Event {
				e := validVerdict()
				e.Publisher.PeerID = testPeerIDA
				e.Evidence.Events = 1 << 53
				return e
			},
			key:     keyA,
			wantErr: ErrMalformed,
		},
		{
			name: "sub-second issued_at",
			event: func() *Event {
				e := validVerdict()
				e.Publisher.PeerID = testPeerIDA
				e.IssuedAt.Time = e.IssuedAt.Add(500 * time.Millisecond)
				return e
			},
			key:     keyA,
			wantErr: ErrMalformed,
		},
		{
			name:    "nil event",
			event:   func() *Event { return nil },
			key:     keyA,
			wantErr: ErrMalformed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := tt.event()
			var before *Event
			if e != nil {
				c := *e
				before = &c
			}
			err := Sign(e, tt.key)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Sign() error = %v, want %v", err, tt.wantErr)
			}
			if e != nil && !reflect.DeepEqual(*e, *before) {
				t.Error("Sign() modified the event despite failing")
			}
		})
	}
}

func TestSignRejectsMalformedKey(t *testing.T) {
	e := validVerdict()
	e.Publisher.PeerID = testPeerIDA
	if err := Sign(e, make(ed25519.PrivateKey, 32)); err == nil {
		t.Error("Sign() with a 32-byte key: error = nil, want error")
	}
}

// TestSignIgnoresCorruptPublicHalf checks that a private key whose public
// half does not match its seed still produces verifiable signatures.
func TestSignIgnoresCorruptPublicHalf(t *testing.T) {
	key := testKey(t, testSeedA)
	corrupt := append(ed25519.PrivateKey{}, key[:32]...)
	corrupt = append(corrupt, testKey(t, testSeedB)[32:]...)
	e := validVerdict()
	e.Publisher.PeerID = testPeerIDA
	if err := Sign(e, corrupt); err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	if err := Verify(e); err != nil {
		t.Errorf("Verify() error = %v", err)
	}
}

func TestVerifyRejects(t *testing.T) {
	sigOf := func(e *Event) string { return strings.TrimPrefix(e.Publisher.Signature, "ed25519:") }
	tests := []struct {
		name    string
		mutate  func(e *Event)
		wantErr error
	}{
		{"missing signature", func(e *Event) { e.Publisher.Signature = "" }, ErrInvalidSignature},
		{"no prefix", func(e *Event) { e.Publisher.Signature = sigOf(e) }, ErrInvalidSignature},
		{"wrong prefix", func(e *Event) { e.Publisher.Signature = "ed448:" + sigOf(e) }, ErrInvalidSignature},
		{"upper-case prefix", func(e *Event) { e.Publisher.Signature = "ED25519:" + sigOf(e) }, ErrInvalidSignature},
		{"padded base64", func(e *Event) { e.Publisher.Signature += "==" }, ErrInvalidSignature},
		{"standard base64 alphabet", func(e *Event) {
			raw, _ := base64.RawURLEncoding.DecodeString(sigOf(e))
			raw[0], raw[1] = 0xfb, 0xff // encodes to "-_" in base64url, "+/" in base64
			e.Publisher.Signature = "ed25519:" + base64.RawStdEncoding.EncodeToString(raw)
		}, ErrInvalidSignature},
		{"garbled", func(e *Event) { e.Publisher.Signature = "ed25519:!!not base64!!" }, ErrInvalidSignature},
		{"truncated", func(e *Event) { e.Publisher.Signature = e.Publisher.Signature[:len(e.Publisher.Signature)-2] }, ErrInvalidSignature},
		{"flipped signature bit", func(e *Event) {
			raw, _ := base64.RawURLEncoding.DecodeString(sigOf(e))
			raw[10] ^= 0x01
			e.Publisher.Signature = "ed25519:" + base64.RawURLEncoding.EncodeToString(raw)
		}, ErrInvalidSignature},
		{"signature by a different key", func(e *Event) {
			msg, err := CanonicalBytes(e)
			if err != nil {
				panic(err)
			}
			e.Publisher.Signature = "ed25519:" + base64.RawURLEncoding.EncodeToString(ed25519.Sign(testKey(t, testSeedB), msg))
		}, ErrInvalidSignature},
		{"peer ID of a different key", func(e *Event) { e.Publisher.PeerID = testPeerIDB }, ErrInvalidSignature},
		{"peer ID without embedded key", func(e *Event) {
			e.Publisher.PeerID = "QmYyQSo1c1Ym7orWxLYvCrM2EmxFTANf8wXmmE7DWjhx5N"
		}, ErrPublisherMismatch},
		{"garbled peer ID", func(e *Event) { e.Publisher.PeerID = "0OIl" }, ErrPublisherMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := signedBy(t, validVerdict(), testSeedA)
			tt.mutate(e)
			if err := Verify(e); !errors.Is(err, tt.wantErr) {
				t.Errorf("Verify() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
	if err := Verify(nil); !errors.Is(err, ErrMalformed) {
		t.Errorf("Verify(nil) error = %v, want %v", err, ErrMalformed)
	}
}

// TestVerifyRejectsWeakKeyForgery shows why weak keys must be rejected: with
// the identity point as public key, crypto/ed25519 accepts the signature
// (R = identity, S = 0) for any message.
func TestVerifyRejectsWeakKeyForgery(t *testing.T) {
	weak := mustHex(t, weakPublicKey)
	for _, value := range []string{"85.10.20.30", "1.2.3.4"} {
		e := validVerdict()
		e.Indicator.Value = value
		e.Publisher.PeerID = rawPeerID(weak)
		e.Publisher.Signature = "ed25519:" + base64.RawURLEncoding.EncodeToString(forgedSignature)
		msg, err := CanonicalBytes(e)
		if err != nil {
			t.Fatal(err)
		}
		if !ed25519.Verify(weak, msg, forgedSignature) {
			t.Fatal("crypto/ed25519 rejects the forgery; the test premise no longer holds")
		}
		if err := Verify(e); !errors.Is(err, ErrPublisherMismatch) {
			t.Errorf("Verify(forged %s) error = %v, want %v", value, err, ErrPublisherMismatch)
		}
	}
}

// TestVerifyDetectsTampering changes every signed field of a verdict and a
// revocation after signing.
func TestVerifyDetectsTampering(t *testing.T) {
	asn := uint32(64497)
	verdictTampers := map[string]func(e *Event){
		"id":                       func(e *Event) { e.ID = testOtherID },
		"spec":                     func(e *Event) { e.Spec = "obie/0.2" },
		"type":                     func(e *Event) { e.Type = TypeRevoke },
		"issued_at":                func(e *Event) { e.IssuedAt = NewTimestamp(testNow.Add(time.Second)) },
		"indicator.kind":           func(e *Event) { e.Indicator.Kind = KindCIDR },
		"indicator.value":          func(e *Event) { e.Indicator.Value = "85.10.20.31" },
		"indicator.scope":          func(e *Event) { e.Indicator.Scope = "/31" },
		"protocol":                 func(e *Event) { e.Protocol = "smtp" },
		"protocol removed":         func(e *Event) { e.Protocol = "" },
		"evidence.events":          func(e *Event) { e.Evidence.Events++ },
		"evidence.reason":          func(e *Event) { e.Evidence.Reason = "port_scan" },
		"evidence.log_hash":        func(e *Event) { e.Evidence.LogHash = "sha256:" + strings.Repeat("cd", 32) },
		"evidence.log_hash absent": func(e *Event) { e.Evidence.LogHash = "" },
		"evidence.honeypot":        func(e *Event) { e.Evidence.Honeypot = false },
		"evidence removed":         func(e *Event) { e.Evidence = nil },
		"verdict.suggested_action": func(e *Event) { e.Verdict.SuggestedAction = ActionWatch },
		"verdict.confidence":       func(e *Event) { e.Verdict.Confidence = 0.9200000000000002 },
		"verdict.ttl_seconds":      func(e *Event) { e.Verdict.TTLSeconds-- },
		"mitre reordered":          func(e *Event) { e.MITRE[0], e.MITRE[1] = e.MITRE[1], e.MITRE[0] },
		"mitre extended":           func(e *Event) { e.MITRE = append(e.MITRE, "T1021") },
		"mitre removed":            func(e *Event) { e.MITRE = nil },
		"revokes added":            func(e *Event) { e.Revokes = testOtherID },
		"publisher.asn":            func(e *Event) { e.Publisher.ASN = &asn },
		"publisher.asn removed":    func(e *Event) { e.Publisher.ASN = nil },
	}
	revokeTampers := map[string]func(e *Event){
		"revokes":        func(e *Event) { e.Revokes = "01923e4a-7b2c-7def-8a12-3456789abcdf" },
		"reason":         func(e *Event) { e.Reason = "expired" },
		"publisher.asn":  func(e *Event) { e.Publisher.ASN = &asn },
		"protocol added": func(e *Event) { e.Protocol = "ssh" },
	}
	run := func(build func() *Event, tampers map[string]func(e *Event)) {
		for name, tamper := range tampers {
			t.Run(name, func(t *testing.T) {
				e := signedBy(t, build(), testSeedA)
				tamper(e)
				if err := Verify(e); !errors.Is(err, ErrInvalidSignature) {
					t.Errorf("Verify() after tampering with %s: error = %v, want %v", name, err, ErrInvalidSignature)
				}
			})
		}
	}
	run(validVerdict, verdictTampers)
	run(validRevoke, revokeTampers)
}

// FuzzVerify checks that Verify never panics on arbitrary input, whether the
// bytes are only JSON-unmarshalled into an Event or also pass Decode.
func FuzzVerify(f *testing.F) {
	for _, build := range []func() *Event{validVerdict, validRevoke} {
		data, err := json.Marshal(signedBy(f, build(), testSeedA))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Add([]byte(`{"publisher":{"peer_id":"` + testPeerIDA + `","signature":"ed25519:"}}`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(_ *testing.T, data []byte) {
		var e Event
		if json.Unmarshal(data, &e) != nil {
			return
		}
		_ = Verify(&e)
		if decoded, err := Decode(data); err == nil {
			_ = Verify(decoded)
		}
	})
}

// BenchmarkVerify reports how many events one core verifies per second.
func BenchmarkVerify(b *testing.B) {
	e := signedBy(b, validVerdict(), testSeedA)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := Verify(e); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "events/s")
}

func BenchmarkSign(b *testing.B) {
	e := signedBy(b, validVerdict(), testSeedA)
	key := testKey(b, testSeedA)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := Sign(e, key); err != nil {
			b.Fatal(err)
		}
	}
}
