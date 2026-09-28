// Package e2e holds the end-to-end test of OBIE's core promise: several
// complete obied nodes in one process, each with its own configuration
// file, store, admin socket and libp2p host on 127.0.0.1, turn a report on
// one node into a block on another under trust-weighted consensus, and
// undo it on revocation. It runs with a plain `go test`; the variant
// behind the `privileged` build tag enforces with nftables, every node in
// its own network namespace. See ADR 0016.
package e2e
