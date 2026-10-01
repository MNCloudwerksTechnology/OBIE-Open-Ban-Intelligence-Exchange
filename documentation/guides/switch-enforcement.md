# How do I switch from observe to enforce, and back?

Let your [node](../glossary.md#node) block what it decides, in
[enforce mode](../glossary.md#enforce-mode), once you are sure that it
cannot lock you out; and go back to
[observe mode](../glossary.md#observe-mode), which blocks nothing, at any
time.

> **Warning:** In enforce mode, the node blocks addresses in your
> firewall. If an address you administer the server from is not
> protected, the node can lock you out. Protect it first, keep a way into
> the server that does not depend on its network, and know the way back
> below before you switch.

## Before you start

- Your node runs in observe mode, as
  [Get started](../getting-started.md#9-review-what-would-be-blocked)
  leaves it after step 9, and you have reviewed what it would block
  ([How do I review what my node would block before enforcing?](review-what-would-be-blocked.md)).
- You have a way into the server that does not depend on its network,
  such as your provider's web console.

## Undo

To go back to observe mode, set it in `/etc/obie/obie.yaml` and reload
the node. It lifts every block at once, and removes its firewall table:

```sh
sudo sed -i 's/^  mode: enforce$/  mode: observe/' /etc/obie/obie.yaml
sudo systemctl reload obied
```

```sh
sudo obiectl enforced
```

```text
No entries applied: the node is in observe mode.
```

The firewall no longer holds OBIE's table `inet obie`. Other tables, such
as Fail2Ban's, are not OBIE's and stay:

```sh
sudo nft list tables
```

```text
table inet f2b-table
```

If you cannot reach the server any more, see
[How do I recover after locking myself out?](recover-from-a-lockout.md).

## Steps

Make sure that the node never blocks your own access. The self-check's
`SSH session` line must say that your session is protected, and there
must be no `PROBLEM`:

```sh
sudo obied self-check
```

```text
OBIE self-check of /etc/obie/obie.yaml (obied 0.1.0, as root)
…
OK       SSH session    your SSH session comes from 85.10.3.20, which is protected (allow-listed: allowlist.cidrs entry 85.10.3.20/32)

Result: 0 problems, …
```

`85.10.3.20` stands for the address of your SSH session. Protect every
other address you cannot afford to lose, such as your office network,
your monitoring and your DNS resolvers, as
[step 10 of Get started](../getting-started.md#protect-your-own-access)
shows.

Set enforce mode. The first command changes the line `mode:` in the
section `node:`:

```sh
sudo sed -i 's/^  mode: observe$/  mode: enforce/' /etc/obie/obie.yaml
```

The node blocks through its own table in
[nftables](../glossary.md#nftables), the Linux firewall, once the file
has an `enforce:` section that says so. This command adds the section
unless the file has one, for example from an earlier switch:

```sh
sudo grep -q '^enforce:' /etc/obie/obie.yaml || printf '\nenforce:\n  backend: nftables\n' | sudo tee -a /etc/obie/obie.yaml
```

```sh
sudo grep -A 1 '^enforce:' /etc/obie/obie.yaml
```

```text
enforce:
  backend: nftables
```

Check the file, and restart the node, which chooses its firewall backend
only when it starts:

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

The node says that it blocks:

```sh
sudo obiectl status
```

```text
Mode:     ENFORCE (blocks are sent to the enforcer)
…
```

The firewall applies what the node decided. `85.10.0.7` stands for an
attacker your Fail2Ban banned; your list shows the blocks active on your
server, or none:

```sh
sudo obiectl enforced
```

```text
Entries applied: 1

PREFIX        EXPIRES               REMAINING
85.10.0.7/32  2026-10-14T09:22:05Z  5m38s
```

`sudo nft list table inet obie` shows the same blocks in the kernel's
table. If `obiectl status` still says `OBSERVE`, see
[Nothing is enforced](../operations/troubleshooting.md#nothing-is-enforced).
