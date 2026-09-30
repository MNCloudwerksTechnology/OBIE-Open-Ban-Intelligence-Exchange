package simtrust

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"sync"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/decision"
	"github.com/MNCloudwerksTechnology/obie/internal/sovereignty"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// Profile is a settings profile of the observer (ADR 0034).
type Profile string

// Settings profiles.
const (
	// ProfileDefault is config.Default() with the trusted remotes listed.
	ProfileDefault Profile = "default"
	// ProfileLab lowers decision.threshold to the compose lab's.
	ProfileLab Profile = "lab"
	// ProfileAllowlist is ProfileDefault with the published benign ranges
	// in allowlist.cidrs.
	ProfileAllowlist Profile = "allowlist"
)

// LabThreshold is decision.threshold in the compose lab
// (packaging/compose/lab-init.sh).
const LabThreshold = 1.5

// Ceiling is the trust weight every trusted remote is listed with: the
// most a publisher can have.
const Ceiling = 1.0

// idleInterval is the wall-time interval of the store's and the engine's
// background timers, which must never fire during a run: the harness
// sweeps and evaluates in virtual time.
const idleInterval = 24 * time.Hour

// NodeSpec is what the observer is configured with.
type NodeSpec struct {
	Profile Profile
	// Trusted are the peer IDs listed in trust.publishers, each with
	// weight Ceiling.
	Trusted []string
	// Published are the published benign ranges, which ProfileAllowlist
	// allow-lists.
	Published []netip.Prefix
}

// Config returns the validated configuration of the observer: the
// defaults of obied with the profile applied.
func (s NodeSpec) Config() (config.Config, error) {
	cfg := config.Default()
	for i, id := range s.Trusted {
		cfg.Trust.Publishers = append(cfg.Trust.Publishers,
			config.Publisher{PeerID: id, Name: fmt.Sprintf("publisher-%02d", i+1), Weight: Ceiling})
	}
	switch s.Profile {
	case ProfileDefault:
	case ProfileLab:
		cfg.Decision.Threshold = LabThreshold
	case ProfileAllowlist:
		for _, p := range s.Published {
			cfg.Allowlist.CIDRs = append(cfg.Allowlist.CIDRs, p.String())
		}
	default:
		return config.Config{}, fmt.Errorf("unknown settings profile %q", s.Profile)
	}
	if err := cfg.Validate(); err != nil {
		return config.Config{}, fmt.Errorf("configuration of profile %s: %w", s.Profile, err)
	}
	return cfg, nil
}

// virtualClock is the time of a run.
type virtualClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *virtualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *virtualClock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

// Node is the observer: the store, allow-list and decision engine of obied,
// built from a configuration as internal/daemon builds them, on a virtual
// clock. It is the replay adapter of ADR 0034: events go into the real
// store, and the enforced bans come out of the real engine's block change
// stream.
type Node struct {
	clock       *virtualClock
	store       *store.DB
	engine      *decision.Engine
	unsubscribe func()
}

// StartNode starts the observer with peer ID self and configuration cfg
// at virtual time start. onChange receives the engine's block changes in
// order; it runs on the goroutine of Settle or Sweep, or on the engine's
// worker, one change at a time.
func StartNode(ctx context.Context, self string, cfg *config.Config, start time.Time, onChange func(decision.Change)) (*Node, error) {
	log := slog.New(slog.DiscardHandler)
	// The simulated world lives in 2001:db8::/32; the observer has no
	// interface or bootstrap addresses of its own.
	allow, err := sovereignty.Build(ctx, cfg, sovereignty.Env{
		InterfaceAddrs:          func() ([]netip.Addr, error) { return nil, nil },
		LookupIP:                func(context.Context, string, string) ([]netip.Addr, error) { return nil, nil },
		OmitDocumentationRanges: true,
	}, log)
	if err != nil {
		return nil, fmt.Errorf("allow-list: %w", err)
	}
	clock := &virtualClock{t: start}
	st := store.NewMemory(log, store.Options{
		SweepInterval:  idleInterval,
		GCInterval:     idleInterval,
		Now:            clock.Now,
		MaxIndicators:  cfg.Store.MaxIndicators,
		EndedRetention: cfg.Store.EndedRetention.Std(),
		Self:           self,
	})
	if err := st.Start(ctx); err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	engine := decision.New(st, decision.NewPolicy(self, cfg.Trust, cfg.Decision), log,
		decision.Options{RefreshInterval: idleInterval, Now: clock.Now, Allowlist: allow})
	unsubscribe := engine.Subscribe(onChange)
	if err := engine.Start(ctx); err != nil {
		unsubscribe()
		return nil, errors.Join(fmt.Errorf("decision engine: %w", err), st.Stop(ctx))
	}
	return &Node{clock: clock, store: st, engine: engine, unsubscribe: unsubscribe}, nil
}

// Now returns the node's virtual time.
func (n *Node) Now() time.Time {
	return n.clock.Now()
}

// Advance moves the virtual clock to t; it never goes back.
func (n *Node) Advance(t time.Time) error {
	if now := n.clock.Now(); t.Before(now) {
		return fmt.Errorf("virtual time cannot go back from %s to %s", now.Format(time.RFC3339), t.Format(time.RFC3339))
	}
	n.clock.set(t)
	return nil
}

// Deliver stores ev at the current virtual time, as the gossip layer does
// with a valid event, and evaluates its indicator before it returns, so
// that every event is decided on its own and a run is deterministic. It
// reports whether the store kept the event.
func (n *Node) Deliver(ev *obieproto.Event) (bool, error) {
	kept, err := n.store.Put(ev)
	if err != nil {
		return false, err
	}
	return kept, n.settle()
}

// Sweep removes the verdicts expired at the current virtual time, as the
// store's periodic sweep does, and evaluates the indicators it changed.
func (n *Node) Sweep() error {
	if err := n.store.Sweep(n.clock.Now()); err != nil {
		return fmt.Errorf("sweep: %w", err)
	}
	return n.settle()
}

// settle evaluates the indicators changed since the last call at the
// current virtual time; their block changes are delivered before it
// returns, whether it or the engine's worker evaluates them.
func (n *Node) settle() error {
	n.engine.Flush()
	return n.engine.Ready()
}

// Weight returns the trust weight the engine gives the publisher with
// peerID now.
func (n *Node) Weight(peerID string) float64 {
	p := n.engine.Policy()
	return p.Weight(peerID)
}

// Close stops the engine and the store.
func (n *Node) Close(ctx context.Context) error {
	n.unsubscribe()
	return errors.Join(n.engine.Stop(ctx), n.store.Stop(ctx))
}
