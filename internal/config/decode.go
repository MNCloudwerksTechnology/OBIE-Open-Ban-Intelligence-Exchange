package config

import (
	"encoding"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// lineMap records the file line of every decoded key path.
type lineMap map[string]int

// decode strictly decodes YAML data over cfg. Unlike yaml.Unmarshal it
// rejects unknown and duplicate keys and values of the wrong YAML type, and
// returns every such problem with its key path; values with a problem keep
// what cfg held. err is only set when the file cannot be parsed at all.
func decode(data []byte, cfg *Config) (lines lineMap, ps problems, err error) {
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return lineMap{}, nil, nil // empty file: all defaults
		}
		return nil, nil, fmt.Errorf("parse config: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, nil, fmt.Errorf("parse config: %w", err)
		}
		return nil, nil, &Error{Problems: []Problem{{Path: "(document)", Line: extra.Line, Message: "the file must contain exactly one YAML document"}}}
	}

	d := &decoder{lines: lineMap{}}
	root := &doc
	if root.Kind == yaml.DocumentNode && len(root.Content) == 1 {
		root = root.Content[0]
	}
	d.value(root, "", reflect.ValueOf(cfg).Elem())
	return d.lines, d.problems, nil
}

type decoder struct {
	problems problems
	lines    lineMap
}

// bigInteger matches decimal integers YAML resolves as floats because they
// overflow int64.
var bigInteger = regexp.MustCompile(`^[-+]?[0-9_]+$`)

var textUnmarshalerType = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()

func (d *decoder) value(n *yaml.Node, path string, v reflect.Value) {
	for n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	d.lines[path] = n.Line
	name := displayPath(path)

	if reflect.PointerTo(v.Type()).Implements(textUnmarshalerType) {
		if !d.expectScalar(n, name, "!!str") {
			return
		}
		if err := v.Addr().Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(n.Value)); err != nil {
			d.problems.addf(name, n.Line, "%v", err)
		}
		return
	}

	switch v.Kind() {
	case reflect.Struct:
		d.mapping(n, path, v)
	case reflect.Slice:
		d.sequence(n, path, v)
	case reflect.String:
		if d.expectScalar(n, name, "!!str") {
			v.SetString(n.Value)
		}
	case reflect.Int:
		if n.Kind == yaml.ScalarNode && n.ShortTag() == "!!float" && bigInteger.MatchString(n.Value) {
			d.problems.addf(name, n.Line, "invalid value %q: out of range", n.Value)
			return
		}
		if d.expectScalar(n, name, "!!int") {
			d.scalar(n, name, v)
		}
	case reflect.Bool:
		if d.expectScalar(n, name, "!!bool") {
			d.scalar(n, name, v)
		}
	case reflect.Float64:
		if d.expectScalar(n, name, "!!int", "!!float") {
			d.scalar(n, name, v)
		}
	default:
		panic(fmt.Sprintf("config: unsupported field kind %s at %s", v.Kind(), name))
	}
}

// scalar decodes a scalar whose YAML type was already checked.
func (d *decoder) scalar(n *yaml.Node, name string, v reflect.Value) {
	if err := n.Decode(v.Addr().Interface()); err != nil {
		d.problems.addf(name, n.Line, "invalid value %q: out of range", n.Value)
	}
}

func (d *decoder) expectScalar(n *yaml.Node, name string, tags ...string) bool {
	if n.Kind == yaml.ScalarNode {
		for _, tag := range tags {
			if n.ShortTag() == tag {
				return true
			}
		}
	}
	want := make([]string, len(tags))
	for i, tag := range tags {
		want[i] = tagNames[tag]
	}
	d.problems.addf(name, n.Line, "must be %s, got %s", strings.Join(want, " or "), yamlTypeName(n))
	return false
}

// mapping decodes a YAML mapping into a struct. A null value (a section
// with every key commented out) keeps the defaults.
func (d *decoder) mapping(n *yaml.Node, path string, v reflect.Value) {
	name := displayPath(path)
	if isNull(n) {
		return
	}
	if n.Kind != yaml.MappingNode {
		d.problems.addf(name, n.Line, "must be a mapping, got %s", yamlTypeName(n))
		return
	}
	fields := structFields(v.Type())
	seen := make(map[string]bool, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		keyNode, valNode := n.Content[i], n.Content[i+1]
		key := keyNode.Value
		keyPath := joinPath(path, key)
		if keyNode.Kind != yaml.ScalarNode || keyNode.ShortTag() != "!!str" {
			d.problems.addf(displayPath(keyPath), keyNode.Line, "keys must be strings, got %s", yamlTypeName(keyNode))
			continue
		}
		if seen[key] {
			d.problems.addf(keyPath, keyNode.Line, "duplicate key")
			continue
		}
		seen[key] = true
		f, ok := fields[key]
		if !ok {
			d.problems.addf(keyPath, keyNode.Line, "unknown key (valid keys: %s)", strings.Join(sortedKeys(fields), ", "))
			continue
		}
		d.value(valNode, keyPath, v.Field(f.index))
	}
	for _, key := range sortedKeys(fields) {
		if fields[key].required && !seen[key] {
			d.problems.addf(joinPath(path, key), n.Line, "required key is missing")
		}
	}
}

// sequence decodes a YAML sequence into a slice, replacing the default. A
// null value (every entry commented out) yields an empty list.
func (d *decoder) sequence(n *yaml.Node, path string, v reflect.Value) {
	if isNull(n) {
		v.Set(reflect.MakeSlice(v.Type(), 0, 0))
		return
	}
	if n.Kind != yaml.SequenceNode {
		d.problems.addf(displayPath(path), n.Line, "must be a list, got %s", yamlTypeName(n))
		return
	}
	s := reflect.MakeSlice(v.Type(), len(n.Content), len(n.Content))
	for i, item := range n.Content {
		d.value(item, fmt.Sprintf("%s[%d]", path, i), s.Index(i))
	}
	v.Set(s)
}

type field struct {
	index    int
	required bool
}

func structFields(t reflect.Type) map[string]field {
	fields := make(map[string]field, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		name, opts, _ := strings.Cut(t.Field(i).Tag.Get("yaml"), ",")
		if name == "" {
			panic(fmt.Sprintf("config: field %s.%s has no yaml tag", t.Name(), t.Field(i).Name))
		}
		fields[name] = field{index: i, required: opts == "required"}
	}
	return fields
}

func sortedKeys(fields map[string]field) []string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func isNull(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null"
}

var tagNames = map[string]string{
	"!!str":   "a string",
	"!!int":   "an integer",
	"!!float": "a number",
	"!!bool":  "a boolean",
	"!!null":  "null",
}

func yamlTypeName(n *yaml.Node) string {
	switch n.Kind {
	case yaml.MappingNode:
		return "a mapping"
	case yaml.SequenceNode:
		return "a list"
	case yaml.ScalarNode:
		if name, ok := tagNames[n.ShortTag()]; ok {
			return name
		}
	}
	return n.ShortTag()
}

func joinPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// displayPath names the document root, whose path is empty.
func displayPath(path string) string {
	if path == "" {
		return "(root)"
	}
	return path
}
