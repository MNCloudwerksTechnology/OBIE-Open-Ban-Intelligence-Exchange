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
| **Exchange warnings only with [peers](glossary.md#peer) you choose.** Your [node](glossary.md#node), the OBIE program on your server, connects to the peers you name: a friend's server, a partner's, or your own. There is no central service and no account. | Supported | [Federation](operations/federation.md) |
| **Block only when enough trusted peers agree.** You give each peer a [trust weight](glossary.md#trust-weight). Your node blocks an address only when the weighted verdicts reach a [threshold](glossary.md#threshold) and enough different peers agree, the [quorum](glossary.md#quorum). With the default settings, one peer alone can never get an address blocked. Your node shows why it blocks an address, or why not. | Supported | [Choose trust weights and quorum](operations/federation.md#choose-trust-weights-and-quorum) |
| **Keep the last word.** Always allow or always block any address with an [override](glossary.md#override), and put the networks you depend on on the [allow-list](glossary.md#allow-list). Your server's own addresses, internal networks and your peers are protected from the start. | Supported | [Override the mesh](operations/operations.md#override-the-mesh), [quick start, step 5](operations/quickstart.md#5-enforce) |
| **Watch before you block.** A new node starts in [observe mode](glossary.md#observe-mode): it decides and lists what it would block, but does not touch your firewall. | Supported | [Quick start, step 2](operations/quickstart.md#2-start-in-observe-mode) |
| **Block attackers in your firewall.** In [enforce mode](glossary.md#enforce-mode), your node blocks through its own [nftables](glossary.md#nftables) table and never changes the rules your administrators made. Every block ends on its own when its verdicts expire. | Off by default | [Quick start, step 5](operations/quickstart.md#5-enforce), [nftables guide](guides/nftables.md) |
| **Take back a report.** A [revocation](glossary.md#revocation) withdraws a verdict of your node from the peers it reaches. | Supported | [Fail2Ban guide](guides/fail2ban.md#remove), [federation](operations/federation.md#leave-a-federation) |
| **See how the node is doing.** Health checks and metrics for your monitoring (Prometheus), and a Grafana dashboard. They are only reachable on the server itself unless you open them. | Supported | [Monitoring](operations/monitoring.md#metrics) |
| **Keep an audit trail.** A log of every decision, override and report, in a format security tools (SIEM) read. | Off by default | [Audit log](operations/monitoring.md#audit-log) |
| **Look into the node in a browser.** A web console shows the node's health, peers, decisions and verdicts, and lets you allow, block, report or withdraw after a confirmation. Only the node's operators can sign in, on the server itself. | Off by default | [Web console](operations/console.md) |
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
| **Other firewalls.** OBIE blocks only through nftables on Linux: not through iptables-legacy, pf, Windows Firewall or a cloud provider's firewall. Faster blocking with eBPF is planned. | Planned for eBPF ([whitepaper](whitepaper.md#33-the-tech-stack)), no plan yet for others | OBIE's own table works next to firewalld, Docker and iptables-nft ([nftables guide](guides/nftables.md)). |
| **Hosts other than Linux.** No Windows, macOS or BSD, and no 32-bit processors. | No plan yet | A Linux router or firewall in front of those servers can run OBIE and block for them ([nftables guide](guides/nftables.md#configuration)). |
| **Blocking from the container image.** The container image only observes. | No plan yet | Install OBIE on the host itself to block ([install](operations/install.md#install-on-a-host-with-systemd)). |
| **Catching up after being offline.** A node receives verdicts only while it is connected; most of what it missed never arrives. | No plan yet | Keep your node connected. A peer's Fail2Ban reports an address again when it bans it again. |
| **Limiting who may connect.** Any node that reaches the OBIE port can connect. Its warnings count for nothing unless you trust it, but it uses bandwidth, processor time and disk. | No plan yet | Open the OBIE port only to your peers in your firewall ([federation](operations/federation.md#open-the-mesh-port)). |
