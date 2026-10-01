package e2e

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/MNCloudwerksTechnology/obie/internal/admin"
	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// The attacking addresses of the scenarios, all in documentation ranges.
const (
	// ipX is reported by A and B and blocked under quorum.
	ipX = "203.0.113.10"
	// ipZ is reported by A and B below the threshold.
	ipZ = "203.0.113.20"
	// ipY is reported by the untrusted D only.
	ipY = "203.0.113.30"
	// ipForged is the indicator of the forged event.
	ipForged = "203.0.113.40"
	// ipAllowlisted lies in allowlist.cidrs.
	ipAllowlisted = "198.51.100.5"
)

// TestSignalToEnforcement runs four complete nodes: A, B and C trust each
// other with weight 1.0, D is trusted by nobody. It walks a report on one
// node to a block on another under threshold 1.8 and quorum 2, and back.
// The scenarios build on each other and run in order.
func TestSignalToEnforcement(t *testing.T) {
	start := time.Now()
	c := newCluster(t, clusterOptions{backend: config.BackendDryRun}, "A", "B", "C", "D")
	a, b, cn, d := c.node("A"), c.node("B"), c.node("C"), c.node("D")
	t.Logf("cluster of 4 nodes ready after %s", time.Since(start).Round(time.Millisecond))

	steps := []struct {
		name string
		run  func(t *testing.T)
	}{
		{"A reports X: A blocks alone, B and C only store the verdict", func(t *testing.T) {
			stepLocalAutoblock(t, c)
		}},
		{"B reports X: C blocks under quorum and explains why", func(t *testing.T) {
			stepQuorumBlock(t, c)
			holds(t, quietPeriod, "untrusted D must not block X", func(ctx context.Context) error { return d.notBlocking(ctx, ipX) })
		}},
		{"A and B report Z at 0.8: C stays below the threshold", func(t *testing.T) {
			a.report(t, ipZ, 0.8, obieproto.ActionBan)
			b.report(t, ipZ, 0.8, obieproto.ActionBan)
			within(t, propagationBound, "C stores the verdicts of A and B on Z", func(ctx context.Context) error {
				return errors.Join(cn.hasVerdict(ctx, ipZ, a, obieproto.ActionBan), cn.hasVerdict(ctx, ipZ, b, obieproto.ActionBan))
			})
			holds(t, quietPeriod, "C must not block Z", func(ctx context.Context) error { return cn.notBlocking(ctx, ipZ) })
			assertScore(t, cn, ipZ, 1.6, 2)
		}},
		{"untrusted D reports Y repeatedly: only D blocks", func(t *testing.T) {
			for i := range 3 {
				resp := d.report(t, ipY, 1.0, obieproto.ActionBan)
				if i > 0 && !resp.Coalesced {
					t.Errorf("report %d of D on Y issued a new verdict; want it coalesced", i+1)
				}
			}
			within(t, enforcementBound, "D blocks Y", func(ctx context.Context) error { return d.blocks(ctx, ipY) })
			within(t, propagationBound, "A, B and C store the verdict of D on Y", func(ctx context.Context) error {
				return errors.Join(a.hasVerdict(ctx, ipY, d, obieproto.ActionBan), b.hasVerdict(ctx, ipY, d, obieproto.ActionBan),
					cn.hasVerdict(ctx, ipY, d, obieproto.ActionBan))
			})
			holds(t, quietPeriod, "A, B and C must not block Y", func(ctx context.Context) error {
				return errors.Join(a.notBlocking(ctx, ipY), b.notBlocking(ctx, ipY), cn.notBlocking(ctx, ipY))
			})
			assertScore(t, cn, ipY, 0, 0)
		}},
		{"a report of an allow-listed address is refused", func(t *testing.T) {
			err := call(func(ctx context.Context) error {
				_, err := a.client.Report(ctx, reportRequest(ipAllowlisted, 0.95, obieproto.ActionBan))
				return err
			})
			var apiErr *admin.APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(apiErr.Message, allowlisted) {
				t.Fatalf("report of allow-listed %s = %v, want 422 naming %s", ipAllowlisted, err, allowlisted)
			}
			holds(t, quietPeriod, "nobody holds a verdict on the allow-listed address", func(ctx context.Context) error {
				for _, n := range c.nodes {
					dec, err := n.explain(ctx, ipAllowlisted)
					if err != nil {
						return err
					}
					if len(dec.Publishers) > 0 {
						return fmt.Errorf("node %s holds verdicts on %s: %+v", n.name, ipAllowlisted, dec.Publishers)
					}
				}
				return nil
			})
		}},
		{"a forged event is rejected and counted", func(t *testing.T) {
			stepForgedEvent(t, cn, b)
		}},
		{"C restarts: its blocks are restored from the store", func(t *testing.T) {
			cn.stop(t)
			restart := time.Now()
			cn.start(t)
			within(t, enforcementBound, "restarted C applies the block of X again", func(ctx context.Context) error { return cn.blocks(ctx, ipX) })
			t.Logf("C restarted and re-applied its blocks after %s", time.Since(restart).Round(time.Millisecond))
			// C listens on new ports; A and B learn of it through its dial.
			c.awaitPeers(t, cn, 2)
			c.awaitMesh(t, a, cn)
		}},
		{"A revokes X: C unblocks", func(t *testing.T) {
			stepRevoke(t, c)
		}},
	}
	for _, s := range steps {
		if !t.Run(s.name, s.run) {
			t.FailNow()
		}
	}
	if took := time.Since(start); took > suiteBound {
		t.Errorf("the end-to-end test took %s, want at most %s", took.Round(time.Millisecond), suiteBound)
	}
}

// stepLocalAutoblock: A reports X with confidence 0.95; A blocks it on its
// own (local autoblock), B and C store the verdict but one publisher is
// below quorum.
func stepLocalAutoblock(t *testing.T, c *cluster) {
	a, b, cn := c.node("A"), c.node("B"), c.node("C")
	a.report(t, ipX, 0.95, obieproto.ActionBan)
	within(t, enforcementBound, "A blocks X", func(ctx context.Context) error { return a.blocks(ctx, ipX) })
	within(t, propagationBound, "B and C store the verdict of A on X", func(ctx context.Context) error {
		return errors.Join(b.hasVerdict(ctx, ipX, a, obieproto.ActionBan), cn.hasVerdict(ctx, ipX, a, obieproto.ActionBan))
	})
	holds(t, quietPeriod, "B and C must not block X on one publisher", func(ctx context.Context) error {
		return errors.Join(b.notBlocking(ctx, ipX), cn.notBlocking(ctx, ipX))
	})
	assertScore(t, cn, ipX, 0.95, 1)
}

// stepQuorumBlock: B reports X with confidence 0.95; with A's verdict the
// score on C is 1.9 from 2 publishers, so C blocks and explains it.
func stepQuorumBlock(t *testing.T, c *cluster) {
	a, b, cn := c.node("A"), c.node("B"), c.node("C")
	b.report(t, ipX, 0.95, obieproto.ActionBan)
	within(t, propagationBound, "C stores the verdict of B on X", func(ctx context.Context) error {
		return cn.hasVerdict(ctx, ipX, b, obieproto.ActionBan)
	})
	within(t, enforcementBound, "C blocks X", func(ctx context.Context) error { return cn.blocks(ctx, ipX) })
	d := assertScore(t, cn, ipX, 1.9, 2)
	for _, publisher := range []*node{a, b} {
		contrib, ok := contribution(d, publisher)
		switch {
		case !ok:
			t.Errorf("explain on C does not list %s: %+v", publisher.name, d.Publishers)
		case contrib.Name != publisher.name || !near(contrib.Weight, trusted) || !near(contrib.Confidence, 0.95) ||
			!contrib.Contributes || !near(contrib.Score, 0.95):
			t.Errorf("explain on C lists %s as %+v, want name %s, weight %.1f, confidence 0.95, contributing 0.95",
				publisher.name, contrib, publisher.name, trusted)
		}
	}
	if d.LocalAutoblock {
		t.Errorf("C's block of X is a local autoblock; want the consensus of A and B")
	}
}

// stepRevoke: A revokes X; C (score 0.95 from B alone) and A (B's verdict
// only) unblock it, B keeps its own block.
func stepRevoke(t *testing.T, c *cluster) {
	a, b, cn := c.node("A"), c.node("B"), c.node("C")
	a.revoke(t, ipX)
	within(t, propagationBound, "C drops the verdict of A on X", func(ctx context.Context) error {
		if err := cn.hasVerdict(ctx, ipX, a, obieproto.ActionBan); err == nil {
			return errors.New("C still holds the verdict of A")
		}
		return nil
	})
	within(t, enforcementBound, "A and C unblock X", func(ctx context.Context) error {
		return errors.Join(cn.notBlocking(ctx, ipX), a.notBlocking(ctx, ipX))
	})
	assertScore(t, cn, ipX, 0.95, 1)
	if err := call(func(ctx context.Context) error { return b.blocks(ctx, ipX) }); err != nil {
		t.Errorf("B must keep blocking X on its own verdict: %v", err)
	}
}

// assertScore checks the score and the contributors of n's decision on ip
// and returns the decision.
func assertScore(t *testing.T, n *node, ip string, score float64, contributors int) *admin.DecisionResponse {
	t.Helper()
	var d *admin.DecisionResponse
	if err := call(func(ctx context.Context) error {
		var err error
		d, err = n.explain(ctx, ip)
		return err
	}); err != nil {
		t.Fatalf("explain %s on %s: %v", ip, n.name, err)
	}
	if !near(d.Score, score) || d.Contributors != contributors || !near(d.Threshold, threshold) || d.Quorum != quorum {
		t.Errorf("node %s on %s: score %.2f from %d contributors (threshold %.1f, quorum %d), want %.2f from %d (%.1f, %d)",
			n.name, ip, d.Score, d.Contributors, d.Threshold, d.Quorum, score, contributors, threshold, quorum)
	}
	return d
}

// invalidSignatures is the counter of events dropped for a bad signature.
const invalidSignatures = `obie_events_received_total{outcome="invalid_signature"}`

// stepForgedEvent injects an event claiming to come from the trusted
// publisher into the mesh at target, signed with another key: target must
// reject it, count it and never act on it.
func stepForgedEvent(t *testing.T, target, publisher *node) {
	before := metricValue(t, target, invalidSignatures)
	topic := joinAsAttacker(t, target)
	data := forgedEvent(t, publisher.peerID, ipForged)
	if err := topic.Publish(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	within(t, propagationBound, "C counts the forged event", func(ctx context.Context) error {
		v, err := target.metric(ctx, invalidSignatures)
		if err == nil && v <= before {
			err = fmt.Errorf("%s = %v, was %v", invalidSignatures, v, before)
		}
		return err
	})
	holds(t, quietPeriod, "C must not store or act on the forged event", func(ctx context.Context) error {
		d, err := target.explain(ctx, ipForged)
		if err == nil && len(d.Publishers) > 0 {
			err = fmt.Errorf("C holds verdicts on %s: %+v", ipForged, d.Publishers)
		}
		return errors.Join(err, target.notBlocking(ctx, ipForged))
	})
}

// metricValue reads a sample of n's /metrics.
func metricValue(t *testing.T, n *node, sample string) float64 {
	t.Helper()
	var v float64
	if err := call(func(ctx context.Context) error {
		var err error
		v, err = n.metric(ctx, sample)
		return err
	}); err != nil {
		t.Fatalf("node %s: %v", n.name, err)
	}
	return v
}

// joinAsAttacker starts a bare libp2p host with GossipSub, without any of
// OBIE's checks, connects it to target and returns the topic once target
// is a topic peer.
func joinAsAttacker(t *testing.T, target *node) *pubsub.Topic {
	t.Helper()
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	// Like an OBIE node: no author and no pubsub signature, or the
	// messages would not even reach the event checks.
	ps, err := pubsub.NewGossipSub(ctx, h, pubsub.WithMessageSignaturePolicy(pubsub.StrictNoSign), pubsub.WithNoAuthor(),
		pubsub.WithMessageIdFn(func(m *pb.Message) string {
			sum := sha256.Sum256(m.GetData())
			return string(sum[:])
		}))
	if err != nil {
		t.Fatal(err)
	}
	topic, err := ps.Join(obieproto.Topic)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = topic.Close() })
	id, err := peer.Decode(target.peerID)
	if err != nil {
		t.Fatal(err)
	}
	info := peer.AddrInfo{ID: id}
	for _, a := range target.endpoints.Mesh {
		info.Addrs = append(info.Addrs, ma.StringCast(a))
	}
	if err := call(func(ctx context.Context) error { return h.Connect(ctx, info) }); err != nil {
		t.Fatalf("connect to %s: %v", target.name, err)
	}
	within(t, meshBound, "the attacker sees C on the topic", func(context.Context) error {
		if !slices.Contains(topic.ListPeers(), id) {
			return errors.New("not yet")
		}
		return nil
	})
	return topic
}

// forgedEvent returns a verdict on ip that names claimed as its publisher
// but is signed with a fresh key.
func forgedEvent(t *testing.T, claimed, ip string) []byte {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	forger, err := obieproto.PeerIDFromPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	ev := &obieproto.Event{
		ID: obieproto.NewID(now), Spec: obieproto.Spec, Type: obieproto.TypeVerdict,
		IssuedAt:  obieproto.NewTimestamp(now),
		Indicator: obieproto.Indicator{Kind: obieproto.KindIPv4, Value: ip, Scope: "/32"},
		Protocol:  "ssh",
		Evidence:  &obieproto.Evidence{Events: 50, Reason: "password_bruteforce"},
		Verdict:   &obieproto.Verdict{SuggestedAction: obieproto.ActionBan, Confidence: 1, TTLSeconds: 3600},
		Publisher: obieproto.Publisher{PeerID: forger},
	}
	if err := obieproto.Sign(ev, key); err != nil {
		t.Fatal(err)
	}
	ev.Publisher.PeerID = claimed
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
