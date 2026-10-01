package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MNCloudwerksTechnology/obie/internal/identity"
	"github.com/MNCloudwerksTechnology/obie/internal/lifecycle"
	"github.com/MNCloudwerksTechnology/obie/internal/store"
	"github.com/MNCloudwerksTechnology/obie/internal/verdicts"
	"github.com/MNCloudwerksTechnology/obie/pkg/obieproto"
)

// storePublisher stores events like Mesh.Publish, or fails with fail.
type storePublisher struct {
	db   *store.DB
	fail error
}

func (p *storePublisher) Publish(_ context.Context, ev *obieproto.Event) error {
	if p.fail != nil {
		return p.fail
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	checked, err := obieproto.Receive(data)
	if err != nil {
		return err
	}
	_, err = p.db.Put(checked)
	return err
}

type verdictFixture struct {
	handler http.Handler
	info    Info
	db      *store.DB
	pub     *storePublisher
	key     *identity.Key
	logs    *syncBuffer
}

// syncBuffer is a bytes.Buffer the store's goroutines can log into while
// the test reads it; badger logs after it has released the writer.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// newVerdictFixture returns the admin handler backed by a real verdict
// service and store; allow-listed is 85.20.0.0/16. Service and admin API
// log into one buffer.
func newVerdictFixture(t *testing.T) *verdictFixture {
	t.Helper()
	dir := t.TempDir()
	key, err := identity.Create(filepath.Join(dir, "state"), false)
	if err != nil {
		t.Fatal(err)
	}
	var logs syncBuffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	db := store.New(filepath.Join(dir, "db"), log, store.Options{})
	if err := db.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Stop(context.Background()) })
	pub := &storePublisher{db: db}
	svc := verdicts.New(verdicts.Options{
		Store: db, Publisher: pub, Signer: key,
		DefaultTTL: 7 * 24 * time.Hour, MaxTTL: 30 * 24 * time.Hour,
		Allowlist: []netip.Prefix{netip.MustParsePrefix("85.20.0.0/16")},
	}, log)
	info := testInfo(lifecycle.Status{Name: "admin", State: lifecycle.StateRunning, Ready: true})
	info.Verdicts = svc
	return &verdictFixture{handler: Handler(info, log), info: info, db: db, pub: pub, key: key, logs: &logs}
}

func (f *verdictFixture) do(t *testing.T, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, httptest.NewRequest(method, target, strings.NewReader(body)))
	return rec
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, rec.Body.String())
	}
	return v
}

const sshBody = `{"ip":"85.10.0.7","protocol":"ssh","reason":"password_bruteforce","events":5}`

func TestReportEndpoint(t *testing.T) {
	f := newVerdictFixture(t)
	rec := f.do(t, http.MethodPost, ReportsPath, sshBody)
	if rec.Code != http.StatusCreated || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("POST %s = %d %q: %s", ReportsPath, rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	resp := decodeBody[ReportResponse](t, rec)
	ev := resp.Event
	if resp.Coalesced || ev.Type != obieproto.TypeVerdict || ev.Indicator.Key() != "ipv4:85.10.0.7" ||
		ev.Verdict.SuggestedAction != obieproto.ActionBan || ev.Verdict.Confidence != 0.8 ||
		ev.Verdict.TTLSeconds != 7*86400 || ev.Publisher.PeerID != f.key.PeerID() {
		t.Errorf("report response = %+v, verdict %+v", resp, ev.Verdict)
	}
	if err := obieproto.Verify(ev); err != nil {
		t.Errorf("returned event signature: %v", err)
	}

	// A second report within the window is coalesced into the first.
	rec = f.do(t, http.MethodPost, ReportsPath, sshBody)
	if again := decodeBody[ReportResponse](t, rec); rec.Code != http.StatusOK || !again.Coalesced || again.Event.ID != ev.ID {
		t.Errorf("repeated report = %d %+v", rec.Code, again)
	}
}

func TestReportEndpointOptions(t *testing.T) {
	f := newVerdictFixture(t)
	for body, ttl := range map[string]int64{
		`{"cidr":"85.10.1.9/24","protocol":"http","reason":"scanning","events":3,"confidence":1,"ttl":"2h","action":"watch","mitre":["T1595"]}`: 7200,
		`{"ip":"2a02:1:2::7","protocol":"ssh","reason":"x","events":1,"ttl":900}`:                                                               900,
		`{"ip":"85.10.0.9","protocol":"ssh","reason":"x","events":1,"ttl":"365d"}`:                                                              30 * 86400,
	} {
		rec := f.do(t, http.MethodPost, ReportsPath, body)
		if rec.Code != http.StatusCreated {
			t.Errorf("POST %s = %d: %s", body, rec.Code, rec.Body.String())
			continue
		}
		if ev := decodeBody[ReportResponse](t, rec).Event; ev.Verdict.TTLSeconds != ttl {
			t.Errorf("POST %s: ttl_seconds = %d, want %d", body, ev.Verdict.TTLSeconds, ttl)
		}
	}
}

func TestReportEndpointValidation(t *testing.T) {
	f := newVerdictFixture(t)
	for name, tc := range map[string]struct {
		body string
		code int
		msg  string
	}{
		"empty body":       {``, http.StatusBadRequest, "empty body"},
		"not json":         {`ip=85.10.0.7`, http.StatusBadRequest, "syntax error"},
		"unknown field":    {`{"ip":"85.10.0.7","protocol":"ssh","reason":"x","events":1,"user":"root"}`, http.StatusBadRequest, `unknown field "user"`},
		"trailing data":    {sshBody + sshBody, http.StatusBadRequest, "trailing data"},
		"wrong type":       {`{"ip":"85.10.0.7","protocol":"ssh","reason":"x","events":"many"}`, http.StatusBadRequest, `field "events" must be of type int64`},
		"no indicator":     {`{"protocol":"ssh","reason":"x","events":1}`, http.StatusBadRequest, "ip or cidr: missing"},
		"ip and cidr":      {`{"ip":"85.10.0.7","cidr":"85.10.0.0/24","protocol":"ssh","reason":"x","events":1}`, http.StatusBadRequest, "only one"},
		"ip is a range":    {`{"ip":"85.10.0.0/24","protocol":"ssh","reason":"x","events":1}`, http.StatusBadRequest, "use cidr for ranges"},
		"bad ip":           {`{"ip":"host.example","protocol":"ssh","reason":"x","events":1}`, http.StatusBadRequest, "ip: invalid indicator"},
		"broad cidr":       {`{"cidr":"85.0.0.0/8","protocol":"ssh","reason":"x","events":1}`, http.StatusBadRequest, "cidr: invalid indicator"},
		"no protocol":      {`{"ip":"85.10.0.7","reason":"x","events":1}`, http.StatusBadRequest, "protocol: missing"},
		"bad protocol":     {`{"ip":"85.10.0.7","protocol":"SSH login","reason":"x","events":1}`, http.StatusBadRequest, "protocol"},
		"no reason":        {`{"ip":"85.10.0.7","protocol":"ssh","events":1}`, http.StatusBadRequest, "reason: missing"},
		"bad reason":       {`{"ip":"85.10.0.7","protocol":"ssh","reason":"Failed password for root","events":1}`, http.StatusBadRequest, "evidence.reason"},
		"no events":        {`{"ip":"85.10.0.7","protocol":"ssh","reason":"x"}`, http.StatusBadRequest, "events: must be at least 1"},
		"bad action":       {`{"ip":"85.10.0.7","protocol":"ssh","reason":"x","events":1,"action":"drop"}`, http.StatusBadRequest, "action"},
		"confidence":       {`{"ip":"85.10.0.7","protocol":"ssh","reason":"x","events":1,"confidence":1.2}`, http.StatusBadRequest, "confidence"},
		"ttl string":       {`{"ip":"85.10.0.7","protocol":"ssh","reason":"x","events":1,"ttl":"forever"}`, http.StatusBadRequest, "invalid duration"},
		"ttl fraction":     {`{"ip":"85.10.0.7","protocol":"ssh","reason":"x","events":1,"ttl":90.5}`, http.StatusBadRequest, "whole number of seconds"},
		"ttl type":         {`{"ip":"85.10.0.7","protocol":"ssh","reason":"x","events":1,"ttl":true}`, http.StatusBadRequest, "ttl must be"},
		"ttl too short":    {`{"ip":"85.10.0.7","protocol":"ssh","reason":"x","events":1,"ttl":30}`, http.StatusBadRequest, "minimum of 60s"},
		"bad mitre":        {`{"ip":"85.10.0.7","protocol":"ssh","reason":"x","events":1,"mitre":["brute"]}`, http.StatusBadRequest, "mitre"},
		"private address":  {`{"ip":"192.168.1.20","protocol":"ssh","reason":"x","events":1}`, http.StatusUnprocessableEntity, "not a public address"},
		"loopback":         {`{"ip":"::1","protocol":"ssh","reason":"x","events":1}`, http.StatusUnprocessableEntity, "not a public address"},
		"documentation":    {`{"ip":"203.0.113.7","protocol":"ssh","reason":"x","events":1}`, http.StatusUnprocessableEntity, "not a public address"},
		"allow-listed":     {`{"ip":"85.20.3.4","protocol":"ssh","reason":"x","events":1}`, http.StatusUnprocessableEntity, "allow-listed network 85.20.0.0/16"},
		"allow-list range": {`{"cidr":"85.20.3.0/24","protocol":"ssh","reason":"x","events":1}`, http.StatusUnprocessableEntity, "allow-listed"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := f.do(t, http.MethodPost, ReportsPath, tc.body)
			if rec.Code != tc.code || !strings.Contains(rec.Body.String(), tc.msg) {
				t.Errorf("POST %s = %d %q, want %d containing %q", tc.body, rec.Code, rec.Body.String(), tc.code, tc.msg)
			}
		})
	}
	page, err := f.db.ListIndicators(time.Now(), store.Filter{}, store.Page{})
	if err != nil || len(page.Items) != 0 {
		t.Errorf("rejected reports stored verdicts: %+v, %v", page.Items, err)
	}
}

func TestReportEndpointBodyLimit(t *testing.T) {
	f := newVerdictFixture(t)
	body := `{"ip":"85.10.0.7","protocol":"ssh","reason":"x","events":1,"evidence_lines":["` +
		strings.Repeat("a", maxRequestBody) + `"]}`
	if rec := f.do(t, http.MethodPost, ReportsPath, body); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized report = %d %q", rec.Code, rec.Body.String())
	}
}

// TestReportEvidenceStaysLocal checks that evidence lines appear neither in
// the response, the stored event nor the logs of the admin API and service.
func TestReportEvidenceStaysLocal(t *testing.T) {
	f := newVerdictFixture(t)
	const secret = "Failed password for invalid user secret-admin from 85.10.0.7 port 40022"
	body := `{"ip":"85.10.0.7","protocol":"ssh","reason":"password_bruteforce","events":2,` +
		`"evidence_lines":["Sep 28 12:00:01 ` + secret + `","Sep 28 12:00:02 second line"]}`
	rec := f.do(t, http.MethodPost, ReportsPath, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body.String())
	}
	ev := decodeBody[ReportResponse](t, rec).Event
	if !strings.HasPrefix(ev.Evidence.LogHash, "sha256:") {
		t.Errorf("log_hash = %q", ev.Evidence.LogHash)
	}
	stored, err := f.db.Get(ev.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(stored)
	for what, text := range map[string]string{"response": rec.Body.String(), "stored event": string(data), "logs": f.logs.String()} {
		if strings.Contains(text, "secret-admin") || strings.Contains(text, "second line") {
			t.Errorf("%s contains raw evidence:\n%s", what, text)
		}
	}

	// A rejected request carrying evidence does not echo it either.
	bad := `{"ip":"10.0.0.1","protocol":"ssh","reason":"x","events":1,"evidence_lines":["` + secret + `"]}`
	if rec := f.do(t, http.MethodPost, ReportsPath, bad); strings.Contains(rec.Body.String(), "secret-admin") ||
		strings.Contains(f.logs.String(), "secret-admin") {
		t.Errorf("refused report echoes evidence: %s", rec.Body.String())
	}
}

func TestReportEndpointPublishFailure(t *testing.T) {
	f := newVerdictFixture(t)
	f.pub.fail = errors.New("mesh not started")
	rec := f.do(t, http.MethodPost, ReportsPath, sshBody)
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "reporting failed; see the obied log") ||
		!strings.Contains(f.logs.String(), "verdict request failed") || !strings.Contains(f.logs.String(), "reporting") ||
		!strings.Contains(f.logs.String(), "mesh not started") {
		t.Errorf("POST with failing mesh = %d %q; logs:\n%s", rec.Code, rec.Body.String(), f.logs.String())
	}
}

func TestVerdictEndpointsUnavailable(t *testing.T) {
	h := Handler(testInfo(), discardLogger())
	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodPost, ReportsPath, strings.NewReader(sshBody)),
		httptest.NewRequest(http.MethodPost, RevocationsPath, strings.NewReader(`{"indicator":"85.10.0.7","reason":"x"}`)),
		httptest.NewRequest(http.MethodGet, IndicatorsPath, nil),
		httptest.NewRequest(http.MethodGet, IndicatorsPath+"/85.10.0.7", nil),
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s = %d, want 503", req.Method, req.URL, rec.Code)
		}
	}
}

func TestRevocationEndpoint(t *testing.T) {
	f := newVerdictFixture(t)
	first := decodeBody[ReportResponse](t, f.do(t, http.MethodPost, ReportsPath, sshBody)).Event
	f.do(t, http.MethodPost, ReportsPath, `{"ip":"85.10.0.8","protocol":"ssh","reason":"x","events":1}`)

	rec := f.do(t, http.MethodPost, RevocationsPath, `{"event_id":"`+first.ID+`","reason":"false_positive"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("revoke by event_id = %d %s", rec.Code, rec.Body.String())
	}
	revs := decodeBody[RevocationsResponse](t, rec).Revocations
	if len(revs) != 1 || revs[0].Type != obieproto.TypeRevoke || revs[0].Revokes != first.ID || revs[0].Reason != "false_positive" {
		t.Errorf("revocations = %+v", revs)
	}

	rec = f.do(t, http.MethodPost, RevocationsPath, `{"indicator":"85.10.0.8/32","reason":"operator_request"}`)
	if rec.Code != http.StatusCreated || len(decodeBody[RevocationsResponse](t, rec).Revocations) != 1 {
		t.Errorf("revoke by indicator = %d %s", rec.Code, rec.Body.String())
	}

	for name, tc := range map[string]struct {
		body string
		code int
		msg  string
	}{
		"already revoked": {`{"event_id":"` + first.ID + `","reason":"x"}`, http.StatusNotFound, "no longer active"},
		"no verdict":      {`{"indicator":"85.10.0.99","reason":"x"}`, http.StatusNotFound, "no active verdict on ipv4:85.10.0.99"},
		"unknown event":   {`{"event_id":"01900000-0000-7000-8000-000000000999","reason":"x"}`, http.StatusNotFound, "no event"},
		"neither":         {`{"reason":"x"}`, http.StatusBadRequest, "exactly one"},
		"both":            {`{"event_id":"` + first.ID + `","indicator":"85.10.0.7","reason":"x"}`, http.StatusBadRequest, "exactly one"},
		"no reason":       {`{"indicator":"85.10.0.7"}`, http.StatusBadRequest, "reason: missing"},
		"bad indicator":   {`{"indicator":"nope","reason":"x"}`, http.StatusBadRequest, "indicator: invalid indicator"},
		"unknown field":   {`{"indicator":"85.10.0.7","reason":"x","force":true}`, http.StatusBadRequest, "unknown field"},
		"not json":        {`[`, http.StatusBadRequest, "invalid JSON"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := f.do(t, http.MethodPost, RevocationsPath, tc.body)
			if rec.Code != tc.code || !strings.Contains(rec.Body.String(), tc.msg) {
				t.Errorf("POST %s = %d %q, want %d containing %q", tc.body, rec.Code, rec.Body.String(), tc.code, tc.msg)
			}
		})
	}

	// A revocation reason that is not a short token is invalid.
	f.do(t, http.MethodPost, ReportsPath, `{"ip":"85.10.0.9","protocol":"ssh","reason":"x","events":1}`)
	if rec := f.do(t, http.MethodPost, RevocationsPath, `{"indicator":"85.10.0.9","reason":"Oops, wrong IP"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("free-text reason = %d %q", rec.Code, rec.Body.String())
	}
}

// putForeignVerdict stores a verdict of another publisher on value.
func putForeignVerdict(t *testing.T, db *store.DB, value string) string {
	t.Helper()
	key, err := identity.Create(filepath.Join(t.TempDir(), "other"), false)
	if err != nil {
		t.Fatal(err)
	}
	ev := &obieproto.Event{
		ID: obieproto.NewID(time.Now()), Spec: obieproto.Spec, Type: obieproto.TypeVerdict,
		IssuedAt:  obieproto.NewTimestamp(time.Now()),
		Indicator: obieproto.Indicator{Kind: obieproto.KindIPv4, Value: value, Scope: "/32"},
		Protocol:  "ssh", Evidence: &obieproto.Evidence{Events: 1, Reason: "x"},
		Verdict: &obieproto.Verdict{SuggestedAction: obieproto.ActionBan, Confidence: 1, TTLSeconds: 3600},
	}
	if err := obieproto.SignWith(ev, key); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.Put(ev); err != nil || !ok {
		t.Fatalf("Put: %v, %v", ok, err)
	}
	return key.PeerID()
}

func TestIndicatorsEndpoint(t *testing.T) {
	f := newVerdictFixture(t)
	f.do(t, http.MethodPost, ReportsPath, sshBody)
	f.do(t, http.MethodPost, ReportsPath, `{"ip":"85.10.0.8","protocol":"ssh","reason":"x","events":1}`)
	foreign := putForeignVerdict(t, f.db, "85.10.0.7")
	putForeignVerdict(t, f.db, "85.10.0.9")

	rec := f.do(t, http.MethodGet, IndicatorsPath+"?active=true&limit=2", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d %s", rec.Code, rec.Body.String())
	}
	page := decodeBody[IndicatorsResponse](t, rec)
	if len(page.Indicators) != 2 || page.NextCursor != "ipv4:85.10.0.8" {
		t.Fatalf("first page = %+v", page)
	}
	first := page.Indicators[0]
	if first.Indicator.Key() != "ipv4:85.10.0.7" || len(first.Verdicts) != 2 {
		t.Fatalf("first indicator = %+v", first)
	}
	locals := 0
	for _, v := range first.Verdicts {
		if v.Local != (v.Event.Publisher.PeerID == f.key.PeerID()) {
			t.Errorf("verdict by %s has local=%v", v.Event.Publisher.PeerID, v.Local)
		}
		if v.Local {
			locals++
		}
	}
	if locals != 1 {
		t.Errorf("%d local verdicts on the first indicator, want 1", locals)
	}

	rec = f.do(t, http.MethodGet, IndicatorsPath+"?cursor="+page.NextCursor, "")
	if next := decodeBody[IndicatorsResponse](t, rec); len(next.Indicators) != 1 || next.Indicators[0].Indicator.Value != "85.10.0.9" ||
		next.NextCursor != "" {
		t.Errorf("second page = %+v", next)
	}
	rec = f.do(t, http.MethodGet, IndicatorsPath+"?publisher="+f.key.PeerID(), "")
	if mine := decodeBody[IndicatorsResponse](t, rec); len(mine.Indicators) != 2 {
		t.Errorf("own indicators = %+v", mine)
	}
	rec = f.do(t, http.MethodGet, IndicatorsPath+"?publisher="+foreign, "")
	if theirs := decodeBody[IndicatorsResponse](t, rec); len(theirs.Indicators) != 1 {
		t.Errorf("foreign indicators = %+v", theirs)
	}

	for query, msg := range map[string]string{
		"?active=false": "only active verdicts",
		"?active=yes":   "only active verdicts",
		"?limit=0":      "limit",
		"?limit=1001":   "limit",
		"?limit=ten":    "limit",
	} {
		if rec := f.do(t, http.MethodGet, IndicatorsPath+query, ""); rec.Code != http.StatusBadRequest ||
			!strings.Contains(rec.Body.String(), msg) {
			t.Errorf("GET %s = %d %q, want 400 containing %q", query, rec.Code, rec.Body.String(), msg)
		}
	}
}

func TestIndicatorsEndpointEmpty(t *testing.T) {
	f := newVerdictFixture(t)
	rec := f.do(t, http.MethodGet, IndicatorsPath, "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"indicators":[]}` {
		t.Errorf("GET empty = %d %s", rec.Code, rec.Body.String())
	}
}

func TestIndicatorEndpoint(t *testing.T) {
	f := newVerdictFixture(t)
	f.do(t, http.MethodPost, ReportsPath, sshBody)
	putForeignVerdict(t, f.db, "85.10.0.7")

	for _, path := range []string{"/85.10.0.7", "/ipv4:85.10.0.7", "/85.10.0.7%2F32"} {
		rec := f.do(t, http.MethodGet, IndicatorsPath+path, "")
		got := decodeBody[IndicatorResponse](t, rec)
		if rec.Code != http.StatusOK || got.Indicator.Key() != "ipv4:85.10.0.7" || len(got.Verdicts) != 2 {
			t.Errorf("GET %s = %d %+v", path, rec.Code, got)
		}
	}
	rec := f.do(t, http.MethodGet, IndicatorsPath+"/85.10.0.200", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"verdicts":[]`) {
		t.Errorf("GET unknown indicator = %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.do(t, http.MethodGet, IndicatorsPath+"/not-an-ip", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("GET bad indicator = %d %s", rec.Code, rec.Body.String())
	}
}

func TestTTLJSON(t *testing.T) {
	for in, want := range map[string]time.Duration{`"7d"`: 7 * 24 * time.Hour, `"90m"`: 90 * time.Minute, `3600`: time.Hour, `0`: 0} {
		var ttl TTL
		if err := json.Unmarshal([]byte(in), &ttl); err != nil || time.Duration(ttl) != want {
			t.Errorf("unmarshal %s = %v, %v; want %v", in, time.Duration(ttl), err, want)
		}
	}
	for _, in := range []string{`"soon"`, `1e30`, `1.5`, `null`, `[]`} {
		var ttl TTL
		if err := json.Unmarshal([]byte(in), &ttl); err == nil && in != `null` {
			t.Errorf("unmarshal %s: no error", in)
		}
	}
	data, err := json.Marshal(ReportRequest{TTL: TTL(36 * time.Hour)})
	if err != nil || !strings.Contains(string(data), `"ttl":"36h0m0s"`) {
		t.Errorf("marshal = %s, %v", data, err)
	}
	if data, _ := json.Marshal(ReportRequest{}); strings.Contains(string(data), "ttl") {
		t.Errorf("zero ttl is not omitted: %s", data)
	}
}

func TestVerdictClientOverSocket(t *testing.T) {
	f := newVerdictFixture(t)
	path := socketPath(t)
	s := New(path, "obie-no-such-group", f.info, discardLogger())
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	ctx := context.Background()
	c := NewClient(path)

	rep, err := c.Report(ctx, ReportRequest{IP: "85.10.0.7", Protocol: "ssh", Reason: "x", Events: 1,
		EvidenceLines: []string{"line"}, TTL: TTL(time.Hour)})
	if err != nil || rep.Event.Verdict.TTLSeconds != 3600 || rep.Event.Evidence.LogHash == "" {
		t.Fatalf("Report = %+v, %v", rep, err)
	}
	_, err = c.Report(ctx, ReportRequest{IP: "10.0.0.1", Protocol: "ssh", Reason: "x", Events: 1})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(apiErr.Message, "not a public address") {
		t.Errorf("refused Report error = %#v", err)
	}
	list, err := c.Indicators(ctx, IndicatorsQuery{Publisher: f.key.PeerID(), Limit: 10})
	if err != nil || len(list.Indicators) != 1 || !list.Indicators[0].Verdicts[0].Local {
		t.Errorf("Indicators = %+v, %v", list, err)
	}
	one, err := c.Indicator(ctx, "85.10.0.7")
	if err != nil || len(one.Verdicts) != 1 {
		t.Errorf("Indicator = %+v, %v", one, err)
	}
	if _, err := c.Indicator(ctx, " "); err == nil {
		t.Error("Indicator with an empty argument: no error")
	}
	revs, err := c.Revoke(ctx, RevocationRequest{EventID: rep.Event.ID, Reason: "false_positive"})
	if err != nil || len(revs.Revocations) != 1 {
		t.Errorf("Revoke = %+v, %v", revs, err)
	}
	_, err = c.Revoke(ctx, RevocationRequest{EventID: rep.Event.ID, Reason: "false_positive"})
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
		t.Errorf("second Revoke error = %v", err)
	}
}
