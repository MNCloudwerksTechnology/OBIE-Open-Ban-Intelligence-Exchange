package simtrust

import (
	"context"
	"math/rand/v2"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/decision"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

var testStart = time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)

// testNode is an observer trusting three remotes, with its ban log.
type testNode struct {
	t       *testing.T
	node    *Node
	bans    *BanLog
	self    Key
	remotes []Key
	ids     idSource
}

func newTestNode(t *testing.T, profile Profile, published ...netip.Prefix) *testNode {
	t.Helper()
	rng := rand.New(rand.NewPCG(1, 2)) // #nosec G404 -- reproducible test data.
	tn := &testNode{t: t, bans: NewBanLog(), self: NewKey(1, "observer"), ids: idSource{rng: rng}}
	var trusted []string
	for _, label := range []string{"a", "b", "c"} {
		k := NewKey(1, label)
		tn.remotes = append(tn.remotes, k)
		trusted = append(trusted, k.PeerID)
	}
	cfg, err := NodeSpec{Profile: profile, Trusted: trusted, Published: published}.Config()
	if err != nil {
		t.Fatal(err)
	}
	node, err := StartNode(context.Background(), tn.self.PeerID, &cfg, testStart, tn.bans.Record)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := node.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	tn.node = node
	return tn
}

// at moves the clock to testStart+d.
func (tn *testNode) at(d time.Duration) {
	tn.t.Helper()
	if err := tn.node.Advance(testStart.Add(d)); err != nil {
		tn.t.Fatal(err)
	}
}

// report delivers a ban verdict of k on addr now and settles the engine.
func (tn *testNode) report(k Key, addr string, confidence float64, ttl time.Duration) *obieproto.Event {
	tn.t.Helper()
	now := tn.node.Now()
	ev := newVerdict(tn.ids.next(now), publisherOf(k.PeerID, 0), netip.MustParseAddr(addr), now, confidence, ttl)
	tn.put(ev)
	return ev
}

func (tn *testNode) put(ev *obieproto.Event) {
	tn.t.Helper()
	if ok, err := tn.node.Deliver(ev); err != nil || !ok {
		tn.t.Fatalf("Deliver(%s) = %v, %v; want stored", ev.ID, ok, err)
	}
}

func (tn *testNode) sweep() {
	tn.t.Helper()
	if err := tn.node.Sweep(); err != nil {
		tn.t.Fatal(err)
	}
}

func (tn *testNode) episodes(end time.Duration) []Episode {
	tn.t.Helper()
	eps, err := tn.bans.Episodes(testStart.Add(end))
	if err != nil {
		tn.t.Fatal(err)
	}
	return eps
}

const victim = "2001:db8:1:1::7"

// TestTwoTrustedRemotesAtDefaultConfidence is the finding of ADR 0034:
// two fully trusted remotes at confidence 0.8 score 1.6 and never ban
// under the defaults; the compose lab's threshold lets them; three ban
// under both.
func TestTwoTrustedRemotesAtDefaultConfidence(t *testing.T) {
	for _, tt := range []struct {
		profile Profile
		remotes int
		want    int
	}{
		{ProfileDefault, 2, 0},
		{ProfileDefault, 3, 1},
		{ProfileLab, 2, 1},
	} {
		tn := newTestNode(t, tt.profile)
		for _, k := range tn.remotes[:tt.remotes] {
			tn.report(k, victim, 0.8, time.Hour)
		}
		if got := len(tn.episodes(2 * time.Hour)); got != tt.want {
			t.Errorf("%s, %d remotes at 0.8: %d bans, want %d", tt.profile, tt.remotes, got, tt.want)
		}
	}
}

func TestEpisodeNamesContributorsInOrder(t *testing.T) {
	tn := newTestNode(t, ProfileLab)
	tn.report(tn.remotes[0], victim, 0.8, time.Hour)
	tn.at(time.Minute)
	tn.report(tn.remotes[1], victim, 0.8, time.Hour)
	tn.at(2 * time.Minute)
	tn.report(tn.remotes[2], victim, 0.8, time.Hour)
	eps := tn.episodes(2 * time.Hour)
	if len(eps) != 1 {
		t.Fatalf("episodes = %+v, want one", eps)
	}
	want := []Contribution{
		{tn.remotes[0].PeerID, testStart.Add(time.Minute)},
		{tn.remotes[1].PeerID, testStart.Add(time.Minute)},
		{tn.remotes[2].PeerID, testStart.Add(2 * time.Minute)},
	}
	slices.SortFunc(want[:2], func(a, b Contribution) int { return strings.Compare(a.PeerID, b.PeerID) })
	if !slices.Equal(eps[0].Contributors, want) {
		t.Errorf("contributors = %+v, want %+v", eps[0].Contributors, want)
	}
	if e := eps[0]; e.Addr != netip.MustParseAddr(victim) || !e.Start.Equal(testStart.Add(time.Minute)) || e.Autoblock {
		t.Errorf("episode = %+v, want a consensus ban of %s from 1m", e, victim)
	}
	if want := testStart.Add(time.Hour + 2*time.Minute); !eps[0].End.Equal(want) {
		t.Errorf("open episode ends at %s, want the latest verdict's expiry %s", eps[0].End, want)
	}
}

func TestEpisodeEndsAtExpiryNotAtSweep(t *testing.T) {
	tn := newTestNode(t, ProfileLab)
	tn.report(tn.remotes[0], victim, 0.8, 10*time.Minute)
	tn.report(tn.remotes[1], victim, 0.8, 10*time.Minute)
	tn.at(15 * time.Minute)
	tn.sweep()
	// A new ban after the first ended is an episode of its own.
	tn.report(tn.remotes[0], victim, 0.8, 10*time.Minute)
	tn.report(tn.remotes[2], victim, 0.8, 10*time.Minute)
	eps := tn.episodes(time.Hour)
	if len(eps) != 2 {
		t.Fatalf("episodes = %+v, want two", eps)
	}
	if want := testStart.Add(10 * time.Minute); !eps[0].End.Equal(want) {
		t.Errorf("first episode ends at %s, want its expiry %s", eps[0].End, want)
	}
	if want := testStart.Add(15 * time.Minute); !eps[1].Start.Equal(want) {
		t.Errorf("second episode starts at %s, want %s", eps[1].Start, want)
	}
}

// blockChange is a block change of victim evaluated at testStart+at.
func blockChange(typ decision.ChangeType, at, expires time.Duration, peerIDs ...string) decision.Change {
	c := decision.Change{Type: typ, Key: "ipv6:" + victim, Decision: decision.Decision{
		Indicator:   indicatorOf(netip.MustParseAddr(victim)),
		EvaluatedAt: testStart.Add(at), ExpiresAt: testStart.Add(expires),
	}}
	for _, id := range peerIDs {
		c.Contributors = append(c.Contributors, decision.Contributor{PeerID: id})
	}
	return c
}

func TestBanLogEpisodes(t *testing.T) {
	for _, tt := range []struct {
		name    string
		changes []decision.Change
		want    []Episode
	}{
		{
			name: "update extends and adds contributors",
			changes: []decision.Change{
				blockChange(decision.ChangeAdded, 0, 10*time.Minute, "a", "b"),
				blockChange(decision.ChangeUpdated, 5*time.Minute, time.Hour, "b", "c"),
				blockChange(decision.ChangeRemoved, 30*time.Minute, 0, "b", "c"),
			},
			want: []Episode{{Start: testStart, End: testStart.Add(30 * time.Minute), Contributors: []Contribution{
				{"a", testStart}, {"b", testStart}, {"c", testStart.Add(5 * time.Minute)}}}},
		},
		{
			// Without a sweep between them, the engine reports a ban that
			// follows an expired one as an update.
			name: "update after the expiry starts a new episode",
			changes: []decision.Change{
				blockChange(decision.ChangeAdded, 0, 10*time.Minute, "a", "b"),
				blockChange(decision.ChangeUpdated, 20*time.Minute, 30*time.Minute, "a", "c"),
			},
			want: []Episode{
				{Start: testStart, End: testStart.Add(10 * time.Minute), Contributors: []Contribution{{"a", testStart}, {"b", testStart}}},
				{Start: testStart.Add(20 * time.Minute), End: testStart.Add(30 * time.Minute), Contributors: []Contribution{
					{"a", testStart.Add(20 * time.Minute)}, {"c", testStart.Add(20 * time.Minute)}}},
			},
		},
		{
			name: "removal after the expiry ends it at the expiry",
			changes: []decision.Change{
				blockChange(decision.ChangeAdded, 0, 10*time.Minute, "a"),
				blockChange(decision.ChangeRemoved, 14*time.Minute, 0),
			},
			want: []Episode{{Start: testStart, End: testStart.Add(10 * time.Minute), Contributors: []Contribution{{"a", testStart}}}},
		},
		{
			name: "open episode ends at the end of the run",
			changes: []decision.Change{
				blockChange(decision.ChangeAdded, 0, 48*time.Hour, "a"),
			},
			want: []Episode{{Start: testStart, End: testStart.Add(24 * time.Hour), Contributors: []Contribution{{"a", testStart}}}},
		},
	} {
		l := NewBanLog()
		for _, c := range tt.changes {
			l.Record(c)
		}
		got, err := l.Episodes(testStart.Add(24 * time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		for i := range tt.want {
			tt.want[i].Addr = netip.MustParseAddr(victim)
		}
		if !slices.EqualFunc(got, tt.want, sameEpisode) {
			t.Errorf("%s: episodes = %+v, want %+v", tt.name, got, tt.want)
		}
	}
}

func sameEpisode(a, b Episode) bool {
	return a.Addr == b.Addr && a.Start.Equal(b.Start) && a.End.Equal(b.End) && a.Autoblock == b.Autoblock &&
		slices.EqualFunc(a.Contributors, b.Contributors, func(x, y Contribution) bool { return x.PeerID == y.PeerID && x.At.Equal(y.At) })
}

func TestRevocationEndsBan(t *testing.T) {
	tn := newTestNode(t, ProfileDefault)
	var first *obieproto.Event
	for i, k := range tn.remotes {
		ev := tn.report(k, victim, 0.8, time.Hour)
		if i == 0 {
			first = ev
		}
	}
	tn.at(5 * time.Minute)
	tn.put(newRevocation(tn.ids.next(tn.node.Now()), first, tn.node.Now()))
	eps := tn.episodes(time.Hour)
	if len(eps) != 1 || !eps[0].End.Equal(testStart.Add(5*time.Minute)) {
		t.Fatalf("episodes = %+v, want one ended by the revocation at 5m", eps)
	}
}

func TestLocalVerdictAutoblocks(t *testing.T) {
	tn := newTestNode(t, ProfileDefault)
	tn.report(tn.self, victim, 0.8, time.Hour)
	eps := tn.episodes(time.Hour)
	if len(eps) != 1 || !eps[0].Autoblock {
		t.Fatalf("episodes = %+v, want one autoblock", eps)
	}
}

func TestAllowlistProfileProtectsPublishedRanges(t *testing.T) {
	published := netip.MustParsePrefix("2001:db8:1:1::/64")
	for _, tt := range []struct {
		profile Profile
		want    int
	}{{ProfileDefault, 1}, {ProfileAllowlist, 0}} {
		tn := newTestNode(t, tt.profile, published)
		for _, k := range tn.remotes {
			tn.report(k, victim, 0.8, time.Hour)
		}
		if got := len(tn.episodes(time.Hour)); got != tt.want {
			t.Errorf("%s: %d bans of an address in a published range, want %d", tt.profile, got, tt.want)
		}
	}
}

func TestWeights(t *testing.T) {
	tn := newTestNode(t, ProfileDefault)
	for _, tt := range []struct {
		name, peerID string
		want         float64
	}{
		{"trusted remote", tn.remotes[0].PeerID, Ceiling},
		{"observer", tn.self.PeerID, 1},
		{"unlisted key", NewKey(1, "stranger").PeerID, 0},
	} {
		if got := tn.node.Weight(tt.peerID); got != tt.want {
			t.Errorf("weight of the %s = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestConfigProfiles(t *testing.T) {
	cfg, err := NodeSpec{Profile: ProfileLab}.Config()
	if err != nil || cfg.Decision.Threshold != LabThreshold || cfg.Decision.Quorum != 2 {
		t.Errorf("lab config = %+v, %v; want threshold %v, quorum 2", cfg.Decision, err, LabThreshold)
	}
	if _, err := (NodeSpec{Profile: "nightly"}).Config(); err == nil {
		t.Error("an unknown profile was accepted")
	}
	if _, err := (NodeSpec{Profile: ProfileDefault, Trusted: []string{"not-a-peer-id"}}).Config(); err == nil {
		t.Error("an invalid peer ID passed the configuration's validation")
	}
}

func TestAdvanceNeverGoesBack(t *testing.T) {
	tn := newTestNode(t, ProfileDefault)
	tn.at(time.Hour)
	if err := tn.node.Advance(testStart); err == nil {
		t.Error("the clock went back")
	}
}

func TestBanLogRejectsNonAddress(t *testing.T) {
	l := NewBanLog()
	l.Record(decision.Change{Type: decision.ChangeAdded, Key: "cidr:2001:db8::/32",
		Decision: decision.Decision{Indicator: obieproto.Indicator{Kind: obieproto.KindCIDR, Value: "2001:db8::/32"}}})
	if _, err := l.Episodes(testStart); err == nil {
		t.Error("a block on a range was recorded as an address")
	}
}
