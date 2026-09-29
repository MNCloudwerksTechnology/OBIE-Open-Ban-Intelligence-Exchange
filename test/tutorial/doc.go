// Package tutorial keeps the getting-started tutorial,
// documentation/getting-started.md, honest (ADR 0030). Its plain tests run
// with `make test`, without Docker: the page's form (the steps in order,
// each with its purpose, expected output and way to troubleshooting, only
// blocks the check runs) and how the check reads and compares output. The
// test behind the build tag tutorial, `make tutorial-check`, runs every
// command of the page on a systemd host in a container against the release
// built from this tree and compares the output with the page.
package tutorial
