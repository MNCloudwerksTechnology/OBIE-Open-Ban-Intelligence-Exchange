# ADR 0028: Interactive three-node demo on the landing page

- **Status:** Accepted
- **Date:** 2026-09-29
- **Work package:** [#1759](https://openproject.niew.dev/work_packages/1759)
- **Amends:** [ADR 0012](0012-landing-page-content-and-design-system.md) (content file),
  [ADR 0015](0015-website-seo-and-delivery.md) (delivery)

## Context

The "How it works" section explains OBIE in text and a static diagram.
Newcomers still cannot picture why sharing reports does not mean that one
server can make another block someone. The section gets a demo that the
visitor steps through: three servers, two attackers, a rogue participant
and an innocent address, told in ten steps. Its numbers must follow the
decision rule of the current release, the page must keep working without
JavaScript, with reduced motion and for search engines, stay within the
performance budget and make no third-party request.

## Decision

- **The demo computes its numbers.** A TypeScript copy of the node's
  decision rule ([ADR 0011](0011-trust-weighted-decision.md),
  [ADR 0013](0013-local-sovereignty.md)) is a pure function: score = Σ
  weight × confidence over the latest `ban` verdict of each distinct
  publisher with a weight above 0, block when the score reaches the
  threshold (1e-9 tolerance) and the quorum is met, own verdicts block at
  once (local autoblock), the safety list always wins. The scenario is data:
  each server's settings and trust, and per step the events (detections,
  reports, the rogue's flood, a revocation, an expiry). The state shown at
  step *n* is the replay of the events of steps 1 to *n*, so jumping,
  going back and rapid clicks always show a consistent state.
- **Tests pin the numbers to the documentation.** The scenario's settings
  are checked against `documentation/examples/obie.yaml`, the Fail2Ban
  confidence and the federation guide; where the demo deviates (threshold
  1.2 instead of 1.8, as the federation guide suggests for three to five
  nodes, and servers that block instead of only observing), the demo says
  so. Tests fix the outcome of every step: who blocks, who only watches.
  The rule itself is a copy: its tests mirror the cases of the Go engine,
  but nothing compares the two automatically.
- **Copy in the content file, behaviour in the demo.** Captions, labels and
  the settings sentence live in `landing.content.ts` under
  `howItWorks.demo` (ADR 0012), so the operator reviews and changes the
  wording in one place. Addresses, weights, confidences and events live in
  the demo's scenario module.
- **Progressive enhancement.** Prerendered, the demo is an ordered list of
  the ten steps with their captions; that is what visitors without
  JavaScript and search engines get. After hydration it becomes
  interactive, and the same list stays available as a text version. The
  component is in a `@defer (hydrate on idle)` block, so its code is a lazy
  chunk and the initial JavaScript grows only by the demo's copy. The
  interactive layout has another height than the list; when the demo is
  above the viewport at the switch (a link to `/#contact`), the page scrolls
  by the difference, so the visitor's target stays in place.
- **No library, no canvas.** The map is inline SVG, hidden from assistive
  technology; the state of every server is HTML text (state, score,
  reporters). Animation is CSS only and runs only with
  `prefers-reduced-motion: no-preference`; nothing waits for an animation
  to end.
- **Autoplay only on request.** It never starts on its own; it shows a step
  for 0.4 s per word of its text (at least 8 s), and stops at the last
  step, on any manual navigation, when the demo leaves the viewport
  (IntersectionObserver) and when the tab is hidden. The demo stores
  nothing and reports nothing: no cookies, no storage, no requests.

## Alternatives considered

- **Hard-coded states per step** — simpler, but nothing would catch a
  number that does not add up or a rule that changed; rejected.
- **An animation or diagram library** — adds weight and possibly
  third-party requests for a single, small visual; rejected.
- **A video or animated image** — no text state, no keyboard control, no
  reduced motion, and heavy; rejected.

## Consequences

- When the node's documented defaults change, the demo's tests fail until
  the demo is updated, which is intended.
- The rule exists twice (Go and TypeScript). The TypeScript copy only
  serves the illustration and never decides anything on a node; a change
  to `internal/decision` has to be carried over by hand.
- A visitor who looks at the demo at the moment it hydrates sees the list
  turn into the interactive demo. Hydration comes when the browser is idle,
  shortly after load, while the demo is usually still below the fold.
