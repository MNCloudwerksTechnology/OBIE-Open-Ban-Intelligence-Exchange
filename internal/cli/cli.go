// Package cli provides the command-line handling shared by the OBIE binaries.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/MNCloudwerksTechnology/obie/internal/version"
)

// Exit codes returned by Run. Diagnostics written to stderr are best effort:
// a failing stderr cannot be reported anywhere else.
const (
	ExitOK             = 0
	ExitNotImplemented = 1
	ExitUsage          = 2
	ExitIOError        = 3
)

// Run parses args for the named program and returns the process exit code.
// Only --version is implemented at this stage; any other invocation reports
// that the program has no functionality yet.
func Run(program string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet(program, flag.ContinueOnError)
	fs.SetOutput(stderr)
	showVersion := fs.Bool("version", false, "print the version and exit")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitOK
		}
		return ExitUsage
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "%s: unexpected argument %q\n", program, fs.Arg(0))
		fs.Usage()
		return ExitUsage
	}
	if *showVersion {
		if _, err := fmt.Fprintln(stdout, version.String(program)); err != nil {
			_, _ = fmt.Fprintf(stderr, "%s: writing version: %v\n", program, err)
			return ExitIOError
		}
		return ExitOK
	}

	_, _ = fmt.Fprintf(stderr, "%s: not implemented yet; only --version is available\n", program)
	return ExitNotImplemented
}
