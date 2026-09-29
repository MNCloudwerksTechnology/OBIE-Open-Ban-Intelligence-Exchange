package cli

import (
	"context"
	"io"
)

// daemonCommand is an obied command.
type daemonCommand struct {
	commandHelp
	run func(ctx context.Context, reload <-chan struct{}, args []string, stdout, stderr io.Writer) int
}

// offline adapts a command that never runs the node, and so needs neither
// its shutdown nor its reload signal.
func offline(run func(args []string, stdout, stderr io.Writer) int) func(context.Context, <-chan struct{}, []string, io.Writer, io.Writer) int {
	return func(_ context.Context, _ <-chan struct{}, args []string, stdout, stderr io.Writer) int {
		return run(args, stdout, stderr)
	}
}

// daemonTool describes obied.
func daemonTool() *tool {
	t := &tool{
		name:    daemonName,
		summary: "run an OBIE node and prepare this server for it",
		usage:   []string{"<command> [flags]", "--config <file> [--check-config]"},
		description: `obied is the OBIE node. The obied service runs it with obied --config
/etc/obie/obie.yaml: it connects to your peers, exchanges signed verdicts
with them, decides which addresses to block and, in enforce mode, blocks
them in its own nftables table inet obie.

Its other commands work without a running node: they write the
configuration, check the node and this server, show or create the node's
identity key and remove OBIE's firewall table. obiectl controls the
running node.`,
		start: `Start here: sudo obied setup writes the configuration after a few questions;
then sudo systemctl enable --now obied starts the node, and sudo obied
self-check checks it and says what to do about anything that is not right.
Help on a command: obied help <command>, or obied <command> --help.
The running node is controlled with obiectl: obiectl --help.`,
		run: func(args []string, stdout, stderr io.Writer) int {
			return runDaemon(context.Background(), args, stdout, stderr)
		},
	}
	for _, c := range daemonCommands() {
		t.commands = append(t.commands, c.commandHelp)
	}
	return t
}

// daemonCommands are the commands of obied, in the order the overview lists
// them within their group.
func daemonCommands() []daemonCommand {
	return []daemonCommand{
		{commandHelp: commandHelp{
			name: "self-check", group: groupLook,
			summary: "check the node and this server; every problem comes with the next step",
			usage:   []string{"[--config <file>] [--service-user <user>] [--timeout <duration>] [--json]"},
			description: `Checks the node and this server, before the first start or at any time
later, and reports each check as OK, WARNING or PROBLEM with the next step:
configuration, identity, admin access, node, peers, clock, Fail2Ban,
firewall and your SSH session's address. It changes nothing. Run it as
root to let it look everywhere.

Exit status: 0 no problem (warnings may remain), 1 at least one problem,
2 wrong usage, 3 the report could not be written.`,
			examples: []example{
				{"Check the node and this server:", "sudo obied self-check"},
				{"List the IDs of the checks that are not OK, for a script:",
					`sudo obied self-check --json | jq -r '.checks[] | select(.status != "ok") | .id'`},
			},
		}, run: offline(runSelfCheck)},
		{commandHelp: commandHelp{
			name: "identity", group: groupLook,
			summary: "show the node's peer ID and key fingerprint from its key file",
			usage:   []string{"[--config <file> | --state-dir <directory>] [--json]"},
			description: `Reads the node's identity key from its state directory and shows its peer
ID and fingerprint, never the key itself. It works without a running node,
for example to check a backup of the key.`,
			examples: []example{
				{"Show the peer ID of this node:", "sudo obied identity"},
				{"Check which node a backup of the key belongs to:", "sudo obied identity --state-dir /root/obie-backup"},
			},
		}, run: offline(runIdentity)},
		{commandHelp: commandHelp{
			name: "setup", group: groupManage,
			summary: "write the node's configuration after a few questions (first-run assistant)",
			usage:   []string{"[--config <file>]", "--non-interactive [answer flags] [--force]"},
			description: `Writes the configuration of this node after asking a few questions:
where it keeps its state and audit log, which peers it connects to and
how much it trusts them, whether it starts in observe mode, and which
addresses it must never block. Every question offers a safe default. An
existing configuration file is only replaced after you agree; the old one
is kept as a backup. With --non-interactive the answers come from the
flags, and the same answers always write the same file.`,
			examples: []example{
				{"Answer the questions and write /etc/obie/obie.yaml:", "sudo obied setup"},
				{"Write it without questions, with one peer and your office network:",
					"sudo obied setup --non-interactive --peer /dns4/obie.friend.example/tcp/4001/p2p/" + peerIDExample +
						",name=friend,weight=0.8 --allow 198.51.100.0/24"},
			},
			values: map[string][]string{"mode": {"observe", "enforce"}},
		}, run: offline(runSetup)},
		{commandHelp: commandHelp{
			name: "run", group: groupManage,
			summary: "run the node in the foreground, as the obied service does",
			usage:   []string{"[--config <file>] [--check-config]"},
			description: `Loads the configuration and the allow-list files and runs the node until
it receives SIGTERM or SIGINT; SIGHUP reloads the configuration. With
--check-config it only checks the configuration and names every problem
with its line. The obied service runs obied --config /etc/obie/obie.yaml,
which is the same. To run the node as a service: sudo systemctl enable
--now obied.`,
			examples: []example{
				{"Check the configuration before you restart the node:",
					"sudo obied run --config /etc/obie/obie.yaml --check-config"},
				{"Run a test node with a configuration of your own in the foreground:",
					"obied run --config ./obie.yaml"},
			},
		}, run: func(ctx context.Context, reload <-chan struct{}, args []string, stdout, stderr io.Writer) int {
			return runNode(ctx, reload, daemonName+" run", args, stdout, stderr)
		}},
		{commandHelp: commandHelp{
			name: "keygen", group: groupManage,
			summary: "create the node's identity key",
			usage:   []string{"[--config <file> | --state-dir <directory>] [--force]"},
			description: `Creates the key the node signs its verdicts with, and from which its peer ID
comes, in the state directory, and shows the peer ID. The node creates
its key itself at its first start; keygen creates one ahead of time, for
example for a test node. The key must belong to the user the node runs as.

--force replaces an existing key: the node gets a new peer ID, and the
peers that trust the old one must change their configuration.`,
			examples: []example{
				{"Create the key of a test node in a directory of your own:", "obied keygen --state-dir ./node-a"},
				{"Replace a stolen key; the peer ID changes:",
					"sudo systemctl stop obied && sudo -u obie obied keygen --force"},
			},
		}, run: offline(runKeygen)},
		{commandHelp: commandHelp{
			name: "teardown-firewall", group: groupManage,
			summary: "remove OBIE's nftables table inet obie, and with it every block",
			usage:   []string{"[--on-stop [--config <file>]]"},
			description: `Deletes the nftables table inet obie, which holds every block OBIE applied,
and nothing else: your own firewall rules stay. Use it when you are locked
out or before you uninstall OBIE. Stop the node first, or in enforce mode
it puts its blocks back.

With --on-stop, as the systemd unit's ExecStopPost, it only removes the
table if enforce.nftables.teardown_on_stop is true in the configuration.`,
			examples: []example{
				{"Lift every OBIE block at once:", "sudo systemctl stop obied && sudo obied teardown-firewall"},
			},
		}, run: offline(runTeardownFirewall)},
	}
}
