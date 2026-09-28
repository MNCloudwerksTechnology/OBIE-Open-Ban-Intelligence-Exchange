//go:build linux

package nft

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"slices"
	"time"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"

	"github.com/MNCloudwerksTechnology/obie/internal/enforce"
)

// Backend is the nftables enforcement backend. The Reconciler never calls
// it concurrently; Teardown may run from another process.
type Backend struct {
	log  *slog.Logger
	opts Options
}

var _ enforce.Enforcer = (*Backend)(nil)

// New returns the backend; it touches nothing before Setup.
func New(opts Options, log *slog.Logger) *Backend {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Backend{log: log, opts: opts}
}

// family is one address family of the table: its set and the protocol
// match of its rule.
type family struct {
	set     string
	keyType nftables.SetDatatype
	nfproto byte
	// offset and length of the source address in the network header.
	offset, length uint32
}

var families = []family{
	{set: SetV4, keyType: nftables.TypeIPAddr, nfproto: unix.NFPROTO_IPV4, offset: 12, length: 4},
	{set: SetV6, keyType: nftables.TypeIP6Addr, nfproto: unix.NFPROTO_IPV6, offset: 8, length: 16},
}

func table() *nftables.Table {
	return &nftables.Table{Name: Table, Family: nftables.TableFamilyINet}
}

// chains returns the chains the table must have.
func (b *Backend) chains(t *nftables.Table) []*nftables.Chain {
	accept := nftables.ChainPolicyAccept
	out := []*nftables.Chain{{Name: ChainInput, Table: t, Type: nftables.ChainTypeFilter,
		Hooknum: nftables.ChainHookInput, Priority: nftables.ChainPriorityRef(Priority), Policy: &accept}}
	if b.opts.Forward {
		out = append(out, &nftables.Chain{Name: ChainForward, Table: t, Type: nftables.ChainTypeFilter,
			Hooknum: nftables.ChainHookForward, Priority: nftables.ChainPriorityRef(Priority), Policy: &accept})
	}
	return out
}

// newSet returns the set of f.
func newSet(t *nftables.Table, f family) *nftables.Set {
	return &nftables.Set{Table: t, Name: f.set, KeyType: f.keyType, Interval: true, HasTimeout: true}
}

// ruleExprs is `<ip|ip6> saddr @<set> counter drop`.
func ruleExprs(f family, setID uint32) []expr.Any {
	return []expr.Any{
		&expr.Meta{Key: expr.MetaKeyNFPROTO, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{f.nfproto}},
		&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: f.offset, Len: f.length},
		&expr.Lookup{SourceRegister: 1, SetName: f.set, SetID: setID},
		&expr.Counter{},
		&expr.Verdict{Kind: expr.VerdictDrop},
	}
}

// Setup creates the table, or keeps it if its structure is as expected;
// otherwise it replaces it, deleting and recreating it in one transaction.
// Only the table inet obie is ever touched.
func (b *Backend) Setup(ctx context.Context) error {
	conn, err := newConn(ctx)
	if err != nil {
		return err
	}
	existing, err := findTable(conn)
	if err != nil {
		return err
	}
	if existing != nil {
		ok, err := b.matches(conn, existing)
		if err != nil {
			return err
		}
		if ok {
			_, err := b.entries(conn, existing)
			if err == nil {
				b.log.Info("nftables: reusing table inet " + Table)
				return nil
			}
			if !errors.Is(err, errForeign) {
				return err
			}
			b.log.Warn("nftables: table inet "+Table+" holds elements obied did not add; replacing it", "error", err)
		} else {
			b.log.Warn("nftables: table inet " + Table + " has an unexpected structure; replacing it")
		}
		conn.DelTable(existing)
	}
	t := conn.AddTable(table())
	sets := make([]*nftables.Set, len(families))
	for i, f := range families {
		sets[i] = newSet(t, f)
		if err := conn.AddSet(sets[i], nil); err != nil {
			return fmt.Errorf("add set %s: %w", f.set, err)
		}
	}
	for _, c := range b.chains(t) {
		conn.AddChain(c)
		for i, f := range families {
			conn.AddRule(&nftables.Rule{Table: t, Chain: c, Exprs: ruleExprs(f, sets[i].ID)})
		}
	}
	if err := flush(conn); err != nil {
		return fmt.Errorf("create table inet %s: %w", Table, err)
	}
	b.log.Info("nftables: table inet "+Table+" created", "forward", b.opts.Forward)
	return nil
}

// matches reports whether t has exactly the sets, chains and rules Setup
// creates.
func (b *Backend) matches(conn *nftables.Conn, t *nftables.Table) (bool, error) {
	if t.Flags != 0 { // e.g. dormant: its chains filter nothing
		return false, nil
	}
	sets, err := conn.GetSets(t)
	if err != nil {
		return false, fmt.Errorf("list the sets: %w", err)
	}
	if len(sets) != len(families) {
		return false, nil
	}
	for _, f := range families {
		i := slices.IndexFunc(sets, func(s *nftables.Set) bool { return s.Name == f.set })
		if i < 0 {
			return false, nil
		}
		s := sets[i]
		if s.KeyType.Name != f.keyType.Name || !s.Interval || !s.HasTimeout || s.IsMap || s.Constant || s.Anonymous {
			return false, nil
		}
	}
	all, err := conn.ListChainsOfTableFamily(nftables.TableFamilyINet)
	if err != nil {
		return false, fmt.Errorf("list the chains: %w", err)
	}
	var have []*nftables.Chain
	for _, c := range all {
		if c.Table.Name == Table {
			have = append(have, c)
		}
	}
	want := b.chains(t)
	if len(have) != len(want) {
		return false, nil
	}
	for _, w := range want {
		i := slices.IndexFunc(have, func(c *nftables.Chain) bool { return c.Name == w.Name })
		if i < 0 || !sameChain(have[i], w) {
			return false, nil
		}
		rules, err := conn.GetRules(t, have[i])
		if err != nil {
			return false, fmt.Errorf("list the rules of chain %s: %w", w.Name, err)
		}
		if len(rules) != len(families) {
			return false, nil
		}
		for j, f := range families {
			if !reflect.DeepEqual(normalize(rules[j].Exprs), normalize(ruleExprs(f, 0))) {
				return false, nil
			}
		}
	}
	return true, nil
}

func sameChain(a, b *nftables.Chain) bool {
	return a.Type == b.Type && a.Hooknum != nil && *a.Hooknum == *b.Hooknum &&
		a.Priority != nil && *a.Priority == *b.Priority && a.Policy != nil && *a.Policy == *b.Policy
}

// normalize zeroes the volatile fields of a rule's expressions: counter
// values and set IDs.
func normalize(exprs []expr.Any) []expr.Any {
	out := make([]expr.Any, len(exprs))
	for i, e := range exprs {
		switch e := e.(type) {
		case *expr.Counter:
			out[i] = &expr.Counter{}
		case *expr.Lookup:
			l := *e
			l.SetID = 0
			out[i] = &l
		default:
			out[i] = e
		}
	}
	return out
}

// List returns the unexpired entries of both sets. It fails with ErrDrift
// if the table is missing, is not as Setup made it or holds elements
// obied did not add; the next Setup restores it.
func (b *Backend) List(ctx context.Context) ([]enforce.Entry, error) {
	conn, err := newConn(ctx)
	if err != nil {
		return nil, err
	}
	t, err := findTable(conn)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, ErrDrift
	}
	ok, err := b.matches(conn, t)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrDrift
	}
	entries, err := b.entries(conn, t)
	if errors.Is(err, errForeign) {
		return nil, fmt.Errorf("%w: %w", ErrDrift, err)
	}
	return entries, err
}

// entries reads the entries of both sets of t.
func (b *Backend) entries(conn *nftables.Conn, t *nftables.Table) ([]enforce.Entry, error) {
	now := b.opts.Now()
	var out []enforce.Entry
	for _, f := range families {
		elems, err := conn.GetSetElements(newSet(t, f))
		if err != nil {
			return nil, fmt.Errorf("list set %s: %w", f.set, err)
		}
		mapped := make([]element, len(elems))
		for i, el := range elems {
			mapped[i] = element{key: el.Key, end: el.IntervalEnd, comment: el.Comment, timeout: el.Timeout, expires: el.Expires}
		}
		entries, err := fromElements(mapped, now, now.Add(never))
		if err != nil {
			return nil, fmt.Errorf("set %s: %w", f.set, err)
		}
		out = append(out, entries...)
	}
	return out, nil
}

// chunk is one netlink message: elements added to or deleted from a set.
type chunk struct {
	set   *nftables.Set
	del   bool
	elems []nftables.SetElement
	size  int
}

// Apply deletes remove, then adds add with their remaining timeouts, in
// one netlink transaction of messages with at most MaxChunk elements. Only
// if the socket buffers cannot hold the transaction or its acks (obied
// lacks CAP_NET_ADMIN in the initial user namespace to enlarge them) is
// it split; the next pass corrects a partial result.
func (b *Backend) Apply(ctx context.Context, add, remove []enforce.Entry) error {
	chunks, err := b.chunks(add, remove)
	if err != nil || len(chunks) == 0 {
		return err
	}
	total := batchOverhead
	for _, c := range chunks {
		total += c.size
	}
	var bufs buffers
	conn, err := newConn(ctx, nftables.AsLasting(), nftables.WithSockOptions(func(c *netlink.Conn) error {
		var err error
		bufs, err = growBuffers(c, total, len(chunks))
		return err
	}))
	if err != nil {
		return err
	}
	defer func() { _ = conn.CloseLasting() }()
	txs := split(chunks, bufs.send-sndbufReserve, (bufs.recv-rcvbufReserve)/ackSize)
	if len(txs) > 1 {
		b.log.Warn("nftables: the socket buffers cannot hold the whole change; applying it in several transactions",
			"transactions", len(txs), "bytes", total, "messages", len(chunks), "send_buffer", bufs.send, "receive_buffer", bufs.recv)
	}
	for i, tx := range txs {
		for _, c := range tx {
			if c.del {
				err = conn.SetDeleteElements(c.set, c.elems)
			} else {
				err = conn.SetAddElements(c.set, c.elems)
			}
			if err != nil {
				return err
			}
		}
		if err := flush(conn); err != nil {
			return fmt.Errorf("transaction %d of %d (%d additions, %d removals): %w", i+1, len(txs), len(add), len(remove), err)
		}
	}
	return nil
}

// chunks maps the removals, then the additions, to element messages per
// set of at most MaxChunk elements and maxChunkBytes.
func (b *Backend) chunks(add, remove []enforce.Entry) ([]chunk, error) {
	now := b.opts.Now()
	t := table()
	var chunks []chunk
	for _, del := range []bool{true, false} {
		entries := add
		if del {
			entries = remove
		}
		for _, f := range families {
			set := newSet(t, f)
			var cur []nftables.SetElement
			size := messageOverhead
			emit := func() {
				if len(cur) > 0 {
					chunks = append(chunks, chunk{set: set, del: del, elems: cur, size: size})
					cur, size = nil, messageOverhead
				}
			}
			for _, e := range entries {
				if e.Prefix.Addr().Is4() != (f.set == SetV4) {
					continue
				}
				elems, err := elementsOf(e, del, now)
				if err != nil {
					return nil, err
				}
				n := elementsSize(elems)
				if len(cur)+len(elems) > MaxChunk || size+n > maxChunkBytes {
					emit()
				}
				cur = append(cur, elems...)
				size += n
			}
			emit()
		}
	}
	return chunks, nil
}

// elementsOf maps e to the netlink elements that add it or, with del, that
// delete it.
func elementsOf(e enforce.Entry, del bool, now time.Time) ([]nftables.SetElement, error) {
	var elems []element
	if del {
		elems = keys(e.Prefix)
	} else {
		var err error
		if elems, err = toElements(e, now); err != nil {
			return nil, err
		}
	}
	out := make([]nftables.SetElement, len(elems))
	for i, el := range elems {
		out[i] = nftables.SetElement{Key: el.key, IntervalEnd: el.end, Comment: el.comment, Timeout: el.timeout}
	}
	return out, nil
}

// Size estimates of the netlink encoding, upper bounds with some slack.
const (
	// messageOverhead covers the headers of one element message: netlink
	// and nfgen headers, the table and set names, the set ID and the list.
	messageOverhead = 128
	// maxChunkBytes keeps a message's element list below the 64 KiB
	// limit of a netlink attribute.
	maxChunkBytes = 56 << 10
	// batchOverhead covers the batch begin and end messages.
	batchOverhead = 64
	// sndbufReserve is what the kernel keeps back from the send buffer.
	sndbufReserve = 1024
	// ackSize is the receive buffer one ack takes (its skb's truesize).
	ackSize = 2048
	// rcvbufReserve leaves room for the error of a failed message.
	rcvbufReserve = 64 << 10
)

// elementsSize estimates the encoded size of elems.
func elementsSize(elems []nftables.SetElement) int {
	n := 0
	for _, el := range elems {
		n += 4 + 12 + len(el.Key) // element, key
		if el.Timeout > 0 {
			n += 12
		}
		if el.Comment != "" {
			n += 12 + len(el.Comment) // userdata TLV, padded
		}
		if el.IntervalEnd {
			n += 8
		}
	}
	return n
}

// split groups chunks into transactions of at most maxBytes and maxMsgs
// messages, keeping their order; each transaction holds one chunk at least.
func split(chunks []chunk, maxBytes, maxMsgs int) [][]chunk {
	var txs [][]chunk
	var cur []chunk
	size := batchOverhead
	for _, c := range chunks {
		if len(cur) > 0 && (size+c.size > maxBytes || len(cur) >= maxMsgs) {
			txs = append(txs, cur)
			cur, size = nil, batchOverhead
		}
		cur = append(cur, c)
		size += c.size
	}
	return append(txs, cur)
}

// buffers are the socket buffer sizes a connection got.
type buffers struct{ send, recv int }

// growBuffers sizes the socket for a transaction of sendBytes in msgs
// messages: the send buffer holds the transaction, the receive buffer one
// ack per message. Beyond net.core.wmem_max/rmem_max this needs
// CAP_NET_ADMIN in the initial user namespace. Acks carry no copy of the
// request (NETLINK_CAP_ACK), so an error cannot overflow the buffer.
func growBuffers(c *netlink.Conn, sendBytes, msgs int) (buffers, error) {
	if err := c.SetOption(netlink.CapAcknowledge, true); err != nil {
		return buffers{}, err
	}
	raw, err := c.SyscallConn()
	if err != nil {
		return buffers{}, err
	}
	var got buffers
	var sockErr error
	err = raw.Control(func(fd uintptr) {
		got.send, sockErr = grow(int(fd), unix.SO_SNDBUF, unix.SO_SNDBUFFORCE, sendBytes+sndbufReserve)
		if sockErr == nil {
			got.recv, sockErr = grow(int(fd), unix.SO_RCVBUF, unix.SO_RCVBUFFORCE, msgs*ackSize+rcvbufReserve)
		}
	})
	if err == nil {
		err = sockErr
	}
	return got, err
}

// grow raises the socket buffer opt of fd to want bytes if it is smaller,
// through force if permitted, and returns its size.
func grow(fd, opt, force, want int) (int, error) {
	if got, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, opt); err != nil || got >= want {
		return got, err
	}
	// The kernel doubles the value it is given.
	if unix.SetsockoptInt(fd, unix.SOL_SOCKET, force, want/2+1) != nil {
		_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, opt, want/2+1)
	}
	return unix.GetsockoptInt(fd, unix.SOL_SOCKET, opt)
}

// Teardown deletes the table inet obie with every entry; a missing table
// is no error.
func (b *Backend) Teardown(ctx context.Context) error {
	conn, err := newConn(ctx)
	if err != nil {
		return err
	}
	t, err := findTable(conn)
	if err != nil || t == nil {
		return err
	}
	conn.DelTable(t)
	if err := flush(conn); err != nil && !errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("delete table inet %s: %w", Table, err)
	}
	b.log.Info("nftables: table inet " + Table + " deleted")
	return nil
}

// newConn returns a netlink connection bounded by ctx's deadline.
func newConn(ctx context.Context, opts ...nftables.ConnOption) (*nftables.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		opts = append(opts, nftables.WithSockOptions(func(c *netlink.Conn) error { return c.SetDeadline(deadline) }))
	}
	conn, err := nftables.New(opts...)
	if err != nil {
		return nil, permission(fmt.Errorf("open netlink: %w", err))
	}
	return conn, nil
}

// findTable returns the table inet obie, nil if there is none.
func findTable(conn *nftables.Conn) (*nftables.Table, error) {
	tables, err := conn.ListTablesOfFamily(nftables.TableFamilyINet)
	if err != nil {
		return nil, permission(fmt.Errorf("list the nftables tables: %w", err))
	}
	for _, t := range tables {
		if t.Name == Table {
			return t, nil
		}
	}
	return nil, nil
}

// flush sends the queued messages as one transaction.
func flush(conn *nftables.Conn) error {
	return permission(conn.Flush())
}

// permission turns a refusal by the kernel into ErrPermission.
func permission(err error) error {
	if errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("%w: %w", ErrPermission, err)
	}
	return err
}
