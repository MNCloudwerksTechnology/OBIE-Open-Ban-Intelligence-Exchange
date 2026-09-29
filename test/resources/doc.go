// Package resources measures what one OBIE node costs to run: its memory,
// CPU time and disk in a mesh of real obied processes, idle, while it
// receives verdicts and at rest. Its test carries the build tag
// resources, so `make test` never runs it; `make resources` does, and the
// results are recorded in documentation/operations/performance.md.
package resources
