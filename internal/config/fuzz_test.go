package config

import (
	"errors"
	"os"
	"testing"
)

// FuzzParse checks that parsing an arbitrary configuration file never
// panics, that every rejected file is reported with a message, and that
// every accepted configuration passes validation.
func FuzzParse(f *testing.F) {
	if example, err := os.ReadFile(examplePath); err == nil { // #nosec G304 -- fixed repository path.
		f.Add(example)
	}
	for _, s := range []string{
		"",
		"node:\n  mode: enforce\n",
		"mesh:\n  listen: [/ip4/127.0.0.1/tcp/4001]\n  bootstrap: [/ip4/192.0.2.1/tcp/4001/p2p/12D3KooWGzBX6MWMMz3kHmFfyT3vJxFoy4xQF8NbXN7xBAFhGyvd]\n",
		"trust:\n  publishers:\n    - {peer_id: 12D3KooWGzBX6MWMMz3kHmFfyT3vJxFoy4xQF8NbXN7xBAFhGyvd, name: a, weight: 0.5}\n",
		"decision:\n  max_ttl: 7d\n  default_ttl: 36h\n",
		"allowlist:\n  cidrs: [192.0.2.0/24, 2001:db8::/32]\n",
		"store:\n  max_indicators: 0x10\n",
		"node: [1, 2]\n",
		"a: &a [*a]\n",
		"node:\n  unknown: 1\n",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		cfg, err := Parse(data)
		if err != nil {
			var cfgErr *Error
			if errors.As(err, &cfgErr) && len(cfgErr.Problems) == 0 {
				t.Fatalf("Parse() error %q lists no problem", err)
			}
			if err.Error() == "" {
				t.Fatal("Parse() error has no message")
			}
			return
		}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("Parse() accepted a configuration that Validate rejects: %v", err)
		}
	})
}
