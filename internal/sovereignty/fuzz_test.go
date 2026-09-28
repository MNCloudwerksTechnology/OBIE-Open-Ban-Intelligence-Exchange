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

// FuzzParseFile checks that parsing an allow-list file never panics, and
// that every entry it accepts is a valid prefix without host bits, labeled
// with the file and the line it came from.
func FuzzParseFile(f *testing.F) {
	f.Add([]byte("# management\n192.0.2.0/24\n\n2001:db8::1 # admin host\n"))
	f.Add([]byte("192.0.2.1\r\n198.51.100.0/24\r\n"))
	f.Add([]byte("192.0.2.1/24\n"))
	f.Add([]byte(strings.Repeat("a", maxFileLine+1)))
	f.Add([]byte("\x00\xff#\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		entries, err := parseFile(bytes.NewReader(data), "allow.txt")
		if err != nil {
			if !strings.HasPrefix(err.Error(), "allow-list file allow.txt") {
				t.Fatalf("error %q does not name the file", err)
			}
			return
		}
		lines := strings.Split(string(data), "\n")
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
