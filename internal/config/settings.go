package config

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// Applied says when a changed setting takes effect.
type Applied string

// When settings take effect.
const (
	// OnReload: a reload (SIGHUP) applies the setting at once.
	OnReload Applied = "reload"
	// OnRestart: only a restart of obied applies the setting.
	OnRestart Applied = "restart"
)

// Setting describes one key of the configuration for the operator; the web
// console lists them with their values (ADR 0024). The configuration
// reference (documentation/operations/configuration.md) explains them at
// length; a test keeps the two in step.
type Setting struct {
	// Key is the key path, e.g. "mesh.rate_limit.peer.burst".
	Key string
	// Summary explains the key in one line.
	Summary string
	Applied Applied
	// Secret is set for a key whose value must never be shown.
	Secret bool
}

// Section returns the section of the file the key is in, e.g. "mesh".
func (s Setting) Section() string {
	section, _, _ := strings.Cut(s.Key, ".")
	return section
}

// settings lists every key in the order of the configuration reference.
var settings = []Setting{
	{"node.state_dir", "Directory of the node's persistent state: its identity key, the event store and the format marker.", OnRestart, false},
	{"node.mode", "observe decides and shows blocks without applying them; enforce also applies them to the firewall.", OnReload, false},
	{"node.shutdown_timeout", "Longest a graceful shutdown may take before obied stops waiting for its parts.", OnRestart, false},

	{"admin.socket", "Unix socket of the local admin API that obiectl talks to.", OnRestart, false},
	{"admin.socket_group", "Group whose members may use obiectl and this console, besides root and obied's own user.", OnRestart, false},

	{"mesh.listen", "Addresses the mesh listens on for other nodes, as libp2p multiaddrs.", OnRestart, false},
	{"mesh.bootstrap", "Peers the node connects to and keeps connected; their addresses are never blocked.", OnRestart, false},
	{"mesh.rate_limit.publisher.events_per_second", "Events accepted per second, on average, signed by one publisher.", OnRestart, false},
	{"mesh.rate_limit.publisher.burst", "Events one publisher may send at once.", OnRestart, false},
	{"mesh.rate_limit.peer.events_per_second", "Events accepted per second, on average, forwarded by one connected peer.", OnRestart, false},
	{"mesh.rate_limit.peer.burst", "Events one connected peer may forward at once.", OnRestart, false},

	{"store.max_indicators", "Most verdicts the store holds; beyond it the verdict that expires first makes room.", OnRestart, false},

	{"trust.publishers", "Publishers whose verdicts count, each with the trust weight you give it, from 0 to 1.", OnReload, false},
	{"trust.default_weight", "Weight of the publishers not listed in trust.publishers; keep it 0.", OnReload, false},
	{"trust.local_weight", "Weight of this node's own verdicts.", OnReload, false},

	{"decision.threshold", "Score at which an address is blocked: the sum of weight times confidence of its ban verdicts.", OnReload, false},
	{"decision.quorum", "Distinct publishers with a weight above 0 that must report an address before it is blocked.", OnReload, false},
	{"decision.local_autoblock", "Lets this node's own ban verdicts block at once, without threshold and quorum.", OnReload, false},
	{"decision.max_ttl", "Longest a block lasts from its decision; its cap on this node's own reports needs a restart.", OnReload, false},
	{"decision.default_ttl", "Lifetime of the verdicts this node reports without one.", OnRestart, false},

	{"allowlist.cidrs", "Networks never blocked, besides the built-in ranges and the node's own and its peers' addresses.", OnReload, false},
	{"allowlist.files", "Files of addresses and networks never blocked, one per line, read again on every reload.", OnReload, false},

	{"enforce.backend", "What applies the blocks in enforce mode: dryrun only logs them, nftables drops the traffic.", OnRestart, false},
	{"enforce.max_entries", "Most addresses and networks blocked at once; beyond it the lowest scores are left out.", OnRestart, false},
	{"enforce.reconcile_interval", "How often the firewall is compared with the decisions and corrected.", OnRestart, false},
	{"enforce.nftables.forward", "Also drops blocked sources in a forward chain, for routers and container hosts.", OnRestart, false},
	{"enforce.nftables.teardown_on_stop", "Removes the firewall table when the systemd unit stops, instead of letting blocks expire.", OnRestart, false},

	{"metrics.listen", "Address of the Prometheus metrics and the health endpoints.", OnRestart, false},

	{"console.enabled", "Serves this web console.", OnReload, false},
	{"console.listen", "Loopback address this web console listens on.", OnReload, false},

	{"audit.path", "File of the JSON audit log of every decision change; empty switches it off.", OnRestart, false},

	{"log.level", "Least level of the log lines obied writes: debug, info, warn or error.", OnRestart, false},
}

// Settings lists every key of the configuration in the order of the
// configuration reference.
func Settings() []Setting {
	return slices.Clone(settings)
}

// Lookup returns the setting key; ok is false if key names no setting.
func Lookup(key string) (s Setting, ok bool) {
	i := slices.IndexFunc(settings, func(s Setting) bool { return s.Key == key })
	if i < 0 {
		return Setting{}, false
	}
	return settings[i], true
}

// Reload returns the file a successful reload from f to next leaves
// running: the values of next, and whether it sets them, for the settings
// a reload applies; those of f for the others. Neither f nor next changes.
func (f *File) Reload(next *File) *File {
	cfg := *f.Config
	out := &File{Path: next.Path, Config: &cfg, set: lineMap{}}
	for _, s := range settings {
		from := f
		if s.Applied == OnReload {
			from = next
			to, _ := cfg.field(s.Key)
			value, _ := next.Config.field(s.Key)
			to.Set(value)
		}
		if line, ok := from.set[s.Key]; ok {
			out.set[s.Key] = line
		}
	}
	return out
}

// Value is the value of a key as the configuration file spells it.
type Value struct {
	// List is set for a list, whose Items are its entries; Text is any
	// other value.
	List  bool
	Items []string
	Text  string
}

// Value returns the value of the setting key in c; ok is false if key names
// no setting.
func (c *Config) Value(key string) (v Value, ok bool) {
	field, ok := c.field(key)
	if !ok {
		return Value{}, false
	}
	if field.Kind() != reflect.Slice {
		return Value{Text: spell(field)}, true
	}
	v.List = true
	for i := range field.Len() {
		v.Items = append(v.Items, spell(field.Index(i)))
	}
	return v, true
}

// field returns the field of c that holds the setting key: a value or a
// list, never a section.
func (c *Config) field(key string) (reflect.Value, bool) {
	v := reflect.ValueOf(c).Elem()
	for _, part := range strings.Split(key, ".") {
		if v.Kind() != reflect.Struct || isText(v) {
			return reflect.Value{}, false
		}
		f, ok := structFields(v.Type())[part]
		if !ok {
			return reflect.Value{}, false
		}
		v = v.Field(f.index)
	}
	if v.Kind() == reflect.Struct && !isText(v) {
		return reflect.Value{}, false // a section
	}
	return v, true
}

// isText reports whether v decodes from a string, like a Duration.
func isText(v reflect.Value) bool {
	return reflect.PointerTo(v.Type()).Implements(textUnmarshalerType)
}

// spell writes the value v as the configuration file would: a string as
// is ("" when empty), a list item that is a mapping in YAML flow style.
func spell(v reflect.Value) string {
	if s, ok := v.Interface().(fmt.Stringer); ok {
		return s.String()
	}
	switch v.Kind() {
	case reflect.String:
		if v.String() == "" {
			return `""`
		}
		return v.String()
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.Int:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'g', -1, 64)
	case reflect.Struct:
		fields := make([]string, v.NumField())
		for i := range fields {
			name, _, _ := strings.Cut(v.Type().Field(i).Tag.Get("yaml"), ",")
			fields[i] = name + ": " + spell(v.Field(i))
		}
		return "{" + strings.Join(fields, ", ") + "}"
	default:
		panic(fmt.Sprintf("config: cannot spell a value of kind %s", v.Kind()))
	}
}

// Changed returns the keys whose values differ between a and b, in the
// order of Settings. An empty list and a missing one are the same.
func Changed(a, b *Config) []string {
	return changed(a, b, func(Setting) bool { return true })
}

// ChangedOnReload returns the keys that a reload applies whose values
// differ between a and b, in the order of Settings.
func ChangedOnReload(a, b *Config) []string {
	return changed(a, b, func(s Setting) bool { return s.Applied == OnReload })
}

// ChangedOnRestart returns the keys that only a restart applies whose
// values differ between a and b, in the order of Settings.
func ChangedOnRestart(a, b *Config) []string {
	return changed(a, b, func(s Setting) bool { return s.Applied == OnRestart })
}

func changed(a, b *Config, among func(Setting) bool) []string {
	var keys []string
	for _, s := range settings {
		if !among(s) {
			continue
		}
		va, _ := a.Value(s.Key)
		vb, _ := b.Value(s.Key)
		if va.Text != vb.Text || !slices.Equal(va.Items, vb.Items) {
			keys = append(keys, s.Key)
		}
	}
	return keys
}
