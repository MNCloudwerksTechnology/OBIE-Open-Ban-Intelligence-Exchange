package audit

import (
	"bytes"
	"encoding/json"
	"io"
)

// scanChunk is how much of the file scanBack reads at once; maxLine the
// longest line it assembles, beyond which a line is skipped unread.
const (
	scanChunk = 64 << 10
	maxLine   = 1 << 20
)

// scanBack calls fn with the lines of r that end at or before the offset
// end, the last first, without their newline, and with the offset where
// each starts. It stops when fn returns false, before the line fn was
// given, or once it read budget bytes, and returns the offset where the
// lines it did not give fn end: 0 once it reached the start. A line longer
// than maxLine is given as nil.
func scanBack(r io.ReaderAt, end, budget int64, fn func(line []byte, start int64) bool) (int64, error) {
	var (
		pos     = end // data holds the bytes from pos to lineEnd
		lineEnd = end
		data    []byte
		long    bool // the line being assembled is longer than maxLine
		read    int64
	)
	for lineEnd > 0 {
		// The line ending at lineEnd starts after the last newline before
		// its own.
		if len(data) > 0 {
			if i := bytes.LastIndexByte(data[:len(data)-1], '\n'); i >= 0 || pos == 0 {
				start := pos + int64(i) + 1
				line := bytes.TrimSuffix(data[i+1:], []byte{'\n'})
				if long {
					line = nil
				}
				if !fn(line, start) {
					return lineEnd, nil
				}
				lineEnd, data, long = start, data[:i+1], false
				continue
			}
		}
		if read >= budget {
			if lineEnd == end {
				return pos, nil // not even one line fit: go on from here
			}
			return lineEnd, nil
		}
		n := min(scanChunk, pos)
		chunk := make([]byte, n, n+int64(len(data)))
		if _, err := r.ReadAt(chunk, pos-n); err != nil {
			return lineEnd, err
		}
		pos, read = pos-n, read+n
		if len(data) > maxLine {
			// Keep only the line's last byte, its newline, and forget the
			// rest of it.
			long, data = true, data[len(data)-1:]
		}
		data = append(chunk, data...)
	}
	return 0, nil
}

// parseEntry reads a line of the audit log; ok is false if it is none.
func parseEntry(line []byte) (e Entry, ok bool) {
	if len(line) == 0 || json.Unmarshal(line, &e) != nil {
		return Entry{}, false
	}
	if e.Event.Dataset != "obie.audit" || e.Event.Action == "" || e.Time().IsZero() {
		return Entry{}, false
	}
	return e, true
}
