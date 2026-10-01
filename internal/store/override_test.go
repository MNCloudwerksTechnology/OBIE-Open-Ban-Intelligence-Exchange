package store

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

func TestOverrides(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	rec := watch(db)
	now := clk.Now()
	block := ipv4("11.0.0.1")
	allow := obieproto.Indicator{Kind: "CIDR", Value: " 11.2.3.4/24 "} // normalized on store

	if err := db.SetOverride(Override{Indicator: block, Action: ForceBlock, Note: "scanner"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetOverride(Override{Indicator: allow, Action: ForceAllow, ExpiresAt: now.Add(time.Hour + 500*time.Millisecond)}); err != nil {
		t.Fatal(err)
	}
	allowKey := "cidr:11.2.3.0/24"
	if got := rec.take(); !reflect.DeepEqual(got, []Change{{block.Key(), ReasonOverride}, {allowKey, ReasonOverride}}) {
		t.Errorf("changes = %v", got)
	}

	got, err := db.Override(allowKey, now)
	if err != nil {
		t.Fatalf("Override: %v", err)
	}
	want := Override{
		Indicator: obieproto.Indicator{Kind: obieproto.KindCIDR, Value: "11.2.3.0/24", Scope: "/24"},
		Action:    ForceAllow,
		CreatedAt: now,
		ExpiresAt: now.Add(time.Hour),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Override = %+v, want %+v", got, want)
	}
	list, err := db.Overrides(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Indicator.Key() != allowKey || list[1].Note != "scanner" {
		t.Errorf("Overrides = %+v", list)
	}

	// Replacing drops the old expiry: the override no longer expires.
	if err := db.SetOverride(Override{Indicator: allow, Action: ForceBlock}); err != nil {
		t.Fatal(err)
	}
	rec.take()
	clk.Advance(2 * time.Hour)
	if err := db.Sweep(clk.Now()); err != nil {
		t.Fatal(err)
	}
	if got := rec.take(); len(got) != 0 {
		t.Errorf("replaced override expired: %v", got)
	}
	if o, err := db.Override(allowKey, clk.Now()); err != nil || o.Action != ForceBlock {
		t.Errorf("Override = %+v, %v", o, err)
	}

	deleted, err := db.DeleteOverride(block.Key())
	if err != nil || !deleted {
		t.Fatalf("DeleteOverride = %v, %v", deleted, err)
	}
	if got := rec.take(); !reflect.DeepEqual(got, []Change{{block.Key(), ReasonOverride}}) {
		t.Errorf("changes = %v", got)
	}
	if deleted, err := db.DeleteOverride(block.Key()); err != nil || deleted {
		t.Errorf("second DeleteOverride = %v, %v", deleted, err)
	}
	if got := rec.take(); len(got) != 0 {
		t.Errorf("deleting nothing notified %v", got)
	}
	if _, err := db.Override(block.Key(), clk.Now()); !errors.Is(err, ErrNotFound) {
		t.Errorf("Override after delete = %v, want ErrNotFound", err)
	}
}

func TestOverrideExpiry(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	rec := watch(db)
	ind := ipv4("11.0.0.1")
	if err := db.SetOverride(Override{Indicator: ind, Action: ForceBlock, ExpiresAt: clk.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	rec.take()

	clk.Advance(time.Hour)
	if _, err := db.Override(ind.Key(), clk.Now()); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired Override = %v, want ErrNotFound", err)
	}
	if list, err := db.Overrides(clk.Now()); err != nil || len(list) != 0 {
		t.Errorf("Overrides = %v, %v", list, err)
	}
	if err := db.Sweep(clk.Now()); err != nil {
		t.Fatal(err)
	}
	if got := rec.take(); !reflect.DeepEqual(got, []Change{{ind.Key(), ReasonExpiry}}) {
		t.Errorf("changes = %v", got)
	}
	if deleted, err := db.DeleteOverride(ind.Key()); err != nil || deleted {
		t.Errorf("DeleteOverride after sweep = %v, %v", deleted, err)
	}
}

func TestSetOverrideRejectsInvalid(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	ind := ipv4("11.0.0.1")
	tests := map[string]Override{
		"no action":      {Indicator: ind},
		"unknown action": {Indicator: ind, Action: "maybe"},
		"bad indicator":  {Indicator: obieproto.Indicator{Kind: "ipv4", Value: "nope"}, Action: ForceBlock},
		"long note":      {Indicator: ind, Action: ForceBlock, Note: strings.Repeat("x", MaxNoteLength+1)},
		"expired":        {Indicator: ind, Action: ForceBlock, ExpiresAt: clk.Now()},
	}
	for name, o := range tests {
		t.Run(name, func(t *testing.T) {
			// CheckOverride refuses what SetOverride refuses, with the same
			// error, and stores nothing.
			checked := CheckOverride(o, clk.Now())
			err := db.SetOverride(o)
			if !errors.Is(err, ErrInvalid) || !errors.Is(checked, ErrInvalid) || !strings.Contains(err.Error(), checked.Error()) {
				t.Errorf("SetOverride = %v, CheckOverride = %v, want the same ErrInvalid", err, checked)
			}
		})
	}
	valid := Override{Indicator: ind, Action: ForceAllow, Note: "partner", ExpiresAt: clk.Now().Add(time.Hour)}
	if err := CheckOverride(valid, clk.Now()); err != nil {
		t.Errorf("CheckOverride(valid) = %v", err)
	}
	if list, err := db.Overrides(clk.Now()); err != nil || len(list) != 0 {
		t.Errorf("Overrides after the checks = %v, %v; want none", list, err)
	}
}

func TestOverridesAreSeparateFromVerdicts(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	ind := ipv4("11.0.0.1")
	if err := db.SetOverride(Override{Indicator: ind, Action: ForceAllow}); err != nil {
		t.Fatal(err)
	}
	if got := activeIDs(t, db, ind.Key(), clk.Now()); len(got) != 0 {
		t.Errorf("override shows up as verdict: %v", got)
	}
	res, err := db.ListIndicators(clk.Now(), Filter{}, Page{})
	if err != nil || len(res.Items) != 0 {
		t.Errorf("ListIndicators = %+v, %v", res, err)
	}
}

// TestExpiredOverridesAreKept: an override that expired is listed as
// expired for OverrideRetention, before and after the sweep moves it, and
// never again in effect (ADR 0024).
func TestExpiredOverridesAreKept(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	block, allow, never := ipv4("11.0.0.1"), ipv4("11.0.0.2"), ipv4("11.0.0.3")
	set := func(o Override) {
		t.Helper()
		if err := db.SetOverride(o); err != nil {
			t.Fatal(err)
		}
	}
	created := clk.Now()
	blockEnds := created.Add(time.Hour)
	set(Override{Indicator: block, Action: ForceBlock, Note: "scanner", ExpiresAt: blockEnds})
	set(Override{Indicator: allow, Action: ForceAllow, ExpiresAt: created.Add(2 * time.Hour)})
	set(Override{Indicator: never, Action: ForceAllow})
	expired := func() []Override {
		t.Helper()
		list, err := db.ExpiredOverrides(clk.Now())
		if err != nil {
			t.Fatal(err)
		}
		return list
	}
	active := func() []string {
		t.Helper()
		list, err := db.Overrides(clk.Now())
		if err != nil {
			t.Fatal(err)
		}
		var keys []string
		for _, o := range list {
			keys = append(keys, o.Indicator.Key()+" "+string(o.Action))
		}
		return keys
	}
	if got := expired(); len(got) != 0 {
		t.Errorf("before any expiry: %+v", got)
	}

	// Expired but not swept yet: listed already, no longer in effect, and
	// not deleted by DeleteOverride.
	clk.Advance(time.Hour)
	want := []Override{{Indicator: block, Action: ForceBlock, Note: "scanner", CreatedAt: created, ExpiresAt: blockEnds}}
	want[0].Indicator.Scope = "/32"
	if got := expired(); !reflect.DeepEqual(got, want) {
		t.Errorf("expired before the sweep = %+v\nwant %+v", got, want)
	}
	if deleted, err := db.DeleteOverride(block.Key()); err != nil || deleted {
		t.Errorf("DeleteOverride of an expired override = %v, %v", deleted, err)
	}
	if err := db.Sweep(clk.Now()); err != nil {
		t.Fatal(err)
	}
	if got := expired(); !reflect.DeepEqual(got, want) {
		t.Errorf("expired after the sweep = %+v\nwant %+v", got, want)
	}
	if got := active(); !reflect.DeepEqual(got, []string{allow.Key() + " force_allow", never.Key() + " force_allow"}) {
		t.Errorf("in effect = %v", got)
	}
	wantTTL(t, db, expiredOverrideKey(block.Key()), blockEnds.Add(OverrideRetention))

	// An override replacing one that expired unswept keeps it as expired.
	clk.Advance(time.Hour)
	set(Override{Indicator: allow, Action: ForceBlock, Note: "again"})
	if got := expired(); len(got) != 2 || got[1].Indicator.Key() != allow.Key() || got[1].Action != ForceAllow {
		t.Errorf("expired after a replacement = %+v", got)
	}
	if got := active(); !reflect.DeepEqual(got, []string{allow.Key() + " force_block", never.Key() + " force_allow"}) {
		t.Errorf("in effect after a replacement = %v", got)
	}

	// A later override on the same indicator that expires replaces the one
	// kept, and is listed once, before and after the sweep.
	set(Override{Indicator: block, Action: ForceAllow, Note: "second", ExpiresAt: clk.Now().Add(time.Hour)})
	wantTTL(t, db, overrideKey(block.Key()), clk.Now().Add(time.Hour+OverrideRetention))
	clk.Advance(time.Hour)
	for _, when := range []string{"before", "after"} {
		got := expired()
		if len(got) != 2 || got[0].Note != "second" || got[1].Indicator.Key() != allow.Key() {
			t.Errorf("expired %s the sweep of a second expiry = %+v", when, got)
		}
		if err := db.Sweep(clk.Now()); err != nil {
			t.Fatal(err)
		}
	}
}

// wantTTL checks that Badger forgets the entry at key at the second at.
func wantTTL(t *testing.T, db *DB, key []byte, at time.Time) {
	t.Helper()
	err := db.view(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		if got := item.ExpiresAt(); got != uint64(at.Unix()) { // #nosec G115 -- a time after 1970.
			t.Errorf("%s expires at %d, want %d (%s)", key, got, at.Unix(), at)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
