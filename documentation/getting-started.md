# Get started with OBIE

This tutorial takes you from nothing to a working OBIE
[node](glossary.md#node) on your own server. At the end, the node turns
the bans of [Fail2Ban](glossary.md#fail2ban) into signed
[verdicts](glossary.md#verdict), shares them with one
[peer](glossary.md#peer), and shows you what it would block, in
[observe mode](glossary.md#observe-mode). If you want, it then blocks
attackers in your [firewall](glossary.md#firewall), once you have made
sure that it cannot lock you out.

Plan on about 30 minutes, plus the wait for Fail2Ban's next ban in step 7.
You never have to choose between two ways: follow the steps in order.

Each step says in one sentence what it is for. Then it shows the command
to run and what it prints on a successful run. Parts of the output are
different on your server: [peer IDs](glossary.md#peer-id), times,
durations, and the addresses that stand for yours. A `…` stands for
output that varies from server to server. If the result differs in any
other way, the step says what to do.

The same steps run automatically on every change to OBIE, and must print
what this page shows ([how](#how-this-page-is-tested)).

1. [Check the requirements](#1-check-the-requirements)
2. [Install OBIE](#2-install-obie)
3. [Set up the node](#3-set-up-the-node)
4. [Start the node in observe mode](#4-start-the-node-in-observe-mode)
5. [Let the node check itself](#5-let-the-node-check-itself)
6. [Connect Fail2Ban](#6-connect-fail2ban)
7. [See the first verdict](#7-see-the-first-verdict)
8. [Connect to a peer](#8-connect-to-a-peer)
9. [Review what would be blocked](#9-review-what-would-be-blocked)
10. [Switch to enforcement (optional)](#10-switch-to-enforcement-optional)

## 1. Check the requirements

Make sure that this server can run OBIE before you install anything.

You need:

- a Linux server with systemd, on a 64-bit x86 (amd64) or ARM (arm64)
  processor, where you can run commands with `sudo`;
- Fail2Ban 0.11 or newer with its `sshd` jail switched on, as the
  Fail2Ban packages of Debian and Ubuntu do. Without Fail2Ban, OBIE works
  too; step 6 says how;
- for step 10 only: a way into the server that does not depend on its
  network, such as your provider's web console.

Run these commands on the server, in an SSH session:

```sh
uname -sm
```

```text
Linux x86_64
```

`x86_64` means amd64. On ARM, it says `aarch64`: then write `arm64`
instead of `amd64` in the file names of step 2.

```sh
ps -p 1 -o comm=
```

```text
systemd
```

```sh
sudo fail2ban-client version
```

```text
1.0.2
```

Any version from 0.11 on works.

```sh
sudo fail2ban-client status
```

```text
Status
|- Number of jail:	1
`- Jail list:	sshd
```

Your server may list more jails; `sshd` must be one of them.

If `ps` names another program than `systemd`, OBIE runs, but this
tutorial does not fit your server yet
([what OBIE needs](capabilities.md#what-it-needs)). If `fail2ban-client`
is not found, go on anyway: step 6 shows the way without it. Anything
else: see
[before you install](operations/troubleshooting.md#the-installation-fails).

## 2. Install OBIE

Download the release archive, check that it arrived intact, and install
it.

This tutorial installs OBIE on the server itself, because only there can
it read Fail2Ban's bans and block. To run it in a container instead, see
[Install the container image instead](#install-the-container-image-instead).

Download the archive and its checksums:

```sh
curl -fsSLO https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange/releases/download/v0.1.0/obie-0.1.0-linux-amd64.tar.gz
curl -fsSLO https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange/releases/download/v0.1.0/SHA256SUMS
```

Both commands print nothing when they succeed. Check the archive:

```sh
sha256sum -c --ignore-missing SHA256SUMS
```

```text
obie-0.1.0-linux-amd64.tar.gz: OK
```

Unpack it and run the installer:

```sh
tar -xzf obie-0.1.0-linux-amd64.tar.gz
```

```sh
sudo ./obie-0.1.0-linux-amd64/install.sh
```

```text
install.sh: created group obie
install.sh: created user obie
install.sh: installed obied and obiectl into /usr/local/bin
install.sh: installed the manual pages and bash, zsh and fish completions below /usr/local/share
install.sh: installed /etc/obie/obie.yaml
install.sh: installed /etc/systemd/system/obied.service
install.sh: installed the Fail2Ban action /etc/fail2ban/action.d/obie.conf (override it in obie.local)
install.sh: done. Next steps:
  1. Answer a few questions to write /etc/obie/obie.yaml:
       /usr/local/bin/obied setup
     (or review the file yourself and check it:
       /usr/local/bin/obied --config /etc/obie/obie.yaml --check-config)
  2. Start the node:  systemctl enable --now obied
  3. Let it check itself; every problem comes with the next step:
       /usr/local/bin/obied self-check
  4. Use obiectl as root or as a member of the group obie:
     usermod -aG obie <user>; obiectl status
  Every command explains itself: obiectl --help, man obiectl
```

OBIE is now installed, and nothing runs yet. The installer created the
user `obie` that the node will run as. It put the two programs into
`/usr/local/bin`: `obied` is the node, `obiectl` is how you talk to it.
It also installed an example configuration, the service and, as Fail2Ban
is installed, OBIE's Fail2Ban action
([what it does in detail](operations/install.md#install-on-a-host-with-systemd)).

If `curl` fails with `404`, check the version and the processor in the
file name. If `sha256sum` does not say `OK`, download the archive again.
For every other problem, see
[the installation fails](operations/troubleshooting.md#the-installation-fails).

## 3. Set up the node

Answer five questions, and the setup assistant writes the node's
configuration for you.

Each question comes with an explanation and a safe suggestion in
brackets. Press Enter at every question to take the suggestion, and once
more at the end to write the file:

```sh
sudo obied setup
```

```text
OBIE setup

This assistant writes the configuration of this node to /etc/obie/obie.yaml.
It asks 5 questions. Each suggestion in [brackets] is a safe choice:
press Enter to take it. Nothing is written until you confirm at the end.

/etc/obie/obie.yaml exists already (the unchanged example that install.sh installed).
You will be asked before it is replaced; the old file is kept as a backup.

1/5  Where should the node keep its state?
…
State directory [/var/lib/obie]:

2/5  Where should the node write its audit log?
…
Audit log [/var/log/obie/audit.jsonl]:

3/5  Which peers should this node connect to?
…
Peer address (empty: done):

4/5  Should the node start in observe mode?
…
Start in observe mode? [Y/n]:

5/5  Which addresses must never be blocked?
…
      Your SSH session comes from 85.10.3.20, which is not protected yet.
Addresses or networks, separated by spaces, or none [85.10.3.20/32]:

Summary
  State directory: /var/lib/obie
  Audit log:       /var/log/obie/audit.jsonl
  Peers:           none: the node works on its own
  Mode:            observe
  Never blocked:   85.10.3.20/32

Replace /etc/obie/obie.yaml? The old file is kept as a backup. [Y/n]:
Wrote /etc/obie/obie.yaml.
The previous file is kept as /etc/obie/obie.yaml.bak.

Next steps:
  1. Start the node (if it runs already, restart it instead):
       sudo systemctl enable --now obied
       sudo systemctl restart obied
  2. Check the node and this server; every problem comes with what to do:
       sudo obied self-check
  3. Open the tutorial and go on with connecting Fail2Ban:
       https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange/blob/main/documentation/getting-started.md#6-connect-fail2ban
```

`85.10.3.20` stands for the address your SSH session comes from. With
these answers, the node keeps its state in `/var/lib/obie` and records
every decision in `/var/log/obie/audit.jsonl`. It has no peers yet;
step 8 adds one. It starts in observe mode: it
decides and shows what it would block, but blocks nothing. It never blocks
the address of your SSH session, which is now on its
[allow-list](glossary.md#allow-list).

If the fifth question names no SSH session, you are not logged in over
SSH, for example at a console. Then type the addresses you administer the
server from. If the assistant stops with a message, do what its `Next:`
line says ([every message](operations/messages.md)); the
[setup guide](operations/setup.md#set-up-the-node) explains each question.
See also [Troubleshooting](operations/troubleshooting.md).

## 4. Start the node in observe mode

Start the node as a service, so that it runs now and after every reboot.

```sh
sudo systemctl enable --now obied
```

```text
Created symlink /etc/systemd/system/multi-user.target.wants/obied.service → /etc/systemd/system/obied.service.
```

Ask the node how it is:

```sh
sudo obiectl status
```

```text
Mode:     OBSERVE (decisions are logged, nothing is blocked)
Version:  0.1.0
Uptime:   3s
Ready:    yes

SUBSYSTEM  STATE    READY  ERROR  DETAIL
admin      running  yes    -      -
audit      running  yes    -      -
console    running  yes    -      disabled
decision   running  yes    -      0 blocked of 0 indicators
enforce    running  yes    -      observing
mesh       running  yes    -      degraded: 0 peers connected (0/0 bootstrap peers)
ops        running  yes    -      -
store      running  yes    -      -
```

The node runs in observe mode and is ready. `mesh` is `degraded` because
the node has no peers yet, which is fine for now. On its first start, the
node created its identity: a key in `/var/lib/obie/node.key`. Back it up
later ([how](operations/operations.md#back-up-the-node-key)).

If `obiectl status` says that `obied` is not running, see
[obied does not start](operations/troubleshooting.md#obied-does-not-start).

## 5. Let the node check itself

Run the self-check, which looks at the node and this server and says what
to do about anything that is not right.

```sh
sudo obied self-check
```

```text
OBIE self-check of /etc/obie/obie.yaml (obied 0.1.0, as root)

OK       Configuration  /etc/obie/obie.yaml is valid; the node runs in observe mode
OK       Identity       /var/lib/obie/node.key exists and only obie can read it; the node's peer ID is 12D3KooWPqtsL3NjMswRrqG8xYAsfMjPgfajfvg9625cc6Y9WmiD
OK       Admin access   the admin socket /run/obie/obie.sock is open to root, obie and the group obie only (mode 0660)
                        - alice is not in the group obie: use sudo obiectl, or join the group with sudo usermod -aG obie alice and log in again
OK       Node           obied 0.1.0 is running and ready in observe mode, up 3s
WARNING  Peers          stand-alone node: no peers are configured, so the node acts only on what this server detects
                        Next: if that is what you want, there is nothing to do; to exchange verdicts with other nodes, run sudo obied setup again or see documentation/operations/federation.md
OK       Clock          the clock is synchronized (estimated error 12ms)
WARNING  Fail2Ban       no Fail2Ban jail uses OBIE's action yet, so no ban is reported
                        - OBIE's action is installed: /etc/fail2ban/action.d/obie.conf
                        Next: add obie to the action of a jail in /etc/fail2ban/jail.local and restart Fail2Ban (getting started, step 6: documentation/getting-started.md#6-connect-fail2ban)
OK       Firewall       not needed yet: in observe mode the node blocks nothing
OK       SSH session    your SSH session comes from 85.10.3.20, which is protected (allow-listed: allowlist.cidrs entry 85.10.3.20/32)

Result: 0 problems, 2 warnings, 7 OK. No problems; read the warnings.
```

Every line starts with `OK`, `WARNING` or `PROBLEM`. There must be no
`PROBLEM`. The two warnings are expected at this point: step 6 connects
Fail2Ban, and step 8 a peer. `alice` stands for your user name. Keep using
`sudo obiectl`, or join the group `obie` as the line says.

If a check says `PROBLEM`, do what its `Next:` line says and run the
self-check again. The [setup guide](operations/setup.md#check-the-node)
explains every check, and [Troubleshooting](operations/troubleshooting.md)
starts here too.

## 6. Connect Fail2Ban

Make Fail2Ban report every ban of its `sshd` jail to the node, which turns
each ban into a signed verdict.

**No Fail2Ban?** Then see
[If this server has no Fail2Ban](#if-this-server-has-no-fail2ban), and
come back for step 8.

The installer put OBIE's action into `/etc/fail2ban/action.d/obie.conf`.
This command adds it to the `sshd` jail, next to the jail's own ban
action, in a file of its own:

```sh
sudo tee /etc/fail2ban/jail.d/obie.local <<'EOF'
[sshd]
action = %(action_)s
         obie
EOF
```

```text
[sshd]
action = %(action_)s
         obie
```

If your `sshd` jail already sets an `action` of its own in
`/etc/fail2ban/jail.local`, this file replaces it. Then add `obie` to that
action instead, as the [Fail2Ban guide](guides/fail2ban.md#install)
shows.

Check Fail2Ban's configuration:

```sh
sudo fail2ban-client -t
```

```text
…
OK: configuration test is successful
```

Some Fail2Ban versions print warnings before the last line, such as one
about `allowipv6`; they do not matter. Restart Fail2Ban, since a reload
does not add an action to a running jail:

```sh
sudo systemctl restart fail2ban
```

Now test the way from Fail2Ban to the node without reporting anything.
Ban an address that is reserved for examples, look for the message of
OBIE's action, and lift the ban:

```sh
sudo fail2ban-client set sshd banip 203.0.113.7
```

```text
1
```

```sh
sudo journalctl -t obie-fail2ban -n 1 -o cat
```

```text
could not report 203.0.113.7 of jail sshd to OBIE (obiectl exit code 1): obiectl report: nothing was reported: ipv4:203.0.113.7 is not a public address: 203.0.113.7/32 overlaps special-purpose range 203.0.113.0/24 Why: OBIE never reports private, loopback, link-local and other special-purpose addresses, nor the networks on your allow-list, so that no node blocks them because of you Next: this address is reserved for examples, like those in the help; report the attacking address from your log instead
```

```sh
sudo fail2ban-client set sshd unbanip 203.0.113.7
```

```text
1
```

The message is what you want to see: the ban reached the node, and the
node refused to report an example address. Every real ban of the jail now
becomes a verdict. Fail2Ban's matched log lines never leave the server;
only a hash and the number of failures do.

If the message says `obied is not running` or `obied did not answer in
time`, or there is no message, see
[Fail2Ban reports do not arrive](operations/troubleshooting.md#fail2ban-reports-do-not-arrive).

## 7. See the first verdict

Wait for Fail2Ban's next ban and see it become the node's first verdict.

On a server whose SSH port is open to the internet, Fail2Ban usually bans
someone within minutes. Look at the jail:

```sh
sudo fail2ban-client status sshd
```

```text
Status for the jail: sshd
|- Filter
|  |- Currently failed:	0
|  |- Total failed:	5
|  `- Journal matches:	_SYSTEMD_UNIT=sshd.service + _COMM=sshd
`- Actions
   |- Currently banned:	1
   |- Total banned:	2
   `- Banned IP list:	85.10.0.7
```

`Total banned` counts the test ban of step 6 too. Once `Currently banned`
is at least 1, list the node's own verdicts:

```sh
sudo obiectl indicators --mine
```

```text
INDICATOR       PUBLISHER    ACTION  CONFIDENCE  EVENTS  PROTOCOL  REASON      EXPIRES
ipv4:85.10.0.7  (this node)  ban     0.8         5       ssh       bruteforce  2026-10-14T09:22:05Z
```

`85.10.0.7` stands for the address your Fail2Ban banned. The node signed a
verdict that this address attacked it over SSH, with 5 failed logins. The
verdict ends when Fail2Ban's ban ends, after 10 minutes by default.

If nobody is banned after 15 minutes, your SSH port may not be reachable
from the internet, or nobody tried yet. That is no error: go on with
step 8, and come back later. If Fail2Ban bans, but no verdict appears,
see
[Fail2Ban reports do not arrive](operations/troubleshooting.md#fail2ban-reports-do-not-arrive).

## 8. Connect to a peer

Connect the node to one other node, so that the two exchange their
verdicts.

**No peer yet?** Skip this step: the node works on its own and acts on
what this server detects. Come back when someone gives you the address of
their node; step 9 works either way.

A peer is a node run by someone you know, such as a friend or a second
server of yours. Tell its operator how to reach your node: your server's
public address and your node's peer ID. This shows the peer ID:

```sh
sudo obiectl identity
```

```text
Peer ID:      12D3KooWPqtsL3NjMswRrqG8xYAsfMjPgfajfvg9625cc6Y9WmiD
Fingerprint:  SHA256:wIufDNocPY1kRab1DT/AV/aVBV49J50jXJFOMwVUY2w
```

Your node's address for the peer is then
`/ip4/<your server's public address>/tcp/4001/p2p/<your peer ID>`. In
return, the operator gives you their node's address, for example
`/ip4/198.51.100.20/tcp/4001/p2p/12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf`.
Exchange them over a channel where you know who you are talking to. Open
port 4001, TCP and UDP, for the peer in your firewall
([how](operations/federation.md#open-the-mesh-port)).

Run the setup assistant again. Press Enter at every question, except
three: paste the peer's address at `Peer address`, name the peer
`friend`, and answer `y` to replace the file. Coming back to this step
after step 10? Then also answer `n` at question 4 and `y` when the
assistant asks whether to start in
[enforce mode](glossary.md#enforce-mode) anyway, or the node goes back to
observe mode.

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

The node now connects to `friend` and gives its verdicts a
[trust weight](glossary.md#trust-weight) of 0.8. A peer's verdicts count
towards a block only together with others: by default, at least two
[publishers](glossary.md#publisher) must agree (the
[quorum](glossary.md#quorum)), with enough
weight to reach the [threshold](glossary.md#threshold). So one peer alone
never gets an address blocked on your server. The node reads its peers
only when it starts, so restart it:

```sh
sudo systemctl restart obied
```

```sh
sudo obiectl peers
```

```text
PEER ID                                               NAME    TRUST  BOOTSTRAP  CONNECTED SINCE       LATENCY  ADDRESSES
12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf  friend  0.8    yes        2026-10-14T09:14:03Z  1ms      /ip4/198.51.100.20/tcp/4001
```

From now on, the two nodes send each other every verdict. To add more
peers or choose other weights, see [Federation](operations/federation.md).

If the peer does not appear within a minute, see
[No peers](operations/troubleshooting.md#no-peers).

## 9. Review what would be blocked

See what the node would block and why, before you let it block anything.

```sh
sudo obiectl decisions --state block
```

```text
Decisions: 1 (1 block, 0 allowed, 0 none)

INDICATOR       STATE  SCORE  PUBLISHERS  EXPIRES               REASON
ipv4:85.10.0.7  block  0.8    1           2026-10-14T09:22:05Z  local autoblock: this node's own ban verdict (score 0.8 < threshold 1.8, 1 < quorum 2)
```

The node would block the address Fail2Ban banned. Ask it why:

```sh
sudo obiectl explain 85.10.0.7
```

```text
Indicator:             ipv4:85.10.0.7
Decision:              block until 2026-10-14T09:22:05Z
Reason:                local autoblock: this node's own ban verdict (score 0.8 < threshold 1.8, 1 < quorum 2)
Score:                 0.8 (threshold 1.8)
Publishers:            1 (quorum 2)
Local autoblock:       yes
Allow-list/overrides:  none apply
Evaluated:             2026-10-14T09:15:40Z

PUBLISHER    PEER ID                                               ACTION  WEIGHT  CONFIDENCE  SCORE  COUNTS  PROTOCOL  REASON      ISSUED                EXPIRES
(this node)  12D3KooWPqtsL3NjMswRrqG8xYAsfMjPgfajfvg9625cc6Y9WmiD  ban     1       0.8         0.8    yes     ssh       bruteforce  2026-10-14T09:12:05Z  2026-10-14T09:22:05Z
```

Your own node's verdicts block at once, without waiting for others: the
node trusts this server's Fail2Ban as much as you do
([local autoblock](glossary.md#local-autoblock)). Verdicts of your peer
show up here too, but block only when enough publishers agree. Nothing is
blocked yet:

```sh
sudo obiectl enforced
```

```text
No entries applied: the node is in observe mode.
```

Let the node observe for a few days, and review what it would block from
time to time. These lists show what is active right now: a verdict from
Fail2Ban lasts as long as its ban, so later they show other addresses, or
none. The audit log `/var/log/obie/audit.jsonl` keeps every decision.
Once you switch it on, the [web console](operations/console.md) shows the
same in a browser. If a decision looks wrong,
[Nothing is enforced](operations/troubleshooting.md#nothing-is-enforced)
explains every reason.

If there is no decision, no ban is active right now (step 7).

## 10. Switch to enforcement (optional)

Let the node block what it decides, after you have made sure that it
cannot lock you out and that you know the way back.

In [enforce mode](glossary.md#enforce-mode), the node blocks through its
own table in the Linux firewall, [nftables](glossary.md#nftables), and
never touches any other rule. Switch it on only once the review of step 9
looks right.

### Protect your own access

Every check should now be `OK`, except a warning for the peers if you
skipped step 8, and one for Fail2Ban if this server has none. Above all,
the `SSH session` line must say that your session is protected:

```sh
sudo obied self-check
```

```text
OBIE self-check of /etc/obie/obie.yaml (obied 0.1.0, as root)

OK       Configuration  /etc/obie/obie.yaml is valid; the node runs in observe mode
OK       Identity       /var/lib/obie/node.key exists and only obie can read it; the node's peer ID is 12D3KooWPqtsL3NjMswRrqG8xYAsfMjPgfajfvg9625cc6Y9WmiD
OK       Admin access   the admin socket /run/obie/obie.sock is open to root, obie and the group obie only (mode 0660)
                        - alice is not in the group obie: use sudo obiectl, or join the group with sudo usermod -aG obie alice and log in again
OK       Node           obied 0.1.0 is running and ready in observe mode, up 1m12s
OK       Peers          1 of 1 peer in mesh.bootstrap connected; 1 connected in all
                        - friend is connected
OK       Clock          the clock is synchronized (estimated error 12ms)
OK       Fail2Ban       jails that report every ban to the node: sshd
                        - the node holds verdicts of its own, so bans reach it
                        - OBIE's action is installed: /etc/fail2ban/action.d/obie.conf
OK       Firewall       not needed yet: in observe mode the node blocks nothing
OK       SSH session    your SSH session comes from 85.10.3.20, which is protected (allow-listed: allowlist.cidrs entry 85.10.3.20/32)

Result: 0 problems, 0 warnings, 9 OK. Everything checked is fine.
```

The node itself confirms that it never blocks your address:

```sh
sudo obiectl explain 85.10.3.20
```

```text
Indicator:             ipv4:85.10.3.20
Decision:              allowed
Reason:                allow-listed: allowlist.cidrs entry 85.10.3.20/32; verdicts: no active verdicts
Score:                 0 (threshold 1.8)
Publishers:            0 (quorum 2)
Local autoblock:       no
Allow-list/overrides:  allow-listed: allowlist.cidrs entry 85.10.3.20/32
Evaluated:             2026-10-14T09:16:10Z

No active verdicts.
```

Protect every other address you cannot afford to lose in the same way:
the networks you administer from, your monitoring, your DNS resolvers.
An [override](glossary.md#override) that always allows them works at once:

```sh
sudo obiectl allow 192.0.2.0/24 --note "office"
```

```text
Override set: force_allow on cidr:192.0.2.0/24, until removed (note: "office").
Decision now: allowed — operator force-allow override on cidr:192.0.2.0/24 (note: "office"); verdicts: no active verdicts
```

`192.0.2.0/24` stands for your office network. Do not go on until
`sudo obiectl explain` says `Decision: allowed` for each of them.

### Know the way back

There are two ways back, and both work at any time. To stop blocking and
keep the node running, set observe mode again and restart the node, which
lifts every block:

```sh
sudo sed -i 's/^  mode: enforce$/  mode: observe/' /etc/obie/obie.yaml
sudo systemctl restart obied
```

If you ever lock yourself out, log in through your provider's console,
stop the node and remove its firewall table:

```sh
sudo systemctl stop obied
```

```sh
sudo obied teardown-firewall
```

```text
obied teardown-firewall: table inet obie removed; nothing is blocked by OBIE anymore
```

Try them now, while the node blocks nothing: they change nothing yet, and
you have run them once before you need them. Your own firewall rules are
never touched. Then start the node again:

```sh
sudo systemctl start obied
```

### Switch enforcement on

Set enforce mode, and let the node block through nftables. Run the second
command only once: the section it adds stays when you go back to observe
mode.

```sh
sudo sed -i 's/^  mode: observe$/  mode: enforce/' /etc/obie/obie.yaml
```

```sh
printf '\nenforce:\n  backend: nftables\n' | sudo tee -a /etc/obie/obie.yaml
```

```text

enforce:
  backend: nftables
```

Check the file, and start the node with it:

```sh
sudo obied --config /etc/obie/obie.yaml --check-config
```

```text
obied: configuration /etc/obie/obie.yaml is valid
```

```sh
sudo systemctl restart obied
```

```sh
sudo obiectl status
```

```text
Mode:     ENFORCE (blocks are sent to the enforcer)
…
```

```sh
sudo obiectl enforced
```

```text
Entries applied: 1

PREFIX        EXPIRES               REMAINING
85.10.0.7/32  2026-10-14T09:22:05Z  5m38s
```

The node now blocks what it decided: here, the address Fail2Ban banned.
The list shows the blocks active right now, so yours may name other
addresses, or none. The Linux firewall shows them in OBIE's own table,
`inet obie`:

```sh
sudo nft list table inet obie
```

```text
table inet obie {
	set obie_v4 {
		type ipv4_addr
		flags interval,timeout
		elements = { 85.10.0.7 timeout 5m41s173ms expires 5m38s98ms comment "85.10.0.7/32" }
	}

	set obie_v6 {
		type ipv6_addr
		flags interval,timeout
	}

	chain input {
		type filter hook input priority filter - 10; policy accept;
		ip saddr @obie_v4 counter packets 0 bytes 0 drop
		ip6 saddr @obie_v6 counter packets 0 bytes 0 drop
	}
}
```

If `obiectl status` still says `OBSERVE`, or an address that
`sudo obiectl decisions --state block` lists is missing here, see
[Nothing is enforced](operations/troubleshooting.md#nothing-is-enforced).
If you lose access, see [Locked out](operations/troubleshooting.md#locked-out).

## If this server has no Fail2Ban

Without Fail2Ban, the node still acts on its peers' verdicts, and you can
report attackers yourself.

Report an address that attacked your server, for example one you found in
your logs. `85.10.0.9` stands for it:

```sh
sudo obiectl report --protocol ssh --reason password_bruteforce 85.10.0.9
```

```text
Reported ipv4:85.10.0.9: verdict 01a0ef55-20f8-7627-97e6-0a7cd4dd1560 issued and published.

Indicator:   ipv4:85.10.0.9
Action:      ban
Confidence:  0.8
Protocol:    ssh
Reason:      password_bruteforce
Events:      1
Log hash:    -
Issued:      2026-10-14T09:18:08Z
Expires:     2026-10-21T09:18:08Z (7d)
Publisher:   12D3KooWPqtsL3NjMswRrqG8xYAsfMjPgfajfvg9625cc6Y9WmiD
```

The verdict lasts 7 days. To report bans automatically, install Fail2Ban.
On Debian and Ubuntu:

```sh
sudo apt install fail2ban
```

Other distributions have it too, under the same name. Then run OBIE's
installer again, which adds the Fail2Ban action, and go on with step 6:

```sh
sudo ./obie-0.1.0-linux-amd64/install.sh
```

```text
…
install.sh: installed the Fail2Ban action /etc/fail2ban/action.d/obie.conf (override it in obie.local)
…
```

If a command stops with a message, its `Next:` line says what to do;
[Messages of obied and obiectl](operations/messages.md) explains every
message, and [Troubleshooting](operations/troubleshooting.md) the rest.

## Install the container image instead

The container image runs a node that exchanges verdicts, but cannot read
the server's Fail2Ban and cannot block: it runs in observe mode and only
lists what it would block. Use it to try OBIE, or to run a node next to
other containers. You need Docker.

```sh
docker run -d --name obie --restart unless-stopped -v obie-state:/var/lib/obie -p 4001:4001 -p 4001:4001/udp ghcr.io/mncloudwerkstechnology/obie:0.1.0
```

```text
…
0f0e1d7c4c9e2f3b8a6d5e4c3b2a19087f6e5d4c3b2a1908f7e6d5c4b3a29180
```

Docker downloads the image first. The last line is the container's ID.
Ask the node how it is:

```sh
docker exec obie obiectl status
```

```text
Mode:     OBSERVE (decisions are logged, nothing is blocked)
…
```

In the container, run `docker exec obie obiectl` wherever this tutorial
runs `sudo obiectl`. Steps 3 to 7 and 10 do not apply. The node's
identity is in the volume `obie-state`:

```sh
docker exec obie obiectl identity
```

```text
Peer ID:      12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf
Fingerprint:  SHA256:iJQCoAB0CUvEzbf/YZDCIElsOo7GuqUNHru08MWXzVY
```

To connect to a peer, write a configuration file and mount it over
`/etc/obie/obie.yaml`
([how](operations/install.md#run-the-container-image)). To remove the
node and its identity:

```sh
docker rm -f obie
docker volume rm obie-state
```

If a command fails, see
[Troubleshooting](operations/troubleshooting.md).

## How this page is tested

`make tutorial-check` runs every command on this page, in order, and
compares its output with the page. It runs on every change to OBIE,
against the release built from that change. The server is a container
with Ubuntu 24.04, systemd, Fail2Ban 1.0.2, OpenSSH and nftables, on
amd64. Its firewall rules stay inside the container.

What a reader brings along is played by the check:

- The SSH session comes from `85.10.3.20`.
- The release is downloaded from a copy inside the container.
- The attacker `85.10.0.7` fails five SSH logins, and Fail2Ban bans it,
  for an hour instead of ten minutes, so that the ban outlasts the check.
- The peer `friend` is a second node at `198.51.100.20`.
- The container image is built from the same change.

At the end, the check also takes both ways back of step 10 while the node
blocks, and makes sure that no block is left.

Before it compares, the check replaces what differs from run to run:
peer IDs, event IDs, fingerprints, container IDs, times and durations.
It ignores column widths, and `…` stands for any output. Other Linux
distributions, ARM processors and a real attack are not tested. On your
server, lines that name versions, jails or times may differ.

## What next

- **How-to guides:** the [Fail2Ban guide](guides/fail2ban.md) reports
  more jails and tunes them; the [nftables guide](guides/nftables.md)
  explains how blocking works and how to block traffic that passes
  through the server.
- **The web console:** look into the node in a browser: its health, its
  peers, every decision and why
  ([Web console](operations/console.md)).
- **Federation:** connect more peers, and choose trust weights and a
  quorum ([Federation](operations/federation.md)).
- **Operations:** upgrades, backups of the node key, the audit log,
  monitoring and uninstalling
  ([Operations](operations/operations.md),
  [Monitoring](operations/monitoring.md)).
- **When something is wrong:** [Troubleshooting](operations/troubleshooting.md),
  and every message explained in
  [Messages of obied and obiectl](operations/messages.md).
- **What OBIE protects against, and what not:** the
  [threat model](../SECURITY.md#threat-model).
