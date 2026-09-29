package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

// ReferencePath is where the command-line reference is kept, relative to
// the repository root.
const ReferencePath = "documentation/operations/cli.md"

// mdText escapes the characters of s that Markdown would read as markup:
// the help writes <command> and [,name=NAME] as plain text.
var mdText = strings.NewReplacer(`\`, `\\`, "<", `\<`, ">", `\>`, "[", `\[`, "]", `\]`, "*", `\*`).Replace

// mdCell escapes s for a cell of a Markdown table.
func mdCell(s string) string { return strings.ReplaceAll(mdText(s), "|", `\|`) }

// mdAnchor is the anchor GitHub gives a heading of simple words.
func mdAnchor(heading string) string {
	return strings.ToLower(strings.ReplaceAll(heading, " ", "-"))
}

// WriteReference writes the command-line reference of both tools, in
// Markdown, to w: every command with its synopsis, description, flags and
// examples, as their help shows it.
func WriteReference(w io.Writer) error {
	var b strings.Builder
	b.WriteString("# Command-line reference\n\n")
	b.WriteString("<!-- Generated from the help of obied and obiectl; do not edit. After changing\n" +
		"     the help, run: go run ./packaging/gendocs -reference " + ReferencePath + " -->\n\n")
	b.WriteString("Every command of `obied` and `obiectl`, with its flags and examples, as\n" +
		"`obied help <command>` and `obiectl help <command>` show it. The manual\n" +
		"pages `man obied` and `man obiectl` hold the same text; the release\n" +
		"downloads install them with the shell completion.\n\n")
	for _, name := range ToolNames {
		t := toolByName(name)
		fmt.Fprintf(&b, "- [`%s`](#%s): %s\n", t.name, t.name, t.summary)
	}
	for _, name := range ToolNames {
		writeToolReference(&b, toolByName(name))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// writeToolReference writes the reference of tool t.
func writeToolReference(b *strings.Builder, t *tool) {
	fmt.Fprintf(b, "\n## %s\n\n%s\n\n```text\n", t.name, mdText(strings.TrimSpace(t.description)))
	for _, u := range t.usage {
		fmt.Fprintf(b, "%s %s\n", t.name, u)
	}
	b.WriteString("```\n\n| Task | Command | What it does |\n|------|---------|--------------|\n")
	for _, g := range groups {
		task, _, _ := strings.Cut(groupTitles[g], ":")
		for _, c := range t.commands {
			if c.group == g {
				fmt.Fprintf(b, "| %s | [`%s`](#%s) | %s |\n", task, c.name, mdAnchor(t.name+" "+c.name), mdCell(c.summary))
			}
		}
	}
	fmt.Fprintf(b, "\n%s\n", mdText(strings.TrimSpace(t.start)))
	if fs := t.flags(""); hasFlags(fs) {
		b.WriteString("\nThe global flags go before the command:\n\n")
		writeFlagTable(b, fs)
	}
	for _, c := range t.commands {
		fmt.Fprintf(b, "\n### %s %s\n\n%s.\n\n```text\n", t.name, c.name, mdText(upperFirst(c.summary)))
		for _, u := range c.usage {
			fmt.Fprintf(b, "%s %s %s\n", t.name, c.name, u)
		}
		fmt.Fprintf(b, "```\n\n%s\n", mdText(strings.TrimSpace(c.description)))
		if fs := t.flags(c.name); hasFlags(fs) {
			b.WriteString("\n")
			writeFlagTable(b, fs)
		}
		b.WriteString("\nExamples:\n\n```sh\n")
		for i, e := range c.examples {
			if i > 0 {
				b.WriteString("\n")
			}
			fmt.Fprintf(b, "# %s\n%s\n", strings.TrimSuffix(e.what, ":"), e.command)
		}
		b.WriteString("```\n")
	}
}

// writeFlagTable writes the flags of fs as a table with their defaults.
func writeFlagTable(b *strings.Builder, fs *flag.FlagSet) {
	b.WriteString("| Flag | What it does |\n|------|--------------|\n")
	fs.VisitAll(func(f *flag.Flag) {
		arg, usage := flag.UnquoteUsage(f)
		name := "--" + f.Name
		if arg != "" {
			name += " " + arg
		}
		fmt.Fprintf(b, "| `%s` | %s |\n", mdCell(name), mdCell(usage+defaultText(f, usage)))
	})
}

// upperFirst returns s with its first letter in upper case.
func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
