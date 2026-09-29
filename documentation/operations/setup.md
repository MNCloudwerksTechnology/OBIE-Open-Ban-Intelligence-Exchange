# Set up and check a node

Two commands help you with a first [node](../glossary.md#node), the OBIE
program on your server. `obied setup` asks the few questions that matter
and writes the configuration for you. `obied self-check` checks the node
and the server and tells you, for everything that is not right, what to
do next. Both come with every release. Run them as root: the configuration,
the node's state and its admin interface are closed to other users.

The [quick start](quickstart.md) uses both. This page explains what they
ask, what they check and how to use them in scripts.

## Set up the node

After [installing](install.md), run the setup assistant:

```sh
sudo obied setup
```

It asks five questions. Each one is explained on the screen and comes with
a safe suggestion in brackets; press Enter to take it.

| Question | Suggestion | What it sets |
|----------|------------|--------------|
| Where should the node keep its state? | `/var/lib/obie` | `node.state_dir`: the node's identity key and its database of [verdicts](../glossary.md#verdict) |
| Where should the node write its audit log? | `/var/log/obie/audit.jsonl`; `none` for no audit log | `audit.path`; the node's own log always goes to the journal |
| Which [peers](../glossary.md#peer) should this node connect to? | none: the node works on its own | `mesh.bootstrap` and `trust.publishers`: for each peer its address, a name and a [trust weight](../glossary.md#trust-weight) from 0 to 1, suggested 0.8 |
| Should the node start in [observe mode](../glossary.md#observe-mode)? | yes | `node.mode`; [enforce mode](../glossary.md#enforce-mode) also sets `enforce.backend: nftables` |
| Which addresses must never be blocked? | the address of your SSH session, unless it is protected already | `allowlist.cidrs`, the [allow-list](../glossary.md#allow-list) |

With the suggested trust weight and the default settings, a single peer
never gets an address blocked on your node on its own
([Federation](federation.md#choose-trust-weights-and-quorum) explains why).
Choosing enforce mode needs a second confirmation, because the node then
blocks from its first start.

Before it writes anything, the assistant shows a summary and asks once
more. The file it writes sets only what you answered, each key with a
comment that explains it; every other key keeps its default, as described
in the [configuration reference](configuration.md). It is checked before it
is written, so `obied` always accepts it. It gets mode 0640 and the group
`obie`, like the file `install.sh` installs.

**An existing configuration is never replaced without asking.** The
assistant tells you at the start that `/etc/obie/obie.yaml` exists. At the
end it asks whether to replace it, with *No* as the suggestion, or *Yes*
if the file is still the unchanged example that `install.sh` installed. A
replaced file is kept as `/etc/obie/obie.yaml.bak` (or `.bak.1`, `.bak.2`,
… if that name is taken).

When it has written the file, the assistant tells you what to do next:
start the node, run the self-check and open the
[tutorial](quickstart.md#3-connect-fail2ban) where it goes on with
Fail2Ban. If your SSH session comes from an address the new configuration
does not protect, it warns you of a lockout first. For another file than
`/etc/obie/obie.yaml`, the commands it shows name that file, and the first
step says to set it in the shipped service, which reads
`/etc/obie/obie.yaml`.

Run as another user than root, the assistant says so before it asks
anything:

```text
obied setup: cannot write /etc/obie/obie.yaml as user alice: permission denied
obied setup: run it as root: sudo obied setup
```

### Set up without questions

For automated installs, give the answers as flags with
`--non-interactive`. The same answers always write the same file as the
questions would:

```sh
sudo obied setup --non-interactive \
  --peer /dns4/obie.friend.example/tcp/4001/p2p/12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf,name=friend,weight=0.8 \
  --allow 198.51.100.0/24
```

| Flag | Answers | Default |
|------|---------|---------|
| `--state-dir <directory>` | where the node keeps its state | `/var/lib/obie` |
| `--audit-log <file>` | where it writes its audit log, or `none` | `/var/log/obie/audit.jsonl` |
| `--peer <address>[,name=<name>][,weight=<0..1>]` | a peer to connect to and trust; repeat it for more | none; the name defaults to the address's host, the weight to 0.8 |
| `--mode observe` or `--mode enforce` | observe or enforce mode | `observe` |
| `--allow <address or network>` | an address or network never to block; repeat it for more | none |
| `--force` | replace an existing configuration file, keeping the old one as a backup | an existing file is not replaced |
| `--config <file>` | the file to write | `/etc/obie/obie.yaml` |

Without `--force`, an existing file is left alone and the assistant exits
with status 1. The address of your SSH session is not added on its own in
this mode: pass it with `--allow`, or the assistant ends with a lockout
warning. A wrong answer exits with status 2 and names the flag.

The shipped service lets `obied` write only to `/var/lib/obie` and
`/var/log/obie`. For other directories the assistant prints the commands
that create them and allow them to the service.

## Check the node

Run the self-check at any time: right after the setup, before the first
start, after every change and whenever something seems wrong.

```sh
sudo obied self-check
```

```text
OBIE self-check of /etc/obie/obie.yaml (obied 0.1.0, as root)

OK       Configuration  /etc/obie/obie.yaml is valid; the node runs in observe mode
OK       Identity       /var/lib/obie/node.key exists and only obie can read it; the node's peer ID is 12D3KooW...
OK       Admin access   the admin socket /run/obie/obie.sock is open to root, obie and the group obie only (mode 0660)
                        - alice is in the group obie and may use obiectl without sudo
OK       Node           obied 0.1.0 is running and ready in observe mode, up 2h13m4s
WARNING  Peers          stand-alone node: no peers are configured, so the node acts only on what this server detects
                        Next: if that is what you want, there is nothing to do; to exchange verdicts with other nodes, ...
OK       Clock          the clock is synchronized (estimated error 12ms)
OK       Fail2Ban       jails that report every ban to the node: sshd
                        - the node holds verdicts of its own, so bans reach it
                        - OBIE's action is installed: /etc/fail2ban/action.d/obie.conf
OK       Firewall       not needed yet: in observe mode the node blocks nothing
OK       SSH session    your SSH session comes from 85.10.3.20, which is protected (allow-listed: allowlist.cidrs entry 85.10.3.20/32)

Result: 0 problems, 1 warning, 8 OK. No problems; read the warnings.
```

Each check starts with one of three words, so that nothing depends on colour:

- **OK**: all is well.
- **WARNING**: something needs your attention, or could not be checked,
  for example because the node is not running yet.
- **PROBLEM**: something is wrong, and the node cannot work as configured.

Every warning and problem is followed by a line `Next:` with what to do.

| Check | What it looks at | Before the first start |
|-------|------------------|------------------------|
| Configuration (`config`) | the configuration file exists, can be read and is valid, with its allow-list files; every mistake is listed with its key | the same |
| Identity (`identity`) | `node.key` exists, belongs to the user the service runs as (`obie`), nobody else may read it, and the state directory is not writable by others | a missing key is a warning: the node creates it at its first start |
| Admin access (`admin`) | the group of `admin.socket_group` exists, the admin socket is open to root, the service user and that group only, and whether you may use `obiectl` without `sudo` | the socket does not exist yet, which is fine |
| Node (`node`) | `obied` is running, every part of it is ready, and it is the installed version | a stopped node is a warning |
| Peers (`peers`) | peers are configured, each peer in `mesh.bootstrap` answers on its port, and, while the node runs, each is connected; no peers at all is a warning, as a node on its own may be what you want | only whether the peers answer |
| Clock (`clock`) | the time is plausible and kept in sync by NTP: verdicts expire by this clock, and peers drop verdicts dated more than five minutes ahead | the same |
| Fail2Ban (`fail2ban`) | Fail2Ban is installed, OBIE's action is installed, a jail uses it, and bans reach the node: the node holds verdicts of its own, or the action's last message says why not | whether bans arrive shows once the node runs |
| Firewall (`firewall`) | in enforce mode, the nftables backend can block; the running node's mode matches the configuration | nftables is probed without changing anything |
| SSH session (`session`) | the address your SSH session comes from is protected from being blocked, by the allow-list or an [override](../glossary.md#override) | the allow-list of the configuration |

The self-check changes nothing and never shows the node key.

### When it warns of a lockout

If your SSH session comes from an address that the node does not
protect, the report ends with a banner:

```text
!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!
!! LOCKOUT RISK: your SSH session comes from 85.10.3.20, and OBIE does not protect that address.
!! If this node or a trusted peer reports it, the node blocks it and you lose access to this server.
!! The node is in observe mode and blocks nothing yet, but it will as soon as you switch to enforce mode.
!! Protect it: add 85.10.3.20/32 to allowlist.cidrs in the configuration and reload the node
!! (sudo systemctl reload obied), or on a running node: sudo obiectl allow 85.10.3.20 --note "my SSH session"
!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!
```

In observe mode this is a warning; in enforce mode it is a problem. Add the
address, or better the network you administer from, to `allowlist.cidrs`
and reload the node. The self-check finds the address also when you run it
through `sudo` or `su`. Inside `tmux` or `screen`, a window keeps the
address of the login that opened it, which may not be the one you work
from now: run the self-check and the assistant from a fresh SSH login.

### Run it without root

As another user, the self-check still runs every check, but it cannot look
into the state directory or reach the admin socket. It says so at the top,
and each check that could not look reports a warning with the next step
`run the self-check as root: sudo obied self-check`.

### Use it in scripts

The exit status tells a script how it went:

| Exit status | Meaning |
|-------------|---------|
| 0 | no problem; warnings may remain |
| 1 | at least one problem |
| 2 | wrong usage, such as an unknown flag |
| 3 | the report could not be written |

`--json` prints the same report for machines. Each check has a fixed `id`
(the names in brackets in the table above), a `status` (`ok`, `warning` or
`problem`), a `summary`, and `details` and `next_steps` where it has any:

```sh
sudo obied self-check --json
```

```json
{
  "status": "warning",
  "config": "/etc/obie/obie.yaml",
  "version": "0.1.0",
  "user": "root",
  "root": true,
  "checks": [
    {
      "id": "peers",
      "name": "Peers",
      "status": "warning",
      "summary": "stand-alone node: no peers are configured, so the node acts only on what this server detects",
      "next_steps": ["if that is what you want, there is nothing to do; ..."]
    }
  ],
  "summary": {"ok": 8, "problem": 0, "warning": 1}
}
```

(The example shows one of the nine checks.) Further flags: `--config
<file>` checks another configuration, `--service-user <user>` names the
user `obied` runs as if it is not `obie`, and `--timeout <duration>` bounds
the whole check (one minute by default).

## How this page is tested

The questions, their suggestions, the refusal to replace a file without
consent and the identical result of both ways are exercised by
`TestSetupInteractiveMatchesNonInteractive`,
`TestSetupEnterTakesSafeDefaults`, `TestSetupNeverReplacesWithoutAsking`
and `TestSetupWithoutPermission` in `internal/cli`. Every check, its
statuses and next steps are tested in `internal/selfcheck`; the command
itself, before the first start and against a running node, by
`TestSelfCheckBeforeFirstStart` and `TestSelfCheckAgainstInProcessDaemon`.
