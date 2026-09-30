# How do I connect with a friend's node and choose a trust level?

Connect your [node](../glossary.md#node) to a node that a friend or a
partner organisation runs, so that the two exchange their
[verdicts](../glossary.md#verdict), and decide how much your node trusts
theirs. Both of you do the same on your own server; it takes a few
minutes.

## Before you start

- Your node runs as [Get started](../getting-started.md#7-see-the-first-verdict)
  leaves it after step 7, in [observe mode](../glossary.md#observe-mode).
  Once you have connected a [peer](../glossary.md#peer), keep observe mode
  for a week and watch what its verdicts would do.
- The other node runs too, and you can reach its operator over a channel
  where you know who you are talking to: in person, on a call, or in a
  signed mail.
- Port 4001, TCP and UDP, is open between the two servers
  ([how](../operations/federation.md#open-the-mesh-port)).
- The setup assistant writes the whole configuration again. If your node
  already has peers, `sudo obiectl peers` lists them: enter them again,
  together with the new one.

## Steps

Choose a [trust weight](../glossary.md#trust-weight) for the other node, a
number from 0 to 1. Your node blocks an address when the verdicts on it,
each counted with its sender's weight, reach the
[threshold](../glossary.md#threshold), and enough senders agree (the
[quorum](../glossary.md#quorum)). With the default settings:

| The other node is run by | Trust weight | What its verdicts do on your node |
|--------------------------|--------------|-----------------------------------|
| a friend or a partner you know well | 0.8, the suggestion | They count, but never block alone: at least three nodes like it must agree. |
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

Run the setup assistant. Press Enter at every question, except three:
paste the other node's address at `Peer address`, give it a name, such as
`friend`, and type the trust weight you chose, or press Enter for 0.8.
Answer `y` to replace the file.

```sh
sudo obied setup
```

```text
OBIE setup
…
1/5  Where should the node keep its state?
…
State directory [/var/lib/obie]:

2/5  Where should the node write its audit log?
…
Audit log [/var/log/obie/audit.jsonl]:

3/5  Which peers should this node connect to?
…
Peer address (empty: done): /ip4/198.51.100.20/tcp/4001/p2p/12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf
  Name of this peer [198.51.100.20]: friend
  How much do you trust its verdicts, from 0 (not at all) to 1 (fully)?
  With the default settings, one peer alone never gets an address blocked.
  Trust weight [0.8]:
Peer address (empty: done):

4/5  Should the node start in observe mode?
…
Start in observe mode? [Y/n]:

5/5  Which addresses must never be blocked?
…
Addresses or networks, separated by spaces, or none [85.10.3.20/32]:

Summary
  State directory: /var/lib/obie
  Audit log:       /var/log/obie/audit.jsonl
  Peers:           friend (trust 0.8) /ip4/198.51.100.20/tcp/4001/p2p/12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf
  Mode:            observe
  Never blocked:   85.10.3.20/32

Replace /etc/obie/obie.yaml? The old file is kept as a backup. [y/N]: y
Wrote /etc/obie/obie.yaml.
The previous file is kept as /etc/obie/obie.yaml.bak.1.
…
```

The last lines name the backup of the previous file; you need it to undo.
The node reads its peers only when it starts, so restart it:

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

## Undo

To go back to the configuration you had, copy the backup that the setup
assistant named back into place, and restart the node:

```sh
sudo cp /etc/obie/obie.yaml.bak.1 /etc/obie/obie.yaml
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
