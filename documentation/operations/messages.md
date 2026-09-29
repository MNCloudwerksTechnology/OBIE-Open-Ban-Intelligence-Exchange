# Messages of obied and obiectl

This page lists every message that `obied`, `obiectl` and the log of the
[node](../glossary.md#node) show when something is wrong or needs your
attention, with what it means and what to do. It also states the rules
these messages follow, so that the next command and the next message
follow them too.

Look a message up by its first words. The errors of the two command-line
tools also have an ID, the first column below, which stays the same when
the wording changes. For the checks of `obied self-check`, see
[Set up and check a node](setup.md#check-the-node); for the refusals of
the web console, see [Web console](console.md#when-it-does-not-work).

Two tests keep this page and the tools in step. `TestMessageInventory`
(`test/docs/messages_test.go`) fails when the code has an error ID, or a
warning or error the node logs, that is not listed here, and when this
page lists one the code no longer has. `TestEveryCommandIsDocumented`
(`internal/cli/help_test.go`) fails when a command has no summary, no
example that parses, or a flag without a documented default.

## Rules

### What, why, next

An error of `obied` or `obiectl` says what went wrong, why when the tool
knows, and what to do next:

```text
obiectl status: obied is not running: there is no admin socket /run/obie/obie.sock
  Why:  the node was not started, has stopped, or uses another socket
  Next: start it: sudo systemctl start obied
  Next: if it does not stay up, see why: sudo journalctl -u obied -n 20, or sudo obied self-check
  Next: if it listens on another socket (admin.socket), name that: obiectl --socket <path> status
```

1. The first line starts with the program and its command and says
   **what** went wrong. It names the file, address, socket, user or
   setting involved. Particulars follow on lines of their own, such as
   every mistake in a configuration file as `file:line: setting: problem`,
   the form editors and `grep` understand.
2. `Why:` says why it happened, when the tool can tell. It is left out
   rather than guessed.
3. Each `Next:` line is one thing to do, the most likely first. Commands
   are complete and can be copied, with `sudo` where they need root, and
   keep the `--config`, `--state-dir` or `--socket` you gave. Settings are
   named by their key in `obie.yaml`, such as `admin.socket_group`.
4. A usage mistake, such as an unknown flag, a missing argument or an
   address that is none, is one line and where the help is.
5. The node logs JSON lines. `msg` says what happened and what the node
   does about it, `error` why, and `next` what to do where you must act.
   A line that points to a fault in OBIE itself has no `next`: report it
   in an [issue](https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange/issues),
   with the line.

### Words, times and numbers

- The words are those of the [glossary](../glossary.md), such as
  [peer](../glossary.md#peer), [verdict](../glossary.md#verdict),
  [allow-list](../glossary.md#allow-list) and
  [override](../glossary.md#override), used the same way in every command,
  message and guide.
- Times are RFC 3339 in UTC: `2026-09-29T13:05:00Z`.
- Spans of time are given to the second, with days for long ones: `45s`,
  `1h2m3s`, `3d`, `3d4h5m6s`. Configuration and flags take the same form
  (`--ttl 3d`).
- Scores and [trust weights](../glossary.md#trust-weight) are decimal numbers (`1.8`), and counts say
  what they count (`3 peers`).
- There is no colour. Labels are words (`block`, `allowed`, `OBSERVE`),
  so output reads the same in a terminal, a pipe, a file and a screen
  reader.

### Output for people and for programs

- `--help`, `-h` and `help <command>` print the full help of a command to
  standard output: its purpose, every flag with its default, and examples.
  Without a command, both tools list their commands grouped by task
  (look, decide, report, manage) and say where to start.
- Errors go to standard error. The exit status is 0 on success, 1 when the
  command failed (also for an invalid configuration), 2 for a usage
  mistake and 3 when the output could not be written.
- Every listing command has `--json`, which keeps its field names; scripts
  use it. Tables are for people and may change.
- Long listings, such as thousands of decisions, start with a summary,
  show blocks first and at most `--limit` rows (100 by default), and say
  below the table how to see the rest: `--limit 0` or `--json`, which
  are complete.
- `obiectl indicators` is the exception: the node hands out verdicts a
  page at a time, so `--limit` is the size of a page (0 is the node's
  default of 100, at most 1000), in the table and in `--json` alike, and
  `--cursor` fetches the next page. The last line of the table, and
  `next_cursor` in `--json`, name the cursor.
- Nothing asks a question unless it runs in a terminal. `obied setup`
  refuses to ask into a pipe and points to `--non-interactive`.

### Adding a command or a message

- A new command goes into the command registry of its tool
  (`internal/cli/ctl_commands.go` or `internal/cli/daemon_commands.go`)
  with its task group, a one-line summary, a description and at least one
  realistic example. Every flag's usage text says what it does; its
  default is shown from the flag itself. Then regenerate the
  [command-line reference](cli.md):
  `go run ./packaging/gendocs -reference documentation/operations/cli.md`.
- A new error of a command is a `problem` (`internal/cli/problem.go`) with
  a new ID, written as a string literal, and a row below. Give it a
  `Next:` step and a test that shows it.
- A new warning or error in the node's log gets a row below, and a `next`
  attribute when you must act on it.

## Command-line errors

Every `obiectl` command that asks the node, which is all but
`completion` and `help`, can hit the `node-…` and
`admin-permission-denied` errors. `Why` and `Next` are
shortened here; the tools print them in full.

| ID | Where | Message (what) | Why, and what to do next |
|----|-------|----------------|--------------------------|
| `usage` | every command | a flag or argument mistake, e.g. `obiectl report: --protocol is missing: name the attacked service, e.g. --protocol ssh` or `unexpected argument "x"` | Next: see how to use it, `<command> --help`. Exit status 2. |
| `unknown-command` | both tools | `unknown command "statu"; did you mean "status"?`, then the commands grouped by task | Next: choose one of them; `help <command>` explains it. Exit status 2. |
| `invalid-address` | `obiectl allow`, `block`, `unoverride`, `explain`, `show`, `report`, `revoke` | `"10.0.0.300" is not an IP address or range`; for `revoke`, `… is neither an event ID nor an IP address or range`; for a range OBIE does not act on, the reason, e.g. `"10.0.0.0/8" is broader than /16` | Next: give an address such as `203.0.113.7` or `2001:db8::7`, or a range such as `203.0.113.0/24`. For a range that is too broad, Why: OBIE acts on ranges of at most /16 in IPv4 and /32 in IPv6; Next: a narrower range or single addresses. Checked before the node is asked; exit status 2. |
| `node-not-running` | `obiectl` | `obied is not running: there is no admin socket /run/obie/obie.sock`, or `… nothing answers on the admin socket …` | Why: the node was not started, has stopped, or uses another socket. Next: `sudo systemctl start obied`; if it does not stay up, `sudo journalctl -u obied -n 20` or `sudo obied self-check`; another socket: `obiectl --socket <path>`. |
| `admin-permission-denied` | `obiectl` | `permission denied: user alice may not use the admin socket /run/obie/obie.sock` | Why: only root and members of the group `obie` (`admin.socket_group`) may control the node. Next: run it with `sudo`, or join the group: `sudo usermod -aG obie alice`, then log in again. |
| `node-timeout` | `obiectl` | `obied did not answer in time (--timeout)` | Why: the node is busy, still starting, or stuck. Next: `sudo obiectl --timeout 30s <command>`; `sudo obiectl status` and `sudo journalctl -u obied -n 50`. With `--socket`, the commands keep it. |
| `node-unreachable` | `obiectl` | `cannot talk to obied: <error>` | Next: `sudo obied self-check` checks the node and its admin socket. |
| `node-unavailable` | `obiectl` | the node's answer that a part is not available yet | Why: the node is still starting, or one of its parts failed. Next: `sudo obiectl status` shows which; `sudo journalctl -u obied -n 50`. |
| `node-failed` | `obiectl` | the node's answer that the request failed inside it | Next: see why in the node's log, `sudo journalctl -u obied -n 50`. |
| `node-error` | `obiectl` | `the node answered <status>: <message>`, for any other answer | Next: see the node's log, `sudo journalctl -u obied -n 50`. |
| `request-invalid` | `obiectl` | `the node refused the request: <reason>` | Next: check the arguments, `obiectl <command> --help`. |
| `not-found` | `obiectl explain`, `show`, `revoke` | what the node does not know, e.g. no verdict with that event ID | Next: for `revoke`, `sudo obiectl indicators --mine` lists what this node reported; otherwise `sudo obiectl decisions` lists what the node knows. |
| `no-override` | `obiectl unoverride` | `no override on 203.0.113.7` | Next: `sudo obiectl overrides` lists your overrides. |
| `address-protected` | `obiectl report` | `nothing was reported: 192.168.1.9 is not a public address: …`, or `… overlaps the allow-listed network …` | Why: OBIE never reports private, loopback, link-local and other special-purpose addresses, nor the networks on your allow-list. Next: nothing, if this is right; `sudo obiectl explain <address>` shows the rule; to report it after all, remove it from `allowlist.cidrs` and reload. For an address reserved for examples, such as `203.0.113.7` from the help: report the attacking address from your log instead. |
| `block-protected` | `obiectl block` | `warning: …`, the override is kept but has no effect | Why: protected addresses are never blocked, not even by an override, so that OBIE cannot cut this server off. Next: remove it, `sudo obiectl unoverride <address>`. Exit status 0. |
| `block-overruled` | `obiectl block` | `warning: the force-block does not take effect: operator force-allow override on cidr:198.51.100.0/24; …` | Why: an always-allow override beats every other rule, also an always-block override. Next: to block the address, remove the always-allow override it names, `sudo obiectl unoverride 198.51.100.0/24`; otherwise remove the block. Exit status 0. |
| `evidence-unreadable` | `obiectl report` | `cannot use the evidence of --evidence-file: <error>` | Next: check the file, or pass the log lines on standard input: `grep <address> <log file> \| sudo obiectl report --evidence-from-stdin …`. |
| `config-invalid` | `obied`, `obied --check-config`, `identity`, `keygen`, `teardown-firewall` | `the configuration /etc/obie/obie.yaml is invalid:`, then one `file:line: setting: problem` line per mistake; or `… cannot be read: <error>` when it is no YAML | Next: fix these settings, then `sudo obied --check-config`; every setting is described in `/etc/obie/obie.yaml.example` and in the [configuration reference](configuration.md). |
| `config-missing` | as `config-invalid` | `the configuration file /etc/obie/obie.yaml does not exist` | Next: write it after a few questions, `sudo obied setup`, or name the file with `--config <file>`. |
| `config-unreadable` | as `config-invalid` | `cannot read the configuration file /etc/obie/obie.yaml as user alice: permission denied` | Why: only root and the group `obie` may read it, because it may name internal networks. Next: run it with `sudo`. |
| `allowlist-file-invalid` | `obied`, `obied --check-config` | the file and line, e.g. `allow-list file /etc/obie/allow.txt:2: invalid address "foo": want an IP address or CIDR range` | Why: the node reads every file in `allowlist.files` at start and on every reload; each line holds one address or network. Next: fix or create the file, or remove it from `allowlist.files`; then `sudo obied --check-config`. |
| `identity-missing` | `obied identity` | `this node has no identity yet: /var/lib/obie/node.key does not exist`, or `… its state directory /var/lib/obie does not exist` | Why: the node creates its identity key the first time it starts. Next: `sudo systemctl enable --now obied`; or `sudo -u obie obied keygen`; or restore the key from your backup; or name the state directory, `--state-dir <directory>`. |
| `identity-unreadable` | `obied identity` | `cannot read the identity key in /var/lib/obie as user alice: permission denied` | Why: the key belongs to the user the node runs as, and nobody else may read it. Next: run it with `sudo`. |
| `identity-unusable` | `obied identity` | `insecure key file …` (mode or owner wrong) or `corrupted key file …` | Next: the fix the message names, such as `chmod 600` on the key or restoring it from a backup; otherwise `sudo obied self-check`. |
| `identity-exists` | `obied keygen` | `/var/lib/obie/node.key exists already: this node has an identity` | Next: keep it, `obied identity` shows its peer ID; to replace it, `obied keygen --force`, which gives the node a new peer ID that its peers must be told. Both keep the `--state-dir` or `--config` you gave. |
| `keygen-failed` | `obied keygen` | `cannot create the identity key in /var/lib/obie: <error>` | Why: the key and its directory must belong to the user the node runs as. Next: `sudo -u obie obied keygen`. |
| `state-dir-unusable` | `obied keygen` | `state directory has a newer format: …` or another problem with the state directory | Next: the fix the message names, such as running the newer `obied` again or restoring a backup; otherwise `sudo obied self-check`. |
| `teardown-failed` | `obied teardown-firewall` | `cannot remove the table inet obie: the kernel refused`, or `… <error>` | Why: changing the firewall needs root (`CAP_NET_ADMIN`). Next: `sudo obied teardown-firewall`; otherwise `sudo nft delete table inet obie`. |
| `setup-no-terminal` | `obied setup` | `obied setup asks questions, but its input or output is not a terminal` | Why: questions written into a pipe or a file would go unseen. Next: run it in a terminal, or give the answers as flags with `--non-interactive`. Exit status 2. |
| `setup-path-refused` | `obied setup` | why the file cannot be the configuration, e.g. `… is not a regular file (a symbolic link?), which obied setup never replaces` | Next: the step the message names, otherwise another file, `--config <file>`. |
| `setup-cannot-write` | `obied setup` | `cannot write /etc/obie/obie.yaml as user alice: permission denied` | Why: the configuration directory belongs to root. Next: `sudo obied setup`; as root, choose a place it can write, `--config <file>`. |
| `setup-file-exists` | `obied setup --non-interactive` | `/etc/obie/obie.yaml exists already (…)` | Why: `obied setup` never replaces a configuration without your consent. Next: add `--force`; the old file is kept as a backup. |
| `setup-input-ended` | `obied setup` | `the input ended before every question was answered; nothing was written` | Next: set up without questions, `--non-interactive` with the answers as flags. |
| `setup-write-failed` | `obied setup` | `cannot write the configuration: <error>` | Next: check the directory, its permissions and free space, then run `obied setup` again. |

## Other command-line messages

These say what happened and what follows; they are no errors.

| Where | Message | Meaning |
|-------|---------|---------|
| `obiectl status` | `the node is not ready; sudo obied self-check says what to do about each subsystem …` | A part of the node is not running or not ready; the table above it names it. |
| `obiectl peers` | `if this node should have peers, sudo obied self-check tests whether each configured peer answers …` | No [peer](../glossary.md#peer) is connected; [No peers](troubleshooting.md#no-peers) lists the usual causes. |
| `obiectl console` | `to switch the console on, set console.enabled: true …`, `the console is switched on but not serving …`, `open http://127.0.0.1:9465/ in a browser on this host …` | What to do to reach the web console. |
| `obiectl console --rotate` | `issued a new console token; the old one no longer works …` | Every browser must sign in again. |
| `obied --check-config` | `configuration /etc/obie/obie.yaml is valid` | Standard output, exit status 0. |
| `obied keygen` | `wrote a new node key to /var/lib/obie/node.key`; with `--force` also `a running obied keeps its old key until it is restarted` | Restart the node to use the new key. |
| `obied teardown-firewall` | `table inet obie removed; nothing is blocked by OBIE anymore`; with `--on-stop`, `keeping table inet obie: enforce.nftables.teardown_on_stop is not set` | The blocks are gone, or were kept on purpose. |
| `obied setup` | `Nothing was written.` | You answered no to the last question. |
| every command | `writing <what>: <error>` | The output could not be written, e.g. to a full disk; exit status 3. |

## install.sh

| Message | What to do |
|---------|------------|
| `unexpected argument …; see --help` | `install.sh` takes no arguments; `PREFIX` and `DESTDIR` are environment variables. |
| `PREFIX must be an absolute path, got '…'` | Give an absolute path, such as `PREFIX=/opt/obie`. |
| `PREFIX must not contain spaces, # or & nor lie below /home or /root (the unit sets ProtectHome), got '…'` | Choose another prefix, such as `/opt/obie`. |
| `…/bin/obied is missing; run install.sh from an extracted release tarball` (or another file) | Extract the whole tarball and run `install.sh` from its directory. |
| `must run as root (or with DESTDIR set)` | `sudo ./install.sh`. |

## Log messages of the node

`obied` logs JSON lines to the journal (`sudo journalctl -u obied`). These
are all its warnings (`"level":"WARN"`) and errors (`"level":"ERROR"`).
`What to do` is also in the line's `next` attribute where the node knows
it. **Report** means the line points to a fault in OBIE: nothing needs to be
done unless it repeats; then report it with the line.

| Message | Level | Meaning and what to do |
|---------|-------|------------------------|
| `obied failed` | error | The node could not start or stopped with an error (`error`); `next` names the step, and [obied does not start](troubleshooting.md#obied-does-not-start) lists the usual causes. |
| `subsystem failed to start; stopping started subsystems` | error | One part could not start (`subsystem`, `error`); the node stops and logs `obied failed` with the next step. |
| `subsystem failed to stop` | error | A part did not stop cleanly at shutdown. Report. |
| `configuration reload rejected; the running configuration is kept` | error | A reload found a mistake (`error`); the node runs on unchanged. `sudo obied --check-config` names every problem; fix them and reload again. |
| `configuration changes that need a restart were not applied` | warn | A reload changed settings that only a restart applies (`keys`): `sudo systemctl restart obied`. |
| `bootstrap peer unreachable` | warn | A [bootstrap peer](../glossary.md#bootstrap-peer) could not be reached; the node retries (`retry_in`). Check that its node runs, that port 4001 is open both ways and that the peer ID is right ([No peers](troubleshooting.md#no-peers)). |
| `bootstrap peer disconnected` | warn | A connected bootstrap peer went away; the node dials it again (`redial_in`). |
| `ignoring bootstrap peer: it is this node` | warn | `mesh.bootstrap` lists this node itself; remove that entry. |
| `cannot resolve a bootstrap peer for the allow-list; it stays unprotected until the next reload` | warn | The name in the peer's address did not resolve. Check it with `getent hosts <name>`; reload once it resolves. |
| `cannot list the interface addresses for the allow-list; list public addresses in allowlist.cidrs` | warn | The node cannot protect its own addresses by itself; add them to `allowlist.cidrs`. |
| `admin socket group not found; keeping the process group` | warn | `admin.socket_group` names no group. Create it (`sudo groupadd --system obie`) or name an existing one, then restart the node. |
| `admin socket group not found; only root and obied's own user may use the admin API` | warn | As above; until then only root may use `obiectl`. |
| `admin socket group not found; only root and obied's own user may use the console` | warn | As above, for the web console. |
| `refusing an admin API request` | warn | A local user outside the admin group tried `obiectl` (`uid`); they get `admin-permission-denied`. |
| `refusing an admin API request with unknown peer credentials` | warn | The node could not tell which user connected, and refused. Report if it repeats. |
| `cannot look up the groups of a local peer` | warn | The node could not read a user's groups and refused that user; check the user database (`id <user>`). |
| `console not started; the node runs without it` | error | The web console cannot listen (`listen`, `error`), usually because the port is taken. Free it or change `console.listen`, then reload. |
| `console not moved; it keeps serving at its old address` | error | A reload changed `console.listen`, but the new address cannot be used; the console stays where it was (`url`). |
| `stopping the console` | warn | The console did not stop cleanly at shutdown. Report. |
| `console action failed` | error | An action in the console, such as an override, failed (`action`, `error`); the page says so as well. |
| `rendering a console page` | error | A console page could not be shown. Report. |
| `console sign-in with a wrong token` | warn | Someone signed in with a token that is not the current one; `sudo obiectl console` shows the right one. |
| `console sign-in refused: too many attempts` | warn | Sign-ins come too fast; the console refuses them for a few seconds. |
| `refusing a console connection of a local user outside the admin group` | warn | A local user who is not root and not in the admin group opened the console (`user`). |
| `refusing a console connection whose local user cannot be told` | warn | The console could not tell which local user connected, and refused. |
| `HTTP server stopped serving` | error | The metrics or console server stopped (`addr`, `error`). Restart the node; report if it repeats. |
| `enforcement failed; retrying` | error | The node could not change the firewall (`error`) and retries (`retry_in`). `sudo obied self-check` checks the firewall access; `sudo obiectl enforced` shows what is applied. |
| `enforce.max_entries reached; the lowest-score blocks are not applied` | warn | More blocks than `enforce.max_entries`; raise it if the host can hold more. |
| `block decision refused by the allow-list right before apply` | warn | A block would have hit an address on the [allow-list](../glossary.md#allow-list), which always wins; nothing to do. |
| `block decision without an address range, not applied` | warn | A decision had no usable address. Report. |
| `enforce mode: no enforcement backend, block decision not applied` | warn | The node runs in [enforce mode](../glossary.md#enforce-mode) without a backend, which the node itself never sets up. Report. |
| `node mode changed` | warn | A reload switched between [observe mode](../glossary.md#observe-mode) and enforce mode (`from`, `to`). |
| `observe mode: every applied block was withdrawn` | warn | After a switch to observe mode, the node removed its blocks from the firewall, as intended. |
| `nftables: table inet obie has an unexpected structure; replacing it` | warn | Someone changed OBIE's table by hand; the node puts it back. Keep your own rules in your own tables. |
| `nftables: table inet obie holds elements obied did not add; replacing it` | warn | As above, for entries added by hand; use `obiectl block` instead. |
| `nftables: the socket buffers cannot hold the whole change; applying it in several transactions` | warn | A very large change is applied in steps; nothing to do. |
| `too many events wait for a peer; dropping the oldest, which still count on this node` | warn | No peer has been connected for a long time and what this node reported piles up; see [No peers](troubleshooting.md#no-peers). |
| `sending the held events failed; they are tried again` | warn | Held events could not be sent yet; the node retries. |
| `event store full; evicting the verdicts that expire first` | warn | The node holds `store.max_indicators` addresses with [verdicts](../glossary.md#verdict); raise it if the host has room. |
| `the store keeps the most verdicts of other publishers that ended; newer ones are not kept until older ones are forgotten` | warn | The history of ended verdicts is full (`store.max_ended`); older entries make room over time. |
| `the console cannot show the audit log's history` | warn | The audit log could not be read back (`reason`); the console shows only what happened since the start. |
| `reopening the audit log failed; writing on to the previous file` | error | After a reload or log rotation the audit log could not be opened (`path`, `error`); check the directory and its permissions. |
| `closing the previous audit log failed` | warn | The old audit log did not close cleanly after a rotation. Report if it repeats. |
| `audit log is not open; record lost` | error | An audit record could not be written because the log is not open; fix the cause of the earlier audit log error. |
| `writing the audit log failed; record lost` | error | The audit log could not be written (`error`), e.g. a full disk; free space. |
| `encoding an audit record failed` | error | Report. |
| `reading overrides failed; using the previous ones` | error | The node's database could not be read; it keeps working with what it had. Report if it repeats. |
| `reading overrides failed; keeping the previous decisions and retrying` | error | As above. |
| `reading verdicts failed; keeping the previous decision and retrying` | error | As above. |
| `explaining a decision failed` | error | `obiectl explain` got an error from the node. Report. |
| `explaining the decision after an override change failed` | warn | The override was set, but its effect could not be shown; `obiectl explain` shows it. |
| `listing the enforced entries failed` | error | `obiectl enforced` got an error from the node. Report. |
| `listing overrides failed` | error | `obiectl overrides` got an error from the node. Report. |
| `setting an override failed` | error | `obiectl allow` or `block` got an error from the node. Report. |
| `deleting an override failed` | error | `obiectl unoverride` got an error from the node. Report. |
| `encoding response` | error | An answer to `obiectl` could not be written. Report. |
| `verdict request failed` | error | `obiectl report`, `revoke`, `indicators` or `show` failed inside the node (`action`, `error`); `obiectl` says `node-failed`. The `error` names the cause, such as a mesh that is not started yet; report it otherwise. |
| `duplicate check failed` | error | A received event could not be checked against the database. Report. |
| `storing received event failed` | error | A received event could not be stored. Report. |
| `expiry sweep failed` | error | Expired verdicts could not be removed this time; the node tries again. Report if it repeats. |
| `value log GC failed` | warn | The database could not reclaim space this time; it tries again. |
| `evicting undecodable verdict` | error | A damaged entry in the database was removed. Report. |
| `dropping expiry of undecodable entry` | error | As above. |
| `background loop did not stop in time; closing database anyway` | warn | The shutdown took long; nothing to do unless it repeats. |
| Other messages of the `store` component | error, warn | The database library's own messages, passed on as they are. Report errors that repeat. |

## Review

The messages on this page were reviewed against the rules above on
2026-09-29 (work package #1694):

- Every command-line error says what went wrong and at least one next
  step, and why wherever the tool can tell. The errors the work package
  names are covered: node not running (`node-not-running`), permission
  denied on the admin interface (`admin-permission-denied`), invalid
  address or network (`invalid-address`), address is protected
  (`address-protected`, `block-protected`), configuration invalid with
  file, line and setting (`config-invalid`), identity missing
  (`identity-missing`), and peer unreachable (`bootstrap peer unreachable`
  in the log, the `peers` check of `obied self-check`, and the note of
  `obiectl peers`).
- The log lines an operator is likely to meet name the next step in their
  `next` attribute: `obied failed`, `configuration reload rejected`,
  `configuration changes that need a restart were not applied`,
  `bootstrap peer unreachable`, `console not started`, `enforcement
  failed` and the three `admin socket group not found` lines. The others
  say what the node does about the problem, or point to a fault in OBIE.
- No message relies on colour, and none asks a question outside a
  terminal.
