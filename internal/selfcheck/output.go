package selfcheck

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// labels are the status labels of the text report: words, so that nothing
// depends on color.
var labels = map[Status]string{OK: "OK", Warning: "WARNING", Problem: "PROBLEM"}

// WriteText writes the report for people: a line per check with its
// status, followed by its details and next steps, and the result.
func WriteText(w io.Writer, r Report) error {
	var b strings.Builder
	as := r.User
	if r.Root {
		as = "root"
	}
	fmt.Fprintf(&b, "OBIE self-check of %s (obied %s, as %s)\n\n", r.Config, r.Version, as)
	if !r.Root {
		fmt.Fprintf(&b, "Note: this runs as %s, not as root, so some checks cannot look everywhere and\n"+
			"say so. For a complete check: sudo obied self-check\n\n", r.User)
	}
	width := 0
	for _, c := range r.Checks {
		width = max(width, len(c.Name))
	}
	indent := strings.Repeat(" ", 9+width+2)
	for _, c := range r.Checks {
		fmt.Fprintf(&b, "%-8s %-*s  %s\n", labels[c.Status], width, c.Name, c.Summary)
		for _, d := range c.Details {
			fmt.Fprintf(&b, "%s- %s\n", indent, d)
		}
		for _, n := range c.NextSteps {
			fmt.Fprintf(&b, "%sNext: %s\n", indent, n)
		}
	}
	fmt.Fprintf(&b, "\nResult: %s, %s, %d OK. %s\n",
		plural(r.Summary[Problem], "problem"), plural(r.Summary[Warning], "warning"), r.Summary[OK], verdict(r.Status))
	_, err := io.WriteString(w, b.String())
	return err
}

// verdict sums up the report in a sentence.
func verdict(s Status) string {
	switch s {
	case Problem:
		return "Fix the problems, then run the self-check again."
	case Warning:
		return "No problems; read the warnings."
	default:
		return "Everything checked is fine."
	}
}

// WriteJSON writes the report as indented JSON.
func WriteJSON(w io.Writer, r Report) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", data)
	return err
}
