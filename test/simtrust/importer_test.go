package simtrust

import (
	"bytes"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testKey = []byte("0123456789abcdef-shared-by-contributors")

func testPseudonymizer(t *testing.T) Pseudonymizer {
	t.Helper()
	pz, err := NewPseudonymizer(testKey)
	if err != nil {
		t.Fatal(err)
	}
	return pz
}

func TestPseudonymsPreservePrefixes(t *testing.T) {
	pz := testPseudonymizer(t)
	other, err := NewPseudonymizer([]byte("another key of sixteen bytes or more"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		r    string
		in   []string
		out  string
		room netip.Prefix
	}{
		{"198.51.100.0/24", []string{"198.51.100.1", "198.51.100.254"}, "198.51.101.1", pseudoV4},
		{"104.16.0.0/13", []string{"104.16.0.1", "104.23.255.9"}, "104.24.0.1", pseudoV4},
		{"2a00:1450:4001::/48", []string{"2a00:1450:4001::1", "2a00:1450:4001:82a::200e"}, "2a00:1450:4002::1", pseudoV6},
	} {
		r := netip.MustParsePrefix(tt.r)
		pr := pz.Prefix(r)
		if !tt.room.Contains(pr.Addr()) || pr.Bits() != tt.room.Bits()+r.Bits() {
			t.Errorf("pseudonym of %s is %s, want a /%d in %s", r, pr, tt.room.Bits()+r.Bits(), tt.room)
		}
		for _, s := range tt.in {
			a := netip.MustParseAddr(s)
			p := pz.Addr(a)
			if !pr.Contains(p) {
				t.Errorf("pseudonym %s of %s is outside %s, the pseudonym of its range %s", p, a, pr, r)
			}
			if p == a || p != pz.Addr(a) || p == other.Addr(a) {
				t.Errorf("pseudonym of %s: %s under the key, %s under another; want a stable one that depends on the key", a, p, other.Addr(a))
			}
		}
		if outside := pz.Addr(netip.MustParseAddr(tt.out)); pr.Contains(outside) {
			t.Errorf("pseudonym %s of %s, outside %s, is inside %s", outside, tt.out, r, pr)
		}
	}
}

func TestPseudonymsAreDistinct(t *testing.T) {
	pz := testPseudonymizer(t)
	seen := map[netip.Addr]bool{}
	for i := range 4096 {
		p := pz.Addr(netip.AddrFrom4([4]byte{203, 0, byte(i >> 8), byte(i)}))
		if seen[p] {
			t.Fatalf("two addresses share the pseudonym %s", p)
		}
		seen[p] = true
	}
	if _, err := NewPseudonymizer([]byte("short")); err == nil {
		t.Error("a key of 5 bytes was accepted")
	}
}

func TestReadBenignRanges(t *testing.T) {
	got, err := ReadBenignRanges(strings.NewReader("# CDN edges\n104.16.0.0/13 cdn\n\n66.249.64.0/19 crawler # Googlebot\n2001:db8:9::/48 customer\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[1].Class != ClassCrawler || got[1].Prefix != netip.MustParsePrefix("66.249.64.0/19") {
		t.Errorf("ranges = %+v", got)
	}
	for _, bad := range []string{"104.16.0.0/13\n", "104.16.0.0/13 attacker\n", "104.16.0.0 cdn\n", "a b c\n"} {
		if _, err := ReadBenignRanges(strings.NewReader(bad)); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

const (
	observerLog = `2026-09-01 10:15:02,123 fail2ban.actions        [812]: NOTICE  [sshd] Ban 198.51.100.7
2026-09-01 10:15:40,001 fail2ban.filter         [812]: INFO    [sshd] Found 198.51.100.7 - 2026-09-01 10:15:40
2026-09-01 11:00:00,000 fail2ban.actions        [812]: NOTICE  [sshd] Unban 198.51.100.7
2026-09-01 12:00:00,000 fail2ban.actions        [812]: NOTICE  [sshd] Restore Ban 192.0.2.99
2026-09-01 12:30:00,500 fail2ban.actions        [812]: NOTICE  [nginx-http-auth] Ban 104.16.3.4
`
	publisherLog = `2026-09-01 10:20:00,000 fail2ban.actions        [99]: NOTICE  [sshd] Ban 198.51.100.7
2026-09-01 13:05:00,000 fail2ban.actions        [99]: NOTICE  [sshd] Ban 2001:db8:77::5
`
	benignRanges = "104.16.0.0/13 cdn\n66.249.64.0/19 crawler\n"
)

func TestImportFail2Ban(t *testing.T) {
	pz := testPseudonymizer(t)
	benign, err := ReadBenignRanges(strings.NewReader(benignRanges))
	if err != nil {
		t.Fatal(err)
	}
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	logs := []OperatorLog{
		{Operator{Name: "observer", Bantime: time.Hour}, strings.NewReader(observerLog)},
		{Operator{Name: "publisher", Bantime: 10 * time.Minute}, strings.NewReader(publisherLog)},
	}
	tr, err := ImportFail2Ban(logs, benign, pz, berlin, DefaultWorld(), 1)
	if err != nil {
		t.Fatal(err)
	}
	// 10:15:02 CEST is 08:15:02 UTC; the trace starts on the hour and lasts
	// until the last ban, at 11:05 UTC.
	if want := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC); !tr.Start.Equal(want) || tr.Hours != 4 {
		t.Errorf("trace starts at %s for %d hours, want %s for 4", tr.Start, tr.Hours, want)
	}
	attacker, cdn := pz.Addr(netip.MustParseAddr("198.51.100.7")), pz.Addr(netip.MustParseAddr("104.16.3.4"))
	want := []Observation{
		{time.Date(2026, 9, 1, 8, 15, 2, 0, time.UTC), 0, attacker},
		{time.Date(2026, 9, 1, 8, 20, 0, 0, time.UTC), 1, attacker},
		{time.Date(2026, 9, 1, 10, 30, 0, 0, time.UTC), 0, cdn},
		{time.Date(2026, 9, 1, 11, 5, 0, 0, time.UTC), 1, pz.Addr(netip.MustParseAddr("2001:db8:77::5"))},
	}
	if len(tr.Observations) != len(want) {
		t.Fatalf("observations = %+v, want %+v", tr.Observations, want)
	}
	for i := range want {
		if o := tr.Observations[i]; !o.At.Equal(want[i].At) || o.Operator != want[i].Operator || o.Addr != want[i].Addr {
			t.Errorf("observation %d = %+v, want %+v", i, o, want[i])
		}
	}
	if classOf(tr, attacker) != ClassAttacker || classOf(tr, cdn) != ClassCDN {
		t.Errorf("classes: %s %s, %s %s; want attacker and cdn", attacker, classOf(tr, attacker), cdn, classOf(tr, cdn))
	}
	if len(tr.Published) != 2 || !tr.Published[0].Prefix.Contains(cdn) {
		t.Errorf("published = %+v, want the pseudonyms of the CDN and crawler ranges", tr.Published)
	}
	counts := map[Class]int{}
	for _, a := range tr.Addresses {
		counts[a.Class]++
	}
	// The logs hold one CDN address; the synthetic population fills the
	// classes they lack, crawlers inside the published crawler range.
	p := DefaultWorld()
	if counts[ClassCDN] != 1 || counts[ClassCrawler] != p.Crawlers*p.RangesPerCrawler*p.CrawlersPerRange ||
		counts[ClassCustomer] != p.Customers || counts[ClassNAT] != p.NATs {
		t.Errorf("address classes = %v", counts)
	}
	for _, a := range tr.Addresses {
		if a.Class == ClassCrawler && !tr.Published[1].Prefix.Contains(a.Addr) {
			t.Errorf("crawler %s outside the published crawler range %s", a.Addr, tr.Published[1].Prefix)
		}
	}
	for _, original := range []string{"198.51.100.7", "104.16.3.4", "2001:db8:77::5"} {
		if classOf(tr, netip.MustParseAddr(original)) != "" {
			t.Errorf("the trace holds the original address %s", original)
		}
	}
}

func TestTraceRoundTrip(t *testing.T) {
	p := DefaultWorld()
	p.Hours = 6
	tr := GenerateWorld(p, 3, testStart)
	var buf bytes.Buffer
	if err := WriteTrace(&buf, tr); err != nil {
		t.Fatal(err)
	}
	got, err := ReadTrace(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	var again bytes.Buffer
	if err := WriteTrace(&again, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), again.Bytes()) {
		t.Error("a trace read back differs from the trace written")
	}
	if got.Source != tr.Source || len(got.Observations) != len(tr.Observations) || got.Operators[3] != tr.Operators[3] {
		t.Errorf("read %q with %d observations, want %q with %d", got.Source, len(got.Observations), tr.Source, len(tr.Observations))
	}
}

func TestReadTraceRejects(t *testing.T) {
	header := `{"kind":"trace","format":"obie-trust-trace/1","start":"2100-01-01T00:00:00Z","hours":1}` + "\n"
	ops := `{"kind":"operator","name":"a","bantime_seconds":600}` + "\n" + `{"kind":"operator","name":"b","bantime_seconds":600}` + "\n"
	for name, text := range map[string]string{
		"no header":          ops,
		"other format":       strings.Replace(header, "/1", "/2", 1) + ops,
		"unknown kind":       header + ops + `{"kind":"peer"}` + "\n",
		"unknown field":      header + ops + `{"kind":"operator","name":"c","bantime_seconds":600,"size":3}` + "\n",
		"undeclared address": header + ops + `{"kind":"ban","at":"2100-01-01T00:10:00Z","operator":1,"addr":"2001:db8::1"}` + "\n",
		"unknown operator":   header + ops + `{"kind":"address","addr":"2001:db8::1","class":"attacker"}` + "\n" + `{"kind":"ban","at":"2100-01-01T00:10:00Z","operator":2,"addr":"2001:db8::1"}` + "\n",
		"after the end":      header + ops + `{"kind":"address","addr":"2001:db8::1","class":"attacker"}` + "\n" + `{"kind":"ban","at":"2100-01-01T01:00:00Z","operator":1,"addr":"2001:db8::1"}` + "\n",
		"one operator":       header + `{"kind":"operator","name":"a","bantime_seconds":600}` + "\n",
		"unknown class":      header + ops + `{"kind":"address","addr":"2001:db8::1","class":"friend"}` + "\n",
		"malicious range":    header + ops + `{"kind":"range","range":"2001:db8::/64","class":"attacker"}` + "\n",
	} {
		if _, err := ReadTrace(strings.NewReader(text)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestRunImport(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	key, benign := write("key", string(testKey)), write("benign", benignRanges)
	a, b := write("a.log", observerLog), write("b.log", publisherLog)
	out := filepath.Join(dir, "trace.jsonl")
	if err := RunImport([]string{"-key", key, "-benign", benign, "-o", out, "op00:1h:" + a, "op01:10m:" + b}, io.Discard); err != nil {
		t.Fatal(err)
	}
	tr, err := readFileWith(out, ReadTrace)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Operators) != 2 || tr.Operators[1].Bantime != 10*time.Minute || len(tr.Observations) != 4 {
		t.Errorf("imported %d operators (%+v) and %d bans, want 2 and 4", len(tr.Operators), tr.Operators, len(tr.Observations))
	}
	for _, args := range [][]string{
		{"-key", key, "-benign", benign, "-o", out, "op00:1h:" + a},
		{"-key", key, "-benign", benign, "-o", out, "op00:1h:" + a, "op01:" + b},
		{"-key", write("short-key", "short"), "-benign", benign, "-o", out, "op00:1h:" + a, "op01:10m:" + b},
		{"-key", key, "-benign", a, "-o", out, "op00:1h:" + a, "op01:10m:" + b},
	} {
		if err := RunImport(args, io.Discard); err == nil {
			t.Errorf("RunImport(%q) succeeded", args)
		}
	}
}
