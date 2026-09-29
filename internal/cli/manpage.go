package cli

import (
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"
)

// DocumentationURL is where the documentation of the release is read.
const DocumentationURL = "https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange/tree/main/documentation"

// manFiles are the files each tool uses, for the FILES section of its
// manual page.
var manFiles = map[string][][2]string{
	daemonName: {
		{"/etc/obie/obie.yaml", "the configuration (--config); obied setup writes it"},
		{"/etc/obie/obie.yaml.example", "the example configuration, which describes every setting"},
		{"/var/lib/obie", "the state directory (node.state_dir): the identity key node.key and the database of verdicts"},
		{"/run/obie/obie.sock", "the admin socket (admin.socket) that obiectl talks to"},
	},
	ctlName: {
		{"/run/obie/obie.sock", "the admin socket of the node (admin.socket, --socket)"},
	},
}

// exitStatus is the EXIT STATUS section of every manual page.
const exitStatus = `0 success; 1 failure, also an invalid configuration; 2 wrong usage;
3 the output could not be written. obied self-check exits 1 if at least one
check found a problem.`

// roffText escapes text for roff: backslashes, a leading period, and
// hyphens and quotes, which groff would otherwise typeset as dashes and
// curly quotes, so that commands and flags can be copied from the page.
func roffText(s string) string {
	s = strings.NewReplacer(`\`, `\e`, "-", `\-`, "'", `\(aq`, "`", `\(ga`).Replace(s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, ".") {
			lines[i] = `\&` + l
		}
	}
	return strings.Join(lines, "\n")
}

// roffQuote escapes s as a quoted macro argument.
func roffQuote(s string) string {
	return `"` + strings.ReplaceAll(roffText(s), `"`, `\(dq`) + `"`
}

// writeRoffParagraphs writes text, paragraphs separated by blank lines.
func writeRoffParagraphs(b *strings.Builder, text string) {
	for i, p := range strings.Split(strings.TrimSpace(text), "\n\n") {
		if i > 0 {
			b.WriteString(".PP\n")
		}
		fmt.Fprintf(b, "%s\n", roffText(p))
	}
}

// writeRoffFlags writes the flags of fs as a tagged list.
func writeRoffFlags(b *strings.Builder, fs *flag.FlagSet) {
	fs.VisitAll(func(f *flag.Flag) {
		arg, usage := flag.UnquoteUsage(f)
		b.WriteString(".TP\n")
		if arg == "" {
			fmt.Fprintf(b, ".B %s\n", roffText("--"+f.Name))
		} else {
			fmt.Fprintf(b, ".BI %s %s\n", roffQuote("--"+f.Name+" "), roffQuote(arg))
		}
		fmt.Fprintf(b, "%s\n", roffText(usage+defaultText(f, usage)))
	})
}

// WriteManPage writes the manual page of the tool called toolName (obied
// or obiectl), section 1, to w, with version and date (YYYY-MM-DD) in its
// header.
func WriteManPage(w io.Writer, toolName, version, date string) error {
	if !slices.Contains(ToolNames, toolName) {
		return fmt.Errorf("unknown tool %q", toolName)
	}
	t := toolByName(toolName)
	var b strings.Builder
	fmt.Fprintf(&b, ".\\\" Manual page of %s, generated from its help by packaging/gendocs.\n", t.name)
	fmt.Fprintf(&b, ".TH %s 1 %s %s \"OBIE Manual\"\n", strings.ToUpper(t.name), roffQuote(date), roffQuote("OBIE "+version))
	// Commands and paths in the text must stay whole to be copied: no
	// hyphenation, no stretched spaces. The man macros restore the
	// adjustment from the string AD after every paragraph, so it is set too.
	b.WriteString(".nh\n.ad l\n.ds AD l\n")
	fmt.Fprintf(&b, ".SH NAME\n%s \\- %s\n", t.name, roffText(t.summary))
	b.WriteString(".SH SYNOPSIS\n")
	for i, u := range t.usage {
		if i > 0 {
			b.WriteString(".br\n")
		}
		fmt.Fprintf(&b, ".B %s\n%s\n", t.name, roffText(u))
	}
	b.WriteString(".SH DESCRIPTION\n")
	writeRoffParagraphs(&b, t.description)
	b.WriteString(".PP\n")
	writeRoffParagraphs(&b, t.start)
	b.WriteString(".SH COMMANDS\n")
	for _, g := range groups {
		first := true
		for _, c := range t.commands {
			if c.group != g {
				continue
			}
			if first {
				fmt.Fprintf(&b, ".SS %s\n", roffQuote(groupTitles[g]))
				first = false
			}
			fmt.Fprintf(&b, ".TP\n.B %s\n%s\n", roffText(c.name), roffText(c.summary))
		}
	}
	if fs := t.flags(""); hasFlags(fs) {
		fmt.Fprintf(&b, ".SH FLAGS\n%s.\n", roffText(t.flagsIntro))
		writeRoffFlags(&b, fs)
	}
	b.WriteString(".SH \"EVERY COMMAND\"\n")
	for _, c := range t.commands {
		fmt.Fprintf(&b, ".SS %s\n", roffQuote(t.name+" "+c.name))
		for i, u := range c.usage {
			if i > 0 {
				b.WriteString(".br\n")
			}
			fmt.Fprintf(&b, ".B %s\n%s\n", roffText(t.name+" "+c.name), roffText(u))
		}
		b.WriteString(".PP\n")
		writeRoffParagraphs(&b, c.description)
		if fs := t.flags(c.name); hasFlags(fs) {
			b.WriteString(".PP\nFlags:\n")
			writeRoffFlags(&b, fs)
		}
		b.WriteString(".PP\nExamples:\n")
		for _, e := range c.examples {
			fmt.Fprintf(&b, ".PP\n%s\n.RS 4\n.nf\n%s\n.fi\n.RE\n", roffText(e.what), roffText(e.command))
		}
	}
	fmt.Fprintf(&b, ".SH \"EXIT STATUS\"\n%s\n", roffText(exitStatus))
	b.WriteString(".SH FILES\n")
	for _, f := range manFiles[t.name] {
		fmt.Fprintf(&b, ".TP\n.I %s\n%s\n", roffText(f[0]), roffText(f[1]))
	}
	b.WriteString(".SH \"SEE ALSO\"\n")
	var others []string
	for _, name := range ToolNames {
		if name != t.name {
			others = append(others, ".BR "+name+" (1)")
		}
	}
	fmt.Fprintf(&b, "%s\n.PP\nThe documentation:\n.UR %s\n.UE\n", strings.Join(others, ",\n"), DocumentationURL)
	_, err := io.WriteString(w, b.String())
	return err
}
