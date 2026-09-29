package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/selfcheck"
	"github.com/MNCloudwerksTechnology/obie/internal/version"
)

// selfCheckTimeout bounds a self-check unless --timeout says otherwise.
const selfCheckTimeout = time.Minute

// selfCheckEnv returns the environment obied self-check checks; tests
// replace it.
var selfCheckEnv = selfcheck.HostEnv

// runSelfCheck checks the node and this host and reports every check as
// OK, warning or problem, with the next step for each warning and
// problem. It exits 1 if any check found a problem.
func runSelfCheck(args []string, stdout, stderr io.Writer) int {
	const program = "obied self-check"
	fs := newFlagSet(program)
	configPath := fs.String("config", config.DefaultPath, "configuration `file` of the node")
	serviceUser := fs.String("service-user", "obie", "`user` obied runs as")
	asJSON := fs.Bool("json", false, "print the report as JSON, for scripts")
	timeout := fs.Duration("timeout", selfCheckTimeout, "give up after this `duration`, e.g. 30s")
	if code, done := parseNoArgs(fs, args, stdout, stderr); done {
		return code
	}
	if *timeout <= 0 {
		usageProblem(program, fmt.Sprintf("--timeout must be positive, e.g. 30s, got %s", *timeout)).write(stderr, program)
		return ExitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	report := selfcheck.Run(ctx, selfCheckEnv(*configPath, *serviceUser, version.Version))
	write := selfcheck.WriteText
	if *asJSON {
		write = selfcheck.WriteJSON
	}
	if err := write(stdout, report); err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: writing the report: %v\n", program, err)
		return ExitIOError
	}
	if report.Status == selfcheck.Problem {
		return ExitFailure
	}
	return ExitOK
}
