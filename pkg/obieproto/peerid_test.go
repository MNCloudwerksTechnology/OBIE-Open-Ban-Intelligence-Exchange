package obieproto

import (
	"crypto/ed25519"
	"encoding/hex"
	"testing"
)

// Keys from RFC 8032 section 7.1 (tests 1 and 2). The peer IDs were computed
// with go-libp2p's peer.IDFromPublicKey.
const (
	testSeedA   = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"
	testPubA    = "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a"
	testPeerIDA = "12D3KooWQK1wnefoLrcVHbbnf5tLzbopUd3K3bFAoJpA7YJgL5pV"
	testSeedB   = "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb"
	testPubB    = "3d4017c3e843895a92b70aa74d1b7ebc9c982ccf2ec4968cc0cd55f12af4660c"
	testPeerIDB = "12D3KooWDwTirQce1RRKnasT5fPVFgzXCy6SiRgSwrwPGLC7zE91"
)

func mustHex(t testing.TB, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func testKey(t testing.TB, seedHex string) ed25519.PrivateKey {
	t.Helper()
	return ed25519.NewKeyFromSeed(mustHex(t, seedHex))
}

func TestPeerIDRoundTrip(t *testing.T) {
	tests := []struct{ pub, peerID string }{
		{testPubA, testPeerIDA},
		{testPubB, testPeerIDB},
	}
	for _, tt := range tests {
		t.Run(tt.peerID, func(t *testing.T) {
			got, err := PeerIDFromPublicKey(mustHex(t, tt.pub))
			if err != nil || got != tt.peerID {
				t.Fatalf("PeerIDFromPublicKey() = %q, %v; want %q", got, err, tt.peerID)
			}
			pub, err := PublicKeyFromPeerID(tt.peerID)
			if err != nil || hex.EncodeToString(pub) != tt.pub {
				t.Fatalf("PublicKeyFromPeerID() = %x, %v; want %s", pub, err, tt.pub)
			}
		})
	}
}

func TestPeerIDFromPublicKeyRejectsWrongSize(t *testing.T) {
	if _, err := PeerIDFromPublicKey(make([]byte, 31)); err == nil {
		t.Error("PeerIDFromPublicKey(31 bytes) error = nil, want error")
	}
}

func TestPublicKeyFromPeerIDRejects(t *testing.T) {
	tests := []struct{ name, peerID string }{
		{"empty", ""},
		{"not base58", "12D3KooW0OIl"},
		{"secp256k1 key", "16Uiu2HAm8RymmxmEHvqcSJhoQMKpJSZmo22eiGyz2wJxScjQHZxL"},
		{"sha256 multihash (RSA style)", "QmYyQSo1c1Ym7orWxLYvCrM2EmxFTANf8wXmmE7DWjhx5N"},
		{"truncated", testPeerIDA[:len(testPeerIDA)-1]},
		{"extra character", testPeerIDA + "1"},
		{"leading zero byte", "1" + testPeerIDA},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if pub, err := PublicKeyFromPeerID(tt.peerID); err == nil {
				t.Errorf("PublicKeyFromPeerID(%q) = %x, want error", tt.peerID, pub)
			}
		})
	}
}
