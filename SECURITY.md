# Security policy

## Supported versions

OBIE is in early development. There is no released version yet, and no
version is supported for production use: do not rely on OBIE to protect
production servers. Security fixes land on the `develop` branch.

## Reporting a vulnerability

Please do not describe a vulnerability in a public issue, pull request or
discussion.

A private reporting channel is being set up. Until it is in place, open an
issue titled "Security contact request" that contains no details about the
problem; a maintainer will reply with a private way to send the report.

## Scope

In scope: the node (`obied`, `obiectl`), the obie/0.1 protocol specification
and its reference implementation in `pkg/obieproto`, and the website in
`website/`. The threat model of the protocol is described in the
specification's
[security considerations](documentation/spec/obie-0.1.md#14-security-considerations).
