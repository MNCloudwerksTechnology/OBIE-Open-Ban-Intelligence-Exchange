package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// DecisionsPath lists the decisions; DecisionsPath + "/{indicator}" explains
// one.
const DecisionsPath = "/v1/decisions"

// Decision states accepted by the state filter of GET /v1/decisions.
const (
	StateBlock = "block"
	StateNone  = "none"
)

// DecisionResponse is the decision on one indicator: the JSON body of
// GET /v1/decisions/{indicator} and an item of GET /v1/decisions (there
// without Publishers and Sovereignty).
type DecisionResponse struct {
	Indicator obieproto.Indicator `json:"indicator"`
	// State is StateBlock or StateNone.
	State string `json:"state"`
	// Score is the sum of weight × confidence over the contributing
	// publishers.
	Score     float64 `json:"score"`
	Threshold float64 `json:"threshold"`
	// Contributors counts the publishers with an active ban verdict and a
	// weight above 0.
	Contributors int `json:"contributors"`
	Quorum       int `json:"quorum"`
	// LocalAutoblock is set when this node's own verdict alone blocks.
	LocalAutoblock bool `json:"local_autoblock"`
	// ExpiresAt is when a block ends; omitted for StateNone.
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	Reason      string     `json:"reason"`
	EvaluatedAt time.Time  `json:"evaluated_at"`
	// Publishers lists every active verdict on the indicator.
	Publishers []ContributionResponse `json:"publishers,omitempty"`
	// Sovereignty reports the effect of the allow-list and overrides.
	Sovereignty *SovereigntyResponse `json:"sovereignty,omitempty"`
}

// ContributionResponse is one publisher's verdict within a decision.
type ContributionResponse struct {
	PeerID string `json:"peer_id"`
	// Name is the publisher's name in trust.publishers; empty if unlisted.
	Name string `json:"name,omitempty"`
	// Local is set for this node's own verdicts.
	Local      bool    `json:"local"`
	EventID    string  `json:"event_id"`
	Action     string  `json:"action"`
	Weight     float64 `json:"weight"`
	Confidence float64 `json:"confidence"`
	// Score is weight × confidence if the verdict contributes, else 0.
	Score       float64   `json:"score"`
	Contributes bool      `json:"contributes"`
	IssuedAt    time.Time `json:"issued_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// SovereigntyResponse reports how the allow-list and operator overrides
// affect a decision.
type SovereigntyResponse struct {
	// Applied is false while they are not evaluated.
	Applied bool   `json:"applied"`
	Note    string `json:"note"`
}

// DecisionsResponse is the JSON body of GET /v1/decisions.
type DecisionsResponse struct {
	Decisions []DecisionResponse `json:"decisions"`
}

// ParseIndicator parses what an operator types for an indicator: an IPv4 or
// IPv6 address, a CIDR range (a full-length prefix is the single address),
// or an indicator key such as "ipv4:203.0.113.7". The result is normalized.
func ParseIndicator(s string) (obieproto.Indicator, error) {
	s = strings.TrimSpace(s)
	var ind obieproto.Indicator
	if kind, value, ok := strings.Cut(s, ":"); ok && (kind == obieproto.KindIPv4 || kind == obieproto.KindIPv6 || kind == obieproto.KindCIDR) {
		ind = obieproto.Indicator{Kind: kind, Value: value}
	} else {
		var err error
		if ind, err = indicatorOf(s); err != nil {
			return obieproto.Indicator{}, err
		}
	}
	if err := ind.Normalize(); err != nil {
		return obieproto.Indicator{}, fmt.Errorf("invalid indicator %q: %w", s, err)
	}
	return ind, nil
}

// indicatorOf derives the kind of a bare address or CIDR range.
func indicatorOf(s string) (obieproto.Indicator, error) {
	prefix, err := netip.ParsePrefix(s)
	if err != nil {
		addr, addrErr := netip.ParseAddr(s)
		if addrErr != nil || addr.Zone() != "" {
			return obieproto.Indicator{}, fmt.Errorf("invalid indicator %q: want an IP address or CIDR range", s)
		}
		prefix = netip.PrefixFrom(addr, addr.BitLen())
	}
	switch {
	case prefix.Bits() < prefix.Addr().BitLen():
		return obieproto.Indicator{Kind: obieproto.KindCIDR, Value: prefix.String()}, nil
	case prefix.Addr().Is4():
		return obieproto.Indicator{Kind: obieproto.KindIPv4, Value: prefix.Addr().String()}, nil
	default:
		return obieproto.Indicator{Kind: obieproto.KindIPv6, Value: prefix.Addr().String()}, nil
	}
}

// handleDecisions registers the decision endpoints on mux.
func handleDecisions(mux *http.ServeMux, info Info, log *slog.Logger) {
	mux.HandleFunc("GET "+DecisionsPath, func(w http.ResponseWriter, r *http.Request) {
		state := r.URL.Query().Get("state")
		if state != "" && state != StateBlock && state != StateNone {
			http.Error(w, fmt.Sprintf("invalid state %q: want %s or %s", state, StateBlock, StateNone), http.StatusBadRequest)
			return
		}
		resp := DecisionsResponse{Decisions: []DecisionResponse{}}
		if info.Decisions != nil {
			resp.Decisions = append(resp.Decisions, info.Decisions(state)...)
		}
		writeJSON(w, resp, log)
	})
	mux.HandleFunc("GET "+DecisionsPath+"/{indicator...}", func(w http.ResponseWriter, r *http.Request) {
		ind, err := ParseIndicator(r.PathValue("indicator"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if info.Explain == nil {
			http.Error(w, "the decision engine is not available", http.StatusServiceUnavailable)
			return
		}
		resp, err := info.Explain(ind)
		if err != nil {
			log.Error("explaining a decision failed", "indicator", ind.Key(), "error", err)
			http.Error(w, "explaining the decision failed; see the obied log", http.StatusInternalServerError)
			return
		}
		writeJSON(w, resp, log)
	})
}

// Explain fetches the explained decision on indicator.
func (c *Client) Explain(ctx context.Context, indicator string) (*DecisionResponse, error) {
	if strings.TrimSpace(indicator) == "" {
		return nil, errors.New("missing indicator")
	}
	var resp DecisionResponse
	if err := c.get(ctx, DecisionsPath+"/"+url.PathEscape(indicator), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Decisions fetches the decisions in state, or all for "".
func (c *Client) Decisions(ctx context.Context, state string) (*DecisionsResponse, error) {
	path := DecisionsPath
	if state != "" {
		path += "?" + url.Values{"state": {state}}.Encode()
	}
	var resp DecisionsResponse
	if err := c.get(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
