package enforce

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/netip"
	"testing"
	"time"
)

func entry(prefix string, expires time.Time) Entry {
	return Entry{Prefix: netip.MustParsePrefix(prefix), Expires: expires}
}

// TestDryRun: the dry-run backend keeps entries in memory, expires them
// like the kernel and logs every add and remove as JSON.
func TestDryRun(t *testing.T) {
	ctx := context.Background()
	var logs bytes.Buffer
	d := NewDryRun(slog.New(slog.NewJSONHandler(&logs, nil)))
	now := t0
	d.now = func() time.Time { return now }

	if err := d.Setup(ctx); err != nil {
		t.Fatal(err)
	}
	add := []Entry{entry("2001:db8::1/128", t0.Add(time.Hour)), entry("198.51.100.0/24", t0.Add(time.Minute)),
		entry("192.0.2.1/32", t0.Add(time.Hour))}
	if err := d.Apply(ctx, add, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := d.List(ctx)
	if len(got) != 3 || got[0].Prefix.String() != "192.0.2.1/32" || got[1].Prefix.String() != "198.51.100.0/24" ||
		got[2].Prefix.String() != "2001:db8::1/128" {
		t.Errorf("List = %+v", got)
	}

	// Replacing an entry: remove first, then add.
	if err := d.Apply(ctx, []Entry{entry("192.0.2.1/32", t0.Add(2*time.Hour))}, []Entry{entry("192.0.2.1/32", t0.Add(time.Hour)),
		entry("2001:db8::1/128", t0.Add(time.Hour))}); err != nil {
		t.Fatal(err)
	}
	now = t0.Add(time.Minute) // 198.51.100.0/24 expires
	got, _ = d.List(ctx)
	if len(got) != 1 || got[0] != entry("192.0.2.1/32", t0.Add(2*time.Hour)) {
		t.Errorf("List = %+v", got)
	}

	if err := d.Teardown(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ = d.List(ctx); len(got) != 0 {
		t.Errorf("List after Teardown = %+v", got)
	}

	var adds, removes int
	sc := bufio.NewScanner(&logs)
	for sc.Scan() {
		var line map[string]any
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			t.Fatalf("log line %q is not JSON: %v", sc.Text(), err)
		}
		switch line["msg"] {
		case "dryrun: add":
			adds++
			if line["prefix"] == "192.0.2.1/32" && line["timeout"] != "1h0m0s" && line["timeout"] != "2h0m0s" {
				t.Errorf("add line = %v", line)
			}
		case "dryrun: remove":
			removes++
		}
	}
	if adds != 4 || removes != 2 {
		t.Errorf("logged %d adds and %d removes, want 4 and 2:\n%s", adds, removes, logs.String())
	}
}
