package simtrust

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"slices"
	"time"
)

// TraceFormat names the trace file format (ADR 0034).
const TraceFormat = "obie-trust-trace/1"

// maxTraceLine bounds a line of a trace file.
const maxTraceLine = 64 << 10

// Class is the ground truth of an address.
type Class string

// Address classes.
const (
	// ClassAttacker: an address that attacked an operator.
	ClassAttacker Class = "attacker"
	// ClassCDN: an edge address of a CDN, in a published range.
	ClassCDN Class = "cdn"
	// ClassCrawler: a search engine's crawler, in a published range.
	ClassCrawler Class = "crawler"
	// ClassCustomer: a customer of an operator.
	ClassCustomer Class = "customer"
	// ClassNAT: a shared NAT address, which carries legitimate users and
	// sometimes attackers.
	ClassNAT Class = "nat"
)

// classes are the valid classes, attackers first.
var classes = []Class{ClassAttacker, ClassCDN, ClassCrawler, ClassCustomer, ClassNAT}

// benignClasses are the classes the network must protect.
var benignClasses = classes[1:]

// Benign reports whether the address is one the network must protect.
func (c Class) Benign() bool {
	return c != ClassAttacker
}

// Operator runs a node and Fail2Ban; operator 0 of a trace is the
// observer.
type Operator struct {
	Name    string
	ASN     uint32
	Bantime time.Duration
}

// Address is an address of a trace with its ground truth.
type Address struct {
	Addr  netip.Addr
	Class Class
	ASN   uint32
}

// Range is a published benign range: one anyone can download.
type Range struct {
	Prefix netip.Prefix
	Class  Class
}

// Observation is a Fail2Ban ban: operator Operator banned Addr at At.
type Observation struct {
	At       time.Time
	Operator int
	Addr     netip.Addr
}

// Trace is what a run replays (ADR 0034): the operators, every address
// with its class, the published benign ranges and the operators' bans,
// ordered by time.
type Trace struct {
	// Source says where the trace comes from.
	Source string
	// Start is the beginning of the trace; it lasts Hours.
	Start        time.Time
	Hours        int
	Operators    []Operator
	Addresses    []Address
	Published    []Range
	Observations []Observation
}

// ShiftedTo returns the trace moved later by whole weeks, so that it
// starts at from or after it, with its weekdays and times of day kept; t
// itself if it does.
func (t *Trace) ShiftedTo(from time.Time) *Trace {
	if !t.Start.Before(from) {
		return t
	}
	const week = 7 * 24 * time.Hour
	by := (from.Sub(t.Start) + week - 1) / week * week
	out := *t
	out.Start = t.Start.Add(by)
	out.Observations = make([]Observation, len(t.Observations))
	for i, o := range t.Observations {
		o.At = o.At.Add(by)
		out.Observations[i] = o
	}
	return &out
}

// End returns the end of the trace.
func (t *Trace) End() time.Time {
	return t.Start.Add(time.Duration(t.Hours) * time.Hour)
}

// Validate checks that the trace can be replayed: an observer and at
// least one publisher, known classes, every observed address declared
// once, observations of known operators in order within the trace.
func (t *Trace) Validate() error {
	var errs []error
	if len(t.Operators) < 2 {
		errs = append(errs, fmt.Errorf("%d operators, want the observer and at least one publisher", len(t.Operators)))
	}
	if t.Hours < 1 || t.Start.IsZero() {
		errs = append(errs, fmt.Errorf("start %s, %d hours; want a start and at least 1 hour", t.Start, t.Hours))
	}
	for i, o := range t.Operators {
		if o.Bantime < time.Minute {
			errs = append(errs, fmt.Errorf("operator %d (%s): bantime %s, want at least 1m", i, o.Name, o.Bantime))
		}
	}
	declared := make(map[netip.Addr]bool, len(t.Addresses))
	for _, a := range t.Addresses {
		if !a.Addr.IsValid() || !slices.Contains(classes, a.Class) {
			errs = append(errs, fmt.Errorf("address %s: class %q, want one of %q", a.Addr, a.Class, classes))
		}
		if declared[a.Addr] {
			errs = append(errs, fmt.Errorf("address %s declared twice", a.Addr))
		}
		declared[a.Addr] = true
	}
	for _, r := range t.Published {
		if !r.Prefix.IsValid() || !r.Class.Benign() || !slices.Contains(classes, r.Class) {
			errs = append(errs, fmt.Errorf("published range %s: class %q, want a benign class", r.Prefix, r.Class))
		}
	}
	end := t.End()
	for i, o := range t.Observations {
		switch {
		case o.Operator < 0 || o.Operator >= len(t.Operators):
			errs = append(errs, fmt.Errorf("observation %d: unknown operator %d", i, o.Operator))
		case !declared[o.Addr]:
			errs = append(errs, fmt.Errorf("observation %d: address %s is not declared", i, o.Addr))
		case o.At.Before(t.Start) || !o.At.Before(end):
			errs = append(errs, fmt.Errorf("observation %d: %s outside the trace", i, o.At.Format(time.RFC3339)))
		case i > 0 && o.At.Before(t.Observations[i-1].At):
			errs = append(errs, fmt.Errorf("observation %d: %s before the one before it", i, o.At.Format(time.RFC3339)))
		}
		if len(errs) > 20 {
			errs = append(errs, errors.New("more errors omitted"))
			break
		}
	}
	return errors.Join(errs...)
}

// traceLine is one line of a trace file; Kind says which fields it uses.
type traceLine struct {
	Kind string `json:"kind"`
	// kind "trace"
	Format string    `json:"format,omitempty"`
	Source string    `json:"source,omitempty"`
	Start  time.Time `json:"start,omitzero"`
	Hours  int       `json:"hours,omitempty"`
	// kind "operator"
	Name           string `json:"name,omitempty"`
	BantimeSeconds int64  `json:"bantime_seconds,omitempty"`
	// kinds "operator", "address"
	ASN uint32 `json:"asn,omitempty"`
	// kinds "address", "range"
	Class Class `json:"class,omitempty"`
	// kinds "address", "ban"
	Addr netip.Addr `json:"addr,omitzero"`
	// kind "range"
	Range netip.Prefix `json:"range,omitzero"`
	// kind "ban"
	At       time.Time `json:"at,omitzero"`
	Operator *int      `json:"operator,omitempty"`
}

// WriteTrace writes t as JSON lines: its header, then its operators,
// addresses, published ranges and observations.
func WriteTrace(w io.Writer, t *Trace) error {
	bw := bufio.NewWriter(w)
	enc := json.NewEncoder(bw)
	lines := []traceLine{{Kind: "trace", Format: TraceFormat, Source: t.Source, Start: t.Start.UTC(), Hours: t.Hours}}
	for _, o := range t.Operators {
		lines = append(lines, traceLine{Kind: "operator", Name: o.Name, ASN: o.ASN, BantimeSeconds: int64(o.Bantime / time.Second)})
	}
	for _, a := range t.Addresses {
		lines = append(lines, traceLine{Kind: "address", Addr: a.Addr, Class: a.Class, ASN: a.ASN})
	}
	for _, r := range t.Published {
		lines = append(lines, traceLine{Kind: "range", Range: r.Prefix, Class: r.Class})
	}
	for _, line := range lines {
		if err := enc.Encode(line); err != nil {
			return err
		}
	}
	for _, o := range t.Observations {
		op := o.Operator
		if err := enc.Encode(traceLine{Kind: "ban", At: o.At.UTC(), Operator: &op, Addr: o.Addr}); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// ReadTrace reads and validates a trace written by WriteTrace.
func ReadTrace(r io.Reader) (*Trace, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 4096), maxTraceLine)
	t := &Trace{}
	header := false
	for n := 1; sc.Scan(); n++ {
		var line traceLine
		dec := json.NewDecoder(bytes.NewReader(sc.Bytes()))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&line); err != nil {
			return nil, fmt.Errorf("trace line %d: %w", n, err)
		}
		if !header && line.Kind != "trace" {
			return nil, fmt.Errorf("trace line %d: want the %q header first", n, "trace")
		}
		switch line.Kind {
		case "trace":
			if header || line.Format != TraceFormat {
				return nil, fmt.Errorf("trace line %d: format %q, want one header of %q", n, line.Format, TraceFormat)
			}
			header = true
			t.Source, t.Start, t.Hours = line.Source, line.Start, line.Hours
		case "operator":
			t.Operators = append(t.Operators, Operator{Name: line.Name, ASN: line.ASN, Bantime: time.Duration(line.BantimeSeconds) * time.Second})
		case "address":
			t.Addresses = append(t.Addresses, Address{Addr: line.Addr, Class: line.Class, ASN: line.ASN})
		case "range":
			t.Published = append(t.Published, Range{Prefix: line.Range, Class: line.Class})
		case "ban":
			if line.Operator == nil {
				return nil, fmt.Errorf("trace line %d: ban without operator", n)
			}
			t.Observations = append(t.Observations, Observation{At: line.At, Operator: *line.Operator, Addr: line.Addr})
		default:
			return nil, fmt.Errorf("trace line %d: unknown kind %q", n, line.Kind)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read trace: %w", err)
	}
	if !header {
		return nil, errors.New("empty trace")
	}
	return t, t.Validate()
}

// sortObservations orders observations by time, operator and address.
func sortObservations(obs []Observation) {
	slices.SortFunc(obs, func(a, b Observation) int {
		return cmp.Or(a.At.Compare(b.At), cmp.Compare(a.Operator, b.Operator), a.Addr.Compare(b.Addr))
	})
}

// sortAddresses orders addresses by address.
func sortAddresses(addrs []Address) {
	slices.SortFunc(addrs, func(a, b Address) int { return a.Addr.Compare(b.Addr) })
}
