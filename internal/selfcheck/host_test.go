package selfcheck

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/enforce/nft"
)

// installFail2Ban creates Fail2Ban's configuration directory, with OBIE's
// action if action is set.
func (h *testHost) installFail2Ban(t *testing.T, action bool) {
	t.Helper()
	dir := filepath.Join(h.env.Fail2BanDir, "action.d")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if action {
		if err := os.WriteFile(filepath.Join(dir, "obie.conf"), []byte("[Definition]\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// fail2banDump is what fail2ban-client -d prints for two jails that report
// to OBIE, in the forms of different Fail2Ban versions, and one that does
// not.
const fail2banDump = `['set', 'sshd', 'addaction', 'obie']
['multi-set', 'sshd', 'action', 'obie', [['actionban', '...']]]
['set', 'recidive', 'addaction', 'iptables-allports']
['multi-set', 'nginx-http-auth', 'action', 'obie', [['reason', 'password_bruteforce']]]
`

func TestCheckFail2BanInstallation(t *testing.T) {
	h := newTestHost(t, "")
	assertCheck(t, h.run(t, "fail2ban"), Warning, "Fail2Ban is not installed, so this server reports no attacks of its own",
		"install Fail2Ban")

	h.installFail2Ban(t, false)
	action := filepath.Join(h.env.Fail2BanDir, "action.d", "obie.conf")
	assertCheck(t, h.run(t, "fail2ban"), Problem, "Fail2Ban is installed, but OBIE's action "+action+" is not", "run install.sh from the release again")

	h.installFail2Ban(t, true)
	c := h.run(t, "fail2ban")
	assertCheck(t, c, Warning, "fail2ban-client is not on the PATH", `sudo fail2ban-client -d | grep "'obie'"`)
	if len(c.Details) != 1 || c.Details[0] != "OBIE's action is installed: "+action {
		t.Errorf("details = %q", c.Details)
	}

	h.commands["fail2ban-client"] = func([]string) ([]byte, error) { return nil, errors.New("exit status 255") }
	assertCheck(t, h.run(t, "fail2ban"), Warning, "cannot list the jails with fail2ban-client -d: exit status 255", "sudo fail2ban-client -t")
	h.env.Euid = func() int { return 1000 }
	assertCheck(t, h.run(t, "fail2ban"), Warning, "cannot list the jails", "sudo obied self-check")

	h.commands["fail2ban-client"] = func([]string) ([]byte, error) { return []byte("['set', 'sshd', 'addaction', 'iptables']\n"), nil }
	// Not connected yet, as between steps 2 and 3 of the quick start: the
	// node works, so it is no problem.
	assertCheck(t, h.run(t, "fail2ban"), Warning, "no Fail2Ban jail uses OBIE's action yet, so no ban is reported", "jail.local")
}

func TestCheckFail2BanReports(t *testing.T) {
	h := newTestHost(t, "")
	h.installFail2Ban(t, true)
	var args []string
	h.commands["fail2ban-client"] = func(a []string) ([]byte, error) { args = a; return []byte(fail2banDump), nil }
	c := h.run(t, "fail2ban")
	assertCheck(t, c, Warning, "whether bans reach the node shows only while it runs", "start the node")
	if strings.Join(args, " ") != "-d" || len(c.Details) != 2 || c.Details[0] != "jails that report every ban to the node: sshd, nginx-http-auth" {
		t.Errorf("fail2ban-client %q; details = %q", args, c.Details)
	}

	h.node.status, h.node.statusErr = readyStatus("1.2.3", "observe"), nil
	var journal string
	var journalArgs []string
	h.commands["journalctl"] = func(a []string) ([]byte, error) { journalArgs = a; return []byte(journal), nil }
	assertCheck(t, h.run(t, "fail2ban"), Warning, "no ban has reached the node yet", "sudo fail2ban-client set sshd banip 203.0.113.7")
	if got := strings.Join(journalArgs, " "); !strings.Contains(got, "--identifier obie-fail2ban") || !strings.Contains(got, "--lines 1") {
		t.Errorf("journalctl %s", got)
	}

	journal = "could not report 203.0.113.7 of jail sshd to OBIE (obiectl exit code 1): obiectl: 203.0.113.7 is not a public address\n"
	assertCheck(t, h.run(t, "fail2ban"), OK, "jails that report every ban to the node")
	if c := h.run(t, "fail2ban"); !strings.Contains(strings.Join(c.Details, "\n"), "which refused it as intended: could not report 203.0.113.7") {
		t.Errorf("details = %q", c.Details)
	}

	journal = "could not report 85.10.0.7 of jail sshd to OBIE (obiectl exit code 127): obiectl: not found"
	assertCheck(t, h.run(t, "fail2ban"), Problem, "the last report failed: could not report 85.10.0.7", "troubleshooting.md#fail2ban-reports-do-not-arrive")

	h.node.indicators = 1
	c = h.run(t, "fail2ban")
	assertCheck(t, c, OK, "jails that report every ban to the node: sshd, nginx-http-auth")
	if len(c.Details) != 2 || c.Details[0] != "the node holds verdicts of its own, so bans reach it" {
		t.Errorf("details = %q", c.Details)
	}
}

func TestCheckFirewall(t *testing.T) {
	h := newTestHost(t, "")
	assertCheck(t, h.run(t, "firewall"), OK, "not needed yet: in observe mode the node blocks nothing")

	h.writeConfigWith(t, "  mode: enforce\n", "enforce:\n  backend: dryrun\n")
	assertCheck(t, h.run(t, "firewall"), Warning, "enforce mode with the dryrun backend blocks nothing", "set enforce.backend: nftables")

	h.writeConfigWith(t, "  mode: enforce\n", "enforce:\n  backend: nftables\n")
	assertCheck(t, h.run(t, "firewall"), OK, "nftables is available")
	h.nftErr = fmt.Errorf("%w: operation not permitted", nft.ErrPermission)
	assertCheck(t, h.run(t, "firewall"), Problem, "nftables cannot be used", "modprobe nf_tables")
	h.env.Euid = func() int { return 1000 }
	assertCheck(t, h.run(t, "firewall"), Warning, "cannot check nftables as user alice", "sudo obied self-check")
	h.nftErr = errors.New("protocol not supported")
	assertCheck(t, h.run(t, "firewall"), Problem, "nftables cannot be used: protocol not supported", "modprobe nf_tables")

	// A running node reports its backend itself.
	h.node.status, h.node.statusErr = readyStatus("1.2.3", "enforce"), nil
	h.node.status.Subsystems["enforce"] = admin.SubsystemStatus{Ready: true, Detail: "3 entries applied"}
	assertCheck(t, h.run(t, "firewall"), OK, "the nftables backend blocks through the table inet obie; 3 entries applied")
	h.node.status.Subsystems["enforce"] = admin.SubsystemStatus{Ready: true, Detail: "enforcing via dryrun"}
	assertCheck(t, h.run(t, "firewall"), Warning, "still enforces with the dryrun backend", "sudo systemctl restart obied")
	h.node.status.Subsystems["enforce"] = admin.SubsystemStatus{Error: nft.ErrPermission.Error()}
	assertCheck(t, h.run(t, "firewall"), Problem, "the nftables backend cannot block: nftables access denied", "grants CAP_NET_ADMIN")
	h.node.status.Subsystems["enforce"] = admin.SubsystemStatus{Error: "table inet obie is missing"}
	assertCheck(t, h.run(t, "firewall"), Problem, "cannot block: table inet obie is missing", "sudo obiectl status")

	h.node.status = readyStatus("1.2.3", "observe")
	h.nftErr = nft.ErrPermission
	assertCheck(t, h.run(t, "firewall"), Warning, "the configuration sets enforce mode, but the node runs in observe mode",
		"sudo systemctl reload obied", "sudo obied self-check")
}

// TestChecksWhenTheNodeRefusesTheUser checks that the checks which ask the
// node say that this user may not ask it, rather than that it does not run.
func TestChecksWhenTheNodeRefusesTheUser(t *testing.T) {
	h := newTestHost(t, federatedConfig)
	h.env.Euid = func() int { return 1000 }
	h.node.statusErr = fmt.Errorf("%w on admin socket %s: run as root", os.ErrPermission, h.socket)
	h.dialer.up["198.51.100.20:4001"] = true
	h.dialer.up["obie.partner.example:4001"] = true
	c := h.run(t, "peers")
	assertCheck(t, c, Warning, "cannot ask the node which peers are connected as user alice", "sudo obied self-check")
	if len(c.Details) != 3 || c.Details[0] != "every peer in mesh.bootstrap answers (2 configured)" {
		t.Errorf("details = %q", c.Details)
	}

	h.writeConfig(t, "trust:\n  publishers:\n    - {peer_id: "+friendID+", name: friend, weight: 1}\n")
	c = h.run(t, "peers")
	assertCheck(t, c, Warning, "cannot ask the node which peers are connected", "sudo obied self-check")
	if len(c.Details) != 1 || c.Details[0] != "1 peer trusted, none in mesh.bootstrap: they connect to this node" {
		t.Errorf("details = %q", c.Details)
	}

	h.installFail2Ban(t, true)
	h.commands["fail2ban-client"] = func([]string) ([]byte, error) { return []byte(fail2banDump), nil }
	c = h.run(t, "fail2ban")
	assertCheck(t, c, Warning, "cannot ask the node whether bans reach it as user alice", "sudo obied self-check")
	if strings.Contains(strings.Join(append(c.Details, c.NextSteps...), "\n"), "start the node") {
		t.Errorf("the check asks to start a node that may run: %+v", c)
	}

	h.session = netip.MustParseAddr("85.10.0.7")
	assertCheck(t, h.run(t, "session"), Warning, "which OBIE does not protect", "sudo obiectl allow 85.10.0.7", "sudo obied self-check")
	if c := h.run(t, "session"); len(c.Details) != 1 || c.Details[0] != "cannot ask the node about overrides of 85.10.0.7 as user alice" {
		t.Errorf("details = %q", c.Details)
	}
}

func TestCheckSession(t *testing.T) {
	h := newTestHost(t, "")
	assertCheck(t, h.run(t, "session"), OK, "no SSH session found")

	h.session = netip.MustParseAddr("10.1.2.3")
	assertCheck(t, h.run(t, "session"), OK, "your SSH session comes from 10.1.2.3, which is protected (allow-listed: built-in range 10.0.0.0/8")

	h.session = netip.MustParseAddr("85.10.0.7")
	report := Run(context.Background(), h.env)
	c := report.Checks[len(report.Checks)-1]
	assertCheck(t, c, Warning, "your SSH session comes from 85.10.0.7, which OBIE does not protect: a block would lock you out once the node enforces",
		"add 85.10.0.7/32 to allowlist.cidrs in "+h.config)
	var text bytes.Buffer
	if err := WriteText(&text, report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text.String(), "!! LOCKOUT RISK: your SSH session comes from 85.10.0.7") || !strings.Contains(text.String(), "observe mode") {
		t.Errorf("text report has no lockout banner:\n%s", text.String())
	}

	h.writeConfigWith(t, "  mode: enforce\n", "")
	assertCheck(t, h.run(t, "session"), Problem, "which OBIE does not protect: a block would lock you out", "sudo obiectl allow 85.10.0.7")

	h.writeConfigWith(t, "  mode: enforce\n", "allowlist:\n  cidrs: [85.10.0.0/24]\n")
	assertCheck(t, h.run(t, "session"), OK, "which is protected (allow-listed: allowlist.cidrs entry 85.10.0.0/24)")

	// A running node's explanation counts, overrides included.
	h.node.status, h.node.statusErr = readyStatus("1.2.3", "enforce"), nil
	h.node.allowed = map[string]string{}
	assertCheck(t, h.run(t, "session"), Problem, "which OBIE does not protect", "sudo obiectl allow 85.10.0.7")
	h.node.allowed["85.10.0.7"] = "operator force-allow override on ipv4:85.10.0.7"
	assertCheck(t, h.run(t, "session"), OK, "which is protected (operator force-allow override on ipv4:85.10.0.7)")
	h.node.explainErr = errors.New("503 Service Unavailable")
	assertCheck(t, h.run(t, "session"), OK, "which is protected (allow-listed: allowlist.cidrs entry 85.10.0.0/24)")

	h.writeConfigWith(t, "", "allowlist:\n  files: ["+filepath.Join(h.dir, "missing.txt")+"]\n")
	h.node.statusErr = admin.ErrDaemonNotRunning
	assertCheck(t, h.run(t, "session"), Warning, "cannot tell whether 85.10.0.7, your SSH session's address, is protected", "sudo obiectl explain 85.10.0.7")
}
