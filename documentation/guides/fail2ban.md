# Publishing Fail2Ban bans to OBIE

If you already run Fail2Ban, one extra line in a jail makes every ban of that
jail a signed verdict of your OBIE node. The action
[`contrib/fail2ban/action.d/obie.conf`](../../contrib/fail2ban/action.d/obie.conf)
calls `obiectl report` for each ban:

```sh
obiectl --socket /run/obie/obie.sock --timeout 5s report --ip <ip> \
    --protocol <protocol> --reason bruteforce --events <failures> \
    --mitre T1110 --ttl <bantime>s --evidence-from-stdin
```

- **Only hashes leave the host.** Fail2Ban's matched log lines are piped to
  `obiectl` on stdin and handed to `obied` over the local admin socket. `obied`
  hashes them (`evidence.log_hash`) and drops them; the published verdict
  carries the hash and the failure count, never the lines, usernames or
  hostnames in them.
- **The ban time is the verdict's lifetime.** It is rounded down to whole
  seconds and raised to at least 60 s, obied's minimum. A permanent ban
  (`bantime = -1`) uses `decision.default_ttl`. `obied` caps every TTL at
  `decision.max_ttl`.
- **Fail2Ban is never blocked.** `obiectl` gives up after 5 s. If `obied` is
  down, hung or refuses the report (for example for an address on its
  allow-list), the action logs the error to syslog with the tag
  `obie-fail2ban` and still succeeds. The ban itself is not affected.
- **A manual ban counts as one event.** `fail2ban-client set <jail> banip`
  has no failures and no matched lines, so it reports one event without
  evidence.
- **Unbanning does nothing by default.** The verdict expires with its TTL.
  With `revoke_on_unban = true` the action revokes the verdict when Fail2Ban
  lifts the ban.
- **Restarts do not re-report.** Bans that Fail2Ban restores after a restart
  were already reported (`norestored`).
- **Repeated bans refresh the verdict.** If an address is banned again while
  its verdict is active, `obied` refreshes that verdict and adds up the
  failure counts. It never publishes a second verdict (see
  [ADR 0012](../adr/0012-local-verdict-reporting.md)).

## Requirements

- Fail2Ban 0.10 or newer. Its actions run as root, which may use the admin
  socket.
- `obied` running on the same host, and `obiectl` on Fail2Ban's `PATH`
  (usually `/usr/local/bin` or `/usr/bin`). Otherwise set the action's
  `obiectl` parameter to the absolute path.
- `logger` (util-linux or bsdutils) for the syslog messages. Without it, the
  messages go to stderr, which Fail2Ban writes to its own log.

## Install

1. Copy the action into Fail2Ban's configuration:

   ```sh
   sudo install -m 0644 contrib/fail2ban/action.d/obie.conf /etc/fail2ban/action.d/obie.conf
   ```

2. Add `obie` to the actions of every jail that should report, in
   `/etc/fail2ban/jail.local` or a file in `/etc/fail2ban/jail.d/`. Keep the
   jail's existing ban action, which is `%(action_)s` by default. The indented
   second line is the one line OBIE needs:

   ```ini
   [sshd]
   enabled = true
   action  = %(action_)s
             obie
   ```

3. Check the configuration and reload:

   ```sh
   sudo fail2ban-client -t
   sudo fail2ban-client reload
   ```

If `admin.socket` in `obie.yaml` is not the default `/run/obie/obie.sock`, pass
it to the action: `obie[socket=/path/to/obie.sock]`.

## Jail parameters

Parameters are passed in brackets after the action name, for example
`obie[reason=password_bruteforce, confidence=0.9]`. All of them are optional.

| Parameter         | Default          | Meaning |
|-------------------|------------------|---------|
| `protocol`        | from jail name   | Attacked service, e.g. `ssh`, `http`, `smtp`. If empty, it is derived from the jail name: `*ssh*`/`dropbear` → `ssh`; `postfix*`/`exim*`/`sendmail*`/`*smtp*` → `smtp`; `dovecot*`/`courier*`/`*imap*` → `imap`; `*ftp*` → `ftp`; `nginx*`/`apache*`/`lighttpd*`/`*http*` → `http`; otherwise the jail name in lower case, with every character outside `a-z0-9_-` replaced by `_` and cut to 32 characters. |
| `reason`          | `bruteforce`     | Behavior class of the attack, `[a-z0-9_]+`, e.g. `password_bruteforce` or `web_scan`. |
| `confidence`      | `0.8` (obiectl)  | Your confidence in the verdict, in `[0, 1]`. Peers weight it with the trust they give your node. |
| `mitre`           | `T1110`          | Comma-separated MITRE ATT&CK technique IDs. `T1110` (Brute Force) fits authentication jails. For other jails, set it to something else, or to empty (`mitre=`) to send none. |
| `revoke_on_unban` | `false`          | `true`: revoke the verdict when Fail2Ban unbans the address. |
| `revoke_reason`   | `unbanned`       | Reason given for those revocations. |
| `obiectl`         | `obiectl`        | The `obiectl` binary, as a name on `PATH` or an absolute path. |
| `socket`          | `/run/obie/obie.sock` | `obied`'s admin socket (`admin.socket` in `obie.yaml`). |
| `obiectl_timeout` | `5s`             | How long `obiectl` waits for `obied`. |
| `syslog_priority` | `daemon.warning` | Syslog priority of failure messages. |

The jail's own `protocol` option (`tcp`/`udp`, used by the firewall actions)
does not reach this action. Only the brackets set `protocol` here.

## Example jails

These examples use the filters and log paths that ship with Fail2Ban. Put them
in `/etc/fail2ban/jail.local`.

```ini
[sshd]
enabled = true
action  = %(action_)s
          obie

[nginx-http-auth]
enabled = true
action  = %(action_)s
          obie[reason=password_bruteforce, confidence=0.9]

[postfix-sasl]
enabled = true
action  = %(action_)s
          obie[protocol=smtp, reason=password_bruteforce, revoke_on_unban=true]
```

- `sshd` reports with protocol `ssh` (from the jail name), reason
  `bruteforce` and MITRE `T1110`.
- `nginx-http-auth` reports failed HTTP basic authentication as `http`,
  reason `password_bruteforce`, with a higher confidence.
- `postfix-sasl` reports SASL login failures as `smtp` and revokes the
  verdict when Fail2Ban unbans the address.

For a jail that does not detect authentication failures, override `mitre`,
e.g. `obie[reason=web_scan, mitre=T1595]` for `nginx-botsearch`.

## Verify

1. Check that Fail2Ban loaded the action:

   ```sh
   sudo fail2ban-client -d | grep "'obie'"
   ```

2. Test the path to `obied` without publishing anything by banning an address
   from a documentation range. `obied` refuses non-public addresses, so the
   action logs the refusal and publishes no verdict:

   ```sh
   sudo fail2ban-client set sshd banip 203.0.113.7
   journalctl -t obie-fail2ban -n 5     # or: grep obie-fail2ban /var/log/syslog
   sudo fail2ban-client set sshd unbanip 203.0.113.7
   ```

   The message `could not report 203.0.113.7 of jail sshd to OBIE (obiectl exit
   code 1): ...` should name the refusal. If it says `obied is not running`
   or `context deadline exceeded`, `obied` is down or not answering, or the
   `socket` parameter is wrong.

3. After the next real ban, list this node's active verdicts:

   ```sh
   sudo obiectl indicators --mine
   sudo obiectl show --json <banned ip> # details, including evidence.log_hash
   ```

   Each ban appears as a verdict with the jail's protocol, reason and failure
   count, expiring at the end of the ban. A successful report logs nothing.
   `journalctl -t obie-fail2ban` lists only the failures.

## Remove

1. Delete the `obie` lines from the jails' `action` options, then remove the
   action and reload Fail2Ban:

   ```sh
   sudo rm /etc/fail2ban/action.d/obie.conf
   sudo fail2ban-client reload
   ```

2. Verdicts already published stay active until their TTL runs out. To
   withdraw them earlier, revoke them:

   ```sh
   sudo obiectl indicators --mine
   sudo obiectl revoke --reason operator_withdrawn <ip>
   ```
