package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/auth"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/delivery"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

// fakeDeliveryService records what the REST surface asked of the check.
type fakeDeliveryService struct {
	testErr  error
	mu       sync.Mutex
	calls    []string
	selector delivery.Selector
	reason   string
	until    time.Time
	by       string
	err      error
	changed  int
	ctxErr   error
	noSends  bool
}

func (f *fakeDeliveryService) note(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *fakeDeliveryService) Pass(ctx context.Context) (delivery.Report, error) {
	f.note("pass")
	f.ctxErr = ctx.Err()
	return delivery.Report{ReportOnly: true, Result: delivery.ResultRed, Pass: 2}, f.err
}
func (f *fakeDeliveryService) Report() delivery.Report {
	f.note("report")
	return delivery.Report{ReportOnly: true, Result: delivery.ResultGreen, Pass: 1}
}
func (f *fakeDeliveryService) Ack(sel delivery.Selector, by string) (int, error) {
	f.note("ack")
	f.selector, f.by = sel, by
	return f.changed, f.err
}
func (f *fakeDeliveryService) Accept(sel delivery.Selector, reason string, until time.Time, by string) (int, error) {
	f.note("accept")
	f.selector, f.reason, f.until, f.by = sel, reason, until, by
	return f.changed, f.err
}
func (f *fakeDeliveryService) Drop(sel delivery.Selector, reason, by string) (int, error) {
	f.note("drop")
	f.selector, f.reason, f.by = sel, reason, by
	return f.changed, f.err
}
func (f *fakeDeliveryService) TestMessage(ctx context.Context) ([]delivery.SendResult, error) {
	f.note("test")
	if f.testErr != nil {
		return nil, f.testErr
	}
	if f.noSends {
		return nil, nil
	}
	return []delivery.SendResult{{Channel: "signal", OK: true}}, nil
}

func deliveryMux(s *Server) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/delivery", s.handleDelivery)
	mux.HandleFunc("/api/delivery/check", s.handleDeliveryCheck)
	mux.HandleFunc("/api/delivery/ack", s.handleDeliveryAck)
	mux.HandleFunc("/api/delivery/accept", s.handleDeliveryAccept)
	mux.HandleFunc("/api/delivery/drop", s.handleDeliveryDrop)
	mux.HandleFunc("/api/delivery/test-message", s.handleDeliveryTestMessage)
	return mux
}

func deliveryCall(h http.Handler, method, path, token, body string, header map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

type deliveryRoute struct {
	name, method, path, body, call string
	write                          bool
}

var deliveryRoutes = []deliveryRoute{
	{"status", http.MethodGet, "/api/delivery", "", "report", false},
	{"check", http.MethodPost, "/api/delivery/check", "", "pass", false},
	{"ack", http.MethodPost, "/api/delivery/ack", `{"keys":["task:t1:failed"]}`, "ack", true},
	{"accept", http.MethodPost, "/api/delivery/accept", `{"keys":["task:t1:failed"],"reason":"parked","until":"2026-12-01"}`, "accept", true},
	{"drop", http.MethodPost, "/api/delivery/drop", `{"backlog":true,"reason":"obsolete"}`, "drop", true},
	{"test-message", http.MethodPost, "/api/delivery/test-message", "", "test", true},
}

func TestDeliveryRoutesAreServed(t *testing.T) {
	cfg := &config.Config{}
	h := New(&cfg.Server, cfg, "", t.TempDir(), nil, "test", nil)
	f := &fakeDeliveryService{}
	h.SetDelivery(f)
	for _, rt := range deliveryRoutes {
		rr := deliveryCall(h.Handler(), rt.method, rt.path, "", rt.body, nil)
		if rr.Code != http.StatusOK {
			t.Errorf("%s: %d %s", rt.name, rr.Code, rr.Body.String())
		}
	}
	want := []string{"report", "pass", "ack", "accept", "drop", "test"}
	if !reflect.DeepEqual(f.calls, want) {
		t.Fatalf("calls = %v, want %v", f.calls, want)
	}
}

func TestDeliveryNotWired(t *testing.T) {
	mux := deliveryMux(&Server{})
	for _, rt := range deliveryRoutes {
		rr := deliveryCall(mux, rt.method, rt.path, "", rt.body, nil)
		if rr.Code != http.StatusServiceUnavailable || !strings.Contains(rr.Body.String(), "the delivery check is not available") {
			t.Errorf("%s: %d %q", rt.name, rr.Code, rr.Body.String())
		}
	}
}

func TestDeliveryMethodNotAllowed(t *testing.T) {
	f := &fakeDeliveryService{}
	mux := deliveryMux(&Server{delivery: f})
	for _, rt := range deliveryRoutes {
		wrong := http.MethodGet
		if rt.method == http.MethodGet {
			wrong = http.MethodPost
		}
		if rr := deliveryCall(mux, wrong, rt.path, "", rt.body, nil); rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d, want 405", wrong, rt.path, rr.Code)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("a wrong method reached the check: %v", f.calls)
	}
}

func TestDeliveryReportsAreReturned(t *testing.T) {
	f := &fakeDeliveryService{}
	mux := deliveryMux(&Server{delivery: f})
	for path, wantPass := range map[string]int{"/api/delivery": 1, "/api/delivery/check": 2} {
		method := http.MethodGet
		if strings.HasSuffix(path, "check") {
			method = http.MethodPost
		}
		rr := deliveryCall(mux, method, path, "", "", nil)
		var r delivery.Report
		if err := json.Unmarshal(rr.Body.Bytes(), &r); err != nil || r.Pass != wantPass || !r.ReportOnly {
			t.Errorf("%s: %d %s (%v)", path, rr.Code, rr.Body.String(), err)
		}
	}
}

// A caller that goes away does not stop a pass halfway through its sends.
func TestDeliveryCheckOutlivesTheRequest(t *testing.T) {
	f := &fakeDeliveryService{}
	mux := deliveryMux(&Server{delivery: f})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodPost, "/api/delivery/check", nil).WithContext(ctx)
	mux.ServeHTTP(httptest.NewRecorder(), req)
	if f.ctxErr != nil {
		t.Fatalf("pass context = %v, want not cancelled", f.ctxErr)
	}
}

func TestDeliveryActionsPassTheBody(t *testing.T) {
	f := &fakeDeliveryService{changed: 3}
	mux := deliveryMux(&Server{delivery: f})
	body := `{"keys":["task:t1:failed","bug:b1:open"],"kind":"task","state":"failed","match_reason":"over_limit","backlog":true,"reason":"parked","until":"2026-12-01T10:00:00Z","actor":"web"}`
	rr := deliveryCall(mux, http.MethodPost, "/api/delivery/accept", "", body, nil)
	if rr.Code != http.StatusOK || strings.TrimSpace(rr.Body.String()) != `{"changed":3}` {
		t.Fatalf("accept: %d %q", rr.Code, rr.Body.String())
	}
	want := delivery.Selector{Keys: []string{"task:t1:failed", "bug:b1:open"}, Kind: delivery.KindTask, State: "failed", Reason: "over_limit", Backlog: true}
	if !reflect.DeepEqual(f.selector, want) {
		t.Errorf("selector = %+v", f.selector)
	}
	if f.reason != "parked" || !f.until.Equal(time.Date(2026, 12, 1, 10, 0, 0, 0, time.UTC)) || f.by != "web" {
		t.Errorf("reason=%q until=%v by=%q", f.reason, f.until, f.by)
	}
	rr = deliveryCall(mux, http.MethodPost, "/api/delivery/drop", "", `{"kind":"bug","reason":"obsolete","actor":"cli"}`, nil)
	if rr.Code != http.StatusOK || f.reason != "obsolete" || f.by != "cli" || f.selector.Kind != delivery.KindBug {
		t.Errorf("drop: %d reason=%q by=%q sel=%+v", rr.Code, f.reason, f.by, f.selector)
	}
	rr = deliveryCall(mux, http.MethodPost, "/api/delivery/ack", "", `{"backlog":true,"actor":"channel"}`, nil)
	if rr.Code != http.StatusOK || f.by != "channel" || !f.selector.Backlog {
		t.Errorf("ack: %d by=%q sel=%+v", rr.Code, f.by, f.selector)
	}
}

func TestDeliveryTestMessageAnswer(t *testing.T) {
	f := &fakeDeliveryService{}
	mux := deliveryMux(&Server{delivery: f})
	rr := deliveryCall(mux, http.MethodPost, "/api/delivery/test-message", "", "", nil)
	var out struct {
		Results []delivery.SendResult `json:"results"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil || len(out.Results) != 1 || !out.Results[0].OK {
		t.Fatalf("answer %q (%v)", rr.Body.String(), err)
	}
	f.noSends = true
	rr = deliveryCall(mux, http.MethodPost, "/api/delivery/test-message", "", "", nil)
	if strings.TrimSpace(rr.Body.String()) != `{"results":[]}` {
		t.Fatalf("no channel answer = %q, want an empty list", rr.Body.String())
	}
}

func TestDeliveryErrorStatuses(t *testing.T) {
	cases := []struct {
		err  error
		code int
	}{
		{delivery.ErrSelector, http.StatusBadRequest},
		{delivery.ErrReasonCode, http.StatusBadRequest},
		{fmt.Errorf("%w: too far", delivery.ErrUntil), http.StatusBadRequest},
		{delivery.ErrNoMatch, http.StatusNotFound},
		{delivery.ErrNotDroppable, http.StatusConflict},
		{delivery.ErrCannotRecord, http.StatusServiceUnavailable},
		{fmt.Errorf("%w: open", delivery.ErrRecords), http.StatusServiceUnavailable},
		{errors.New("something else"), http.StatusInternalServerError},
	}
	for _, c := range cases {
		f := &fakeDeliveryService{err: c.err}
		mux := deliveryMux(&Server{delivery: f})
		for _, rt := range deliveryRoutes {
			if rt.name == "status" || rt.name == "test-message" {
				continue
			}
			rr := deliveryCall(mux, rt.method, rt.path, "", rt.body, nil)
			if rr.Code != c.code || !strings.Contains(rr.Body.String(), c.err.Error()) {
				t.Errorf("%s with %v: %d %q, want %d", rt.name, c.err, rr.Code, rr.Body.String(), c.code)
			}
		}
	}
}

// A body that could be read as a wider choice than the caller meant is
// refused before the check is touched.
func TestDeliveryBadBodies(t *testing.T) {
	f := &fakeDeliveryService{}
	mux := deliveryMux(&Server{delivery: f})
	cases := map[string][]string{
		"/api/delivery/ack":    {``, `{`, `[]`, `{"kind":"task","stat":"failed"}`, `{"backlog":true,"reason":"x"}`},
		"/api/delivery/accept": {``, `{"backlog":true,"reason":"parked"}`, `{"backlog":true,"reason":"parked","until":"next week"}`, `{"backlog":true,"reason":"parked","until":"2026-12-01","by":"operator"}`},
		"/api/delivery/drop":   {``, `{"kind":"task","stat":"failed","reason":"x"}`, `{"backlog":true,"reason":"x","until":"2026-12-01"}`},
	}
	for path, bodies := range cases {
		for _, body := range bodies {
			if rr := deliveryCall(mux, http.MethodPost, path, "", body, nil); rr.Code != http.StatusBadRequest {
				t.Errorf("%s %q = %d %q, want 400", path, body, rr.Code, rr.Body.String())
			}
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("a refused body reached the check: %v", f.calls)
	}
}

// deliveryCallers builds a server behind the real auth middleware with one
// credential of every kind.
func deliveryCallers(t *testing.T) (*Server, *fakeDeliveryService, http.Handler, map[string]string) {
	t.Helper()
	s, store, _ := newFedTestServer(t)
	tokenStore, err := auth.NewSessionTokenStore("")
	if err != nil {
		t.Fatal(err)
	}
	s.sessionTokens = tokenStore
	f := &fakeDeliveryService{changed: 1}
	s.delivery = f
	tokens := map[string]string{"operator": "admin-token"}
	for name, caps := range map[string][]string{
		"session":        federation.Resolve([]string{"session-default"}, nil),
		"session-no-cap": federation.Resolve([]string{"monitor"}, nil),
	} {
		tok, err := s.sessionTokens.Mint("sess-"+name, caps)
		if err != nil {
			t.Fatal(err)
		}
		tokens[name] = tok
	}
	for name, caps := range map[string][]string{
		"peer-granted": {federation.CapDeliveryRead, federation.CapDeliveryWrite},
		"peer-reader":  {"read-only"},
		"peer-default": {"federation-peer"},
		"peer-full":    {"full-control"},
	} {
		tok := "ptok-" + name
		if err := store.Add(&multiserver.Entry{Name: name, Token: tok, URL: "http://" + name + ":8080", Enabled: true, Federated: true, Capabilities: caps}); err != nil {
			t.Fatal(err)
		}
		tokens[name] = tok
	}
	return s, f, s.fedAuthMiddleware(deliveryMux(s)), tokens
}

// Every action by every kind of caller. Nothing is refused for being a
// session or a peer where the credential holds the capability, and who did it
// comes from the credential: a claim in the body is ignored for both.
func TestDeliveryActionByCaller(t *testing.T) {
	_, f, h, tokens := deliveryCallers(t)
	type access struct {
		read, write bool
		by          string
	}
	table := map[string]access{
		"operator":       {true, true, "cli"},
		"session":        {true, true, "session:sess-session"},
		"session-no-cap": {false, false, ""},
		"peer-granted":   {true, true, "peer:peer-granted"},
		"peer-full":      {true, true, "peer:peer-full"},
		"peer-reader":    {true, false, ""},
		"peer-default":   {false, false, ""},
	}
	for caller, a := range table {
		for _, rt := range deliveryRoutes {
			f.calls, f.by = nil, ""
			body := rt.body
			if rt.write && rt.name != "test-message" {
				body = strings.TrimSuffix(body, "}") + `,"actor":"cli"}`
			}
			rr := deliveryCall(h, rt.method, rt.path, tokens[caller], body, nil)
			allowed := a.read
			if rt.write {
				allowed = a.write
			}
			if !allowed {
				if rr.Code != http.StatusForbidden || len(f.calls) != 0 {
					t.Errorf("%s %s: %d calls=%v, want 403 and no call", caller, rt.name, rr.Code, f.calls)
				}
				continue
			}
			if rr.Code != http.StatusOK || !reflect.DeepEqual(f.calls, []string{rt.call}) {
				t.Errorf("%s %s: %d %q calls=%v", caller, rt.name, rr.Code, rr.Body.String(), f.calls)
				continue
			}
			if rt.write && rt.name != "test-message" && f.by != a.by {
				t.Errorf("%s %s: by = %q, want %q", caller, rt.name, f.by, a.by)
			}
		}
	}
}

// The operator may only label the surface used; any other claim is recorded
// as "operator", so records never take free text from a body.
func TestDeliveryOperatorLabel(t *testing.T) {
	_, f, h, tokens := deliveryCallers(t)
	for claim, want := range map[string]string{
		"": "operator", "cli": "cli", "web": "web", "channel": "channel", " web ": "web",
		"operator": "operator", "a title with words": "operator", "session:abc": "operator", "peer:x": "operator",
	} {
		body, _ := json.Marshal(map[string]any{"backlog": true, "actor": claim})
		deliveryCall(h, http.MethodPost, "/api/delivery/ack", tokens["operator"], string(body), nil)
		if f.by != want {
			t.Errorf("claim %q: by = %q, want %q", claim, f.by, want)
		}
	}
}

// The assistant tools and chat commands call back with the admin token and
// name the session or peer they act for in a header. It is read only on an
// admin request, can only name a session or peer, and never beats the
// credential of a session or peer.
func TestDeliveryCallerHeader(t *testing.T) {
	_, f, h, tokens := deliveryCallers(t)
	cases := []struct{ token, header, want string }{
		{"operator", "session:host-1a2b", "session:host-1a2b"},
		{"operator", "peer:desk", "peer:desk"},
		{"operator", "session:two words", "session:unknown"},
		{"operator", "peer:" + strings.Repeat("x", 300), "peer:unknown"},
		{"operator", "operator", "web"},
		{"session", "peer:desk", "session:sess-session"},
		{"session", "session:other", "session:sess-session"},
		{"peer-granted", "session:other", "peer:peer-granted"},
	}
	for _, c := range cases {
		rr := deliveryCall(h, http.MethodPost, "/api/delivery/ack", tokens[c.token], `{"backlog":true,"actor":"web"}`,
			map[string]string{federation.HeaderDeliveryCaller: c.header})
		if rr.Code != http.StatusOK || f.by != c.want {
			t.Errorf("%s with header %q: %d by=%q, want %q", c.token, c.header, rr.Code, f.by, c.want)
		}
	}
}

// ----- settings -------------------------------------------------------------

func deliveryConfigView(t *testing.T, s *Server) map[string]interface{} {
	t.Helper()
	sec, ok := getConfigView(t, s)["delivery"].(map[string]interface{})
	if !ok {
		t.Fatal("GET /api/config has no delivery section")
	}
	return sec
}

func TestDeliveryConfigDefaults(t *testing.T) {
	got := deliveryConfigView(t, newRoundTripServer(t))
	want := map[string]interface{}{"enabled": true, "notify": true, "interval_seconds": float64(300)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("defaults = %v", got)
	}
}

func TestDeliveryConfigRoundTrip(t *testing.T) {
	s := newRoundTripServer(t)
	// Strings are what the chat channel and MCP config_set send.
	for _, patch := range []map[string]interface{}{
		{"delivery.enabled": true, "delivery.notify": true, "delivery.interval_seconds": 60},
		{"delivery.enabled": "false", "delivery.notify": "false", "delivery.interval_seconds": "900"},
	} {
		if rr := putPatch(t, s, patch); rr.Code != http.StatusOK {
			t.Fatalf("PUT %v = %d: %s", patch, rr.Code, rr.Body.String())
		}
	}
	want := map[string]interface{}{"enabled": false, "notify": false, "interval_seconds": float64(900)}
	if got := deliveryConfigView(t, s); !reflect.DeepEqual(got, want) {
		t.Fatalf("readback = %v", got)
	}
	loaded, err := config.Load(s.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Delivery.EnabledOn() || loaded.Delivery.NotifyOn() || loaded.Delivery.IntervalSeconds != 900 {
		t.Fatalf("saved = %+v", loaded.Delivery)
	}
	// 0 goes back to the default.
	if rr := putPatch(t, s, map[string]interface{}{"delivery.interval_seconds": 0}); rr.Code != http.StatusOK {
		t.Fatal(rr.Body.String())
	}
	if got := deliveryConfigView(t, s)["interval_seconds"]; got != float64(300) {
		t.Fatalf("interval after 0 = %v", got)
	}
}

func TestDeliveryConfigRefusesBadValues(t *testing.T) {
	for _, patch := range []map[string]interface{}{
		{"delivery.enabled": "maybe"},
		{"delivery.enabled": 1},
		{"delivery.notify": nil},
		{"delivery.interval_seconds": 59},
		{"delivery.interval_seconds": -300},
		{"delivery.interval_seconds": 90.5},
		{"delivery.interval_seconds": "soon"},
		{"delivery.interval_seconds": 1e12},
		{"delivery.interval_seconds": true},
		{"delivery.enabled": false, "delivery.interval_seconds": 5},
	} {
		s := newRoundTripServer(t)
		rr := putPatch(t, s, patch)
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "delivery.") {
			t.Errorf("PUT %v = %d %q, want 400 naming the key", patch, rr.Code, rr.Body.String())
		}
		if !reflect.DeepEqual(s.cfg.Delivery, config.DeliveryConfig{}) {
			t.Errorf("PUT %v stored a value despite the refusal: %+v", patch, s.cfg.Delivery)
		}
	}
}

// The daemon connects the check after the web server has started serving, so
// wiring it must be safe beside requests (run with -race).
func TestDeliveryWiredWhileServing(t *testing.T) {
	s := &Server{}
	h := &HTTPServer{api: s}
	mux := deliveryMux(s)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			rr := deliveryCall(mux, http.MethodGet, "/api/delivery", "", "", nil)
			if rr.Code != http.StatusOK && rr.Code != http.StatusServiceUnavailable {
				t.Errorf("status %d", rr.Code)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		h.SetDelivery(&fakeDeliveryService{})
	}()
	wg.Wait()
	if rr := deliveryCall(mux, http.MethodGet, "/api/delivery", "", "", nil); rr.Code != http.StatusOK {
		t.Fatalf("after wiring: %d %s", rr.Code, rr.Body.String())
	}
}
