// Package sim is the routing simulation of ADR 0035: thousands of honest
// OBIE nodes — the real internal/mesh and internal/gossip — and hostile
// peers on an in-memory libp2p network, in the virtual time of a
// testing/synctest bubble. A scenario builds the network, its adversaries
// and its traffic; a run records the per-event trace of every node and
// measures delivery, latency, duplicates, hop counts, the adversaries'
// share of mesh slots, mesh recovery, retention under flood and loss by
// cause. `make sim-routing SCENARIO=…` runs the scenarios with 20 seeds
// and writes the versioned reports.
package sim
