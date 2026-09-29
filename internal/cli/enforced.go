package cli

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
)

func runEnforced(ctx context.Context, client *admin.Client, args []string, stdout, stderr io.Writer) int {
	const program = "obiectl enforced"
	fs := newFlagSet(program)
	asJSON := fs.Bool("json", false, "print the entries as JSON, for scripts")
	if code, done := parseNoArgs(fs, args, stdout, stderr); done {
		return code
	}
	resp, err := client.Enforced(ctx)
	if err != nil {
		reportClientError(stderr, program, client, err)
		return ExitFailure
	}
	if *asJSON {
		err = writeJSON(stdout, resp)
	} else {
		err = writeEnforcedTable(stdout, resp, time.Now())
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "obiectl: writing enforced entries: %v\n", err)
		return ExitIOError
	}
	return ExitOK
}

// writeEnforcedTable prints one row per applied entry, in the order the
// daemon sends them (sorted by prefix), with the time left at now.
func writeEnforcedTable(w io.Writer, resp *admin.EnforcedResponse, now time.Time) error {
	if len(resp.Entries) == 0 {
		msg := "No entries applied."
		if resp.Mode == string(config.ModeObserve) {
			msg = "No entries applied: the node is in observe mode."
		}
		_, err := fmt.Fprintln(w, msg)
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "PREFIX\tEXPIRES\tREMAINING\n")
	for _, e := range resp.Entries {
		remaining := max(e.ExpiresAt.Sub(now), 0).Truncate(time.Second)
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", e.Prefix, e.ExpiresAt.UTC().Format(time.RFC3339), remaining)
	}
	return tw.Flush()
}
