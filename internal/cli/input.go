package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// addressExamples says what an address or range looks like.
const addressExamples = "an IPv4 or IPv6 address such as 203.0.113.7 or 2001:db8::7, or a range in CIDR notation such as 203.0.113.0/24"

// checkAddress reports whether s is an address, a range or an indicator
// key; otherwise it explains the mistake of program on stderr. obiectl
// checks what it can before it asks the node.
func checkAddress(program, s string, stderr io.Writer) bool {
	if _, err := admin.ParseIndicator(s); err != nil {
		problem{id: "invalid-address", what: fmt.Sprintf("%q is not an IP address or range", s),
			next: []string{"give " + addressExamples}}.write(stderr, program)
		return false
	}
	return true
}

// parseAddressArg parses the flags of a command that takes one address or
// range and checks it; see parseFlags.
func parseAddressArg(fs *flag.FlagSet, args []string, stdout, stderr io.Writer) (arg string, code int, done bool) {
	arg, code, done = parseOneArg(fs, args, "address or range", stdout, stderr)
	if !done && !checkAddress(fs.Name(), arg, stderr) {
		return "", ExitUsage, true
	}
	return arg, code, done
}

// checkRevokeTarget reports whether s is an event ID, an address or a
// range; otherwise it explains the mistake on stderr.
func checkRevokeTarget(program, s string, stderr io.Writer) bool {
	if eventIDPattern.MatchString(s) {
		return true
	}
	if _, err := admin.ParseIndicator(s); err != nil {
		problem{id: "invalid-address", what: fmt.Sprintf("%q is neither an event ID nor an IP address or range", s),
			next: []string{"give the event ID of a verdict of this node, such as 1b4e28ba-2fa1-41d2-883f-0016d3cca427 " +
				"(sudo obiectl show <address> lists them), or " + addressExamples}}.write(stderr, program)
		return false
	}
	return true
}

// reportFlagMistake checks the flags of a report before it is sent, so that a
// mistake names the flag; it returns "" if they are fine.
func reportFlagMistake(req admin.ReportRequest, confidence float64) string {
	switch {
	case req.Protocol == "":
		return "--protocol is missing: name the attacked service, e.g. --protocol ssh"
	case req.Reason == "":
		return "--reason is missing: name what the attacker did, e.g. --reason password_bruteforce"
	case req.Events < 1:
		return fmt.Sprintf("--events must be at least 1, got %d", req.Events)
	case !(confidence >= 0 && confidence <= 1):
		return fmt.Sprintf("--confidence must be a number from 0 to 1, got %v", confidence)
	case req.Action != obieproto.ActionBan && req.Action != obieproto.ActionWatch:
		return fmt.Sprintf("--action must be %s or %s, got %q", obieproto.ActionBan, obieproto.ActionWatch, req.Action)
	}
	return ""
}
