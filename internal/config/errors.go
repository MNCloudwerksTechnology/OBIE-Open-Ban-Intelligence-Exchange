package config

import (
	"fmt"
	"strings"
)

// Problem is one configuration mistake at a YAML key path such as
// "trust.publishers[1].weight".
type Problem struct {
	Path string
	// Line is the 1-based line in the file, or 0 if unknown.
	Line    int
	Message string
}

func (p Problem) String() string {
	if p.Line > 0 {
		return fmt.Sprintf("%s (line %d): %s", p.Path, p.Line, p.Message)
	}
	return fmt.Sprintf("%s: %s", p.Path, p.Message)
}

// Error reports every problem found in a configuration.
type Error struct {
	Problems []Problem
}

func (e *Error) Error() string {
	lines := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		lines[i] = p.String()
	}
	return "invalid configuration:\n  " + strings.Join(lines, "\n  ")
}

// problems collects Problems while decoding or validating.
type problems []Problem

func (ps *problems) addf(path string, line int, format string, args ...any) {
	*ps = append(*ps, Problem{Path: path, Line: line, Message: fmt.Sprintf(format, args...)})
}

// err returns nil when no problem was recorded.
func (ps problems) err() error {
	if len(ps) == 0 {
		return nil
	}
	return &Error{Problems: ps}
}
