package obieproto

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mr-tron/base58"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto/internal/jcs"
)

// updateVectors rewrites the published test vectors; `go generate` sets it.
var updateVectors = flag.Bool("update", false, "rewrite the test vectors in "+vectorDir)

// vectorDir holds the published signing test vectors.
const vectorDir = "../../documentation/spec/test-vectors"

// vectorTime is issued_at of every vector event.
var vectorTime = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

// testVector is the file format of a published test vector.
type testVector struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// PrivateKeySeed is the 32-byte Ed25519 seed (RFC 8032) of the key that
	// made the signature, in hex. It is public and for tests only. A forged
	// signature has no seed.
	PrivateKeySeed string `json:"private_key_seed,omitempty"`
	// PublicKey is the public key of PrivateKeySeed, in hex, or the forger's
	// weak key.
	PublicKey string `json:"public_key"`
	// Event is the event as a receiver gets it, signature included.
	Event json.RawMessage `json:"event"`
	// Canonical is the hex of the RFC 8785 form of Event without
	// publisher.signature: the bytes a verifier checks the signature against.
	Canonical string `json:"canonical"`
	// Signature is Event's publisher.signature.
	Signature string `json:"signature"`
	// ProtocolValid reports whether Event satisfies every obie/0.1 field rule
	// (Decode accepts it at issued_at).
	ProtocolValid bool `json:"protocol_valid"`
	// Valid reports whether the signature verifies.
	Valid bool `json:"valid"`
	// Error classifies why an invalid vector is rejected: vectorErrSignature
	// or vectorErrPublisher.
	Error string `json:"error,omitempty"`
}

// Values of testVector.Error.
const (
	vectorErrSignature = "invalid_signature"
	vectorErrPublisher = "publisher_mismatch"
)

// vectorErrors maps testVector.Error to the sentinel Verify returns.
var vectorErrors = map[string]error{vectorErrSignature: ErrInvalidSignature, vectorErrPublisher: ErrPublisherMismatch}

// weakPublicKey is the identity point: a small-order Ed25519 public key for
// which the signature (R = identity, S = 0) verifies every message under
// plain RFC 8032 verification.
const weakPublicKey = "0100000000000000000000000000000000000000000000000000000000000000"

// forgedSignature is (R = identity, S = 0).
var forgedSignature = append(mustHexConst(weakPublicKey), make([]byte, 32)...)

func mustHexConst(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// rawPeerID builds a peer ID without checking the key, as a forger would.
func rawPeerID(pub []byte) string {
	return base58.Encode(append(bytes.Clone(ed25519PeerIDPrefix), pub...))
}

// vectorSpec describes how to build one vector.
type vectorSpec struct {
	name, description string
	// signer is the seed of the key that signs; peerOf that of the key whose
	// peer ID is the publisher (normally the same).
	signer, peerOf string
	event          func() *Event
	// tamper changes the event after signing (negative vectors).
	tamper func(e *Event)
	// forged replaces key and signature with weakPublicKey and
	// forgedSignature; signer and peerOf are ignored.
	forged        bool
	protocolValid bool
	valid         bool
	// err is testVector.Error.
	err string
}

func vectorSpecs() []vectorSpec {
	return []vectorSpec{
		{
			name:        "01-verdict-ipv4",
			description: "Verdict about a single IPv4 address with every optional field set.",
			signer:      testSeedA, peerOf: testSeedA,
			event: func() *Event {
				asn := uint32(24940)
				return &Event{
					ID: "0199a1b2-c3d4-7e5f-8a6b-7c8d9e0f1a01", Spec: Spec, Type: TypeVerdict,
					IssuedAt:  NewTimestamp(vectorTime),
					Indicator: Indicator{Kind: KindIPv4, Value: "85.10.20.30", Scope: "/32"},
					Protocol:  "ssh",
					Evidence: &Evidence{
						Events: 47, Reason: "password_bruteforce",
						LogHash:  "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
						Honeypot: true,
					},
					Verdict:   &Verdict{SuggestedAction: ActionBan, Confidence: 0.92, TTLSeconds: 604800},
					MITRE:     []string{"T1110", "T1110.001"},
					Publisher: Publisher{ASN: &asn},
				}
			},
			protocolValid: true, valid: true,
		},
		{
			name:        "02-verdict-ipv6",
			description: "Verdict about a single IPv6 address; optional fields (asn, log_hash, mitre) are omitted and confidence is the integer 1.",
			signer:      testSeedB, peerOf: testSeedB,
			event: func() *Event {
				return &Event{
					ID: "0199a1b2-c3d4-7e5f-8a6b-7c8d9e0f1a02", Spec: Spec, Type: TypeVerdict,
					IssuedAt:  NewTimestamp(vectorTime),
					Indicator: Indicator{Kind: KindIPv6, Value: "2a01:4f8:c17:b8f::2", Scope: "/128"},
					Protocol:  "http",
					Evidence:  &Evidence{Events: 3, Reason: "path_traversal"},
					Verdict:   &Verdict{SuggestedAction: ActionWatch, Confidence: 1, TTLSeconds: 3600},
				}
			},
			protocolValid: true, valid: true,
		},
		{
			name:        "03-verdict-cidr",
			description: "Verdict about an IPv4 CIDR range with a confidence that needs ECMAScript number formatting.",
			signer:      testSeedA, peerOf: testSeedA,
			event: func() *Event {
				return &Event{
					ID: "0199a1b2-c3d4-7e5f-8a6b-7c8d9e0f1a03", Spec: Spec, Type: TypeVerdict,
					IssuedAt:  NewTimestamp(vectorTime),
					Indicator: Indicator{Kind: KindCIDR, Value: "45.83.64.0/22", Scope: "/22"},
					Protocol:  "smtp",
					Evidence:  &Evidence{Events: 1200, Reason: "credential_stuffing", Honeypot: false},
					Verdict:   &Verdict{SuggestedAction: ActionBan, Confidence: 1.0 / 3, TTLSeconds: 86400},
					MITRE:     []string{"T1110.004"},
				}
			},
			protocolValid: true, valid: true,
		},
		{
			name:        "04-revoke",
			description: "Revocation of the verdict of vector 01 by the same publisher.",
			signer:      testSeedA, peerOf: testSeedA,
			event: func() *Event {
				return &Event{
					ID: "0199a1b2-c3d4-7e5f-8a6b-7c8d9e0f1a04", Spec: Spec, Type: TypeRevoke,
					IssuedAt:  NewTimestamp(vectorTime.Add(time.Hour)),
					Indicator: Indicator{Kind: KindIPv4, Value: "85.10.20.30", Scope: "/32"},
					Revokes:   "0199a1b2-c3d4-7e5f-8a6b-7c8d9e0f1a01",
					Reason:    "false_positive",
				}
			},
			protocolValid: true, valid: true,
		},
		{
			name: "05-unicode-string",
			description: "Exercises RFC 8785 string canonicalization: evidence.reason holds non-ASCII characters " +
				"(including one outside the Basic Multilingual Plane), a quotation mark, a backslash, HTML characters and " +
				"control characters. The reason pattern of obie/0.1 forbids these, so Decode rejects the event, " +
				"but the signature is valid: signing and verification do not depend on field validation.",
			signer: testSeedA, peerOf: testSeedA,
			event: func() *Event {
				return &Event{
					ID: "0199a1b2-c3d4-7e5f-8a6b-7c8d9e0f1a05", Spec: Spec, Type: TypeVerdict,
					IssuedAt:  NewTimestamp(vectorTime),
					Indicator: Indicator{Kind: KindIPv4, Value: "85.10.20.30", Scope: "/32"},
					Protocol:  "ssh",
					Evidence: &Evidence{
						Events: 5,
						Reason: "gr\u00fc\u00dfe_\u20ac_\U0001F600_\"q\"_\\_<&>_\t\n\x01\x7f\u2028",
					},
					Verdict: &Verdict{SuggestedAction: ActionWatch, Confidence: 0.5, TTLSeconds: 60},
				}
			},
			protocolValid: false, valid: true,
		},
		{
			name: "06-tampered",
			description: "Negative: vector 01 with indicator.value changed from 85.10.20.30 to 85.10.20.31 after signing. " +
				"canonical is the form of the tampered event; the signature does not verify.",
			signer: testSeedA, peerOf: testSeedA,
			event: func() *Event { return vectorSpecs()[0].event() },
			tamper: func(e *Event) {
				e.Indicator.Value = "85.10.20.31"
			},
			protocolValid: true, valid: false, err: vectorErrSignature,
		},
		{
			name: "07-wrong-key",
			description: "Negative: publisher.peer_id is the peer ID of public key A (vector 01), but the signature " +
				"was made by the key of private_key_seed (key B). The signature does not verify.",
			signer: testSeedB, peerOf: testSeedA,
			event:         func() *Event { return vectorSpecs()[0].event() },
			protocolValid: true, valid: false, err: vectorErrSignature,
		},
		{
			name: "08-weak-key",
			description: "Negative: publisher.peer_id embeds the identity point, a public key of small order, and the " +
				"signature is (R = identity, S = 0). Plain RFC 8032 verification accepts this signature for any event, " +
				"so verifiers must reject public keys that are small-order or not canonically encoded.",
			event:         func() *Event { return vectorSpecs()[0].event() },
			forged:        true,
			protocolValid: true, valid: false, err: vectorErrPublisher,
		},
	}
}

// buildVector produces the file content of one vector.
func buildVector(t *testing.T, spec vectorSpec) []byte {
	t.Helper()
	e := spec.event()
	// pub is the signer's public key, publisher that of the peer ID.
	pub, publisher := mustHex(t, weakPublicKey), mustHex(t, weakPublicKey)
	sign := func([]byte) []byte { return forgedSignature }
	if !spec.forged {
		signer := testKey(t, spec.signer)
		pub = signer.Public().(ed25519.PublicKey)
		publisher = testKey(t, spec.peerOf).Public().(ed25519.PublicKey)
		// ed25519.Sign rather than Sign, so that 07 can sign for another peer ID.
		sign = func(msg []byte) []byte { return ed25519.Sign(signer, msg) }
	}
	e.Publisher.PeerID = rawPeerID(publisher)
	msg, err := CanonicalBytes(e)
	if err != nil {
		t.Fatalf("%s: CanonicalBytes() error = %v", spec.name, err)
	}
	e.Publisher.Signature = signaturePrefix + base64.RawURLEncoding.EncodeToString(sign(msg))
	if spec.tamper != nil {
		spec.tamper(e)
	}
	if msg, err = CanonicalBytes(e); err != nil {
		t.Fatalf("%s: CanonicalBytes() error = %v", spec.name, err)
	}
	eventJSON, err := marshalVector(e)
	if err != nil {
		t.Fatal(err)
	}
	data, err := marshalVector(testVector{
		Name:           spec.name,
		Description:    spec.description,
		PrivateKeySeed: spec.signer,
		PublicKey:      hex.EncodeToString(pub),
		Event:          eventJSON,
		Canonical:      hex.EncodeToString(msg),
		Signature:      e.Publisher.Signature,
		ProtocolValid:  spec.protocolValid,
		Valid:          spec.valid,
		Error:          spec.err,
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// marshalVector encodes v indented and without HTML escaping, so that the
// files stay readable.
func marshalVector(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// TestVectors regenerates the published test vectors and compares them with
// the files in documentation/spec/test-vectors. Run `go generate` in this
// package to rewrite them after an intended change.
func TestVectors(t *testing.T) {
	want := map[string]bool{}
	for _, spec := range vectorSpecs() {
		file := filepath.Join(vectorDir, spec.name+".json")
		want[filepath.Base(file)] = true
		data := buildVector(t, spec)
		if *updateVectors {
			if err := os.WriteFile(file, data, 0o644); err != nil { //nolint:gosec // documentation, world-readable by design
				t.Fatal(err)
			}
			continue
		}
		onDisk, err := os.ReadFile(file) //nolint:gosec // fixed path below the repository
		if err != nil {
			t.Fatalf("%v (run `go generate ./pkg/obieproto` to create the vectors)", err)
		}
		if !bytes.Equal(onDisk, data) {
			t.Errorf("%s is out of date; run `go generate ./pkg/obieproto` if the change is intended", file)
		}
	}
	files, err := filepath.Glob(filepath.Join(vectorDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if !want[filepath.Base(f)] {
			t.Errorf("%s is not generated by TestVectors; remove it", f)
		}
	}
}

// TestVectorFiles checks each published vector using only its file content,
// the way an independent implementation would.
func TestVectorFiles(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(vectorDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 5 {
		t.Fatalf("found %d test vectors, want at least 5", len(files))
	}
	var sawInvalid bool
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			v := readVector(t, file)
			sawInvalid = sawInvalid || !v.Valid
			checkVector(t, v)
		})
	}
	if !sawInvalid {
		t.Error("no negative test vector")
	}
}

func readVector(t *testing.T, file string) testVector {
	t.Helper()
	data, err := os.ReadFile(file) //nolint:gosec // fixed path below the repository
	if err != nil {
		t.Fatal(err)
	}
	var v testVector
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func checkVector(t *testing.T, v testVector) {
	var key ed25519.PrivateKey
	if v.PrivateKeySeed != "" {
		key = testKey(t, v.PrivateKeySeed)
		if got := hex.EncodeToString(key.Public().(ed25519.PublicKey)); got != v.PublicKey {
			t.Errorf("public_key = %s, want %s derived from the seed", v.PublicKey, got)
		}
	}
	canonical := mustHex(t, v.Canonical)

	// The canonical bytes follow from the event JSON by generic JCS alone.
	generic, err := jcs.Parse(v.Event)
	if err != nil {
		t.Fatal(err)
	}
	delete(generic.(map[string]any)["publisher"].(map[string]any), "signature")
	if got, err := jcs.Encode(generic); err != nil || !bytes.Equal(got, canonical) {
		t.Errorf("JCS(event without signature) = %s, %v; want %s", got, err, canonical)
	}

	var e Event
	if err := json.Unmarshal(v.Event, &e); err != nil {
		t.Fatal(err)
	}
	if got, err := CanonicalBytes(&e); err != nil || !bytes.Equal(got, canonical) {
		t.Errorf("CanonicalBytes() = %s, %v; want %s", got, err, canonical)
	}
	if e.Publisher.Signature != v.Signature {
		t.Errorf("event signature %q differs from signature %q", e.Publisher.Signature, v.Signature)
	}
	_, err = Decode(v.Event, WithClock(func() time.Time { return e.IssuedAt.Time }))
	if (err == nil) != v.ProtocolValid {
		t.Errorf("Decode() error = %v, want protocol_valid = %v", err, v.ProtocolValid)
	}

	verifyErr := Verify(&e)
	if !v.Valid {
		if want := vectorErrors[v.Error]; want == nil || !errors.Is(verifyErr, want) {
			t.Errorf("Verify() = %v, want error %q", verifyErr, v.Error)
		}
		return
	}
	resigned := signaturePrefix + base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, canonical))
	if verifyErr != nil || resigned != v.Signature {
		t.Errorf("Verify() = %v and re-signing gives %s; want a valid signature %s", verifyErr, resigned, v.Signature)
	}
}

// TestVectorCoverage makes sure the vectors cover what WP-1651 promises.
func TestVectorCoverage(t *testing.T) {
	var kinds, types []string
	var unicode, tampered bool
	for _, spec := range vectorSpecs() {
		e := spec.event()
		kinds = append(kinds, e.Indicator.Kind)
		types = append(types, e.Type)
		unicode = unicode || e.Evidence != nil && strings.ContainsFunc(e.Evidence.Reason, func(r rune) bool { return r > 0x7f })
		tampered = tampered || spec.tamper != nil
	}
	for _, need := range []string{KindIPv4, KindIPv6, KindCIDR} {
		if !slices.Contains(kinds, need) {
			t.Errorf("no vector with a %s indicator", need)
		}
	}
	if !slices.Contains(types, TypeRevoke) {
		t.Error("no revocation vector")
	}
	if !unicode || !tampered {
		t.Errorf("unicode vector: %v, tampered vector: %v; want both", unicode, tampered)
	}
}
