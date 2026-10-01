# Enforcing with nftables

With `enforce.backend: nftables` in
[enforce mode](../glossary.md#enforce-mode) (`node.mode: enforce`), the
[node](../glossary.md#node) `obied` drops traffic from blocked addresses
through its own nftables table. It never changes any other table, so it
coexists with an existing ruleset, firewalld, Docker or iptables-nft. See
ADR 0015 for the design.

## What obied creates

```text
table inet obie {
	set obie_v4 {
		type ipv4_addr
		flags interval,timeout
	}
	set obie_v6 {
		type ipv6_addr
		flags interval,timeout
	}
	chain input {
		type filter hook input priority -10; policy accept;
		ip saddr @obie_v4 counter drop
		ip6 saddr @obie_v6 counter drop
	}
}
```

Every element carries the remaining lifetime of its block, so the kernel
removes it on time even if `obied` is not running. Inspect it with
`nft list table inet obie` or `obiectl enforced`.

## Configuration

```yaml
node:
  mode: enforce
enforce:
  backend: nftables
  nftables:
    forward: false          # true: also drop in a forward chain (routers, container hosts)
    teardown_on_stop: false # true: remove the table when the service stops
```

A table `inet obie` with a different structure (an older version, a manual
edit) is replaced at start; a table deleted or changed by hand is restored
by the next reconciliation (`enforce.reconcile_interval`, plus a second).

## Permissions

`obied` needs CAP_NET_ADMIN, nothing more. Without it every nftables call
fails with `nftables access denied: obied needs CAP_NET_ADMIN`. The shipped
unit [`packaging/systemd/obied.service`](../../packaging/systemd/obied.service)
(installed by `install.sh`, see
[install.md](../operations/install.md)) runs obied as the user `obie` with
exactly that:

```ini
[Service]
User=obie
AmbientCapabilities=CAP_NET_ADMIN
CapabilityBoundingSet=CAP_NET_ADMIN
ExecStopPost=-/usr/local/bin/obied teardown-firewall --on-stop --config /etc/obie/obie.yaml
```

`teardown-firewall --on-stop` only removes the table if
`enforce.nftables.teardown_on_stop` is true; by default the blocks stay
across restarts until they expire.

## Uninstall

Stop the service, then remove the table with every block:

```sh
sudo obied teardown-firewall
```

Switching the node to `observe` mode (`node.mode: observe` and a reload)
removes the table as well.
