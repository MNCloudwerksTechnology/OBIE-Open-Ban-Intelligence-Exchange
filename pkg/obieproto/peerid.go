package obieproto

import (
	"bytes"
	"crypto/ed25519"
	"fmt"

	"github.com/mr-tron/base58"
)

// ed25519PeerIDPrefix starts the binary form of every libp2p peer ID of an
// Ed25519 key: an identity multihash (code 0x00, length 36) of the protobuf
// PublicKey message {Type: Ed25519 (1), Data: <32-byte key>}. libp2p inlines
// keys this short instead of hashing them, which is why a verifier can
// recover the public key from the peer ID alone.
var ed25519PeerIDPrefix = []byte{
	0x00, 0x24, // multihash: identity, 36 bytes
	0x08, 0x01, // protobuf field 1 (Type): Ed25519
	0x12, 0x20, // protobuf field 2 (Data): 32 bytes
}

// PeerIDFromPublicKey returns the libp2p peer ID of an Ed25519 public key in
// its base58btc text form ("12D3KooW..."), the form publisher.peer_id uses.
func PeerIDFromPublicKey(pub ed25519.PublicKey) (string, error) {
	if len(pub) != ed25519.PublicKeySize {
		return "", fmt.Errorf("obieproto: Ed25519 public key has %d bytes, want %d", len(pub), ed25519.PublicKeySize)
	}
	return base58.Encode(append(bytes.Clone(ed25519PeerIDPrefix), pub...)), nil
}

// PublicKeyFromPeerID extracts the Ed25519 public key embedded in a libp2p
// peer ID. Peer IDs of other key types hash the key rather than embedding it
// and are rejected, as is any non-canonical encoding.
func PublicKeyFromPeerID(peerID string) (ed25519.PublicKey, error) {
	raw, err := base58.Decode(peerID)
	if err != nil || len(raw) != len(ed25519PeerIDPrefix)+ed25519.PublicKeySize ||
		!bytes.HasPrefix(raw, ed25519PeerIDPrefix) || base58.Encode(raw) != peerID {
		return nil, fmt.Errorf("obieproto: %q is not the peer ID of an Ed25519 key", peerID)
	}
	return ed25519.PublicKey(raw[len(ed25519PeerIDPrefix):]), nil
}
