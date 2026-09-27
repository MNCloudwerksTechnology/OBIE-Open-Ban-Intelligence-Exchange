package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
)

// requestTimeout bounds every admin API call of obiectl.
const requestTimeout = 10 * time.Second

// command is an obiectl subcommand.
type command struct {
	summary string
	run     func(ctx context.Context, client *admin.Client, args []string, stdout, stderr io.Writer) int
}

var commands = map[string]command{
	"status":     {summary: "show the node status", run: runStatus},
	"identity":   {summary: "show the node's peer ID and key fingerprint", run: runCtlIdentity},
	"peers":      {summary: "list the connected mesh peers", run: runPeers},
	"explain":    {summary: "explain the decision on an address or CIDR range", run: runExplain},
	"decisions":  {summary: "list the decisions (--state block|none|allowed)", run: runDecisions},
	"allow":      {summary: "force-allow an address or range: never block it", run: runAllow},
	"block":      {summary: "force-block an address or range, whatever its score", run: runBlock},
	"overrides":  {summary: "list the operator overrides", run: runOverrides},
	"unoverride": {summary: "remove the override of an address or range", run: runUnoverride},
}

// RunCtl runs obiectl with args and returns the process exit code.
func RunCtl(args []string, stdout, stderr io.Writer) int {
	const program = "obiectl"
	fs := newFlagSet(program, stderr)
	socket := fs.String("socket", config.Default().Admin.Socket, "path of the obied admin `socket`")
	fs.Usage = func() { ctlUsage(fs) }
	if code, done := parse(fs, program, args, true, stdout, stderr); done {
		return code
	}
	if fs.NArg() == 0 {
		_, _ = fmt.Fprintf(stderr, "%s: missing command\n", program)
		fs.Usage()
		return ExitUsage
	}
	name := fs.Arg(0)
	cmd, ok := commands[name]
	if !ok {
		_, _ = fmt.Fprintf(stderr, "%s: unknown command %q\n", program, name)
		fs.Usage()
		return ExitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	return cmd.run(ctx, admin.NewClient(*socket), fs.Args()[1:], stdout, stderr)
}

func ctlUsage(fs *flag.FlagSet) {
	out := fs.Output()
	_, _ = fmt.Fprintf(out, "Usage: obiectl [flags] <command> [command flags]\n\nCommands:\n")
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		_, _ = fmt.Fprintf(out, "  %-11s %s\n", name, commands[name].summary)
	}
	_, _ = fmt.Fprintf(out, "\nFlags:\n")
	fs.PrintDefaults()
}

func runStatus(ctx context.Context, client *admin.Client, args []string, stdout, stderr io.Writer) int {
	const program = "obiectl status"
	fs := newFlagSet(program, stderr)
	asJSON := fs.Bool("json", false, "print the status as JSON")
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

	status, err := client.Status(ctx)
	if err != nil {
		reportClientError(stderr, err)
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
	return ExitOK
}

// reportClientError explains a failed admin API call.
func reportClientError(stderr io.Writer, err error) {
	_, _ = fmt.Fprintf(stderr, "obiectl: %v\n", err)
	if errors.Is(err, admin.ErrDaemonNotRunning) {
		_, _ = fmt.Fprintln(stderr, "obiectl: start obied, or point --socket at its admin.socket")
	}
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
	_, _ = fmt.Fprintf(tw, "Uptime:\t%s\n", s.Uptime().Truncate(time.Second))
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
