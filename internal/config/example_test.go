package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// examplePath is the documented example configuration, relative to this package.
var examplePath = filepath.Join("..", "..", "documentation", "examples", "obie.yaml")

// TestExampleDocumentsDefaults keeps the example from drifting: it must spell
// out every key of every section, with the default value.
func TestExampleDocumentsDefaults(t *testing.T) {
	data, err := os.ReadFile(examplePath) // #nosec G304 -- fixed repository path.
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	lines, ps, err := decode(data, &cfg)
	if err != nil || len(ps) > 0 {
		t.Fatalf("decode example: %v %v", err, ps)
	}
	if want := Default(); !reflect.DeepEqual(cfg, want) {
		t.Errorf("example values = %+v, want the defaults %+v", cfg, want)
	}
	for _, path := range keyPaths(reflect.TypeOf(Config{}), "") {
		if _, ok := lines[path]; !ok {
			t.Errorf("example does not document key %s", path)
		}
	}
}

// keyPaths lists the key paths of t's fields, descending into nested
// sections but not into list items.
func keyPaths(t reflect.Type, prefix string) []string {
	var paths []string
	for _, key := range sortedKeys(structFields(t)) {
		path := joinPath(prefix, key)
		paths = append(paths, path)
		f := t.Field(structFields(t)[key].index)
		if f.Type.Kind() == reflect.Struct && !reflect.PointerTo(f.Type).Implements(textUnmarshalerType) {
			paths = append(paths, keyPaths(f.Type, path)...)
		}
	}
	return paths
}
