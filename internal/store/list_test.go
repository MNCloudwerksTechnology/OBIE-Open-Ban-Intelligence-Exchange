package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// ipv4Value returns a distinct public IPv4 address for i < 2^24.
func ipv4Value(i int) string {
	return fmt.Sprintf("11.%d.%d.%d", i>>16&0xff, i>>8&0xff, i&0xff)
}

// listAll pages through ListIndicators with the given limit and returns the
// keys in order.
func listAll(t *testing.T, db *DB, now time.Time, filter Filter, limit int) []string {
	t.Helper()
	var keys []string
	page := Page{Limit: limit}
	for range 10000 {
		res, err := db.ListIndicators(now, filter, page)
		if err != nil {
			t.Fatalf("ListIndicators: %v", err)
		}
		for _, it := range res.Items {
			keys = append(keys, it.Key)
		}
		if res.Next == "" {
			return keys
		}
		if len(res.Items) == 0 {
			t.Fatal("empty page with a Next cursor")
		}
		page.After = res.Next
	}
	t.Fatal("paging did not terminate")
	return nil
}

func TestListIndicators(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	now := clk.Now()

	v4a := ipv4("11.0.0.1")
	v4b := ipv4("11.0.0.2")
	v4c := ipv4("11.0.0.3")
	v6 := obieproto.Indicator{Kind: obieproto.KindIPv6, Value: "2a00::1", Scope: "/128"}
	cidr := obieproto.Indicator{Kind: obieproto.KindCIDR, Value: "11.1.0.0/24", Scope: "/24"}
	revoked := ipv4("11.0.0.4")

	mustPut(t, db, verdict(pubA, v4a, now, time.Hour), true)
	mustPut(t, db, verdict(pubB, v4a, now, time.Hour), true)
	mustPut(t, db, verdict(pubB, v4b, now, time.Hour), true)
	mustPut(t, db, verdict(pubA, v4c, now, time.Hour), true)
	mustPut(t, db, verdict(pubA, v6, now, time.Hour), true)
	mustPut(t, db, verdict(pubC, cidr, now, time.Hour), true)
	rv := verdict(pubA, revoked, now, time.Hour)
	mustPut(t, db, rv, true)
	mustPut(t, db, revoke(pubA, rv, now), true)

	all := []string{cidr.Key(), v4a.Key(), v4b.Key(), v4c.Key(), v6.Key()}
	tests := []struct {
		name   string
		filter Filter
		want   []string
	}{
		{"all", Filter{}, all},
		{"kind ipv4", Filter{Kind: obieproto.KindIPv4}, []string{v4a.Key(), v4b.Key(), v4c.Key()}},
		{"kind ipv6", Filter{Kind: obieproto.KindIPv6}, []string{v6.Key()}},
		{"publisher A", Filter{Publisher: pubA}, []string{v4a.Key(), v4c.Key(), v6.Key()}},
		{"kind and publisher", Filter{Kind: obieproto.KindIPv4, Publisher: pubB}, []string{v4a.Key(), v4b.Key()}},
		{"no match", Filter{Publisher: "unknown"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, limit := range []int{0, 1, 2, 5, 6} {
				if got := listAll(t, db, now, tt.filter, limit); !slices.Equal(got, tt.want) {
					t.Errorf("limit %d: keys = %v, want %v", limit, got, tt.want)
				}
			}
		})
	}

	t.Run("exact last page has no cursor", func(t *testing.T) {
		res, err := db.ListIndicators(now, Filter{}, Page{Limit: len(all)})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Items) != len(all) || res.Next != "" {
			t.Errorf("got %d items, next %q", len(res.Items), res.Next)
		}
	})

	t.Run("items carry the active verdicts", func(t *testing.T) {
		res, err := db.ListIndicators(now, Filter{Kind: obieproto.KindIPv4}, Page{Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		it := res.Items[0]
		if it.Indicator != v4a || len(it.Verdicts) != 2 || res.Next != v4a.Key() {
			t.Errorf("first item = %+v, next %q", it, res.Next)
		}
	})

	t.Run("expired verdicts are not listed", func(t *testing.T) {
		if got := listAll(t, db, now.Add(time.Hour), Filter{}, 0); len(got) != 0 {
			t.Errorf("keys = %v", got)
		}
	})
}

func TestListIndicatorsCapsLimit(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	for i := range MaxPageLimit + 1 {
		mustPut(t, db, verdict(pubA, ipv4(ipv4Value(i)), clk.Now(), time.Hour), true)
	}
	res, err := db.ListIndicators(clk.Now(), Filter{}, Page{Limit: MaxPageLimit * 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != MaxPageLimit || res.Next == "" {
		t.Errorf("got %d items, next %q", len(res.Items), res.Next)
	}
	res, err = db.ListIndicators(clk.Now(), Filter{}, Page{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != DefaultPageLimit {
		t.Errorf("default page has %d items", len(res.Items))
	}
}

// publisherKeys pages through PublisherVerdicts with the given limit and
// returns the indicator keys in order, checking every verdict's publisher.
func publisherKeys(t *testing.T, db *DB, publisher string, now time.Time, limit int) []string {
	t.Helper()
	var keys []string
	page := Page{Limit: limit}
	for range 10000 {
		res, err := db.PublisherVerdicts(publisher, now, page)
		if err != nil {
			t.Fatalf("PublisherVerdicts: %v", err)
		}
		for _, v := range res.Verdicts {
			if v.Publisher.PeerID != publisher {
				t.Errorf("verdict %s of %s listed for %s", v.ID, v.Publisher.PeerID, publisher)
			}
			keys = append(keys, v.Key())
		}
		if res.Next == "" {
			return keys
		}
		if len(res.Verdicts) == 0 {
			t.Fatal("empty page with a Next cursor")
		}
		page.After = res.Next
	}
	t.Fatal("paging did not terminate")
	return nil
}

// TestPublisherVerdicts: one publisher's active verdicts, by indicator,
// page by page; revoked and expired ones are left out.
func TestPublisherVerdicts(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	now := clk.Now()
	v4a, v4b, v4c := ipv4("11.0.0.1"), ipv4("11.0.0.2"), ipv4("11.0.0.3")
	v6 := obieproto.Indicator{Kind: obieproto.KindIPv6, Value: "2a00::1", Scope: "/128"}
	cidr := obieproto.Indicator{Kind: obieproto.KindCIDR, Value: "11.1.0.0/24", Scope: "/24"}
	revoked, short := ipv4("11.0.0.4"), ipv4("11.0.0.5")
	// A publisher whose ID ends with pubB's must not match pubB.
	longer := "x" + pubB

	mustPut(t, db, verdict(pubA, v4a, now, time.Hour), true)
	mustPut(t, db, verdict(pubB, v4a, now, time.Hour), true)
	mustPut(t, db, verdict(pubB, v4b, now, time.Hour), true)
	mustPut(t, db, verdict(longer, v4c, now, time.Hour), true)
	mustPut(t, db, verdict(pubA, v4c, now, time.Hour), true)
	mustPut(t, db, verdict(pubA, v6, now, time.Hour), true)
	mustPut(t, db, verdict(pubA, cidr, now, time.Hour), true)
	mustPut(t, db, verdict(pubA, short, now, time.Minute), true)
	rv := verdict(pubA, revoked, now, time.Hour)
	mustPut(t, db, rv, true)
	mustPut(t, db, revoke(pubA, rv, now), true)

	later := now.Add(time.Minute) // the short verdict has expired
	for publisher, want := range map[string][]string{
		pubA:      {cidr.Key(), v4a.Key(), v4c.Key(), v6.Key()},
		pubB:      {v4a.Key(), v4b.Key()},
		longer:    {v4c.Key()},
		"unknown": nil,
	} {
		for _, limit := range []int{0, 1, 2, 3, 4, 5} {
			if got := publisherKeys(t, db, publisher, later, limit); !slices.Equal(got, want) {
				t.Errorf("%s, limit %d: keys = %v, want %v", publisher, limit, got, want)
			}
		}
	}
	if got := publisherKeys(t, db, pubA, now, 0); !slices.Contains(got, short.Key()) {
		t.Errorf("before it expired: keys = %v, want %s among them", got, short.Key())
	}

	res, err := db.PublisherVerdicts(pubA, later, Page{Limit: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Verdicts) != 4 || res.Next != "" {
		t.Errorf("exact last page: %d verdicts, next %q", len(res.Verdicts), res.Next)
	}

	if err := db.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.PublisherVerdicts(pubA, later, Page{}); !errors.Is(err, ErrClosed) {
		t.Errorf("PublisherVerdicts on a closed store = %v, want ErrClosed", err)
	}
}

func TestPublisherVerdictsCapsLimit(t *testing.T) {
	clk := newClock()
	db := newMemDB(t, clk)
	for i := range MaxPageLimit + 1 {
		mustPut(t, db, verdict(pubA, ipv4(ipv4Value(i)), clk.Now(), time.Hour), true)
	}
	res, err := db.PublisherVerdicts(pubA, clk.Now(), Page{Limit: MaxPageLimit * 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Verdicts) != MaxPageLimit || res.Next == "" {
		t.Errorf("got %d verdicts, next %q", len(res.Verdicts), res.Next)
	}
	if res, err = db.PublisherVerdicts(pubA, clk.Now(), Page{}); err != nil || len(res.Verdicts) != DefaultPageLimit {
		t.Errorf("default page has %d verdicts, %v", len(res.Verdicts), err)
	}
}
