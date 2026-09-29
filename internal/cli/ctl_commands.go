package cli

import (
	"context"
	"io"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
)

// ctlName is the name of the operator CLI.
const ctlName = "obiectl"

// command is an obiectl command.
type command struct {
	commandHelp
	run func(ctx context.Context, client *admin.Client, args []string, stdout, stderr io.Writer) int
}

// ctlTool describes obiectl.
func ctlTool() *tool {
	t := &tool{
		name:    ctlName,
		summary: "control the OBIE node running on this server",
		usage:   []string{"[global flags] <command> [flags] [arguments]"},
		description: `obiectl talks to the node on this server, obied, through its admin socket.
It shows what the node knows and decides, lets you overrule it for single
addresses and ranges, and reports attacks to your peers as signed verdicts.

It needs a running node, and root or membership in the group that owns the
admin socket (admin.socket_group, obie by default).`,
		start: `Start here: sudo obiectl status shows whether the node runs and whether it
blocks (enforce mode) or only observes. Then sudo obiectl decisions --state block
lists what it blocks, or would block in observe mode.
Help on a command: obiectl help <command>, or obiectl <command> --help.`,
		globalFlags: true,
		run:         RunCtl,
	}
	for _, c := range ctlCommands() {
		t.commands = append(t.commands, c.commandHelp)
	}
	return t
}

// peerIDExample is a peer ID for the examples.
const peerIDExample = "12D3KooWKrKnKarP5Ne57JSKsV1sPmXitDQq7ijNTxgw7WSGqEXf"

// ctlCommands are the commands of obiectl, in the order the overview lists
// them within their group.
func ctlCommands() []command {
	return []command{
		{commandHelp: commandHelp{
			name: "status", group: groupLook,
			summary: "show whether the node runs, its mode and the state of its subsystems",
			usage:   []string{"[--json]"},
			description: `Shows the node's mode first: OBSERVE (it decides but blocks nothing) or
ENFORCE (it blocks through the firewall). Then its version, how long it has
been running, whether it is ready, and one row per subsystem with its
state and any error. Run it first when something seems wrong.`,
			examples: []example{
				{"See whether the node runs and whether it blocks:", "sudo obiectl status"},
				{"The same for a monitoring script:", "sudo obiectl status --json"},
			},
		}, run: runStatus},
		{commandHelp: commandHelp{
			name: "peers", group: groupLook,
			summary: "list the connected peers with their trust weight and latency",
			usage:   []string{"[--json]"},
			description: `Lists every peer the node is connected to right now: its peer ID, the name
and trust weight you gave it in trust.publishers, whether it is a bootstrap
peer, since when it is connected, the round-trip time and its addresses.
A peer you configured that is not listed is not connected; obied
self-check tests whether it answers.`,
			examples: []example{
				{"See which peers are connected:", "sudo obiectl peers"},
				{"Only their peer IDs, for a script:", "sudo obiectl peers --json | jq -r '.peers[].peer_id'"},
			},
		}, run: runPeers},
		{commandHelp: commandHelp{
			name: "identity", group: groupLook,
			summary: "show this node's peer ID and key fingerprint",
			usage:   []string{"[--json]"},
			description: `Shows the peer ID that other operators put into their configuration to
connect to and trust this node, and the fingerprint of its key, never the
key itself. obied identity shows the same without a running node.`,
			examples: []example{
				{"Show the peer ID to give to a friend's node:", "sudo obiectl identity"},
			},
		}, run: runCtlIdentity},
		{commandHelp: commandHelp{
			name: "decisions", group: groupLook,
			summary: "list what the node decided for each address: block, allowed or none",
			usage:   []string{"[--state block|none|allowed] [--json]"},
			description: `Lists the node's decision on every address and range it holds verdicts or
overrides for: block (it blocks the address, or would in observe mode),
allowed (the allow-list or an override protects it) or none (not enough
trusted publishers agree), with the score, the number of publishers, when
the decision ends and why it was made. obiectl explain shows the details
of one address.`,
			examples: []example{
				{"See what the node blocks, or would block in observe mode:", "sudo obiectl decisions --state block"},
				{"Every decision as JSON, for a script:", "sudo obiectl decisions --json"},
			},
			values: map[string][]string{"state": {"block", "none", "allowed"}},
		}, run: runDecisions},
		{commandHelp: commandHelp{
			name: "explain", group: groupLook,
			summary: "explain why an address or range is or is not blocked",
			usage:   []string{"[--json] <address | range>"},
			description: `Shows the decision on the address or range and the reason for it: the
score against the threshold, the number of publishers against the quorum,
whether local autoblock, the allow-list or an override applies, and one row
per publisher with its trust weight, confidence and share of the score. The
address may also be written as an indicator key, such as ipv4:203.0.113.7.`,
			examples: []example{
				{"Ask why an address is blocked, or why not:", "sudo obiectl explain 203.0.113.7"},
				{"Ask about a whole range:", "sudo obiectl explain 198.51.100.0/24"},
			},
		}, run: runExplain},
		{commandHelp: commandHelp{
			name: "indicators", group: groupLook,
			summary: "list the addresses and ranges that have active verdicts",
			usage:   []string{"[--mine | --publisher <peer ID>] [--limit <n>] [--cursor <cursor>] [--json]"},
			description: `Lists the active verdicts the node holds, one row per verdict, grouped by
address or range, a page at a time. When more follow, the last line says
how to get the next page.`,
			examples: []example{
				{"See what this node has reported itself:", "sudo obiectl indicators --mine"},
				{"See what one peer has reported:", "sudo obiectl indicators --publisher " + peerIDExample},
			},
		}, run: runIndicators},
		{commandHelp: commandHelp{
			name: "show", group: groupLook,
			summary: "show every active verdict on one address or range",
			usage:   []string{"[--json] <address | range>"},
			description: `Lists the active verdicts on the address or range from every publisher,
with their action, confidence, number of events, reason, when they were
issued and end, and their event ID, which obiectl revoke accepts.`,
			examples: []example{
				{"See who reported an address and why:", "sudo obiectl show 203.0.113.7"},
			},
		}, run: runShow},
		{commandHelp: commandHelp{
			name: "overrides", group: groupLook,
			summary: "list your overrides: the addresses you always allow or always block",
			usage:   []string{"[--json]"},
			description: `Lists the overrides set with obiectl allow and obiectl block, with when each
ends and the note you gave it.`,
			examples: []example{
				{"See which addresses you overruled:", "sudo obiectl overrides"},
			},
		}, run: runOverrides},
		{commandHelp: commandHelp{
			name: "enforced", group: groupLook,
			summary: "list the blocks the firewall applies right now",
			usage:   []string{"[--json]"},
			description: `Lists the addresses and ranges the enforcement backend applies, with when
each block ends. In observe mode the list is empty. sudo nft list table
inet obie shows what the kernel holds.`,
			examples: []example{
				{"See what the firewall blocks right now:", "sudo obiectl enforced"},
			},
		}, run: runEnforced},
		{commandHelp: commandHelp{
			name: "allow", group: groupDecide,
			summary: "always allow an address or range: never block it",
			usage:   []string{"<address | range> [--ttl <duration>] [--note <text>] [--json]"},
			description: `Sets an override that never blocks the address or range, whatever the mesh
reports. It beats every other rule, including the allow-list and always-block
overrides on overlapping ranges. It stays until you remove it with obiectl
unoverride, or until --ttl ends it.`,
			examples: []example{
				{"Never block your office network:", `sudo obiectl allow 198.51.100.0/24 --note "office"`},
				{"Allow an address for a day while you look into a false alarm:",
					`sudo obiectl allow 203.0.113.7 --ttl 1d --note "false alarm, ticket 4711"`},
			},
		}, run: runAllow},
		{commandHelp: commandHelp{
			name: "block", group: groupDecide,
			summary: "always block an address or range, whatever its score",
			usage:   []string{"<address | range> [--ttl <duration>] [--note <text>] [--json]"},
			description: `Sets an override that blocks the address or range whatever the mesh
reports. It beats the networks you added to the allow-list (allowlist.cidrs
and allowlist.files), but never the protected addresses: the built-in
ranges, this node's own addresses and its bootstrap peers. In observe mode
nothing is blocked, but the decision shows it.`,
			examples: []example{
				{"Block a scanner for a week:", `sudo obiectl block 203.0.113.7 --ttl 7d --note "scans our web server"`},
			},
		}, run: runBlock},
		{commandHelp: commandHelp{
			name: "unoverride", group: groupDecide,
			summary: "remove your override of an address or range",
			usage:   []string{"<address | range> [--json]"},
			description: `Removes the override set with obiectl allow or obiectl block. The node then
decides on the address as before, by the allow-list and the verdicts, and
shows the decision now.`,
			examples: []example{
				{"Let the mesh decide on an address again:", "sudo obiectl unoverride 203.0.113.7"},
			},
		}, run: runUnoverride},
		{commandHelp: commandHelp{
			name: "report", group: groupReport,
			summary: "publish a signed verdict on an attacking address or range",
			usage: []string{
				"--protocol <service> --reason <class> [flags] <address | range>",
				"--protocol <service> --reason <class> [flags] --ip <address | range>",
			},
			description: `Turns a detection on this server into a verdict signed with the node's key
and sends it to the peers. With local autoblock (on by default) it counts
on this node at once. Log lines given as evidence are hashed on this
server: only the hash and the number of events leave it. Protected and
allow-listed addresses are never reported.

Reporting the same address again within a minute adds the events to the
next refresh of the verdict; later reports refresh it.`,
			examples: []example{
				{"Report an SSH brute force seen in 12 failed logins:",
					"sudo obiectl report --protocol ssh --reason password_bruteforce --events 12 203.0.113.7"},
				{"Report it with the log lines as evidence; only their hash leaves the server:",
					"grep 203.0.113.7 /var/log/auth.log | sudo obiectl report --protocol ssh --reason password_bruteforce --evidence-from-stdin 203.0.113.7"},
				{"Ask peers to watch, not block, a scanning range for 12 hours:",
					"sudo obiectl report --protocol http --reason scanning --action watch --ttl 12h 198.51.100.0/24"},
			},
			values: map[string][]string{"action": {"ban", "watch"}},
		}, run: runReport},
		{commandHelp: commandHelp{
			name: "revoke", group: groupReport,
			summary: "withdraw a verdict this node published",
			usage:   []string{"[--reason <reason>] [--json] <event ID | address | range>"},
			description: `Revokes this node's own active verdict with that event ID, or its verdicts
on that address or range, and tells the peers. Only the publisher of a
verdict can revoke it. obiectl show lists the event IDs.`,
			examples: []example{
				{"Withdraw a report that was a false alarm:", "sudo obiectl revoke 203.0.113.7"},
				{"Withdraw one verdict by its event ID:",
					"sudo obiectl revoke --reason incident_closed 1b4e28ba-2fa1-41d2-883f-0016d3cca427"},
			},
		}, run: runRevoke},
		{commandHelp: commandHelp{
			name: "console", group: groupManage,
			summary: "show the web console's address and sign-in token",
			usage:   []string{"[--rotate] [--json]"},
			description: `Prints where the web console serves and the token to sign in with, and on
standard error how to open it, also from another machine. The console is
off unless console.enabled is true in the configuration.`,
			examples: []example{
				{"Get the address and the token to sign in:", "sudo obiectl console"},
				{"Issue a new token and sign every browser out:", "sudo obiectl console --rotate"},
			},
		}, run: runConsole},
	}
}
