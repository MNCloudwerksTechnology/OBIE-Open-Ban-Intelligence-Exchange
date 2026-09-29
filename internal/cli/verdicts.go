package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// maxEvidence bounds the evidence file obiectl reads, well below obied's
// request body limit so that the JSON-encoded request still fits.
const maxEvidence = 512 << 10

// eventIDPattern matches an event ID, as opposed to an address or range.
var eventIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func runReport(ctx context.Context, client *admin.Client, args []string, stdout, stderr io.Writer) int {
	const program = "obiectl report"
	fs := newFlagSet(program)
	protocol := fs.String("protocol", "", "attacked `service`, e.g. ssh (required)")
	reason := fs.String("reason", "", "behavior `class`, e.g. password_bruteforce (required)")
	events := fs.Int64("events", 1, "`number` of malicious events observed")
	ip := fs.String("ip", "", "attacking `address` or range, in place of the argument (default: the argument)")
	evidence := fs.String("evidence-file", "", "`file` with the log lines behind the report (- for stdin); only their SHA-256 hash leaves this host (default: no evidence)")
	evidenceStdin := fs.Bool("evidence-from-stdin", false, "read the log lines behind the report from stdin, like --evidence-file -")
	confidence := fs.Float64("confidence", 0.8, "how sure you are, a `number` from 0 to 1")
	ttl := fs.String("ttl", "", "verdict `lifetime`, e.g. 12h or 7d (default decision.default_ttl, capped at decision.max_ttl)")
	action := fs.String("action", obieproto.ActionBan, "suggested `action`: ban or watch")
	mitre := fs.String("mitre", "", "comma-separated MITRE ATT&CK technique `IDs`, e.g. T1110 (default: none)")
	asJSON := fs.Bool("json", false, "print the response as JSON, for scripts")
	positional, code, done := parseFlags(fs, args, stdout, stderr)
	if done {
		return code
	}
	if *ip != "" {
		if len(positional) > 0 {
			usageProblem(program, "give the address either with --ip or as the argument, not both").write(stderr, program)
			return ExitUsage
		}
		positional = []string{*ip}
	}
	target, code, done := oneArg(program, positional, "address or range", stderr)
	if done {
		return code
	}
	if !checkAddress(program, target, stderr) {
		return ExitUsage
	}
	evidenceFlag := "--evidence-file"
	if *evidenceStdin {
		evidenceFlag = "--evidence-from-stdin"
		if *evidence != "" && *evidence != "-" {
			usageProblem(program, "give only one of --evidence-file and --evidence-from-stdin").write(stderr, program)
			return ExitUsage
		}
		*evidence = "-"
	}
	req := admin.ReportRequest{Protocol: *protocol, Reason: *reason, Events: *events, Action: *action}
	if strings.Contains(target, "/") {
		req.CIDR = target
	} else {
		req.IP = target
	}
	if msg := reportFlagMistake(req, *confidence); msg != "" {
		usageProblem(program, msg).write(stderr, program)
		return ExitUsage
	}
	if flagSet(fs, "confidence") {
		req.Confidence = confidence
	}
	if *ttl != "" {
		d, err := config.ParseDuration(*ttl)
		if err != nil {
			usageProblem(program, "--ttl: "+err.Error()).write(stderr, program)
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
			problem{id: "evidence-unreadable", what: fmt.Sprintf("cannot use the evidence of %s: %v", evidenceFlag, err),
				next: []string{"check the file, or pass the log lines about this address on standard input: " +
					"grep <address> <log file> | sudo obiectl report --evidence-from-stdin ..."}}.write(stderr, program)
			return ExitFailure
		}
		req.EvidenceLines = lines
	}

	resp, err := client.Report(ctx, req)
	if err != nil {
		reportClientError(stderr, program, client, err)
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
	fs := newFlagSet(program)
	reason := fs.String("reason", "false_positive", "why the verdict is withdrawn, e.g. false_positive")
	asJSON := fs.Bool("json", false, "print the revocations as JSON, for scripts")
	target, code, done := parseOneArg(fs, args, "event ID, address or range", stdout, stderr)
	if done {
		return code
	}
	if !checkRevokeTarget(program, target, stderr) {
		return ExitUsage
	}
	req := admin.RevocationRequest{Reason: *reason}
	if eventIDPattern.MatchString(target) {
		req.EventID = strings.ToLower(target)
	} else {
		req.Indicator = target
	}
	resp, err := client.Revoke(ctx, req)
	if err != nil {
		reportClientError(stderr, program, client, err)
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
	fs := newFlagSet(program)
	asJSON := fs.Bool("json", false, "print the indicators as JSON, for scripts")
	publisher := fs.String("publisher", "", "list only the verdicts of the publisher with this `peer ID` (default: every publisher)")
	mine := fs.Bool("mine", false, "list only the verdicts of this node")
	limit := fs.Int("limit", 0, "page `size` (default 100, at most 1000)")
	cursor := fs.String("cursor", "", "continue after this `cursor` from the previous page (default: the first page)")
	if code, done := parseNoArgs(fs, args, stdout, stderr); done {
		return code
	}
	if *mine && *publisher != "" {
		usageProblem(program, "give only one of --mine and --publisher").write(stderr, program)
		return ExitUsage
	}
	if *mine {
		id, err := client.Identity(ctx)
		if err != nil {
			reportClientError(stderr, program, client, err)
			return ExitFailure
		}
		*publisher = id.PeerID
	}
	resp, err := client.Indicators(ctx, admin.IndicatorsQuery{Publisher: *publisher, Limit: *limit, Cursor: *cursor})
	if err != nil {
		reportClientError(stderr, program, client, err)
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
	fs := newFlagSet(program)
	asJSON := fs.Bool("json", false, "print the verdicts as JSON, for scripts")
	target, code, done := parseAddressArg(fs, args, stdout, stderr)
	if done {
		return code
	}
	resp, err := client.Indicator(ctx, target)
	if err != nil {
		reportClientError(stderr, program, client, err)
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

// ttlString formats a verdict's TTL like the configuration does, e.g. "7d".
func ttlString(ev *obieproto.Event) string {
	if ev.Verdict == nil {
		return "-"
	}
	return config.Duration(time.Duration(ev.Verdict.TTLSeconds) * time.Second).String()
}
