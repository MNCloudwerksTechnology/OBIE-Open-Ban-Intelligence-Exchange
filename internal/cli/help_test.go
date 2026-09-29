package cli

import (
	"bytes"
	"flag"
	"io"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode"
)

// tools are both binaries, for the checks that apply to every command.
func tools() []*tool { return []*tool{ctlTool(), daemonTool()} }

// TestEveryCommandIsDocumented keeps a command from shipping without help:
// a one-line summary, a task group, a synopsis, a description, at least
// one example that parses with the command's flags, a usage and a default
// for every flag, and --json for every command that lists or shows
// something.
func TestEveryCommandIsDocumented(t *testing.T) {
	for _, tl := range tools() {
		if tl.summary == "" || tl.description == "" || tl.start == "" || len(tl.usage) == 0 {
			t.Errorf("%s: the tool lacks a summary, description, start hint or usage", tl.name)
		}
		seen := map[string]bool{}
		for _, c := range tl.commands {
			name := tl.name + " " + c.name
			t.Run(name, func(t *testing.T) {
				if seen[c.name] {
					t.Errorf("%s is registered twice", name)
				}
				seen[c.name] = true
				checkSummary(t, c.summary)
				if !slices.Contains(groups, c.group) {
					t.Errorf("unknown group %d", c.group)
				}
				if len(c.usage) == 0 || strings.TrimSpace(c.description) == "" {
					t.Errorf("no usage or no description")
				}
				fs := tl.flags(c.name)
				if fs == nil {
					t.Fatalf("running it with --help did not yield its flags")
				}
				checkFlags(t, fs)
				if c.group == groupLook && fs.Lookup("json") == nil {
					t.Errorf("it lists or shows something but has no --json for scripts")
				}
				for flagName := range c.values {
					if fs.Lookup(flagName) == nil {
						t.Errorf("values are given for --%s, which it does not have", flagName)
					}
				}
				if len(c.examples) == 0 {
					t.Errorf("no example")
				}
				for _, e := range c.examples {
					checkExample(t, tl, c.name, e)
				}
			})
		}
	}
}

// checkSummary checks that a summary is one short line in lower case
// without a final period, as the overview lists it.
func checkSummary(t *testing.T, s string) {
	t.Helper()
	switch {
	case s == "":
		t.Errorf("no summary")
	case strings.ContainsAny(s, "\n\t"), len(s) > 76:
		t.Errorf("summary %q is not one line of at most 76 characters", s)
	case strings.HasSuffix(s, "."), unicode.IsUpper(rune(s[0])):
		t.Errorf("summary %q must start in lower case and end without a period", s)
	}
}

// checkFlags checks that every flag has a usage and says its default.
func checkFlags(t *testing.T, fs *flag.FlagSet) {
	t.Helper()
	fs.VisitAll(func(f *flag.Flag) {
		_, usage := flag.UnquoteUsage(f)
		switch {
		case strings.TrimSpace(usage) == "":
			t.Errorf("--%s has no usage", f.Name)
		case f.DefValue == "" && !describesDefault(usage):
			t.Errorf("--%s: the usage %q must say what happens without it, e.g. (default: none)", f.Name, usage)
		}
	})
}

// checkExample checks that an example says what it does and that its
// command line runs the command with flags it has.
func checkExample(t *testing.T, tl *tool, name string, e example) {
	t.Helper()
	if !strings.HasSuffix(e.what, ":") || unicode.IsLower(rune(e.what[0])) {
		t.Errorf("example %q: the sentence must start in upper case and end with a colon", e.what)
	}
	args, ok := exampleArgs(tl.name, name, e.command)
	if !ok {
		t.Errorf("example %q does not run %s %s", e.command, tl.name, name)
		return
	}
	var stderr bytes.Buffer
	if _, code, done := parseFlags(tl.flags(name), args, io.Discard, &stderr); done {
		t.Errorf("example %q does not parse (exit code %d): %s", e.command, code, stderr.String())
	}
}

// exampleArgs returns the arguments after "<tool> <command>" in the
// segment of a command line (split at |, && and ;) that runs the command.
func exampleArgs(toolName, name, line string) ([]string, bool) {
	var segments [][]string
	segment := []string{}
	for _, word := range shellWords(line) {
		if word == "|" || word == "&&" || word == ";" {
			segments, segment = append(segments, segment), []string{}
			continue
		}
		segment = append(segment, word)
	}
	for _, s := range append(segments, segment) {
		for i := 0; i+1 < len(s); i++ {
			if s[i] == toolName && s[i+1] == name {
				return s[i+2:], true
			}
		}
	}
	return nil, false
}

// shellWords splits a command line into words like a shell, for the
// quoting the examples use: single and double quotes, no escapes.
func shellWords(line string) []string {
	var words []string
	var word strings.Builder
	inWord := false
	var quote rune
	for _, r := range line {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			word.WriteRune(r)
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == ' ':
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	if inWord {
		words = append(words, word.String())
	}
	return words
}

func TestShellWordsAndExampleArgs(t *testing.T) {
	got := shellWords(`grep 'a b' log | sudo obiectl report --note "x y" 203.0.113.7`)
	want := []string{"grep", "a b", "log", "|", "sudo", "obiectl", "report", "--note", "x y", "203.0.113.7"}
	if !slices.Equal(got, want) {
		t.Errorf("shellWords = %q, want %q", got, want)
	}
	for line, want := range map[string][]string{
		"sudo obiectl status --json":                                     nil,
		"grep x | sudo obiectl report --ip 203.0.113.7":                  {"--ip", "203.0.113.7"},
		"sudo systemctl stop obiectl && sudo obiectl report 203.0.113.7": {"203.0.113.7"},
		"sudo obiectl report 203.0.113.7 | jq .":                         {"203.0.113.7"},
		"sudo obiectl status; sudo obiectl report":                       {},
	} {
		args, ok := exampleArgs("obiectl", "report", line)
		if ok != (want != nil) || !slices.Equal(args, want) {
			t.Errorf("%q: args = %q, %v; want %q", line, args, ok, want)
		}
	}
}

// TestHelpOfEveryCommand checks the help as people see it: on standard
// output with exit status 0, with the summary, synopsis, every flag with
// its default, the examples, and no color.
func TestHelpOfEveryCommand(t *testing.T) {
	for _, tl := range tools() {
		for _, c := range tl.commands {
			for _, args := range [][]string{{c.name, "--help"}, {c.name, "-h"}, {"help", c.name}} {
				var stdout, stderr bytes.Buffer
				if code := tl.run(args, &stdout, &stderr); code != ExitOK || stderr.Len() > 0 {
					t.Errorf("%s %v: exit code %d, stderr %q", tl.name, args, code, stderr.String())
					continue
				}
				help := stdout.String()
				wants := []string{tl.name + " " + c.name + ": " + c.summary + "\n", "\nUsage:\n", "\nExamples:\n", c.examples[0].command}
				tl.flags(c.name).VisitAll(func(f *flag.Flag) { wants = append(wants, "  --"+f.Name) })
				for _, want := range wants {
					if !strings.Contains(help, want) {
						t.Errorf("%s %v: the help lacks %q:\n%s", tl.name, args, want, help)
					}
				}
				if strings.ContainsRune(help, '\x1b') {
					t.Errorf("%s %v: the help contains escape sequences", tl.name, args)
				}
				for _, line := range strings.Split(help, "\n") {
					if len(line) > 100 && !strings.HasPrefix(line, "    ") {
						t.Errorf("%s %v: line longer than 100 characters: %q", tl.name, args, line)
					}
				}
			}
		}
	}
}

// TestOverviewGroupsCommandsByTask checks what a tool prints without a
// command, with an unknown one and for help: every command under its
// group, in the order look, decide, report, manage, and where to start.
func TestOverviewGroupsCommandsByTask(t *testing.T) {
	for _, tl := range tools() {
		for _, tc := range []struct {
			args         []string
			code         int
			onStdout     bool
			errorOutline string
		}{
			{nil, ExitUsage, false, ""},
			{[]string{"--help"}, ExitOK, true, ""},
			{[]string{"help"}, ExitOK, true, ""},
			{[]string{"frobnicate"}, ExitUsage, false, tl.name + `: unknown command "frobnicate"` + "\n  Next: choose one of the commands below"},
		} {
			var stdout, stderr bytes.Buffer
			code := tl.run(tc.args, &stdout, &stderr)
			out, other := stderr.String(), stdout.String()
			if tc.onStdout {
				out, other = other, out
			}
			if code != tc.code || other != "" {
				t.Errorf("%s %v: exit code %d, other stream %q", tl.name, tc.args, code, other)
			}
			if !strings.HasPrefix(out, tc.errorOutline) {
				t.Errorf("%s %v: output does not start with %q:\n%s", tl.name, tc.args, tc.errorOutline, out)
			}
			last := -1
			for _, g := range groups {
				for _, c := range tl.commands {
					if c.group != g {
						continue
					}
					line := "\n  " + c.name + " "
					i := strings.Index(out, line)
					if i < last || i < strings.Index(out, "\n"+groupTitles[g]+"\n") {
						t.Errorf("%s %v: %s is not listed under %q in order:\n%s", tl.name, tc.args, c.name, groupTitles[g], out)
					}
					last = i
				}
			}
			if !strings.Contains(out, "\nStart here: sudo ") || !strings.Contains(out, "Help on a command: "+tl.name+" help <command>") {
				t.Errorf("%s %v: the overview does not say where to start:\n%s", tl.name, tc.args, out)
			}
			for _, line := range strings.Split(out, "\n") {
				if len(line) > 100 {
					t.Errorf("%s %v: line longer than 100 characters: %q", tl.name, tc.args, line)
				}
			}
		}
	}
}

func TestClosest(t *testing.T) {
	names := []string{"status", "peers", "decisions", "indicators", "unoverride"}
	for name, want := range map[string]string{
		"stauts": "status", "stat": "status", "peer": "peers", "decision": "decisions", "indicator": "indicators",
		"unoveride": "unoverride", "frobnicate": "", "": "", "xy": "",
	} {
		if got := closest(name, names); got != want {
			t.Errorf("closest(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestParseFlagsStopsAtDoubleDash(t *testing.T) {
	fs := newFlagSet("obiectl test")
	asJSON := fs.Bool("json", false, "")
	args, code, done := parseFlags(fs, []string{"a", "--json", "--", "-b", "--json"}, io.Discard, io.Discard)
	if done || code != 0 || !*asJSON || !slices.Equal(args, []string{"a", "-b", "--json"}) {
		t.Errorf("args = %q, json %v, code %d, done %v", args, *asJSON, code, done)
	}
}

func TestHelpWriteError(t *testing.T) {
	var stderr bytes.Buffer
	if code := RunCtl([]string{"status", "--help"}, failingWriter{}, &stderr); code != ExitIOError ||
		!strings.Contains(stderr.String(), "writing the help") {
		t.Errorf("exit code %d, stderr %q", code, stderr.String())
	}
	stderr.Reset()
	if code := RunCtl([]string{"help"}, failingWriter{}, &stderr); code != ExitIOError {
		t.Errorf("help: exit code %d, stderr %q", code, stderr.String())
	}
}

// discouraged are words the help must not use: the glossary
// (documentation/glossary.md) names these things differently.
var discouraged = regexp.MustCompile(`(?i)\b(white|black)[ -]?list|\bban ?list|\bIOCs?\b|\bthreat score`)

// TestHelpUsesGlossaryTerms checks the help of every command against the
// glossary: the allow-list is written with a hyphen unless a configuration
// key (allowlist.cidrs) is meant, and no synonym replaces a glossary term.
func TestHelpUsesGlossaryTerms(t *testing.T) {
	for _, tl := range tools() {
		help := func(args ...string) string {
			var stdout bytes.Buffer
			tl.run(args, &stdout, io.Discard)
			return stdout.String()
		}
		texts := []string{help("help")}
		for _, c := range tl.commands {
			texts = append(texts, help(c.name, "--help"))
		}
		for _, text := range texts {
			if m := discouraged.FindString(text); m != "" {
				t.Errorf("%s help uses %q, which the glossary calls differently:\n%s", tl.name, m, text)
			}
			for i := strings.Index(text, "allowlist"); i >= 0; {
				if end := i + len("allowlist"); end >= len(text) || text[end] != '.' {
					t.Errorf("%s help writes %q; the glossary term is allow-list (allowlist.<key> for a setting)", tl.name, text[max(0, i-20):min(len(text), end+20)])
				}
				next := strings.Index(text[i+1:], "allowlist")
				if next < 0 {
					break
				}
				i += 1 + next
			}
		}
	}
}
