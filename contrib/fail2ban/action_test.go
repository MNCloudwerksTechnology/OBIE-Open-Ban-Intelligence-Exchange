// Package fail2ban_test tests the Fail2Ban action action.d/obie.conf: it
// substitutes the action's tags the way Fail2Ban does and runs the resulting
// commands with a fake obiectl on PATH.
package fail2ban_test

import (
	"bufio"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

const actionFile = "action.d/obie.conf"

// ban is the ticket Fail2Ban hands to an action on a ban.
type ban struct {
	ip, failures, bantime, matches string
}

// sshdBan carries matched lines full of shell metacharacters.
var sshdBan = ban{
	ip: "203.0.113.7", failures: "5", bantime: "600",
	matches: "Sep 28 12:00:01 host sshd[1]: Failed password for \"root\" from 203.0.113.7 port 22 $(touch pwned)\n" +
		"Sep 28 12:00:02 host sshd[1]: Invalid user `touch pwned`; it's 100% me & <you> from 203.0.113.7\\n",
}

func TestActionBan(t *testing.T) {
	for name, tc := range map[string]struct {
		jail     string
		params   map[string]string
		ticket   ban
		wantArgs []string
	}{
		"defaults": {
			jail: "sshd", ticket: sshdBan,
			wantArgs: []string{"--socket", "/run/obie/obie.sock", "--timeout", "5s", "report", "--ip", "203.0.113.7",
				"--protocol", "ssh", "--reason", "bruteforce", "--events", "5", "--mitre", "T1110", "--ttl", "600s",
				"--evidence-from-stdin"},
		},
		"jail parameters": {
			jail: "postfix-sasl",
			params: map[string]string{"protocol": "submission", "reason": "password_bruteforce", "confidence": "0.9",
				"mitre": "T1110.001,T1110.003", "socket": "/tmp/obie test.sock", "obiectl_timeout": "2s"},
			ticket: sshdBan,
			wantArgs: []string{"--socket", "/tmp/obie test.sock", "--timeout", "2s", "report", "--ip", "203.0.113.7",
				"--protocol", "submission", "--reason", "password_bruteforce", "--events", "5", "--confidence", "0.9",
				"--mitre", "T1110.001,T1110.003", "--ttl", "600s", "--evidence-from-stdin"},
		},
		"increased ban time": {
			jail: "sshd", ticket: ban{ip: "203.0.113.7", failures: "5", bantime: "1234.5678"},
			wantArgs: []string{"--socket", "/run/obie/obie.sock", "--timeout", "5s", "report", "--ip", "203.0.113.7",
				"--protocol", "ssh", "--reason", "bruteforce", "--events", "5", "--mitre", "T1110", "--ttl", "1234s"},
		},
		"manual ban without failures": {
			jail: "sshd", ticket: ban{ip: "203.0.113.7", failures: "0", bantime: "600"},
			wantArgs: []string{"--socket", "/run/obie/obie.sock", "--timeout", "5s", "report", "--ip", "203.0.113.7",
				"--protocol", "ssh", "--reason", "bruteforce", "--events", "1", "--mitre", "T1110", "--ttl", "600s"},
		},
		"ban shorter than the minimum TTL": {
			jail: "sshd", ticket: ban{ip: "203.0.113.7", failures: "5", bantime: "30"},
			wantArgs: []string{"--socket", "/run/obie/obie.sock", "--timeout", "5s", "report", "--ip", "203.0.113.7",
				"--protocol", "ssh", "--reason", "bruteforce", "--events", "5", "--mitre", "T1110", "--ttl", "60s"},
		},
		"permanent ban without matches or mitre": {
			jail: "recidive", params: map[string]string{"mitre": ""},
			ticket: ban{ip: "2001:db8::7", failures: "3", bantime: "-1"},
			wantArgs: []string{"--socket", "/run/obie/obie.sock", "--timeout", "5s", "report", "--ip", "2001:db8::7",
				"--protocol", "recidive", "--reason", "bruteforce", "--events", "3"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			env := newFakeEnv(t, 0, "Reported ipv4:203.0.113.7: verdict issued and published.")
			out, err := env.run(t, "actionban", tc.jail, tc.params, tc.ticket)
			if err != nil {
				t.Fatalf("actionban: %v\n%s", err, out)
			}
			if got := env.calls(t); len(got) != 1 || !slices.Equal(got[0], tc.wantArgs) {
				t.Errorf("obiectl calls = %q, want [%q]", got, tc.wantArgs)
			}
			wantStdin := ""
			if tc.ticket.matches != "" {
				wantStdin = tc.ticket.matches + "\n"
			}
			if stdin := env.stdin(t); stdin != wantStdin {
				t.Errorf("obiectl stdin = %q, want %q", stdin, wantStdin)
			}
			if logged := env.logged(t); logged != "" {
				t.Errorf("successful report logged %q", logged)
			}
			if _, err := os.Stat(filepath.Join(env.dir, "pwned")); err == nil {
				t.Error("matched log lines were executed")
			}
		})
	}
}

func TestActionBanDerivesProtocolFromJailName(t *testing.T) {
	for jail, want := range map[string]string{
		"sshd": "ssh", "sshd-ddos": "ssh", "dropbear": "ssh",
		"nginx-http-auth": "http", "apache-auth": "http", "nginx-botsearch": "http",
		"postfix-sasl": "smtp", "exim": "smtp", "sendmail-auth": "smtp",
		"dovecot": "imap", "courier-auth": "imap", "proftpd": "ftp", "vsftpd": "ftp",
		"recidive": "recidive", "Custom-Jail": "custom-jail",
		"My.Jail": "my_jail", "a-very-long-jail-name-beyond-the-limit": "a-very-long-jail-name-beyond-the",
	} {
		t.Run(jail, func(t *testing.T) {
			env := newFakeEnv(t, 0, "")
			if out, err := env.run(t, "actionban", jail, nil, sshdBan); err != nil {
				t.Fatalf("actionban: %v\n%s", err, out)
			}
			calls := env.calls(t)
			if len(calls) != 1 {
				t.Fatalf("obiectl calls = %q, want 1", calls)
			}
			if i := slices.Index(calls[0], "--protocol"); i < 0 || calls[0][i+1] != want {
				t.Errorf("obiectl args = %q, want --protocol %s", calls[0], want)
			}
		})
	}
}

func TestActionUnban(t *testing.T) {
	ticket := ban{ip: "203.0.113.7", failures: "5", bantime: "600"}
	t.Run("default is a no-op", func(t *testing.T) {
		env := newFakeEnv(t, 0, "")
		if out, err := env.run(t, "actionunban", "sshd", nil, ticket); err != nil {
			t.Fatalf("actionunban: %v\n%s", err, out)
		}
		if calls := env.calls(t); len(calls) != 0 {
			t.Errorf("obiectl calls = %q, want none: the TTL governs", calls)
		}
	})
	t.Run("revoke_on_unban", func(t *testing.T) {
		env := newFakeEnv(t, 0, "Revoked verdict ...")
		params := map[string]string{"revoke_on_unban": "true"}
		if out, err := env.run(t, "actionunban", "sshd", params, ticket); err != nil {
			t.Fatalf("actionunban: %v\n%s", err, out)
		}
		want := []string{"--socket", "/run/obie/obie.sock", "--timeout", "5s", "revoke", "--reason", "unbanned", "203.0.113.7"}
		if calls := env.calls(t); len(calls) != 1 || !slices.Equal(calls[0], want) {
			t.Errorf("obiectl calls = %q, want [%q]", calls, want)
		}
		if logged := env.logged(t); logged != "" {
			t.Errorf("successful revocation logged %q", logged)
		}
	})
	t.Run("revoke_on_unban after expiry", func(t *testing.T) {
		env := newFakeEnv(t, 1, "obiectl: no active verdict on ipv4:203.0.113.7 (obied answered 404 Not Found)")
		params := map[string]string{"revoke_on_unban": "true"}
		if out, err := env.run(t, "actionunban", "sshd", params, ticket); err != nil {
			t.Fatalf("actionunban: %v\n%s", err, out)
		}
		if logged := env.logged(t); logged != "" {
			t.Errorf("revoking an expired verdict logged %q", logged)
		}
	})
}

// TestActionDaemonDown checks that a failing obiectl never fails Fail2Ban:
// the action logs the error to syslog and exits 0.
func TestActionDaemonDown(t *testing.T) {
	const daemonDown = "obiectl: obied is not running (no socket at /run/obie/obie.sock)\nobiectl: start obied, or point --socket at its admin.socket"
	for name, tc := range map[string]struct {
		action     string
		params     map[string]string
		wantLogged []string
	}{
		"ban": {action: "actionban", wantLogged: []string{
			"-t obie-fail2ban -p daemon.warning -- could not report 203.0.113.7 of jail sshd to OBIE (obiectl exit code 1): " +
				"obiectl: obied is not running (no socket at /run/obie/obie.sock) obiectl: start obied, or point --socket at its admin.socket"}},
		"unban with revoke_on_unban": {action: "actionunban", params: map[string]string{"revoke_on_unban": "true", "syslog_priority": "authpriv.err"},
			wantLogged: []string{"-t obie-fail2ban -p authpriv.err -- could not revoke the OBIE verdict on 203.0.113.7 of jail sshd (obiectl exit code 1): "}},
	} {
		t.Run(name, func(t *testing.T) {
			env := newFakeEnv(t, 1, daemonDown)
			out, err := env.run(t, tc.action, "sshd", tc.params, sshdBan)
			if err != nil {
				t.Fatalf("%s failed Fail2Ban: %v\n%s", tc.action, err, out)
			}
			logged := env.logged(t)
			for _, want := range tc.wantLogged {
				if !strings.Contains(logged, want) {
					t.Errorf("syslog = %q, want it to contain %q", logged, want)
				}
			}
			if strings.Contains(logged, "Failed password") {
				t.Errorf("syslog contains matched log lines: %q", logged)
			}
		})
	}

	t.Run("obiectl missing", func(t *testing.T) {
		env := newFakeEnv(t, 0, "")
		out, err := env.run(t, "actionban", "sshd", map[string]string{"obiectl": filepath.Join(env.dir, "missing")}, sshdBan)
		if err != nil {
			t.Fatalf("actionban failed Fail2Ban: %v\n%s", err, out)
		}
		if logged := env.logged(t); !strings.Contains(logged, "(obiectl exit code 127)") {
			t.Errorf("syslog = %q, want the missing obiectl logged", logged)
		}
	})

	t.Run("without logger", func(t *testing.T) {
		env := newFakeEnv(t, 1, daemonDown)
		if err := os.Remove(filepath.Join(env.bin, "logger")); err != nil {
			t.Fatal(err)
		}
		for _, tool := range []string{"tr", "cat"} {
			if err := os.Symlink(lookPath(t, tool), filepath.Join(env.bin, tool)); err != nil {
				t.Fatal(err)
			}
		}
		env.path = env.bin // no logger anywhere on PATH
		out, err := env.run(t, "actionban", "sshd", nil, sshdBan)
		if err != nil {
			t.Fatalf("actionban failed Fail2Ban: %v\n%s", err, out)
		}
		if !strings.Contains(out, "obie-fail2ban: could not report 203.0.113.7 of jail sshd to OBIE") {
			t.Errorf("output = %q, want the error on stderr", out)
		}
	})
}

// TestActionWithRealObiectl runs the ban action with the real obiectl
// against an obied that is down, and against one that accepts connections but
// never answers: the action must log and succeed within the timeout.
func TestActionWithRealObiectl(t *testing.T) {
	if testing.Short() {
		t.Skip("builds obiectl")
	}
	obiectl := filepath.Join(t.TempDir(), "obiectl")
	build := exec.Command("go", "build", "-o", obiectl, "github.com/MNCloudwerksTechnology/obie/cmd/obiectl") // #nosec G204 -- fixed arguments.
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building obiectl: %v\n%s", err, out)
	}

	socketDir, err := os.MkdirTemp("", "obie-f2b") // short path: unix socket paths are limited
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	hung := filepath.Join(socketDir, "hung.sock")
	ln, err := net.Listen("unix", hung)
	if err != nil {
		t.Fatal(err)
	}
	conns := make(chan net.Conn, 8)
	go func() {
		defer close(conns)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conns <- conn // never answers
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		for conn := range conns {
			_ = conn.Close()
		}
	})

	for name, tc := range map[string]struct {
		socket, wantLogged string
	}{
		"obied down": {filepath.Join(socketDir, "missing.sock"), "obied is not running"},
		"obied hung": {hung, "obied did not answer in time"},
	} {
		t.Run(name, func(t *testing.T) {
			env := newFakeEnv(t, 0, "")
			params := map[string]string{"obiectl": obiectl, "socket": tc.socket, "obiectl_timeout": "300ms"}
			start := time.Now()
			out, err := env.run(t, "actionban", "sshd", params, sshdBan)
			if err != nil {
				t.Fatalf("actionban failed Fail2Ban: %v\n%s", err, out)
			}
			if elapsed := time.Since(start); elapsed > 10*time.Second {
				t.Errorf("actionban took %v despite obiectl_timeout 300ms", elapsed)
			}
			logged := env.logged(t)
			if !strings.Contains(logged, "could not report 203.0.113.7 of jail sshd to OBIE (obiectl exit code 1)") ||
				!strings.Contains(logged, tc.wantLogged) {
				t.Errorf("syslog = %q, want the failure with %q", logged, tc.wantLogged)
			}
		})
	}
}

// TestFail2BanAcceptsAction runs `fail2ban-client -t` on the Fail2Ban
// configuration with the action added to the guide's example jails. It is
// skipped unless fail2ban-client and its configuration (/etc/fail2ban, or
// $OBIE_FAIL2BAN_CONFDIR) are installed.
func TestFail2BanAcceptsAction(t *testing.T) {
	client, err := exec.LookPath("fail2ban-client")
	if err != nil {
		t.Skip("fail2ban-client is not installed")
	}
	src := os.Getenv("OBIE_FAIL2BAN_CONFDIR")
	if src == "" {
		src = "/etc/fail2ban"
	}
	if _, err := os.Stat(filepath.Join(src, "jail.conf")); err != nil { // #nosec G703 -- the tester chooses the configuration directory.
		t.Skipf("no Fail2Ban configuration: %v", err)
	}
	dir := t.TempDir()
	conf := filepath.Join(dir, "fail2ban")
	if err := os.CopyFS(conf, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	// Only the jails below: drop the distribution's jail.d and *.local files.
	if err := os.RemoveAll(filepath.Join(conf, "jail.d")); err != nil {
		t.Fatal(err)
	}
	locals, _ := filepath.Glob(filepath.Join(conf, "*.local"))
	for _, local := range locals {
		if err := os.Remove(local); err != nil {
			t.Fatal(err)
		}
	}
	action, err := os.ReadFile(actionFile)
	if err != nil {
		t.Fatal(err)
	}
	logfile := filepath.Join(dir, "auth.log")
	for path, content := range map[string]string{
		filepath.Join(conf, "action.d", "obie.conf"): string(action),
		logfile: "",
		// The distribution's jails set their own logpath, so each jail needs one.
		filepath.Join(conf, "jail.local"): strings.ReplaceAll(`[DEFAULT]
backend = auto

[sshd]
enabled = true
logpath = LOGFILE
action  = %(action_)s
          obie

[nginx-http-auth]
enabled = true
logpath = LOGFILE
action  = %(action_)s
          obie[reason=password_bruteforce]

[postfix-sasl]
enabled = true
logpath = LOGFILE
action  = %(action_)s
          obie[protocol=smtp, confidence=0.9, revoke_on_unban=true]
`, "LOGFILE", logfile),
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	out, err := exec.Command(client, "-c", conf, "-t").CombinedOutput() // #nosec G204 -- runs fail2ban-client on the test's configuration.
	if err != nil {
		t.Fatalf("fail2ban-client -t: %v\n%s", err, out)
	}
	// The jail's own "protocol = tcp" must not leak into the action.
	out, err = exec.Command(client, "-c", conf, "-d").CombinedOutput() // #nosec G204 -- as above.
	if err != nil {
		t.Fatalf("fail2ban-client -d: %v\n%s", err, out)
	}
	// One "multi-set <jail> action obie [...]" line per jail, in jail order.
	var obie []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "'action', 'obie', [[") {
			obie = append(obie, line)
		}
	}
	if len(obie) != 3 {
		t.Fatalf("fail2ban-client -d configures the obie action %d times, want 3:\n%s", len(obie), out)
	}
	for i, wants := range [][]string{
		{"['protocol', '']", "--evidence-from-stdin"},
		{"['protocol', '']", "['reason', 'password_bruteforce']"},
		{"['protocol', 'smtp']", "['confidence', '0.9']", "['revoke_on_unban', 'true']"},
	} {
		for _, want := range wants {
			if !strings.Contains(obie[i], want) {
				t.Errorf("obie action %d lacks %s:\n%s", i, want, obie[i])
			}
		}
		if strings.Contains(obie[i], "['protocol', 'tcp']") {
			t.Errorf("the jail's protocol leaked into obie action %d:\n%s", i, obie[i])
		}
	}
}

// fakeEnv is a directory with fake obiectl and logger commands that record
// how they were called.
type fakeEnv struct {
	dir, bin, path string
}

// fakeObiectl records its arguments (one per line, calls separated by a
// line "--call--") and stdin, then prints $FAKE_OUTPUT to stderr and exits
// with $FAKE_EXIT.
const fakeObiectl = `#!/bin/sh
{ echo --call--; for a in "$@"; do printf '%s\n' "$a"; done; } >> "$FAKE_DIR/obiectl.args"
case " $* " in *" --evidence-from-stdin "*) cat >> "$FAKE_DIR/obiectl.stdin";; esac
if [ -n "$FAKE_OUTPUT" ]; then printf '%s\n' "$FAKE_OUTPUT" >&2; fi
exit "$FAKE_EXIT"
`

// fakeLogger records its arguments on one line.
const fakeLogger = `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_DIR/logger"
`

func newFakeEnv(t *testing.T, exitCode int, output string) *fakeEnv {
	t.Helper()
	dir := t.TempDir()
	env := &fakeEnv{dir: dir, bin: filepath.Join(dir, "bin")}
	if err := os.Mkdir(env.bin, 0o750); err != nil {
		t.Fatal(err)
	}
	for name, script := range map[string]string{"obiectl": fakeObiectl, "logger": fakeLogger} {
		if err := os.WriteFile(filepath.Join(env.bin, name), []byte(script), 0o700); err != nil { // #nosec G306 -- an executable test fake.
			t.Fatal(err)
		}
	}
	env.path = env.bin + ":" + os.Getenv("PATH")
	t.Setenv("FAKE_DIR", dir)
	t.Setenv("FAKE_EXIT", strconv.Itoa(exitCode))
	t.Setenv("FAKE_OUTPUT", output)
	return env
}

// run executes the action command option for a ban of jail, with the jail's
// action parameters, as Fail2Ban does, and returns its combined output.
func (e *fakeEnv) run(t *testing.T, option, jail string, params map[string]string, ticket ban) (string, error) {
	t.Helper()
	cmd := fail2banCommand(t, option, jail, params, ticket)
	cmd.Dir = e.dir
	cmd.Env = append(os.Environ(), "PATH="+e.path)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (e *fakeEnv) calls(t *testing.T) [][]string {
	t.Helper()
	var calls [][]string
	for _, line := range strings.Split(e.read(t, "obiectl.args"), "\n") {
		switch {
		case line == "--call--":
			calls = append(calls, []string{})
		case len(calls) > 0 && line != "":
			calls[len(calls)-1] = append(calls[len(calls)-1], line)
		}
	}
	return calls
}

func (e *fakeEnv) stdin(t *testing.T) string  { return e.read(t, "obiectl.stdin") }
func (e *fakeEnv) logged(t *testing.T) string { return e.read(t, "logger") }

func (e *fakeEnv) read(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(e.dir, name)) // #nosec G304 -- a file of the test's temporary directory.
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func lookPath(t *testing.T, command string) string {
	t.Helper()
	path, err := exec.LookPath(command)
	if err != nil {
		t.Skipf("%s not found: %v", command, err)
	}
	return path
}

// tagPattern matches a Fail2Ban tag such as <ip> (TAG_CRE in Fail2Ban).
var tagPattern = regexp.MustCompile(`<([^ <>]+)>`)

// escapePattern matches the characters for which Fail2Ban passes a dynamic
// tag's value as a shell variable instead of inline (ESCAPE_CRE).
var escapePattern = regexp.MustCompile("[\\\\#&;`|*?~<>^()\\[\\]{}$'\"\n\r]")

// fail2banCommand builds the shell command Fail2Ban 0.10+ runs for option:
// static tags are replaced recursively by the action's options (the jail's
// parameters override [Init]), then the ticket's dynamic tags are replaced
// non-recursively; a value with shell metacharacters becomes a variable
// f2bV_<tag> set from a positional argument (Utils.buildShellCmd).
func fail2banCommand(t *testing.T, option, jail string, params map[string]string, ticket ban) *exec.Cmd {
	t.Helper()
	options := readAction(t)
	for k, v := range params {
		options[k] = v
	}
	options["name"] = jail
	options["actname"] = "obie"
	script, ok := options[option]
	if !ok {
		t.Fatalf("%s has no option %s", actionFile, option)
	}
	dynamic := map[string]string{"ip": ticket.ip, "failures": ticket.failures, "bantime": ticket.bantime, "matches": ticket.matches}

	for range 50 {
		next := tagPattern.ReplaceAllStringFunc(script, func(tag string) string {
			name := tag[1 : len(tag)-1]
			if _, isDynamic := dynamic[name]; isDynamic {
				return tag
			}
			if v, ok := options[name]; ok {
				return v
			}
			return tag
		})
		if next == script {
			break
		}
		script = next
	}

	var names, values []string
	script = tagPattern.ReplaceAllStringFunc(script, func(tag string) string {
		name := tag[1 : len(tag)-1]
		v, ok := dynamic[name]
		if !ok {
			t.Fatalf("%s: unknown tag %s", option, tag)
		}
		if !escapePattern.MatchString(v) {
			return v
		}
		names = append(names, "f2bV_"+name)
		values = append(values, v)
		return "$f2bV_" + name
	})
	if len(names) > 0 {
		var vars strings.Builder
		for i, name := range names {
			vars.WriteString(name + "=$" + strconv.Itoa(i) + " ")
		}
		script = vars.String() + "\n" + script
	}
	return exec.Command("/bin/sh", append([]string{"-c", script}, values...)...) // #nosec G204 -- runs the action under test.
}

// readAction reads the [Definition] and [Init] options of the action like
// Python's configparser does for Fail2Ban: indented lines continue a value,
// "%%" is a literal "%", and " ;" starts an inline comment, which the action
// must therefore not contain.
func readAction(t *testing.T) map[string]string {
	t.Helper()
	f, err := os.Open(actionFile)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	options := map[string]string{}
	var section, key string
	scanner := bufio.NewScanner(f)
	for n := 1; scanner.Scan(); n++ {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
			continue
		case strings.HasPrefix(line, "["):
			section, key = strings.Trim(trimmed, "[]"), ""
			continue
		case section != "Definition" && section != "Init":
			continue
		}
		if strings.Contains(line, " ;") || strings.Contains(line, "\t;") {
			t.Errorf("%s:%d: %q: configparser cuts the line at the inline comment \" ;\"", actionFile, n, line)
		}
		if strings.Contains(strings.ReplaceAll(line, "%%", ""), "%") {
			t.Errorf("%s:%d: %q: a single %% is configparser interpolation; write %%%%", actionFile, n, line)
		}
		trimmed = strings.ReplaceAll(trimmed, "%%", "%")
		if line[0] == ' ' || line[0] == '\t' {
			if key == "" {
				t.Fatalf("%s:%d: continuation line without an option", actionFile, n)
			}
			options[key] += "\n" + trimmed
			continue
		}
		k, v, ok := strings.Cut(trimmed, "=")
		if !ok {
			t.Fatalf("%s:%d: %q is not an option", actionFile, n, line)
		}
		key = strings.TrimSpace(k)
		options[key] = strings.TrimSpace(v)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return options
}
