package cli

import (
	"context"
	"fmt"
	"io"
	"net"
	"text/tabwriter"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
)

func runConsole(ctx context.Context, client *admin.Client, args []string, stdout, stderr io.Writer) int {
	const program = "obiectl console"
	fs := newFlagSet(program, stderr)
	rotate := fs.Bool("rotate", false, "issue a new token: the old one stops working and every browser is signed out")
	asJSON := fs.Bool("json", false, "print the console and its token as JSON")
	if code, done := parseCommand(fs, program, args, stderr); done {
		return code
	}
	var resp *admin.ConsoleResponse
	var err error
	if *rotate {
		resp, err = client.RotateConsoleToken(ctx)
	} else {
		resp, err = client.Console(ctx)
	}
	if err != nil {
		reportClientError(stderr, err)
		return ExitFailure
	}
	if *asJSON {
		err = writeJSON(stdout, resp)
	} else {
		err = writeConsoleTable(stdout, resp)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "obiectl: writing the console: %v\n", err)
		return ExitIOError
	}
	if *rotate {
		_, _ = fmt.Fprintln(stderr, "obiectl: issued a new console token; the old one no longer works and every browser must sign in again")
	}
	if !*asJSON {
		_, _ = fmt.Fprintln(stderr, consoleHint(resp))
	}
	return ExitOK
}

// writeConsoleTable prints where the console is and the token to sign in
// with.
func writeConsoleTable(w io.Writer, c *admin.ConsoleResponse) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "Console:\t%s\n", consoleState(c))
	_, _ = fmt.Fprintf(tw, "Token:\t%s\n", c.Token)
	return tw.Flush()
}

func consoleState(c *admin.ConsoleResponse) string {
	switch {
	case !c.Enabled:
		return "disabled (console.enabled is false)"
	case c.URL != "":
		return "serving at " + c.URL
	case c.Error != "":
		return "not serving: " + c.Error
	default:
		return "enabled, not serving yet"
	}
}

// consoleHint tells the operator what to do next.
func consoleHint(c *admin.ConsoleResponse) string {
	switch {
	case !c.Enabled:
		return "obiectl: to switch the console on, set console.enabled: true in the configuration and reload obied (sudo systemctl reload obied)"
	case c.URL == "":
		return "obiectl: the console is switched on but not serving; fix the cause and reload obied (sudo systemctl reload obied)"
	}
	forward := c.Listen
	if host, port, err := net.SplitHostPort(c.Listen); err == nil {
		forward = port + ":" + net.JoinHostPort(host, port)
	}
	return fmt.Sprintf("obiectl: open %s in a browser on this host and sign in with the token. "+
		"From another machine, forward the port first: ssh -L %s <this host>, then open the same address there.", c.URL, forward)
}
