package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// maxEvidence bounds the evidence file obiectl reads; obied rejects larger
// requests anyway.
const maxEvidence = 1 << 20

// eventIDPattern matches an event ID, as opposed to an address or range.
var eventIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func runReport(ctx context.Context, client *admin.Client, args []string, stdout, stderr io.Writer) int {
	const program = "obiectl report"
	fs := newFlagSet(program, stderr)
	protocol := fs.String("protocol", "", "attacked `service`, e.g. ssh (required)")
	reason := fs.String("reason", "", "behavior `class`, e.g. password_bruteforce (required)")
	events := fs.Int64("events", 1, "`number` of malicious events observed")
	evidence := fs.String("evidence-file", "", "`file` with the log lines behind the report (- for stdin); only their SHA-256 hash leaves this host")
	confidence := fs.Float64("confidence", 0.8, "confidence in [0, 1]")
	ttl := fs.String("ttl", "", "verdict `lifetime`, e.g. 12h or 7d (default decision.default_ttl, capped at decision.max_ttl)")
	action := fs.String("action", obieproto.ActionBan, "suggested `action`: ban or watch")
	mitre := fs.String("mitre", "", "comma-separated MITRE ATT&CK technique `IDs`, e.g. T1110")
	asJSON := fs.Bool("json", false, "print the response as JSON")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(fs.Output(), "Usage: %s --protocol <service> --reason <class> [flags] <ip | cidr>\n\nFlags:\n", program)
		fs.PrintDefaults()
	}
	target, code, done := parseTarget(fs, program, args, stderr)
	if done {
		return code
	}
	req := admin.ReportRequest{Protocol: *protocol, Reason: *reason, Events: *events, Action: *action}
	if strings.Contains(target, "/") {
		req.CIDR = target
	} else {
		req.IP = target
	}
	if flagSet(fs, "confidence") {
		req.Confidence = confidence
	}
	if *ttl != "" {
		d, err := config.ParseDuration(*ttl)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "%s: --ttl: %v\n", program, err)
			return ExitUsage
		}
		req.TTL = admin.TTL(d)
	}
	if *mitre != "" {
		for id := range strings.SplitSeq(*mitre, ",") {
			req.MITRE = append(req.MITRE, strings.TrimSpace(id))
		}
	}
	if *evidence != "" {
		lines, err := readEvidence(*evidence)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "%s: --evidence-file: %v\n", program, err)
			return ExitFailure
		}
		req.EvidenceLines = lines
	}

	resp, err := client.Report(ctx, req)
	if err != nil {
		reportClientError(stderr, err)
		return ExitFailure
	}
	if *asJSON {
		err = writeJSON(stdout, resp)
	} else {
		err = writeReport(stdout, resp)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "obiectl: writing report: %v\n", err)
		return ExitIOError
	}
	return ExitOK
}

func runRevoke(ctx context.Context, client *admin.Client, args []string, stdout, stderr io.Writer) int {
	const program = "obiectl revoke"
	fs := newFlagSet(program, stderr)
	reason := fs.String("reason", "false_positive", "why the verdict is withdrawn, e.g. false_positive")
	asJSON := fs.Bool("json", false, "print the revocations as JSON")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(fs.Output(), "Usage: %s [--reason <reason>] [--json] <event id | ip | cidr>\n\n"+
			"Revokes this node's own active verdict with that ID, or on that address or range.\n\nFlags:\n", program)
		fs.PrintDefaults()
	}
	target, code, done := parseTarget(fs, program, args, stderr)
	if done {
		return code
	}
	req := admin.RevocationRequest{Reason: *reason}
	if eventIDPattern.MatchString(target) {
		req.EventID = strings.ToLower(target)
	} else {
		req.Indicator = target
	}
	resp, err := client.Revoke(ctx, req)
	if err != nil {
		reportClientError(stderr, err)
		return ExitFailure
	}
	if *asJSON {
		err = writeJSON(stdout, resp)
	} else {
		err = writeRevocations(stdout, resp.Revocations)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "obiectl: writing revocations: %v\n", err)
		return ExitIOError
	}
	return ExitOK
}

func runIndicators(ctx context.Context, client *admin.Client, args []string, stdout, stderr io.Writer) int {
	const program = "obiectl indicators"
	fs := newFlagSet(program, stderr)
	asJSON := fs.Bool("json", false, "print the indicators as JSON")
	publisher := fs.String("publisher", "", "list only indicators with a verdict by this `peer ID`")
	mine := fs.Bool("mine", false, "list only indicators with a verdict by this node")
	limit := fs.Int("limit", 0, "page `size` (default 100, at most 1000)")
	cursor := fs.String("cursor", "", "continue after this `cursor` from the previous page")
	if code, done := parseCommand(fs, program, args, stderr); done {
		return code
	}
	if *mine && *publisher != "" {
		_, _ = fmt.Fprintf(stderr, "%s: give only one of --mine and --publisher\n", program)
		return ExitUsage
	}
	if *mine {
		id, err := client.Identity(ctx)
		if err != nil {
			reportClientError(stderr, err)
			return ExitFailure
		}
		*publisher = id.PeerID
	}
	resp, err := client.Indicators(ctx, admin.IndicatorsQuery{Publisher: *publisher, Limit: *limit, Cursor: *cursor})
	if err != nil {
		reportClientError(stderr, err)
		return ExitFailure
	}
	if *asJSON {
		err = writeJSON(stdout, resp)
	} else {
		err = writeIndicatorsTable(stdout, resp)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "obiectl: writing indicators: %v\n", err)
		return ExitIOError
	}
	return ExitOK
}

func runShow(ctx context.Context, client *admin.Client, args []string, stdout, stderr io.Writer) int {
	const program = "obiectl show"
	fs := newFlagSet(program, stderr)
	asJSON := fs.Bool("json", false, "print the verdicts as JSON")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(fs.Output(), "Usage: %s [--json] <ip | cidr | indicator key>\n\nFlags:\n", program)
		fs.PrintDefaults()
	}
	target, code, done := parseTarget(fs, program, args, stderr)
	if done {
		return code
	}
	resp, err := client.Indicator(ctx, target)
	if err != nil {
		reportClientError(stderr, err)
		return ExitFailure
	}
	if *asJSON {
		err = writeJSON(stdout, resp)
	} else {
		err = writeIndicator(stdout, resp)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "obiectl: writing verdicts: %v\n", err)
		return ExitIOError
	}
	return ExitOK
}

// parseTarget parses a command's flags, which may also follow its single
// positional argument, and returns that argument.
func parseTarget(fs *flag.FlagSet, program string, args []string, stderr io.Writer) (target string, code int, done bool) {
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

// flagSet reports whether the flag name was given on the command line.
func flagSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) { set = set || f.Name == name })
	return set
}

// readEvidence reads the log lines of path, or of stdin for "-". Empty
// trailing lines are dropped.
func readEvidence(path string) ([]string, error) {
	var r io.Reader = os.Stdin
	if path != "-" {
		f, err := os.Open(path) // #nosec G304 -- the operator names the evidence file.
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		r = f
	}
	data, err := io.ReadAll(io.LimitReader(r, maxEvidence+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxEvidence {
		return nil, fmt.Errorf("larger than %d bytes; pass only the relevant log excerpt", maxEvidence)
	}
	text := strings.TrimRight(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if text == "" {
		return nil, errors.New("contains no log lines")
	}
	return strings.Split(text, "\n"), nil
}

// writeReport prints the outcome of a report and the verdict.
func writeReport(w io.Writer, r *admin.ReportResponse) error {
	ev := r.Event
	var headline string
	switch {
	case r.Coalesced:
		headline = fmt.Sprintf("Coalesced: this node reported %s less than a minute ago; the events are added to the next refresh of verdict %s.",
			ev.Key(), ev.ID)
	case r.Supersedes != "":
		headline = fmt.Sprintf("Refreshed the verdict on %s: %s replaces %s.", ev.Key(), ev.ID, r.Supersedes)
	default:
		headline = fmt.Sprintf("Reported %s: verdict %s issued and published.", ev.Key(), ev.ID)
	}
	if _, err := fmt.Fprintf(w, "%s\n\n", headline); err != nil {
		return err
	}
	return writeVerdictDetails(w, ev)
}

// writeVerdictDetails prints the fields of a verdict, one per line.
func writeVerdictDetails(w io.Writer, ev *obieproto.Event) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "Indicator:\t%s\n", ev.Key())
	if ev.Verdict != nil {
		_, _ = fmt.Fprintf(tw, "Action:\t%s\n", ev.Verdict.SuggestedAction)
		_, _ = fmt.Fprintf(tw, "Confidence:\t%s\n", formatScore(ev.Verdict.Confidence))
	}
	_, _ = fmt.Fprintf(tw, "Protocol:\t%s\n", ev.Protocol)
	if ev.Evidence != nil {
		_, _ = fmt.Fprintf(tw, "Reason:\t%s\n", ev.Evidence.Reason)
		_, _ = fmt.Fprintf(tw, "Events:\t%d\n", ev.Evidence.Events)
		_, _ = fmt.Fprintf(tw, "Log hash:\t%s\n", orDash(ev.Evidence.LogHash))
	}
	if len(ev.MITRE) > 0 {
		_, _ = fmt.Fprintf(tw, "MITRE:\t%s\n", strings.Join(ev.MITRE, ", "))
	}
	_, _ = fmt.Fprintf(tw, "Issued:\t%s\n", formatTime(ev.IssuedAt.Time))
	_, _ = fmt.Fprintf(tw, "Expires:\t%s (%s)\n", formatTime(ev.ExpiresAt()), ttlString(ev))
	_, _ = fmt.Fprintf(tw, "Publisher:\t%s\n", ev.Publisher.PeerID)
	return tw.Flush()
}

// writeRevocations prints one line per revocation.
func writeRevocations(w io.Writer, revs []*obieproto.Event) error {
	for _, r := range revs {
		if _, err := fmt.Fprintf(w, "Revoked verdict %s on %s (revocation %s, reason %s).\n", r.Revokes, r.Key(), r.ID, r.Reason); err != nil {
			return err
		}
	}
	return nil
}

// writeIndicatorsTable prints one row per active verdict, grouped by
// indicator in the order the daemon sends them (sorted by key).
func writeIndicatorsTable(w io.Writer, resp *admin.IndicatorsResponse) error {
	if len(resp.Indicators) == 0 {
		_, err := fmt.Fprintln(w, "No active verdicts.")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "INDICATOR\tPUBLISHER\tACTION\tCONFIDENCE\tEVENTS\tPROTOCOL\tREASON\tEXPIRES\n")
	for _, ind := range resp.Indicators {
		for _, v := range ind.Verdicts {
			ev := v.Event
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\n", ind.Indicator.Key(), publisherName(v),
				ev.Verdict.SuggestedAction, formatScore(ev.Verdict.Confidence), ev.Evidence.Events, ev.Protocol,
				ev.Evidence.Reason, formatTime(ev.ExpiresAt()))
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if resp.NextCursor != "" {
		_, err := fmt.Fprintf(w, "\nMore indicators follow: obiectl indicators --cursor %s\n", resp.NextCursor)
		return err
	}
	return nil
}

// writeIndicator prints the active verdicts on one indicator.
func writeIndicator(w io.Writer, resp *admin.IndicatorResponse) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "Indicator:\t%s\n", resp.Indicator.Key())
	_, _ = fmt.Fprintf(tw, "Active verdicts:\t%d\n", len(resp.Verdicts))
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(resp.Verdicts) == 0 {
		_, err := fmt.Fprintln(w, "\nNo active verdicts.")
		return err
	}
	_, _ = fmt.Fprintf(tw, "\nPUBLISHER\tACTION\tCONFIDENCE\tEVENTS\tPROTOCOL\tREASON\tISSUED\tEXPIRES\tEVENT ID\n")
	for _, v := range resp.Verdicts {
		ev := v.Event
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\n", publisherName(v), ev.Verdict.SuggestedAction,
			formatScore(ev.Verdict.Confidence), ev.Evidence.Events, ev.Protocol, ev.Evidence.Reason,
			formatTime(ev.IssuedAt.Time), formatTime(ev.ExpiresAt()), ev.ID)
	}
	return tw.Flush()
}

func publisherName(v admin.VerdictResponse) string {
	if v.Local {
		return "(this node)"
	}
	return v.Event.Publisher.PeerID
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// ttlString formats a verdict's TTL like the configuration does, e.g. "7d".
func ttlString(ev *obieproto.Event) string {
	if ev.Verdict == nil {
		return "-"
	}
	return config.Duration(time.Duration(ev.Verdict.TTLSeconds) * time.Second).String()
}

// apiErrorHint explains common admin API refusals to the operator.
func apiErrorHint(e *admin.APIError) string {
	switch e.StatusCode {
	case http.StatusForbidden:
		return "run obiectl as root or as a member of the admin socket's group"
	case http.StatusUnprocessableEntity:
		return "nothing was published"
	default:
		return ""
	}
}
