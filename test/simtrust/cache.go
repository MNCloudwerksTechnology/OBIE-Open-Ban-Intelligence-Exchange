package simtrust

import (
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// runCache keeps the metrics of finished runs in a directory, so that an
// interrupted scenario resumes where it stopped. A run's key covers the
// program that ran it, so a change to the code never reuses a result.
type runCache struct {
	dir string
	// base is the hash of the program, the scenario and the trace.
	base []byte
}

// newRunCache returns the cache in dir for sc replaying trace, if any;
// nil if dir is empty.
func newRunCache(dir string, sc Scenario, trace *Trace) (*runCache, error) {
	if dir == "" {
		return nil, nil
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	h := sha256.New()
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("run cache: %w", err)
	}
	f, err := os.Open(exe) // #nosec G304 -- this program itself.
	if err != nil {
		return nil, fmt.Errorf("run cache: %w", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := io.Copy(h, f); err != nil {
		return nil, fmt.Errorf("run cache: %w", err)
	}
	_, _ = fmt.Fprintf(h, "%s\n%+v\n%+v\n", sc.Name, sc.World, sc.Models)
	if trace != nil {
		if err := WriteTrace(h, trace); err != nil {
			return nil, err
		}
	}
	return &runCache{dir: dir, base: h.Sum(nil)}, nil
}

// path returns the file of the run of spec.
func (c *runCache) path(spec RunSpec) string {
	h := sha256.New()
	h.Write(c.base)
	_, _ = fmt.Fprintf(h, "%+v", spec)
	return filepath.Join(c.dir, hex.EncodeToString(h.Sum(nil))+".gob")
}

// load returns the metrics of the run of spec, if the cache has them.
func (c *runCache) load(spec RunSpec) (*Metrics, bool) {
	if c == nil {
		return nil, false
	}
	f, err := os.Open(c.path(spec))
	if err != nil {
		return nil, false
	}
	defer func() { _ = f.Close() }()
	var m Metrics
	if err := gob.NewDecoder(f).Decode(&m); err != nil || m.Spec != spec {
		return nil, false
	}
	return &m, true
}

// store keeps the metrics m of a run; the file appears whole or not at
// all.
func (c *runCache) store(m *Metrics) error {
	if c == nil {
		return nil
	}
	path := c.path(m.Spec)
	tmp, err := os.CreateTemp(c.dir, ".run-*")
	if err != nil {
		return err
	}
	if err := gob.NewEncoder(tmp).Encode(m); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
