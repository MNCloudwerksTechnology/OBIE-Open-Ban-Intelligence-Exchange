package simtrust

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/netip"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
)

// banLine matches a ban in fail2ban.log; "Restore Ban" lines after a
// restart repeat earlier bans and do not match.
var banLine = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}),\d+ fail2ban\.actions\s*\[\d+\]:\s*NOTICE\s+\[[^\]]+\]\s+Ban\s+(\S+)\s*$`)

// fail2banTime is the time layout of fail2ban.log.
const fail2banTime = "2006-01-02 15:04:05"

// OperatorLog is one operator's Fail2Ban log.
type OperatorLog struct {
	Operator Operator
	Log      io.Reader
}

// BenignRange is a range of the benign-ranges file: every address in it
// has its class.
type BenignRange struct {
	Prefix netip.Prefix
	Class  Class
}

// ReadBenignRanges reads a benign-ranges file: one CIDR range and a
// class (cdn, crawler, customer or nat) per line; blank lines and "#"
// comments are ignored.
func ReadBenignRanges(r io.Reader) ([]BenignRange, error) {
	var out []BenignRange
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line, _, _ := strings.Cut(sc.Text(), "#")
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 2 {
			return nil, fmt.Errorf("benign ranges line %d: want a CIDR range and a class", n)
		}
		p, err := netip.ParsePrefix(fields[0])
		class := Class(fields[1])
		if err != nil || !class.Benign() || !slices.Contains(classes, class) {
			return nil, fmt.Errorf("benign ranges line %d: want a CIDR range and one of cdn, crawler, customer, nat", n)
		}
		out = append(out, BenignRange{Prefix: p.Masked(), Class: class})
	}
	return out, sc.Err()
}

// ImportFail2Ban turns the operators' Fail2Ban logs into a trace: every
// ban of fail2ban.actions, its time read in loc, its address classed by
// the benign ranges and then pseudonymized. The CDN and crawler ranges
// become the trace's published ranges. Classes the logs lack are filled
// with the synthetic population of p, drawn from seed (ADR 0034). The
// first log is the observer's.
func ImportFail2Ban(logs []OperatorLog, benign []BenignRange, pz Pseudonymizer, loc *time.Location, p WorldParams, seed uint64) (*Trace, error) {
	t := &Trace{Source: fmt.Sprintf("Fail2Ban logs of %d operators, pseudonymized", len(logs))}
	classOf := map[netip.Addr]Class{}
	for i, l := range logs {
		t.Operators = append(t.Operators, l.Operator)
		if err := readBans(l.Log, loc, func(at time.Time, a netip.Addr) {
			class := ClassAttacker
			if j := slices.IndexFunc(benign, func(r BenignRange) bool { return r.Prefix.Contains(a) }); j >= 0 {
				class = benign[j].Class
			}
			pseudo := pz.Addr(a)
			classOf[pseudo] = class
			t.Observations = append(t.Observations, Observation{At: at.UTC(), Operator: i, Addr: pseudo})
		}); err != nil {
			return nil, fmt.Errorf("log of %s: %w", l.Operator.Name, err)
		}
	}
	if len(t.Observations) == 0 {
		return nil, errors.New("the logs hold no ban")
	}
	sortObservations(t.Observations)
	t.Start = t.Observations[0].At.Truncate(time.Hour)
	t.Hours = int(t.Observations[len(t.Observations)-1].At.Sub(t.Start)/time.Hour) + 1
	for a, class := range classOf {
		t.Addresses = append(t.Addresses, Address{Addr: a, Class: class})
	}
	for _, r := range benign {
		if r.Class == ClassCDN || r.Class == ClassCrawler {
			t.Published = append(t.Published, Range{Prefix: pz.Prefix(r.Prefix), Class: r.Class})
		}
	}
	complete(t, p, seed)
	return t, t.Validate()
}

// readBans calls ban for every ban in the fail2ban.log r.
func readBans(r io.Reader, loc *time.Location, ban func(time.Time, netip.Addr)) error {
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		m := banLine.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		at, err := time.ParseInLocation(fail2banTime, m[1], loc)
		if err != nil {
			return fmt.Errorf("line %d: %w", n, err)
		}
		a, err := netip.ParseAddr(m[2])
		if err != nil {
			return fmt.Errorf("line %d: %w", n, err)
		}
		ban(at, a)
	}
	return sc.Err()
}

// completionASNBase is the index of the first ASN that complete
// allocates, far from those of a synthetic world.
const completionASNBase = 0xe000

// complete adds the benign population of p for every class the trace has
// no address of: addresses inside its published ranges of the class, or
// in synthetic ASNs.
func complete(t *Trace, p WorldParams, seed uint64) {
	g := newWorldGen(p, seed, completionASNBase, t)
	count := map[Class]int{}
	for _, a := range t.Addresses {
		count[a.Class]++
	}
	fill := func(class Class, owners, ranges, hosts int) {
		if count[class] > 0 {
			return
		}
		var published []netip.Prefix
		for _, r := range t.Published {
			if r.Class == class {
				published = append(published, r.Prefix)
			}
		}
		if len(published) == 0 {
			g.addPublished(class, owners, ranges, hosts)
			return
		}
		// Small ranges hold fewer addresses than wanted: the draws are
		// bounded, and the class gets what fits.
		want := owners * ranges * hosts
		for tries := 0; want > 0 && tries < 64*owners*ranges*hosts; tries++ {
			if a, ok := g.randomIn(published[g.rng.IntN(len(published))]); ok {
				g.add(a, class, 0)
				want--
			}
		}
	}
	fill(ClassCDN, p.CDNs, p.RangesPerCDN, p.EdgesPerRange)
	fill(ClassCrawler, p.Crawlers, p.RangesPerCrawler, p.CrawlersPerRange)
	if count[ClassCustomer] == 0 {
		g.addHosts(ClassCustomer, g.allocASNs(p.ResidentialASNs), p.Customers)
	}
	if count[ClassNAT] == 0 {
		g.addHosts(ClassNAT, g.allocASNs(p.CarrierASNs), p.NATs)
	}
	sortAddresses(t.Addresses)
}

// randomIn draws a random address inside the IPv6 range r, as every
// pseudonymized range is; ok is false if it is in use or the range's own
// address.
func (g *worldGen) randomIn(r netip.Prefix) (netip.Addr, bool) {
	b := r.Addr().As16()
	for i := r.Bits(); i < 128; i++ {
		b[i/8] |= byte(g.rng.IntN(2)) << (7 - i%8) // #nosec G115 -- 0 or 1.
	}
	a := netip.AddrFrom16(b)
	return a, !g.used[a] && a != r.Addr()
}

// RunImport is the trace-import command: it reads the operators' Fail2Ban
// logs named by args and writes the pseudonymized trace (ADR 0034).
func RunImport(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("trace-import", flag.ContinueOnError)
	fs.SetOutput(stderr)
	keyFile := fs.String("key", "", "file holding the pseudonymization key the contributors share (at least 16 bytes)")
	benignFile := fs.String("benign", "", "benign-ranges file: a CIDR range and its class (cdn, crawler, customer, nat) per line")
	tz := fs.String("tz", "UTC", "time zone of the log times")
	seed := fs.Uint64("seed", 1, "seed of the synthetic population added for the classes the logs lack")
	out := fs.String("o", "", "trace file to write")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: trace-import -key FILE -benign FILE -o TRACE [-tz ZONE] NAME:BANTIME:LOG ... (the first is the observer)")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *keyFile == "" || *benignFile == "" || *out == "" || fs.NArg() < 2 {
		fs.Usage()
		return errors.New("-key, -benign, -o and the logs of at least two operators are required")
	}
	key, err := os.ReadFile(*keyFile) // #nosec G304 -- the operator names the key file.
	if err != nil {
		return err
	}
	pz, err := NewPseudonymizer(key)
	if err != nil {
		return err
	}
	loc, err := time.LoadLocation(*tz)
	if err != nil {
		return err
	}
	benign, err := readFileWith(*benignFile, ReadBenignRanges)
	if err != nil {
		return err
	}
	var logs []OperatorLog
	for _, arg := range fs.Args() {
		name, rest, _ := strings.Cut(arg, ":")
		bantime, path, ok := strings.Cut(rest, ":")
		d, err := time.ParseDuration(bantime)
		if !ok || name == "" || err != nil {
			return fmt.Errorf("%q: want NAME:BANTIME:LOG, e.g. op00:1h:/var/log/fail2ban.log", arg)
		}
		f, err := os.Open(path) // #nosec G304 -- the operator names the logs to import.
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		logs = append(logs, OperatorLog{Operator: Operator{Name: name, Bantime: d}, Log: f})
	}
	t, err := ImportFail2Ban(logs, benign, pz, loc, DefaultWorld(), *seed)
	if err != nil {
		return err
	}
	return writeFileWith(*out, t, WriteTrace)
}

// readFileWith reads the file at path with read.
func readFileWith[T any](path string, read func(io.Reader) (T, error)) (T, error) {
	f, err := os.Open(path) // #nosec G304 -- the operator names the file.
	if err != nil {
		var zero T
		return zero, err
	}
	defer func() { _ = f.Close() }()
	return read(f)
}

// writeFileWith writes v to a new file at path with write.
func writeFileWith[T any](path string, v T, write func(io.Writer, T) error) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) // #nosec G304 -- the operator names the file.
	if err != nil {
		return err
	}
	if err := write(f, v); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
