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
	limit := limitFlag(fs)
	if code, done := parseNoArgs(fs, args, stdout, stderr); done {
		return code
	}
	if !checkLimit(program, *limit, stderr) {
		return ExitUsage
	}
	resp, err := client.Enforced(ctx)
	if err != nil {
		reportClientError(ctx, stderr, program, client, err)
		return ExitFailure
	}
	if *asJSON {
		err = writeJSON(stdout, resp)
	} else {
		err = writeEnforcedTable(stdout, resp, time.Now(), *limit)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "obiectl: writing enforced entries: %v\n", err)
		return ExitIOError
	}
	return ExitOK
}

// writeEnforcedTable prints how many entries are applied, then at most
// limit rows (0: every row), in the order the daemon sends them (sorted by
// prefix), with the time left at now.
func writeEnforcedTable(w io.Writer, resp *admin.EnforcedResponse, now time.Time, limit int) error {
	if len(resp.Entries) == 0 {
		msg := "No entries applied."
		if resp.Mode == string(config.ModeObserve) {
			msg = "No entries applied: the node is in observe mode."
		}
		_, err := fmt.Fprintln(w, msg)
		return err
	}
	if _, err := fmt.Fprintf(w, "Entries applied: %d\n\n", len(resp.Entries)); err != nil {
		return err
	}
	rows := resp.Entries[:shownRows(len(resp.Entries), limit)]
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "PREFIX\tEXPIRES\tREMAINING\n")
	for _, e := range rows {
		remaining := formatDuration(max(e.ExpiresAt.Sub(now), 0))
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", e.Prefix, formatTime(e.ExpiresAt), remaining)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	return writeMoreRows(w, len(rows), len(resp.Entries), "entries", "")
}
