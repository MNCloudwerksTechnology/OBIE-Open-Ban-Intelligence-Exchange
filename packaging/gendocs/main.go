// Command gendocs writes the manual pages, the shell completion scripts
// and the command-line reference of obied and obiectl, all from their help
// (ADR 0028):
//
//	go run ./packaging/gendocs -out <dir> -version <version>
//	go run ./packaging/gendocs -reference documentation/operations/cli.md
//
// With -out it writes, below dir, the layout of $PREFIX/share that
// packaging/release.sh puts into every release tarball:
//
//	man/man1/obied.1, man/man1/obiectl.1
//	bash-completion/completions/obied, bash-completion/completions/obiectl
//	zsh/site-functions/_obied, zsh/site-functions/_obiectl
//	fish/vendor_completions.d/obied.fish, fish/vendor_completions.d/obiectl.fish
//
// The date in the manual pages is that of SOURCE_DATE_EPOCH, else today,
// so that a release is reproducible.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/cli"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

// completionPaths are where each shell looks for completion scripts below
// $PREFIX/share, as a directory and a file name pattern for the tool.
var completionPaths = map[string][2]string{
	"bash": {"bash-completion/completions", "%s"},
	"zsh":  {"zsh/site-functions", "_%s"},
	"fish": {"fish/vendor_completions.d", "%s.fish"},
}

// run runs gendocs with args and returns the exit code.
func run(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("gendocs", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("out", "", "write the manual pages and completion scripts below this `directory`")
	version := fs.String("version", "", "the release `version` for the manual pages")
	reference := fs.String("reference", "", "write the command-line reference to this `file`")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if (*out == "") == (*reference == "") || fs.NArg() > 0 || *out != "" && *version == "" {
		_, _ = fmt.Fprintln(stderr, "usage: gendocs -out <dir> -version <version> | gendocs -reference <file>")
		return 2
	}
	var err error
	if *reference != "" {
		err = writeFile(*reference, cli.WriteReference)
	} else {
		err = writeShare(*out, *version)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "gendocs: %v\n", err)
		return 1
	}
	return 0
}

// writeShare writes the manual pages and completion scripts below dir.
func writeShare(dir, version string) error {
	date, err := releaseDate()
	if err != nil {
		return err
	}
	for _, tool := range cli.ToolNames {
		page := filepath.Join(dir, "man", "man1", tool+".1")
		if err := writeFile(page, func(w io.Writer) error { return cli.WriteManPage(w, tool, version, date) }); err != nil {
			return err
		}
		for _, shell := range cli.Shells {
			p := completionPaths[shell]
			path := filepath.Join(dir, p[0], fmt.Sprintf(p[1], tool))
			if err := writeFile(path, func(w io.Writer) error { return cli.WriteCompletion(w, tool, shell) }); err != nil {
				return err
			}
		}
	}
	return nil
}

// releaseDate is the day of SOURCE_DATE_EPOCH, else today, in UTC.
func releaseDate() (string, error) {
	now := time.Now()
	if s := os.Getenv("SOURCE_DATE_EPOCH"); s != "" {
		sec, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return "", errors.New("SOURCE_DATE_EPOCH must be a number of seconds")
		}
		now = time.Unix(sec, 0)
	}
	return now.UTC().Format(time.DateOnly), nil
}

// writeFile writes what write produces to path, creating its directory.
func writeFile(path string, write func(io.Writer) error) error {
	var b bytes.Buffer
	if err := write(&b); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { // #nosec G301 -- shipped documentation, readable by all.
		return err
	}
	return os.WriteFile(path, b.Bytes(), 0o644) // #nosec G306 -- shipped documentation, readable by all.
}
