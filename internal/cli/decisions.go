package cli

import (
	"context"
	"fmt"
	"io"
	"math"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
)

func runExplain(ctx context.Context, client *admin.Client, args []string, stdout, stderr io.Writer) int {
	const program = "obiectl explain"
	fs := newFlagSet(program)
	asJSON := fs.Bool("json", false, "print the explanation as JSON, for scripts")
	target, code, done := parseOneArg(fs, args, "address or range", stdout, stderr)
	if done {
		return code
	}
	d, err := client.Explain(ctx, target)
	if err != nil {
		reportClientError(stderr, err)
		return ExitFailure
	}
	if *asJSON {
		err = writeJSON(stdout, d)
	} else {
		err = writeExplanation(stdout, d)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "obiectl: writing explanation: %v\n", err)
		return ExitIOError
	}
	return ExitOK
}

func runDecisions(ctx context.Context, client *admin.Client, args []string, stdout, stderr io.Writer) int {
	const program = "obiectl decisions"
	fs := newFlagSet(program)
	asJSON := fs.Bool("json", false, "print the decisions as JSON, for scripts")
	state := fs.String("state", "", "list only the decisions in `state`: block, none or allowed (default: every state)")
	if code, done := parseNoArgs(fs, args, stdout, stderr); done {
		return code
	}
	resp, err := client.Decisions(ctx, *state)
	if err != nil {
		reportClientError(stderr, err)
		return ExitFailure
	}
	if *asJSON {
		err = writeJSON(stdout, resp)
	} else {
		err = writeDecisionsTable(stdout, resp.Decisions)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "obiectl: writing decisions: %v\n", err)
		return ExitIOError
	}
	return ExitOK
}

// writeExplanation prints the decision summary followed by one row per
// publisher, in the order the daemon sends them (sorted by peer ID).
func writeExplanation(w io.Writer, d *admin.DecisionResponse) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "Indicator:\t%s\n", d.Indicator.Key())
	decision := d.State
	if d.ExpiresAt != nil {
		decision += " until " + d.ExpiresAt.UTC().Format(time.RFC3339)
	}
	_, _ = fmt.Fprintf(tw, "Decision:\t%s\n", decision)
	_, _ = fmt.Fprintf(tw, "Reason:\t%s\n", d.Reason)
	_, _ = fmt.Fprintf(tw, "Score:\t%s (threshold %s)\n", formatScore(d.Score), formatScore(d.Threshold))
	_, _ = fmt.Fprintf(tw, "Publishers:\t%d (quorum %d)\n", d.Contributors, d.Quorum)
	_, _ = fmt.Fprintf(tw, "Local autoblock:\t%s\n", yesNo(d.LocalAutoblock))
	if s := d.Sovereignty; s != nil {
		_, _ = fmt.Fprintf(tw, "Allow-list/overrides:\t%s\n", sovereigntyText(s))
		if s.OverrideNote != "" {
			_, _ = fmt.Fprintf(tw, "Override note:\t%s\n", s.OverrideNote)
		}
	}
	_, _ = fmt.Fprintf(tw, "Evaluated:\t%s\n", d.EvaluatedAt.UTC().Format(time.RFC3339))
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(d.Publishers) == 0 {
		_, err := fmt.Fprintln(w, "\nNo active verdicts.")
		return err
	}
	_, _ = fmt.Fprintf(tw, "\nPUBLISHER\tPEER ID\tACTION\tWEIGHT\tCONFIDENCE\tSCORE\tCOUNTS\tPROTOCOL\tREASON\tISSUED\tEXPIRES\n")
	for _, p := range d.Publishers {
		name := p.Name
		if p.Local {
			name = "(this node)"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", orDash(name), p.PeerID, p.Action,
			formatScore(p.Weight), formatScore(p.Confidence), formatScore(p.Score), yesNo(p.Contributes),
			orDash(p.Protocol), orDash(p.Reason), p.IssuedAt.UTC().Format(time.RFC3339), p.ExpiresAt.UTC().Format(time.RFC3339))
	}
	return tw.Flush()
}

// sovereigntyText says how the allow-list and overrides affect a decision.
func sovereigntyText(s *admin.SovereigntyResponse) string {
	switch {
	case !s.Applied:
		return "not applied (" + s.Note + ")"
	case s.Effect == "":
		return "none apply"
	default:
		return s.Note
	}
}

// writeDecisionsTable prints one row per decision, in the order the daemon
// sends them (sorted by indicator key).
func writeDecisionsTable(w io.Writer, ds []admin.DecisionResponse) error {
	if len(ds) == 0 {
		_, err := fmt.Fprintln(w, "No decisions.")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "INDICATOR\tSTATE\tSCORE\tPUBLISHERS\tEXPIRES\tREASON\n")
	for _, d := range ds {
		expires := "-"
		if d.ExpiresAt != nil {
			expires = d.ExpiresAt.UTC().Format(time.RFC3339)
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\n", d.Indicator.Key(), d.State, formatScore(d.Score),
			d.Contributors, expires, d.Reason)
	}
	return tw.Flush()
}

// formatScore prints a score, weight or confidence rounded to four decimals,
// hiding float noise such as 1.7999999999999998.
func formatScore(f float64) string {
	return strconv.FormatFloat(math.Round(f*1e4)/1e4, 'f', -1, 64)
}
