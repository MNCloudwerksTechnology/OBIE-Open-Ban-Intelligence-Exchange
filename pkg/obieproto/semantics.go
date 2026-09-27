package obieproto

// Supersedes reports whether verdict e replaces verdict old under the
// obie/0.1 rule "the latest verdict per publisher and indicator wins": both
// are verdicts by the same publisher about the same indicator, and e was
// issued later than old, or in the same second with a greater ID. A receiver
// keeps only the latest verdict of each publisher on each indicator and
// ignores a verdict that does not supersede the one it holds.
func (e *Event) Supersedes(old *Event) bool {
	if e == nil || old == nil || e.Type != TypeVerdict || old.Type != TypeVerdict ||
		e.Publisher.PeerID != old.Publisher.PeerID || e.Key() != old.Key() {
		return false
	}
	if !e.IssuedAt.Equal(old.IssuedAt.Time) {
		return e.IssuedAt.After(old.IssuedAt.Time)
	}
	return e.ID > old.ID
}

// Withdraws reports whether the revocation e withdraws the verdict v: e
// names v's ID in revokes, and has v's publisher and indicator. A revocation
// by another publisher, or naming another indicator, has no effect.
func (e *Event) Withdraws(v *Event) bool {
	return e != nil && v != nil && e.Type == TypeRevoke && v.Type == TypeVerdict &&
		e.Revokes == v.ID && e.Publisher.PeerID == v.Publisher.PeerID && e.Key() == v.Key()
}
