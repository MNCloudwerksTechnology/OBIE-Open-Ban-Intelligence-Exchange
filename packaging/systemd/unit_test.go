// Package systemd_test checks that the systemd unit obied.service keeps the
// sandbox the release promises (ADR 0017). `make check-unit` additionally
// runs systemd-analyze on it.
package systemd_test

import (
	"bufio"
	"os"
	"slices"
	"strings"
	"testing"
)

// readUnit returns the settings of the [Service] section of obied.service,
// each key with its values in order.
func readUnit(t *testing.T) map[string][]string {
	t.Helper()
	f, err := os.Open("obied.service")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	settings := map[string][]string{}
	section := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "["):
			section = line
		case section == "[Service]":
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				t.Fatalf("malformed line %q", line)
			}
			settings[key] = append(settings[key], value)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return settings
}

func TestUnitSandbox(t *testing.T) {
	settings := readUnit(t)
	for key, want := range map[string]string{
		"User":                    "obie",
		"Group":                   "obie",
		"StateDirectory":          "obie",
		"StateDirectoryMode":      "0700",
		"RuntimeDirectory":        "obie",
		"ConfigurationDirectory":  "obie",
		"AmbientCapabilities":     "CAP_NET_ADMIN",
		"CapabilityBoundingSet":   "CAP_NET_ADMIN",
		"NoNewPrivileges":         "yes",
		"ProtectSystem":           "strict",
		"ProtectHome":             "yes",
		"PrivateTmp":              "yes",
		"ProtectKernelTunables":   "yes",
		"ProtectKernelModules":    "yes",
		"ProtectKernelLogs":       "yes",
		"RestrictAddressFamilies": "AF_UNIX AF_INET AF_INET6 AF_NETLINK",
		"Restart":                 "on-failure",
		"ExecReload":              "/bin/kill -HUP $MAINPID",
		"ExecStart":               "/usr/local/bin/obied --config /etc/obie/obie.yaml",
	} {
		if got := settings[key]; !slices.Equal(got, []string{want}) {
			t.Errorf("%s = %q, want exactly %q", key, got, want)
		}
	}
	if got := settings["SystemCallFilter"]; len(got) == 0 || got[0] != "@system-service" {
		t.Errorf("SystemCallFilter = %q, want @system-service first", got)
	}
	for _, key := range []string{"PrivateUsers", "PrivateNetwork", "ReadWritePaths"} {
		if got, ok := settings[key]; ok {
			t.Errorf("%s = %q is set; PrivateUsers and PrivateNetwork break CAP_NET_ADMIN on the host, "+
				"and ProtectSystem=strict must not be widened", key, got)
		}
	}
}
