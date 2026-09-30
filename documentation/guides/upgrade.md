# How do I upgrade to a new release?

Install a new release of OBIE over the one your
[node](../glossary.md#node) runs. The node keeps its identity, its
configuration and what it knows, and is down for a few seconds. Blocks
already in the firewall stay in place meanwhile.

## Before you start

- Your node was installed from the release archive, as
  [Get started](../getting-started.md#6-connect-fail2ban) leaves it after
  step 6.
- You have read what the new release changes, in the
  [changelog](../../CHANGELOG.md). `0.1.1` stands for the new version
  here, and `0.1.0` for the one your node runs.
- To go back, you need the archive of the version your node runs. It is
  still in your home directory from step 2 of Get started; if not,
  download it again the same way.

## Steps

Download the new release and its checksums, check the archive, and
unpack it:

```sh
curl -fsSLO https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange/releases/download/v0.1.1/obie-0.1.1-linux-amd64.tar.gz
curl -fsSLO https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange/releases/download/v0.1.1/SHA256SUMS
```

```sh
sha256sum -c --ignore-missing SHA256SUMS
```

```text
obie-0.1.1-linux-amd64.tar.gz: OK
```

```sh
tar -xzf obie-0.1.1-linux-amd64.tar.gz
```

Stop the node and back up its state directory, so that you can go back:

```sh
sudo systemctl stop obied
sudo tar -C /var/lib -czf /root/obie-state-before-upgrade.tar.gz obie
```

Run the new release's installer. It replaces the programs, the service,
the manual pages and OBIE's Fail2Ban action, and keeps your
configuration:

```sh
sudo ./obie-0.1.1-linux-amd64/install.sh
```

```text
install.sh: installed obied and obiectl into /usr/local/bin
install.sh: installed the manual pages and bash, zsh and fish completions below /usr/local/share
install.sh: kept /etc/obie/obie.yaml; the current example is /etc/obie/obie.yaml.example
install.sh: installed /etc/systemd/system/obied.service
install.sh: installed the Fail2Ban action /etc/fail2ban/action.d/obie.conf (override it in obie.local)
install.sh: done. Next steps:
…
```

The next steps it lists are those of a first installation; you need none
of them. `/etc/obie/obie.yaml.example` shows every setting of the new
release, if the changelog names one you want. Start the node:

```sh
sudo systemctl start obied
```

## Check that it worked

The node runs the new version:

```sh
sudo obiectl status
```

```text
Mode:     OBSERVE (decisions are logged, nothing is blocked)
Version:  0.1.1
…
```

Let it check itself; there must be no `PROBLEM`:

```sh
sudo obied self-check
```

```text
OBIE self-check of /etc/obie/obie.yaml (obied 0.1.1, as root)
…
Result: 0 problems, …
```

If the node does not start, see
[obied does not start](../operations/troubleshooting.md#obied-does-not-start),
or go back as below.

## Undo

To go back to the version you ran before, stop the node, put back the
state directory from the backup, and install the old release again. A
newer node may convert its state directory, and an older one refuses to
start on it; the backup is how you go back:

```sh
sudo systemctl stop obied
sudo rm -r /var/lib/obie
sudo tar -C /var/lib -xzf /root/obie-state-before-upgrade.tar.gz
sudo ./obie-0.1.0-linux-amd64/install.sh
sudo systemctl start obied
```

```sh
sudo obiectl status
```

```text
Mode:     OBSERVE (decisions are logged, nothing is blocked)
Version:  0.1.0
…
```

What the node learned since the backup is lost: the
[verdicts](../glossary.md#verdict) it received, and the
[overrides](../glossary.md#override) you set.
