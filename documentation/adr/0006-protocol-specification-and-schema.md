# ADR 0006: Protocol specification, JSON Schema and requirement traceability

- **Status:** Accepted
- **Date:** 2026-09-27
- **Work package:** [#1653](https://openproject.niew.dev/work_packages/1653)

## Context

A third party must be able to build an interoperable obie/0.1 node from the
documentation alone. Until now the rules lived in `pkg/obieproto` (code and
doc comments), ADR 0004 and the test-vector README. A written specification
drifts from code unless something checks it, and a JSON Schema is only
useful if it provably matches the reference validator.

Some rules the specification has to state are not about one event but about
receiving and relaying messages (author binding, expiry) and about which
verdicts are in effect (latest verdict wins, revocation). No code expressed
them in a form other implementations or the future mesh and store packages
could share.

## Decision

- **Specification** in `documentation/spec/obie-0.1.md`, written with RFC 2119
  keywords. Every paragraph or list item containing MUST, MUST NOT, REQUIRED
  or SHALL carries a requirement tag such as `[ENV-3]`. Appendix A maps each
  tag to the Go tests that back it. `TestSpecRequirementsAreTested` fails if
  a normative statement has no tag, a tag is missing from the appendix, or
  the appendix names a test function that does not exist. Requirements that
  no code in the repository can check yet (for example libp2p configuration
  before the mesh exists, or rate limits) are written as SHOULD.
- **JSON Schema** (2020-12) in `documentation/spec/obie-0.1.schema.json`
  describes what `Decode` accepts, including unsigned events (empty
  `publisher.signature`), because it mirrors the decoder. Range constraints
  apply to numbers as written, as schema validators compare them, so
  `Decode` rejects a confidence literal such as `1.0000000000000001` that
  only its binary64 rounding brings into range. The tests validate
  every test vector and a list of valid and invalid samples against the
  schema and require `Decode` to agree on each. Rules the schema cannot
  express are listed in the specification and covered by
  `TestSchemaLimits`, which fails if the schema starts rejecting one of them
  (the sample then moves to the agreement test).
- **Schema validator:** `github.com/santhosh-tekuri/jsonschema/v6`, used by
  tests only. It implements draft 2020-12 fully, checks the schema against
  the meta-schema, has no dependencies beyond `golang.org/x/text` (already in
  the module graph) and uses Go's `regexp`, whose syntax covers the schema's
  patterns identically to ECMA-262.
- **Protocol rules as code** in `pkg/obieproto`: `Topic`
  (`obie/0.1/verdicts`); `Receive(data, from, opts…)`, the complete check a
  node applies before acting on or forwarding a message (`Decode`, author
  equals `publisher.peer_id`, not expired → `ErrExpired`, `Verify`; it
  never accepts documentation ranges); `Event.Supersedes(old)` (later `issued_at`, ties broken by the greater ID) and
  `Event.Withdraws(v)` (same publisher, same indicator, `revokes` = ID). The
  mesh and the store are to use these instead of re-implementing the rules;
  the store of WP-1654, developed in parallel, applies equivalent rules of
  its own and should switch to these functions once both are merged.

## Consequences

- Changing a MUST requires a tagged statement and a test; the spec test
  enforces the bookkeeping, reviewers judge whether the test really backs
  the statement.
- A change to the event format has to update the schema, or the agreement
  test fails; the test vectors already guard the signed form (ADR 0004).
- The schema is necessary but not sufficient: implementers still need the
  rules beyond the schema, which the specification lists.
