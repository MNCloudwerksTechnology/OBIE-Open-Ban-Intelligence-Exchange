package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"
)

// group is the task a command serves. The overview and the manual pages
// list the commands by group, in this order.
type group int

const (
	groupLook group = iota
	groupDecide
	groupReport
	groupManage
)

// groups are the groups in the order they are listed.
var groups = []group{groupLook, groupDecide, groupReport, groupManage}

// groupTitles head the groups in the overview and the manual pages.
var groupTitles = map[group]string{
	groupLook:   "Look: what the node knows and does",
	groupDecide: "Decide: overrule the mesh for one address or range",
	groupReport: "Report: tell your peers about attacks",
	groupManage: "Manage: set up, run and look after the node",
}

// example is a realistic use of a command.
type example struct {
	// what says in a sentence what the command line does.
	what string
	// command is the command line as typed, possibly with sudo in front or
	// at the end of a pipeline.
	command string
}

// commandHelp describes a command once, for its help, the overview, the
// manual page, the shell completion and the CLI reference.
type commandHelp struct {
	name  string
	group group
	// summary is the purpose in one line, lower case and without a final
	// period, as the overview lists it.
	summary string
	// usage are the synopsis lines, each after "<tool> <name> ".
	usage []string
	// description explains the command in paragraphs separated by blank
	// lines.
	description string
	examples    []example
	// values are the fixed values of flags, by flag name, for the shell
	// completion.
	values map[string][]string
}

// tool describes one of the binaries: its purpose, its commands and where a
// newcomer starts.
type tool struct {
	name    string
	summary string
	// usage are the synopsis lines of the tool, each after "<name> ".
	usage []string
	// description explains the tool, for its manual page.
	description string
	// start tells a newcomer where to start, below the overview.
	start    string
	commands []commandHelp
	// globalFlags says whether the tool has flags of its own, given before
	// the command.
	globalFlags bool
	// run runs the tool with args, without signal handling.
	run func(args []string, stdout, stderr io.Writer) int
}

// toolByName returns the tool named name: obiectl or obied.
func toolByName(name string) *tool {
	if name == ctlName {
		return ctlTool()
	}
	return daemonTool()
}

// command returns the command called name.
func (t *tool) command(name string) (commandHelp, bool) {
	i := slices.IndexFunc(t.commands, func(c commandHelp) bool { return c.name == name })
	if i < 0 {
		return commandHelp{}, false
	}
	return t.commands[i], true
}

// names returns the names of the tool's commands.
func (t *tool) names() []string {
	names := make([]string, len(t.commands))
	for i, c := range t.commands {
		names[i] = c.name
	}
	return names
}

// flags returns the flag set of the command called name, or of the tool
// itself for ""; nil if it has none.
func (t *tool) flags(name string) *flag.FlagSet {
	args := []string{"--help"}
	switch {
	case name != "":
		args = []string{name, "--help"}
	case !t.globalFlags:
		return nil
	}
	rec := &flagRecorder{}
	t.run(args, rec, io.Discard)
	return rec.fs
}

// flagRecorder is a standard output that records the flag set of a command
// instead of printing its help. Running a command with --help against it
// yields the command's flags, with their usage and defaults, for the help,
// the manual pages and the shell completion.
type flagRecorder struct{ fs *flag.FlagSet }

func (*flagRecorder) Write(p []byte) (int, error) { return len(p), nil }

// newFlagSet returns the flag set of program ("<tool>" or "<tool>
// <command>"). It prints nothing itself: parseFlags and parse explain
// mistakes and print the help.
func newFlagSet(program string) *flag.FlagSet {
	fs := flag.NewFlagSet(program, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	return fs
}

// writeHelp writes the help of the program whose flag set is fs to w: the
// overview of a tool, or the help of one of its commands.
func writeHelp(w io.Writer, fs *flag.FlagSet) error {
	if rec, ok := w.(*flagRecorder); ok {
		rec.fs = fs
		return nil
	}
	toolName, name, _ := strings.Cut(fs.Name(), " ")
	t := toolByName(toolName)
	var b strings.Builder
	if c, ok := t.command(name); ok {
		t.writeCommandHelp(&b, c, fs)
	} else {
		t.writeOverview(&b)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// writeOverview writes the tool's commands grouped by task, its own flags
// and where to start.
func (t *tool) writeOverview(b *strings.Builder) {
	fmt.Fprintf(b, "%s: %s\n\nUsage:\n", t.name, t.summary)
	for _, u := range t.usage {
		fmt.Fprintf(b, "  %s %s\n", t.name, u)
	}
	width := 0
	for _, c := range t.commands {
		width = max(width, len(c.name))
	}
	for _, g := range groups {
		var lines []string
		for _, c := range t.commands {
			if c.group == g {
				lines = append(lines, fmt.Sprintf("  %-*s  %s\n", width, c.name, c.summary))
			}
		}
		if len(lines) > 0 {
			fmt.Fprintf(b, "\n%s\n%s", groupTitles[g], strings.Join(lines, ""))
		}
	}
	if fs := t.flags(""); fs != nil {
		b.WriteString("\nGlobal flags, before the command:\n")
		writeFlags(b, fs)
	}
	fmt.Fprintf(b, "\n%s\n", t.start)
}

// writeCommandHelp writes the help of command c, whose flag set is fs: its
// purpose, synopsis, description, flags with their defaults and examples.
func (t *tool) writeCommandHelp(b *strings.Builder, c commandHelp, fs *flag.FlagSet) {
	fmt.Fprintf(b, "%s %s: %s\n\nUsage:\n", t.name, c.name, c.summary)
	for _, u := range c.usage {
		fmt.Fprintf(b, "  %s %s %s\n", t.name, c.name, u)
	}
	fmt.Fprintf(b, "\n%s\n", strings.TrimSpace(c.description))
	if hasFlags(fs) {
		b.WriteString("\nFlags:\n")
		writeFlags(b, fs)
	}
	if t.globalFlags {
		fmt.Fprintf(b, "\nThe global flags, such as --socket, go before the command: %s --help.\n", t.name)
	}
	b.WriteString("\nExamples:\n")
	for i, e := range c.examples {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(b, "  %s\n    %s\n", e.what, e.command)
	}
}

func hasFlags(fs *flag.FlagSet) bool {
	n := 0
	if fs != nil {
		fs.VisitAll(func(*flag.Flag) { n++ })
	}
	return n > 0
}

// writeFlags lists every flag of fs, sorted by name, with its argument, its
// usage and its default.
func writeFlags(b *strings.Builder, fs *flag.FlagSet) {
	fs.VisitAll(func(f *flag.Flag) {
		arg, usage := flag.UnquoteUsage(f)
		fmt.Fprintf(b, "  --%s", f.Name)
		if arg != "" {
			fmt.Fprintf(b, " %s", arg)
		}
		b.WriteString("\n")
		for _, line := range wrap(usage+defaultText(f, usage), 72) {
			fmt.Fprintf(b, "      %s\n", line)
		}
	})
}

// wrap breaks text into lines of at most width characters where it can,
// between words.
func wrap(text string, width int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		switch {
		case line == "":
			line = word
		case len(line)+1+len(word) > width:
			lines, line = append(lines, line), word
		default:
			line += " " + word
		}
	}
	return append(lines, line)
}

// defaultText is " (default: …)" for the help of flag f with usage, or ""
// if the usage says itself what happens without the flag.
func defaultText(f *flag.Flag, usage string) string {
	switch {
	case describesDefault(usage), f.Name == "version":
		return ""
	case isBoolFlag(f) && f.DefValue == "false":
		return " (default: off)"
	case isBoolFlag(f):
		return " (default: on)"
	case f.DefValue == "":
		return ""
	}
	return " (default: " + f.DefValue + ")"
}

// describesDefault reports whether a flag's usage says what happens
// without the flag.
func describesDefault(usage string) bool {
	return strings.Contains(usage, "default") || strings.Contains(usage, "(required)")
}

func isBoolFlag(f *flag.Flag) bool {
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}

// parseFlags parses the flags of a command, which may come before, between
// and after its arguments, and returns the arguments; after "--" every
// argument is one. done means the command must exit with code: after
// --help, which printed the help on stdout, or after a usage error,
// explained on stderr.
func parseFlags(fs *flag.FlagSet, args []string, stdout, stderr io.Writer) (positional []string, code int, done bool) {
	for {
		if err := fs.Parse(args); err != nil {
			return nil, flagError(fs, err, stdout, stderr), true
		}
		rest := fs.Args()
		switch {
		case len(rest) == 0:
			return positional, 0, false
		case len(rest) < len(args) && args[len(args)-len(rest)-1] == "--":
			return append(positional, rest...), 0, false
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

// parseNoArgs parses the flags of a command that takes no arguments; see
// parseFlags.
func parseNoArgs(fs *flag.FlagSet, args []string, stdout, stderr io.Writer) (code int, done bool) {
	positional, code, done := parseFlags(fs, args, stdout, stderr)
	if done {
		return code, true
	}
	if len(positional) > 0 {
		usageProblem(fs.Name(), fmt.Sprintf("unexpected argument %q: %s takes no arguments", positional[0], fs.Name())).
			write(stderr, fs.Name())
		return ExitUsage, true
	}
	return 0, false
}

// parseOneArg parses the flags of a command that takes exactly one
// argument, described by what, e.g. "address or range"; see parseFlags.
func parseOneArg(fs *flag.FlagSet, args []string, what string, stdout, stderr io.Writer) (arg string, code int, done bool) {
	positional, code, done := parseFlags(fs, args, stdout, stderr)
	if done {
		return "", code, true
	}
	return oneArg(fs.Name(), positional, what, stderr)
}

// oneArg returns the only one of args, or explains on stderr why there is
// not exactly one.
func oneArg(program string, args []string, what string, stderr io.Writer) (arg string, code int, done bool) {
	switch len(args) {
	case 1:
		return args[0], 0, false
	case 0:
		usageProblem(program, "missing the "+what).write(stderr, program)
	default:
		usageProblem(program, fmt.Sprintf("expects one %s, got %d arguments: %s", what, len(args), strings.Join(args, " "))).
			write(stderr, program)
	}
	return "", ExitUsage, true
}

// flagError answers a failed fs.Parse: with the help on stdout after
// --help, else with the usage error on stderr. It returns the exit code.
func flagError(fs *flag.FlagSet, err error, stdout, stderr io.Writer) int {
	if errors.Is(err, flag.ErrHelp) {
		if err := writeHelp(stdout, fs); err != nil {
			_, _ = fmt.Fprintf(stderr, "%s: writing the help: %v\n", fs.Name(), err)
			return ExitIOError
		}
		return ExitOK
	}
	usageProblem(fs.Name(), flagMistake(fs, err)).write(stderr, fs.Name())
	return ExitUsage
}

// flagMistake rephrases an error of the flag package with the double
// dashes the documentation uses and suggests the flag that was probably
// meant.
func flagMistake(fs *flag.FlagSet, err error) string {
	msg := err.Error()
	if name, ok := strings.CutPrefix(msg, "flag provided but not defined: -"); ok {
		var names []string
		fs.VisitAll(func(f *flag.Flag) { names = append(names, f.Name) })
		msg = "unknown flag --" + name
		if s := closest(name, names); s != "" {
			msg += "; did you mean --" + s + "?"
		}
		return msg
	}
	if name, ok := strings.CutPrefix(msg, "flag needs an argument: -"); ok {
		return "--" + name + " needs a value"
	}
	return strings.Replace(msg, " for flag -", " for --", 1)
}

// closest returns the candidate that name is most likely a typing mistake
// of: the one it is a prefix of, or one at most two edits away; "" if none
// is close enough.
func closest(name string, candidates []string) string {
	best, bestDist := "", 3
	for _, c := range candidates {
		if name != "" && strings.HasPrefix(c, name) {
			return c
		}
		if d := editDistance(name, c); d < bestDist && d < len(c) {
			best, bestDist = c, d
		}
	}
	return best
}

// editDistance is the Levenshtein distance between a and b.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// unknownCommand explains on w that the tool has no command called name,
// followed by the overview.
func (t *tool) unknownCommand(w io.Writer, name string) {
	what := fmt.Sprintf("unknown command %q", name)
	if s := closest(name, t.names()); s != "" {
		what += fmt.Sprintf("; did you mean %q?", s)
	}
	p := problem{id: "unknown-command", what: what,
		next: []string{"choose one of the commands below; help on one: " + t.name + " help <command>"}}
	p.write(w, t.name)
	var b strings.Builder
	b.WriteString("\n")
	t.writeOverview(&b)
	_, _ = io.WriteString(w, b.String())
}

// overview writes the overview to w, for a tool run without a command.
func (t *tool) overview(w io.Writer) {
	var b strings.Builder
	t.writeOverview(&b)
	_, _ = io.WriteString(w, b.String())
}

// runHelp runs "<tool> help [command]": the overview, or the help of the
// command, on stdout.
func (t *tool) runHelp(args []string, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 0:
		var b strings.Builder
		t.writeOverview(&b)
		if _, err := io.WriteString(stdout, b.String()); err != nil {
			_, _ = fmt.Fprintf(stderr, "%s: writing the help: %v\n", t.name, err)
			return ExitIOError
		}
		return ExitOK
	case len(args) > 1:
		usageProblem(t.name+" help", "expects one command, got "+strings.Join(args, " ")).write(stderr, t.name+" help")
		return ExitUsage
	}
	if _, ok := t.command(args[0]); !ok {
		t.unknownCommand(stderr, args[0])
		return ExitUsage
	}
	return t.run([]string{args[0], "--help"}, stdout, stderr)
}
