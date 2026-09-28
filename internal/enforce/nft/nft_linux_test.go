//go:build linux

package nft

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/netip"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/google/nftables"
	"github.com/mdlayher/netlink"

	"github.com/MNCloudwerksTechnology/obie/internal/enforce"
)

func TestSplit(t *testing.T) {
	c := func(size int) chunk { return chunk{size: size} }
	sizes := func(txs [][]chunk) [][]int {
		var out [][]int
		for _, tx := range txs {
			var s []int
			for _, c := range tx {
				s = append(s, c.size)
			}
			out = append(out, s)
		}
		return out
	}
	tests := []struct {
		name           string
		chunks         []chunk
		maxBytes, msgs int
		want           [][]int
	}{
		{"all in one", []chunk{c(100), c(100), c(100)}, 1 << 20, 100, [][]int{{100, 100, 100}}},
		{"split by bytes in order", []chunk{c(100), c(100), c(100)}, batchOverhead + 200, 100, [][]int{{100, 100}, {100}}},
		{"split by acks", []chunk{c(100), c(100), c(100)}, 1 << 20, 2, [][]int{{100, 100}, {100}}},
		{"oversized chunk alone", []chunk{c(10), c(500), c(10)}, batchOverhead + 100, 100, [][]int{{10}, {500}, {10}}},
		{"no ack budget", []chunk{c(10), c(10)}, 1 << 20, 0, [][]int{{10}, {10}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sizes(split(tt.chunks, tt.maxBytes, tt.msgs)); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("split = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestChunks checks the limits of the element messages and that their
// size estimate bounds what the library actually sends.
func TestChunks(t *testing.T) {
	now := time.Now()
	b := New(Options{Now: func() time.Time { return now }}, slog.New(slog.DiscardHandler))
	var add, remove []enforce.Entry
	for i := range 1500 {
		v6 := netip.AddrFrom16([16]byte{0x20, 0x01, 0x0d, 0xb8, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, byte(i >> 8), byte(i)})
		add = append(add, enforce.Entry{Prefix: netip.PrefixFrom(v6, 128), Expires: now.Add(time.Hour)})
		v4 := netip.AddrFrom4([4]byte{10, 0, byte(i >> 8), byte(i)})
		remove = append(remove, enforce.Entry{Prefix: netip.PrefixFrom(v4, 32), Expires: now.Add(time.Hour)})
	}
	chunks, err := b.chunks(add, remove)
	if err != nil {
		t.Fatal(err)
	}
	var sent []int
	conn, err := nftables.New(nftables.WithTestDial(func(req []netlink.Message) ([]netlink.Message, error) {
		for _, m := range req {
			if m.Header.Flags&netlink.Acknowledge != 0 {
				sent = append(sent, 16+len(m.Data))
			}
		}
		return nil, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range chunks {
		if len(c.elems) > MaxChunk || c.size > maxChunkBytes+messageOverhead {
			t.Errorf("chunk %d: %d elements, %d bytes", i, len(c.elems), c.size)
		}
		if i > 0 && c.del && !chunks[i-1].del {
			t.Errorf("chunk %d: a removal after an addition", i)
		}
		if c.del {
			_ = conn.SetDeleteElements(c.set, c.elems)
		} else {
			_ = conn.SetAddElements(c.set, c.elems)
		}
	}
	_ = conn.Flush()
	if len(sent) != len(chunks) {
		t.Fatalf("sent %d messages for %d chunks", len(sent), len(chunks))
	}
	for i, n := range sent {
		if n > chunks[i].size {
			t.Errorf("chunk %d: sent %d bytes, estimated %d", i, n, chunks[i].size)
		}
	}
}

// TestNeedsCapNetAdmin runs unprivileged: the kernel refuses every
// request and the error says what is missing.
func TestNeedsCapNetAdmin(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	b := New(Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"Setup":    func() error { return b.Setup(ctx) },
		"List":     func() error { _, err := b.List(ctx); return err },
		"Teardown": func() error { return b.Teardown(ctx) },
	} {
		err := call()
		if !errors.Is(err, ErrPermission) {
			t.Errorf("%s: err = %v, want ErrPermission", name, err)
		}
	}
}
