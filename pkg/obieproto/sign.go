package obieproto

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto/internal/jcs"
)

// CanonicalBytes returns the bytes an event's signature covers: the RFC 8785
// (JSON Canonicalization Scheme) form of the event's JSON encoding with
// publisher.signature removed. Strings must be valid UTF-8, integers must lie
// within ±(2^53-1) and issued_at must have whole seconds, so that every
// field is covered exactly; otherwise the error matches [ErrMalformed].
func CanonicalBytes(e *Event) ([]byte, error) {
	if e == nil {
		return nil, &FieldError{Err: ErrMalformed, Detail: "nil event"}
	}
	// encoding/json silently replaces invalid UTF-8 with U+FFFD, which would
	// let two different events share one signature.
	if path, bad := invalidUTF8(reflect.ValueOf(*e)); bad {
		return nil, &FieldError{Field: strings.TrimPrefix(path, "."), Err: ErrMalformed, Detail: "not valid UTF-8"}
	}
	// The JSON encoding drops sub-second precision, which the signature would
	// then not cover.
	if e.IssuedAt.Nanosecond() != 0 {
		return nil, &FieldError{Field: "issued_at", Err: ErrMalformed, Detail: "has sub-second precision"}
	}
	data, err := json.Marshal(e)
	if err != nil {
		return nil, &FieldError{Err: ErrMalformed, Detail: err.Error()}
	}
	// json.Marshal output is valid I-JSON apart from possibly out-of-range
	// integers, which jcs.Encode rejects, so the strict and slower jcs.Parse
	// is not needed.
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v map[string]any
	if err := dec.Decode(&v); err != nil {
		return nil, &FieldError{Err: ErrMalformed, Detail: err.Error()}
	}
	// The type assertion holds for anything json.Marshal produces from an Event.
	delete(v["publisher"].(map[string]any), "signature")
	canonical, err := jcs.Encode(v)
	if err != nil {
		return nil, &FieldError{Err: ErrMalformed, Detail: err.Error()}
	}
	return canonical, nil
}

// invalidUTF8 walks the exported fields of v and reports whether a string
// is not valid UTF-8, and if so its Go path.
func invalidUTF8(v reflect.Value) (path string, bad bool) {
	switch v.Kind() {
	case reflect.String:
		return "", !utf8.ValidString(v.String())
	case reflect.Pointer:
		if !v.IsNil() {
			return invalidUTF8(v.Elem())
		}
	case reflect.Slice:
		for i := range v.Len() {
			if p, bad := invalidUTF8(v.Index(i)); bad {
				return fmt.Sprintf("[%d]%s", i, p), true
			}
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if f := v.Type().Field(i); f.IsExported() {
				if p, bad := invalidUTF8(v.Field(i)); bad {
					return "." + f.Name + p, true
				}
			}
		}
	}
	return "", false
}

// Sign signs the event with key and stores the result in
// publisher.signature, replacing any previous signature. publisher.peer_id
// must already be the peer ID of key (see [PeerIDFromPublicKey]); otherwise
// Sign returns an error matching [ErrPublisherMismatch]. Sign does not
// validate the event; call [Event.Validate] first. On error the event is left
// unchanged.
//
// Ed25519 signatures are deterministic: signing the same event with the same
// key always yields the same signature.
func Sign(e *Event, key ed25519.PrivateKey) error {
	if len(key) != ed25519.PrivateKeySize {
		return fmt.Errorf("obieproto: Ed25519 private key has %d bytes, want %d", len(key), ed25519.PrivateKeySize)
	}
	// Rebuild the key from its seed so that a corrupted public half cannot
	// produce signatures that nobody can verify.
	key = ed25519.NewKeyFromSeed(key.Seed())
	peerID, err := PeerIDFromPublicKey(key.Public().(ed25519.PublicKey))
	if err != nil {
		return err
	}
	if e != nil && e.Publisher.PeerID != peerID {
		return &FieldError{Field: "publisher.peer_id", Err: ErrPublisherMismatch,
			Detail: fmt.Sprintf("%q is not the peer ID %q of the signing key", e.Publisher.PeerID, peerID)}
	}
	msg, err := CanonicalBytes(e)
	if err != nil {
		return err
	}
	e.Publisher.Signature = signaturePrefix + base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, msg))
	return nil
}

// Signer produces Ed25519 signatures with a key that the caller does not
// hand out, such as the node identity.
type Signer interface {
	// PeerID returns the libp2p peer ID of the signing key.
	PeerID() string
	// Sign returns the Ed25519 signature of msg.
	Sign(msg []byte) []byte
}

// SignWith is [Sign] for a key held by signer: it sets publisher.peer_id to
// the signer's peer ID, signs the event and checks the result with
// [Verify], so a signer whose peer ID does not belong to its key is caught
// (the error matches [ErrPublisherMismatch] or [ErrInvalidSignature]). On
// error the event is left unchanged.
func SignWith(e *Event, signer Signer) error {
	if e == nil {
		return &FieldError{Err: ErrMalformed, Detail: "nil event"}
	}
	signed := *e
	signed.Publisher.PeerID = signer.PeerID()
	msg, err := CanonicalBytes(&signed)
	if err != nil {
		return err
	}
	signed.Publisher.Signature = signaturePrefix + base64.RawURLEncoding.EncodeToString(signer.Sign(msg))
	if err := Verify(&signed); err != nil {
		return err
	}
	*e = signed
	return nil
}

// Verify checks publisher.signature against the Ed25519 public key embedded
// in publisher.peer_id; no key lookup is involved. It returns nil only if the
// signature covers exactly this event's [CanonicalBytes]. A missing,
// malformed or non-matching signature yields an error matching
// [ErrInvalidSignature]; a peer ID that does not embed a usable Ed25519 key
// (including weak keys of small order) yields [ErrPublisherMismatch].
//
// Verify checks the signature only. Receivers obtain the event from [Decode],
// which enforces every obie/0.1 rule, and must verify it before acting on it.
func Verify(e *Event) error {
	if e == nil {
		return &FieldError{Err: ErrMalformed, Detail: "nil event"}
	}
	if e.Publisher.Signature == "" {
		return &FieldError{Field: "publisher.signature", Err: ErrInvalidSignature, Detail: "missing"}
	}
	sig, err := decodeSignature(e.Publisher.Signature)
	if err != nil {
		return &FieldError{Field: "publisher.signature", Err: ErrInvalidSignature, Detail: err.Error()}
	}
	pub, err := PublicKeyFromPeerID(e.Publisher.PeerID)
	if err != nil {
		return &FieldError{Field: "publisher.peer_id", Err: ErrPublisherMismatch, Detail: err.Error()}
	}
	msg, err := CanonicalBytes(e)
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, msg, sig) {
		return &FieldError{Field: "publisher.signature", Err: ErrInvalidSignature,
			Detail: "does not match the event and the publisher's key"}
	}
	return nil
}

// decodeSignature parses "ed25519:" followed by 64 bytes of unpadded
// base64url.
func decodeSignature(s string) ([]byte, error) {
	encoded, ok := strings.CutPrefix(s, signaturePrefix)
	if !ok {
		return nil, fmt.Errorf("must start with %q", signaturePrefix)
	}
	sig, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, fmt.Errorf("must be %d bytes of unpadded base64url", ed25519.SignatureSize)
	}
	return sig, nil
}
