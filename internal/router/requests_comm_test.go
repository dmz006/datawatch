package router

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/delivery"
	"github.com/dmz006/datawatch/internal/federation"
)

const chatReqMarker = "MARKER-c0de-secret-words"

type reqCommHit struct {
	method, path, caller, surface, auth string
	body                                map[string]any
}

type reqCommREST struct {
	mu     sync.Mutex
	hits   []reqCommHit
	status int
	refuse string
}

func (f *reqCommREST) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	hit := reqCommHit{method: r.Method, path: r.URL.RequestURI(), caller: r.Header.Get(federation.HeaderDeliveryCaller),
		surface: r.Header.Get(federation.HeaderRequestSurface), auth: r.Header.Get("Authorization")}
	if data, _ := io.ReadAll(r.Body); len(data) > 0 {
		_ = json.Unmarshal(data, &hit.body)
	}
	f.hits = append(f.hits, hit)
	if f.status != 0 {
		http.Error(w, f.refuse, f.status)
		return
	}
	view := delivery.RequestView{Request: delivery.Request{ID: "r-1a2b", Kind: delivery.ReqAsk, Role: delivery.RoleSupervisor,
		Scope: delivery.Scope{Kind: delivery.ScopeProject, ID: "web"}, State: delivery.StateReported}, Label: delivery.LabelNotAccepted}
	var out any = view
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/requests" && r.URL.Query().Get("id") == "":
		out = map[string]any{"requests": []delivery.RequestView{view}}
	case r.URL.Path == "/api/requests/roles":
		out = map[string]any{"roles": []delivery.RoleBinding{{Role: delivery.RoleSupervisor, Scope: view.Scope, SessionID: "h-s1"}}}
	case r.URL.Path == "/api/requests/roles/give":
		out = delivery.RoleBinding{Role: delivery.RoleSupervisor, Scope: view.Scope, SessionID: "h-s1"}
	case r.URL.Path == "/api/requests/roles/release":
		out = map[string]any{"ok": true}
	case r.URL.Path == "/api/requests/mine":
		out = delivery.MineReply{SessionID: "h-s1", Items: []delivery.RequestView{view}}
	case r.Method == http.MethodPost && (r.URL.Path == "/api/requests" || r.URL.Path == "/api/requests/from-backlog"):
		out = map[string]any{"request": view, "created": true}
	case strings.HasSuffix(r.URL.Path, "/take"):
		out = delivery.TakeReply{Request: view, Content: chatReqMarker, Digest: "d1"}
	}
	_ = json.NewEncoder(w).Encode(out)
}

func newRequestRouter(t *testing.T) (*Router, *reqCommREST) {
	t.Helper()
	f := &reqCommREST{}
	ts := httptest.NewServer(f)
	t.Cleanup(ts.Close)
	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return &Router{hostname: "h", webPort: port, webToken: "admin-tok"}, f
}

func TestReqSurfaceChatParse(t *testing.T) {
	for in, want := range map[string]struct{ verb, rest string }{
		"request":                           {"list", ""},
		"Request list state=waiting":        {"list", "state=waiting"},
		"request SHOW r-1":                  {"show", "r-1"},
		"request  create scope=web ask=Fix": {"create", "scope=web ask=Fix"},
		"request role give session=s":       {"role", "give session=s"},
		"request accept-unproven r-1":       {"accept-unproven", "r-1"},
	} {
		c := Parse(in)
		if c.Type != CmdRequest || c.RequestVerb != want.verb || c.Text != want.rest {
			t.Errorf("Parse(%q) = type %s verb %q rest %q", in, c.Type, c.RequestVerb, c.Text)
		}
	}
	// An ordinary message that starts with the word is not a command.
	for _, in := range []string{"request more detail please", "requests", "requestx list", "request_take r-1", "please request list"} {
		if c := Parse(in); c.Type == CmdRequest {
			t.Errorf("Parse(%q) captured", in)
		}
	}
}

func TestReqSurfaceChatCommands(t *testing.T) {
	scope := map[string]any{"kind": "project", "id": "web"}
	cases := []struct {
		say, method, path string
		body              map[string]any
		replyHas          []string
	}{
		{"request", "GET", "/api/requests", nil, []string{"r-1a2b", delivery.LabelNotAccepted}},
		{"request list state=waiting scope=web holder=h-s1", "GET", "/api/requests?holder=h-s1&scope_id=web&state=waiting", nil, []string{"r-1a2b"}},
		{"request show r-1a2b", "GET", "/api/requests?id=r-1a2b", nil, []string{"r-1a2b", "state reported"}},
		{"request mine", "GET", "/api/requests/mine", nil, []string{"session h-s1"}},
		{"request roles", "GET", "/api/requests/roles", nil, []string{"supervisor@project:web", "h-s1"}},
		{"request role give session=h-s1 scope=web force=true reason=stuck", "POST", "/api/requests/roles/give",
			map[string]any{"role": "supervisor", "scope": scope, "session_id": "h-s1", "force": true, "reason_code": "stuck"}, []string{"h-s1"}},
		{"request role release scope=web", "POST", "/api/requests/roles/release",
			map[string]any{"role": "supervisor", "scope": scope, "session_id": ""}, []string{"released"}},
		{"request take r-1a2b", "POST", "/api/requests/r-1a2b/take", map[string]any{}, []string{"r-1a2b"}},
		{"request accept r-1a2b code=rc-9", "POST", "/api/requests/r-1a2b/accept", map[string]any{"code": "rc-9"}, []string{"r-1a2b"}},
		{"request report r-1a2b text=all of it, a=b too", "POST", "/api/requests/r-1a2b/report", map[string]any{"text": "all of it, a=b too"}, []string{"r-1a2b"}},
		{"request decline r-1a2b reason=declined text=no time", "POST", "/api/requests/r-1a2b/decline",
			map[string]any{"reason_code": "declined", "text": "no time"}, []string{"r-1a2b"}},
		{"request reissue r-1a2b reason=again repeat_ok=true", "POST", "/api/requests/r-1a2b/reissue",
			map[string]any{"reason_code": "again", "repeat_ok": true}, []string{"r-1a2b"}},
		{"request drop r-1a2b reason=obsolete words=not needed", "POST", "/api/requests/r-1a2b/drop",
			map[string]any{"reason_code": "obsolete", "words": "not needed"}, []string{"r-1a2b"}},
		{"request force r-1a2b reason=stuck to=h-s2", "POST", "/api/requests/r-1a2b/force",
			map[string]any{"reason_code": "stuck", "to_session": "h-s2"}, []string{"r-1a2b", "may run twice"}},
		{"request accept-unproven r-1a2b reason=seen words=I looked", "POST", "/api/requests/r-1a2b/accept-unproven",
			map[string]any{"reason_code": "seen", "words": "I looked"}, []string{"r-1a2b"}},
	}
	for _, c := range cases {
		r, f := newRequestRouter(t)
		reply := deliverySay(r, c.say, "")
		if len(f.hits) != 1 {
			t.Errorf("%q: %d calls, reply %q", c.say, len(f.hits), reply)
			continue
		}
		hit := f.hits[0]
		if hit.method != c.method || hit.path != c.path || hit.surface != "chat" || hit.caller != "" || hit.auth != "Bearer admin-tok" {
			t.Errorf("%q: %+v", c.say, hit)
		}
		if c.body != nil && !reflect.DeepEqual(hit.body, c.body) && !(len(c.body) == 0 && len(hit.body) == 0) {
			t.Errorf("%q: body %v, want %v", c.say, hit.body, c.body)
		}
		for _, want := range c.replyHas {
			if !strings.Contains(reply, want) {
				t.Errorf("%q: reply %q lacks %q", c.say, reply, want)
			}
		}
		if strings.Contains(reply, chatReqMarker) {
			t.Errorf("%q: chat shows content: %q", c.say, reply)
		}
	}
}

func TestReqSurfaceChatCreate(t *testing.T) {
	r, f := newRequestRouter(t)
	reply := deliverySay(r, "request create scope=web take_by=30m key=k1 ask=Fix the "+chatReqMarker+" scope=other", "")
	if len(f.hits) != 1 {
		t.Fatalf("calls %d: %s", len(f.hits), reply)
	}
	b := f.hits[0].body
	content, _ := b["content"].(map[string]any)
	if b["kind"] != "ask" || b["role"] != "supervisor" || b["key"] != "operator:k1" || b["take_by"] != float64(30*60*1e9) ||
		content["ask"] != "Fix the "+chatReqMarker+" scope=other" || !reflect.DeepEqual(b["scope"], map[string]any{"kind": "project", "id": "web"}) {
		t.Errorf("body = %v", b)
	}
	if !strings.Contains(reply, "r-1a2b") || !strings.Contains(reply, "created") || strings.Contains(reply, chatReqMarker) {
		t.Errorf("reply = %q", reply)
	}
	r, f = newRequestRouter(t)
	reply = deliverySay(r, "request from-backlog task:t1:failed scope=web ask=Redo it", "")
	if len(f.hits) != 1 || f.hits[0].path != "/api/requests/from-backlog" || f.hits[0].body["entry_key"] != "task:t1:failed" {
		t.Errorf("from-backlog: %v %s", f.hits, reply)
	}
}

// A chat message sent for a session or a peer names it, so the daemon never
// reads it as the operator or as a holder. A rest command on a request route
// does too.
func TestReqSurfaceChatCaller(t *testing.T) {
	for _, say := range []string{"request take r-1a2b", "request drop r-1a2b reason=x", "rest POST /api/requests/r-1a2b/take"} {
		r, f := newRequestRouter(t)
		deliverySay(r, say, "session:h-s1")
		if len(f.hits) != 1 || f.hits[0].caller != "session:h-s1" || f.hits[0].surface != "chat" {
			t.Errorf("%q: %+v", say, f.hits)
		}
	}
}

func TestReqSurfaceChatRefusals(t *testing.T) {
	for _, say := range []string{
		"request show", "request take", "request decline r-1", "request create ask=" + chatReqMarker,
		"request create scope=web " + chatReqMarker, "request create scope=web nonsense=" + chatReqMarker,
		"request list nonsense=1", "request role take scope=web", "request role give scope=web",
		"request reissue r-1", "request accept-unproven r-1 reason=x", "request take r-1 r-2", "request from-backlog scope=web",
		"request mine now", "request drop reason=x",
	} {
		r, f := newRequestRouter(t)
		reply := deliverySay(r, say, "")
		if len(f.hits) != 0 || !strings.Contains(reply, "usage: request") || strings.Contains(reply, chatReqMarker) {
			t.Errorf("%q: %d calls, %q", say, len(f.hits), reply)
		}
	}
	r, f := newRequestRouter(t)
	f.status, f.refuse = http.StatusForbidden, delivery.ErrNotHolder.Error()
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(old)
	reply := deliverySay(r, "request report r-1 text="+chatReqMarker, "")
	if !strings.Contains(reply, "HTTP 403") || strings.Contains(reply, chatReqMarker) || strings.Contains(logs.String(), chatReqMarker) {
		t.Errorf("refusal: %q", reply)
	}
}

func TestReqSurfaceChatHelp(t *testing.T) {
	help := HelpText("h")
	for _, want := range []string{"request create", "request take", "request accept-unproven", "request role give"} {
		if !strings.Contains(help, want) {
			t.Errorf("help lacks %q", want)
		}
	}
}
