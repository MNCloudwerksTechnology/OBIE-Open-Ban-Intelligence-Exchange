# How do I stop trusting a peer?

Make the [verdicts](../glossary.md#verdict) of a
[peer](../glossary.md#peer) count for nothing on your
[node](../glossary.md#node), and disconnect from it if you want: for
example when you no longer know who runs it, or when its key may have
been stolen.

> **Warning:** In [enforce mode](../glossary.md#enforce-mode), this lifts
> at once every block that needed the peer's verdicts: addresses that
> only this peer reported can reach your server again. Your own
> detections still block.

## Before you start

- Your node is connected to the peer, as
  [Get started](../getting-started.md#8-connect-to-a-peer) leaves it after
  step 8. The peer is called `friend` here.
- You know the peer's peer ID; `sudo obiectl peers` shows it.
- If you suspect that the peer's key was stolen, tell its operator.

## Undo

To trust the peer again, set its [trust weight](../glossary.md#trust-weight)
back to what it was, 0.8 here, and reload the node:

```sh
sudo sed -i '/^      name: "friend"$/{n;s/^      weight: 0$/      weight: 0.8/}' /etc/obie/obie.yaml
sudo obied --config /etc/obie/obie.yaml --check-config
sudo systemctl reload obied
```

If you disconnected it too, add its address back to the peers the node
connects to, and restart the node:

```sh
sudo sed -i -e 's/^  bootstrap: \[\]$/  bootstrap:/' -e '/^  bootstrap:$/a\    - "/ip4/198.51.100.20/tcp/4001/p2p/12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf"' /etc/obie/obie.yaml
sudo systemctl restart obied
```

```sh
sudo obiectl peers
```

```text
PEER ID                                               NAME    TRUST  BOOTSTRAP  CONNECTED SINCE       LATENCY  ADDRESSES
12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf  friend  0.8    yes        2026-10-14T09:14:03Z  1ms      /ip4/198.51.100.20/tcp/4001
```

## Steps

Set the peer's trust weight to 0. This command changes the line
`weight:` after the peer's `name:` in `/etc/obie/obie.yaml`, as the setup
assistant writes them; in an editor, make the same change:

```sh
sudo sed -i '/^      name: "friend"$/{n;s/^      weight: .*/      weight: 0/}' /etc/obie/obie.yaml
```

```sh
sudo grep -A 1 'name: "friend"' /etc/obie/obie.yaml
```

```text
      name: "friend"
      weight: 0
```

Check the file, and reload the node, which applies a new trust weight at
once, without a restart:

```sh
sudo obied --config /etc/obie/obie.yaml --check-config
```

```text
obied: configuration /etc/obie/obie.yaml is valid
```

```sh
sudo systemctl reload obied
```

The node still exchanges verdicts with the peer and shows them, but they
no longer count. That is enough when you only doubt the peer's judgement.

If the peer's key may have been stolen, or you do not want it to use your
server's bandwidth any more, also disconnect from it. Delete its address
from the peers your node connects to, and restart the node, which reads
them only when it starts:

```sh
sudo sed -i '\|/p2p/12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf"$|d' /etc/obie/obie.yaml
sudo systemctl restart obied
```

Then close port 4001 for the peer's address in your firewall
([how it was opened](../operations/federation.md#open-the-mesh-port)):
while the port is open, the peer can still connect to your node, but its
verdicts count for nothing.

## Check that it worked

The explanation of an address that the peer reported shows its verdict
with the weight 0, not counting. `85.10.0.66` stands for such an
address:

```sh
sudo obiectl explain 85.10.0.66
```

```text
Indicator:             ipv4:85.10.0.66
Decision:              none
…
PUBLISHER  PEER ID                                               ACTION  WEIGHT  CONFIDENCE  SCORE  COUNTS  PROTOCOL  REASON      ISSUED                EXPIRES
friend     12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf  ban     0       0.8         0      no      ssh       bruteforce  2026-10-14T09:14:05Z  2026-10-21T09:14:05Z
```

After you disconnected, the peer is not connected any more:

```sh
sudo obiectl peers
```

```text
No peers connected.
obiectl peers: if this node should have peers, sudo obied self-check tests whether each configured peer answers, and documentation/operations/troubleshooting.md#no-peers lists the usual causes
```

The verdicts it sent before stay until they expire, and count for nothing.
