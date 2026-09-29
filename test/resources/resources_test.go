//go:build resources

package resources

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/identity"
)

// The measurement; `make resources` sets the load.
var (
	binDir     = flag.String("resources.bin", "../../bin", "directory holding the obied binary to measure (make build)")
	idleFor    = flag.Duration("resources.idle", 2*time.Minute, "how long the idle node is measured")
	verdicts   = flag.String("resources.verdicts", "10000,100000", "comma-separated, ascending: the verdicts the measured node holds at each measurement under load")
	rate       = flag.Float64("resources.rate", 100, "reports per second each of the two publishing nodes sends")
	restFor    = flag.Duration("resources.rest", 2*time.Minute, "how long the node is measured at rest after the load")
	gomaxprocs = flag.Int("resources.gomaxprocs", 1, "GOMAXPROCS of every node: the CPUs of the host it models")
	outFile    = flag.String("resources.out", "", "file the results are also written to, as Markdown")
)

const (
	// weight is every node's trust in the two others. With threshold 1.2
	// and quorum 2, an address both publishers report (2 × 0.8 × 0.8 =
	// 1.28) is blocked on the measured node, as in the federation guide's
	// "three to five nodes" setup.
	weight    = 0.8
	threshold = 1.2
	quorum    = 2
	// reporters bounds the reports in flight at once per publisher.
	reporters = 32
	// startBound bounds a node's start, meshBound the mesh forming,
	// drainBound the arrival of the last events after the last report.
	startBound = 30 * time.Second
	meshBound  = 30 * time.Second
	drainBound = 2 * time.Minute
	// requestTimeout bounds one admin API or metrics request.
	requestTimeout = 10 * time.Second
	// userHZ is the unit of utime and stime in /proc/<pid>/stat: USER_HZ,
	// 100 on every Linux architecture Go supports.
	userHZ = 100
)

// firstIP is the first reported address; the i-th report is on firstIP +
// i. 11.0.0.0/8 is public, so every report takes the full path through
// validation and decision.
var firstIP = netip.MustParseAddr("11.0.0.0")

// probeIP is the first address of the probes that check the mesh forwards
// events; they come from a /16 no load reaches.
var probeIP = netip.MustParseAddr("11.255.0.0")

// node is one obied process with its directory, configuration, admin
// socket, metrics address and log.
type node struct {
	name, dir, peerID string
	meshPort          int
	metrics           string
	client            *admin.Client
	cmd               *exec.Cmd
	done              chan error
}

// TestResources starts three obied processes on 127.0.0.1 that trust each
// other and measures the first, A: idle and connected to its two peers;
// while the other two report the same addresses at -resources.rate each
// until A holds each number of verdicts in -resources.verdicts; and at
// rest afterwards. It records A's resident memory (RSS, and its peak in
// the phase), its CPU time as a share of one CPU, and the disk its state
// directory and audit log take. A runs in enforce mode with the dryrun
// backend, so it keeps every block it decides without a firewall.
func TestResources(t *testing.T) {
	targets, err := parseTargets(*verdicts)
	if err != nil {
		t.Fatal(err)
	}
	obied, err := filepath.Abs(filepath.Join(*binDir, "obied"))
	if err != nil {
		t.Fatal(err)
	}
	version, err := exec.Command(obied, "--version").Output() // #nosec G204 -- the tester chooses the binary.
	if err != nil {
		t.Fatalf("%s --version: %v (run make build first)", obied, err)
	}

	nodes := startMesh(t, obied, "A", "B", "C")
	a, publishers := nodes[0], nodes[1:]
	awaitMesh(t, a, publishers)
	host := describeHost(strings.TrimSpace(string(version)))
	t.Log(host)

	var rows []row
	phase := startPhase(t, a)
	time.Sleep(*idleFor)
	rows = append(rows, phase.end(t, a, fmt.Sprintf("idle for %s, 2 peers", *idleFor)))

	sent := 0
	for _, target := range targets {
		phase = startPhase(t, a)
		// Each address is reported by both publishers: two verdicts on A.
		want := (target + 1) / 2
		if err := sendLoad(publishers, sent, want, *rate); err != nil {
			t.Fatal(err)
		}
		awaitAccepted(t, a, phase.accepted+float64(2*(want-sent)))
		sent = want
		rows = append(rows, phase.end(t, a, fmt.Sprintf("receiving %g verdicts/s", 2**rate)))
	}

	phase = startPhase(t, a)
	time.Sleep(*restFor)
	rows = append(rows, phase.end(t, a, fmt.Sprintf("at rest for %s", *restFor)))

	report := host + "\n\n" + table(rows)
	t.Log("\n" + report)
	if *outFile != "" {
		if err := os.WriteFile(*outFile, []byte(report), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// parseTargets parses -resources.verdicts.
func parseTargets(s string) ([]int, error) {
	var out []int
	for _, f := range strings.Split(s, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil || n < 2 || (len(out) > 0 && n <= out[len(out)-1]) {
			return nil, fmt.Errorf("-resources.verdicts %q: want ascending numbers of at least 2", s)
		}
		out = append(out, n)
	}
	return out, nil
}

// startMesh starts a node per name in order, each bootstrapping to the
// nodes started before it, and waits until every node is connected to all
// others.
func startMesh(t *testing.T, obied string, names ...string) []*node {
	t.Helper()
	var nodes []*node
	for _, name := range names {
		nodes = append(nodes, newNode(t, name))
	}
	for i, n := range nodes {
		writeConfig(t, n, nodes, nodes[:i])
		n.start(t, obied)
	}
	for _, n := range nodes {
		n.awaitPeers(t, len(nodes)-1)
	}
	return nodes
}

// newNode creates the node's directory and key, and picks its ports.
func newNode(t *testing.T, name string) *node {
	t.Helper()
	// Unix socket paths are limited to about 100 bytes; t.TempDir can exceed that.
	dir, err := os.MkdirTemp("", "obie-resources-"+name+"-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("node %s kept in %s for inspection", name, dir)
			return
		}
		_ = os.RemoveAll(dir)
	})
	key, err := identity.Create(filepath.Join(dir, "state"), false)
	if err != nil {
		t.Fatal(err)
	}
	return &node{
		name: name, dir: dir, peerID: key.PeerID(),
		meshPort: freePort(t), metrics: fmt.Sprintf("127.0.0.1:%d", freePort(t)),
		client: admin.NewClient(filepath.Join(dir, "obie.sock")),
	}
}

// freePort returns a TCP port on 127.0.0.1 that was free a moment ago.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

// writeConfig writes n's configuration: it trusts every other node of
// all, bootstraps to those in bootstrap, and admits the load's rate.
func writeConfig(t *testing.T, n *node, all, bootstrap []*node) {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "node:\n  state_dir: %s\n  mode: enforce\n", filepath.Join(n.dir, "state"))
	// The socket stays in the test user's own group.
	fmt.Fprintf(&b, "admin:\n  socket: %s\n  socket_group: obie-resources-no-such-group\n", filepath.Join(n.dir, "obie.sock"))
	fmt.Fprintf(&b, "mesh:\n  listen: [/ip4/127.0.0.1/tcp/%d]\n  bootstrap:\n", n.meshPort)
	for _, peer := range bootstrap {
		fmt.Fprintf(&b, "    - /ip4/127.0.0.1/tcp/%d/p2p/%s\n", peer.meshPort, peer.peerID)
	}
	// Each publisher sends -resources.rate events per second; a peer also
	// forwards the other's.
	fmt.Fprintf(&b, "  rate_limit:\n    publisher: {events_per_second: %g, burst: %d}\n    peer: {events_per_second: %g, burst: %d}\n",
		2**rate, int(10**rate), 4**rate, int(20**rate))
	b.WriteString("trust:\n  publishers:\n")
	for _, peer := range all {
		if peer != n {
			fmt.Fprintf(&b, "    - {peer_id: %s, name: %s, weight: %.1f}\n", peer.peerID, peer.name, weight)
		}
	}
	fmt.Fprintf(&b, "decision:\n  threshold: %.1f\n  quorum: %d\n", threshold, quorum)
	b.WriteString("enforce:\n  backend: dryrun\n")
	fmt.Fprintf(&b, "metrics:\n  listen: %s\n", n.metrics)
	fmt.Fprintf(&b, "audit:\n  path: %s\n", filepath.Join(n.dir, "audit.jsonl"))
	if err := os.WriteFile(filepath.Join(n.dir, "obie.yaml"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

// start runs obied on the node's configuration, with its log in the
// node's directory, until the test ends.
func (n *node) start(t *testing.T, obied string) {
	t.Helper()
	logs, err := os.Create(filepath.Join(n.dir, "obied.log"))
	if err != nil {
		t.Fatal(err)
	}
	n.cmd = exec.Command(obied, "--config", filepath.Join(n.dir, "obie.yaml")) // #nosec G204 -- the tester chooses the binary.
	n.cmd.Env = append(os.Environ(), "GOMAXPROCS="+strconv.Itoa(*gomaxprocs))
	n.cmd.Stdout, n.cmd.Stderr = logs, logs
	if err := n.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	n.done = make(chan error, 1)
	go func() { n.done <- n.cmd.Wait(); _ = logs.Close() }()
	t.Cleanup(func() { n.stop(t) })
	err = poll(startBound, func(ctx context.Context) error {
		select {
		case err := <-n.done:
			n.done <- err
			return fmt.Errorf("obied exited: %w", err)
		default:
		}
		_, err := n.client.Status(ctx)
		return err
	})
	if err != nil {
		t.Fatalf("node %s did not start: %v\n%s", n.name, err, n.logTail())
	}
}

// stop stops obied gracefully, or kills it after startBound.
func (n *node) stop(t *testing.T) {
	if n.cmd == nil || n.cmd.Process == nil {
		return
	}
	_ = n.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-n.done:
	case <-time.After(startBound):
		t.Errorf("node %s did not stop within %s; killed", n.name, startBound)
		_ = n.cmd.Process.Kill()
		<-n.done
	}
	if t.Failed() {
		t.Logf("node %s log:\n%s", n.name, n.logTail())
	}
}

// logTail returns the last lines of the node's log.
func (n *node) logTail() string {
	data, err := os.ReadFile(filepath.Join(n.dir, "obied.log"))
	if err != nil {
		return err.Error()
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	return strings.Join(lines[max(0, len(lines)-30):], "\n")
}

// awaitPeers waits until n is connected to want peers.
func (n *node) awaitPeers(t *testing.T, want int) {
	t.Helper()
	err := poll(meshBound, func(ctx context.Context) error {
		resp, err := n.client.Peers(ctx)
		if err == nil && len(resp.Peers) < want {
			err = fmt.Errorf("%d peers, want %d", len(resp.Peers), want)
		}
		return err
	})
	if err != nil {
		t.Fatalf("node %s: %v", n.name, err)
	}
}

// awaitMesh waits until events of every publisher reach a: GossipSub
// forwards only to peers whose subscription it has seen, so an event
// published earlier may never arrive. Each probe is a watch verdict, which
// blocks nothing, on a fresh address.
func awaitMesh(t *testing.T, a *node, publishers []*node) {
	t.Helper()
	probe := 0
	for _, p := range publishers {
		err := poll(meshBound, func(ctx context.Context) error {
			probe++
			ip := addrAt(probeIP, probe)
			confidence := 0.1
			if _, err := p.client.Report(ctx, admin.ReportRequest{
				IP: ip, Protocol: "ssh", Reason: "probe", Events: 1,
				Confidence: &confidence, Action: "watch", TTL: admin.TTL(time.Minute),
			}); err != nil {
				return err
			}
			return poll(time.Second, func(ctx context.Context) error {
				d, err := a.client.Explain(ctx, ip)
				if err != nil {
					return err
				}
				for _, c := range d.Publishers {
					if c.PeerID == p.peerID {
						return nil
					}
				}
				return fmt.Errorf("the probe on %s of node %s did not reach node %s", ip, p.name, a.name)
			})
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// sendLoad makes every publisher report the addresses from..to-1 at rate
// reports per second each, and returns the first error.
func sendLoad(publishers []*node, from, to int, rate float64) error {
	var wg sync.WaitGroup
	errs := make(chan error, len(publishers))
	for _, p := range publishers {
		wg.Go(func() { errs <- p.report(from, to, rate) })
	}
	wg.Wait()
	close(errs)
	var all []error
	for err := range errs {
		all = append(all, err)
	}
	return errors.Join(all...)
}

// report reports the addresses from..to-1 at rate reports per second.
func (n *node) report(from, to int, rate float64) error {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var first error
	slots := make(chan struct{}, reporters)
	ticker := time.NewTicker(time.Duration(float64(time.Second) / rate))
	defer ticker.Stop()
	for i := from; i < to; i++ {
		<-ticker.C
		ip := addrAt(firstIP, i)
		slots <- struct{}{}
		wg.Go(func() {
			defer func() { <-slots }()
			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			defer cancel()
			_, err := n.client.Report(ctx, admin.ReportRequest{
				IP: ip, Protocol: "ssh", Reason: "password_bruteforce", Events: 6,
				EvidenceLines: []string{"sshd[4711]: Failed password for root from " + ip + " port 52814 ssh2"},
			})
			if err != nil {
				mu.Lock()
				if first == nil {
					first = fmt.Errorf("node %s: report %s: %w", n.name, ip, err)
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return first
}

// addrAt returns the address i after base.
func addrAt(base netip.Addr, i int) string {
	b := base.As4()
	binary.BigEndian.PutUint32(b[:], binary.BigEndian.Uint32(b[:])+uint32(i)) // #nosec G115 -- i is far below 2^24.
	return netip.AddrFrom4(b).String()
}

// awaitAccepted waits until a accepted want events from its peers.
func awaitAccepted(t *testing.T, a *node, want float64) {
	t.Helper()
	err := poll(drainBound, func(ctx context.Context) error {
		got, err := a.metric(ctx, `obie_events_received_total{outcome="accepted"}`)
		if err == nil && got < want {
			err = fmt.Errorf("accepted %.0f events, want %.0f", got, want)
		}
		return err
	})
	if err != nil {
		t.Fatalf("node %s: %v", a.name, err)
	}
}

// phase is the start of a measured phase.
type phase struct {
	at       time.Time
	cpu      time.Duration
	accepted float64
}

// startPhase resets the measured node's peak RSS and notes its CPU time
// and accepted events.
func startPhase(t *testing.T, a *node) *phase {
	t.Helper()
	pid := a.cmd.Process.Pid
	// Writing 5 to clear_refs resets the peak RSS (VmHWM) to the current RSS.
	if err := os.WriteFile(fmt.Sprintf("/proc/%d/clear_refs", pid), []byte("5"), 0); err != nil {
		t.Fatal(err)
	}
	cpu, err := cpuTime(pid)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := metricNow(a, `obie_events_received_total{outcome="accepted"}`)
	if err != nil {
		t.Fatal(err)
	}
	return &phase{at: time.Now(), cpu: cpu, accepted: accepted}
}

// row is the measurement of one phase.
type row struct {
	phase            string
	verdicts, blocks float64
	rss, peak        uint64
	cpu              float64
	state, audit     uint64
}

// end measures the node at the end of the phase.
func (p *phase) end(t *testing.T, a *node, name string) row {
	t.Helper()
	pid := a.cmd.Process.Pid
	cpu, err := cpuTime(pid)
	if err != nil {
		t.Fatal(err)
	}
	r := row{phase: name, cpu: float64(cpu-p.cpu) / float64(time.Since(p.at))}
	if r.rss, r.peak, err = memory(pid); err != nil {
		t.Fatal(err)
	}
	if r.verdicts, err = metricNow(a, "obie_store_active_verdicts"); err != nil {
		t.Fatal(err)
	}
	if r.blocks, err = metricNow(a, `obie_decisions{state="block"}`); err != nil {
		t.Fatal(err)
	}
	if r.state, err = diskUsage(filepath.Join(a.dir, "state")); err != nil {
		t.Fatal(err)
	}
	if r.audit, err = diskUsage(filepath.Join(a.dir, "audit.jsonl")); err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %.0f verdicts, %.0f blocks, RSS %.0f MiB (peak %.0f MiB), CPU %.1f %% of one CPU, state %.0f MiB, audit log %.1f MiB",
		r.phase, r.verdicts, r.blocks, mib(r.rss), mib(r.peak), 100*r.cpu, mib(r.state), mib(r.audit))
	return r
}

// cpuTime returns the user and system time of every thread of pid.
func cpuTime(pid int) (time.Duration, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	// The command name, field 2, may hold spaces and parentheses; the
	// fields after it start behind the last ')'. utime and stime are
	// fields 14 and 15.
	s := string(data)
	fields := strings.Fields(s[strings.LastIndexByte(s, ')')+1:])
	if len(fields) < 13 {
		return 0, fmt.Errorf("/proc/%d/stat: %d fields", pid, len(fields))
	}
	var ticks int64
	for _, f := range fields[11:13] {
		n, err := strconv.ParseInt(f, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("/proc/%d/stat: %w", pid, err)
		}
		ticks += n
	}
	return time.Duration(ticks) * time.Second / userHZ, nil
}

// memory returns the resident set size of pid and its peak since the
// last reset, in bytes.
func memory(pid int) (rss, peak uint64, err error) {
	f, err := os.Open(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, value, _ := strings.Cut(sc.Text(), ":")
		var into *uint64
		switch key {
		case "VmRSS":
			into = &rss
		case "VmHWM":
			into = &peak
		default:
			continue
		}
		kib, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(value), " kB"), 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("/proc/%d/status %s: %w", pid, key, err)
		}
		*into = kib << 10
	}
	if err := sc.Err(); err != nil {
		return 0, 0, err
	}
	if rss == 0 || peak == 0 {
		return 0, 0, fmt.Errorf("/proc/%d/status has no VmRSS or VmHWM", pid)
	}
	return rss, peak, nil
}

// diskUsage returns the bytes the regular files below path occupy on disk,
// like du: sparse files count only with their allocated blocks.
func diskUsage(path string) (uint64, error) {
	var total uint64
	err := filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			total += uint64(st.Blocks) * 512 // #nosec G115 -- a block count is never negative.
		}
		return nil
	})
	return total, err
}

// metricNow returns a sample of the node's /metrics.
func metricNow(n *node, sample string) (float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	return n.metric(ctx, sample)
}

// metric returns the value of the sample name (with labels) on the node's
// /metrics.
func (n *node) metric(ctx context.Context, sample string) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+n.metrics+"/metrics", nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if value, ok := strings.CutPrefix(sc.Text(), sample+" "); ok {
			return strconv.ParseFloat(value, 64)
		}
	}
	if err := sc.Err(); err != nil {
		return 0, err
	}
	// A gauge with labels appears only once it was set.
	return 0, nil
}

// poll calls check until it succeeds or bound elapsed; it returns the
// last error then.
func poll(bound time.Duration, check func(ctx context.Context) error) error {
	deadline := time.Now().Add(bound)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		err := check(ctx)
		cancel()
		if err == nil || time.Now().After(deadline) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// describeHost describes the machine and the measured build.
func describeHost(version string) string {
	cpu := "unknown CPU"
	if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if name, ok := strings.CutPrefix(line, "model name"); ok {
				cpu = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), ":"))
				break
			}
		}
	}
	kernel, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	return fmt.Sprintf("%s on %s, Linux %s (%s), %d CPUs; each node GOMAXPROCS=%d; %s",
		version, cpu, strings.TrimSpace(string(kernel)), runtime.GOARCH, runtime.NumCPU(), *gomaxprocs,
		time.Now().Format(time.DateOnly))
}

// table formats the rows as a Markdown table.
func table(rows []row) string {
	var b strings.Builder
	b.WriteString("| Phase | Verdicts held | Blocks | RSS | Peak RSS | CPU (share of one CPU) | State directory | Audit log |\n")
	b.WriteString("|---|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| %s | %.0f | %.0f | %.0f MiB | %.0f MiB | %.1f %% | %.0f MiB | %.1f MiB |\n",
			r.phase, r.verdicts, r.blocks, mib(r.rss), mib(r.peak), 100*r.cpu, mib(r.state), mib(r.audit))
	}
	return b.String()
}

func mib(b uint64) float64 { return float64(b) / (1 << 20) }
