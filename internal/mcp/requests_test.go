package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/auth"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/delivery"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/server/multiserver"
	"github.com/dmz006/datawatch/internal/session"
)

const (
	mcpReqHolder = "sess-holder"
	mcpReqMarker = "MARKER-91aa-secret-words"
)

type mcpReqHit struct {
	op, id string
	caller delivery.Caller
	in     any
}

// mcpFakeRequests refuses as the real store does.
type mcpFakeRequests struct {
	mu   sync.Mutex
	hits []mcpReqHit
}

func (f *mcpFakeRequests) note(op, id string, c delivery.Caller, in any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hits = append(f.hits, mcpReqHit{op, id, c, in})
	if c.Kind == delivery.CallerPeer {
		return delivery.ErrCrossHost
	}
	if id != "" && id != "r-held" {
		return delivery.ErrRequestNotFound
	}
	return nil
}

func (f *mcpFakeRequests) view() delivery.RequestView {
	return delivery.RequestView{Request: delivery.Request{ID: "r-held", Kind: delivery.ReqAsk, State: delivery.StateWaiting}, Label: delivery.LabelWaiting}
}
func (f *mcpFakeRequests) holder(op, id string, c delivery.Caller, in any) (delivery.RequestView, error) {
	if err := f.note(op, id, c, in); err != nil {
		return delivery.RequestView{}, err
	}
	if !c.Holds(mcpReqHolder) {
		return delivery.RequestView{}, delivery.ErrNotHolder
	}
	return f.view(), nil
}
func (f *mcpFakeRequests) action(op, id string, c delivery.Caller, in any) (delivery.RequestView, error) {
	if err := f.note(op, id, c, in); err != nil {
		return delivery.RequestView{}, err
	}
	return f.view(), nil
}
func (f *mcpFakeRequests) Create(_ context.Context, c delivery.Caller, in delivery.CreateInput) (delivery.RequestView, bool, error) {
	v, err := f.action("create", "", c, in)
	return v, err == nil, err
}
func (f *mcpFakeRequests) Take(_ context.Context, c delivery.Caller, id string, in delivery.TakeInput) (delivery.TakeReply, error) {
	v, err := f.holder("take", id, c, in)
	if err != nil {
		return delivery.TakeReply{}, err
	}
	return delivery.TakeReply{Request: v, Content: mcpReqMarker, Digest: "d1"}, nil
}
func (f *mcpFakeRequests) Seen(_ context.Context, c delivery.Caller, id string, in delivery.SeenInput) (delivery.RequestView, error) {
	return f.holder("seen", id, c, in)
}
func (f *mcpFakeRequests) Accept(_ context.Context, c delivery.Caller, id string, in delivery.AcceptInput) (delivery.RequestView, error) {
	return f.holder("accept", id, c, in)
}
func (f *mcpFakeRequests) Report(_ context.Context, c delivery.Caller, id string, in delivery.ReportInput) (delivery.RequestView, error) {
	return f.holder("report", id, c, in)
}
func (f *mcpFakeRequests) Decline(_ context.Context, c delivery.Caller, id string, in delivery.DeclineInput) (delivery.RequestView, error) {
	return f.holder("decline", id, c, in)
}
func (f *mcpFakeRequests) Mine(_ context.Context, c delivery.Caller) (delivery.MineReply, error) {
	if err := f.note("mine", "", c, nil); err != nil {
		return delivery.MineReply{}, err
	}
	if c.Kind != delivery.CallerSession {
		return delivery.MineReply{}, delivery.ErrNotHolder
	}
	return delivery.MineReply{SessionID: c.SessionID}, nil
}
func (f *mcpFakeRequests) Get(_ context.Context, c delivery.Caller, id string) (delivery.RequestView, error) {
	return f.action("get", id, c, nil)
}
func (f *mcpFakeRequests) List(_ context.Context, c delivery.Caller, fl delivery.RequestFilter) ([]delivery.RequestView, error) {
	return []delivery.RequestView{f.view()}, f.note("list", "", c, fl)
}
func (f *mcpFakeRequests) Roles(_ context.Context, c delivery.Caller) ([]delivery.RoleBinding, error) {
	return nil, f.note("roles", "", c, nil)
}
func (f *mcpFakeRequests) GiveRole(_ context.Context, c delivery.Caller, in delivery.RoleInput) (delivery.RoleBinding, error) {
	return delivery.RoleBinding{SessionID: in.SessionID}, f.note("give", "", c, in)
}
func (f *mcpFakeRequests) ReleaseRole(_ context.Context, c delivery.Caller, in delivery.RoleInput) error {
	return f.note("release", "", c, in)
}
func (f *mcpFakeRequests) Reissue(_ context.Context, c delivery.Caller, id string, in delivery.ReasonInput) (delivery.RequestView, error) {
	return f.action("reissue", id, c, in)
}
func (f *mcpFakeRequests) Drop(_ context.Context, c delivery.Caller, id string, in delivery.ReasonInput) (delivery.RequestView, error) {
	return f.action("drop", id, c, in)
}
func (f *mcpFakeRequests) Force(_ context.Context, c delivery.Caller, id string, in delivery.ForceInput) (delivery.RequestView, error) {
	return f.action("force", id, c, in)
}
func (f *mcpFakeRequests) AcceptUnproven(_ context.Context, c delivery.Caller, id string, in delivery.ReasonInput) (delivery.RequestView, error) {
	return f.action("accept-unproven", id, c, in)
}
func (f *mcpFakeRequests) FromBacklog(_ context.Context, c delivery.Caller, in delivery.BacklogInput) (delivery.RequestView, bool, error) {
	v, err := f.action("from-backlog", "", c, in)
	return v, err == nil, err
}

func (f *mcpFakeRequests) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.hits)
}
func (f *mcpFakeRequests) last(t *testing.T) mcpReqHit {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.hits) == 0 {
		t.Fatal("the service was not called")
	}
	return f.hits[len(f.hits)-1]
}

func newRequestMCP(t *testing.T) (*Server, *mcpFakeRequests) {
	t.Helper()
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	allowTestTemp(mgr)
	trackSessionManager(t, mgr)
	mgr.WithFakeTmux()
	// No web port: a request tool that called the daemon back would fail.
	s := New("testhost", mgr, &config.MCPConfig{Enabled: true}, dir, Options{Version: "test"})
	f := &mcpFakeRequests{}
	s.SetRequests(f)
	q, err := session.NewQueueStore(filepath.Join(dir, "queue-test.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.queueStore = q
	return s, f
}

func (s *Server) reqCall(t *testing.T, ctx context.Context, tool string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := s.CallTool(ctx, tool, args)
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if text, ok := c["text"].(string); ok {
			b.WriteString(text)
		}
	}
	return b.String(), res.IsError
}

func ctxSession(id string) context.Context {
	return federation.WithCallerSessionID(federation.ContextWithPrincipal(context.Background(), "session-scoped"), id)
}

var mcpReqCalls = []struct {
	tool, op string
	args     map[string]any
	holder   bool
}{
	{"request_mine", "mine", nil, false},
	{"request_take", "take", map[string]any{"id": "r-held"}, true},
	{"request_accept", "accept", map[string]any{"id": "r-held", "code": "rc-1"}, true},
	{"request_report", "report", map[string]any{"id": "r-held", "text": "finished"}, true},
	{"request_decline", "decline", map[string]any{"id": "r-held", "reason_code": "declined"}, true},
	{"request_create", "create", map[string]any{"scope_id": "web", "ask": "x"}, false},
	{"request_list", "list", nil, false},
	{"request_list", "get", map[string]any{"id": "r-held"}, false},
	{"request_list", "roles", map[string]any{"roles": true}, false},
	{"request_role", "give", map[string]any{"action": "give", "scope_id": "web", "session_id": mcpReqHolder}, false},
	{"request_role", "release", map[string]any{"action": "release", "scope_id": "web", "session_id": mcpReqHolder}, false},
	{"request_manage", "reissue", map[string]any{"action": "reissue", "id": "r-held", "reason_code": "again", "repeat_ok": true}, false},
	{"request_manage", "drop", map[string]any{"action": "drop", "id": "r-held", "reason_code": "obsolete"}, false},
	{"request_manage", "force", map[string]any{"action": "force", "id": "r-held", "reason_code": "stuck"}, false},
	{"request_manage", "accept-unproven", map[string]any{"action": "accept-unproven", "id": "r-held", "reason_code": "seen", "words": "I looked"}, false},
	{"request_manage", "from-backlog", map[string]any{"action": "from-backlog", "entry_key": "task:t1:failed", "scope_id": "web", "ask": "x"}, false},
}

// Every tool by every kind of caller. The service is called in this process
// with a caller built from the credential; the tool server's own session field
// never makes a holder.
func TestReqSurfaceMCPCallerByCredential(t *testing.T) {
	peerCtx := context.WithValue(context.Background(), mcpFedPeerKey, &multiserver.Entry{Name: "box", Capabilities: []string{"full-control"}})
	callers := []struct {
		name  string
		ctx   context.Context
		field string
		want  delivery.Caller
	}{
		{"operator", federation.ContextWithPrincipal(context.Background(), "admin"), "", delivery.Caller{Kind: delivery.CallerOperator}},
		{"local", context.Background(), "", delivery.Caller{Kind: delivery.CallerOperator}},
		{"holder", ctxSession(mcpReqHolder), "", delivery.Caller{Kind: delivery.CallerSession, SessionID: mcpReqHolder}},
		{"other", ctxSession("sess-other"), "", delivery.Caller{Kind: delivery.CallerSession, SessionID: "sess-other"}},
		{"unknown", federation.ContextWithPrincipal(context.Background(), "session-scoped"), "", delivery.Caller{Kind: delivery.CallerSessionUnknown}},
		{"instance-token", ctxSession(auth.InstanceTokenOwnerPrefix + "x"), "", delivery.Caller{Kind: delivery.CallerSessionUnknown}},
		{"peer", peerCtx, "", delivery.Caller{Kind: delivery.CallerPeer, Peer: "box"}},
		{"peer-principal", federation.ContextWithPrincipal(context.Background(), "peer:box2"), "", delivery.Caller{Kind: delivery.CallerPeer, Peer: "box2"}},
		{"server-field", context.Background(), mcpReqHolder, delivery.Caller{Kind: delivery.CallerSessionUnknown, ClaimedSession: mcpReqHolder}},
	}
	s, f := newRequestMCP(t)
	for _, who := range callers {
		s.SetCallerSessionID(who.field)
		for _, c := range mcpReqCalls {
			before := f.count()
			text, isErr := s.reqCall(t, who.ctx, c.tool, c.args)
			if f.count() != before+1 {
				t.Fatalf("%s by %s: service calls %d: %s", c.op, who.name, f.count()-before, text)
			}
			hit := f.last(t)
			want := who.want
			want.Surface = delivery.SurfaceMCP
			if hit.op != c.op || hit.caller != want {
				t.Errorf("%s by %s: %s %+v", c.op, who.name, hit.op, hit.caller)
			}
			wantErr := who.want.Kind == delivery.CallerPeer ||
				(c.holder && who.name != "holder") ||
				(c.op == "mine" && who.want.Kind != delivery.CallerSession)
			if isErr != wantErr {
				t.Errorf("%s by %s: error=%v: %s", c.op, who.name, isErr, text)
			}
			if isErr && who.want.Kind != delivery.CallerPeer && !strings.Contains(text, "HTTP 403") {
				t.Errorf("%s by %s: refusal %q", c.op, who.name, text)
			}
			if who.want.Kind == delivery.CallerPeer && !strings.Contains(text, delivery.ErrCrossHost.Error()) {
				t.Errorf("%s by %s: refusal %q", c.op, who.name, text)
			}
		}
	}
}

func TestReqSurfaceMCPArgumentsNeverHold(t *testing.T) {
	s, f := newRequestMCP(t)
	for _, tool := range []string{"request_take", "request_accept", "request_report", "request_decline"} {
		args := map[string]any{"id": "r-held", "claimed_by": mcpReqHolder, "session_id": mcpReqHolder}
		if tool == "request_decline" {
			args["reason_code"] = "declined"
		}
		text, isErr := s.reqCall(t, ctxSession("sess-other"), tool, args)
		c := f.last(t).caller
		if !isErr || c.Holds(mcpReqHolder) || c.ClaimedName != mcpReqHolder || c.ClaimedSession != mcpReqHolder {
			t.Errorf("%s: %v %+v %s", tool, isErr, c, text)
		}
	}
}

func TestReqSurfaceMCPInputs(t *testing.T) {
	s, f := newRequestMCP(t)
	ctx := ctxSession(mcpReqHolder)
	in := func() any { return f.last(t).in }

	s.reqCall(t, ctx, "request_take", map[string]any{"id": "r-held"})
	if in() != (delivery.TakeInput{Via: delivery.ViaTool}) {
		t.Errorf("take = %+v", in())
	}
	s.reqCall(t, ctx, "request_report", map[string]any{"id": "r-held", "text": "t", "result_id": "res", "rules_version": "v1"})
	if in() != (delivery.ReportInput{Text: "t", ResultID: "res", RulesVersion: "v1"}) {
		t.Errorf("report = %+v", in())
	}
	s.reqCall(t, ctx, "request_create", map[string]any{
		"scope_id": "web", "ask": "a", "detail": "d", "key": "k1", "take_by": "30m", "due": "2h", "after": "r-held",
		"source_type": "bug", "source_id": "b1", "host": "elsewhere", "project_dir": "/p",
		"params": `{"p":"v"}`, "items": `[{"statement":"s","method":"confirm"}]`,
	})
	want := delivery.CreateInput{Kind: delivery.ReqAsk, Role: delivery.RoleSupervisor, Scope: delivery.Scope{Kind: delivery.ScopeProject, ID: "web"},
		Host: "elsewhere", Key: "operator:k1", Source: delivery.SourceRef{Type: "bug", ID: "b1"}, After: "r-held",
		TakeBy: 30 * time.Minute, Due: 2 * time.Hour,
		Content: delivery.RequestContent{Ask: "a", Detail: "d", ProjectDir: "/p", Params: map[string]string{"p": "v"}, Items: []delivery.DoneItem{{Statement: "s", Method: "confirm"}}}}
	if got := in(); !reflect.DeepEqual(got, want) {
		t.Errorf("create = %+v", got)
	}
	s.reqCall(t, ctx, "request_list", map[string]any{"state": "waiting", "kind": "ask", "role": "supervisor", "scope_kind": "project", "scope_id": "web", "holder": mcpReqHolder})
	if in() != (delivery.RequestFilter{State: "waiting", Kind: "ask", Role: "supervisor", ScopeKind: "project", ScopeID: "web", Holder: mcpReqHolder}) {
		t.Errorf("filter = %+v", in())
	}
	s.reqCall(t, ctx, "request_role", map[string]any{"action": "give", "scope_kind": "project", "scope_id": "web", "session_id": "s2", "force": "true", "reason_code": "stuck"})
	if in() != (delivery.RoleInput{Role: delivery.RoleSupervisor, Scope: delivery.Scope{Kind: delivery.ScopeProject, ID: "web"}, SessionID: "s2", Force: true, ReasonCode: "stuck"}) {
		t.Errorf("role = %+v", in())
	}
	s.reqCall(t, ctx, "request_manage", map[string]any{"action": "accept-unproven", "id": "r-held", "reason_code": "seen", "words": "I looked"})
	if in() != (delivery.ReasonInput{ReasonCode: "seen", Words: "I looked"}) {
		t.Errorf("accept-unproven = %+v", in())
	}
	s.reqCall(t, ctx, "request_manage", map[string]any{"action": "force", "id": "r-held", "reason_code": "stuck", "to_session": "s2"})
	if in() != (delivery.ForceInput{ReasonCode: "stuck", ToSession: "s2"}) {
		t.Errorf("force = %+v", in())
	}

	before := f.count()
	for _, bad := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"request_take", map[string]any{}, "id"},
		{"request_decline", map[string]any{"id": "r-held"}, "reason_code"},
		{"request_create", map[string]any{"ask": "x"}, "scope_id"},
		{"request_create", map[string]any{"scope_id": "web", "take_by": "soon"}, "take_by"},
		{"request_create", map[string]any{"scope_id": "web", "items": `[{"statement":"s","method":"command","command":"rm"}]`}, "items"},
		{"request_create", map[string]any{"scope_id": "web", "scop": "x"}, "unknown argument"},
		{"request_create", map[string]any{"scope_id": "web", "key": "has space"}, "key"},
		{"request_list", map[string]any{"stat": "waiting"}, "unknown argument"},
		{"request_role", map[string]any{"action": "take", "scope_id": "web", "session_id": "s"}, "give or release"},
		{"request_manage", map[string]any{"action": "finish", "id": "r-held"}, "action"},
		{"request_manage", map[string]any{"action": "accept-unproven", "id": "r-held", "reason_code": "seen"}, "words"},
		{"request_manage", map[string]any{"action": "drop", "reason_code": "obsolete"}, "id"},
		{"request_manage", map[string]any{"action": "from-backlog", "scope_id": "web"}, "entry_key"},
	} {
		text, isErr := s.reqCall(t, ctx, bad.tool, bad.args)
		if !isErr || !strings.Contains(text, bad.want) {
			t.Errorf("%s %v: %v %q", bad.tool, bad.args, isErr, text)
		}
	}
	if f.count() != before {
		t.Error("a refused call reached the service")
	}
}

func TestReqSurfaceMCPUnknownIDIsAudited(t *testing.T) {
	s, _ := newRequestMCP(t)
	var got []string
	s.SetRequestAudit(func(c delivery.Caller, id, verb string) { got = append(got, c.Observed()+" "+id+" "+verb) })
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"request_take", map[string]any{"id": "r-invented"}},
		{"request_list", map[string]any{"id": "r-invented"}},
		{"request_manage", map[string]any{"action": "drop", "id": "r-invented", "reason_code": "x"}},
		{"queue_complete", map[string]any{"id": "r-invented"}},
	} {
		if text, isErr := s.reqCall(t, ctxSession(mcpReqHolder), c.tool, c.args); !isErr || !strings.Contains(text, "HTTP 404") {
			t.Errorf("%s: %v %s", c.tool, isErr, text)
		}
	}
	want := []string{"session:sess-holder r-invented take", "session:sess-holder r-invented get", "session:sess-holder r-invented drop", "session:sess-holder r-invented report"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("audit = %v", got)
	}
}

// Only the holder's take returns content.
func TestReqSurfaceMCPListsHoldNoContent(t *testing.T) {
	s, _ := newRequestMCP(t)
	for _, c := range mcpReqCalls {
		text, _ := s.reqCall(t, ctxSession(mcpReqHolder), c.tool, c.args)
		if c.op != "take" && strings.Contains(text, mcpReqMarker) {
			t.Errorf("%s holds content: %s", c.op, text)
		}
	}
	text, _ := s.reqCall(t, ctxSession(mcpReqHolder), "request_take", map[string]any{"id": "r-held"})
	var reply delivery.TakeReply
	if err := json.Unmarshal([]byte(text), &reply); err != nil || reply.Content != mcpReqMarker || reply.Digest != "d1" {
		t.Fatalf("take = %s", text)
	}
}

func TestReqSurfaceMCPOldQueue(t *testing.T) {
	s, f := newRequestMCP(t)
	for who, ctx := range map[string]context.Context{mcpReqHolder: ctxSession(mcpReqHolder), "admin-token": context.Background()} {
		if _, err := s.queueStore.Push("role-"+who, nil); err != nil {
			t.Fatal(err)
		}
		for _, args := range []map[string]any{{"role": "role-" + who, "claimed_by": "somebody-else"}} {
			text, isErr := s.reqCall(t, ctx, "queue_claim", args)
			var it session.QueueItem
			if err := json.Unmarshal([]byte(text), &it); isErr || err != nil || it.ClaimedBy != who {
				t.Errorf("claim by %s: %s", who, text)
			}
		}
	}
	// claimed_by is no longer needed.
	if text, isErr := s.reqCall(t, context.Background(), "queue_claim", map[string]any{"role": "empty"}); isErr {
		t.Errorf("claim without claimed_by: %s", text)
	}
	text, isErr := s.reqCall(t, ctxSession(mcpReqHolder), "queue_complete", map[string]any{"id": "r-held", "result": `{"out":"ok"}`})
	if hit := f.last(t); isErr || hit.op != "report" || hit.in.(delivery.ReportInput).Text != `{"out":"ok"}` {
		t.Errorf("complete: %v %s %+v", isErr, text, hit)
	}
	text, isErr = s.reqCall(t, ctxSession(mcpReqHolder), "queue_fail", map[string]any{"id": "r-held", "error": "cannot"})
	if hit := f.last(t); isErr || hit.op != "decline" || hit.in != (delivery.DeclineInput{ReasonCode: delivery.WhyDeclined, Text: "cannot"}) {
		t.Errorf("fail: %v %s %+v", isErr, text, hit)
	}
	for _, tool := range []string{"queue_complete", "queue_fail"} {
		if text, isErr := s.reqCall(t, ctxSession("sess-other"), tool, map[string]any{"id": "r-held", "error": "x"}); !isErr || !strings.Contains(text, "HTTP 403") {
			t.Errorf("%s by another: %v %s", tool, isErr, text)
		}
	}
}

func TestReqSurfaceMCPNotWired(t *testing.T) {
	s, _ := newRequestMCP(t)
	s.SetRequests(nil)
	for _, c := range mcpReqCalls {
		if text, isErr := s.reqCall(t, ctxSession(mcpReqHolder), c.tool, c.args); !isErr || !strings.Contains(text, "requests are not available") {
			t.Errorf("%s: %s", c.tool, text)
		}
	}
}

// The nine tools of the contract are registered, listed, capped on the queue
// capabilities and classified. There is no tool for "seen".
func TestReqSurfaceMCPToolsAndCaps(t *testing.T) {
	s, _ := newRequestMCP(t)
	tools, err := s.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tool := range tools {
		if strings.HasPrefix(tool.Name, "request_") {
			got = append(got, tool.Name)
		}
	}
	slices.Sort(got)
	if !reflect.DeepEqual(got, delivery.RequestToolNames) {
		t.Fatalf("request tools = %v", got)
	}
	for _, name := range delivery.RequestToolNames {
		want := federation.CapQueueWrite
		if name == delivery.ToolRequestMine || name == delivery.ToolRequestList {
			want = federation.CapQueueRead
		}
		if got := federation.MCPToolCap[name]; got != want {
			t.Errorf("cap of %s = %q, want %q", name, got, want)
		}
		if !federation.IsNonConfigWriteTool(name) {
			t.Errorf("%s is not classified", name)
		}
	}
	docs := map[string]bool{}
	for _, d := range s.ToolDocs() {
		docs[d.Name] = true
	}
	for _, name := range delivery.RequestToolNames {
		if !docs[name] {
			t.Errorf("%s is not in the tool reference", name)
		}
	}
}
