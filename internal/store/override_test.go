package store

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

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
			if err := db.SetOverride(o); !errors.Is(err, ErrInvalid) {
				t.Errorf("SetOverride = %v, want ErrInvalid", err)
			}
		})
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
