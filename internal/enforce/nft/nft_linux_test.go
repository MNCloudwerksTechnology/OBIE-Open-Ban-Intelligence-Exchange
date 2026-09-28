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
		name   string
		chunks []chunk
		limit  int
		want   [][]int
	}{
		{"all in one", []chunk{c(100), c(100), c(100)}, 1 << 20, [][]int{{100, 100, 100}}},
		{"split in order", []chunk{c(100), c(100), c(100)}, batchOverhead + 200, [][]int{{100, 100}, {100}}},
		{"oversized chunk alone", []chunk{c(10), c(500), c(10)}, batchOverhead + 100, [][]int{{10}, {500}, {10}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sizes(split(tt.chunks, tt.limit)); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("split = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMessageSizeIsAnUpperBound(t *testing.T) {
	now := time.Now()
	var elems []nftables.SetElement
	for _, p := range []string{"192.0.2.1/32", "2001:db8::/64", "255.255.255.255/32"} {
		els, err := elementsOf(enforce.Entry{Prefix: netip.MustParsePrefix(p), Expires: now.Add(time.Hour)}, false, now)
		if err != nil {
			t.Fatal(err)
		}
		elems = append(elems, els...)
	}
	// start: 4 + 12 + key, timeout 12, comment 12 + len; end: 4 + 12 + key + flags 8
	if got, want := messageSize(elems), messageOverhead+(16+4+12+12+12)+(16+4+8)+(16+16+12+12+13)+(16+16+8)+(16+4+12+12+18); got != want {
		t.Errorf("messageSize = %d, want %d", got, want)
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
