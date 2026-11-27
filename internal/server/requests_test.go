package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/auth"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/delivery"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/server/multiserver"
	"github.com/dmz006/datawatch/internal/session"
)

const (
	reqHeld   = "r-held"
	reqHolder = "sess-holder"
	reqMarker = "MARKER-7f3a-secret-words"
)

type reqHit struct {
	op     string
	id     string
	caller delivery.Caller
	in     any
}

// fakeRequests refuses as the real store does: a peer gets ErrCrossHost, an
// unknown ID ErrRequestNotFound, and only the holder's own session credential
// may take, see, accept, report or decline.
type fakeRequests struct {
	mu   sync.Mutex
	hits []reqHit
	err  error
}

func (f *fakeRequests) note(op, id string, c delivery.Caller, in any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hits = append(f.hits, reqHit{op, id, c, in})
	if c.Kind == delivery.CallerPeer {
		return delivery.ErrCrossHost
	}
	if id != "" && id != reqHeld {
		return delivery.ErrRequestNotFound
	}
	return f.err
}

func (f *fakeRequests) holder(op, id string, c delivery.Caller, in any) (delivery.RequestView, error) {
	if err := f.note(op, id, c, in); err != nil {
		return delivery.RequestView{}, err
	}
	if !c.Holds(reqHolder) {
		return delivery.RequestView{}, delivery.ErrNotHolder
	}
	return reqView(), nil
}

func reqView() delivery.RequestView {
	return delivery.RequestView{Request: delivery.Request{
		ID: reqHeld, Kind: delivery.ReqAsk, Role: delivery.RoleSupervisor,
		Scope: delivery.Scope{Kind: delivery.ScopeProject, ID: "web"}, State: delivery.StateWaiting,
		Holder: &delivery.Holder{SessionID: reqHolder},
	}, Label: delivery.LabelWaiting}
}

func (f *fakeRequests) Create(_ context.Context, c delivery.Caller, in delivery.CreateInput) (delivery.RequestView, bool, error) {
	if err := f.note("create", "", c, in); err != nil {
		return delivery.RequestView{}, false, err
	}
	if in.Host != "" && in.Host != "this-host" {
		return delivery.RequestView{}, false, delivery.ErrCrossHost
	}
	return reqView(), true, nil
}
func (f *fakeRequests) Take(_ context.Context, c delivery.Caller, id string, in delivery.TakeInput) (delivery.TakeReply, error) {
	v, err := f.holder("take", id, c, in)
	if err != nil {
		return delivery.TakeReply{}, err
	}
	return delivery.TakeReply{Request: v, Content: "do it " + reqMarker, Digest: "d1", Open: []string{id}}, nil
}
func (f *fakeRequests) Seen(_ context.Context, c delivery.Caller, id string, in delivery.SeenInput) (delivery.RequestView, error) {
	return f.holder("seen", id, c, in)
}
func (f *fakeRequests) Accept(_ context.Context, c delivery.Caller, id string, in delivery.AcceptInput) (delivery.RequestView, error) {
	return f.holder("accept", id, c, in)
}
func (f *fakeRequests) Report(_ context.Context, c delivery.Caller, id string, in delivery.ReportInput) (delivery.RequestView, error) {
	return f.holder("report", id, c, in)
}
func (f *fakeRequests) Decline(_ context.Context, c delivery.Caller, id string, in delivery.DeclineInput) (delivery.RequestView, error) {
	return f.holder("decline", id, c, in)
}
func (f *fakeRequests) Mine(_ context.Context, c delivery.Caller) (delivery.MineReply, error) {
	if err := f.note("mine", "", c, nil); err != nil {
		return delivery.MineReply{}, err
	}
	if c.Kind != delivery.CallerSession {
		return delivery.MineReply{}, delivery.ErrNotHolder
	}
	return delivery.MineReply{SessionID: c.SessionID, Items: []delivery.RequestView{reqView()}}, nil
}
func (f *fakeRequests) Get(_ context.Context, c delivery.Caller, id string) (delivery.RequestView, error) {
	if err := f.note("get", id, c, nil); err != nil {
		return delivery.RequestView{}, err
	}
	return reqView(), nil
}
func (f *fakeRequests) List(_ context.Context, c delivery.Caller, fl delivery.RequestFilter) ([]delivery.RequestView, error) {
	if err := f.note("list", "", c, fl); err != nil {
		return nil, err
	}
	return []delivery.RequestView{reqView()}, nil
}
func (f *fakeRequests) Roles(_ context.Context, c delivery.Caller) ([]delivery.RoleBinding, error) {
	return nil, f.note("roles", "", c, nil)
}
func (f *fakeRequests) GiveRole(_ context.Context, c delivery.Caller, in delivery.RoleInput) (delivery.RoleBinding, error) {
	return delivery.RoleBinding{Role: in.Role, Scope: in.Scope, SessionID: in.SessionID}, f.note("give", "", c, in)
}
func (f *fakeRequests) ReleaseRole(_ context.Context, c delivery.Caller, in delivery.RoleInput) error {
	return f.note("release", "", c, in)
}
func (f *fakeRequests) action(op, id string, c delivery.Caller, in any) (delivery.RequestView, error) {
	if err := f.note(op, id, c, in); err != nil {
		return delivery.RequestView{}, err
	}
	return reqView(), nil
}
func (f *fakeRequests) Reissue(_ context.Context, c delivery.Caller, id string, in delivery.ReasonInput) (delivery.RequestView, error) {
	return f.action("reissue", id, c, in)
}
func (f *fakeRequests) Drop(_ context.Context, c delivery.Caller, id string, in delivery.ReasonInput) (delivery.RequestView, error) {
	return f.action("drop", id, c, in)
}
func (f *fakeRequests) Force(_ context.Context, c delivery.Caller, id string, in delivery.ForceInput) (delivery.RequestView, error) {
	return f.action("force", id, c, in)
}
func (f *fakeRequests) AcceptUnproven(_ context.Context, c delivery.Caller, id string, in delivery.ReasonInput) (delivery.RequestView, error) {
	return f.action("accept-unproven", id, c, in)
}
func (f *fakeRequests) FromBacklog(_ context.Context, c delivery.Caller, in delivery.BacklogInput) (delivery.RequestView, bool, error) {
	return reqView(), true, f.note("from-backlog", "", c, in)
}

func (f *fakeRequests) last(t *testing.T) reqHit {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.hits) == 0 {
		t.Fatal("the service was not called")
	}
	return f.hits[len(f.hits)-1]
}
func (f *fakeRequests) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.hits)
}

type reqFixture struct {
	s      *Server
	f      *fakeRequests
	h      http.Handler
	tokens map[string]string
	audit  string // path of the audit log
}

func reqCallers(t *testing.T) *reqFixture {
	t.Helper()
	s, store, _ := newFedTestServer(t)
	tokenStore, err := auth.NewSessionTokenStore("")
	if err != nil {
		t.Fatal(err)
	}
	s.sessionTokens = tokenStore
	s.cfg = config.DefaultConfig()
	dir := t.TempDir()
	al, err := audit.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = al.Close() })
	s.auditLog = al
	q, err := session.NewQueueStore(filepath.Join(t.TempDir(), "queue.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.queueStore = q
	f := &fakeRequests{}
	s.requests = f

	grant := federation.Resolve([]string{"session-default"}, nil)
	tokens := map[string]string{"operator": "admin-token", "none": ""}
	for name, id := range map[string]string{"holder": reqHolder, "other": "sess-other"} {
		if tokens[name], err = tokenStore.Mint(id, grant); err != nil {
			t.Fatal(err)
		}
	}
	// A session credential that is tied to no one session.
	if tokens["unknown"], err = tokenStore.MintScoped(auth.InstanceTokenOwnerPrefix+"x", grant, time.Hour); err != nil {
		t.Fatal(err)
	}
	for name, group := range map[string]string{"reader": "read-only", "no-cap": "monitor"} {
		if tokens[name], err = tokenStore.Mint("sess-"+name, federation.Resolve([]string{group}, nil)); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Add(&multiserver.Entry{Name: "other-box", Token: "ptok", URL: "http://other-box:8080", Enabled: true, Federated: true, Capabilities: []string{"full-control"}}); err != nil {
		t.Fatal(err)
	}
	tokens["peer"] = "ptok"

	mux := http.NewServeMux()
	mux.HandleFunc(delivery.RouteRequests, s.handleRequests)
	mux.HandleFunc(delivery.RouteRequests+"/", s.handleRequests)
	mux.HandleFunc("/api/queue", s.handleQueue)
	mux.HandleFunc("/api/queue/", s.handleQueue)
	mux.HandleFunc("/api/proxy/", s.handleProxy)
	mux.HandleFunc("/remote/", s.handleRemotePWA)
	return &reqFixture{s: s, f: f, h: s.fedAuthMiddleware(mux), tokens: tokens, audit: filepath.Join(dir, "audit.log")}
}

func (x *reqFixture) call(method, path, who, body string, header map[string]string) *httptest.ResponseRecorder {
	return deliveryCall(x.h, method, path, x.tokens[who], body, header)
}

type reqRoute struct {
	op, method, path, body string
	holderOnly             bool
}

var reqRoutes = []reqRoute{
	{"list", "GET", "/api/requests", "", false},
	{"get", "GET", "/api/requests?id=r-held", "", false},
	{"create", "POST", "/api/requests", `{"kind":"ask","role":"supervisor","scope":{"kind":"project","id":"web"},"content":{"ask":"x"}}`, false},
	{"mine", "GET", "/api/requests/mine", "", false},
	{"roles", "GET", "/api/requests/roles", "", false},
	{"give", "POST", "/api/requests/roles/give", `{"role":"supervisor","scope":{"kind":"project","id":"web"},"session_id":"sess-holder"}`, false},
	{"release", "POST", "/api/requests/roles/release", `{"role":"supervisor","scope":{"kind":"project","id":"web"},"session_id":"sess-holder"}`, false},
	{"from-backlog", "POST", "/api/requests/from-backlog", `{"entry_key":"task:t1:failed","role":"supervisor","scope":{"kind":"project","id":"web"},"content":{"ask":"x"}}`, false},
	{"take", "POST", "/api/requests/r-held/take", `{"via":"program"}`, true},
	{"seen", "POST", "/api/requests/r-held/seen", `{"digest":"d1","via":"program","reporter_version":"v1"}`, true},
	{"accept", "POST", "/api/requests/r-held/accept", `{"code":"rc-1"}`, true},
	{"report", "POST", "/api/requests/r-held/report", `{"text":"finished"}`, true},
	{"decline", "POST", "/api/requests/r-held/decline", `{"reason_code":"declined"}`, true},
	{"reissue", "POST", "/api/requests/r-held/reissue", `{"reason_code":"again","repeat_ok":true}`, false},
	{"drop", "POST", "/api/requests/r-held/drop", `{"reason_code":"obsolete"}`, false},
	{"force", "POST", "/api/requests/r-held/force", `{"reason_code":"stuck","to_session":"sess-other"}`, false},
	{"accept-unproven", "POST", "/api/requests/r-held/accept-unproven", `{"reason_code":"seen","words":"I looked"}`, false},
}

// Every action by every kind of caller: the service gets a caller built from
// the credential, and only the holder's own credential holds.
func TestReqSurfaceRESTCallerByCredential(t *testing.T) {
	x := reqCallers(t)
	want := map[string]delivery.Caller{
		"operator": {Kind: delivery.CallerOperator},
		"holder":   {Kind: delivery.CallerSession, SessionID: reqHolder},
		"other":    {Kind: delivery.CallerSession, SessionID: "sess-other"},
		"unknown":  {Kind: delivery.CallerSessionUnknown},
		"peer":     {Kind: delivery.CallerPeer, Peer: "other-box"},
	}
	for _, rt := range reqRoutes {
		for who, wc := range want {
			before := x.f.count()
			rr := x.call(rt.method, rt.path, who, rt.body, nil)
			if x.f.count() != before+1 {
				t.Fatalf("%s by %s: service calls %d -> %d (%d %s)", rt.op, who, before, x.f.count(), rr.Code, rr.Body.String())
			}
			hit := x.f.last(t)
			wc.Surface = delivery.SurfaceREST
			if hit.op != rt.op || hit.caller != wc {
				t.Errorf("%s by %s: got %s %+v", rt.op, who, hit.op, hit.caller)
			}
			wantCode := http.StatusOK
			switch {
			case who == "peer":
				wantCode = delivery.HTTPStatus(delivery.ErrCrossHost)
			case rt.holderOnly && who != "holder":
				wantCode = http.StatusForbidden
			case rt.op == "mine" && (who == "operator" || who == "unknown"):
				wantCode = http.StatusForbidden
			}
			if rr.Code != wantCode {
				t.Errorf("%s by %s: %d, want %d: %s", rt.op, who, rr.Code, wantCode, rr.Body.String())
			}
		}
		before := x.f.count()
		if rr := x.call(rt.method, rt.path, "none", rt.body, nil); rr.Code != http.StatusUnauthorized || x.f.count() != before {
			t.Errorf("%s with no credential: %d", rt.op, rr.Code)
		}
	}
}

// Reading needs queue:read and every other call queue:write.
func TestReqSurfaceRESTCapabilities(t *testing.T) {
	x := reqCallers(t)
	for _, rt := range reqRoutes {
		before := x.f.count()
		if rr := x.call(rt.method, rt.path, "no-cap", rt.body, nil); rr.Code != http.StatusForbidden || x.f.count() != before {
			t.Errorf("%s with neither: %d", rt.op, rr.Code)
		}
		rr := x.call(rt.method, rt.path, "reader", rt.body, nil)
		if rt.method == http.MethodPost && (rr.Code != http.StatusForbidden || x.f.count() != before) {
			t.Errorf("%s with queue:read only: %d", rt.op, rr.Code)
		}
		if rt.method == http.MethodGet && x.f.count() != before+1 {
			t.Errorf("%s with queue:read did not reach the service: %d %s", rt.op, rr.Code, rr.Body.String())
		}
	}
}

// A body's claimed_by, session or via fields never make a holder.
func TestReqSurfaceRESTBodyClaimsNeverHold(t *testing.T) {
	x := reqCallers(t)
	for _, verb := range []string{"take", "seen", "accept", "report", "decline"} {
		body := `{"claimed_by":"sess-holder","session_id":"sess-holder","via":"program"}`
		if verb == "decline" {
			body = `{"reason_code":"declined","claimed_by":"sess-holder","session_id":"sess-holder","via":"program"}`
		}
		for _, who := range []string{"operator", "other", "unknown"} {
			rr := x.call("POST", "/api/requests/r-held/"+verb, who, body, nil)
			if rr.Code != http.StatusForbidden {
				t.Errorf("%s by %s with claims: %d %s", verb, who, rr.Code, rr.Body.String())
			}
			c := x.f.last(t).caller
			if c.Holds(reqHolder) || c.ClaimedName != reqHolder || c.ClaimedSession != reqHolder {
				t.Errorf("%s by %s: caller %+v", verb, who, c)
			}
		}
	}
}

// The header an assistant tool or chat command sets can only lower an admin
// request to a session or peer; it never makes a holder, and a session or peer
// credential ignores it.
func TestReqSurfaceRESTCallerHeaders(t *testing.T) {
	x := reqCallers(t)
	take := func(who string, header map[string]string) (int, delivery.Caller) {
		rr := x.call("POST", "/api/requests/r-held/take", who, "", header)
		return rr.Code, x.f.last(t).caller
	}
	code, c := take("operator", map[string]string{federation.HeaderDeliveryCaller: "session:" + reqHolder})
	if code != http.StatusForbidden || c.Kind != delivery.CallerSessionUnknown || c.SessionID != "" || c.ClaimedSession != reqHolder {
		t.Errorf("admin for a session: %d %+v", code, c)
	}
	_, c = take("operator", map[string]string{federation.HeaderDeliveryCaller: "peer:elsewhere"})
	if c.Kind != delivery.CallerPeer || c.Peer != "elsewhere" {
		t.Errorf("admin for a peer: %+v", c)
	}
	_, c = take("operator", map[string]string{federation.HeaderDeliveryCaller: "session:bad name!"})
	if c.Kind != delivery.CallerSessionUnknown || c.ClaimedSession != "" {
		t.Errorf("unreadable header: %+v", c)
	}
	code, c = take("other", map[string]string{federation.HeaderDeliveryCaller: "session:" + reqHolder})
	if code != http.StatusForbidden || c.SessionID != "sess-other" {
		t.Errorf("session with a header: %d %+v", code, c)
	}
	for value, want := range map[string]delivery.Surface{"cli": delivery.SurfaceCLI, "chat": delivery.SurfaceChat, "daemon": delivery.SurfaceREST, "mcp": delivery.SurfaceREST} {
		if _, c := take("holder", map[string]string{federation.HeaderRequestSurface: value}); c.Surface != want {
			t.Errorf("surface %q = %q, want %q", value, c.Surface, want)
		}
	}
}

func TestReqSurfaceRESTRepliesAndRefusals(t *testing.T) {
	x := reqCallers(t)
	rr := x.call("POST", "/api/requests/r-held/take", "holder", "", nil)
	var reply delivery.TakeReply
	if err := json.Unmarshal(rr.Body.Bytes(), &reply); err != nil || reply.Request.ID != reqHeld || reply.Digest != "d1" || reply.Content == "" {
		t.Fatalf("take reply: %v %s", err, rr.Body.String())
	}
	var list struct {
		Requests []delivery.RequestView `json:"requests"`
	}
	rr = x.call("GET", "/api/requests?state=waiting&kind=ask&role=supervisor&scope_kind=project&scope_id=web&holder=sess-holder", "operator", "", nil)
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil || len(list.Requests) != 1 {
		t.Fatalf("list: %v %s", err, rr.Body.String())
	}
	want := delivery.RequestFilter{State: delivery.StateWaiting, Kind: delivery.ReqAsk, Role: delivery.RoleSupervisor, ScopeKind: delivery.ScopeProject, ScopeID: "web", Holder: reqHolder}
	if got := x.f.last(t).in; got != want {
		t.Fatalf("filter = %+v", got)
	}
	rr = x.call("GET", "/api/requests/roles", "operator", "", nil)
	if strings.TrimSpace(rr.Body.String()) != `{"roles":[]}` {
		t.Errorf("no roles = %s", rr.Body.String())
	}
	var created struct {
		Request delivery.RequestView `json:"request"`
		Created bool                 `json:"created"`
	}
	rr = x.call("POST", "/api/requests", "operator", `{"kind":"ask","role":"supervisor","scope":{"kind":"project","id":"web"},"content":{"ask":"x"}}`, nil)
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil || !created.Created || created.Request.ID != reqHeld {
		t.Fatalf("create reply: %v %s", err, rr.Body.String())
	}

	for _, bad := range []struct {
		method, path, body string
		code               int
	}{
		{"PUT", "/api/requests", "", http.StatusMethodNotAllowed},
		{"POST", "/api/requests/mine", "", http.StatusMethodNotAllowed},
		{"POST", "/api/requests/roles", "", http.StatusMethodNotAllowed},
		{"GET", "/api/requests/r-held/take", "", http.StatusMethodNotAllowed},
		{"POST", "/api/requests/r-held/finish", "", http.StatusNotFound},
		{"POST", "/api/requests/r-held", "", http.StatusNotFound},
		{"POST", "/api/requests", `{"kinds":"ask"}`, http.StatusBadRequest},
		{"POST", "/api/requests", `{} {}`, http.StatusBadRequest},
		{"POST", "/api/requests/r-held/take", `[1]`, http.StatusBadRequest},
		{"POST", "/api/requests/r-held/drop", `{"reason":"x"}`, http.StatusBadRequest},
	} {
		before := x.f.count()
		rr := x.call(bad.method, bad.path, "holder", bad.body, nil)
		if rr.Code != bad.code || x.f.count() != before {
			t.Errorf("%s %s %s: %d (want %d), calls %d", bad.method, bad.path, bad.body, rr.Code, bad.code, x.f.count()-before)
		}
	}
	for _, e := range []error{delivery.ErrRequestState, delivery.ErrMayRunTwice, delivery.ErrRoleHeld, delivery.ErrCannotRecord, delivery.ErrCheckMethod, delivery.ErrReasonNeeded} {
		x.f.err = e
		rr := x.call("POST", "/api/requests/r-held/drop", "operator", `{"reason_code":"obsolete"}`, nil)
		if rr.Code != delivery.HTTPStatus(e) || !strings.Contains(rr.Body.String(), e.Error()) {
			t.Errorf("%v: %d %s", e, rr.Code, rr.Body.String())
		}
	}
	x.s.requests = nil
	if rr := x.call("GET", "/api/requests", "operator", "", nil); rr.Code != http.StatusServiceUnavailable {
		t.Errorf("not wired: %d", rr.Code)
	}
}

// A create takes only what a creator may write, and always has an occurrence
// key of the operator's form.
func TestReqSurfaceRESTCreateKeyAndContent(t *testing.T) {
	x := reqCallers(t)
	base := `{"kind":"ask","role":"supervisor","scope":{"kind":"project","id":"web"},`
	keys := map[string]bool{}
	for range 2 {
		x.call("POST", "/api/requests", "operator", base+`"content":{"ask":"x"}}`, nil)
		in := x.f.last(t).in.(delivery.CreateInput)
		if !strings.HasPrefix(in.Key, "operator:") || len(in.Key) < len("operator:")+16 {
			t.Fatalf("generated key = %q", in.Key)
		}
		keys[in.Key] = true
	}
	if len(keys) != 2 {
		t.Fatal("two creates share a generated key")
	}
	for given, want := range map[string]string{"mine-1": "operator:mine-1", "operator:mine-2": "operator:mine-2", "hello:sess-holder:1": "operator:hello:sess-holder:1"} {
		x.call("POST", "/api/requests", "operator", base+`"key":"`+given+`"}`, nil)
		if in := x.f.last(t).in.(delivery.CreateInput); in.Key != want {
			t.Errorf("key %q = %q, want %q", given, in.Key, want)
		}
	}
	before := x.f.count()
	for _, body := range []string{
		base + `"key":"has space"}`,
		base + `"content":{"receipt_code":"rc-1"}}`,
		base + `"content":{"key":"hello:s:1"}}`,
		base + `"content":{"report_text":"done"}}`,
		base + `"content":{"claimed_names":["x"]}}`,
	} {
		if rr := x.call("POST", "/api/requests", "operator", body, nil); rr.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", body, rr.Code)
		}
	}
	if x.f.count() != before {
		t.Fatal("a refused create reached the service")
	}
	rr := x.call("POST", "/api/requests", "operator", base+`"host":"another-box","content":{"ask":"x"}}`, nil)
	if rr.Code != http.StatusUnprocessableEntity || !strings.Contains(rr.Body.String(), delivery.ErrCrossHost.Error()) {
		t.Errorf("another host: %d %s", rr.Code, rr.Body.String())
	}
}

func (x *reqFixture) auditText(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(x.audit)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(b)
}

// An unknown ID is a 404 and an audit line that holds identifiers only.
func TestReqSurfaceUnknownIDIsAudited(t *testing.T) {
	x := reqCallers(t)
	for _, c := range []struct{ method, path, body, id, verb string }{
		{"POST", "/api/requests/r-invented1/take", `{"claimed_by":"` + reqMarker + `"}`, "r-invented1", "take"},
		{"POST", "/api/requests/r-invented2/report", `{"text":"` + reqMarker + `"}`, "r-invented2", "report"},
		{"GET", "/api/requests?id=r-invented3", "", "r-invented3", "get"},
		{"POST", "/api/requests/" + reqMarker + "%20words/accept", "", "invalid", "accept"},
		{"POST", "/api/queue/complete", `{"id":"r-invented4","result":{"x":"` + reqMarker + `"}}`, "r-invented4", "report"},
	} {
		rr := x.call(c.method, c.path, "holder", c.body, nil)
		if rr.Code != http.StatusNotFound {
			t.Errorf("%s %s: %d %s", c.method, c.path, rr.Code, rr.Body.String())
		}
		entries, err := x.s.auditLog.Read(audit.QueryFilter{Action: "request_unknown_id", Limit: 1})
		if err != nil || len(entries) != 1 {
			t.Fatalf("%s: audit entries %d %v", c.path, len(entries), err)
		}
		e := entries[0]
		if e.Actor != "session:"+reqHolder || e.Details["request_id"] != c.id || e.Details["verb"] != c.verb || e.Details["surface"] != "rest" || len(e.Details) != 3 {
			t.Errorf("%s: entry %+v", c.path, e)
		}
	}
	if strings.Contains(x.auditText(t), reqMarker) {
		t.Fatal("the audit log holds content")
	}
}

// Lists, errors, logs and audit hold no content.
func TestReqSurfaceRESTHoldsNoContent(t *testing.T) {
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(old)
	x := reqCallers(t)
	content := `"content":{"ask":"` + reqMarker + `","detail":"` + reqMarker + `","params":{"p":"` + reqMarker + `"},"items":[{"statement":"` + reqMarker + `","method":"confirm"}]}`
	var seen []string
	for _, c := range []struct{ method, path, who, body string }{
		{"POST", "/api/requests", "operator", `{"kind":"ask","role":"supervisor","scope":{"kind":"project","id":"web"},` + content + `}`},
		{"POST", "/api/requests", "operator", `{"` + reqMarker + `":1}`},
		{"POST", "/api/requests", "operator", `{"kind":"ask","content":{"receipt_code":"` + reqMarker + `"}}`},
		{"POST", "/api/requests/r-held/drop", "operator", `{"reason_code":"obsolete","words":"` + reqMarker + `"}`},
		{"POST", "/api/requests/r-held/report", "holder", `{"text":"` + reqMarker + `"}`},
		{"GET", "/api/requests", "operator", ""},
		{"GET", "/api/requests?id=r-held", "operator", ""},
		{"GET", "/api/requests/mine", "holder", ""},
		{"GET", "/api/requests/roles", "operator", ""},
	} {
		seen = append(seen, x.call(c.method, c.path, c.who, c.body, nil).Body.String())
	}
	for i, body := range seen {
		// The unknown field's name is the caller's own word, back to the caller.
		if i != 1 && strings.Contains(body, reqMarker) {
			t.Errorf("reply %d holds content: %s", i, body)
		}
	}
	if strings.Contains(logs.String(), reqMarker) || strings.Contains(x.auditText(t), reqMarker) {
		t.Fatal("a log holds content")
	}
}

func TestReqSurfaceOldQueue(t *testing.T) {
	x := reqCallers(t)
	// A claim ignores the body's claimed_by.
	for who, want := range map[string]string{"holder": reqHolder, "operator": "admin-token", "unknown": "session:unknown", "peer": "peer:other-box"} {
		if _, err := x.s.queueStore.Push("role-"+who, nil); err != nil {
			t.Fatal(err)
		}
		rr := x.call("POST", "/api/queue/claim", who, `{"role":"role-`+who+`","claimed_by":"somebody-else"}`, nil)
		var it session.QueueItem
		if err := json.Unmarshal(rr.Body.Bytes(), &it); err != nil || rr.Code != http.StatusOK || it.ClaimedBy != want {
			t.Errorf("claim by %s: %d claimed_by=%q want %q (%s)", who, rr.Code, it.ClaimedBy, want, rr.Body.String())
		}
	}
	// complete and fail on a request ID become the holder's report and decline.
	rr := x.call("POST", "/api/queue/complete", "holder", `{"id":"r-held","result":{"out":"ok"}}`, nil)
	hit := x.f.last(t)
	if rr.Code != http.StatusOK || hit.op != "report" || hit.in.(delivery.ReportInput).Text != `{"out":"ok"}` {
		t.Errorf("complete by holder: %d %+v %s", rr.Code, hit, rr.Body.String())
	}
	rr = x.call("POST", "/api/queue/fail", "holder", `{"id":"r-held","error":"cannot"}`, nil)
	hit = x.f.last(t)
	if in, _ := hit.in.(delivery.DeclineInput); rr.Code != http.StatusOK || hit.op != "decline" || in.ReasonCode != delivery.WhyDeclined || in.Text != "cannot" {
		t.Errorf("fail by holder: %d %+v", rr.Code, hit)
	}
	for _, who := range []string{"operator", "other", "unknown"} {
		for _, p := range []string{"complete", "fail"} {
			if rr := x.call("POST", "/api/queue/"+p, who, `{"id":"r-held"}`, nil); rr.Code != http.StatusForbidden {
				t.Errorf("%s by %s: %d %s", p, who, rr.Code, rr.Body.String())
			}
		}
	}
	// Without the request service an r- ID is not treated as a queue item that
	// anyone may complete.
	x.s.requests = nil
	if rr := x.call("POST", "/api/queue/complete", "operator", `{"id":"r-held"}`, nil); rr.Code == http.StatusOK {
		t.Errorf("complete with no request service: %d", rr.Code)
	}
}

// Nothing about a tracked request is sent to another computer.
func TestReqSurfaceProxyRefusesRequests(t *testing.T) {
	x := reqCallers(t)
	var mu sync.Mutex
	var got []string
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, r.Method+" "+r.URL.Path+" "+string(body))
		mu.Unlock()
		_, _ = w.Write([]byte(`{}`))
	}))
	defer remote.Close()
	x.s.cfg.Servers = []config.RemoteServerConfig{{Name: "box", URL: remote.URL, Enabled: true, Token: "remote-admin"}}

	create := `{"kind":"ask","role":"supervisor","scope":{"kind":"project","id":"web"},"content":{"ask":"x"}}`
	for _, c := range []struct{ method, path, body string }{
		{"POST", "/api/proxy/box/api/requests", create},
		{"GET", "/api/proxy/box/api/requests", ""},
		{"POST", "/api/proxy/box/api/requests/roles/give", `{}`},
		{"POST", "/api/proxy/box/api/requests/r-held/drop", `{}`},
		{"POST", "/api/proxy/box/api/%72equests", create},
		{"POST", "/api/proxy/box/api/mcp/call", `{"tool":"request_create","args":{}}`},
		{"POST", "/api/proxy/box/api/queue/complete", `{"id":"r-held"}`},
		{"POST", "/api/proxy/box/api/queue/fail", `{"id":"r-held"}`},
		{"POST", "/api/proxy/box/api/test/message", `{"text":"Request create scope=web ask=x"}`},
		// An unrelated field of another type is no way through: the other
		// computer reads only its own fields.
		{"POST", "/api/proxy/box/api/mcp/call", `{"tool":"request_create","id":5,"text":7}`},
		{"POST", "/api/proxy/box/api/queue/complete", `{"id":"r-held","tool":5,"text":7}`},
		{"POST", "/api/proxy/box/api/test/message", `{"text":"request list","id":5,"tool":7}`},
		{"POST", "/api/proxy/agent/a1/api/requests", create},
		{"POST", "/remote/box/api/requests", create},
	} {
		rr := x.call(c.method, c.path, "operator", c.body, nil)
		if rr.Code != http.StatusUnprocessableEntity || !strings.Contains(rr.Body.String(), delivery.ErrCrossHost.Error()) {
			t.Errorf("%s %s: %d %s", c.method, c.path, rr.Code, rr.Body.String())
		}
	}
	if len(got) != 0 {
		t.Fatalf("sent to the other computer: %v", got)
	}
	// Other work is still forwarded, with its body whole.
	for _, c := range []struct{ path, body string }{
		{"/api/proxy/box/api/queue/push", `{"role":"w"}`},
		{"/api/proxy/box/api/queue/complete", `{"id":"q-1"}`},
		{"/api/proxy/box/api/mcp/call", `{"tool":"queue_list","args":{}}`},
		{"/api/proxy/box/api/test/message", `{"text":"status"}`},
	} {
		mu.Lock()
		got = nil
		mu.Unlock()
		x.call("POST", c.path, "operator", c.body, nil)
		if len(got) != 1 || !strings.HasSuffix(got[0], " "+c.body) {
			t.Errorf("%s: forwarded %v", c.path, got)
		}
	}
	if x.f.count() != 0 {
		t.Error("a refused proxy call reached the local service")
	}
}

// The holder's own "done when" answer is recorded under its session, so the
// list reads it as the doer's claim.
func TestReqSurfaceHolderAcceptanceAnswerIsItsOwn(t *testing.T) {
	x := reqCallers(t)
	var got []string
	h := x.s.fedAuthMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		c := x.s.acceptanceCaller(r.Context(), acceptanceCallerFields{Via: "cli", ViaSession: "somebody-else", Actor: "operator"})
		got = append(got, c.Observed+"|"+c.SessionID)
	}))
	deliveryCall(h, "POST", "/api/acceptance/confirm", x.tokens["holder"], "", nil)
	if len(got) != 1 || got[0] != "session:"+reqHolder+"|"+reqHolder {
		t.Fatalf("holder's own answer recorded as %v", got)
	}
}

type reqAuditBridge struct {
	fakeMCPBridge
	record func(delivery.Caller, string, string)
}

func (b *reqAuditBridge) SetRequestAudit(fn func(delivery.Caller, string, string)) { b.record = fn }

// The assistant tools write the same audit line as REST: the web server hands
// them its writer when it is given the tool bridge.
func TestReqSurfaceBridgeGetsTheAuditWriter(t *testing.T) {
	x := reqCallers(t)
	b := &reqAuditBridge{}
	x.s.SetMCPBridge(b)
	if b.record == nil {
		t.Fatal("the bridge got no audit writer")
	}
	b.record(delivery.Caller{Kind: delivery.CallerSession, SessionID: "s9", Surface: delivery.SurfaceMCP}, "r-nope", "take")
	if text := x.auditText(t); !strings.Contains(text, "r-nope") || !strings.Contains(text, "session:s9") {
		t.Fatalf("audit = %s", text)
	}
}
