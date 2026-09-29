package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/config"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/internal/verdicts"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// Endpoints for this node's verdicts (ADR 0012).
const (
	// ReportsPath turns a local detection into a verdict.
	ReportsPath = "/v1/reports"
	// RevocationsPath revokes this node's verdicts.
	RevocationsPath = "/v1/revocations"
	// IndicatorsPath lists the indicators with active verdicts;
	// IndicatorsPath + "/{indicator}" shows the active verdicts on one.
	IndicatorsPath = "/v1/indicators"
)

// maxRequestBody bounds a request body; evidence lines make up most of it.
const maxRequestBody = 1 << 20

// VerdictService issues and lists this node's verdicts; verdicts.Service
// implements it.
type VerdictService interface {
	// PeerID returns the peer ID this node publishes under.
	PeerID() string
	Report(ctx context.Context, r verdicts.Report) (verdicts.Result, error)
	Revoke(ctx context.Context, r verdicts.Revocation) ([]*obieproto.Event, error)
	List(publisher string, page store.Page) (store.IndicatorPage, error)
	Lookup(ind obieproto.Indicator) ([]*obieproto.Event, error)
}

// TTL is a verdict lifetime in a request: a JSON number of seconds or a
// duration string such as "12h" or "7d". Zero means the default.
type TTL time.Duration

// UnmarshalJSON accepts whole seconds or a duration string.
func (t *TTL) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		d, err := config.ParseDuration(s)
		if err != nil {
			return err
		}
		*t = TTL(d)
		return nil
	}
	var secs float64
	if err := json.Unmarshal(data, &secs); err != nil {
		return errors.New("ttl must be a number of seconds or a duration string like \"12h\" or \"7d\"")
	}
	if secs != math.Trunc(secs) || math.Abs(secs) > math.MaxInt64/float64(time.Second) {
		return fmt.Errorf("ttl %v is not a whole number of seconds in range", secs)
	}
	*t = TTL(time.Duration(secs) * time.Second)
	return nil
}

// MarshalJSON writes the TTL as a duration string.
func (t TTL) MarshalJSON() ([]byte, error) {
	return json.Marshal(config.Duration(t).String())
}

// ReportRequest is the JSON body of POST /v1/reports. Exactly one of IP and
// CIDR names the indicator.
type ReportRequest struct {
	IP   string `json:"ip,omitempty"`
	CIDR string `json:"cidr,omitempty"`
	// Protocol is the attacked service, e.g. "ssh".
	Protocol string `json:"protocol"`
	// Reason classifies the behavior, e.g. "password_bruteforce".
	Reason string `json:"reason"`
	// Events is the number of malicious events observed (at least 1).
	Events int64 `json:"events"`
	// EvidenceLines is the log excerpt behind the report. obied only keeps
	// its SHA-256 hash (evidence.log_hash); the lines are never stored or
	// sent.
	EvidenceLines []string `json:"evidence_lines,omitempty"`
	// Confidence is in [0, 1]; 0.8 when omitted.
	Confidence *float64 `json:"confidence,omitempty"`
	// TTL is the verdict lifetime; decision.default_ttl when omitted,
	// capped at decision.max_ttl.
	TTL TTL `json:"ttl,omitempty"`
	// Action is "ban" (the default) or "watch".
	Action string `json:"action,omitempty"`
	// MITRE lists MITRE ATT&CK technique IDs, e.g. "T1110".
	MITRE []string `json:"mitre,omitempty"`
}

// ReportResponse is the JSON body answering POST /v1/reports.
type ReportResponse struct {
	// Event is the issued verdict, or the current one if Coalesced.
	Event *obieproto.Event `json:"event"`
	// Coalesced is set when no new verdict was issued because this node
	// issued one on the indicator less than a minute ago; the report's
	// events are added to the next refresh.
	Coalesced bool `json:"coalesced"`
	// Supersedes is the ID of the verdict the new one refreshes.
	Supersedes string `json:"supersedes,omitempty"`
}

// RevocationRequest is the JSON body of POST /v1/revocations: exactly one
// of EventID and Indicator (an address, CIDR range or indicator key).
type RevocationRequest struct {
	EventID   string `json:"event_id,omitempty"`
	Indicator string `json:"indicator,omitempty"`
	// Reason explains the revocation, e.g. "false_positive".
	Reason string `json:"reason"`
}

// RevocationsResponse is the JSON body answering POST /v1/revocations.
type RevocationsResponse struct {
	Revocations []*obieproto.Event `json:"revocations"`
}

// IndicatorResponse is an indicator with its active verdicts: the JSON body
// of GET /v1/indicators/{indicator} and an item of GET /v1/indicators.
type IndicatorResponse struct {
	Indicator obieproto.Indicator `json:"indicator"`
	// Verdicts are the active verdicts, one per publisher, by peer ID.
	Verdicts []VerdictResponse `json:"verdicts"`
}

// VerdictResponse is one active verdict.
type VerdictResponse struct {
	// Local is set for this node's own verdicts.
	Local bool             `json:"local"`
	Event *obieproto.Event `json:"event"`
}

// IndicatorsResponse is the JSON body of GET /v1/indicators.
type IndicatorsResponse struct {
	Indicators []IndicatorResponse `json:"indicators"`
	// NextCursor is the cursor of the next page; empty on the last page.
	NextCursor string `json:"next_cursor,omitempty"`
}

// IndicatorsQuery selects a page of GET /v1/indicators.
type IndicatorsQuery struct {
	// Publisher restricts the list to indicators with a verdict by this
	// peer ID.
	Publisher string
	// Limit is the page size; the daemon's default when 0.
	Limit int
	// Cursor is the NextCursor of the previous page.
	Cursor string
}

// handleVerdicts registers the verdict endpoints on mux.
func handleVerdicts(mux *http.ServeMux, info Info, log *slog.Logger) {
	svc := info.Verdicts
	available := func(w http.ResponseWriter) bool {
		if svc == nil {
			http.Error(w, "verdict reporting is not available", http.StatusServiceUnavailable)
			return false
		}
		return true
	}
	mux.HandleFunc("POST "+ReportsPath, func(w http.ResponseWriter, r *http.Request) {
		var req ReportRequest
		if !decodeRequest(w, r, &req) || !available(w) {
			return
		}
		report, err := req.Check()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		res, err := svc.Report(withOrigin(r), report)
		if err != nil {
			writeServiceError(w, "reporting", err, log)
			return
		}
		status := http.StatusCreated
		if res.Coalesced {
			status = http.StatusOK
		}
		writeJSONStatus(w, status, ReportResponse{Event: res.Event, Coalesced: res.Coalesced, Supersedes: res.Supersedes}, log)
	})
	mux.HandleFunc("POST "+RevocationsPath, func(w http.ResponseWriter, r *http.Request) {
		var req RevocationRequest
		if !decodeRequest(w, r, &req) || !available(w) {
			return
		}
		rev, err := req.Check()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		events, err := svc.Revoke(withOrigin(r), rev)
		if err != nil {
			writeServiceError(w, "revoking", err, log)
			return
		}
		writeJSONStatus(w, http.StatusCreated, RevocationsResponse{Revocations: events}, log)
	})
	mux.HandleFunc("GET "+IndicatorsPath, func(w http.ResponseWriter, r *http.Request) {
		publisher, page, err := indicatorsQuery(r.URL.Query())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if !available(w) {
			return
		}
		res, err := svc.List(publisher, page)
		if err != nil {
			writeServiceError(w, "listing indicators", err, log)
			return
		}
		resp := IndicatorsResponse{Indicators: make([]IndicatorResponse, len(res.Items)), NextCursor: res.Next}
		for i, item := range res.Items {
			resp.Indicators[i] = indicatorResponse(item.Indicator, item.Verdicts, svc.PeerID())
		}
		writeJSON(w, resp, log)
	})
	mux.HandleFunc("GET "+IndicatorsPath+"/{indicator...}", func(w http.ResponseWriter, r *http.Request) {
		ind, err := ParseIndicator(r.PathValue("indicator"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if !available(w) {
			return
		}
		events, err := svc.Lookup(ind)
		if err != nil {
			writeServiceError(w, "looking up the indicator", err, log)
			return
		}
		writeJSON(w, indicatorResponse(ind, events, svc.PeerID()), log)
	})
}

// decodeRequest decodes a JSON request body of at most maxRequestBody
// bytes into v; see decodeLimited.
func decodeRequest(w http.ResponseWriter, r *http.Request, v any) bool {
	return decodeLimited(w, r, v, maxRequestBody)
}

// decodeLimited decodes a JSON request body of at most limit bytes into v,
// answering 413 for a larger and 400 for a malformed one. Error messages
// never quote the body, which may carry evidence lines.
func decodeLimited(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	dec.DisallowUnknownFields()
	err := dec.Decode(v)
	if err == nil && dec.More() {
		err = errors.New("trailing data after the JSON object")
	}
	if err == nil {
		return true
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		http.Error(w, fmt.Sprintf("request body exceeds %d bytes", limit), http.StatusRequestEntityTooLarge)
		return false
	}
	http.Error(w, "invalid JSON body: "+jsonProblem(err), http.StatusBadRequest)
	return false
}

// jsonProblem describes a decoding error without quoting input values.
func jsonProblem(err error) string {
	var typeErr *json.UnmarshalTypeError
	var syntaxErr *json.SyntaxError
	switch {
	case errors.As(err, &typeErr):
		return fmt.Sprintf("field %q must be of type %s", typeErr.Field, typeErr.Type)
	case errors.As(err, &syntaxErr):
		return fmt.Sprintf("syntax error at byte %d", syntaxErr.Offset)
	case errors.Is(err, io.EOF):
		return "empty body"
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		return strings.TrimPrefix(err.Error(), "json: ")
	default:
		return err.Error()
	}
}

// Check checks the request and converts it, as the admin API and the
// console do before reporting; errors name the field.
func (req *ReportRequest) Check() (verdicts.Report, error) {
	ind, err := reportIndicator(req.IP, req.CIDR)
	if err != nil {
		return verdicts.Report{}, err
	}
	switch {
	case req.Protocol == "":
		return verdicts.Report{}, errors.New("protocol: missing, e.g. \"ssh\"")
	case req.Reason == "":
		return verdicts.Report{}, errors.New("reason: missing, e.g. \"password_bruteforce\"")
	case req.Events < 1:
		return verdicts.Report{}, errors.New("events: must be at least 1")
	case req.Action != "" && req.Action != obieproto.ActionBan && req.Action != obieproto.ActionWatch:
		return verdicts.Report{}, fmt.Errorf("action: %q must be %q or %q", req.Action, obieproto.ActionBan, obieproto.ActionWatch)
	case req.Confidence != nil && !(*req.Confidence >= 0 && *req.Confidence <= 1):
		return verdicts.Report{}, fmt.Errorf("confidence: %v must be in [0, 1]", *req.Confidence)
	}
	return verdicts.Report{
		Indicator: ind, Protocol: req.Protocol, Reason: req.Reason, Events: req.Events,
		EvidenceLines: req.EvidenceLines, Confidence: req.Confidence, TTL: time.Duration(req.TTL),
		Action: req.Action, MITRE: req.MITRE,
	}, nil
}

// reportIndicator parses the ip or cidr of a report.
func reportIndicator(ip, cidr string) (obieproto.Indicator, error) {
	switch {
	case ip == "" && cidr == "":
		return obieproto.Indicator{}, errors.New("ip or cidr: missing; give the attacker's address or range")
	case ip != "" && cidr != "":
		return obieproto.Indicator{}, errors.New("ip and cidr: give only one of them")
	case cidr != "":
		ind, err := ParseIndicator(cidr)
		if err != nil {
			return obieproto.Indicator{}, fmt.Errorf("cidr: %w", err)
		}
		return ind, nil
	}
	ind, err := ParseIndicator(ip)
	if err != nil {
		return obieproto.Indicator{}, fmt.Errorf("ip: %w", err)
	}
	if ind.Kind == obieproto.KindCIDR {
		return obieproto.Indicator{}, fmt.Errorf("ip: %q is a range; use cidr for ranges", ip)
	}
	return ind, nil
}

// Check checks the request and converts it, as the admin API and the
// console do before revoking.
func (req *RevocationRequest) Check() (verdicts.Revocation, error) {
	switch {
	case (req.EventID == "") == (req.Indicator == ""):
		return verdicts.Revocation{}, errors.New("event_id or indicator: give exactly one of them")
	case req.Reason == "":
		return verdicts.Revocation{}, errors.New("reason: missing, e.g. \"false_positive\"")
	}
	rev := verdicts.Revocation{EventID: req.EventID, Reason: req.Reason}
	if req.Indicator != "" {
		ind, err := ParseIndicator(req.Indicator)
		if err != nil {
			return verdicts.Revocation{}, fmt.Errorf("indicator: %w", err)
		}
		rev.Indicator = &ind
	}
	return rev, nil
}

// indicatorsQuery parses the query of GET /v1/indicators.
func indicatorsQuery(q url.Values) (publisher string, page store.Page, err error) {
	if active := q.Get("active"); active != "" && active != "true" {
		return "", store.Page{}, fmt.Errorf("active: %q is not supported; obied keeps only active verdicts, so omit active or set it to true", active)
	}
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > store.MaxPageLimit {
			return "", store.Page{}, fmt.Errorf("limit: %q must be a whole number from 1 to %d", s, store.MaxPageLimit)
		}
		page.Limit = n
	}
	page.After = q.Get("cursor")
	return q.Get("publisher"), page, nil
}

func indicatorResponse(ind obieproto.Indicator, events []*obieproto.Event, self string) IndicatorResponse {
	resp := IndicatorResponse{Indicator: ind, Verdicts: make([]VerdictResponse, len(events))}
	for i, ev := range events {
		resp.Verdicts[i] = VerdictResponse{Local: ev.Publisher.PeerID == self, Event: ev}
	}
	return resp
}

// writeServiceError answers a failed verdict service call: 400, 422 and 404
// with the explanation, 500 for anything else.
func writeServiceError(w http.ResponseWriter, action string, err error, log *slog.Logger) {
	switch {
	case errors.Is(err, verdicts.ErrInvalid):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, verdicts.ErrRefused):
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
	case errors.Is(err, verdicts.ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	default:
		log.Error("verdict request failed", "action", action, "error", err)
		http.Error(w, action+" failed; see the obied log", http.StatusInternalServerError)
	}
}

func writeJSONStatus(w http.ResponseWriter, status int, v any, log *slog.Logger) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		log.Error("encoding response", "error", err)
		http.Error(w, "encoding the response failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(buf.Bytes()); err != nil {
		log.Debug("writing response", "error", err)
	}
}

// Report sends a report and returns the issued (or coalesced) verdict.
func (c *Client) Report(ctx context.Context, req ReportRequest) (*ReportResponse, error) {
	var resp ReportResponse
	if err := c.post(ctx, ReportsPath, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Revoke revokes this node's verdicts named by req.
func (c *Client) Revoke(ctx context.Context, req RevocationRequest) (*RevocationsResponse, error) {
	var resp RevocationsResponse
	if err := c.post(ctx, RevocationsPath, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Indicators fetches a page of the indicators with active verdicts.
func (c *Client) Indicators(ctx context.Context, q IndicatorsQuery) (*IndicatorsResponse, error) {
	values := url.Values{"active": {"true"}}
	if q.Publisher != "" {
		values.Set("publisher", q.Publisher)
	}
	if q.Limit != 0 {
		values.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Cursor != "" {
		values.Set("cursor", q.Cursor)
	}
	var resp IndicatorsResponse
	if err := c.get(ctx, IndicatorsPath+"?"+values.Encode(), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Indicator fetches the active verdicts on indicator.
func (c *Client) Indicator(ctx context.Context, indicator string) (*IndicatorResponse, error) {
	if strings.TrimSpace(indicator) == "" {
		return nil, errors.New("missing indicator")
	}
	var resp IndicatorResponse
	if err := c.get(ctx, IndicatorsPath+"/"+url.PathEscape(indicator), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
