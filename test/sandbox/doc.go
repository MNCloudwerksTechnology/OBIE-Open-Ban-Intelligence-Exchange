// Package sandbox keeps the sandbox of packaging/sandbox and its
// walkthrough, documentation/sandbox.md, honest (ADR 0029). Its plain tests
// run with `make test`, without Docker: the configurations sandbox-init
// writes, the Compose file's hardening, the script's messages against the
// walkthrough's troubleshooting section, and the walkthrough's structure.
// The test behind the build tag sandbox, `make sandbox-check`, runs every
// step of the walkthrough against a real sandbox and compares the output.
package sandbox
