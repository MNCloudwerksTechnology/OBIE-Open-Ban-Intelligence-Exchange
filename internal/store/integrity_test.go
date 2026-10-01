package store

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newDiskStore writes a store with n verdicts into a new directory and
// stops it.
func newDiskStore(t *testing.T, n int) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "db")
	clk := newClock()
	db := New(dir, discardLogger(), Options{Now: clk.Now})
	if err := db.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	for i := range n {
		mustPut(t, db, verdict(pubB, ipv4(fmt.Sprintf("85.0.%d.%d", i>>8, i&0xff)), clk.Now(), time.Hour), true)
	}
	if err := db.SetOverride(Override{Indicator: ipv4("85.1.0.1"), Action: ForceAllow, CreatedAt: clk.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := db.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	return dir
}

// dataFile returns the path of the first file in dir with suffix.
func dataFile(t *testing.T, dir, suffix string) string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(dir, "*"+suffix))
	if err != nil || len(m) == 0 {
		t.Fatalf("no %s file in %s: %v", suffix, dir, err)
	}
	return m[0]
}

func overwrite(t *testing.T, path string, off int64, data []byte) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR, 0) // #nosec G304 -- a test file.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteAt(data, off); err != nil {
		t.Fatal(err)
	}
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

var garbage = []byte("GARBAGE GARBAGE GARBAGE GARBAGE")

func TestStartDetectsCorruption(t *testing.T) {
	tests := []struct {
		name    string
		corrupt func(t *testing.T, dir string)
		want    string
	}{
		{"manifest damaged", func(t *testing.T, dir string) {
			overwrite(t, filepath.Join(dir, manifestFile), 10, garbage)
		}, "Manifest file might be corrupted"},
		{"manifest truncated", func(t *testing.T, dir string) {
			if err := os.Truncate(filepath.Join(dir, manifestFile), 5); err != nil {
				t.Fatal(err)
			}
		}, "manifest has bad magic"},
		{"manifest missing", func(t *testing.T, dir string) {
			if err := os.Remove(filepath.Join(dir, manifestFile)); err != nil {
				t.Fatal(err)
			}
		}, "MANIFEST is missing"},
		{"key registry damaged", func(t *testing.T, dir string) {
			overwrite(t, filepath.Join(dir, "KEYREGISTRY"), 0, garbage)
		}, "KEYREGISTRY"},
		{"value log header damaged", func(t *testing.T, dir string) {
			overwrite(t, dataFile(t, dir, ".vlog"), 0, garbage)
		}, "000001.vlog is damaged"},
		{"value log empty", func(t *testing.T, dir string) {
			if err := os.Truncate(dataFile(t, dir, ".vlog"), 0); err != nil {
				t.Fatal(err)
			}
		}, "000001.vlog is empty"},
		{"table block damaged", func(t *testing.T, dir string) {
			overwrite(t, dataFile(t, dir, ".sst"), 100, garbage)
		}, "checksum validation failed"},
		{"table index damaged", func(t *testing.T, dir string) {
			path := dataFile(t, dir, ".sst")
			overwrite(t, path, fileSize(t, path)-200, garbage)
		}, "000001.sst"},
		{"table missing", func(t *testing.T, dir string) {
			if err := os.Remove(dataFile(t, dir, ".sst")); err != nil {
				t.Fatal(err)
			}
		}, "table 000001.sst listed in MANIFEST"},
		{"table truncated", func(t *testing.T, dir string) {
			if err := os.Truncate(dataFile(t, dir, ".sst"), 100); err != nil {
				t.Fatal(err)
			}
		}, "000001.sst is truncated or damaged"},
		{"table emptied", func(t *testing.T, dir string) {
			if err := os.Truncate(dataFile(t, dir, ".sst"), 0); err != nil {
				t.Fatal(err)
			}
		}, "000001.sst is truncated"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := newDiskStore(t, 2000)
			tt.corrupt(t, dir)
			db := New(dir, discardLogger(), Options{})
			err := db.Start(context.Background())
			if err == nil {
				_ = db.Stop(context.Background())
				t.Fatal("Start opened a damaged store")
			}
			msg := err.Error()
			if !errors.Is(err, ErrCorrupt) || !strings.Contains(msg, tt.want) || !strings.Contains(msg, dir) ||
				!strings.Contains(msg, "mv "+dir+" "+dir+".corrupt") {
				t.Errorf("Start error = %q, want ErrCorrupt naming the damage (%q), the directory and the remedy", msg, tt.want)
			}
			if err := db.Ready(); !errors.Is(err, ErrClosed) {
				t.Errorf("Ready after a failed start = %v, want ErrClosed", err)
			}
		})
	}
}

func TestStartOpensHealthyStore(t *testing.T) {
	dir := newDiskStore(t, 2000)
	db := startDB(t, New(dir, discardLogger(), Options{}))
	if got := db.Verdicts(); got != 2000 {
		t.Errorf("Verdicts() after reopening = %d, want 2000", got)
	}
}

// TestStartOpensStoreAfterCrash copies a store while it is open, as a
// crash or power loss leaves it: full memtable and value logs, no clean
// shutdown. The copy must open with every verdict, not be reported as
// corrupt.
func TestStartOpensStoreAfterCrash(t *testing.T) {
	clk := newClock()
	db := startDB(t, New(filepath.Join(t.TempDir(), "db"), discardLogger(), Options{Now: clk.Now}))
	for i := range 3000 {
		mustPut(t, db, verdict(pubB, ipv4(fmt.Sprintf("85.0.%d.%d", i>>8, i&0xff)), clk.Now(), time.Hour), true)
	}
	crashed := filepath.Join(t.TempDir(), "db")
	if err := os.CopyFS(crashed, os.DirFS(db.dir)); err != nil {
		t.Fatal(err)
	}
	if got := startDB(t, New(crashed, discardLogger(), Options{Now: clk.Now})).Verdicts(); got != 3000 {
		t.Errorf("Verdicts() after the crash = %d, want 3000", got)
	}
}

func TestStartReportsStoreInUse(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "db")
	startDB(t, New(dir, discardLogger(), Options{}))
	err := New(dir, discardLogger(), Options{}).Start(context.Background())
	if err == nil || errors.Is(err, ErrCorrupt) || !strings.Contains(err.Error(), "is another obied running?") {
		t.Errorf("Start on a store in use = %v, want an error naming another obied", err)
	}
}

// TestStartNeverCrashesOnDamage damages random bytes of random files of a
// store: Start either opens it or reports it as corrupt, but never crashes
// the process.
func TestStartNeverCrashesOnDamage(t *testing.T) {
	rounds := 300
	if testing.Short() {
		rounds = 5
	}
	base := newDiskStore(t, 3000)
	names, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	for round := range rounds {
		rng := rand.New(rand.NewPCG(uint64(round), 7)) // #nosec G404 -- reproducible test damage.
		name := names[rng.IntN(len(names))].Name()
		dir := filepath.Join(t.TempDir(), "db")
		if err := os.CopyFS(dir, os.DirFS(base)); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, name)
		size := fileSize(t, path)
		if size == 0 {
			continue
		}
		off := rng.Int64N(size)
		how := "overwrote"
		if rng.IntN(3) == 0 {
			how = "truncated"
			if err := os.Truncate(path, off); err != nil {
				t.Fatal(err)
			}
		} else {
			overwrite(t, path, off, garbage[:1+rng.IntN(len(garbage))])
		}
		db := New(dir, discardLogger(), Options{})
		if err := db.Start(context.Background()); err != nil {
			t.Logf("round %d: %s %s at %d: %v", round, how, name, off, err)
			continue
		}
		if _, err := db.ListIndicators(time.Now(), Filter{}, Page{Limit: MaxPageLimit}); err != nil {
			t.Logf("round %d: %s %s at %d: opened, listing failed: %v", round, how, name, off, err)
		}
		if err := db.Stop(context.Background()); err != nil {
			t.Logf("round %d: stop: %v", round, err)
		}
	}
}

// TestCacheBytes checks that CacheBytes reports what reading tables puts
// into Badger's caches, and 0 once the store is closed.
func TestCacheBytes(t *testing.T) {
	db := New(newDiskStore(t, 3000), discardLogger(), Options{})
	if err := db.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ListIndicators(time.Now(), Filter{}, Page{Limit: MaxPageLimit}); err != nil {
		t.Fatal(err)
	}
	// The caches admit entries asynchronously.
	deadline := time.Now().Add(5 * time.Second)
	for db.CacheBytes() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := db.CacheBytes(); n <= 0 {
		t.Errorf("CacheBytes() = %d after reading the tables, want > 0", n)
	}
	if err := db.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := db.CacheBytes(); n != 0 {
		t.Errorf("CacheBytes() = %d on a closed store, want 0", n)
	}
}
