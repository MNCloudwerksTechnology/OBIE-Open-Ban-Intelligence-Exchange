package config

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// referencePath is the operator configuration reference, relative to this
// package.
var referencePath = filepath.Join("..", "..", "documentation", "operations", "configuration.md")

// referenceRow matches a key row of the reference tables:
// | `key` | `default` | reload or restart | meaning |.
var referenceRow = regexp.MustCompile("^\\| `([a-z_.]+)` \\| `([^`]*)` \\| ([a-z]+) \\|")

// TestConfigurationReferenceDocumentsEveryKey keeps the configuration
// reference from drifting: every leaf key has exactly one row, every row
// names a real key, and the defaults of all rows, decoded together, are
// exactly Default().
func TestConfigurationReferenceDocumentsEveryKey(t *testing.T) {
	data, err := os.ReadFile(referencePath) // #nosec G304 -- fixed repository path.
	if err != nil {
		t.Fatal(err)
	}
	leaves := leafKeyPaths()
	rows := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		m := referenceRow.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key, def, applied := m[1], m[2], m[3]
		if _, dup := rows[key]; dup {
			t.Errorf("key %s is documented twice", key)
		}
		rows[key] = def
		if !leaves[key] {
			t.Errorf("reference documents unknown key %s", key)
		}
		if applied != "reload" && applied != "restart" {
			t.Errorf("key %s: applied on %q, want reload or restart", key, applied)
		}
	}
	for key := range leaves {
		if _, ok := rows[key]; !ok {
			t.Errorf("reference does not document key %s", key)
		}
	}

	doc, err := referenceDefaults(rows)
	if err != nil {
		t.Fatalf("build a configuration from the documented defaults: %v", err)
	}
	var cfg Config
	if _, ps, err := decode(doc, &cfg); err != nil || len(ps) > 0 {
		t.Fatalf("decode documented defaults: %v %v\n%s", err, ps, doc)
	}
	if want := Default(); !reflect.DeepEqual(cfg, want) {
		t.Errorf("documented defaults = %+v, want %+v", cfg, want)
	}
}

// leafKeyPaths returns the key paths that hold a value rather than a section.
func leafKeyPaths() map[string]bool {
	paths := keyPaths(reflect.TypeOf(Config{}), "")
	leaves := make(map[string]bool, len(paths))
	for _, p := range paths {
		leaves[p] = true
	}
	for _, p := range paths {
		if i := strings.LastIndex(p, "."); i >= 0 {
			delete(leaves, p[:i])
		}
	}
	return leaves
}

// referenceDefaults builds one YAML document that sets every documented key
// to its documented default, each default read as a YAML flow value.
func referenceDefaults(rows map[string]string) ([]byte, error) {
	root := map[string]any{}
	for key, def := range rows {
		var value any
		if err := yaml.Unmarshal([]byte(def), &value); err != nil {
			return nil, err
		}
		section := root
		parts := strings.Split(key, ".")
		for _, part := range parts[:len(parts)-1] {
			next, ok := section[part].(map[string]any)
			if !ok {
				next = map[string]any{}
				section[part] = next
			}
			section = next
		}
		section[parts[len(parts)-1]] = value
	}
	return yaml.Marshal(root)
}
