package cli

import (
	"context"
	"flag"
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
	fs := newFlagSet(program, stderr)
	configPath := fs.String("config", config.DefaultPath, "configuration `file` of the node")
	serviceUser := fs.String("service-user", "obie", "`user` obied runs as")
	asJSON := fs.Bool("json", false, "print the report as JSON, for scripts")
	timeout := fs.Duration("timeout", selfCheckTimeout, "give up after this `duration`, e.g. 30s")
	fs.Usage = func() { selfCheckUsage(fs) }
	if code, done := parseCommand(fs, program, args, stderr); done {
		return code
	}
	if *timeout <= 0 {
		_, _ = fmt.Fprintf(stderr, "%s: --timeout must be positive\n", program)
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

func selfCheckUsage(fs *flag.FlagSet) {
	out := fs.Output()
	_, _ = fmt.Fprintf(out, `Usage: obied self-check [flags]

Checks the node and this server, before the first start or at any time
later, and reports each check as OK, WARNING or PROBLEM with the next step:
configuration, identity, admin access, node, peers, clock, Fail2Ban,
firewall and your SSH session's address. It changes nothing. Run it as
root to let it look everywhere.

Exit status: 0 no problem (warnings may remain), 1 at least one problem,
2 wrong usage, 3 the report could not be written.

Flags:
`)
	fs.PrintDefaults()
}
