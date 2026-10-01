package sim

import (
	"crypto/ed25519"
	"sync"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// memStore is the store of a simulated node where eviction is not
// measured: it remembers the IDs of the events it accepted until they
// expire, which is all that routing asks of a store (Seen and Put). It
// keeps no event and no indicator state (ADR 0035).
type memStore struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

var _ store.Store = (*memStore)(nil)

func newMemStore() *memStore { return &memStore{seen: map[string]time.Time{}} }

// Put accepts an unexpired event it has not seen, like store.DB.
func (s *memStore) Put(ev *obieproto.Event) (bool, error) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if exp, ok := s.seen[ev.ID]; ok && now.Before(exp) {
		return false, nil
	}
	if ev.Expired(now) {
		return false, nil
	}
	s.seen[ev.ID] = ev.ExpiresAt()
	return true, nil
}

// Seen reports whether Put accepted an event with the ID that has not
// expired.
func (s *memStore) Seen(id string) (bool, error) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.seen[id]
	return ok && now.Before(exp), nil
}

// The store keeps no events, indicators or overrides.

func (*memStore) Get(string) (*obieproto.Event, error) { return nil, store.ErrNotFound }
func (*memStore) ActiveVerdicts(string, time.Time) ([]*obieproto.Event, error) {
	return nil, nil
}
func (*memStore) ListIndicators(time.Time, store.Filter, store.Page) (store.IndicatorPage, error) {
	return store.IndicatorPage{}, nil
}
func (*memStore) Subscribe(func(store.Change)) func() { return func() {} }
func (*memStore) SetOverride(store.Override) error    { return store.ErrInvalid }
func (*memStore) Override(string, time.Time) (store.Override, error) {
	return store.Override{}, store.ErrNotFound
}
func (*memStore) DeleteOverride(string) (bool, error)           { return false, nil }
func (*memStore) Overrides(time.Time) ([]store.Override, error) { return nil, nil }

// simIdentity is a node key in memory; internal/identity keeps keys in
// files only.
type simIdentity struct {
	key    ed25519.PrivateKey
	peerID string
}

// newSimIdentity returns the identity of the Ed25519 key with the given
// seed.
func newSimIdentity(seed [ed25519.SeedSize]byte) (*simIdentity, error) {
	key := ed25519.NewKeyFromSeed(seed[:])
	id, err := obieproto.PeerIDFromPublicKey(key.Public().(ed25519.PublicKey))
	if err != nil {
		return nil, err
	}
	return &simIdentity{key: key, peerID: id}, nil
}

func (s *simIdentity) PeerID() string { return s.peerID }

func (s *simIdentity) PublicKey() ed25519.PublicKey { return s.key.Public().(ed25519.PublicKey) }

func (s *simIdentity) Sign(msg []byte) []byte { return ed25519.Sign(s.key, msg) }
