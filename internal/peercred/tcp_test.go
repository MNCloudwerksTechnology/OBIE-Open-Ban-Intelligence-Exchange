package peercred

import (
	"encoding/binary"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"testing"
)

// kernelAddr formats ap like the kernel's socket table: each 32-bit word
// of the address read in host byte order and printed as hex, then the port.
func kernelAddr(ap netip.AddrPort) string {
	raw := ap.Addr().AsSlice()
	var b strings.Builder
	for i := 0; i < len(raw); i += 4 {
		fmt.Fprintf(&b, "%08X", binary.NativeEndian.Uint32(raw[i:]))
	}
	return fmt.Sprintf("%s:%04X", b.String(), ap.Port())
}

func tableLine(local, remote netip.AddrPort, state string, uid int) string {
	return fmt.Sprintf("   3: %s %s %s 00000000:00000000 00:00000000 00000000  %d        0 123456 1 0000000000000000 20 4 30 10 -1",
		kernelAddr(local), kernelAddr(remote), state, uid)
}

const tableHeader = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode"

func TestParseSocketTable(t *testing.T) {
	server := netip.MustParseAddrPort("127.0.0.1:9465")
	client := netip.MustParseAddrPort("127.0.0.1:54321")
	other := netip.MustParseAddrPort("127.0.0.1:54322")
	server6 := netip.MustParseAddrPort("[::1]:9465")
	client6 := netip.MustParseAddrPort("[::1]:40000")
	table := strings.Join([]string{
		tableHeader,
		tableLine(server, netip.MustParseAddrPort("0.0.0.0:0"), "0A", 997),
		tableLine(server, client, "01", 997), // the server end, owned by obied
		tableLine(client, server, "06", 0),   // an old connection in TIME_WAIT
		tableLine(other, server, "01", 1001),
		tableLine(client, server, "01", 1000),
	}, "\n")
	table6 := strings.Join([]string{tableHeader, tableLine(server6, client6, "01", 997), tableLine(client6, server6, "01", 1234)}, "\n")

	for name, tc := range map[string]struct {
		table          string
		client, server netip.AddrPort
		uid            uint32
		err            string
	}{
		"ipv4 client end":       {table, client, server, 1000, ""},
		"other client":          {table, other, server, 1001, ""},
		"ipv6 client end":       {table6, client6, server6, 1234, ""},
		"not in table":          {table, netip.MustParseAddrPort("127.0.0.1:1"), server, 0, "is not in the socket table"},
		"only in time wait":     {tableHeader + "\n" + tableLine(client, server, "06", 0), client, server, 0, "is not in the socket table"},
		"empty":                 {"", client, server, 0, "no header"},
		"garbage lines skipped": {tableHeader + "\n  0: xyz\n  1: 0100007F 00 01\n" + tableLine(client, server, "01", 5), client, server, 5, ""},
		"invalid uid":           {tableHeader + "\n" + strings.Replace(tableLine(client, server, "01", 5), "  5  ", "  x  ", 1), client, server, 0, `invalid uid "x"`},
	} {
		t.Run(name, func(t *testing.T) {
			uid, err := parseSocketTable(strings.NewReader(tc.table), tc.client, tc.server)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Errorf("= %d, %v; want error containing %q", uid, err, tc.err)
				}
				return
			}
			if err != nil || uid != tc.uid {
				t.Errorf("= %d, %v; want %d", uid, err, tc.uid)
			}
		})
	}
}

func TestParseHexAddrPort(t *testing.T) {
	for _, s := range []string{"127.0.0.1:9465", "127.1.2.3:1", "[::1]:9465", "[2001:db8::1]:443", "0.0.0.0:0"} {
		want := netip.MustParseAddrPort(s)
		if got, err := parseHexAddrPort(kernelAddr(want)); err != nil || got != want {
			t.Errorf("parseHexAddrPort(%s) = %v, %v; want %v", kernelAddr(want), got, err, want)
		}
	}
	if binary.NativeEndian.Uint16([]byte{1, 0}) == 1 { // little-endian: the kernel's text as seen on amd64 and arm64
		for text, want := range map[string]string{
			"0100007F:24D9":                         "127.0.0.1:9433",
			"00000000000000000000000001000000:24D9": "[::1]:9433",
			"0000000000000000FFFF00000100007F:0050": "127.0.0.1:80",
		} {
			if got, err := parseHexAddrPort(text); err != nil || got.String() != want {
				t.Errorf("parseHexAddrPort(%s) = %v, %v; want %s", text, got, err, want)
			}
		}
	}
	for _, bad := range []string{"0100007F", "0100007F:XYZ", "0100007F:10000", "01007F:0050", "zz00007F:0050"} {
		if got, err := parseHexAddrPort(bad); err == nil {
			t.Errorf("parseHexAddrPort(%q) = %v, want an error", bad, got)
		}
	}
}

// fakeConn is a net.Conn with fixed addresses.
type fakeConn struct {
	net.Conn
	local, remote net.Addr
}

func (c fakeConn) LocalAddr() net.Addr  { return c.local }
func (c fakeConn) RemoteAddr() net.Addr { return c.remote }

func TestLoopbackTCPRefusesOtherConnections(t *testing.T) {
	tcp := func(s string) net.Addr { return net.TCPAddrFromAddrPort(netip.MustParseAddrPort(s)) }
	for name, c := range map[string]net.Conn{
		"unix socket": fakeConn{local: &net.UnixAddr{Name: "/run/obie/obie.sock", Net: "unix"}, remote: &net.UnixAddr{Net: "unix"}},
		"remote peer": fakeConn{local: tcp("192.0.2.1:9465"), remote: tcp("192.0.2.7:50000")},
	} {
		if cred, err := LoopbackTCP(c); err == nil {
			t.Errorf("%s: LoopbackTCP = %+v, want an error", name, cred)
		}
	}
	server, client, err := loopbackEnds(fakeConn{local: tcp("[::ffff:127.0.0.1]:9465"), remote: tcp("[::ffff:127.0.0.1]:5000")})
	if err != nil || server.String() != "127.0.0.1:9465" || client.String() != "127.0.0.1:5000" {
		t.Errorf("loopbackEnds(mapped) = %v, %v, %v; want unmapped IPv4 ends", server, client, err)
	}
}
