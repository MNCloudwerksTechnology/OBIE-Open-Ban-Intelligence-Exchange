package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"
)

// Shells are the shells that completion scripts are written for.
var Shells = []string{"bash", "zsh", "fish"}

// ToolNames are the binaries that have help, manual pages and completion.
var ToolNames = []string{daemonName, ctlName}

// errUnknownShell reports a shell without a completion script.
var errUnknownShell = errors.New("unknown shell")

// completionHelp describes the completion command of the tool called name.
func completionHelp(name string) commandHelp {
	return commandHelp{
		name: "completion", group: groupManage,
		summary: "print the shell completion script for bash, zsh or fish",
		usage:   []string{"bash | zsh | fish"},
		description: `Prints a script that lets your shell complete ` + name + `'s commands, flags
and flag values when you press Tab. The release downloads ship these
scripts and install.sh installs them; this command is for another shell
setup or a place install.sh does not cover.`,
		examples: []example{
			{"Complete in bash, for your user:", name + " completion bash > ~/.local/share/bash-completion/completions/" + name},
			{"Complete in fish, for your user:", name + " completion fish > ~/.config/fish/completions/" + name + ".fish"},
		},
	}
}

// runCompletion runs "<tool> completion <shell>".
func runCompletion(toolName string, args []string, stdout, stderr io.Writer) int {
	program := toolName + " completion"
	fs := newFlagSet(program)
	shell, code, done := parseOneArg(fs, args, "shell: bash, zsh or fish", stdout, stderr)
	if done {
		return code
	}
	err := WriteCompletion(stdout, toolName, shell)
	switch {
	case errors.Is(err, errUnknownShell):
		usageProblem(program, fmt.Sprintf("unknown shell %q: want bash, zsh or fish", shell)).write(stderr, program)
		return ExitUsage
	case err != nil:
		_, _ = fmt.Fprintf(stderr, "%s: writing the script: %v\n", program, err)
		return ExitIOError
	}
	return ExitOK
}

// completionFlag is a flag as the completion offers it.
type completionFlag struct {
	name, usage string
	// takesValue is false for a switch such as --json.
	takesValue bool
	// values are its fixed values; kind is "file", "directory" or "user"
	// for a value the shell can complete itself, else "".
	values []string
	kind   string
	// repeatable is true for a flag that may be given more than once.
	repeatable bool
}

// completionCommand is a command as the completion offers it: its flags,
// and the fixed values of its argument or else what the argument is.
type completionCommand struct {
	name, summary string
	flags         []completionFlag
	args          []string
	operand       string
}

// completionSpec is what the completion of a tool offers: the flags before
// the command and the commands.
type completionSpec struct {
	tool     string
	global   []completionFlag
	commands []completionCommand
	// flagsOnly is true if the tool's own flags stand in place of a
	// command, as obied's run the node: a command is then only offered as
	// the first word.
	flagsOnly bool
}

// newCompletionSpec reads the completion of t from its registry and from
// the flags of every command.
func newCompletionSpec(t *tool) completionSpec {
	s := completionSpec{tool: t.name, global: completionFlags(t.flags(""), nil), flagsOnly: t.bareCommand != "" && !t.globalFlags}
	s.global = append(s.global, completionFlag{name: "help", usage: "show the commands and where to start"})
	for _, c := range t.commands {
		cc := completionCommand{name: c.name, summary: c.summary, flags: completionFlags(t.flags(c.name), c.values), operand: c.operand}
		cc.flags = append(cc.flags, completionFlag{name: "help", usage: "show the help of the command"})
		if c.name == "completion" {
			cc.args = Shells
		}
		s.commands = append(s.commands, cc)
	}
	s.commands = append(s.commands, completionCommand{name: "help", summary: "show the help of a command", args: t.names()})
	return s
}

// completionFlags returns the flags of fs, with values for fixed ones.
func completionFlags(fs *flag.FlagSet, values map[string][]string) []completionFlag {
	var flags []completionFlag
	if fs == nil {
		return flags
	}
	fs.VisitAll(func(f *flag.Flag) {
		arg, usage := flag.UnquoteUsage(f)
		_, repeatable := f.Value.(*repeatable)
		cf := completionFlag{name: f.Name, usage: shortUsage(usage), takesValue: !isBoolFlag(f), values: values[f.Name], repeatable: repeatable}
		switch arg {
		case "file", "socket":
			cf.kind = "file"
		case "directory":
			cf.kind = "directory"
		case "user":
			cf.kind = "user"
		}
		flags = append(flags, cf)
	})
	return flags
}

// shortUsage is the usage of a flag up to its first aside, for the
// descriptions shells show next to a flag.
func shortUsage(usage string) string {
	for _, sep := range []string{"; ", " (", ", e.g."} {
		usage, _, _ = strings.Cut(usage, sep)
	}
	return usage
}

// commandNames returns the names of the commands of s.
func (s completionSpec) commandNames() []string {
	names := make([]string, len(s.commands))
	for i, c := range s.commands {
		names[i] = c.name
	}
	return names
}

// WriteCompletion writes the completion script of the tool called toolName
// (obied or obiectl) for shell (bash, zsh or fish) to w.
func WriteCompletion(w io.Writer, toolName, shell string) error {
	if !slices.Contains(ToolNames, toolName) {
		return fmt.Errorf("unknown tool %q", toolName)
	}
	s := newCompletionSpec(toolByName(toolName))
	var b strings.Builder
	switch shell {
	case "bash":
		s.writeBash(&b)
	case "zsh":
		s.writeZsh(&b)
	case "fish":
		s.writeFish(&b)
	default:
		return fmt.Errorf("%w %q", errUnknownShell, shell)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// flagWords returns --name for every flag.
func flagWords(flags []completionFlag) string {
	words := make([]string, len(flags))
	for i, f := range flags {
		words[i] = "--" + f.name
	}
	return strings.Join(words, " ")
}

// writeBash writes the bash completion: the command is the first word that
// is neither a flag nor the value of one.
func (s completionSpec) writeBash(b *strings.Builder) {
	fn := "_" + s.tool
	var valueFlags []string
	for _, f := range s.global {
		if f.takesValue {
			valueFlags = append(valueFlags, "--"+f.name)
		}
	}
	fmt.Fprintf(b, "# bash completion for %s; generated by: %s completion bash\n\n", s.tool, s.tool)
	fmt.Fprintf(b, "# %s_reply offers the words $1 for a word that starts with -, else the words $2.\n", fn)
	fmt.Fprintf(b, "%s_reply() {\n\tif [[ $cur == -* ]]; then\n\t\tmapfile -t COMPREPLY < <(compgen -W \"$1\" -- \"$cur\")\n"+
		"\telse\n\t\tmapfile -t COMPREPLY < <(compgen -W \"$2\" -- \"$cur\")\n\tfi\n}\n\n", fn)
	fmt.Fprintf(b, "# %s_value completes the value of the flag in $prev and returns 0, or returns 1\n# if $prev is not a flag that takes a value.\n", fn)
	fmt.Fprintf(b, "%s_value() {\n\tcase $cmd:$prev in\n", fn)
	writeBashValues(b, "", s.global)
	for _, c := range s.commands {
		writeBashValues(b, c.name, c.flags)
	}
	b.WriteString("\t*) return 1 ;;\n\tesac\n\treturn 0\n}\n\n")
	fmt.Fprintf(b, "%s() {\n\tlocal cur prev cmd i\n\tcur=${COMP_WORDS[COMP_CWORD]}\n\tprev=${COMP_WORDS[COMP_CWORD-1]}\n", fn)
	// bash splits --flag=value into the words --flag, = and value.
	b.WriteString("\t# bash splits --flag=value into the words --flag, = and value.\n\tif [[ $cur == = ]]; then\n\t\tcur=\n" +
		"\telif [[ $prev == = ]]; then\n\t\tprev=${COMP_WORDS[COMP_CWORD-2]}\n\tfi\n\tcmd=\n\ti=1\n")
	b.WriteString("\twhile [ \"$i\" -lt \"$COMP_CWORD\" ]; do\n\t\tcase ${COMP_WORDS[i]} in\n")
	if len(valueFlags) > 0 {
		fmt.Fprintf(b, "\t\t%s) [[ ${COMP_WORDS[i+1]} == = ]] || i=$((i + 1)) ;;\n", strings.Join(valueFlags, " | "))
	}
	b.WriteString("\t\t=) i=$((i + 1)) ;;\n\t\t-*) ;;\n\t\t*)\n\t\t\tcmd=${COMP_WORDS[i]}\n\t\t\tbreak\n\t\t\t;;\n\t\tesac\n\t\ti=$((i + 1))\n\tdone\n")
	fmt.Fprintf(b, "\t%s_value && return\n\tcase $cmd in\n", fn)
	if s.flagsOnly {
		fmt.Fprintf(b, "\t\"\") if [ \"$COMP_CWORD\" -eq 1 ]; then %s_reply %q %q; else %s_reply %q \"\"; fi ;;\n",
			fn, flagWords(s.global), strings.Join(s.commandNames(), " "), fn, flagWords(s.global))
	} else {
		fmt.Fprintf(b, "\t\"\") %s_reply %q %q ;;\n", fn, flagWords(s.global), strings.Join(s.commandNames(), " "))
	}
	for _, c := range s.commands {
		fmt.Fprintf(b, "\t%s) %s_reply %q %q ;;\n", c.name, fn, flagWords(c.flags), strings.Join(c.args, " "))
	}
	b.WriteString("\t*) COMPREPLY=() ;;\n\tesac\n}\n\n")
	fmt.Fprintf(b, "complete -F %s %s\n", fn, s.tool)
}

// writeBashValues writes the cases of the value completion for the flags
// of command, "" for the flags before the command.
func writeBashValues(b *strings.Builder, command string, flags []completionFlag) {
	for _, f := range flags {
		if !f.takesValue {
			continue
		}
		reply := "COMPREPLY=()"
		switch {
		case len(f.values) > 0:
			reply = fmt.Sprintf("mapfile -t COMPREPLY < <(compgen -W %q -- \"$cur\")", strings.Join(f.values, " "))
		case f.kind == "file":
			reply = "compopt -o filenames 2>/dev/null; mapfile -t COMPREPLY < <(compgen -f -- \"$cur\")"
		case f.kind == "directory":
			reply = "compopt -o filenames 2>/dev/null; mapfile -t COMPREPLY < <(compgen -d -- \"$cur\")"
		case f.kind == "user":
			reply = "mapfile -t COMPREPLY < <(compgen -u -- \"$cur\")"
		}
		fmt.Fprintf(b, "\t%s:--%s) %s ;;\n", command, f.name, reply)
	}
}

// zshQuote quotes s for a single-quoted zsh word.
func zshQuote(s string) string { return strings.ReplaceAll(s, "'", `'\''`) }

// zshSpec is the _arguments specification of flag f. exclude lists what
// is no longer offered once f is given, e.g. "(1 *)" for the arguments;
// "" for nothing.
func zshSpec(f completionFlag, exclude string) string {
	desc := strings.NewReplacer("[", `\[`, "]", `\]`).Replace(zshQuote(f.usage))
	if !f.takesValue {
		return fmt.Sprintf("'%s--%s[%s]'", exclude, f.name, desc)
	}
	action := " "
	switch {
	case len(f.values) > 0:
		action = "(" + strings.Join(f.values, " ") + ")"
	case f.kind == "file":
		action = "_files"
	case f.kind == "directory":
		action = "_files -/"
	case f.kind == "user":
		action = "_users"
	}
	repeat := ""
	if f.repeatable {
		repeat = "*"
	}
	return fmt.Sprintf("'%s%s--%s=[%s]:%s:%s'", exclude, repeat, f.name, desc, f.name, action)
}

// writeZsh writes the zsh completion, a function for $fpath that also
// works when sourced.
func (s completionSpec) writeZsh(b *strings.Builder) {
	fn := "_" + s.tool
	fmt.Fprintf(b, "#compdef %s\n# zsh completion for %s; generated by: %s completion zsh\n\n", s.tool, s.tool, s.tool)
	fmt.Fprintf(b, "%s() {\n\tlocal -a commands\n\tcommands=(\n", fn)
	for _, c := range s.commands {
		fmt.Fprintf(b, "\t\t'%s:%s'\n", c.name, zshQuote(c.summary))
	}
	b.WriteString("\t)\n\tlocal curcontext=$curcontext state line\n\t_arguments -C \\\n")
	exclude := ""
	if s.flagsOnly {
		exclude = "(1 *)"
	}
	for _, f := range s.global {
		fmt.Fprintf(b, "\t\t%s \\\n", zshSpec(f, exclude))
	}
	b.WriteString("\t\t'1: :->command' \\\n\t\t'*:: :->argument'\n")
	fmt.Fprintf(b, "\tcase $state in\n\tcommand) _describe -t commands '%s command' commands ;;\n\targument)\n\t\tcase $words[1] in\n", s.tool)
	for _, c := range s.commands {
		switch c.name {
		case "help":
			fmt.Fprintf(b, "\t\thelp) _describe -t commands '%s command' commands ;;\n", s.tool)
			continue
		}
		fmt.Fprintf(b, "\t\t%s)\n\t\t\t_arguments", c.name)
		for _, f := range c.flags {
			fmt.Fprintf(b, " \\\n\t\t\t\t%s", zshSpec(f, ""))
		}
		switch {
		case len(c.args) > 0:
			fmt.Fprintf(b, " \\\n\t\t\t\t'1:%s:(%s)'", c.name, strings.Join(c.args, " "))
		case c.operand != "":
			fmt.Fprintf(b, " \\\n\t\t\t\t'1:%s: '", zshQuote(c.operand))
		}
		b.WriteString("\n\t\t\t;;\n")
	}
	b.WriteString("\t\tesac\n\t\t;;\n\tesac\n}\n\n")
	fmt.Fprintf(b, "if [ \"$funcstack[1]\" = %q ]; then\n\t%s \"$@\"\nelse\n\tcompdef %s %s\nfi\n", fn, fn, fn, s.tool)
}

// fishQuote quotes s as a single-quoted fish word.
func fishQuote(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, "'", `\'`).Replace(s) + "'"
}

// writeFish writes the fish completion: the command is the first word that
// is neither a flag nor the value of one.
func (s completionSpec) writeFish(b *strings.Builder) {
	fn := "__" + s.tool + "_command"
	var valueFlags []string
	for _, f := range s.global {
		if f.takesValue {
			valueFlags = append(valueFlags, "--"+f.name)
		}
	}
	fmt.Fprintf(b, "# fish completion for %s; generated by: %s completion fish\n\n", s.tool, s.tool)
	fmt.Fprintf(b, "# %s prints the command on the command line and fails if there is none yet.\n", fn)
	fmt.Fprintf(b, "function %s\n\tset -l words (commandline -opc)\n\tset -e words[1]\n\twhile set -q words[1]\n\t\tswitch $words[1]\n", fn)
	if len(valueFlags) > 0 {
		fmt.Fprintf(b, "\t\t\tcase %s\n\t\t\t\tset -e words[1]\n", strings.Join(valueFlags, " "))
	}
	b.WriteString("\t\t\tcase '-*'\n\t\t\tcase '*'\n\t\t\t\techo $words[1]\n\t\t\t\treturn 0\n\t\tend\n\t\tset -e words[1]\n\tend\n\treturn 1\nend\n\n")
	fmt.Fprintf(b, "# %s_is succeeds if the command on the command line is $argv[1].\n", fn)
	fmt.Fprintf(b, "function %s_is\n\tset -l command (%s)\n\tand test \"$command\" = $argv[1]\nend\n\n", fn, fn)
	fmt.Fprintf(b, "complete -c %s -f\n", s.tool)
	none := fishQuote("not " + fn + " >/dev/null")
	writeFishFlags(b, s.tool, none, s.global)
	commandCond := none
	if s.flagsOnly {
		commandCond = fishQuote("test (count (commandline -opc)) -eq 1")
	}
	for _, c := range s.commands {
		fmt.Fprintf(b, "complete -c %s -n %s -a %s -d %s\n", s.tool, commandCond, c.name, fishQuote(c.summary))
	}
	for _, c := range s.commands {
		cond := fishQuote(fn + "_is " + c.name)
		writeFishFlags(b, s.tool, cond, c.flags)
		if len(c.args) > 0 {
			fmt.Fprintf(b, "complete -c %s -n %s -x -a %s\n", s.tool, cond, fishQuote(strings.Join(c.args, " ")))
		}
	}
}

// writeFishFlags writes the completion of flags under the condition cond.
func writeFishFlags(b *strings.Builder, tool, cond string, flags []completionFlag) {
	for _, f := range flags {
		fmt.Fprintf(b, "complete -c %s -n %s -l %s", tool, cond, f.name)
		switch {
		case !f.takesValue:
		case len(f.values) > 0:
			fmt.Fprintf(b, " -x -a %s", fishQuote(strings.Join(f.values, " ")))
		case f.kind == "file" || f.kind == "directory":
			b.WriteString(" -r -F")
		case f.kind == "user":
			b.WriteString(" -x -a '(__fish_complete_users)'")
		default:
			b.WriteString(" -x")
		}
		fmt.Fprintf(b, " -d %s\n", fishQuote(f.usage))
	}
}
