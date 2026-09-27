// Package cli provides the command-line handling shared by the OBIE binaries.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/MNCloudwerksTechnology/obie/internal/version"
)

// Exit codes returned by Run.
const (
	ExitOK             = 0
	ExitNotImplemented = 1
	ExitUsage          = 2
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
		fmt.Fprintf(stderr, "%s: unexpected argument %q\n", program, fs.Arg(0))
		fs.Usage()
		return ExitUsage
	}
	if *showVersion {
		fmt.Fprintln(stdout, version.String(program))
		return ExitOK
	}

	fmt.Fprintf(stderr, "%s: not implemented yet; only --version is available\n", program)
	return ExitNotImplemented
}
