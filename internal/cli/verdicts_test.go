package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

var reportedAt = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

func reportedVerdict() *obieproto.Event {
	return &obieproto.Event{
		ID: "01a0e7e2-de00-7000-8000-000000000001", Spec: obieproto.Spec, Type: obieproto.TypeVerdict,
		IssuedAt:  obieproto.NewTimestamp(reportedAt),
		Indicator: obieproto.Indicator{Kind: obieproto.KindIPv4, Value: "85.10.0.7", Scope: "/32"},
		Protocol:  "ssh",
		Evidence:  &obieproto.Evidence{Events: 5, Reason: "password_bruteforce", LogHash: "sha256:" + strings.Repeat("ab", 32)},
		Verdict:   &obieproto.Verdict{SuggestedAction: obieproto.ActionBan, Confidence: 0.8, TTLSeconds: 7 * 86400},
		MITRE:     []string{"T1110"},
		Publisher: obieproto.Publisher{PeerID: "12D3KooWSSS"},
	}
}

func TestWriteReport(t *testing.T) {
	details := `Indicator:   ipv4:85.10.0.7
Action:      ban
Confidence:  0.8
Protocol:    ssh
Reason:      password_bruteforce
Events:      5
Log hash:    sha256:` + strings.Repeat("ab", 32) + `
MITRE:       T1110
Issued:      2026-09-28T12:00:00Z
Expires:     2026-10-05T12:00:00Z (7d)
Publisher:   12D3KooWSSS
`
	for name, tc := range map[string]struct {
		resp     admin.ReportResponse
		headline string
	}{
		"issued":    {admin.ReportResponse{Event: reportedVerdict()}, "Reported ipv4:85.10.0.7: verdict 01a0e7e2-de00-7000-8000-000000000001 issued and published."},
		"refreshed": {admin.ReportResponse{Event: reportedVerdict(), Supersedes: "01a0e7e2-0000-7000-8000-000000000000"}, "Refreshed the verdict on ipv4:85.10.0.7: 01a0e7e2-de00-7000-8000-000000000001 replaces 01a0e7e2-0000-7000-8000-000000000000."},
		"coalesced": {admin.ReportResponse{Event: reportedVerdict(), Coalesced: true}, "Coalesced: this node reported ipv4:85.10.0.7 less than a minute ago; the events are added to the next refresh of verdict 01a0e7e2-de00-7000-8000-000000000001."},
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if err := writeReport(&out, &tc.resp); err != nil {
				t.Fatal(err)
			}
			if want := tc.headline + "\n\n" + details; out.String() != want {
				t.Errorf("report =\n%s\nwant\n%s", out.String(), want)
			}
		})
	}
}

func TestWriteIndicators(t *testing.T) {
	local := reportedVerdict()
	foreign := reportedVerdict()
	foreign.Publisher.PeerID = "12D3KooWAAA"
	foreign.Verdict.SuggestedAction = obieproto.ActionWatch
	foreign.Verdict.Confidence = 0.55
	ind := admin.IndicatorResponse{Indicator: local.Indicator, Verdicts: []admin.VerdictResponse{{Event: foreign}, {Local: true, Event: local}}}

	var out bytes.Buffer
	if err := writeIndicatorsTable(&out, &admin.IndicatorsResponse{Indicators: []admin.IndicatorResponse{ind}, NextCursor: "ipv4:85.10.0.7"}); err != nil {
		t.Fatal(err)
	}
	want := `INDICATOR       PUBLISHER    ACTION  CONFIDENCE  EVENTS  PROTOCOL  REASON               EXPIRES
ipv4:85.10.0.7  12D3KooWAAA  watch   0.55        5       ssh       password_bruteforce  2026-10-05T12:00:00Z
ipv4:85.10.0.7  (this node)  ban     0.8         5       ssh       password_bruteforce  2026-10-05T12:00:00Z

More indicators follow: obiectl indicators --cursor ipv4:85.10.0.7
`
	if out.String() != want {
		t.Errorf("indicators =\n%s\nwant\n%s", out.String(), want)
	}
	out.Reset()
	if err := writeIndicatorsTable(&out, &admin.IndicatorsResponse{}); err != nil || out.String() != "No active verdicts.\n" {
		t.Errorf("empty indicators = %q, %v", out.String(), err)
	}

	out.Reset()
	if err := writeIndicator(&out, &ind); err != nil {
		t.Fatal(err)
	}
	want = `Indicator:        ipv4:85.10.0.7
Active verdicts:  2

PUBLISHER    ACTION  CONFIDENCE  EVENTS  PROTOCOL  REASON               ISSUED                EXPIRES               EVENT ID
12D3KooWAAA  watch   0.55        5       ssh       password_bruteforce  2026-09-28T12:00:00Z  2026-10-05T12:00:00Z  01a0e7e2-de00-7000-8000-000000000001
(this node)  ban     0.8         5       ssh       password_bruteforce  2026-09-28T12:00:00Z  2026-10-05T12:00:00Z  01a0e7e2-de00-7000-8000-000000000001
`
	if out.String() != want {
		t.Errorf("indicator =\n%s\nwant\n%s", out.String(), want)
	}
	out.Reset()
	if err := writeIndicator(&out, &admin.IndicatorResponse{Indicator: local.Indicator}); err != nil ||
		!strings.HasSuffix(out.String(), "Active verdicts:  0\n\nNo active verdicts.\n") {
		t.Errorf("indicator without verdicts = %q, %v", out.String(), err)
	}
}

func TestWriteRevocations(t *testing.T) {
	rev := &obieproto.Event{ID: "01a0e7e2-de00-7000-8000-000000000009", Type: obieproto.TypeRevoke,
		Indicator: obieproto.Indicator{Kind: obieproto.KindIPv4, Value: "85.10.0.7", Scope: "/32"},
		Revokes:   "01a0e7e2-de00-7000-8000-000000000001", Reason: "false_positive"}
	var out bytes.Buffer
	if err := writeRevocations(&out, []*obieproto.Event{rev}); err != nil {
		t.Fatal(err)
	}
	want := "Revoked verdict 01a0e7e2-de00-7000-8000-000000000001 on ipv4:85.10.0.7 (revocation 01a0e7e2-de00-7000-8000-000000000009, reason false_positive).\n"
	if out.String() != want {
		t.Errorf("revocations = %q, want %q", out.String(), want)
	}
}

func TestReadEvidence(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	lines, err := readEvidence(write("log", "first\r\nsecond\n\n"))
	if err != nil || len(lines) != 2 || lines[0] != "first" || lines[1] != "second" {
		t.Errorf("readEvidence = %q, %v", lines, err)
	}
	if _, err := readEvidence(write("empty", "\n\n")); err == nil {
		t.Error("empty evidence file: no error")
	}
	if _, err := readEvidence(write("big", strings.Repeat("x", maxEvidence+1))); err == nil {
		t.Error("oversized evidence file: no error")
	}
	if _, err := readEvidence(filepath.Join(dir, "missing")); err == nil {
		t.Error("missing evidence file: no error")
	}
}

// withStdin runs f with os.Stdin reading input.
func withStdin(t *testing.T, input string, f func()) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(path) // #nosec G304 -- a file of the test's temporary directory.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = in.Close() }()
	saved := os.Stdin
	os.Stdin = in
	defer func() { os.Stdin = saved }()
	f()
}

func TestVerdictCommandsUsage(t *testing.T) {
	for name, tc := range map[string]struct {
		args   []string
		code   int
		stderr string
	}{
		"report no target":        {[]string{"report", "--protocol", "ssh", "--reason", "x"}, ExitUsage, "missing the address or range"},
		"report two targets":      {[]string{"report", "85.10.0.7", "85.10.0.8"}, ExitUsage, "expects one address or range, got 2 arguments"},
		"report bad ttl":          {[]string{"report", "--protocol", "ssh", "--reason", "x", "--ttl", "soon", "85.10.0.7"}, ExitUsage, "--ttl: invalid duration"},
		"report bad flag":         {[]string{"report", "--user", "root", "85.10.0.7"}, ExitUsage, "unknown flag --user"},
		"report missing file":     {[]string{"report", "--protocol", "ssh", "--reason", "x", "--evidence-file", "/nonexistent/log", "85.10.0.7"}, ExitFailure, "cannot use the evidence of --evidence-file: open /nonexistent/log"},
		"report no protocol":      {[]string{"report", "--reason", "x", "85.10.0.7"}, ExitUsage, "--protocol is missing: name the attacked service, e.g. --protocol ssh"},
		"report no reason":        {[]string{"report", "--protocol", "ssh", "85.10.0.7"}, ExitUsage, "--reason is missing"},
		"report no events":        {[]string{"report", "--protocol", "ssh", "--reason", "x", "--events", "0", "85.10.0.7"}, ExitUsage, "--events must be at least 1, got 0"},
		"report bad confidence":   {[]string{"report", "--protocol", "ssh", "--reason", "x", "--confidence", "1.5", "85.10.0.7"}, ExitUsage, "--confidence must be a number from 0 to 1, got 1.5"},
		"report bad action":       {[]string{"report", "--protocol", "ssh", "--reason", "x", "--action", "nuke", "85.10.0.7"}, ExitUsage, `--action must be ban or watch, got "nuke"`},
		"report not an address":   {[]string{"report", "--protocol", "ssh", "--reason", "x", "85.10.0"}, ExitUsage, `"85.10.0" is not an IP address or range`},
		"revoke not a target":     {[]string{"revoke", "1b4e28ba"}, ExitUsage, `"1b4e28ba" is neither an event ID nor an IP address or range`},
		"revoke too broad":        {[]string{"revoke", "85.10.0.0/15"}, ExitUsage, `"85.10.0.0/15" is broader than /16`},
		"report single cidr":      {[]string{"report", "--protocol", "ssh", "--reason", "x", "cidr:85.10.0.7/32"}, ExitUsage, `"85.10.0.7/32" is a single address`},
		"show not an address":     {[]string{"show", "example.org"}, ExitUsage, `"example.org" is not an IP address or range`},
		"report ip and argument":  {[]string{"report", "--ip", "85.10.0.7", "85.10.0.8"}, ExitUsage, "either with --ip or as the argument"},
		"report two evidences":    {[]string{"report", "--evidence-file", "log", "--evidence-from-stdin", "--ip", "85.10.0.7"}, ExitUsage, "only one of --evidence-file and --evidence-from-stdin"},
		"report --ip not running": {[]string{"report", "--ip", "85.10.0.7", "--protocol", "ssh", "--reason", "x"}, ExitFailure, "obied is not running"},
		"zero timeout":            {[]string{"--timeout", "0s", "status"}, ExitUsage, "--timeout must be positive"},
		"report not running":      {[]string{"report", "85.10.0.7", "--protocol", "ssh", "--reason", "x"}, ExitFailure, "obied is not running"},
		"revoke no target":        {[]string{"revoke"}, ExitUsage, "missing the event ID, address or range"},
		"revoke not running":      {[]string{"revoke", "85.10.0.7"}, ExitFailure, "obied is not running"},
		"show no target":          {[]string{"show"}, ExitUsage, "missing the address or range"},
		"show not running":        {[]string{"show", "85.10.0.7"}, ExitFailure, "obied is not running"},
		"indicators arg":          {[]string{"indicators", "85.10.0.7"}, ExitUsage, `unexpected argument "85.10.0.7"`},
		"indicators both":         {[]string{"indicators", "--mine", "--publisher", "12D3KooWAAA"}, ExitUsage, "only one of --mine and --publisher"},
		"indicators not runnin":   {[]string{"indicators"}, ExitFailure, "obied is not running"},
	} {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			args := append([]string{"--socket", "/nonexistent/obie.sock"}, tc.args...)
			if code := RunCtl(args, &stdout, &stderr); code != tc.code || !strings.Contains(stderr.String(), tc.stderr) {
				t.Errorf("exit code = %d, stderr %q; want %d containing %q", code, stderr.String(), tc.code, tc.stderr)
			}
		})
	}
}

// TestObiectlVerdictsAgainstInProcessDaemon reports, inspects and revokes
// verdicts through obiectl against obied running in-process.
func TestObiectlVerdictsAgainstInProcessDaemon(t *testing.T) {
	n := newTestNodeWith(t, "", "mesh:\n  listen: [/ip4/127.0.0.1/tcp/0]\nallowlist:\n  cidrs: [85.20.0.0/16]\n"+
		"decision:\n  default_ttl: 2d\n  max_ttl: 3d\n")
	var stderr bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exit := startDaemon(ctx, t, n, &stderr, func(ctx context.Context, args []string) int {
		return runDaemon(ctx, args, &bytes.Buffer{}, &stderr)
	})
	ctl := func(wantCode int, args ...string) (string, string) {
		t.Helper()
		var stdout, ctlStderr bytes.Buffer
		if code := RunCtl(append([]string{"--socket", n.socket}, args...), &stdout, &ctlStderr); code != wantCode {
			t.Fatalf("obiectl %v: exit code = %d, want %d; stderr %q", args, code, wantCode, ctlStderr.String())
		}
		return stdout.String(), ctlStderr.String()
	}

	evidence := filepath.Join(t.TempDir(), "auth.log")
	if err := os.WriteFile(evidence, []byte("Sep 28 12:00:01 host sshd[1]: Failed password for secret-user from 85.10.0.7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _ := ctl(ExitOK, "report", "--protocol", "ssh", "--reason", "password_bruteforce", "--events", "5",
		"--evidence-file", evidence, "--mitre", "T1110, T1110.001", "85.10.0.7")
	for _, want := range []string{"Reported ipv4:85.10.0.7: verdict ", "Action:      ban\n", "Confidence:  0.8\n",
		"Events:      5\n", "Log hash:    sha256:", "MITRE:       T1110, T1110.001\n", "(2d)\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("report output lacks %q:\n%s", want, out)
		}
	}

	var coalesced admin.ReportResponse
	out, _ = ctl(ExitOK, "report", "85.10.0.7", "--protocol", "ssh", "--reason", "password_bruteforce", "--json")
	if err := json.Unmarshal([]byte(out), &coalesced); err != nil || !coalesced.Coalesced {
		t.Errorf("repeated report = %s, %v", out, err)
	}

	var watch admin.ReportResponse
	out, _ = ctl(ExitOK, "report", "--protocol", "http", "--reason", "scanning", "--action", "watch", "--confidence", "0",
		"--ttl", "30d", "--json", "85.10.1.0/24")
	if err := json.Unmarshal([]byte(out), &watch); err != nil || watch.Event.Verdict.Confidence != 0 ||
		watch.Event.Verdict.TTLSeconds != 3*86400 || watch.Event.Indicator.Key() != "cidr:85.10.1.0/24" {
		t.Errorf("watch report = %s, %v", out, err)
	}

	// As Fail2Ban's action calls it: target by --ip, matched lines on stdin.
	var piped admin.ReportResponse
	withStdin(t, "Sep 28 12:00:02 host sshd[2]: Failed password for secret-user from 85.10.0.9\n", func() {
		out, _ = ctl(ExitOK, "--timeout", "5s", "report", "--ip", "85.10.0.9", "--protocol", "ssh", "--reason", "bruteforce",
			"--events", "3", "--ttl", "600s", "--evidence-from-stdin", "--json")
	})
	if err := json.Unmarshal([]byte(out), &piped); err != nil || piped.Event.Indicator.Key() != "ipv4:85.10.0.9" ||
		piped.Event.Evidence.Events != 3 || !strings.HasPrefix(piped.Event.Evidence.LogHash, "sha256:") ||
		piped.Event.Verdict.TTLSeconds != 600 {
		t.Errorf("report with --ip and --evidence-from-stdin = %s, %v", out, err)
	}

	_, errOut := ctl(ExitFailure, "report", "--protocol", "ssh", "--reason", "x", "192.168.1.9")
	if !strings.HasPrefix(errOut, "obiectl report: nothing was reported: ipv4:192.168.1.9 is not a public address: "+
		"192.168.1.9/32 overlaps special-purpose range 192.168.0.0/16\n  Why:  OBIE never reports private") ||
		!strings.Contains(errOut, "\n  Next: nothing needs to be done if this is right") {
		t.Errorf("private report stderr = %q", errOut)
	}
	_, errOut = ctl(ExitFailure, "report", "--protocol", "ssh", "--reason", "x", "85.20.0.1")
	if !strings.Contains(errOut, "nothing was reported: ipv4:85.20.0.1 overlaps the allow-listed network 85.20.0.0/16 (allowlist.cidrs)\n") ||
		!strings.Contains(errOut, "Next: if the address should not be protected, remove it from allowlist.cidrs") {
		t.Errorf("allow-listed report stderr = %q", errOut)
	}
	_, errOut = ctl(ExitUsage, "report", "--reason", "x", "85.10.0.8")
	if !strings.Contains(errOut, "--protocol is missing") {
		t.Errorf("report without protocol stderr = %q", errOut)
	}

	out, _ = ctl(ExitOK, "indicators")
	for _, want := range []string{"cidr:85.10.1.0/24  (this node)  watch   0", "ipv4:85.10.0.7     (this node)  ban     0.8"} {
		if !strings.Contains(out, want) {
			t.Errorf("indicators lack %q:\n%s", want, out)
		}
	}
	var page admin.IndicatorsResponse
	out, _ = ctl(ExitOK, "indicators", "--mine", "--limit", "1", "--json")
	if err := json.Unmarshal([]byte(out), &page); err != nil || len(page.Indicators) != 1 || page.NextCursor == "" {
		t.Errorf("first page = %s, %v", out, err)
	}
	out, _ = ctl(ExitOK, "indicators", "--cursor", page.NextCursor)
	if !strings.Contains(out, "ipv4:85.10.0.7") || strings.Contains(out, "cidr:") {
		t.Errorf("second page:\n%s", out)
	}

	out, _ = ctl(ExitOK, "show", "85.10.0.7")
	if !strings.Contains(out, "Active verdicts:  1\n") || !strings.Contains(out, "(this node)  ban") || strings.Contains(out, "secret-user") {
		t.Errorf("show output:\n%s", out)
	}

	out, _ = ctl(ExitOK, "revoke", "85.10.0.7")
	if !strings.Contains(out, "Revoked verdict ") || !strings.Contains(out, "on ipv4:85.10.0.7") || !strings.Contains(out, "reason false_positive") {
		t.Errorf("revoke output = %q", out)
	}
	_, errOut = ctl(ExitFailure, "revoke", "85.10.0.7")
	if errOut != "obiectl revoke: this node has no active verdict on ipv4:85.10.0.7\n"+
		"  Next: see what this node has reported: sudo obiectl --socket "+n.socket+" indicators --mine\n" {
		t.Errorf("second revoke stderr = %q", errOut)
	}
	var revs admin.RevocationsResponse
	out, _ = ctl(ExitOK, "revoke", "--reason", "operator_request", "--json", strings.ToUpper(watch.Event.ID))
	if err := json.Unmarshal([]byte(out), &revs); err != nil || len(revs.Revocations) != 1 || revs.Revocations[0].Revokes != watch.Event.ID {
		t.Errorf("revoke by event ID = %s, %v", out, err)
	}
	if out, _ := ctl(ExitOK, "show", "85.10.0.7"); !strings.Contains(out, "No active verdicts.") {
		t.Errorf("show after revocation:\n%s", out)
	}
	if strings.Contains(stderr.String(), "secret-user") {
		t.Errorf("obied logged raw evidence:\n%s", stderr.String())
	}

	cancel()
	if code := waitExit(t, exit, &stderr); code != ExitOK {
		t.Fatalf("obied exit code = %d:\n%s", code, stderr.String())
	}
}
