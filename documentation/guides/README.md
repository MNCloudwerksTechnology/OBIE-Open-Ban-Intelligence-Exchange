# How-to guides

Short guides for the routine jobs of running an OBIE
[node](../glossary.md#node), one job per guide. Each guide says what you
need before you start, gives the steps, shows how to check that they
worked, and how to undo them. A guide that changes what your firewall
blocks warns you first and shows the way back before the steps.

The guides start from a node set up as in
[Get started](../getting-started.md); each one names the step of the
tutorial your node must have reached. Every command and its output is
checked automatically against the current release
([how](#how-the-guides-are-tested)).

## See what your node decides

Find out what your node blocks, or would block, and why, before you let
it act and whenever someone cannot reach your server.

- [How do I review what my node would block before enforcing?](review-what-would-be-blocked.md)
- [How do I find out why an address is blocked?](why-is-an-address-blocked.md)

## Correct a decision

Overrule your node for one address: let it through, block it, or take
back a [verdict](../glossary.md#verdict) your node reported.

- [How do I unblock an address I trust, now and for good?](unblock-an-address.md)
- [How do I block an address manually?](block-an-address.md)
- [How do I withdraw a verdict I published by mistake?](withdraw-a-verdict.md)

## Let your node block, or stop it

Switch between observing and blocking, and get back into your server if
the node ever locks you out.

- [How do I switch from observe to enforce, and back?](switch-enforcement.md)
- [How do I recover after locking myself out?](recover-from-a-lockout.md)

## Work with other nodes

Exchange verdicts with [peers](../glossary.md#peer), the nodes of people
you know, and decide how much each one counts.

- [How do I connect with a friend's node and choose a trust level?](connect-a-peer.md)
- [How do I stop trusting a peer?](stop-trusting-a-peer.md)

## How the guides are tested

`make guides-check` runs every command of every guide and compares its
output with the page, on every change to OBIE, against the release built
from that change. For each guide, it starts a server of its own: a
container with Ubuntu 24.04, systemd, Fail2Ban, OpenSSH and nftables, on
amd64, whose firewall rules stay inside the container. It then runs the
tutorial's commands up to the step the guide starts from, and the guide:
its steps, the check that they worked, and then the way back, even where
the page shows the way back first.

What a reader brings along is played by the check, as for the tutorial:

- The reader's SSH session comes from `85.10.3.20`.
- The peer `friend` is a second node at `198.51.100.20`. It reports
  `85.10.0.66`, as its Fail2Ban would.
- The attacker `85.10.0.7` fails five SSH logins, and Fail2Ban bans it,
  as in the tutorial.
- The customer `85.10.4.12` mistypes their password five times, and
  Fail2Ban bans them.
- The reader reports `85.10.0.19` by mistake.
- The reader mistypes their password five times from `85.10.3.30`, their
  address at home, and Fail2Ban bans it.

Before it compares, the check replaces what differs from run to run:
peer IDs, event IDs, fingerprints, times and durations. It ignores column
widths, and `…` stands for any output. Other Linux distributions and ARM
processors are not tested.
