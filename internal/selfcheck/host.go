package selfcheck

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/enforce/nft"
	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// commandTimeout bounds every program the checks run.
const commandTimeout = 10 * time.Second

// jailAction matches a jail that uses the obie action in the dump of
// `fail2ban-client -d`, e.g. ['set', 'sshd', 'addaction', 'obie'] or
// ['multi-set', 'sshd', 'action', 'obie', [...]].
var jailAction = regexp.MustCompile(`\['(?:multi-)?set', '([^']+)', '(?:add)?action', 'obie'`)

// refusedAsIntended are parts of obied's answers to reports it refuses by
// design; the quick start tests the way from Fail2Ban with one of them.
var refusedAsIntended = []string{"is not a public address", "overlaps the allow-listed network"}

// Next steps of the Fail2Ban check.
const (
	nextInstallFail2Ban = "to report bans, install Fail2Ban (e.g. sudo apt install fail2ban), run install.sh from the release " +
		"again to add OBIE's action, and follow documentation/guides/fail2ban.md"
	nextAddJail = "add obie to the action of a jail in /etc/fail2ban/jail.local and reload Fail2Ban " +
		"(quick start, step 3: documentation/operations/quickstart.md#3-connect-fail2ban)"
	nextTestBan = "test the way from Fail2Ban to the node: sudo fail2ban-client set sshd banip 203.0.113.7, " +
		"then sudo journalctl -t obie-fail2ban -n 5 (quick start, step 4)"
)

// checkFail2Ban: Fail2Ban has OBIE's action, a jail uses it, and its
// reports reach the node.
func (r *run) checkFail2Ban() Check {
	const id, name = "fail2ban", "Fail2Ban"
	client, lookErr := r.env.LookPath("fail2ban-client")
	info, statErr := os.Stat(r.env.Fail2BanDir)
	if lookErr != nil && (statErr != nil || !info.IsDir()) {
		return newCheck(id, name, warn("Fail2Ban is not installed, so this server reports no attacks of its own; "+
			"the node still acts on its peers' verdicts", nextInstallFail2Ban))
	}
	action := filepath.Join(r.env.Fail2BanDir, "action.d", "obie.conf")
	if _, err := os.Stat(action); err != nil {
		return newCheck(id, name, problem(fmt.Sprintf("Fail2Ban is installed, but OBIE's action %s is not", action),
			fmt.Sprintf("run install.sh from the release again, which adds it, or copy fail2ban/action.d/obie.conf from the release to %s", action)))
	}
	jails, jailsFinding := r.jails(client, lookErr)
	findings := []finding{jailsFinding}
	if jails > 0 {
		findings = append(findings, r.reportsFinding())
	}
	return newCheck(id, name, append(findings, ok("OBIE's action is installed: "+action))...)
}

// jails lists the jails that use the obie action.
func (r *run) jails(client string, lookErr error) (int, finding) {
	if lookErr != nil {
		return 0, warn("fail2ban-client is not on the PATH, so the jails that use OBIE's action cannot be listed",
			`check them yourself: sudo fail2ban-client -d | grep "'obie'"`)
	}
	ctx, cancel := context.WithTimeout(r.ctx, commandTimeout)
	defer cancel()
	out, err := r.env.Command(ctx, client, "-d")
	if err != nil {
		next := "check the Fail2Ban configuration: sudo fail2ban-client -t"
		if r.env.Euid() != 0 {
			next = asRoot
		}
		return 0, warn(fmt.Sprintf("cannot list the jails with fail2ban-client -d: %v", err), next)
	}
	var names []string
	for _, m := range jailAction.FindAllStringSubmatch(string(out), -1) {
		if !slices.Contains(names, m[1]) {
			names = append(names, m[1])
		}
	}
	if len(names) == 0 {
		// As without Fail2Ban, the node works; the quick start connects a
		// jail only after the first start.
		return 0, warn("no Fail2Ban jail uses OBIE's action yet, so no ban is reported", nextAddJail)
	}
	return len(names), ok("jails that report every ban to the node: " + strings.Join(names, ", "))
}

// reportsFinding says whether bans reach the node: it holds verdicts of
// its own, or the action logged what the node answered to the last one.
func (r *run) reportsFinding() finding {
	if r.denied() {
		return r.cannotAsk("whether bans reach it")
	}
	if !r.running() {
		return warn("whether bans reach the node shows only while it runs", "start the node, then run the self-check again")
	}
	if n, err := r.ownVerdicts(); err == nil && n > 0 {
		return ok("the node holds verdicts of its own, so bans reach it")
	}
	last := r.lastActionMessage()
	switch {
	case last == "":
		return warn("no ban has reached the node yet", nextTestBan)
	case slices.ContainsFunc(refusedAsIntended, func(part string) bool { return strings.Contains(last, part) }):
		return ok("the last report reached the node, which refused it as intended: " + last)
	}
	return problem("the last report failed: "+last,
		"see documentation/operations/troubleshooting.md#fail2ban-reports-do-not-arrive for every message and its fix")
}

// ownVerdicts counts (up to one) active verdicts this node published.
func (r *run) ownVerdicts() (int, error) {
	ctx, cancel := context.WithTimeout(r.ctx, callTimeout)
	defer cancel()
	id, err := r.node.Identity(ctx)
	if err != nil {
		return 0, err
	}
	page, err := r.node.Indicators(ctx, admin.IndicatorsQuery{Publisher: id.PeerID, Limit: 1})
	if err != nil {
		return 0, err
	}
	return len(page.Indicators), nil
}

// lastActionMessage returns the last message the Fail2Ban action logged in
// the past day (it logs only failed reports), or "".
func (r *run) lastActionMessage() string {
	ctx, cancel := context.WithTimeout(r.ctx, commandTimeout)
	defer cancel()
	out, err := r.env.Command(ctx, "journalctl", "--quiet", "--no-pager", "--output", "cat",
		"--identifier", "obie-fail2ban", "--since", "-24h", "--lines", "1")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// checkFirewall: in enforce mode, the backend can block.
func (r *run) checkFirewall() Check {
	const id, name = "firewall", "Firewall"
	if r.cfg == nil {
		return r.notChecked(id, name)
	}
	var findings []finding
	mode := r.cfg.Node.Mode
	if r.running() && r.status.Mode != string(mode) {
		findings = append(findings, warn(fmt.Sprintf("the configuration sets %s mode, but the node runs in %s mode", mode, r.status.Mode),
			"reload the node to apply the configuration: sudo systemctl reload obied"))
	}
	switch {
	case mode == config.ModeObserve:
		findings = append(findings, ok("not needed yet: in observe mode the node blocks nothing"))
	case r.cfg.Enforce.Backend == config.BackendDryRun:
		findings = append(findings, warn("enforce mode with the dryrun backend blocks nothing: it only logs what it would block",
			"to block, set enforce.backend: nftables and restart: sudo systemctl restart obied"))
	default:
		findings = append(findings, r.nftablesFinding())
	}
	return newCheck(id, name, findings...)
}

// nftablesFinding says whether the nftables backend works: as the running
// node reports it, or else as a probe of the kernel finds it.
func (r *run) nftablesFinding() finding {
	if r.running() && r.status.Mode == string(config.ModeEnforce) {
		sub, found := r.status.Subsystems["enforce"]
		switch {
		case found && strings.Contains(sub.Detail, string(config.BackendDryRun)):
			return warn("the running node still enforces with the dryrun backend, which blocks nothing",
				"restart the node to switch to nftables: sudo systemctl restart obied")
		case found && sub.Ready:
			return ok(strings.TrimSuffix("the nftables backend blocks through the table inet obie; "+sub.Detail, "; "))
		case found && strings.Contains(sub.Error, "CAP_NET_ADMIN"):
			return problem("the nftables backend cannot block: "+sub.Error,
				"run obied under the shipped systemd unit, which grants CAP_NET_ADMIN, and restart it")
		case found:
			return problem("the nftables backend cannot block: "+sub.Error,
				"see what the node says: sudo obiectl status; sudo journalctl -u obied -n 50")
		}
	}
	ctx, cancel := context.WithTimeout(r.ctx, callTimeout)
	defer cancel()
	err := r.env.NFTables(ctx)
	switch {
	case err == nil:
		return ok("nftables is available: the node can block through its own table inet obie")
	case errors.Is(err, nft.ErrPermission) && r.env.Euid() != 0:
		return warn(fmt.Sprintf("cannot check nftables as user %s", r.me()), asRoot)
	}
	return problem(fmt.Sprintf("nftables cannot be used: %v", err),
		"use a Linux kernel with nftables (load it with: sudo modprobe nf_tables), or stay in observe mode")
}

// checkSession: the address of the operator's SSH session is protected
// from being blocked; otherwise the report warns of a lockout.
func (r *run) checkSession() Check {
	const id, name = "session", "SSH session"
	addr, found := r.env.Session()
	if !found {
		return newCheck(id, name, ok("no SSH session found, so there is no remote address of yours to protect"))
	}
	if r.cfg == nil {
		return r.notChecked(id, name)
	}
	how, protected, err := r.protection(addr)
	if err != nil {
		return newCheck(id, name, warn(fmt.Sprintf("cannot tell whether %s, your SSH session's address, is protected: %v", addr, err),
			fmt.Sprintf("ask the running node: sudo obiectl explain %s", addr)))
	}
	if protected {
		return newCheck(id, name, ok(fmt.Sprintf("your SSH session comes from %s, which is protected (%s)", addr, how)))
	}
	enforcing := r.cfg.Node.Mode == config.ModeEnforce || r.running() && r.status.Mode == string(config.ModeEnforce)
	r.lockout, r.enforcing = addr, enforcing
	text := fmt.Sprintf("your SSH session comes from %s, which OBIE does not protect: a block would lock you out", addr)
	next := fmt.Sprintf("protect it: add %s to allowlist.cidrs in %s and reload: sudo systemctl reload obied; "+
		"on a running node also at once: sudo obiectl allow %s --note \"my SSH session\"",
		netip.PrefixFrom(addr, addr.BitLen()), r.env.ConfigPath, addr)
	f := problem(text, next)
	if !enforcing {
		f = warn(text+" once the node enforces", next)
	}
	if r.denied() {
		// The allow-list of the configuration does not protect it; an
		// override on the running node might.
		return newCheck(id, name, f, r.cannotAsk("about overrides of "+addr.String()))
	}
	return newCheck(id, name, f)
}

// protection says whether addr is never blocked, and why: as the running
// node explains it, overrides included, or else as the allow-list of the
// configuration has it.
func (r *run) protection(addr netip.Addr) (how string, protected bool, err error) {
	if r.running() {
		ctx, cancel := context.WithTimeout(r.ctx, callTimeout)
		d, err := r.node.Explain(ctx, addr.String())
		cancel()
		if err == nil {
			if d.State != admin.StateAllowed {
				return "", false, nil
			}
			if d.Sovereignty != nil && d.Sovereignty.Note != "" {
				return d.Sovereignty.Note, true, nil
			}
			return d.Reason, true, nil
		}
	}
	ctx, cancel := context.WithTimeout(r.ctx, sovereignty.ResolveTimeout)
	defer cancel()
	allow, err := sovereignty.Build(ctx, r.cfg, r.env.Allowlist, slog.New(slog.DiscardHandler))
	if err != nil {
		return "", false, err
	}
	kind := obieproto.KindIPv4
	if addr.Is6() {
		kind = obieproto.KindIPv6
	}
	ruling := sovereignty.Judge(obieproto.Indicator{Kind: kind, Value: addr.String()}, allow, nil, r.env.Now())
	return ruling.Reason, ruling.Effect == sovereignty.EffectAllow, nil
}
