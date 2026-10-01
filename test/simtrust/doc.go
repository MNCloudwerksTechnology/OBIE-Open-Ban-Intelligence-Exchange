// Package simtrust is the trust simulation of ADR 0034: it replays a trace
// of Fail2Ban bans and the verdicts of publishers of known behavior —
// honest ones and eight kinds of adversary — through the real store,
// allow-list and decision engine of one observer node in virtual time. It
// scores the bans the engine enforces against the trace's ground truth,
// hour by hour and cumulatively, and writes the versioned report of a
// scenario over 20 seeds. `make sim-trust SCENARIO=…` runs it; the v0.1
// baseline is in documentation/validation/trust/.
package simtrust
