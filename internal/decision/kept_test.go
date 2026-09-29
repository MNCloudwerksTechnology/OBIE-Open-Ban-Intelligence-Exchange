package decision

import (
	"fmt"
	"net/netip"
	"testing"
)

func TestKeptSet(t *testing.T) {
	var s keptSet
	if _, ok := s.get("ipv4:203.0.113.7"); ok || s.len() != 0 {
		t.Fatal("zero keptSet is not empty")
	}
	a, b, c := indicator("203.0.113.7"), indicator("198.51.100.0/24"), indicator("2001:db8::1")
	s.put(a.Key(), Decision{Indicator: a, State: StateBlock}, nil)
	s.put(b.Key(), Decision{Indicator: b, State: StateNone}, nil)
	s.put(c.Key(), Decision{Indicator: c, State: StateAllowed}, nil)
	s.put(b.Key(), Decision{Indicator: b, State: StateBlock}, []heldVerdict{{counts: true}})
	if k, ok := s.get(b.Key()); !ok || k.d.State != StateBlock || len(k.held) != 1 ||
		k.prefix != netip.MustParsePrefix("198.51.100.0/24") || s.len() != 3 {
		t.Fatalf("replaced = %+v, %d kept", k, s.len())
	}
	s.remove(a.Key())
	s.remove("ipv4:192.0.2.1") // not kept
	if _, ok := s.get(a.Key()); ok || s.len() != 2 {
		t.Fatalf("after remove: %d kept", s.len())
	}
	for _, key := range []string{b.Key(), c.Key()} {
		if k, ok := s.get(key); !ok || k.key != key {
			t.Errorf("get(%s) = %+v, %v after moving the last into the gap", key, k, ok)
		}
	}
}

// TestKeptSetShrinks: once most decisions are gone, the set gives the
// memory back and still finds the rest.
func TestKeptSetShrinks(t *testing.T) {
	var s keptSet
	for i := range 4096 {
		ind := indicator(fmt.Sprintf("10.0.%d.%d", i>>8, i&0xff))
		s.put(ind.Key(), Decision{Indicator: ind}, nil)
	}
	grown := cap(s.items)
	for i := range 4000 {
		s.remove(indicator(fmt.Sprintf("10.0.%d.%d", i>>8, i&0xff)).Key())
	}
	if cap(s.items) >= grown/4 || s.len() != 96 {
		t.Errorf("cap %d of %d kept, grown to %d", cap(s.items), s.len(), grown)
	}
	for i := 4000; i < 4096; i++ {
		key := indicator(fmt.Sprintf("10.0.%d.%d", i>>8, i&0xff)).Key()
		if k, ok := s.get(key); !ok || k.key != key {
			t.Fatalf("get(%s) after shrinking = %v", key, ok)
		}
	}
}
