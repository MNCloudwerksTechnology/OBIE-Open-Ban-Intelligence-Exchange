package cli

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// writeScript writes the completion of tool for shell into a temporary
// file and returns its path.
func writeScript(t *testing.T, tool, shell string) string {
	t.Helper()
	var b bytes.Buffer
	if code := toolByName(tool).run([]string{"completion", shell}, &b, os.Stderr); code != ExitOK {
		t.Fatalf("%s completion %s: exit code %d", tool, shell, code)
	}
	path := filepath.Join(t.TempDir(), tool+"."+shell)
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestBashCompletion sources the bash completion and completes command
// lines as bash would on Tab.
func TestBashCompletion(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not installed")
	}
	for _, tc := range []struct {
		tool  string
		words []string
		want  string
	}{
		{"obiectl", []string{"dec"}, "decisions"},
		{"obiectl", []string{""}, "status peers identity decisions explain indicators show overrides enforced allow block unoverride report revoke console completion help"},
		{"obiectl", []string{"--"}, "--socket --timeout --version --help"},
		{"obiectl", []string{"--socket", "/run/obie/obie.sock", "st"}, "status"},
		{"obiectl", []string{"--timeout", "5s", "decisions", "--st"}, "--state"},
		{"obiectl", []string{"decisions", "--state", ""}, "block none allowed"},
		{"obiectl", []string{"decisions", "--state", "b"}, "block"},
		{"obiectl", []string{"report", "--action", ""}, "ban watch"},
		{"obiectl", []string{"report", "--protocol", ""}, ""},
		{"obiectl", []string{"help", "unov"}, "unoverride"},
		{"obiectl", []string{"completion", "z"}, "zsh"},
		{"obiectl", []string{"explain", ""}, ""},
		{"obied", []string{"se"}, "self-check setup"},
		{"obied", []string{"setup", "--mode", ""}, "observe enforce"},
		{"obied", []string{"self-check", "--j"}, "--json"},
		{"obied", []string{"help", "tear"}, "teardown-firewall"},
		// bash splits --flag=value into the words --flag, = and value.
		{"obiectl", []string{"--socket", "=", "/run/obie/obie.sock", "st"}, "status"},
		{"obiectl", []string{"decisions", "--state", "="}, "block none allowed"},
		{"obiectl", []string{"decisions", "--state", "=", "al"}, "allowed"},
		{"obiectl", []string{"--timeout", "=", "5s", "decisions", "--json", "=", "true", "--st"}, "--state"},
		// Flags in place of a command run the node.
		{"obied", []string{"--"}, "--check-config --config --version --help"},
		{"obied", []string{"--config", "/etc/obie/obie.yaml", "--ch"}, "--check-config"},
		// After them no command may follow.
		{"obied", []string{"--config", "/etc/obie/obie.yaml", ""}, ""},
		{"obied", []string{"--config=/etc/obie/obie.yaml", "se"}, ""},
		{"obied", []string{"se"}, "self-check setup"},
	} {
		script := writeScript(t, tc.tool, "bash")
		words := append([]string{tc.tool}, tc.words...)
		quoted := make([]string, len(words))
		for i, w := range words {
			quoted[i] = "'" + w + "'"
		}
		prog := "source " + script + "\nCOMP_WORDS=(" + strings.Join(quoted, " ") + ")\nCOMP_CWORD=" +
			strconv.Itoa(len(words)-1) + "\n_" + tc.tool + "\necho \"${COMPREPLY[*]}\"\n"
		out, err := exec.Command("bash", "--norc", "--noprofile", "-c", prog).CombinedOutput() // #nosec G204 -- a fixed test program.
		if err != nil {
			t.Errorf("%v: %v\n%s", words, err, out)
			continue
		}
		if got := strings.TrimSpace(string(out)); got != tc.want {
			t.Errorf("%v: completions %q, want %q", words, got, tc.want)
		}
	}
}

// TestZshCompletion checks that the zsh completion is valid zsh and
// registers itself for the tool when sourced.
func TestZshCompletion(t *testing.T) {
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh is not installed")
	}
	for _, tool := range ToolNames {
		script := writeScript(t, tool, "zsh")
		prog := "autoload -U compinit && compinit -u -D && source " + script + " && print -r -- $_comps[" + tool + "]"
		cmd := exec.Command("zsh", "-f", "-c", prog) // #nosec G204 -- a fixed test program.
		cmd.Env = append(os.Environ(), "HOME="+t.TempDir(), "ZDOTDIR="+t.TempDir())
		out, err := cmd.CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != "_"+tool {
			t.Errorf("%s: %v\n%s", tool, err, out)
		}
	}
}

// TestCompletionOfArgumentsAndRepeatedFlags checks the scripts of the shells
// that are not driven here: zsh completes flags after a command's argument
// and offers a flag again that may be repeated; fish finds the command past
// the values of the global flags.
func TestCompletionOfArgumentsAndRepeatedFlags(t *testing.T) {
	var zshCtl, zshDaemon, fish bytes.Buffer
	for w, args := range map[*bytes.Buffer][]string{&zshCtl: {"obiectl", "zsh"}, &zshDaemon: {"obied", "zsh"}, &fish: {"obiectl", "fish"}} {
		if err := WriteCompletion(w, args[0], args[1]); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []string{"\t\tblock)\n\t\t\t_arguments \\\n", "'1:address or range: '\n", "'1:event ID, address or range: '\n", "'1:completion:(bash zsh fish)'"} {
		if !strings.Contains(zshCtl.String(), want) {
			t.Errorf("obiectl zsh completion lacks %q", want)
		}
	}
	for _, want := range []string{"'*--peer=[", "'*--allow=[", "'--config=[path to the YAML configuration file]:config:_files'",
		"'(1 *)--config=[path to the YAML configuration file]:config:_files' \\\n"} {
		if !strings.Contains(zshDaemon.String(), want) {
			t.Errorf("obied zsh completion lacks %q", want)
		}
	}
	for _, want := range []string{"\t\t\tcase --socket --timeout\n\t\t\t\tset -e words[1]\n",
		"complete -c obiectl -n 'not __obiectl_command >/dev/null' -a status ",
		"complete -c obiectl -n '__obiectl_command_is decisions' -l state -x -a 'block none allowed' "} {
		if !strings.Contains(fish.String(), want) {
			t.Errorf("obiectl fish completion lacks %q", want)
		}
	}
	// obied offers its commands only as the first word, not after the
	// flags that run the node.
	var fishDaemon bytes.Buffer
	if err := WriteCompletion(&fishDaemon, "obied", "fish"); err != nil {
		t.Fatal(err)
	}
	if want := "complete -c obied -n 'test (count (commandline -opc)) -eq 1' -a run "; !strings.Contains(fishDaemon.String(), want) {
		t.Errorf("obied fish completion lacks %q", want)
	}
}

// TestCompletionScriptsOfferEveryCommandAndFlag checks every script, also
// of shells not installed here, for every command and flag.
func TestCompletionScriptsOfferEveryCommandAndFlag(t *testing.T) {
	for _, tool := range ToolNames {
		tl := toolByName(tool)
		for _, shell := range Shells {
			var b bytes.Buffer
			if err := WriteCompletion(&b, tool, shell); err != nil {
				t.Fatal(err)
			}
			script := b.String()
			for _, c := range tl.commands {
				if !strings.Contains(script, c.name) {
					t.Errorf("%s %s: no command %s", tool, shell, c.name)
				}
				tl.flags(c.name).VisitAll(func(f *flag.Flag) {
					want := "--" + f.Name
					if shell == "fish" {
						want = "-l " + f.Name
					}
					if !strings.Contains(script, want) {
						t.Errorf("%s %s: no flag %s of %s", tool, shell, want, c.name)
					}
				})
			}
			if strings.ContainsRune(script, '\x1b') {
				t.Errorf("%s %s: escape sequence", tool, shell)
			}
		}
	}
	if err := WriteCompletion(&bytes.Buffer{}, "obiectl", "tcsh"); err == nil {
		t.Error("tcsh: no error")
	}
	if err := WriteCompletion(&bytes.Buffer{}, "nft", "bash"); err == nil {
		t.Error("unknown tool: no error")
	}
}

func TestCompletionCommand(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		code   int
		stderr string
	}{
		{[]string{"completion", "tcsh"}, ExitUsage, `unknown shell "tcsh": want bash, zsh or fish`},
		{[]string{"completion"}, ExitUsage, "missing the shell: bash, zsh or fish"},
		{[]string{"completion", "bash", "zsh"}, ExitUsage, "expects one shell"},
	} {
		var stdout, stderr bytes.Buffer
		if code := RunCtl(tc.args, &stdout, &stderr); code != tc.code || !strings.Contains(stderr.String(), tc.stderr) || stdout.Len() > 0 {
			t.Errorf("%v: exit code %d, stdout %q, stderr %q", tc.args, code, stdout.String(), stderr.String())
		}
	}
	var stderr bytes.Buffer
	if code := RunCtl([]string{"completion", "fish"}, failingWriter{}, &stderr); code != ExitIOError {
		t.Errorf("write error: exit code %d, stderr %q", code, stderr.String())
	}
}
