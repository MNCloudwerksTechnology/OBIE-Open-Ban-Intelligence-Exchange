package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
)

// requestTimeout bounds every admin API call of obiectl unless --timeout
// says otherwise.
const requestTimeout = 10 * time.Second

// RunCtl runs obiectl with args and returns the process exit code.
func RunCtl(args []string, stdout, stderr io.Writer) int {
	const program = ctlName
	fs := newFlagSet(program)
	socket := fs.String("socket", config.Default().Admin.Socket, "path of the node's admin `socket`, admin.socket in its configuration")
	timeout := fs.Duration("timeout", requestTimeout, "give up on the node after this `duration`, e.g. 5s")
	if code, done := parse(fs, args, stdout, stderr); done {
		return code
	}
	if *timeout <= 0 {
		usageProblem(program, fmt.Sprintf("--timeout must be positive, e.g. 5s, got %s", *timeout)).write(stderr, program)
		return ExitUsage
	}
	t := ctlTool()
	if fs.NArg() == 0 {
		t.overview(stderr)
		return ExitUsage
	}
	name := fs.Arg(0)
	if name == "help" {
		return t.runHelp(fs.Args()[1:], stdout, stderr)
	}
	cmds := ctlCommands()
	i := slices.IndexFunc(cmds, func(c command) bool { return c.name == name })
	if i < 0 {
		t.unknownCommand(stderr, name)
		return ExitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	ctx = withInvocation(ctx, invocation{args: fs.Args(), timeout: *timeout})
	return cmds[i].run(ctx, admin.NewClient(*socket), fs.Args()[1:], stdout, stderr)
}

func runStatus(ctx context.Context, client *admin.Client, args []string, stdout, stderr io.Writer) int {
	const program = "obiectl status"
	fs := newFlagSet(program)
	asJSON := fs.Bool("json", false, "print the status as JSON, for scripts")
	if code, done := parseNoArgs(fs, args, stdout, stderr); done {
		return code
	}

	status, err := client.Status(ctx)
	if err != nil {
		reportClientError(ctx, stderr, program, client, err)
		return ExitFailure
	}
	if *asJSON {
		err = writeJSON(stdout, status)
	} else {
		err = writeStatusTable(stdout, status)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "obiectl: writing status: %v\n", err)
		return ExitIOError
	}
	if !*asJSON && !status.Ready {
		_, _ = fmt.Fprintf(stderr, "%s: the node is not ready; sudo obied self-check says what to do about each subsystem "+
			"that is not, and sudo journalctl -u obied -n 50 shows the node's log\n", program)
	}
	return ExitOK
}

func writeJSON(w io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", data)
	return err
}

// modeText describes the node mode prominently.
var modeText = map[string]string{
	string(config.ModeObserve): "OBSERVE (decisions are logged, nothing is blocked)",
	string(config.ModeEnforce): "ENFORCE (blocks are sent to the enforcer)",
}

// writeStatusTable prints the node summary, the mode first, followed by
// one row per subsystem, sorted by name.
func writeStatusTable(w io.Writer, s *admin.StatusResponse) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	mode, ok := modeText[s.Mode]
	if !ok {
		mode = strings.ToUpper(s.Mode)
	}
	_, _ = fmt.Fprintf(tw, "Mode:\t%s\n", mode)
	_, _ = fmt.Fprintf(tw, "Version:\t%s\n", s.Version)
	_, _ = fmt.Fprintf(tw, "Uptime:\t%s\n", formatDuration(s.Uptime()))
	_, _ = fmt.Fprintf(tw, "Ready:\t%s\n", yesNo(s.Ready))
	if err := tw.Flush(); err != nil {
		return err
	}

	names := make([]string, 0, len(s.Subsystems))
	for name := range s.Subsystems {
		names = append(names, name)
	}
	sort.Strings(names)
	_, _ = fmt.Fprintf(tw, "\nSUBSYSTEM\tSTATE\tREADY\tERROR\tDETAIL\n")
	for _, name := range names {
		sub := s.Subsystems[name]
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", name, sub.State, yesNo(sub.Ready), orDash(sub.Error), orDash(sub.Detail))
	}
	return tw.Flush()
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
