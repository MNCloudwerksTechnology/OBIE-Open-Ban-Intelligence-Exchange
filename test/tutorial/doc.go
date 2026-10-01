// Package tutorial keeps the getting-started tutorial,
// documentation/getting-started.md, and the how-to guides in
// documentation/guides honest (ADR 0030, ADR 0031). Its plain tests run
// with `make test`, without Docker: the pages' form (the tutorial's steps
// in order, each with its purpose, expected output and way to
// troubleshooting; each guide's question, sections and way back; only
// blocks the check runs) and how the check reads and compares output. The
// tests behind the build tag tutorial, `make tutorial-check` and `make
// guides-check`, run every command of the pages on a systemd host in a
// container against the release built from this tree and compare the
// output with the pages.
package tutorial
