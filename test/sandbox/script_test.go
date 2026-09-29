package sandbox

import (
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// fakeDocker stands in for docker in the script's tests. FAKE_INFO makes
// `docker info` succeed (ok), fail as for a user outside the docker group
// (denied) or as for a stopped daemon (down); FAKE_COMPOSE=missing makes
// `docker compose` fail; FAKE_UP=port makes `docker compose up` fail with
// Docker's error for a taken port. Every call is appended to FAKE_LOG.
const fakeDocker = `#!/bin/sh
echo "$*" >>"$FAKE_LOG"
case $1 in
info)
	case $FAKE_INFO in
	denied) echo "permission denied while trying to connect to the docker API at unix:///var/run/docker.sock" >&2; exit 1 ;;
	down) echo "failed to connect to the docker API at unix:///var/run/docker.sock; check if the path is correct and if the daemon is running: dial unix /var/run/docker.sock: connect: no such file or directory" >&2; exit 1 ;;
	esac
	exit 0 ;;
compose) [ "$FAKE_COMPOSE" != missing ] || { echo "docker: unknown command: docker compose" >&2; exit 1; } ;;
*) exit 0 ;;
esac
case "$*" in
*" up "*)
	[ "$FAKE_UP" != port ] || {
		echo "Error response from daemon: failed to set up container networking: driver failed programming external connectivity on endpoint obie-sandbox-node2-1 (0f00): Bind for 127.0.0.1:9402 failed: port is already allocated"
		exit 1
	} ;;
*" exec -T "*" obiectl peers --json"*) printf '{"peer_id": "a"}\n{"peer_id": "b"}\n{"peer_id": "c"}\n' ;;
*" exec -T "*" obiectl console --json"*) printf '{\n  "token": "token-of-%s"\n}\n' "$8" ;;
*" port node1 9466"*) echo "127.0.0.1:${OBIE_SANDBOX_CONSOLE1:-9401}" ;;
*" port node2 9466"*) echo "127.0.0.1:${OBIE_SANDBOX_CONSOLE2:-9402}" ;;
*" port node3 9466"*) echo "127.0.0.1:${OBIE_SANDBOX_CONSOLE3:-9403}" ;;
esac
`

// scriptRun is one run of packaging/sandbox/sandbox.
type scriptRun struct {
	stdout, stderr string
	code           int
	// calls are the docker commands it ran.
	calls []string
}

// runScript runs the sandbox script with the stand-in docker first in
// PATH, or without any docker if env holds NO_DOCKER=1, and the env.
func runScript(t *testing.T, env []string, args ...string) scriptRun {
	t.Helper()
	bin := t.TempDir()
	logFile := filepath.Join(bin, "calls.log")
	path := bin + string(os.PathListSeparator) + os.Getenv("PATH")
	for _, e := range env {
		if e == "NO_DOCKER=1" {
			// Only what the script needs before it looks for docker.
			path = bin
			linkTool(t, bin, "dirname")
		}
	}
	if path != bin {
		if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(fakeDocker), 0o700); err != nil { // #nosec G306 -- test script.
			t.Fatal(err)
		}
	}
	script, err := filepath.Abs(filepath.Join(sandboxDir, "sandbox"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", append([]string{script}, args...)...) // #nosec G204 -- test script.
	cmd.Env = append(os.Environ(), "PATH="+path, "FAKE_LOG="+logFile, "OBIE_SANDBOX_TIMEOUT=5")
	cmd.Env = append(cmd.Env, env...)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	run := scriptRun{}
	var exit *exec.ExitError
	if err := cmd.Run(); errors.As(err, &exit) {
		run.code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	run.stdout, run.stderr = stdout.String(), stderr.String()
	if data, err := os.ReadFile(logFile); err == nil { // #nosec G304 -- test file.
		run.calls = strings.Split(strings.TrimSpace(string(data)), "\n")
	}
	return run
}

// linkTool links the tool name from PATH into dir.
func linkTool(t *testing.T, dir, name string) {
	t.Helper()
	target, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s is not installed", name)
	}
	if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
		t.Fatal(err)
	}
}

// called reports whether the run ran a docker command containing part.
func (r scriptRun) called(part string) bool {
	for _, c := range r.calls {
		if strings.Contains(c, part) {
			return true
		}
	}
	return false
}

// freePort returns the first of three consecutive free ports on 127.0.0.1.
func freePort(t *testing.T) int {
	t.Helper()
	for range 20 {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := l.Addr().(*net.TCPAddr).Port
		_ = l.Close()
		if port+2 <= 65535 && !listening(port+1) && !listening(port+2) {
			return port
		}
	}
	t.Fatal("found no three consecutive free ports")
	return 0
}

func listening(port int) bool {
	c, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err == nil {
		_ = c.Close()
	}
	return err == nil
}

// TestSandboxUpStartsAndShowsTheConsoles checks ./sandbox up with a
// working Docker: it builds and starts the project, waits for the mesh and
// lists the three consoles with their tokens.
func TestSandboxUpStartsAndShowsTheConsoles(t *testing.T) {
	port := freePort(t)
	run := runScript(t, []string{"OBIE_SANDBOX_PORT=" + strconv.Itoa(port)}, "up")
	if run.code != 0 {
		t.Fatalf("./sandbox up: exit status %d\n%s%s", run.code, run.stdout, run.stderr)
	}
	if !run.called("compose -f ") || !run.called(" -p obie-sandbox up --detach --build --wait ") {
		t.Errorf("./sandbox up did not start the project obie-sandbox: %q", run.calls)
	}
	for i, node := range trustedNodes {
		want := node + "  http://127.0.0.1:" + strconv.Itoa(port+i) + "/  token token-of-" + node + "\n"
		if !strings.Contains(run.stdout, want) {
			t.Errorf("./sandbox up does not show %q:\n%s", want, run.stdout)
		}
	}
	for _, node := range append(trustedNodes, stranger) {
		if !run.called("exec -T " + node + " obiectl peers --json") {
			t.Errorf("./sandbox up did not wait for %s to connect", node)
		}
	}
}

// TestSandboxDownRemovesEverything checks that ./sandbox down removes the
// containers, network, volumes and images of its project.
func TestSandboxDownRemovesEverything(t *testing.T) {
	run := runScript(t, []string{"OBIE_SANDBOX_PROJECT=obie-sandbox-test"}, "down")
	if run.code != 0 {
		t.Fatalf("./sandbox down: exit status %d\n%s", run.code, run.stderr)
	}
	if !run.called(" -p obie-sandbox-test down --volumes --rmi all --remove-orphans") {
		t.Errorf("./sandbox down ran %q, want down --volumes --rmi all --remove-orphans of its project", run.calls)
	}
}

// TestSandboxExecRunsInTheNode checks that ./sandbox exec runs the command
// in the node, without a terminal, so that its output can be piped.
func TestSandboxExecRunsInTheNode(t *testing.T) {
	run := runScript(t, nil, "exec", "node3", "obiectl", "explain", "1.2.3.4")
	if run.code != 0 || !run.called(" -p obie-sandbox exec -T node3 obiectl explain 1.2.3.4") {
		t.Errorf("./sandbox exec: exit status %d, calls %q", run.code, run.calls)
	}
}

// TestSandboxMessages checks what the script says when it cannot start:
// every problem names what went wrong and the next step, on standard
// error, with exit status 1, and a mistake on the command line exits 2.
func TestSandboxMessages(t *testing.T) {
	tests := []struct {
		name string
		env  []string
		args []string
		code int
		want []string
	}{
		{"no docker", []string{"NO_DOCKER=1"}, []string{"up"}, 1,
			[]string{"sandbox: Docker is not installed", "Next: install Docker Engine"}},
		{"not in the docker group", []string{"FAKE_INFO=denied"}, []string{"up"}, 1,
			[]string{"sandbox: your user may not use Docker", "Why: permission denied", "sudo usermod -aG docker"}},
		{"daemon not running", []string{"FAKE_INFO=down"}, []string{"up"}, 1,
			[]string{"sandbox: Docker is installed but does not answer", "if the daemon is running", "Next: start Docker"}},
		{"no compose plugin", []string{"FAKE_COMPOSE=missing"}, []string{"up"}, 1,
			[]string{"sandbox: the Docker Compose plugin is missing", "Next: install the plugin"}},
		{"port taken, seen by Docker", []string{"FAKE_UP=port", "OBIE_SANDBOX_PORT=" + strconv.Itoa(freePort(t))}, []string{"up"}, 1,
			[]string{"sandbox: port 9402 on 127.0.0.1 is taken by another program", "OBIE_SANDBOX_PORT=9501 ./sandbox up"}},
		{"bad port setting", []string{"OBIE_SANDBOX_PORT=80"}, []string{"up"}, 1,
			[]string{"sandbox: OBIE_SANDBOX_PORT=80 is not a port the consoles can use"}},
		{"unknown command", nil, []string{"start"}, 2,
			[]string{`sandbox: unknown command "start"`, "Next: see how to use it: ./sandbox help"}},
		{"exec without a command", nil, []string{"exec", "node3"}, 2,
			[]string{"sandbox: exec needs a node and a command"}},
		{"console of the stranger", nil, []string{"console", "stranger"}, 2,
			[]string{"sandbox: stranger has no web console"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := runScript(t, tt.env, tt.args...)
			if run.code != tt.code {
				t.Errorf("exit status %d, want %d\n%s", run.code, tt.code, run.stderr)
			}
			for _, w := range tt.want {
				if !strings.Contains(run.stderr, w) {
					t.Errorf("standard error lacks %q:\n%s", w, run.stderr)
				}
			}
			if tt.code == 1 && !strings.Contains(run.stderr, "\n  Next: ") {
				t.Errorf("the problem has no Next: line:\n%s", run.stderr)
			}
			if run.stdout != "" {
				t.Errorf("standard output is not empty: %q", run.stdout)
			}
		})
	}
}

// TestSandboxRemovesAHalfStartedSandbox checks that a start that fails on
// a taken port leaves nothing behind.
func TestSandboxRemovesAHalfStartedSandbox(t *testing.T) {
	run := runScript(t, []string{"FAKE_UP=port", "OBIE_SANDBOX_PORT=" + strconv.Itoa(freePort(t))}, "up")
	if !run.called(" down --volumes --rmi all --remove-orphans") {
		t.Errorf("a failed start did not remove the sandbox: %q", run.calls)
	}
}

// TestSandboxSeesATakenPortFirst checks that ./sandbox up finds a console
// port another program listens on before it builds anything.
func TestSandboxSeesATakenPortFirst(t *testing.T) {
	if _, err := exec.LookPath("ss"); err != nil {
		if _, err := exec.LookPath("lsof"); err != nil {
			t.Skip("neither ss nor lsof is installed; Docker's error is the check")
		}
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	port := l.Addr().(*net.TCPAddr).Port
	run := runScript(t, []string{"OBIE_SANDBOX_PORT=" + strconv.Itoa(port-1)}, "up")
	want := "sandbox: port " + strconv.Itoa(port) + " on 127.0.0.1 is taken by another program"
	if run.code != 1 || !strings.Contains(run.stderr, want) {
		t.Errorf("exit status %d, want 1 and %q:\n%s", run.code, want, run.stderr)
	}
	if run.called(" up ") {
		t.Error("./sandbox up started Docker although a port was taken")
	}
}

var (
	// sudoCall is sudo run as a command.
	sudoCall = regexp.MustCompile(`(^|[;&|(]|\$\()\s*sudo\b`)
	// quoted is a double-quoted string, such as a message that names sudo
	// as the fix for Docker's own setup.
	quoted = regexp.MustCompile(`"(\\.|[^"\\])*"`)
	// comment is a shell comment.
	comment = regexp.MustCompile(`(^|\s)#.*$`)
)

// TestSandboxNeedsNoRoot checks that the scripts never run sudo: the
// sandbox needs a user who may use Docker, and nothing more.
func TestSandboxNeedsNoRoot(t *testing.T) {
	for _, name := range []string{"sandbox", "sandbox-init.sh"} {
		for i, line := range strings.Split(readRepoFile(t, "packaging/sandbox/"+name), "\n") {
			code := comment.ReplaceAllString(quoted.ReplaceAllString(line, `""`), "")
			if sudoCall.MatchString(code) {
				t.Errorf("%s:%d runs sudo: %s", name, i+1, line)
			}
		}
	}
}
