package cli

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/netip"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/session"
	"github.com/MNCloudwerksTechnology/obie/internal/setup"
)

// TutorialURL is the tutorial the assistant sends the operator to once the
// configuration is written: the quick start, from connecting Fail2Ban on.
const TutorialURL = "https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange/blob/main/documentation/operations/quickstart.md#3-connect-fail2ban"

// setupGroup is the group that gets the configuration file: the service
// group install.sh creates, in which obied runs.
const setupGroup = "obie"

// setupQuestions is the number of questions the assistant asks.
const setupQuestions = 5

// setupEnv is what obied setup learns about the host; tests replace it.
type setupEnv struct {
	// session returns the address of the operator's SSH session.
	session func() (netip.Addr, bool)
	// checkWritable reports whether the configuration file can be written.
	checkWritable func(path string) error
	// group is the group that gets the file; groupExists says whether the
	// host has it.
	group       string
	groupExists func(name string) bool
	// userName names the user running the assistant.
	userName func() string
}

func hostSetupEnv() setupEnv {
	return setupEnv{
		session:       func() (netip.Addr, bool) { return session.ClientAddr(session.Env{}) },
		checkWritable: setup.CheckWritable,
		group:         setupGroup,
		groupExists:   func(name string) bool { _, err := user.LookupGroup(name); return err == nil },
		userName:      func() string { return lookupUserName(os.Geteuid()) },
	}
}

// lookupUserName returns the name of the user with uid, or the uid.
func lookupUserName(uid int) string {
	if u, err := user.LookupId(strconv.Itoa(uid)); err == nil {
		return u.Username
	}
	return strconv.Itoa(uid)
}

// repeatable is a flag that may be given several times.
type repeatable []string

func (r *repeatable) String() string     { return strings.Join(*r, " ") }
func (r *repeatable) Set(s string) error { *r = append(*r, s); return nil }

// answerFlags are the flags that answer the assistant's questions up front.
var answerFlags = []string{"state-dir", "audit-log", "peer", "mode", "allow", "force"}

func runSetup(args []string, stdout, stderr io.Writer) int {
	return runSetupWith(args, os.Stdin, stdout, stderr, hostSetupEnv())
}

// runSetupWith is obied setup reading answers from stdin and learning about
// the host from env.
func runSetupWith(args []string, stdin io.Reader, stdout, stderr io.Writer, env setupEnv) int {
	const program = "obied setup"
	defaults := setup.Defaults()
	flags := newFlagSet(program, stderr)
	configPath := flags.String("config", config.DefaultPath, "configuration `file` to write")
	nonInteractive := flags.Bool("non-interactive", false, "ask nothing: take the answers from the flags below")
	stateDir := flags.String("state-dir", defaults.StateDir, "state `directory` of the node (node.state_dir)")
	auditLog := flags.String("audit-log", defaults.AuditLog, "audit log `file` (audit.path), or none")
	var peers, allows repeatable
	flags.Var(&peers, "peer", "a peer to connect to and trust: `address[,name=NAME][,weight=0..1]` (weight "+
		strconv.FormatFloat(setup.DefaultWeight, 'f', -1, 64)+" unless given); repeat for more peers")
	mode := flags.String("mode", string(defaults.Mode), "observe (recommended: block nothing) or enforce")
	flags.Var(&allows, "allow", "an `address or network` never to block; repeat for more")
	force := flags.Bool("force", false, "replace an existing configuration file; the old one is kept as a backup")
	flags.Usage = func() { setupUsage(flags) }
	if code, done := parseCommand(flags, program, args, stderr); done {
		return code
	}
	path, err := filepath.Abs(*configPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", program, err)
		return ExitUsage
	}
	if !*nonInteractive {
		if given := setFlags(flags, answerFlags); len(given) > 0 {
			_, _ = fmt.Fprintf(stderr, "%s: --%s answers a question up front and needs --non-interactive\n", program, given[0])
			return ExitUsage
		}
	}
	if err := env.checkWritable(path); err != nil {
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			err = pathErr.Err
		}
		_, _ = fmt.Fprintf(stderr, "%s: cannot write %s as user %s: %v\n", program, path, env.userName(), err)
		_, _ = fmt.Fprintf(stderr, "%s: run it as root: sudo obied setup\n", program)
		return ExitFailure
	}
	existing := describeExisting(path)

	var answers setup.Answers
	replace := *force
	if *nonInteractive {
		if answers, err = answersFromFlags(*stateDir, *auditLog, *mode, peers, allows); err != nil {
			_, _ = fmt.Fprintf(stderr, "%s: %v\n", program, err)
			return ExitUsage
		}
		if existing != "" && !replace {
			_, _ = fmt.Fprintf(stderr, "%s: %s exists already (%s); pass --force to replace it (the old one is kept as a backup)\n",
				program, path, existing)
			return ExitFailure
		}
	} else {
		a := &assistant{in: bufio.NewReader(stdin), out: stdout, env: env}
		var ok bool
		answers, ok, err = a.run(path, existing)
		switch {
		case errors.Is(err, errInputEnded):
			_, _ = fmt.Fprintf(stderr, "\n%s: the input ended before every question was answered; nothing was written.\n", program)
			_, _ = fmt.Fprintf(stderr, "%s: to set up without questions, give the answers as flags with --non-interactive (see obied setup --help)\n", program)
			return ExitFailure
		case err != nil:
			_, _ = fmt.Fprintf(stderr, "\n%s: %v\n", program, err)
			return ExitIOError
		case !ok:
			_, _ = fmt.Fprintf(stdout, "Nothing was written.\n")
			return ExitOK
		}
		replace = existing != ""
	}
	return writeSetup(program, path, answers, replace, stdout, stderr, env)
}

// setFlags returns which of names were given on the command line.
func setFlags(fs *flag.FlagSet, names []string) []string {
	var given []string
	fs.Visit(func(f *flag.Flag) {
		for _, name := range names {
			if f.Name == name {
				given = append(given, name)
			}
		}
	})
	return given
}

func setupUsage(fs *flag.FlagSet) {
	out := fs.Output()
	_, _ = fmt.Fprintf(out, `Usage: obied setup [--config file]
       obied setup --non-interactive [answer flags] [--force]

Writes the configuration of this node after asking a few questions:
where it keeps its state and audit log, which peers it connects to and
how much it trusts them, whether it starts in observe mode, and which
addresses it must never block. Every question offers a safe default. An
existing configuration file is only replaced after you agree; the old one
is kept as a backup. With --non-interactive the answers come from the
flags, and the same answers always write the same file.

Flags:
`)
	fs.PrintDefaults()
}

// answersFromFlags builds the answers given as flags.
func answersFromFlags(stateDir, auditLog, mode string, peers, allows []string) (setup.Answers, error) {
	var a setup.Answers
	var err error
	if a.StateDir, err = setup.ParseStateDir(stateDir); err != nil {
		return a, fmt.Errorf("--state-dir: %w", err)
	}
	if a.AuditLog, err = setup.ParseAuditLog(auditLog); err != nil {
		return a, fmt.Errorf("--audit-log: %w", err)
	}
	if a.Mode, err = setup.ParseMode(mode); err != nil {
		return a, fmt.Errorf("--mode: %w", err)
	}
	for _, spec := range peers {
		p, err := setup.ParsePeer(spec)
		if err == nil {
			err = a.AddPeer(p)
		}
		if err != nil {
			return a, fmt.Errorf("--peer: %w", err)
		}
	}
	for _, s := range allows {
		p, err := setup.ParseAllow(s)
		if err != nil {
			return a, fmt.Errorf("--allow: %w", err)
		}
		a.AddAllow(p)
	}
	return a, nil
}

// describeExisting says what the file at path is: "" if there is none,
// else whether it is still the example install.sh installed next to it.
func describeExisting(path string) string {
	current, err := os.ReadFile(path) // #nosec G304 -- the configuration path the operator chose.
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return ""
	case err != nil:
		return "it cannot be read: " + err.Error()
	}
	example, err := os.ReadFile(path + ".example") // #nosec G304 -- next to the configuration.
	if err == nil && bytes.Equal(current, example) {
		return exampleUnchanged
	}
	return "with settings of your own"
}

// exampleUnchanged describes a configuration install.sh installed and
// nobody changed.
const exampleUnchanged = "the unchanged example that install.sh installed"

// writeSetup renders and writes the configuration and tells the operator
// what to do next.
func writeSetup(program, path string, a setup.Answers, replace bool, stdout, stderr io.Writer, env setupEnv) int {
	data, err := setup.Render(a, path)
	if err == nil {
		var backup string
		backup, err = setup.Write(path, data, setup.WriteOptions{Replace: replace, Group: env.group})
		if err == nil {
			_, _ = fmt.Fprintf(stdout, "Wrote %s.\n", path)
			if backup != "" {
				_, _ = fmt.Fprintf(stdout, "The previous file is kept as %s.\n", backup)
			}
		}
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", program, err)
		return ExitFailure
	}
	if !env.groupExists(env.group) {
		_, _ = fmt.Fprintf(stdout, "\nThe group %s does not exist, so obied, running as %s, cannot read the file.\n"+
			"Run install.sh from the release first; it creates the user and the group.\n", env.group, env.group)
	}
	for _, note := range a.Notes() {
		_, _ = fmt.Fprintf(stdout, "\n%s\n", note)
	}
	if addr, ok := env.session(); ok && !a.Protects(addr) {
		_, _ = fmt.Fprintf(stdout, "\n%s", session.LockoutWarning(addr, a.Mode == config.ModeEnforce))
	}
	_, err = fmt.Fprintf(stdout, `
Next steps:
  1. Start the node (if it runs already, restart it instead):
       sudo systemctl enable --now obied
       sudo systemctl restart obied
  2. Check the node and this server; every problem comes with what to do:
       sudo obied self-check
  3. Open the tutorial and go on with connecting Fail2Ban:
       %s
`, TutorialURL)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: writing the next steps: %v\n", program, err)
		return ExitIOError
	}
	return ExitOK
}

// errInputEnded means the input ended before every question was answered.
var errInputEnded = errors.New("input ended")

// assistant asks the setup questions on a terminal.
type assistant struct {
	in  *bufio.Reader
	out io.Writer
	env setupEnv
	err error // first error writing to out
}

// printf writes to the operator, remembering the first error.
func (a *assistant) printf(format string, args ...any) {
	if _, err := fmt.Fprintf(a.out, format, args...); err != nil && a.err == nil {
		a.err = err
	}
}

// line prints prompt and reads one answer, trimmed.
func (a *assistant) line(prompt string) (string, error) {
	a.printf("%s", prompt)
	if a.err != nil {
		return "", a.err
	}
	text, err := a.in.ReadString('\n')
	if errors.Is(err, io.EOF) && text == "" {
		return "", errInputEnded
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(text), nil
}

// ask asks until parse accepts the answer; an empty answer takes def.
func (a *assistant) ask(label, def string, parse func(string) error) (string, error) {
	for {
		prompt := label + ": "
		if def != "" {
			prompt = label + " [" + def + "]: "
		}
		answer, err := a.line(prompt)
		if err != nil {
			return "", err
		}
		if answer == "" {
			answer = def
		}
		if err := parse(answer); err != nil {
			a.printf("  %v\n", err)
			continue
		}
		return answer, nil
	}
}

// confirm asks a yes/no question; an empty answer takes def.
func (a *assistant) confirm(label string, def bool) (bool, error) {
	choices := "[y/N]"
	if def {
		choices = "[Y/n]"
	}
	for {
		answer, err := a.line(label + " " + choices + ": ")
		if err != nil {
			return false, err
		}
		switch strings.ToLower(answer) {
		case "":
			return def, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		a.printf("  Please answer y or n.\n")
	}
}

// question prints the number, title and explanation of a question.
func (a *assistant) question(n int, title, explanation string) {
	a.printf("\n%d/%d  %s\n", n, setupQuestions, title)
	for _, line := range strings.Split(strings.TrimSpace(explanation), "\n") {
		a.printf("      %s\n", line)
	}
}

// run asks every question and the final confirmation. ok is false when
// the operator decided not to write the file.
func (a *assistant) run(path, existing string) (answers setup.Answers, ok bool, err error) {
	answers = setup.Defaults()
	a.printf("OBIE setup\n\n"+
		"This assistant writes the configuration of this node to %s.\n"+
		"It asks %d questions. Each suggestion in [brackets] is a safe choice:\n"+
		"press Enter to take it. Nothing is written until you confirm at the end.\n",
		path, setupQuestions)
	if existing != "" {
		a.printf("\n%s exists already (%s).\nYou will be asked before it is replaced; the old file is kept as a backup.\n", path, existing)
	}
	steps := []func(*setup.Answers) error{a.askStateDir, a.askAuditLog, a.askPeers, a.askMode, a.askAllow}
	for _, step := range steps {
		if err := step(&answers); err != nil {
			return answers, false, err
		}
	}
	if _, err := setup.Render(answers, path); err != nil {
		return answers, false, err
	}
	a.summary(answers)
	if existing == "" {
		ok, err = a.confirm("Write "+path+"?", true)
	} else {
		ok, err = a.confirm("Replace "+path+"? The old file is kept as a backup.", existing == exampleUnchanged)
	}
	return answers, ok, err
}

func (a *assistant) askStateDir(answers *setup.Answers) error {
	a.question(1, "Where should the node keep its state?", `
The state directory holds the node's identity key, its name on the
network, which you should back up, and its database of verdicts. The
shipped service can only write to `+setup.ServiceStateDir+`; keep it unless you
also adapt the service.`)
	answer, err := a.ask("State directory", answers.StateDir, func(s string) error {
		_, err := setup.ParseStateDir(s)
		return err
	})
	answers.StateDir, _ = setup.ParseStateDir(answer)
	return err
}

func (a *assistant) askAuditLog(answers *setup.Answers) error {
	a.question(2, "Where should the node write its audit log?", `
The node's own log always goes to the system journal (journalctl -u obied).
The audit log also records every decision, override and report, one JSON
line each, for you or your SIEM. Type none to go without it.`)
	answer, err := a.ask("Audit log", answers.AuditLog, func(s string) error {
		_, err := setup.ParseAuditLog(s)
		return err
	})
	answers.AuditLog, _ = setup.ParseAuditLog(answer)
	return err
}

func (a *assistant) askPeers(answers *setup.Answers) error {
	a.question(3, "Which peers should this node connect to?", `
A peer is another OBIE node, such as a friend's server. Its operator gives
you its address, which ends in its peer ID, for example
  /dns4/obie.friend.example/tcp/4001/p2p/12D3KooW...
Without peers the node works on its own and acts only on what this server
detects; you can add peers later. Enter one address per line and an empty
line when you are done.`)
	for {
		answer, err := a.line("Peer address (empty: done): ")
		if err != nil || answer == "" {
			return err
		}
		p, err := setup.ParsePeerAddress(answer)
		if err == nil && slices.ContainsFunc(answers.Peers, func(q setup.Peer) bool { return q.PeerID == p.PeerID }) {
			err = fmt.Errorf("the peer %s is already listed", p.PeerID)
		}
		if err != nil {
			a.printf("  %v\n", err)
			continue
		}
		name, err := a.ask("  Name of this peer", p.Name, func(s string) error { _, err := setup.ParseName(s); return err })
		if err != nil {
			return err
		}
		p.Name, _ = setup.ParseName(name)
		a.printf("  How much do you trust its verdicts, from 0 (not at all) to 1 (fully)?\n" +
			"  With the default settings, one peer alone never gets an address blocked.\n")
		weight, err := a.ask("  Trust weight", strconv.FormatFloat(p.Weight, 'f', -1, 64), func(s string) error {
			_, err := setup.ParseWeight(s)
			return err
		})
		if err != nil {
			return err
		}
		p.Weight, _ = setup.ParseWeight(weight)
		if err := answers.AddPeer(p); err != nil {
			return err
		}
	}
}

func (a *assistant) askMode(answers *setup.Answers) error {
	a.question(4, "Should the node start in observe mode?", `
In observe mode the node decides and shows what it would block, but never
touches the firewall. This is the default, and recommended: watch what it
would do for a few days before you let it block (enforce mode).`)
	observe, err := a.confirm("Start in observe mode?", true)
	if err != nil || observe {
		return err
	}
	a.printf("  Enforce mode blocks through nftables from the first start. An address you\n" +
		"  administer this server from that is not protected (next question) can lock\n" +
		"  you out.\n")
	enforce, err := a.confirm("  Start in enforce mode anyway?", false)
	if enforce {
		answers.Mode = config.ModeEnforce
	}
	return err
}

func (a *assistant) askAllow(answers *setup.Answers) error {
	explanation := `
Private networks, this server's own addresses and its peers are always
protected. Add the addresses or networks you administer this server from,
so that OBIE can never lock you out.`
	def := "none"
	if addr, ok := a.env.session(); ok {
		if answers.Protects(addr) {
			explanation += "\nYour SSH session comes from " + addr.String() + ", which is protected already."
		} else {
			explanation += "\nYour SSH session comes from " + addr.String() + ", which is not protected yet."
			def = netip.PrefixFrom(addr, addr.BitLen()).String()
		}
	}
	a.question(5, "Which addresses must never be blocked?", explanation)
	var allow []netip.Prefix
	_, err := a.ask("Addresses or networks, separated by spaces, or none", def, func(s string) error {
		var err error
		allow, err = parseAllowList(s)
		return err
	})
	for _, p := range allow {
		answers.AddAllow(p)
	}
	return err
}

// parseAllowList parses addresses and networks separated by spaces or
// commas; "none" is the empty list.
func parseAllowList(s string) ([]netip.Prefix, error) {
	if strings.EqualFold(strings.TrimSpace(s), "none") {
		return nil, nil
	}
	var out []netip.Prefix
	for _, field := range strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' }) {
		p, err := setup.ParseAllow(field)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// summary shows the answers before they are written.
func (a *assistant) summary(answers setup.Answers) {
	audit := answers.AuditLog
	if audit == "" {
		audit = "none"
	}
	peers := "none: the node works on its own"
	if len(answers.Peers) > 0 {
		lines := make([]string, len(answers.Peers))
		for i, p := range answers.Peers {
			lines[i] = fmt.Sprintf("%s (trust %s) %s", p.Name, strconv.FormatFloat(p.Weight, 'f', -1, 64), p.Address)
		}
		peers = strings.Join(lines, "\n                   ")
	}
	allow := "only the always protected addresses"
	if len(answers.Allow) > 0 {
		parts := make([]string, len(answers.Allow))
		for i, p := range answers.Allow {
			parts[i] = p.String()
		}
		allow = strings.Join(parts, " ")
	}
	a.printf("\nSummary\n"+
		"  State directory: %s\n"+
		"  Audit log:       %s\n"+
		"  Peers:           %s\n"+
		"  Mode:            %s\n"+
		"  Never blocked:   %s\n\n",
		answers.StateDir, audit, peers, answers.Mode, allow)
}
