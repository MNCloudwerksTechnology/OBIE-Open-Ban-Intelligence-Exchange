package obieproto

// Supersedes reports whether verdict a replaces verdict b under the obie/0.1
// rule "the latest verdict per publisher and indicator wins": both are
// verdicts by the same publisher about the same indicator, and a was issued
// later than b, or in the same second with a greater ID. A receiver keeps
// only the latest verdict of each publisher on each indicator and ignores a
// verdict that does not supersede the one it holds.
func Supersedes(a, b *Event) bool {
	if a == nil || b == nil || a.Type != TypeVerdict || b.Type != TypeVerdict ||
		a.Publisher.PeerID != b.Publisher.PeerID || a.Key() != b.Key() {
		return false
	}
	if !a.IssuedAt.Equal(b.IssuedAt.Time) {
		return a.IssuedAt.After(b.IssuedAt.Time)
	}
	return a.ID > b.ID
}

// Withdraws reports whether the revocation r withdraws the verdict v: r
// names v's ID in revokes, and has v's publisher and indicator. A revocation
// by another publisher, or naming another indicator, has no effect.
func (r *Event) Withdraws(v *Event) bool {
	return r != nil && v != nil && r.Type == TypeRevoke && v.Type == TypeVerdict &&
		r.Revokes == v.ID && r.Publisher.PeerID == v.Publisher.PeerID && r.Key() == v.Key()
}
