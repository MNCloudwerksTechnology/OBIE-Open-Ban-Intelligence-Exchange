package cli

import (
	"fmt"
	"io"
	"strings"
)

// problem is an error message for people. It says what went wrong, why
// when that is known, and what to do next, each next step on a line of
// its own, like the self-check's findings:
//
//	obiectl: obied is not running: there is no admin socket /run/obie/obie.sock
//	  Why:  the node was not started, has stopped, or uses another socket
//	  Next: start it: sudo systemctl start obied
//
// id names the message in the message inventory,
// documentation/operations/messages.md; TestMessageInventory keeps the two
// in step, so id must be a string literal.
type problem struct {
	id   string
	what string
	why  string
	next []string
}

// write writes p as a message of program to w. A failing w cannot be
// reported anywhere else.
func (p problem) write(w io.Writer, program string) {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s\n", program, p.what)
	if p.why != "" {
		fmt.Fprintf(&b, "  Why:  %s\n", p.why)
	}
	for _, next := range p.next {
		fmt.Fprintf(&b, "  Next: %s\n", next)
	}
	_, _ = io.WriteString(w, b.String())
}

// usageProblem is a usage error of program: what is wrong, and where its
// help is.
func usageProblem(program, what string) problem {
	return problem{id: "usage", what: what, next: []string{"see how to use it: " + program + " --help"}}
}
