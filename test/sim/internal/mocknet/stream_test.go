package mocknet

import (
	"context"
	"errors"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
)

// linkedPair returns two linked and connected hosts on mn whose link has
// the latency.
func linkedPair(t *testing.T, mn Mocknet, latency time.Duration) (a, b host.Host) {
	t.Helper()
	a, err := mn.GenPeer()
	if err != nil {
		t.Fatal(err)
	}
	b, err = mn.GenPeer()
	if err != nil {
		t.Fatal(err)
	}
	l, err := mn.LinkPeers(a.ID(), b.ID())
	if err != nil {
		t.Fatal(err)
	}
	l.SetOptions(LinkOptions{Latency: latency})
	if _, err := mn.ConnectPeers(a.ID(), b.ID()); err != nil {
		t.Fatal(err)
	}
	return a, b
}

// TestStreamPipelinesWrites: 100 writes of 1 KiB made at once over a link
// of 50 ms all arrive 50 ms later, in order; go-libp2p's mocknet delivered
// the last one about 2.5 s later.
func TestStreamPipelinesWrites(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mn := New()
		defer func() { _ = mn.Close() }()
		a, b := linkedPair(t, mn, 50*time.Millisecond)
		const n, size = 100, 1024
		got := make(chan time.Duration, n)
		start := time.Now()
		b.SetStreamHandler("/test", func(s network.Stream) {
			defer func() { _ = s.Close() }()
			buf := make([]byte, size)
			for i := 0; i < n; i++ {
				if _, err := io.ReadFull(s, buf); err != nil {
					t.Error(err)
					return
				}
				if buf[0] != byte(i) {
					t.Errorf("message %d arrived as number %d", buf[0], i)
				}
				got <- time.Since(start)
			}
		})
		s, err := a.NewStream(context.Background(), b.ID(), "/test")
		if err != nil {
			t.Fatal(err)
		}
		start = time.Now()
		msg := make([]byte, size)
		for i := 0; i < n; i++ {
			msg[0] = byte(i)
			if _, err := s.Write(msg); err != nil {
				t.Fatal(err)
			}
		}
		if waited := time.Since(start); waited != 0 {
			t.Errorf("the writes took %v, want no wait", waited)
		}
		for i := 0; i < n; i++ {
			if d := <-got; d != 50*time.Millisecond {
				t.Fatalf("message %d arrived after %v, want 50ms", i, d)
			}
		}
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
}

// TestStreamCloseWriteDelivers: what was written before CloseWrite still
// arrives after the latency, then the reader gets EOF.
func TestStreamCloseWriteDelivers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mn := New()
		defer func() { _ = mn.Close() }()
		a, b := linkedPair(t, mn, 20*time.Millisecond)
		type result struct {
			data []byte
			at   time.Duration
			err  error
		}
		done := make(chan result, 1)
		start := time.Now()
		b.SetStreamHandler("/test", func(s network.Stream) {
			defer func() { _ = s.Close() }()
			data, err := io.ReadAll(s)
			done <- result{data, time.Since(start), err}
		})
		s, err := a.NewStream(context.Background(), b.ID(), "/test")
		if err != nil {
			t.Fatal(err)
		}
		start = time.Now()
		for _, part := range []string{"hello, ", "world"} {
			if _, err := s.Write([]byte(part)); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.CloseWrite(); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Write([]byte("late")); !errors.Is(err, ErrClosed) {
			t.Errorf("a write after CloseWrite returned %v, want ErrClosed", err)
		}
		r := <-done
		if r.err != nil || string(r.data) != "hello, world" || r.at != 20*time.Millisecond {
			t.Errorf("read %q, %v after %v; want \"hello, world\", no error after 20ms", r.data, r.err, r.at)
		}
		_ = s.Close()
	})
}

// TestStreamReset: a reset fails the reads and writes of both ends at
// once and discards what was still under way.
func TestStreamReset(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mn := New()
		defer func() { _ = mn.Close() }()
		a, b := linkedPair(t, mn, 20*time.Millisecond)
		remote := make(chan network.Stream, 1)
		b.SetStreamHandler("/test", func(s network.Stream) { remote <- s })
		s, err := a.NewStream(context.Background(), b.ID(), "/test")
		if err != nil {
			t.Fatal(err)
		}
		// The first write carries the protocol; the handler runs once it
		// arrived.
		if _, err := s.Write([]byte("hi")); err != nil {
			t.Fatal(err)
		}
		r := <-remote
		if _, err := io.ReadFull(r, make([]byte, 2)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Write([]byte("lost")); err != nil {
			t.Fatal(err)
		}
		readErr := make(chan error, 1)
		go func() {
			_, err := r.Read(make([]byte, 16))
			readErr <- err
		}()
		synctest.Wait() // the remote reader waits for the write to arrive
		if err := s.Reset(); err != nil {
			t.Fatal(err)
		}
		if err := <-readErr; !errors.Is(err, network.ErrReset) {
			t.Errorf("the remote read returned %v, want ErrReset", err)
		}
		if _, err := r.Write([]byte("x")); !errors.Is(err, network.ErrReset) {
			t.Errorf("a remote write returned %v, want ErrReset", err)
		}
		if _, err := s.Read(make([]byte, 1)); !errors.Is(err, network.ErrReset) {
			t.Errorf("a local read returned %v, want ErrReset", err)
		}
		_ = r.Reset()
	})
}

// TestConnCloseResetsStreams: closing a connection resets its streams at
// both ends.
func TestConnCloseResetsStreams(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mn := New()
		defer func() { _ = mn.Close() }()
		a, b := linkedPair(t, mn, 5*time.Millisecond)
		remote := make(chan network.Stream, 1)
		b.SetStreamHandler("/test", func(s network.Stream) { remote <- s })
		s, err := a.NewStream(context.Background(), b.ID(), "/test")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Write([]byte("hi")); err != nil {
			t.Fatal(err)
		}
		r := <-remote
		if err := s.Conn().Close(); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if _, err := r.Read(make([]byte, 2)); !errors.Is(err, network.ErrReset) {
			t.Errorf("the remote read returned %v, want ErrReset", err)
		}
		if n := len(b.Network().ConnsToPeer(a.ID())); n != 0 {
			t.Errorf("the remote still has %d connections", n)
		}
	})
}

// TestClosedNetworkRefusesConnections: once a peer's network is closed, no
// connection to or from it opens, although the link remains; go-libp2p's
// mocknet opened one that nobody closed.
func TestClosedNetworkRefusesConnections(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mn := New()
		defer func() { _ = mn.Close() }()
		a, err := mn.GenPeer()
		if err != nil {
			t.Fatal(err)
		}
		b, err := mn.GenPeer()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := mn.LinkPeers(a.ID(), b.ID()); err != nil {
			t.Fatal(err)
		}
		if err := b.Network().Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := a.Network().DialPeer(context.Background(), b.ID()); err == nil {
			t.Error("dialing a closed network opened a connection")
		}
		if _, err := b.Network().DialPeer(context.Background(), a.ID()); err == nil {
			t.Error("a closed network dialed out")
		}
		synctest.Wait()
		if n, m := len(a.Network().Conns()), len(b.Network().Conns()); n+m != 0 {
			t.Errorf("the peers have %d and %d connections, want none", n, m)
		}
	})
}
