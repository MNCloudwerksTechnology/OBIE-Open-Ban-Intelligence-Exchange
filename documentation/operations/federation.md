# Federation

A [node](../glossary.md#node) on its own only acts on its own detections.
[Federating](../glossary.md#federation) means two things, and each operator
decides both for their own node:

- **Connect.** The nodes gossip their signed
  [verdicts](../glossary.md#verdict) to each other over libp2p
  (`mesh.bootstrap`).
- **Trust.** Each node gives the other's verdicts a
  [trust weight](../glossary.md#trust-weight) (`trust.publishers`). Only
  weighted verdicts count towards a block; everything else is shown by
  `obiectl explain` but never acts.

Connecting without trusting is safe. Trusting is where the risk is: a
publisher you trust can get addresses blocked on your host. Read the
[threat model](../../SECURITY.md#threat-model) before you choose weights.

This page federates your node with a friend's node, both installed as in
the [quick start](quickstart.md). Everything is configured by hand in
v0.1: there is no discovery and no public mesh.

## Exchange peer IDs and addresses

A node is known by its **[peer ID](../glossary.md#peer-id)**, derived from
its Ed25519 key. It is both its name on the mesh and the key its verdicts
are signed with, so it is the one thing you must get right. Each of you
runs:

```sh
sudo obiectl identity
```

```text
Peer ID:      12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf
Fingerprint:  SHA256:iJQCoAB0CUvEzbf/YZDCIElsOo7GuqUNHru08MWXzVY
```

Send each other the peer ID together with a **multiaddr**: how to reach
the node, ending in its peer ID. With the default `mesh.listen` a node
listens on port 4001, TCP and QUIC:

```text
/ip4/198.51.100.20/tcp/4001/p2p/12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf
/dns4/obie.friend.example/tcp/4001/p2p/12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf
/ip6/2001:db8::20/udp/4001/quic-v1/p2p/12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf
```

Use the public address or DNS name the other node can reach. The
addresses a node actually listens on are in its start-up log:
`sudo journalctl -u obied | grep 'mesh listening'`.

Exchange peer IDs over a channel where you know who you are talking to
(in person, a signed mail, a call where you read out the fingerprint). The
connection itself is authenticated against the peer ID in the multiaddr,
so an attacker who swaps the address can only make the connection fail;
an attacker who swaps the peer ID becomes the peer you trust.

## Open the mesh port

Allow TCP and UDP port 4001 from the other node in your host firewall.
Any libp2p peer that reaches the port can connect and gossip; v0.1 does
not restrict connections to the configured peers. Its verdicts only count
if you trust it, but it uses your bandwidth and CPU, so open the port only
to the nodes you federate with. OBIE's own nftables table only drops
blocked addresses and never opens anything.

## Configure the peer

Add the friend's node to `/etc/obie/obie.yaml`:

```yaml
mesh:
  bootstrap:
    - /dns4/obie.friend.example/tcp/4001/p2p/12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf
trust:
  publishers:
    - peer_id: 12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf
      name: friend
      weight: 0.8
```

- `mesh.bootstrap` makes your node dial the peer at start and keep the
  connection, redialling with a backoff of up to five minutes. It is enough
  if one side lists the other, but list each other so either can restart.
  The IPs of bootstrap peers are never blocked.
- `trust.publishers` is what makes the friend's verdicts count. Its
  `peer_id` must be exactly the one you exchanged.

Check the file and restart; `mesh.bootstrap` is only read at start
(changes to `trust` alone only need `sudo systemctl reload obied`):

```sh
sudo obied --config /etc/obie/obie.yaml --check-config
sudo systemctl restart obied
sudo obiectl peers
```

```text
PEER ID                                               NAME    TRUST  BOOTSTRAP  CONNECTED SINCE       LATENCY  ADDRESSES
12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf  friend  0.8    yes        2026-09-28T08:12:03Z  14ms     /ip4/198.51.100.20/tcp/4001
```

`obiectl status` now shows `mesh … 1 peers connected (1/1 bootstrap
peers)`, and `obie_peers_connected` is 1. If the peer does not appear, see
[Troubleshooting](troubleshooting.md#no-peers).

Verdicts reach a node only while it is connected. v0.1 has no catch-up:
what your friend publishes while your node is down or disconnected does
not arrive later, until the friend's Fail2Ban bans the address again.

## Choose trust weights and quorum

For every address, each node takes the latest active verdict of each
distinct publisher and computes a score over those that are `ban`
verdicts:

> score = Σ weight × confidence

and blocks the address when the score reaches the
[threshold](../glossary.md#threshold) `decision.threshold` **and** at least
`decision.quorum` distinct publishers with a weight above 0 reported it (the
[quorum](../glossary.md#quorum)). Your own node is a publisher too, with
weight `trust.local_weight` (1.0). A Fail2Ban ban has confidence 0.8 unless
the jail sets `confidence`. Independently of the score, your own verdicts
block at once (`decision.local_autoblock`), so federation is about what
*other* nodes' reports make your node do.

What that means with a Fail2Ban confidence of 0.8:

| Setup | Weights | `threshold` | `quorum` | Blocks on your node when |
|-------|---------|-------------|----------|--------------------------|
| Default | 0.8 each | 1.8 | 2 | three peers agree (3 × 0.64 = 1.92) |
| Two nodes, start here | friend 0.8 | 1.8 | 2 | only your own detections; the friend's reports are visible in `obiectl explain` and the audit log but never block |
| Two nodes, act on the friend | friend 1.0 | 0.8 | 1 | a single report of the friend blocks (1.0 × 0.8 = 0.8) |
| Three to five nodes | 0.8 each | 1.2 | 2 | two peers agree (2 × 0.64 = 1.28) |
| Larger | 0.5 to 0.8 | 1.8 | 3 | three or more peers agree, with enough weight |

Recommended start:

1. **Watch before you act.** For a week after adding peers, keep
   `node.mode: observe`, or, on a node that already enforces, keep the
   default `threshold` and `quorum`.
2. **Weight peers at 0.8**, not 1.0: a full weight says their judgement is
   as good as yours.
3. **Keep `trust.default_weight: 0`.** Anyone can create a key; a non-zero
   default weight lets strangers vote.
4. **Never set `quorum: 1` with remote peers** unless you accept that any
   one of them, or whoever steals its key, can block any public address on
   your host for up to `decision.max_ttl`.
5. **Read what the mesh would do** before you act on it:

   ```sh
   sudo obiectl decisions --state block
   sudo obiectl explain 85.10.0.7
   ```

   `explain` lists every publisher's verdict, its weight and whether it
   counts. When the blocks look right, lower the threshold or switch to
   `enforce`.

`trust` and `decision` take effect on `sudo systemctl reload obied`; every
decision is re-evaluated at once.

## Leave a federation

To **stop trusting** a peer, remove it from `trust.publishers` (or set its
`weight: 0`) and reload. Its verdicts stop counting at once, and blocks
that relied on them are lifted.

To **disconnect**, also remove it from `mesh.bootstrap`, close port 4001
to it and restart. It can still dial you while the port is open.

To **leave for good**, first withdraw what you published, while you are
still connected, since [revocations](../glossary.md#revocation) sent later
never arrive:

```sh
sudo obiectl indicators --mine
sudo obiectl revoke 85.10.0.7
```

Then disconnect as above and ask the other operators to remove your peer
ID from their `trust.publishers`. Verdicts you do not revoke stay valid on
their nodes until they expire (at most `decision.max_ttl`, 30 days).

To **change your node's identity**, e.g. after a suspected key theft, see
[Operations](operations.md#back-up-the-node-key): a new key is a new
peer ID that every peer has to add again.
