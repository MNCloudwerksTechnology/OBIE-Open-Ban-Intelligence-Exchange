# How do I connect with a friend's node and choose a trust level?

Connect your [node](../glossary.md#node) to a node that a friend or a
partner organisation runs, so that the two exchange their
[verdicts](../glossary.md#verdict) as [peers](../glossary.md#peer), and
decide how much your node trusts theirs. Both of you do the same on your own server; it takes a few
minutes.

> **Warning:** In [enforce mode](../glossary.md#enforce-mode), the other
> node's verdicts count at once: together with those of other nodes, they
> can get addresses blocked on your server. Connect in
> [observe mode](../glossary.md#observe-mode), and watch what its verdicts
> would do for a week first.

## Before you start

- Your node runs in observe mode, as
  [Get started](../getting-started.md#7-see-the-first-verdict) leaves it
  after step 7.
- The other node runs too, and you can reach its operator over a channel
  where you know who you are talking to: in person, on a call, or in a
  signed mail.
- Port 4001, TCP and UDP, is open between the two servers
  ([how](../operations/federation.md#open-the-mesh-port)).

## Undo

To disconnect again and stop trusting the other node, delete the two
entries the steps add: its address, and its peer ID with its name and
weight. Then restart the node:

```sh
sudo sed -i -e '\|/p2p/12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf"$|d' -e '/^    - peer_id: "12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf"$/,/^      weight: /d' /etc/obie/obie.yaml
sudo systemctl restart obied
```

The node no longer connects to the other node, and no longer trusts it:

```sh
sudo obiectl peers
```

```text
No peers connected.
obiectl peers: if this node should have peers, sudo obied self-check tests whether each configured peer answers, and documentation/operations/troubleshooting.md#no-peers lists the usual causes
```

To keep the connection but stop counting the other node's verdicts, see
[How do I stop trusting a peer?](stop-trusting-a-peer.md) instead. Ask the
other operator to remove your node from theirs as well.

## Steps

Choose a [trust weight](../glossary.md#trust-weight) for the other node, a
number from 0 to 1. Your node blocks an address when the verdicts on it,
each counted with its sender's weight, reach the
[threshold](../glossary.md#threshold), and enough senders agree (the
[quorum](../glossary.md#quorum)). With the default settings:

| The other node is run by | Trust weight | What its verdicts do on your node |
|--------------------------|--------------|-----------------------------------|
| a friend or a partner you know well | 0.8 | They count, but never block alone: at least three nodes like it must agree. |
| someone you know less well | 0.5 | They count less: more nodes must agree. |
| someone whose verdicts you only want to see | 0 | Your node shows them and never counts them. |

Do not give a node 1: that says its judgement is as good as your own
server's. To let fewer nodes block an address, lower the threshold or the
quorum instead, as [Federation](../operations/federation.md#choose-trust-weights-and-quorum)
explains, and read its warnings first.

Show your node's peer ID, which the other node needs:

```sh
sudo obiectl identity
```

```text
Peer ID:      12D3KooWPqtsL3NjMswRrqG8xYAsfMjPgfajfvg9625cc6Y9WmiD
Fingerprint:  SHA256:wIufDNocPY1kRab1DT/AV/aVBV49J50jXJFOMwVUY2w
```

Give the other operator your node's address,
`/ip4/<your server's public address>/tcp/4001/p2p/<your peer ID>`, and read
the fingerprint out to them, so that they know the peer ID is yours. They
give you their node's address in return, such as
`/ip4/198.51.100.20/tcp/4001/p2p/12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf`,
and read out its fingerprint.

Add the other node's address to the peers your node connects to,
`mesh.bootstrap`. Paste the address you received in place of the
example's:

```sh
sudo sed -i -e 's/^  bootstrap: \[\]$/  bootstrap:/' -e '/^  bootstrap:$/a\    - "/ip4/198.51.100.20/tcp/4001/p2p/12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf"' /etc/obie/obie.yaml
```

Add its peer ID, a name and the trust weight you chose to the nodes your
node trusts, `trust.publishers`. The name, `friend` here, is how the node
names it in every list:

```sh
sudo sed -i -e 's/^  publishers: \[\]$/  publishers:/' -e '/^  publishers:$/a\    - peer_id: "12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf"\n      name: "friend"\n      weight: 0.8' /etc/obie/obie.yaml
```

The two commands change only these two lists, as the setup assistant
writes them, and leave the rest of the file as it is. In an editor, make
the same change. The lists now read:

```sh
sudo grep -A 1 '^  bootstrap:' /etc/obie/obie.yaml
```

```text
  bootstrap:
    - "/ip4/198.51.100.20/tcp/4001/p2p/12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf"
```

```sh
sudo grep -A 3 '^  publishers:' /etc/obie/obie.yaml
```

```text
  publishers:
    - peer_id: "12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf"
      name: "friend"
      weight: 0.8
```

Check the file, and restart the node, which reads its peers only when it
starts:

```sh
sudo obied --config /etc/obie/obie.yaml --check-config
```

```text
obied: configuration /etc/obie/obie.yaml is valid
```

```sh
sudo systemctl restart obied
```

## Check that it worked

Once the other operator has added your node too, the peer is connected,
with the trust weight you chose:

```sh
sudo obiectl peers
```

```text
PEER ID                                               NAME    TRUST  BOOTSTRAP  CONNECTED SINCE       LATENCY  ADDRESSES
12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf  friend  0.8    yes        2026-10-14T09:14:03Z  1ms      /ip4/198.51.100.20/tcp/4001
```

From now on, your node receives what the other node reports. Its
verdicts show up in the explanation of an address, with the weight you
gave it. `85.10.0.66` stands for an address that the other node
reported:

```sh
sudo obiectl explain 85.10.0.66
```

```text
Indicator:             ipv4:85.10.0.66
Decision:              none
…
PUBLISHER  PEER ID                                               ACTION  WEIGHT  CONFIDENCE  SCORE  COUNTS  PROTOCOL  REASON      ISSUED                EXPIRES
friend     12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf  ban     0.8     0.8         0.64   yes     ssh       bruteforce  2026-10-14T09:14:05Z  2026-10-21T09:14:05Z
```

The verdict counts, and it does not block alone. If the peer is not
listed within a minute, see
[No peers](../operations/troubleshooting.md#no-peers).
