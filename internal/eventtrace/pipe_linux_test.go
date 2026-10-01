//go:build linux

package eventtrace

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestOpenWritesToANamedPipe: a named pipe as the trace path, e.g. to
// stream the trace to another program, is only opened for writing: Open
// returns once a reader is there, and the records arrive.
func TestOpenWritesToANamedPipe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.pipe")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	read := make(chan []byte, 1)
	go func() {
		f, err := os.Open(path) // #nosec G304 -- test file; blocks until Open opens it for writing.
		if err != nil {
			read <- nil
			return
		}
		defer func() { _ = f.Close() }()
		data, _ := io.ReadAll(f)
		read <- data
	}()
	opened := make(chan *Writer, 1)
	go func() {
		w, err := Open(path, "node-a", slog.New(slog.DiscardHandler))
		if err != nil {
			t.Error(err)
		}
		opened <- w
	}()
	var w *Writer
	select {
	case w = <-opened:
	case <-time.After(5 * time.Second):
		t.Fatal("Open blocks on a named pipe")
	}
	if w == nil {
		t.FailNow()
	}
	w.Write("e1", "node-b", t0, Accepted)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	recs, err := Read(bytes.NewReader(<-read))
	if err != nil || len(recs) != 1 || recs[0].Event != "e1" {
		t.Errorf("records through the pipe = %+v, %v; want e1", recs, err)
	}
}
