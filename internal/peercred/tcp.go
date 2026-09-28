package peercred

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// lookups bounds the socket-table lookups that run at once: each reads the
// whole table, and any local user can open connections.
var lookups = make(chan struct{}, 2)

// LoopbackTCP returns the owner of the client end of a TCP connection over
// the loopback interface, c being the server end. Loopback TCP carries no
// credentials, so the owner is looked up in the kernel's socket table. PID
// is 0 and GID is UnknownGID: the table records only the owner's UID. A
// client that closed its end has no owner any more and yields an error.
func LoopbackTCP(c net.Conn) (Cred, error) {
	server, client, err := loopbackEnds(c)
	if err != nil {
		return Cred{}, err
	}
	lookups <- struct{}{}
	uid, err := socketOwner(client, server)
	<-lookups
	if err != nil {
		return Cred{}, err
	}
	return Cred{UID: uid, GID: UnknownGID}, nil
}

// loopbackEnds returns the local (server) and remote (client) address of
// the TCP connection c, which must come from a loopback address.
func loopbackEnds(c net.Conn) (server, client netip.AddrPort, err error) {
	local, ok := c.LocalAddr().(*net.TCPAddr)
	remote, ok2 := c.RemoteAddr().(*net.TCPAddr)
	if !ok || !ok2 {
		return server, client, fmt.Errorf("not a TCP connection: %T", c)
	}
	server, client = unmap(local.AddrPort()), unmap(remote.AddrPort())
	if !client.Addr().IsLoopback() {
		return server, client, fmt.Errorf("connection from %s is not over the loopback interface", client)
	}
	return server, client, nil
}

func unmap(ap netip.AddrPort) netip.AddrPort {
	return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
}

// errNotInTable means the client socket has no entry with an owner in
// the socket table, e.g. because the client already closed it.
var errNotInTable = errors.New("is not in the socket table")

// parseSocketTable finds the socket whose local address is client and
// whose remote address is server in a socket table in the format of
// /proc/net/tcp and /proc/net/tcp6, and returns the UID of its owner.
//
// A socket that no process holds any more — one in TIME_WAIT, or one its
// client closed (FIN_WAIT2 and the like) — is listed with inode 0 and UID
// 0; it has no owner and never matches, so it cannot pass for root.
func parseSocketTable(r io.Reader, client, server netip.AddrPort) (uint32, error) {
	sc := bufio.NewScanner(r)
	if !sc.Scan() {
		return 0, errors.Join(errors.New("socket table: no header"), sc.Err())
	}
	for sc.Scan() {
		// sl local_address rem_address st tx_queue:rx_queue tr:tm->when retrnsmt uid timeout inode ...
		f := strings.Fields(sc.Text())
		if len(f) < 10 || f[9] == "0" {
			continue
		}
		if local, err := parseHexAddrPort(f[1]); err != nil || local != client {
			continue
		}
		if remote, err := parseHexAddrPort(f[2]); err != nil || remote != server {
			continue
		}
		uid, err := strconv.ParseUint(f[7], 10, 32)
		if err != nil {
			return 0, fmt.Errorf("socket table: invalid uid %q", f[7])
		}
		return uint32(uid), nil
	}
	if err := sc.Err(); err != nil {
		return 0, fmt.Errorf("socket table: %w", err)
	}
	return 0, fmt.Errorf("socket %s -> %s %w", client, server, errNotInTable)
}

// parseHexAddrPort parses an address of the socket table: the address as
// 32-bit words, each printed as a host-order hex number, then ':' and the
// port in hex.
func parseHexAddrPort(s string) (netip.AddrPort, error) {
	hexAddr, hexPort, ok := strings.Cut(s, ":")
	if !ok {
		return netip.AddrPort{}, fmt.Errorf("address %q has no port", s)
	}
	port, err := strconv.ParseUint(hexPort, 16, 16)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("address %q: invalid port: %w", s, err)
	}
	words, err := hex.DecodeString(hexAddr)
	if err != nil || (len(words) != 4 && len(words) != 16) {
		return netip.AddrPort{}, fmt.Errorf("address %q: invalid IP", s)
	}
	// Each word holds network-order bytes read as a host-order integer;
	// writing the integer back in host order restores the bytes.
	raw := make([]byte, len(words))
	for i := 0; i < len(words); i += 4 {
		binary.NativeEndian.PutUint32(raw[i:], binary.BigEndian.Uint32(words[i:]))
	}
	addr, _ := netip.AddrFromSlice(raw)
	return netip.AddrPortFrom(addr.Unmap(), uint16(port)), nil
}
