# How do I back up and restore the node identity?

Your [node](../glossary.md#node)'s identity is its key,
`/var/lib/obie/node.key`. The node's [peer ID](../glossary.md#peer-id)
comes from it, and the node signs its
[verdicts](../glossary.md#verdict) with it. Your
[peers](../glossary.md#peer) trust that key. Back it up once, and
restore it when you reinstall the server or move the node, so that your
peers still know your node.

## Before you start

- Your node has started once, as
  [Get started](../getting-started.md#4-start-the-node-in-observe-mode)
  leaves it after step 4: the node creates its key at its first start.
- You have a safe place for the copy away from the server, such as your
  password manager or an encrypted backup. Whoever has the key can
  publish verdicts in your node's name.

## Steps

Copy the key into a directory that only root can read:

```sh
sudo install -d -m 0700 /root/obie-backup
sudo install -m 0600 /var/lib/obie/node.key /root/obie-backup/node.key
```

Check that the copy is the key of this node: `obied identity` reads the
file `node.key` in the directory you name, and must show the same peer
ID as `sudo obiectl identity`:

```sh
sudo obied identity --state-dir /root/obie-backup
```

```text
Peer ID:      12D3KooWPqtsL3NjMswRrqG8xYAsfMjPgfajfvg9625cc6Y9WmiD
Fingerprint:  SHA256:wIufDNocPY1kRab1DT/AV/aVBV49J50jXJFOMwVUY2w
```

Copy `/root/obie-backup/node.key` to the safe place, for example with
`scp` from your workstation, and note the peer ID with it. The key is all
you need: the node's other state holds verdicts that expire, and your
[overrides](../glossary.md#override), which `sudo obiectl overrides`
lists if you want to note them down too.

To restore the key, on a new server or after a reinstallation, install
OBIE as in [Get started](../getting-started.md#2-install-obie), and copy
the backup to `/root/obie-backup/node.key` on the server. Stop the node,
if it runs; the key must not change under a running node:

```sh
sudo systemctl stop obied
```

Put the key in place, for the user `obie` that the node runs as. If the
node has a key already, `install` keeps it as
`/var/lib/obie/node.key.before-restore`:

```sh
sudo install -d -o obie -g obie -m 0700 /var/lib/obie
sudo install -b -S .before-restore -o obie -g obie -m 0600 /root/obie-backup/node.key /var/lib/obie/node.key
sudo systemctl start obied
```

`obied` refuses a key file that another user owns, or that others may
read, and says how to fix it.

## Check that it worked

The running node has the peer ID of the backup:

```sh
sudo obiectl identity
```

```text
Peer ID:      12D3KooWPqtsL3NjMswRrqG8xYAsfMjPgfajfvg9625cc6Y9WmiD
Fingerprint:  SHA256:wIufDNocPY1kRab1DT/AV/aVBV49J50jXJFOMwVUY2w
```

Your peers connect to it as before.

## Undo

A backup needs no undo; delete `/root/obie-backup` once the copy is safe
elsewhere. To go back to the key that a restore replaced, put it back
while the node is stopped:

```sh
sudo systemctl stop obied
sudo mv /var/lib/obie/node.key.before-restore /var/lib/obie/node.key
sudo systemctl start obied
```

If the node had no key before the restore, there is nothing to go back
to. If the key may have been stolen, do not restore it: let the node
create a new one, as
[Operations](../operations/operations.md#back-up-the-node-key) explains.
