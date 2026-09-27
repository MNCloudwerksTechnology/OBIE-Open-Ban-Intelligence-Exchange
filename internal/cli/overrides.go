package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
)

func runAllow(ctx context.Context, client *admin.Client, args []string, stdout, stderr io.Writer) int {
	return runSetOverride(ctx, client, "allow", admin.ActionForceAllow, args, stdout, stderr)
}

func runBlock(ctx context.Context, client *admin.Client, args []string, stdout, stderr io.Writer) int {
	return runSetOverride(ctx, client, "block", admin.ActionForceBlock, args, stdout, stderr)
}

// runSetOverride sets a force-allow or force-block override.
func runSetOverride(ctx context.Context, client *admin.Client, name, action string, args []string, stdout, stderr io.Writer) int {
	program := "obiectl " + name
	fs := newFlagSet(program, stderr)
	ttl := fs.String("ttl", "", "remove the override after `duration` (e.g. 90m, 36h, 7d); default: never")
	note := fs.String("note", "", "why the override was set, shown by obiectl overrides and explain")
	asJSON := fs.Bool("json", false, "print the result as JSON")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(fs.Output(), "Usage: %s <ip | cidr> [--ttl duration] [--note text] [--json]\n\n", program)
		_, _ = fmt.Fprintf(fs.Output(), "%s\n\nFlags:\n", overrideHelp[action])
		fs.PrintDefaults()
	}
	indicator, code, done := parseOne(fs, program, args, stderr)
	if done {
		return code
	}
	req := admin.OverrideRequest{Indicator: indicator, Action: action, Note: *note}
	if *ttl != "" {
		d, err := config.ParseDuration(*ttl)
		if err != nil || d <= 0 {
			_, _ = fmt.Fprintf(stderr, "%s: invalid --ttl %q: want a positive duration such as 90m, 36h or 7d\n", program, *ttl)
			return ExitUsage
		}
		req.TTLSeconds = int64(d.Std().Round(time.Second) / time.Second)
	}
	res, err := client.SetOverride(ctx, req)
	if err != nil {
		reportClientError(stderr, err)
		return ExitFailure
	}
	if *asJSON {
		err = writeJSON(stdout, res)
	} else {
		err = writeOverrideResult(stdout, res)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "obiectl: writing result: %v\n", err)
		return ExitIOError
	}
	if action == admin.ActionForceBlock && res.Decision != nil && res.Decision.State != admin.StateBlock {
		_, _ = fmt.Fprintf(stderr, "obiectl: warning: the force-block does not take effect: %s\n", res.Decision.Reason)
	}
	return ExitOK
}

var overrideHelp = map[string]string{
	admin.ActionForceAllow: "Never block the address or range, whatever the mesh reports. Beats every\nother rule, including the allow-list and force-blocks on overlapping ranges.",
	admin.ActionForceBlock: "Block the address or range whatever its score. Beats allowlist.cidrs and\nallowlist.files, but never the built-in ranges, this node's own addresses\nor its bootstrap peers.",
}

func runOverrides(ctx context.Context, client *admin.Client, args []string, stdout, stderr io.Writer) int {
	const program = "obiectl overrides"
	fs := newFlagSet(program, stderr)
	asJSON := fs.Bool("json", false, "print the overrides as JSON")
	if code, done := parseCommand(fs, program, args, stderr); done {
		return code
	}
	resp, err := client.Overrides(ctx)
	if err != nil {
		reportClientError(stderr, err)
		return ExitFailure
	}
	if *asJSON {
		err = writeJSON(stdout, resp)
	} else {
		err = writeOverridesTable(stdout, resp.Overrides)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "obiectl: writing overrides: %v\n", err)
		return ExitIOError
	}
	return ExitOK
}

func runUnoverride(ctx context.Context, client *admin.Client, args []string, stdout, stderr io.Writer) int {
	const program = "obiectl unoverride"
	fs := newFlagSet(program, stderr)
	asJSON := fs.Bool("json", false, "print the result as JSON")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(fs.Output(), "Usage: %s <ip | cidr> [--json]\n\nFlags:\n", program)
		fs.PrintDefaults()
	}
	indicator, code, done := parseOne(fs, program, args, stderr)
	if done {
		return code
	}
	res, err := client.DeleteOverride(ctx, indicator)
	if err != nil {
		reportClientError(stderr, err)
		return ExitFailure
	}
	if *asJSON {
		err = writeJSON(stdout, res)
	} else {
		_, err = fmt.Fprintf(stdout, "Override on %s removed.\n", indicator)
		if err == nil {
			err = writeResultDecision(stdout, res.Decision)
		}
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "obiectl: writing result: %v\n", err)
		return ExitIOError
	}
	return ExitOK
}

// parseOne parses a command's flags, which may also follow the one
// positional argument, and returns that argument.
func parseOne(fs *flag.FlagSet, program string, args []string, stderr io.Writer) (arg string, code int, done bool) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return "", ExitOK, true
			}
			return "", ExitUsage, true
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
	if len(positional) != 1 {
		_, _ = fmt.Fprintf(stderr, "%s: want 1 argument, got %d\n", program, len(positional))
		fs.Usage()
		return "", ExitUsage, true
	}
	return positional[0], 0, false
}

// writeOverrideResult prints the override that was set and the resulting
// decision.
func writeOverrideResult(w io.Writer, res *admin.OverrideResult) error {
	if o := res.Override; o != nil {
		line := fmt.Sprintf("Override set: %s on %s, %s", o.Action, o.Indicator.Key(), untilText(o.ExpiresAt))
		if o.Note != "" {
			line += fmt.Sprintf(" (note: %q)", o.Note)
		}
		if _, err := fmt.Fprintln(w, line+"."); err != nil {
			return err
		}
	}
	return writeResultDecision(w, res.Decision)
}

// writeResultDecision prints the decision after an override change, if the
// daemon sent one.
func writeResultDecision(w io.Writer, d *admin.DecisionResponse) error {
	if d == nil {
		return nil
	}
	_, err := fmt.Fprintf(w, "Decision now: %s — %s\n", d.State, d.Reason)
	return err
}

func untilText(t *time.Time) string {
	if t == nil {
		return "until removed"
	}
	return "until " + t.UTC().Format(time.RFC3339)
}

// writeOverridesTable prints one row per override, in the order the daemon
// sends them (sorted by indicator key).
func writeOverridesTable(w io.Writer, list []admin.OverrideResponse) error {
	if len(list) == 0 {
		_, err := fmt.Fprintln(w, "No overrides.")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "INDICATOR\tACTION\tEXPIRES\tCREATED\tNOTE\n")
	for _, o := range list {
		expires := "never"
		if o.ExpiresAt != nil {
			expires = o.ExpiresAt.UTC().Format(time.RFC3339)
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", o.Indicator.Key(), o.Action, expires,
			o.CreatedAt.UTC().Format(time.RFC3339), orDash(o.Note))
	}
	return tw.Flush()
}
