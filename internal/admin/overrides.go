package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// OverridesPath lists and sets the operator overrides;
// OverridesPath + "/{indicator}" deletes one.
const OverridesPath = "/v1/overrides"

// Override actions.
const (
	ActionForceAllow = "force_allow"
	ActionForceBlock = "force_block"
)

// maxOverrideBody bounds the body of an override request to the admin API.
const maxOverrideBody = 16 << 10

// MaxTTLSeconds is the longest override TTL, the most a time.Duration
// holds.
const MaxTTLSeconds = int64(math.MaxInt64 / time.Second)

// ErrInvalid marks an override the node refuses, e.g. a note that is too
// long; the API answers 400.
var ErrInvalid = errors.New("invalid override")

// ErrNoOverride is returned by Client.DeleteOverride when the indicator has
// no override.
var ErrNoOverride = errors.New("no override")

// Overrides manages the operator overrides.
type Overrides interface {
	// List returns the overrides in effect, ordered by indicator key.
	List() ([]OverrideResponse, error)
	// Set stores or replaces the override of ind; ttl 0 means no expiry.
	// Errors wrapping ErrInvalid are the operator's mistake. ctx carries
	// the origin of the change for the audit trail (audit.WithOrigin).
	Set(ctx context.Context, ind obieproto.Indicator, action string, ttl time.Duration, note string) (OverrideResponse, error)
	// Delete removes the override of ind and reports whether it existed;
	// ctx carries the origin, as for Set.
	Delete(ctx context.Context, ind obieproto.Indicator) (bool, error)
}

// OverrideRequest is the JSON body of POST /v1/overrides.
type OverrideRequest struct {
	// Indicator is an IP address, a CIDR range or an indicator key.
	Indicator string `json:"indicator"`
	// Action is ActionForceAllow or ActionForceBlock.
	Action string `json:"action"`
	// TTLSeconds is how long the override lasts; 0 means until deleted.
	TTLSeconds int64  `json:"ttl_seconds,omitempty"`
	Note       string `json:"note,omitempty"`
}

// OverrideResponse is one operator override.
type OverrideResponse struct {
	Indicator obieproto.Indicator `json:"indicator"`
	Action    string              `json:"action"`
	Note      string              `json:"note,omitempty"`
	CreatedAt time.Time           `json:"created_at"`
	// ExpiresAt is when the override ends; omitted if it does not.
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// OverridesResponse is the JSON body of GET /v1/overrides.
type OverridesResponse struct {
	Overrides []OverrideResponse `json:"overrides"`
}

// OverrideResult is the JSON body of POST /v1/overrides and
// DELETE /v1/overrides/{indicator}: the override that was set (omitted on
// delete) and the resulting decision on its indicator, if the decision
// engine is available.
type OverrideResult struct {
	Override *OverrideResponse `json:"override,omitempty"`
	Decision *DecisionResponse `json:"decision,omitempty"`
	// Warning is set when a force-block does not take effect, e.g. on
	// an address of the built-in allow-list.
	Warning string `json:"warning,omitempty"`
}

// handleOverrides registers the override endpoints on mux.
func handleOverrides(mux *http.ServeMux, info Info, log *slog.Logger) {
	available := func(w http.ResponseWriter) bool {
		if info.Overrides == nil {
			http.Error(w, "overrides are not available", http.StatusServiceUnavailable)
			return false
		}
		return true
	}
	mux.HandleFunc("GET "+OverridesPath, func(w http.ResponseWriter, _ *http.Request) {
		if !available(w) {
			return
		}
		list, err := info.Overrides.List()
		if err != nil {
			log.Error("listing overrides failed", "error", err)
			http.Error(w, "listing the overrides failed; see the obied log", http.StatusInternalServerError)
			return
		}
		writeJSON(w, OverridesResponse{Overrides: append([]OverrideResponse{}, list...)}, log)
	})
	mux.HandleFunc("POST "+OverridesPath, func(w http.ResponseWriter, r *http.Request) {
		if !available(w) {
			return
		}
		var req OverrideRequest
		if !decodeLimited(w, r, &req, maxOverrideBody) {
			return
		}
		ind, err := req.Check()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		o, err := info.Overrides.Set(withOrigin(r), ind, req.Action, time.Duration(req.TTLSeconds)*time.Second, req.Note)
		switch {
		case errors.Is(err, ErrInvalid):
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		case err != nil:
			log.Error("setting an override failed", "indicator", ind.Key(), "error", err)
			http.Error(w, "setting the override failed; see the obied log", http.StatusInternalServerError)
			return
		}
		log.Info("operator override set", "indicator", ind.Key(), "action", o.Action, "expires_at", o.ExpiresAt, "note", o.Note)
		res := OverrideResult{Override: &o, Decision: explainAfter(info, ind, log)}
		res.Warning = OverrideWarning(o.Action, res.Decision)
		writeJSON(w, res, log)
	})
	mux.HandleFunc("DELETE "+OverridesPath+"/{indicator...}", func(w http.ResponseWriter, r *http.Request) {
		if !available(w) {
			return
		}
		ind, err := ParseIndicator(r.PathValue("indicator"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		deleted, err := info.Overrides.Delete(withOrigin(r), ind)
		switch {
		case err != nil:
			log.Error("deleting an override failed", "indicator", ind.Key(), "error", err)
			http.Error(w, "deleting the override failed; see the obied log", http.StatusInternalServerError)
			return
		case !deleted:
			http.Error(w, fmt.Sprintf("%s: %s", ErrNoOverride, ind.Key()), http.StatusNotFound)
			return
		}
		log.Info("operator override deleted", "indicator", ind.Key())
		writeJSON(w, OverrideResult{Decision: explainAfter(info, ind, log)}, log)
	})
}

// OverrideWarning returns the warning for an override of action that
// leaves the decision d on its indicator: a force-block that does not
// take effect, e.g. on an address of the built-in allow-list. It is empty
// if there is nothing to warn about or d is nil.
func OverrideWarning(action string, d *DecisionResponse) string {
	if action != ActionForceBlock || d == nil || d.State == StateBlock {
		return ""
	}
	return "the force-block does not take effect: " + d.Reason
}

// Check checks the body of POST /v1/overrides, as the admin API and the
// console do before setting an override, and returns its indicator.
func (req *OverrideRequest) Check() (obieproto.Indicator, error) {
	if req.Action != ActionForceAllow && req.Action != ActionForceBlock {
		return obieproto.Indicator{}, fmt.Errorf("invalid action %q: want %s or %s", req.Action, ActionForceAllow, ActionForceBlock)
	}
	if req.TTLSeconds < 0 || req.TTLSeconds > MaxTTLSeconds {
		return obieproto.Indicator{}, fmt.Errorf("invalid ttl_seconds %d: must be between 0 and %d", req.TTLSeconds, MaxTTLSeconds)
	}
	return ParseIndicator(req.Indicator)
}

// explainAfter returns the decision on ind after an override change, or
// nil if it cannot be explained; the change itself succeeded either way.
func explainAfter(info Info, ind obieproto.Indicator, log *slog.Logger) *DecisionResponse {
	if info.Explain == nil {
		return nil
	}
	d, err := info.Explain(ind)
	if err != nil {
		log.Warn("explaining the decision after an override change failed", "indicator", ind.Key(), "error", err)
		return nil
	}
	return &d
}

// Overrides fetches the overrides in effect.
func (c *Client) Overrides(ctx context.Context) (*OverridesResponse, error) {
	var resp OverridesResponse
	if err := c.get(ctx, OverridesPath, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SetOverride stores or replaces an override.
func (c *Client) SetOverride(ctx context.Context, req OverrideRequest) (*OverrideResult, error) {
	if strings.TrimSpace(req.Indicator) == "" {
		return nil, errors.New("missing indicator")
	}
	var resp OverrideResult
	if err := c.do(ctx, http.MethodPost, OverridesPath, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DeleteOverride removes the override of indicator; it returns an error
// wrapping ErrNoOverride if there is none.
func (c *Client) DeleteOverride(ctx context.Context, indicator string) (*OverrideResult, error) {
	if strings.TrimSpace(indicator) == "" {
		return nil, errors.New("missing indicator")
	}
	var resp OverrideResult
	err := c.do(ctx, http.MethodDelete, OverridesPath+"/"+url.PathEscape(indicator), nil, &resp)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w on %s", ErrNoOverride, indicator)
	}
	if err != nil {
		return nil, err
	}
	return &resp, nil
}
