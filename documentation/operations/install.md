# Installing and upgrading OBIE

OBIE ships in three forms (ADR 0017):

| Form | For | Enforces with nftables |
|------|-----|------------------------|
| Release tarball + `install.sh` + systemd unit | production hosts | yes |
| Container image | observe/dryrun nodes, Kubernetes, trying it out | no (dryrun) |
| Compose lab (`packaging/compose`) | three nodes on a laptop in minutes | no (dryrun) |

New to OBIE? The [quick start](quickstart.md) walks through a first node
step by step; [Operations](operations.md) covers upgrades, backups and
uninstalling in more detail.

## Release artefacts

Every `v*` tag publishes, for linux/amd64 and linux/arm64:

- `obie-<version>-linux-<arch>.tar.gz` — static `bin/obied` and
  `bin/obiectl`, `install.sh`, `etc/obie.yaml` (the example
  configuration), `systemd/obied.service`, `fail2ban/action.d/obie.conf`,
  `LICENSE.md`, `README.md`;
- `obie-<version>-linux-<arch>.obied.cdx.json` and `….obiectl.cdx.json` —
  CycloneDX SBOMs of the two binaries;
- `SHA256SUMS` over all of them;

and the image `ghcr.io/mncloudwerkstechnology/obie:<version>` (the Gitea
instance's registry for the Gitea release).

The build is reproducible: checking out the tag and running `make release
VERSION=<version>` with the Go version from `go.mod` produces
byte-identical files, which you can compare with the published
`SHA256SUMS`. It needs Linux with GNU tar.

## Install on a host with systemd

```sh
sha256sum -c --ignore-missing SHA256SUMS
tar -xzf obie-0.1.0-linux-amd64.tar.gz
cd obie-0.1.0-linux-amd64
sudo ./install.sh
```

`install.sh` (no `curl | sh`: it only uses the files next to it):

1. creates the system group and user `obie` (home `/var/lib/obie`, no
   login shell);
2. installs `obied` and `obiectl` into `/usr/local/bin` (`PREFIX=/opt/obie
   ./install.sh` for another prefix; the unit is adjusted to match);
3. installs `/etc/obie/obie.yaml` (mode 0640, group `obie`) **only if there
   is none**, and always the current example as
   `/etc/obie/obie.yaml.example`;
4. installs `/etc/systemd/system/obied.service` and runs `systemctl
   daemon-reload`;
5. installs the Fail2Ban action as `/etc/fail2ban/action.d/obie.conf` if
   Fail2Ban is installed (see the [Fail2Ban guide](../guides/fail2ban.md)).

It never starts obied. Review the configuration, then:

```sh
sudo obied --config /etc/obie/obie.yaml --check-config
sudo systemctl enable --now obied
sudo usermod -aG obie "$USER"      # obiectl for your user (log in again)
obiectl status
```

`systemctl reload obied` sends SIGHUP (re-reads the configuration, reopens
the audit log). Logs go to the journal (`journalctl -u obied`); for the
decision audit log set `audit.path: /var/log/obie/audit.jsonl` — the unit
creates `/var/log/obie` for it.

### The service sandbox

`obied.service` runs obied as `obie` with `CAP_NET_ADMIN` as its only
capability (needed by the nftables backend) and these directories:

| Directory | Setting | Contents |
|-----------|---------|----------|
| `/etc/obie` | `ConfigurationDirectory=obie` | `obie.yaml`, allow-list files (read-only for obied) |
| `/var/lib/obie` | `StateDirectory=obie` (0700) | `FORMAT`, `node.key`, `db/` |
| `/run/obie` | `RuntimeDirectory=obie` (0750) | admin socket `obie.sock` |
| `/var/log/obie` | `LogsDirectory=obie` (0750) | audit log, if configured |

Everything else is read-only or hidden (`ProtectSystem=strict`,
`ProtectHome`, `PrivateTmp`, `PrivateDevices`, `ProtectKernel*`,
`ProtectClock`, …), sockets are limited to `AF_UNIX AF_INET AF_INET6
AF_NETLINK`, system calls to `@system-service` minus `@privileged`,
`@mount`, `@debug`, `@obsolete` and `@cpu-emulation`, with
`NoNewPrivileges`, `RestrictNamespaces`, `MemoryDenyWriteExecute` and
`LockPersonality`.

`systemd-analyze security obied.service` (systemd 255) rates it **1.7 OK**
(the release requires ≤ 3.0; `make check-unit` enforces it). The remaining
findings are inherent to what obied does:

| Finding | Exposure | Why it stays |
|---------|----------|--------------|
| `PrivateNetwork=` | 0.5 | the mesh needs the host's network |
| `RestrictAddressFamilies=~AF_(INET\|INET6)` | 0.3 | mesh and metrics |
| `CapabilityBoundingSet=~CAP_NET_ADMIN` | 0.2 | nftables backend |
| `PrivateUsers=` | 0.2 | would drop `CAP_NET_ADMIN` in the host's network namespace |
| `IPAddressDeny=` | 0.2 | mesh peers are arbitrary addresses |
| `SystemCallFilter=~@resources` | 0.2 | the Go runtime raises `RLIMIT_NOFILE` at start |
| `AmbientCapabilities=` | 0.1 | how the unprivileged user gets `CAP_NET_ADMIN` |
| `RestrictAddressFamilies=~AF_NETLINK` | 0.1 | nftables and interface addresses |
| `RestrictAddressFamilies=~AF_UNIX` | 0.1 | admin socket |
| `RootDirectory=`/`RootImage=` | 0.1 | static binary on the host file system |
| `DeviceAllow=` | 0.1 | `PrivateDevices` default (`char-rtc:r`) |
| `UMask=` | 0.1 | `UMask=0027`: the `obie` group may read the audit log |

The filter leaves out `@chown`: obied only changes the group of its admin
socket when `admin.socket_group` differs from the unit's `Group=obie`. If
you change one, change the other (in a drop-in) to match, or obied fails to
start with `chgrp admin socket … operation not permitted`.

Customize with `systemctl edit obied` (a drop-in survives upgrades) rather
than editing the unit; `make check-unit` shows what a change costs.

## Run the container image

```sh
docker run -d --name obie \
  -v obie-state:/var/lib/obie \
  -p 4001:4001 -p 4001:4001/udp \
  ghcr.io/mncloudwerkstechnology/obie:0.1.0
docker exec obie obiectl status
docker exec obie obiectl identity
```

The image is `distroless/static` and runs as `nonroot` (65532) with
`packaging/docker/obie.yaml`: state in the volume `/var/lib/obie`, admin
socket `/run/obie/obie.sock`, mesh on port 4001 (TCP and QUIC), metrics on
9464 (publish it only towards Prometheus), `observe` mode, `dryrun`
backend. Mount your own configuration over `/etc/obie/obie.yaml` to join a
mesh (`mesh.bootstrap`, `trust.publishers`); keep `node.state_dir`,
`admin.socket` and `admin.socket_group: nonroot`. Build it yourself with
`make image` (tag `obie:<git describe>`).

To try a mesh, start the [three-node lab](../../packaging/compose/README.md).

## Upgrade

1. Read the release notes.
2. Extract the new tarball and run `sudo ./install.sh` again: it replaces
   the binaries, the unit and the Fail2Ban action and keeps
   `/etc/obie/obie.yaml` (compare it with the new `obie.yaml.example`).
3. `sudo systemctl restart obied`.

For the container, pull the new tag and recreate the container with the
same volume.

### State directory format

The layout of the state directory is versioned: `<state_dir>/FORMAT` holds
its format (currently `1`). obied writes it on the first start (also into
directories from before the file existed) and migrates older formats when a
release changes the layout. It **refuses to start on a directory of a newer
format**, e.g. after a downgrade, and logs (in the `error` field of the
`obied failed` line):

```text
state directory: state directory has a newer format: /var/lib/obie has format 2,
but obied 0.1.0 only understands format 1 or older; it was last used by a newer
obied. Run that newer obied again, or restore a backup of the state directory
taken before the upgrade
```

So back up `/var/lib/obie` before an upgrade if you may need to roll back.

## Uninstall

```sh
sudo systemctl disable --now obied
sudo obied teardown-firewall            # removes the table inet obie, if any
sudo rm /etc/systemd/system/obied.service /usr/local/bin/obied /usr/local/bin/obiectl
sudo rm -f /etc/fail2ban/action.d/obie.conf
sudo systemctl daemon-reload
# and, to remove the identity, events and configuration as well:
sudo rm -r /var/lib/obie /etc/obie /var/log/obie; sudo userdel obie
```
