package cli

import (
	"context"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"text/tabwriter"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
)

func runExplain(ctx context.Context, client *admin.Client, args []string, stdout, stderr io.Writer) int {
	const program = "obiectl explain"
	fs := newFlagSet(program)
	asJSON := fs.Bool("json", false, "print the explanation as JSON, for scripts")
	target, code, done := parseAddressArg(fs, args, stdout, stderr)
	if done {
		return code
	}
	d, err := client.Explain(ctx, target)
	if err != nil {
		reportClientError(stderr, program, client, err)
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
	limit := limitFlag(fs)
	if code, done := parseNoArgs(fs, args, stdout, stderr); done {
		return code
	}
	if !checkLimit(program, *limit, stderr) {
		return ExitUsage
	}
	resp, err := client.Decisions(ctx, *state)
	if err != nil {
		reportClientError(stderr, program, client, err)
		return ExitFailure
	}
	if *asJSON {
		err = writeJSON(stdout, resp)
	} else {
		err = writeDecisionsTable(stdout, resp.Decisions, *limit)
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
		decision += " until " + formatTime(*d.ExpiresAt)
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
	_, _ = fmt.Fprintf(tw, "Evaluated:\t%s\n", formatTime(d.EvaluatedAt))
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
			orDash(p.Protocol), orDash(p.Reason), formatTime(p.IssuedAt), formatTime(p.ExpiresAt))
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

// stateOrder ranks the decision states for listings: blocks first, then
// what is allowed, then the rest.
var stateOrder = map[string]int{admin.StateBlock: 0, admin.StateAllowed: 1, admin.StateNone: 2}

// writeDecisionsTable prints how many decisions there are in each state,
// then at most limit rows (0: every row), blocks first, then allowed and
// none, each in the order the daemon sends them (sorted by indicator key).
func writeDecisionsTable(w io.Writer, ds []admin.DecisionResponse, limit int) error {
	if len(ds) == 0 {
		_, err := fmt.Fprintln(w, "No decisions.")
		return err
	}
	counts := map[string]int{}
	for _, d := range ds {
		counts[d.State]++
	}
	if _, err := fmt.Fprintf(w, "Decisions: %d (%d block, %d allowed, %d none)\n\n", len(ds),
		counts[admin.StateBlock], counts[admin.StateAllowed], counts[admin.StateNone]); err != nil {
		return err
	}
	rank := func(state string) int {
		if r, ok := stateOrder[state]; ok {
			return r
		}
		return len(stateOrder)
	}
	rows := slices.Clone(ds)
	slices.SortStableFunc(rows, func(a, b admin.DecisionResponse) int { return rank(a.State) - rank(b.State) })
	rows = rows[:shownRows(len(rows), limit)]
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "INDICATOR\tSTATE\tSCORE\tPUBLISHERS\tEXPIRES\tREASON\n")
	for _, d := range rows {
		expires := "-"
		if d.ExpiresAt != nil {
			expires = formatTime(*d.ExpiresAt)
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\n", d.Indicator.Key(), d.State, formatScore(d.Score),
			d.Contributors, expires, d.Reason)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	return writeMoreRows(w, len(rows), len(ds), "decisions, blocks first", "Narrow them down with --state block, allowed or none. ")
}

// formatScore prints a score, weight or confidence rounded to four decimals,
// hiding float noise such as 1.7999999999999998.
func formatScore(f float64) string {
	return strconv.FormatFloat(math.Round(f*1e4)/1e4, 'f', -1, 64)
}
