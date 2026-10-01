package sovereignty

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// FuzzParseEntry checks that parsing an allow-list entry never panics, and
// that every accepted entry is a valid prefix without host bits that
// parses back to itself.
func FuzzParseEntry(f *testing.F) {
	for _, s := range []string{
		"192.0.2.1", "192.0.2.0/24", "2001:db8::/32", "2001:db8::1", "::ffff:192.0.2.1", "fe80::1%eth0",
		"192.0.2.1/24", "0.0.0.0/0", "::/0", " 10.0.0.0/8 ", "1.2.3", "300.1.1.1", "2001:db8::/129", "",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		p, err := ParseEntry(s)
		if err != nil {
			return
		}
		if !p.IsValid() || p != p.Masked() {
			t.Fatalf("ParseEntry(%q) = %v, not a valid prefix without host bits", s, p)
		}
		if p.Addr().Is4In6() || p.Addr().Zone() != "" {
			t.Fatalf("ParseEntry(%q) = %v keeps an IPv4-mapped address or a zone", s, p)
		}
		again, err := ParseEntry(p.String())
		if err != nil || again != p {
			t.Fatalf("ParseEntry(%q) = %v, but its text parses to %v, %v", s, p, again, err)
		}
	})
}

// FuzzParseFile checks that parsing an allow-list file never panics, that
// every entry it accepts is a valid prefix without host bits, labeled with
// the file and the line it came from, and that it counts what it rejects.
func FuzzParseFile(f *testing.F) {
	f.Add([]byte("# management\n192.0.2.0/24\n\n2001:db8::1 # admin host\n"))
	f.Add([]byte("192.0.2.1\r\n198.51.100.0/24\r\n"))
	f.Add([]byte("192.0.2.1/24\n"))
	f.Add([]byte(strings.Repeat("a", maxFileLine+1)))
	f.Add([]byte("\x00\xff#\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		entries, check := parseFile(bytes.NewReader(data), "allow.txt")
		if check.Err != nil {
			if !strings.HasPrefix(check.Err.Error(), "allow-list file allow.txt") {
				t.Fatalf("error %q does not name the file", check.Err)
			}
			return
		}
		lines := strings.Split(string(data), "\n")
		if check.Entries != len(entries) || len(check.Rejected) > MaxRejected || check.RejectedLines < len(check.Rejected) ||
			len(entries)+check.RejectedLines > len(lines) {
			t.Fatalf("%d entries, check %+v for %d lines", len(entries), check, len(lines))
		}
		for _, r := range check.Rejected {
			if r.Line < 1 || r.Line > len(lines) || r.Err == nil || r.Text == "" {
				t.Fatalf("rejected line %+v does not name a line of the file", r)
			}
		}
		for _, e := range entries {
			if !e.Prefix.IsValid() || e.Prefix != e.Prefix.Masked() || e.Source != SourceFile {
				t.Fatalf("entry %+v is not a valid file entry", e)
			}
			var n int
			if _, err := fmt.Sscanf(e.Label, "allow.txt:%d", &n); err != nil || n < 1 || n > len(lines) {
				t.Fatalf("entry label %q does not name a line of the file", e.Label)
			}
		}
	})
}
