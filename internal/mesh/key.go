package mesh

import (
	"errors"
	"fmt"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/crypto/pb"

	"github.com/MNCloudwerksTechnology/obie/internal/identity"
)

// rawKeyDomain is the message whose signature stands in for the raw private
// key (see hostKey.Raw). No OBIE event (a JSON object) and no libp2p
// signature payload (all prefixed with their own domain) can equal it.
const rawKeyDomain = "obie/0.1 mesh: QUIC key material, not an event"

// hostKey adapts the node identity to libp2p's crypto.PrivKey without
// access to the private key, which never leaves internal/identity (ADR
// 0005): signing delegates to the identity.
type hostKey struct {
	id  identity.Identity
	pub crypto.PubKey
}

var _ crypto.PrivKey = (*hostKey)(nil)

// newHostKey returns the libp2p key of id.
func newHostKey(id identity.Identity) (*hostKey, error) {
	pub, err := crypto.UnmarshalEd25519PublicKey(id.PublicKey())
	if err != nil {
		return nil, fmt.Errorf("node public key: %w", err)
	}
	return &hostKey{id: id, pub: pub}, nil
}

// Sign signs msg with the node key.
func (k *hostKey) Sign(msg []byte) ([]byte, error) { return k.id.Sign(msg), nil }

// GetPublic returns the node's public key.
func (k *hostKey) GetPublic() crypto.PubKey { return k.pub }

// Type returns Ed25519.
func (k *hostKey) Type() pb.KeyType { return pb.KeyType_Ed25519 }

// Equals reports whether o is a private key with the same public key.
func (k *hostKey) Equals(o crypto.Key) bool {
	other, ok := o.(crypto.PrivKey)
	return ok && other.Type() == k.Type() && other.GetPublic().Equals(k.pub)
}

// Raw returns secret material bound to the node key instead of the key
// itself: go-libp2p only uses it to derive QUIC's stateless-reset and token
// keys, which must be secret and stable across restarts. The node's
// Ed25519 signature over rawKeyDomain is both (Ed25519 is deterministic).
func (k *hostKey) Raw() ([]byte, error) {
	sig := k.id.Sign([]byte(rawKeyDomain))
	if len(sig) == 0 {
		return nil, errors.New("node identity returned an empty signature")
	}
	return sig, nil
}
