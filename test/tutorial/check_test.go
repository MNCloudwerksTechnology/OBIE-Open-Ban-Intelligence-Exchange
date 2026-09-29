//go:build tutorial

package tutorial

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

const (
	// checkName names the check's containers, images and volume, so that
	// it runs next to anything of the user's.
	checkName = "obie-tutorial-check"
	// settle is how long a reading command is repeated until its output is
	// what the page shows: bans, verdicts and peers take a moment.
	settle = 30 * time.Second
	// buildBound bounds building the release and the images; stepBound
	// every command of the page.
	buildBound = 15 * time.Minute
	stepBound  = 3 * time.Minute
	// reader is the administrator who follows the page on the server, in
	// an SSH session from sessionAddress.
	reader  = "alice"
	session = sessionAddress + " 50022 10.0.0.2 22"
	// attacker fails five SSH logins in step 7; peerAddress is the peer's
	// node in step 8.
	attacker    = "85.10.0.7"
	peerAddress = "198.51.100.20"
	// releases is where the page downloads the release from; the check
	// serves the release it built at downloads instead.
	releases  = "https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange/releases/download/v"
	downloads = "http://127.0.0.1:8000/"
	// attackSection is the step before which the attack happens.
	attackSection = "7. See the first verdict"
)

var cyclonedx = flag.String("tutorial.cyclonedx", "", "the cyclonedx-gomod binary packaging/release.sh needs (make tutorial-check passes it)")

var (
	// pageRelease is the release the page downloads.
	pageRelease = regexp.MustCompile(regexp.QuoteMeta(releases) + `([0-9]+\.[0-9]+\.[0-9]+[^/]*)/`)
	// peerID is a peer ID the page types, which stands for the peer's.
	peerID = regexp.MustCompile(`12D3KooW[1-9A-HJ-NP-Za-km-z]+`)
	// promptLine is a prompt of the setup assistant in the page's
	// transcript, with the answer the reader types after it.
	promptLine = regexp.MustCompile(`^(.*(?:\]|\(empty: done\)):)(?: (.*))?$`)
	// interactive is a command that asks questions on a terminal.
	interactive = regexp.MustCompile(`^sudo obied setup$`)
	// reading is a command that only looks, and may be repeated.
	reading = regexp.MustCompile(`^(sudo )?(uname|ps|sha256sum|journalctl|nft list|fail2ban-client (version|status|-t)|obied (self-check|--config)|obiectl (status|peers|identity|indicators|decisions|explain|enforced|overrides|show))\b` +
		`|^docker exec obie obiectl (status|identity)\b`)
	// containerNames are what the container section names, replaced by
	// the check's own, in this order.
	containerNames = []struct {
		re   *regexp.Regexp
		with string
	}{
		{regexp.MustCompile(`ghcr\.io/mncloudwerkstechnology/obie:`), checkName + "-image:"},
		{regexp.MustCompile(`\bobie-state\b`), checkName + "-state"},
		{regexp.MustCompile(`(--name|exec|rm -f) obie\b`), "$1 " + checkName + "-node"},
		{regexp.MustCompile(`-p 4001:4001 -p 4001:4001/udp`), "-p 127.0.0.1::4001 -p 127.0.0.1::4001/udp"},
	}
)

// check is one run of the tutorial against a server of its own.
type check struct {
	t       *testing.T
	root    string
	version string
	release string // the directory of the release built for the check
	host    string // the server's container
	peerID  string // the peer ID of the peer's node
	failed  bool
}

// TestTutorial runs documentation/getting-started.md: every command in
// order on a systemd host in a container, against the release built from
// this tree, the output compared with what the page shows (ADR 0030).
func TestTutorial(t *testing.T) {
	p := parsePage(readRepoFile(t, tutorialPath))
	if len(p.problems) > 0 {
		t.Fatalf("%s is not in the form the check reads: %q", tutorialPath, p.problems)
	}
	if *cyclonedx == "" {
		t.Fatal("-tutorial.cyclonedx is not set; run the check with make tutorial-check")
	}
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	c := &check{t: t, root: root, version: releaseOf(t, p), host: checkName}
	t.Cleanup(c.remove)
	c.removeContainers() // what an interrupted run left
	c.buildRelease()
	c.buildImages()
	c.startServer()
	c.startPeer()

	attacked := false
	for _, s := range p.steps {
		if s.section == attackSection && !attacked {
			c.attack()
			attacked = true
		}
		for _, cmd := range s.commands {
			c.run(s, cmd)
		}
	}
}

// releaseOf returns the version of the release the page downloads.
func releaseOf(t *testing.T, p page) string {
	t.Helper()
	for _, s := range p.steps {
		for _, cmd := range s.commands {
			if m := pageRelease.FindStringSubmatch(cmd); m != nil {
				return m[1]
			}
		}
	}
	t.Fatalf("%s downloads no release from %s", tutorialPath, releases)
	return ""
}

// run runs one command of the page and compares its output with the
// page's, if the page shows one.
func (c *check) run(s step, cmd string) {
	t := c.t
	t.Logf("line %d: %s", s.line, cmd)
	var got string
	var err error
	deadline := time.Now().Add(settle)
	for {
		switch {
		case s.section == containerSection:
			got, err = c.onDocker(cmd)
		case interactive.MatchString(cmd):
			got, err = c.inTerminal(cmd, answers(s.want))
		default:
			got, err = c.onServer(cmd)
		}
		if err == nil && (!s.hasWant || matches(s.want, got)) {
			return
		}
		if !reading.MatchString(cmd) || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Second)
	}
	c.diagnose()
	if err != nil {
		t.Fatalf("line %d: %s: %v\n%s", s.line, cmd, err, got)
	}
	t.Errorf("line %d: %s printed\n%s\n\nthe page shows\n%s", s.line, cmd,
		strings.Join(normalize(got), "\n"), strings.Join(normalize(s.want), "\n"))
}

// answers returns what the reader types into an interactive command: the
// answer after each prompt of the page's transcript. The page's transcript
// shows every prompt.
func answers(transcript string) []answer {
	var out []answer
	for _, line := range strings.Split(transcript, "\n") {
		if m := promptLine.FindStringSubmatch(strings.TrimRight(line, " ")); m != nil {
			out = append(out, answer{prompt: m[1], text: m[2]})
		}
	}
	return out
}

// onServer runs a command of the page on the server, as the reader in an
// SSH session.
func (c *check) onServer(cmd string) (string, error) {
	cmd = strings.ReplaceAll(cmd, releases+c.version+"/", downloads)
	return c.command(stepBound, "docker", "exec", "-u", reader, "-w", "/home/"+reader, "-e", "SSH_CONNECTION="+session,
		c.host, "bash", "-c", cmd)
}

// inTerminal runs an interactive command of the page on the server and
// types the answers, with the peer's real peer ID for the one the page
// shows.
func (c *check) inTerminal(cmd string, typed []answer) (string, error) {
	for i := range typed {
		typed[i].text = peerID.ReplaceAllString(typed[i].text, c.peerID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), stepBound)
	defer cancel()
	return runInTerminal(ctx, []string{"docker", "exec", "-it", "-u", reader, "-w", "/home/" + reader,
		"-e", "SSH_CONNECTION=" + session, c.host, "bash", "-c", cmd}, typed)
}

// onDocker runs a command of the container section on the Docker host,
// with the check's own names for the container, its volume and image.
func (c *check) onDocker(cmd string) (string, error) {
	for _, n := range containerNames {
		cmd = n.re.ReplaceAllString(cmd, n.with)
	}
	return c.command(stepBound, "bash", "-c", cmd)
}

// asRoot runs a command on the server as root, for what the check sets up.
func (c *check) asRoot(script string) string {
	c.t.Helper()
	out, err := c.command(stepBound, "docker", "exec", c.host, "bash", "-ec", script)
	if err != nil {
		c.t.Fatalf("on the server: %v\n%s\n%s", err, script, out)
	}
	return out
}

// command runs a program on the Docker host and returns its output.
func (c *check) command(bound time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), bound)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- the page's commands and the check's own.
	cmd.Dir = c.root
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// must runs a program on the Docker host and fails the check if it fails.
func (c *check) must(bound time.Duration, name string, args ...string) string {
	c.t.Helper()
	out, err := c.command(bound, name, args...)
	if err != nil {
		c.t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return out
}

// buildRelease builds the release archive the page installs, for amd64.
func (c *check) buildRelease() {
	c.release = c.t.TempDir()
	cmd := exec.Command(filepath.Join(c.root, "packaging", "release.sh"), c.release) // #nosec G204 -- this repository's release script.
	cmd.Dir = c.root
	cmd.Env = append(os.Environ(), "VERSION="+c.version, "PLATFORMS=linux/amd64", "CYCLONEDX_GOMOD="+*cyclonedx)
	if out, err := cmd.CombinedOutput(); err != nil {
		c.t.Fatalf("packaging/release.sh: %v\n%s", err, out)
	}
}

// buildImages builds the server's image and OBIE's container image.
func (c *check) buildImages() {
	c.must(buildBound, "docker", "build", "-q", "-t", checkName+"-host", filepath.Join("test", "tutorial", "testdata", "host"))
	c.must(buildBound, "docker", "build", "-q", "--build-arg", "VERSION="+c.version, "-t", checkName+"-image:"+c.version, ".")
}

// startServer starts the server with systemd, and serves the release to
// the page's download commands.
func (c *check) startServer() {
	c.must(time.Minute, "docker", "run", "-d", "--name", c.host, "--hostname", "server",
		"--privileged", "--cgroupns=private", "--tmpfs", "/run", "--tmpfs", "/run/lock", checkName+"-host")
	// Copied, not mounted: a Docker daemon outside the test's file system,
	// as in some CI runners, would mount an empty directory.
	c.must(time.Minute, "docker", "cp", c.release+"/.", c.host+":/srv/release")
	// systemd answers once its bus is up, and then when it has started
	// everything.
	state := ""
	for deadline := time.Now().Add(2 * time.Minute); time.Now().Before(deadline); time.Sleep(time.Second) {
		out, _ := c.command(2*time.Minute, "docker", "exec", c.host, "systemctl", "is-system-running", "--wait")
		if state = strings.TrimSpace(out); state == "running" || state == "degraded" {
			break
		}
	}
	if state != "running" {
		failed, _ := c.command(time.Minute, "docker", "exec", c.host, "systemctl", "--failed", "--no-pager")
		c.t.Fatalf("the server's systemd is %q, not running:\n%s", state, failed)
	}
	c.must(time.Minute, "docker", "exec", "-d", c.host, "python3", "-m", "http.server", "8000",
		"--bind", "127.0.0.1", "--directory", "/srv/release")
	c.asRoot("for i in $(seq 50); do curl -fsS -o /dev/null " + downloads + "SHA256SUMS && exit 0; sleep 0.2; done; exit 1")
}

// startPeer starts the peer's node at peerAddress, in a network namespace
// of its own on the server: obied of the same release, in observe mode.
func (c *check) startPeer() {
	c.asRoot(fmt.Sprintf(`
mkdir -p /srv/peer /run/obie-peer
tar -xzf /srv/release/obie-%[1]s-linux-amd64.tar.gz -C /srv/peer --strip-components=1
install -d -m 0700 /srv/peer/state
cat >/srv/peer/obie.yaml <<'EOF'
node:
  state_dir: /srv/peer/state
  mode: observe
admin:
  socket: /run/obie-peer/obie.sock
  socket_group: root
mesh:
  listen: [/ip4/0.0.0.0/tcp/4001, /ip4/0.0.0.0/udp/4001/quic-v1]
EOF
/srv/peer/bin/obied keygen --state-dir /srv/peer/state >/srv/peer/identity.txt
ip link add obie-peer type veth peer name obie-peer-ns
ip netns add peer
ip link set obie-peer-ns netns peer
ip addr add 198.51.100.1/24 dev obie-peer
ip link set obie-peer up
ip netns exec peer ip addr add %[2]s/24 dev obie-peer-ns
ip netns exec peer ip link set obie-peer-ns up
ip netns exec peer ip link set lo up
`, c.version, peerAddress))
	c.peerID = peerID.FindString(c.asRoot("cat /srv/peer/identity.txt"))
	if c.peerID == "" {
		c.t.Fatal("obied keygen named no peer ID for the peer")
	}
	c.must(time.Minute, "docker", "exec", "-d", c.host, "ip", "netns", "exec", "peer",
		"/srv/peer/bin/obied", "--config", "/srv/peer/obie.yaml")
	c.asRoot("for i in $(seq 100); do ip netns exec peer /srv/peer/bin/obiectl --socket /run/obie-peer/obie.sock status >/dev/null 2>&1 && exit 0; sleep 0.2; done; exit 1")
}

// attack lets the attacker fail five SSH logins from a network namespace
// of its own, which makes Fail2Ban's sshd jail ban it.
func (c *check) attack() {
	c.asRoot(fmt.Sprintf(`
ip link add obie-atk type veth peer name obie-atk-ns
ip netns add attacker
ip link set obie-atk-ns netns attacker
ip addr add 85.10.0.1/24 dev obie-atk
ip link set obie-atk up
ip netns exec attacker ip addr add %s/24 dev obie-atk-ns
ip netns exec attacker ip link set obie-atk-ns up
ip netns exec attacker ip link set lo up
for i in 1 2 3 4 5; do
  timeout 10 ip netns exec attacker ssh -o BatchMode=yes -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null -o ConnectTimeout=5 intruder@85.10.0.1 true >/dev/null 2>&1 || true
done
`, attacker))
}

// diagnose logs, once, what explains a failed step: the node's and
// Fail2Ban's logs.
func (c *check) diagnose() {
	if c.failed {
		return
	}
	c.failed = true
	out, _ := c.command(time.Minute, "docker", "exec", c.host, "bash", "-c",
		"systemctl --failed --no-pager; journalctl -u obied -n 40 --no-pager; tail -n 20 /var/log/fail2ban.log")
	c.t.Logf("the server's failed units and logs:\n%s", out)
}

// removeContainers removes the check's containers and volume.
func (c *check) removeContainers() {
	_, _ = c.command(time.Minute, "docker", "rm", "-f", "-v", c.host, checkName+"-node")
	_, _ = c.command(time.Minute, "docker", "volume", "rm", "-f", checkName+"-state")
}

// remove removes the check's containers, volume and images.
func (c *check) remove() {
	c.removeContainers()
	_, _ = c.command(time.Minute, "docker", "image", "rm", "-f", checkName+"-host", checkName+"-image:"+c.version)
}
