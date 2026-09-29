# Command-line reference

<!-- Generated from the help of obied and obiectl; do not edit. After changing
     the help, run: go run ./packaging/gendocs -reference documentation/operations/cli.md -->

Every command of `obied` and `obiectl`, with its flags and examples, as
`obied help <command>` and `obiectl help <command>` show it. The manual
pages `man obied` and `man obiectl` hold the same text; the release
downloads install them with the shell completion.

- [`obied`](#obied): run an OBIE node and prepare this server for it
- [`obiectl`](#obiectl): control the OBIE node running on this server

## obied

obied is the OBIE node. The obied service runs it with obied --config
/etc/obie/obie.yaml: it connects to your peers, exchanges signed verdicts
with them, decides which addresses to block and, in enforce mode, blocks
them in its own nftables table inet obie.

Its other commands work without a running node: they write the
configuration, check the node and this server, show or create the node's
identity key and remove OBIE's firewall table. obiectl controls the
running node.

```text
obied <command> [flags]
obied --config <file> [--check-config]
```

| Task | Command | What it does |
|------|---------|--------------|
| Look | [`self-check`](#obied-self-check) | check the node and this server; every problem comes with the next step |
| Look | [`identity`](#obied-identity) | show the node's peer ID and key fingerprint from its key file |
| Manage | [`setup`](#obied-setup) | write the node's configuration after a few questions (first-run assistant) |
| Manage | [`run`](#obied-run) | run the node in the foreground, as the obied service does |
| Manage | [`keygen`](#obied-keygen) | create the node's identity key |
| Manage | [`teardown-firewall`](#obied-teardown-firewall) | remove OBIE's nftables table inet obie, and with it every block |
| Manage | [`completion`](#obied-completion) | print the shell completion script for bash, zsh or fish |

Start here: sudo obied setup writes the configuration after a few questions;
then sudo systemctl enable --now obied starts the node, and sudo obied
self-check checks it and says what to do about anything that is not right.
Help on a command: obied help \<command\>, or obied \<command\> --help.
The running node is controlled with obiectl: obiectl --help.

Flags in place of a command run the node, as obied run does:

| Flag | What it does |
|------|--------------|
| `--check-config` | only check the configuration: exit 0 if it is valid, 1 if not (default: off) |
| `--config file` | path to the YAML configuration file (default: /etc/obie/obie.yaml) |
| `--version` | print the version and exit |

### obied self-check

Check the node and this server; every problem comes with the next step.

```text
obied self-check [--config <file>] [--service-user <user>] [--timeout <duration>] [--json]
```

Checks the node and this server, before the first start or at any time
later, and reports each check as OK, WARNING or PROBLEM with the next step:
configuration, identity, admin access, node, peers, clock, Fail2Ban,
firewall and your SSH session's address. It changes nothing. Run it as
root to let it look everywhere.

Exit status: 0 no problem (warnings may remain), 1 at least one problem,
2 wrong usage, 3 the report could not be written.

| Flag | What it does |
|------|--------------|
| `--config file` | configuration file of the node (default: /etc/obie/obie.yaml) |
| `--json` | print the report as JSON, for scripts (default: off) |
| `--service-user user` | user obied runs as (default: obie) |
| `--timeout duration` | give up after this duration, e.g. 30s (default: 1m0s) |

Examples:

```sh
# Check the node and this server
sudo obied self-check

# List the IDs of the checks that are not OK, for a script
sudo obied self-check --json | jq -r '.checks[] | select(.status != "ok") | .id'
```

### obied identity

Show the node's peer ID and key fingerprint from its key file.

```text
obied identity [--config <file> | --state-dir <directory>] [--json]
```

Reads the node's identity key from its state directory and shows its peer
ID and fingerprint, never the key itself. It works without a running node,
for example to check a backup of the key.

| Flag | What it does |
|------|--------------|
| `--config file` | path to the YAML configuration file naming node.state_dir (default: /etc/obie/obie.yaml) |
| `--json` | print the identity as JSON, for scripts (default: off) |
| `--state-dir directory` | state directory holding node.key (default: node.state_dir of --config) |

Examples:

```sh
# Show the peer ID of this node
sudo obied identity

# Check which node a backup of the key belongs to
sudo obied identity --state-dir /root/obie-backup
```

### obied setup

Write the node's configuration after a few questions (first-run assistant).

```text
obied setup [--config <file>]
obied setup --non-interactive [answer flags] [--force]
```

Writes the configuration of this node after asking a few questions:
where it keeps its state and audit log, which peers it connects to and
how much it trusts them, whether it starts in observe mode, and which
addresses it must never block. Every question offers a safe default. An
existing configuration file is only replaced after you agree; the old one
is kept as a backup. With --non-interactive the answers come from the
flags, and the same answers always write the same file.

| Flag | What it does |
|------|--------------|
| `--allow address or network` | an address or network never to block; repeat for more (default: none besides the protected addresses) |
| `--audit-log file` | audit log file (audit.path), or none (default: /var/log/obie/audit.jsonl) |
| `--config file` | configuration file to write (default: /etc/obie/obie.yaml) |
| `--force` | replace an existing configuration file; the old one is kept as a backup (default: off) |
| `--mode string` | observe or enforce; observe, which blocks nothing, is recommended at first (default: observe) |
| `--non-interactive` | ask nothing: take the answers from the flags below (default: off) |
| `--peer address[,name=NAME][,weight=0..1]` | a peer to connect to and trust: address\[,name=NAME\]\[,weight=0..1\] (weight 0.8 unless given); repeat for more peers (default: no peers) |
| `--state-dir directory` | state directory of the node (node.state_dir) (default: /var/lib/obie) |

Examples:

```sh
# Answer the questions and write /etc/obie/obie.yaml
sudo obied setup

# Write it without questions, with one peer and your office network
sudo obied setup --non-interactive --peer /dns4/obie.friend.example/tcp/4001/p2p/12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf,name=friend,weight=0.8 --allow 198.51.100.0/24
```

### obied run

Run the node in the foreground, as the obied service does.

```text
obied run [--config <file>] [--check-config]
```

Loads the configuration and the allow-list files and runs the node until
it receives SIGTERM or SIGINT; SIGHUP reloads the configuration. With
--check-config it only checks the configuration and names every problem
with its line. The obied service runs obied --config /etc/obie/obie.yaml,
which is the same. To run the node as a service: sudo systemctl enable
--now obied.

| Flag | What it does |
|------|--------------|
| `--check-config` | only check the configuration: exit 0 if it is valid, 1 if not (default: off) |
| `--config file` | path to the YAML configuration file (default: /etc/obie/obie.yaml) |
| `--version` | print the version and exit |

Examples:

```sh
# Check the configuration before you restart the node
sudo obied run --config /etc/obie/obie.yaml --check-config

# Run a test node with a configuration of your own in the foreground
obied run --config ./obie.yaml
```

### obied keygen

Create the node's identity key.

```text
obied keygen [--config <file> | --state-dir <directory>] [--force]
```

Creates the key the node signs its verdicts with, and from which its peer ID
comes, in the state directory, and shows the peer ID. The node creates
its key itself at its first start; keygen creates one ahead of time, for
example for a test node. The key must belong to the user the node runs as.

--force replaces an existing key: the node gets a new peer ID, and the
peers that trust the old one must change their configuration.

| Flag | What it does |
|------|--------------|
| `--config file` | path to the YAML configuration file naming node.state_dir (default: /etc/obie/obie.yaml) |
| `--force` | replace an existing key; this changes the node's peer ID (default: off) |
| `--state-dir directory` | state directory holding node.key (default: node.state_dir of --config) |

Examples:

```sh
# Create the key of a test node in a directory of your own
obied keygen --state-dir ./node-a

# Replace a stolen key; the peer ID changes
sudo systemctl stop obied && sudo -u obie obied keygen --force
```

### obied teardown-firewall

Remove OBIE's nftables table inet obie, and with it every block.

```text
obied teardown-firewall [--on-stop [--config <file>]]
```

Deletes the nftables table inet obie, which holds every block OBIE applied,
and nothing else: your own firewall rules stay. Use it when you are locked
out or before you uninstall OBIE. Stop the node first, or in enforce mode
it puts its blocks back.

With --on-stop, as the systemd unit's ExecStopPost, it only removes the
table if enforce.nftables.teardown_on_stop is true in the configuration.

| Flag | What it does |
|------|--------------|
| `--config file` | path to the YAML configuration file, read with --on-stop (default: /etc/obie/obie.yaml) |
| `--on-stop` | only remove the table if enforce.nftables.teardown_on_stop is true (for ExecStopPost) (default: off) |

Examples:

```sh
# Lift every OBIE block at once
sudo systemctl stop obied && sudo obied teardown-firewall
```

### obied completion

Print the shell completion script for bash, zsh or fish.

```text
obied completion bash | zsh | fish
```

Prints a script that lets your shell complete obied's commands, flags
and flag values when you press Tab. The release downloads ship these
scripts and install.sh installs them; this command is for another shell
setup or a place install.sh does not cover.

Examples:

```sh
# Complete in bash, for your user
obied completion bash > ~/.local/share/bash-completion/completions/obied

# Complete in fish, for your user
obied completion fish > ~/.config/fish/completions/obied.fish
```

## obiectl

obiectl talks to the node on this server, obied, through its admin socket.
It shows what the node knows and decides, lets you overrule it for single
addresses and ranges, and reports attacks to your peers as signed verdicts.

It needs a running node, and root or membership in the group that owns the
admin socket (admin.socket_group, obie by default).

```text
obiectl [global flags] <command> [flags] [arguments]
```

| Task | Command | What it does |
|------|---------|--------------|
| Look | [`status`](#obiectl-status) | show whether the node runs, its mode and the state of its subsystems |
| Look | [`peers`](#obiectl-peers) | list the connected peers with their trust weight and latency |
| Look | [`identity`](#obiectl-identity) | show this node's peer ID and key fingerprint |
| Look | [`decisions`](#obiectl-decisions) | list what the node decided for each address: block, allowed or none |
| Look | [`explain`](#obiectl-explain) | explain why an address or range is or is not blocked |
| Look | [`indicators`](#obiectl-indicators) | list the addresses and ranges that have active verdicts |
| Look | [`show`](#obiectl-show) | show every active verdict on one address or range |
| Look | [`overrides`](#obiectl-overrides) | list your overrides: the addresses you always allow or always block |
| Look | [`enforced`](#obiectl-enforced) | list the blocks the firewall applies right now |
| Decide | [`allow`](#obiectl-allow) | always allow an address or range: never block it |
| Decide | [`block`](#obiectl-block) | always block an address or range, whatever its score |
| Decide | [`unoverride`](#obiectl-unoverride) | remove your override of an address or range |
| Report | [`report`](#obiectl-report) | publish a signed verdict on an attacking address or range |
| Report | [`revoke`](#obiectl-revoke) | withdraw a verdict this node published |
| Manage | [`console`](#obiectl-console) | show the web console's address and sign-in token |
| Manage | [`completion`](#obiectl-completion) | print the shell completion script for bash, zsh or fish |

Start here: sudo obiectl status shows whether the node runs and whether it
blocks (enforce mode) or only observes. Then sudo obiectl decisions --state block
lists what it blocks, or would block in observe mode.
Help on a command: obiectl help \<command\>, or obiectl \<command\> --help.

Global flags, before the command:

| Flag | What it does |
|------|--------------|
| `--socket socket` | path of the node's admin socket, admin.socket in its configuration (default: /run/obie/obie.sock) |
| `--timeout duration` | give up on the node after this duration, e.g. 5s (default: 10s) |
| `--version` | print the version and exit |

### obiectl status

Show whether the node runs, its mode and the state of its subsystems.

```text
obiectl status [--json]
```

Shows the node's mode first: OBSERVE (it decides but blocks nothing) or
ENFORCE (it blocks through the firewall). Then its version, how long it has
been running, whether it is ready, and one row per subsystem with its
state and any error. Run it first when something seems wrong.

| Flag | What it does |
|------|--------------|
| `--json` | print the status as JSON, for scripts (default: off) |

Examples:

```sh
# See whether the node runs and whether it blocks
sudo obiectl status

# The same for a monitoring script
sudo obiectl status --json
```

### obiectl peers

List the connected peers with their trust weight and latency.

```text
obiectl peers [--json]
```

Lists every peer the node is connected to right now: its peer ID, the name
and trust weight you gave it in trust.publishers, whether it is a bootstrap
peer, since when it is connected, the round-trip time and its addresses.
A peer you configured that is not listed is not connected; obied
self-check tests whether it answers.

| Flag | What it does |
|------|--------------|
| `--json` | print the peers as JSON, for scripts (default: off) |

Examples:

```sh
# See which peers are connected
sudo obiectl peers

# Only their peer IDs, for a script
sudo obiectl peers --json | jq -r '.peers[].peer_id'
```

### obiectl identity

Show this node's peer ID and key fingerprint.

```text
obiectl identity [--json]
```

Shows the peer ID that other operators put into their configuration to
connect to and trust this node, and the fingerprint of its key, never the
key itself. obied identity shows the same without a running node.

| Flag | What it does |
|------|--------------|
| `--json` | print the identity as JSON, for scripts (default: off) |

Examples:

```sh
# Show the peer ID to give to a friend's node
sudo obiectl identity
```

### obiectl decisions

List what the node decided for each address: block, allowed or none.

```text
obiectl decisions [--state block|none|allowed] [--limit <n>] [--json]
```

Lists the node's decision on every address and range it holds verdicts or
overrides for: block (it blocks the address, or would in observe mode),
allowed (the allow-list or an override protects it) or none (not enough
trusted publishers agree), with the score, the number of publishers, when
the decision ends and why it was made. obiectl explain shows the details
of one address.

It starts with how many decisions there are in each state, then lists at
most --limit of them, blocks first; the last line says how to see the
others. --json always has every decision.

| Flag | What it does |
|------|--------------|
| `--json` | print the decisions as JSON, for scripts (default: off) |
| `--limit number` | show at most this number of rows; 0 shows every row (--json always has every row) (default: 100) |
| `--state state` | list only the decisions in state: block, none or allowed (default: every state) |

Examples:

```sh
# See what the node blocks, or would block in observe mode
sudo obiectl decisions --state block

# List every decision, however many there are
sudo obiectl decisions --limit 0

# Every decision as JSON, for a script
sudo obiectl decisions --json
```

### obiectl explain

Explain why an address or range is or is not blocked.

```text
obiectl explain [--json] <address | range>
```

Shows the decision on the address or range and the reason for it: the
score against the threshold, the number of publishers against the quorum,
whether local autoblock, the allow-list or an override applies, and one row
per publisher with its trust weight, confidence and share of the score. The
address may also be written as an indicator key, such as ipv4:203.0.113.7.

| Flag | What it does |
|------|--------------|
| `--json` | print the explanation as JSON, for scripts (default: off) |

Examples:

```sh
# Ask why an address is blocked, or why not
sudo obiectl explain 203.0.113.7

# Ask about a whole range
sudo obiectl explain 198.51.100.0/24
```

### obiectl indicators

List the addresses and ranges that have active verdicts.

```text
obiectl indicators [--mine | --publisher <peer ID>] [--limit <n>] [--cursor <cursor>] [--json]
```

Lists the active verdicts the node holds, one row per verdict, grouped by
address or range, a page at a time. When more follow, the last line says
how to get the next page. Unlike the other listings, --limit is the size
of a page and --json holds one page too, with next_cursor for the next.

| Flag | What it does |
|------|--------------|
| `--cursor cursor` | continue after this cursor from the previous page (default: the first page) |
| `--json` | print the indicators as JSON, for scripts (default: off) |
| `--limit size` | page size, in the table and in --json; 0 is the node's default of 100, at most 1000 |
| `--mine` | list only the verdicts of this node (default: off) |
| `--publisher peer ID` | list only the verdicts of the publisher with this peer ID (default: every publisher) |

Examples:

```sh
# See what this node has reported itself
sudo obiectl indicators --mine

# See what one peer has reported
sudo obiectl indicators --publisher 12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf
```

### obiectl show

Show every active verdict on one address or range.

```text
obiectl show [--json] <address | range>
```

Lists the active verdicts on the address or range from every publisher,
with their action, confidence, number of events, reason, when they were
issued and end, and their event ID, which obiectl revoke accepts.

| Flag | What it does |
|------|--------------|
| `--json` | print the verdicts as JSON, for scripts (default: off) |

Examples:

```sh
# See who reported an address and why
sudo obiectl show 203.0.113.7
```

### obiectl overrides

List your overrides: the addresses you always allow or always block.

```text
obiectl overrides [--json]
```

Lists the overrides set with obiectl allow and obiectl block, with when each
ends and the note you gave it.

| Flag | What it does |
|------|--------------|
| `--json` | print the overrides as JSON, for scripts (default: off) |

Examples:

```sh
# See which addresses you overruled
sudo obiectl overrides
```

### obiectl enforced

List the blocks the firewall applies right now.

```text
obiectl enforced [--limit <n>] [--json]
```

Lists the addresses and ranges the enforcement backend applies, with when
each block ends: first how many there are, then at most --limit of them.
In observe mode the list is empty. sudo nft list table inet obie shows
what the kernel holds.

| Flag | What it does |
|------|--------------|
| `--json` | print the entries as JSON, for scripts (default: off) |
| `--limit number` | show at most this number of rows; 0 shows every row (--json always has every row) (default: 100) |

Examples:

```sh
# See what the firewall blocks right now
sudo obiectl enforced
```

### obiectl allow

Always allow an address or range: never block it.

```text
obiectl allow <address | range> [--ttl <duration>] [--note <text>] [--json]
```

Sets an override that never blocks the address or range, whatever the mesh
reports. It beats every other rule, including the allow-list and always-block
overrides on overlapping ranges. It stays until you remove it with obiectl
unoverride, or until --ttl ends it.

| Flag | What it does |
|------|--------------|
| `--json` | print the result as JSON, for scripts (default: off) |
| `--note string` | why the override was set, shown by obiectl overrides and explain (default: no note) |
| `--ttl duration` | remove the override after this duration, e.g. 90m, 36h or 7d (default: never) |

Examples:

```sh
# Never block your office network
sudo obiectl allow 198.51.100.0/24 --note "office"

# Allow an address for a day while you look into a false alarm
sudo obiectl allow 203.0.113.7 --ttl 1d --note "false alarm, ticket 4711"
```

### obiectl block

Always block an address or range, whatever its score.

```text
obiectl block <address | range> [--ttl <duration>] [--note <text>] [--json]
```

Sets an override that blocks the address or range whatever the mesh
reports. It beats the networks you added to the allow-list (allowlist.cidrs
and allowlist.files), but never the protected addresses: the built-in
ranges, this node's own addresses and its bootstrap peers. In observe mode
nothing is blocked, but the decision shows it.

| Flag | What it does |
|------|--------------|
| `--json` | print the result as JSON, for scripts (default: off) |
| `--note string` | why the override was set, shown by obiectl overrides and explain (default: no note) |
| `--ttl duration` | remove the override after this duration, e.g. 90m, 36h or 7d (default: never) |

Examples:

```sh
# Block a scanner for a week
sudo obiectl block 203.0.113.7 --ttl 7d --note "scans our web server"
```

### obiectl unoverride

Remove your override of an address or range.

```text
obiectl unoverride <address | range> [--json]
```

Removes the override set with obiectl allow or obiectl block. The node then
decides on the address as before, by the allow-list and the verdicts, and
shows the decision now.

| Flag | What it does |
|------|--------------|
| `--json` | print the result as JSON, for scripts (default: off) |

Examples:

```sh
# Let the mesh decide on an address again
sudo obiectl unoverride 203.0.113.7
```

### obiectl report

Publish a signed verdict on an attacking address or range.

```text
obiectl report --protocol <service> --reason <class> [flags] <address | range>
obiectl report --protocol <service> --reason <class> [flags] --ip <address | range>
```

Turns a detection on this server into a verdict signed with the node's key
and sends it to the peers. With local autoblock (on by default) it counts
on this node at once. Log lines given as evidence are hashed on this
server: only the hash and the number of events leave it. Protected and
allow-listed addresses are never reported.

Reporting the same address again within a minute adds the events to the
next refresh of the verdict; later reports refresh it.

| Flag | What it does |
|------|--------------|
| `--action action` | suggested action: ban or watch (default: ban) |
| `--confidence number` | how sure you are, a number from 0 to 1 (default: 0.8) |
| `--events number` | number of malicious events observed (default: 1) |
| `--evidence-file file` | file with the log lines behind the report (- for stdin); only their SHA-256 hash leaves this host (default: no evidence) |
| `--evidence-from-stdin` | read the log lines behind the report from stdin, like --evidence-file - (default: off) |
| `--ip address` | attacking address or range, in place of the argument (default: the argument) |
| `--json` | print the response as JSON, for scripts (default: off) |
| `--mitre IDs` | comma-separated MITRE ATT&CK technique IDs, e.g. T1110 (default: none) |
| `--protocol service` | attacked service, e.g. ssh (required) |
| `--reason class` | behavior class, e.g. password_bruteforce (required) |
| `--ttl lifetime` | verdict lifetime, e.g. 12h or 7d (default decision.default_ttl, capped at decision.max_ttl) |

Examples:

```sh
# Report an SSH brute force seen in 12 failed logins
sudo obiectl report --protocol ssh --reason password_bruteforce --events 12 203.0.113.7

# Report it with the log lines as evidence; only their hash leaves the server
grep 203.0.113.7 /var/log/auth.log | sudo obiectl report --protocol ssh --reason password_bruteforce --evidence-from-stdin 203.0.113.7

# Ask peers to watch, not block, a scanning range for 12 hours
sudo obiectl report --protocol http --reason scanning --action watch --ttl 12h 198.51.100.0/24
```

### obiectl revoke

Withdraw a verdict this node published.

```text
obiectl revoke [--reason <reason>] [--json] <event ID | address | range>
```

Revokes this node's own active verdict with that event ID, or its verdicts
on that address or range, and tells the peers. Only the publisher of a
verdict can revoke it. obiectl show lists the event IDs.

| Flag | What it does |
|------|--------------|
| `--json` | print the revocations as JSON, for scripts (default: off) |
| `--reason string` | why the verdict is withdrawn, e.g. false_positive (default: false_positive) |

Examples:

```sh
# Withdraw a report that was a false alarm
sudo obiectl revoke 203.0.113.7

# Withdraw one verdict by its event ID
sudo obiectl revoke --reason incident_closed 1b4e28ba-2fa1-41d2-883f-0016d3cca427
```

### obiectl console

Show the web console's address and sign-in token.

```text
obiectl console [--rotate] [--json]
```

Prints where the web console serves and the token to sign in with, and on
standard error how to open it, also from another machine. The console is
off unless console.enabled is true in the configuration.

| Flag | What it does |
|------|--------------|
| `--json` | print the console and its token as JSON, for scripts (default: off) |
| `--rotate` | issue a new token: the old one stops working and every browser is signed out (default: off) |

Examples:

```sh
# Get the address and the token to sign in
sudo obiectl console

# Issue a new token and sign every browser out
sudo obiectl console --rotate
```

### obiectl completion

Print the shell completion script for bash, zsh or fish.

```text
obiectl completion bash | zsh | fish
```

Prints a script that lets your shell complete obiectl's commands, flags
and flag values when you press Tab. The release downloads ship these
scripts and install.sh installs them; this command is for another shell
setup or a place install.sh does not cover.

Examples:

```sh
# Complete in bash, for your user
obiectl completion bash > ~/.local/share/bash-completion/completions/obiectl

# Complete in fish, for your user
obiectl completion fish > ~/.config/fish/completions/obiectl.fish
```
