# What OBIE can and cannot do yet

This page describes **OBIE 0.1.0**, the first release. It lists what this
release does for you, what it does not do yet, what it needs to run and
which risks remain. Use it to decide whether OBIE fits your servers before
you install anything.

New to OBIE? [What is OBIE?](introduction.md) explains the idea in five
minutes, and the [glossary](glossary.md) explains every term.

## What it can do

Each row is something you get from OBIE 0.1.0, with the guide that shows
how. **Supported** means it works once you have set it up. **Off by
default** means it works, but you have to switch it on. **Observe only**
means it shows what it would block and never blocks anything.

| What you get | Status | How |
|---|---|---|
| **Share what Fail2Ban catches.** Each ban of a [Fail2Ban](glossary.md#fail2ban) jail you choose becomes a signed warning, a [verdict](glossary.md#verdict), for the servers you exchange warnings with. The log lines stay on your server. | Supported | [Fail2Ban guide](guides/fail2ban.md): one extra line per jail |
| **Choose the [peers](glossary.md#peer) you connect to and trust.** Your [node](glossary.md#node), the OBIE program on your server, connects to the peers you name: a friend's server, a partner's, or your own. There is no central service and no account. Other nodes that reach yours can connect too and receive your warnings, but theirs count for nothing unless you trust them. | Supported | [Federation](operations/federation.md) |
| **Block only when enough trusted peers agree.** You give each peer a [trust weight](glossary.md#trust-weight). Your node blocks an address only when the weighted verdicts reach a [threshold](glossary.md#threshold) and enough different peers agree, the [quorum](glossary.md#quorum). With the default settings, one peer alone can never get an address blocked. Your own server's detections block at once ([local autoblock](glossary.md#local-autoblock)). Your node shows why it blocks an address, or why not. | Supported | [Choose trust weights and quorum](operations/federation.md#choose-trust-weights-and-quorum) |
| **Keep the last word.** Always allow any address, or always block any address that is not protected, with an [override](glossary.md#override). Put the networks you depend on on the [allow-list](glossary.md#allow-list). Your server's own addresses, internal networks and the peers you told your node to connect to are [protected](glossary.md#protected-addresses) from the start. A public address your server has only behind a router (NAT) is not detected; put it on the allow-list. | Supported | [Override the mesh](operations/operations.md#override-the-mesh), [quick start, step 5](operations/quickstart.md#5-enforce) |
| **Watch before you block.** A new node starts in [observe mode](glossary.md#observe-mode): it decides and lists what it would block, but does not touch your firewall. | Supported | [Quick start, step 2](operations/quickstart.md#2-start-in-observe-mode) |
| **Block attackers in your firewall.** In [enforce mode](glossary.md#enforce-mode), your node blocks through its own [nftables](glossary.md#nftables) table and never changes the rules your administrators made. Every block ends on its own when its verdicts expire. | Off by default | [Quick start, step 5](operations/quickstart.md#5-enforce), [nftables guide](guides/nftables.md) |
| **Protect what sits behind your server.** On a router or a host of containers, your node can also block traffic that passes through it to other machines or containers, such as the ports Docker publishes. | Off by default | [nftables guide](guides/nftables.md#configuration) |
| **Take back a report.** A [revocation](glossary.md#revocation) withdraws a verdict of your node from the peers it reaches. | Supported | [Fail2Ban guide](guides/fail2ban.md#remove), [federation](operations/federation.md#leave-a-federation) |
| **See how the node is doing.** Health checks and metrics for your monitoring (Prometheus), and a Grafana dashboard. They are only reachable on the server itself unless you open them. | Supported | [Monitoring](operations/monitoring.md#metrics) |
| **Keep an audit trail.** A log of every decision, override and report, in a format security tools (SIEM) read. | Off by default | [Audit log](operations/monitoring.md#audit-log) |
| **Look into the node in a browser.** A web console shows the node's health, peers, decisions and verdicts, and lets you allow, block, report or withdraw after a confirmation. It listens only on the server itself, and only the node's operators can sign in. | Off by default | [Web console](operations/console.md) |
| **Try it without touching a firewall.** Three nodes on a laptop, or the container image, show OBIE at work and block nothing real. | Observe only | [Three-node lab](../packaging/compose/README.md), [container image](operations/install.md#run-the-container-image) |
| **Keep your server running if OBIE fails.** OBIE is not in the path of your traffic. If it stops, existing blocks still end on time and Fail2Ban keeps working. | Supported | [FAQ](faq.md#what-happens-if-obie-crashes) |

## What it cannot do yet

Each row is something OBIE 0.1.0 does not do. **Planned** means the
[whitepaper](whitepaper.md#6-implementation-roadmap) or the architecture's
[future work](../ARCHITECTURE.md#future-work) puts it on the roadmap, without
a date. **No plan yet** means nobody has planned it.

| What is missing | Planned? | Until then |
|---|---|---|
| **Automatic peer discovery.** Nodes do not find each other, and there is no public network of nodes to join. | Planned ([future work](../ARCHITECTURE.md#future-work)) | Exchange addresses with the operator of each peer and enter them by hand ([federation](operations/federation.md#exchange-peer-ids-and-addresses)). |
| **Connecting two nodes that are both behind a router.** One of the two must accept connections from the internet. | Planned ([future work](../ARCHITECTURE.md#future-work)) | Forward the OBIE port on one side, or connect to a peer on a server that is reachable from the internet. |
| **Trust that adapts over time.** Trust weights are numbers you set. OBIE does not track which peers were right, trust does not grow or decay, and new peers get no warm-up period. | Planned ([roadmap](whitepaper.md#6-implementation-roadmap)) | Look at what each peer reported, in the web console or with `obiectl explain`, and change its weight by hand. |
| **Agreement across organisations.** The quorum counts nodes, not organisations or networks: three nodes of one operator count as three peers. | Planned ([future work](../ARCHITECTURE.md#future-work)) | Give the nodes of one operator lower weights, so that together they count like one. |
| **Appeals.** Whoever is blocked by mistake cannot ask the network to lift the block. | Planned ([roadmap](whitepaper.md#6-implementation-roadmap)) | They contact you. You allow their address with an override and ask the peer that reported it to withdraw its verdict. |
| **Replacing a node's key.** A node whose secret key is lost or stolen needs a new identity. | Planned ([future work](../ARCHITECTURE.md#future-work)) | Create a new key and ask every peer's operator to enter your new peer ID ([operations](operations/operations.md#back-up-the-node-key)). |
| **Other detection tools, ready-made.** Only Fail2Ban has a ready-made connection. One for honeypots (Cowrie, T-Pot) is planned; others, such as CrowdSec or Suricata, are not. | Planned for honeypots ([whitepaper](whitepaper.md#53-operational-integration-siem-and-soar)), no plan yet for others | Any tool that can run a command can report an address with `obiectl report`, as the [Fail2Ban action](guides/fail2ban.md) does. |
| **Warnings about more than addresses.** Verdicts name internet addresses and address ranges only: no domain names, web addresses, fingerprints or file hashes. | Planned ([whitepaper](whitepaper.md#321-technical-schema)) | Keep using your other tools for them. |
| **Other firewalls.** OBIE blocks only through nftables on Linux: not through iptables-legacy, pf, Windows Firewall or a cloud provider's firewall. Faster blocking inside the Linux kernel (eBPF), and blocking through Fail2Ban, are planned. | Planned for eBPF and Fail2Ban ([whitepaper](whitepaper.md#33-the-tech-stack)), no plan yet for others | OBIE's own table works next to firewalld, Docker and iptables-nft ([nftables guide](guides/nftables.md)). |
| **Hosts other than Linux.** No Windows, macOS or BSD, and no 32-bit processors. | No plan yet | A Linux router or firewall in front of those servers can run OBIE and block for them, once you switch on blocking of passing traffic ([nftables guide](guides/nftables.md#configuration)). |
| **Blocking from the container image.** The container image only observes. | No plan yet | Install OBIE on the host itself to block ([install](operations/install.md#install-on-a-host-with-systemd)). |
| **Catching up after being offline.** A node receives verdicts only while it is connected; most of what it missed never arrives. | No plan yet | Keep your node connected. A peer's Fail2Ban reports an address again when it bans it again. |
| **Limiting who may connect.** Any node that reaches the OBIE port can connect. Its warnings count for nothing unless you trust it, but it uses bandwidth, processor time and disk. | No plan yet | Open the OBIE port only to your peers in your firewall ([federation](operations/federation.md#open-the-mesh-port)). |

## What it needs

### Operating system and processor

- **Linux on a 64-bit processor:** x86 (amd64) or ARM (arm64). OBIE is two
  programs, each a single file that needs no other software. Every change
  is tested on amd64 with Ubuntu; the arm64 programs are built, but not
  tested on ARM hardware.
- **systemd** for the service that the installer sets up. The service is
  checked with systemd 255 (Ubuntu 24.04). Without systemd, you have to
  start the node yourself; no guide covers that yet.
- **A Linux kernel with nftables**, only for blocking in enforce mode. The
  `nft` command is not needed. Blocking is tested on Linux 7.0, the
  kernel of the test machine.
- **Docker**, only for the container image or the three-node lab.

### Privileges

- **Root, once, to install.** The installer creates the system user
  `obie`, installs the programs and the service, and starts nothing.
- **The node does not run as root.** It runs as the user `obie`, in a
  sandbox, with one extra right: changing the firewall (`CAP_NET_ADMIN`).
  It uses it only in enforce mode
  ([service sandbox](operations/install.md#the-service-sandbox)).
- **Only you control it.** Root and the members of the group `obie` may
  use `obiectl` and sign in to the web console. The Fail2Ban action runs as
  root, like Fail2Ban itself.

### Network

| Port | Reachable from | Used for |
|---|---|---|
| 4001, TCP and UDP | your peers | The connections between nodes. Of every two peers, at least one must accept connections on it. |
| 9464, TCP | this server only | Health checks and metrics. Open it to your monitoring if you use them. |
| 9465, TCP | this server only | The web console, if you switch it on. Reach it from your workstation through SSH. |

- **Outgoing connections go only to your peers.** Your node contacts no
  central server, sends no usage data and does not check for updates. It
  asks DNS for the addresses of peers you name by host name.
- **Clocks must be right.** A node ignores verdicts dated more than five
  minutes ahead and lets verdicts expire by its clock. Keep time
  synchronisation (NTP) running, as most servers do.

### Resources

Measured with `make resources` on the reference host, a desktop computer,
with each node set to use one processor core (`GOMAXPROCS=1`)
([measurement](operations/performance.md#resource-usage-of-one-node)). One
node of three, while its two peers send it verdicts:

| Your node holds | Memory | Processor | Disk |
|---|---:|---:|---:|
| No verdicts, connected to 2 peers | 34 MiB | 0.2 % of one core | under 1 MiB |
| 10,000 verdicts, receiving 200 a second | 123 MiB | 10 % | 17 MiB |
| 100,000 verdicts, receiving 200 a second | 366 MiB, peaking at 514 MiB | 43 % | 87 MiB |
| 100,000 verdicts, at rest | 349 MiB | 2 % | 87 MiB |

- **Memory grows with the verdicts your node holds**, by about 3 MiB per
  1,000. A verdict from Fail2Ban lives as long as its ban: 10 minutes
  with Fail2Ban's default, 7 days for a permanent ban. Five peers that
  each ban 300 addresses a day, each for a week, keep about 10,000
  verdicts on your node.
- **200 verdicts a second is far more than a small federation sends.**
  The processor numbers come from a fast desktop core; a small server's
  core is slower.
- **Disk** counts the node's state and its audit log, if you switch it
  on. The audit log grows until you rotate it
  ([rotation](operations/monitoring.md#rotation)).
- **Limits:** a node keeps at most 1,000,000 verdicts by default. On a
  small server, lower that limit, `store.max_indicators`, to bound memory
  and disk ([configuration](operations/configuration.md#store)).

### Fail2Ban

- **Optional.** Without it, your node acts on your peers' verdicts and on
  what you report yourself.
- **Fail2Ban 0.11 or newer**, on the same server as the node. A real ban
  and unban through the OBIE action work with Fail2Ban 0.11.2, 1.0.2 and
  1.1.0 ([measurement](operations/performance.md#fail2ban-versions)).
  Those are the versions of Ubuntu 22.04, 24.04 and 26.04, Debian 12 and
  13, Alpine 3.22 and Rocky Linux 9. Fail2Ban 0.10 reports bans too, but
  its verdicts then last 7 days instead of the ban time.

## Remaining risks

OBIE 0.1.0 guards against the obvious ways others could misuse your
firewall, but some risks remain. The
[threat model](../SECURITY.md#threat-model) describes each one, how OBIE
limits it and what is left. In short:

- **A peer you trust can get the wrong address blocked.** If enough peers
  you trust agree, or you lower the quorum to one, they can block any
  public address on your server, even by mistake. Nothing limits how many
  addresses one peer can get blocked, and the blocked party cannot appeal
  (threat model: [poisoning by a trusted peer](../SECURITY.md#poisoning-by-a-trusted-peer)).
- **Anyone who reaches the OBIE port can connect.** Their verdicts count
  for nothing unless you trust them, but they use bandwidth, processor time
  and disk. Open the port only to your peers
  (threat model: [fake peers](../SECURITY.md#sybil-peers)).
- **A node that was offline can miss a withdrawal.** It keeps the withdrawn
  verdict until the verdict expires, and someone can send it the old
  verdict again until then. The clocks of all nodes must be right
  (threat model: [replay](../SECURITY.md#replay)).
- **A stolen key speaks in your name.** Whoever steals your node's secret
  key is trusted like you until every peer removes your identity by hand
  (threat model: [key theft](../SECURITY.md#key-theft)).
- **A flood can push out real warnings.** An attacker with many fake nodes
  can fill your node's store, so that verdicts of peers you trust make room
  (threat model: [resource exhaustion](../SECURITY.md#resource-exhaustion)).
- **OBIE cannot know every address you depend on.** Anything that is not
  protected or on your allow-list can be blocked: your office behind a
  router, your monitoring, a content delivery network, or everybody behind
  one shared address. Start in observe mode and fill the allow-list first
  (threat model: [allow-list gaps](../SECURITY.md#self-dos-via-allow-list-gaps)).
- **The web console trusts your workstation.** When you reach it through
  SSH, other users of your workstation can reach it too, and only the
  sign-in token protects it
  (threat model: [local web console](../SECURITY.md#local-web-console)).
- **Other nodes learn about your servers.** Every node connected to your
  mesh, trusted or not, receives your verdicts. It learns which of your
  services were attacked and sees your server's address. Attacker
  addresses can be personal data, for example under the GDPR
  (threat model: [privacy leakage](../SECURITY.md#privacy-leakage)).

OBIE's code is fuzzed and checked by static analysis before every release
([performance](operations/performance.md)), but no independent security
audit has been done yet.

## Is OBIE for me?

Find the situation closest to yours. **Yes** means OBIE 0.1.0 does it.
**Not yet** means something it needs is missing, but planned. **No** means
it is missing and not planned.

| Your situation | Answer | Why |
|---|---|---|
| **One Linux VPS, and someone you trust who runs OBIE too.** | **Yes** | Connect your two nodes. Start with only your own detections blocking; let your peer's verdicts block once you trust them ([federation](operations/federation.md#choose-trust-weights-and-quorum)). |
| **One Linux VPS, and nobody to exchange warnings with.** | **Not yet** | On its own, OBIE adds little to Fail2Ban, and there is no public network of nodes to join until automatic peer discovery and trust that adapts arrive. |
| **Several Linux servers of your own.** | **Yes** | Each server's node warns the others, and you decide how many must agree before one blocks. |
| **A small hosting provider with many Linux servers.** | **Yes** | Start in observe mode, and put your customers' and your monitoring's addresses on the allow-list before you block. Remember that blocking an address shared by many users blocks all of them. |
| **A homelab behind a home router, next to a friend's server on the internet.** | **Yes** | Your node connects out to your friend's, so you open no port at home. If neither of you can accept connections, it does not work yet. |
| **Servers with a firewall managed by firewalld, Docker or iptables-nft.** | **Yes** | OBIE adds its own firewall table and never changes theirs. To protect Docker's containers too, switch on blocking of passing traffic, which is off by default ([nftables guide](guides/nftables.md#configuration)). |
| **Servers that run Windows, macOS or BSD.** | **No** | OBIE runs on Linux only. A Linux router or firewall in front of those servers can run OBIE and block for them, once you switch on blocking of passing traffic. |
| **Only containers, such as managed Kubernetes, without access to the host's firewall.** | **No** | The container image only observes. Blocking needs OBIE on the host itself. |

## How this page is kept current

This page describes one release, named at the top, and changes with every
release. Before a release, the maintainers update the statuses, plans,
requirements and risks, measure the requirements again and record in the
[changelog](../CHANGELOG.md) what changed
([releasing](../CONTRIBUTING.md#releasing)). A release is not built until
this page names it. The page for the release you run is in that release's
source code.
