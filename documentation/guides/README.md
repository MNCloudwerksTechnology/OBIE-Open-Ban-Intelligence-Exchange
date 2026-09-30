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

## Work with other nodes

Exchange [verdicts](../glossary.md#verdict) with
[peers](../glossary.md#peer), the nodes of people you know, and decide
how much each one counts.

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

Before it compares, the check replaces what differs from run to run:
peer IDs, event IDs, fingerprints, times and durations. It ignores column
widths, and `…` stands for any output. Other Linux distributions and ARM
processors are not tested.
