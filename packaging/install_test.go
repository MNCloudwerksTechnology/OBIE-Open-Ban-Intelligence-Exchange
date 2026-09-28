// Package packaging_test tests install.sh: it lays out a release tarball
// the way `make release` does and installs it into a staging directory
// (DESTDIR), twice, to prove the script is idempotent.
package packaging_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// tarballFiles maps the files of a release tarball to their sources in the
// repository; the binaries are stand-ins.
var tarballFiles = map[string]string{
	"install.sh":                  "install.sh",
	"etc/obie.yaml":               "../documentation/examples/obie.yaml",
	"systemd/obied.service":       "systemd/obied.service",
	"fail2ban/action.d/obie.conf": "../contrib/fail2ban/action.d/obie.conf",
}

// newTarball returns the directory of an extracted release tarball.
func newTarball(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for dst, src := range tarballFiles {
		data, err := os.ReadFile(src) // #nosec G304 -- repository file.
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, dst), string(data), 0o755)
	}
	for _, b := range []string{"obied", "obiectl"} {
		writeFile(t, filepath.Join(dir, "bin", b), "#!/bin/sh\necho "+b+"\n", 0o755)
	}
	return dir
}

func writeFile(t *testing.T, path, content string, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), perm); err != nil { // #nosec G306 G703 -- test file in a temporary directory.
		t.Fatal(err)
	}
	if err := os.Chmod(path, perm); err != nil { // WriteFile applies the umask.
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- test file.
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// install runs install.sh from tarball with env and returns its output.
func install(t *testing.T, tarball string, env ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("/bin/sh", filepath.Join(tarball, "install.sh")) // #nosec G204 -- test script.
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Error(err)
		return
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s mode = %04o, want %04o", path, got, want)
	}
}

func TestInstallIsIdempotent(t *testing.T) {
	tarball := newTarball(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "etc/fail2ban/action.d"), 0o750); err != nil {
		t.Fatal(err)
	}
	out, err := install(t, tarball, "DESTDIR="+root)
	if err != nil {
		t.Fatalf("install.sh: %v\n%s", err, out)
	}
	for path, mode := range map[string]os.FileMode{
		"usr/local/bin/obied":              0o755,
		"usr/local/bin/obiectl":            0o755,
		"etc/obie/obie.yaml":               0o640,
		"etc/obie/obie.yaml.example":       0o644,
		"etc/systemd/system/obied.service": 0o644,
		"etc/fail2ban/action.d/obie.conf":  0o644,
	} {
		assertMode(t, filepath.Join(root, path), mode)
	}
	example := readFile(t, "../documentation/examples/obie.yaml")
	if got := readFile(t, filepath.Join(root, "etc/obie/obie.yaml")); got != example {
		t.Error("etc/obie/obie.yaml is not the example configuration")
	}
	if got := readFile(t, filepath.Join(root, "etc/systemd/system/obied.service")); got != readFile(t, "systemd/obied.service") {
		t.Error("the installed unit differs from packaging/systemd/obied.service with the default PREFIX")
	}
	if !strings.Contains(out, "systemctl enable --now obied") {
		t.Errorf("output does not name the next steps:\n%s", out)
	}

	// An operator's configuration survives a second run (an upgrade).
	const edited = "node:\n  mode: enforce\n"
	writeFile(t, filepath.Join(root, "etc/obie/obie.yaml"), edited, 0o640)
	writeFile(t, filepath.Join(tarball, "bin/obied"), "#!/bin/sh\necho obied v2\n", 0o755)
	out, err = install(t, tarball, "DESTDIR="+root)
	if err != nil {
		t.Fatalf("second install.sh: %v\n%s", err, out)
	}
	if got := readFile(t, filepath.Join(root, "etc/obie/obie.yaml")); got != edited {
		t.Errorf("second run replaced the configuration with:\n%s", got)
	}
	if !strings.Contains(out, "kept") {
		t.Errorf("second run does not say it kept the configuration:\n%s", out)
	}
	if got := readFile(t, filepath.Join(root, "usr/local/bin/obied")); !strings.Contains(got, "v2") {
		t.Error("second run did not upgrade obied")
	}
	if got := readFile(t, filepath.Join(root, "etc/obie/obie.yaml.example")); got != example {
		t.Error("etc/obie/obie.yaml.example is not the current example")
	}
	entries, err := os.ReadDir(filepath.Join(root, "etc/systemd/system"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("etc/systemd/system holds %d entries, want only obied.service", len(entries))
	}
}

func TestInstallPrefix(t *testing.T) {
	tarball := newTarball(t)
	root := t.TempDir()
	out, err := install(t, tarball, "DESTDIR="+root, "PREFIX=/opt/obie")
	if err != nil {
		t.Fatalf("install.sh: %v\n%s", err, out)
	}
	assertMode(t, filepath.Join(root, "opt/obie/bin/obied"), 0o755)
	unit := readFile(t, filepath.Join(root, "etc/systemd/system/obied.service"))
	if strings.Contains(unit, "/usr/local/bin") || !strings.Contains(unit, "ExecStart=/opt/obie/bin/obied --config") {
		t.Errorf("unit does not use PREFIX /opt/obie:\n%s", unit)
	}
	// Without /etc/fail2ban the action is not installed, only pointed to.
	if _, err := os.Stat(filepath.Join(root, "etc/fail2ban")); !os.IsNotExist(err) {
		t.Errorf("etc/fail2ban created although Fail2Ban is not installed: %v", err)
	}
	if !strings.Contains(out, "Fail2Ban not found") {
		t.Errorf("output does not mention the missing Fail2Ban:\n%s", out)
	}
}

func TestInstallRefuses(t *testing.T) {
	t.Run("incomplete tarball", func(t *testing.T) {
		tarball := newTarball(t)
		if err := os.Remove(filepath.Join(tarball, "bin/obiectl")); err != nil {
			t.Fatal(err)
		}
		out, err := install(t, tarball, "DESTDIR="+t.TempDir())
		if err == nil || !strings.Contains(out, "bin/obiectl is missing") {
			t.Errorf("install.sh = %v, want a missing-file error:\n%s", err, out)
		}
	})
	t.Run("not root", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("running as root")
		}
		out, err := install(t, newTarball(t), "DESTDIR=")
		if err == nil || !strings.Contains(out, "must run as root") {
			t.Errorf("install.sh = %v, want a root error:\n%s", err, out)
		}
	})
	t.Run("argument", func(t *testing.T) {
		cmd := exec.Command("/bin/sh", filepath.Join(newTarball(t), "install.sh"), "--bogus") // #nosec G204 -- test script.
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(out), "unexpected argument --bogus") {
			t.Errorf("install.sh --bogus = %v, want a usage error:\n%s", err, out)
		}
	})
}

func TestInstallHelp(t *testing.T) {
	cmd := exec.Command("/bin/sh", filepath.Join(newTarball(t), "install.sh"), "--help") // #nosec G204 -- test script.
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install.sh --help: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "sudo ./install.sh") || strings.Contains(string(out), "set -eu") {
		t.Errorf("unexpected help:\n%s", out)
	}
}
