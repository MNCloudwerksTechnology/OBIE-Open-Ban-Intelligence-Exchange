package simtrust

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"net/netip"
)

// minKeyBytes is the shortest pseudonymization key accepted.
const minKeyBytes = 16

// Where pseudonyms live: IPv4 addresses in 3fff::/96, IPv6 addresses in
// 2001:db8::/32, both documentation ranges that no node blocks for real.
var (
	pseudoV4 = netip.MustParsePrefix("3fff::/96")
	pseudoV6 = netip.MustParsePrefix("2001:db8::/32")
)

// Pseudonymizer replaces addresses and ranges by pseudonyms under a key,
// preserving prefixes (Xu et al. 2002, with HMAC-SHA-256): two addresses
// that share their first k bits share the first k bits of their
// pseudonyms, and a range maps to the range of its addresses' pseudonyms.
// An IPv4 address maps into 3fff::/96. Of an IPv6 address the first 64
// bits map into 2001:db8::/32, and 32 bits of a keyed hash of the whole
// address follow them (ADR 0034).
type Pseudonymizer struct {
	key []byte
}

// NewPseudonymizer returns the pseudonymizer with key, which every
// contributor of one trace must share.
func NewPseudonymizer(key []byte) (Pseudonymizer, error) {
	if len(key) < minKeyBytes {
		return Pseudonymizer{}, errors.New("the pseudonymization key must have at least 16 bytes")
	}
	return Pseudonymizer{key: append([]byte(nil), key...)}, nil
}

// Addr returns the pseudonym of a.
func (p Pseudonymizer) Addr(a netip.Addr) netip.Addr {
	a = a.Unmap().WithZone("")
	if a.Is4() {
		v4 := a.As4()
		return p.embed(pseudoV4, p.permute(v4[:], 32, 4))
	}
	b := a.As16()
	out := p.embed(pseudoV6, p.permute(b[:8], 64, 6)).As16()
	mac := hmac.New(sha256.New, p.key)
	mac.Write([]byte("host"))
	mac.Write(b[:])
	copy(out[12:], mac.Sum(nil)[:4])
	return netip.AddrFrom16(out)
}

// Prefix returns the pseudonym of the range r; an IPv6 range longer than
// /64 widens to its /64.
func (p Pseudonymizer) Prefix(r netip.Prefix) netip.Prefix {
	r = r.Masked()
	if r.Addr().Is4() {
		v4 := r.Addr().As4()
		return netip.PrefixFrom(p.embed(pseudoV4, p.permute(v4[:], r.Bits(), 4)), pseudoV4.Bits()+r.Bits())
	}
	b := r.Addr().As16()
	bits := min(r.Bits(), 64)
	return netip.PrefixFrom(p.embed(pseudoV6, p.permute(b[:8], bits, 6)), pseudoV6.Bits()+bits)
}

// permute returns the first n bits of b permuted prefix-preservingly,
// the bits after them zero: bit i is flipped by a keyed function of the
// i bits before it. domain keeps IPv4 and IPv6 apart.
func (p Pseudonymizer) permute(b []byte, n int, domain byte) []byte {
	out := make([]byte, len(b))
	prefix := make([]byte, len(b))
	mac := hmac.New(sha256.New, p.key)
	for i := range n {
		mac.Reset()
		mac.Write([]byte{domain, byte(i)}) // #nosec G115 -- i < 128.
		mac.Write(prefix[:(i+7)/8])
		flip := mac.Sum(nil)[0] & 1
		bit := b[i/8] >> (7 - i%8) & 1
		out[i/8] |= (bit ^ flip) << (7 - i%8)
		prefix[i/8] |= bit << (7 - i%8)
	}
	return out
}

// embed places bits after the prefix inside.
func (p Pseudonymizer) embed(inside netip.Prefix, bits []byte) netip.Addr {
	out := inside.Addr().As16()
	start := inside.Bits() / 8
	copy(out[start:], bits)
	return netip.AddrFrom16(out)
}
