// Package gossip spreads obie/0.1 events over the libp2p mesh with
// GossipSub on the topic obieproto.Topic, and admits only valid,
// non-duplicate events within per-publisher and per-peer rate limits into
// the local store and on to other peers (ADR 0009).
package gossip
