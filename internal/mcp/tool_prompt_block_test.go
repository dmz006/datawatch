// The MCP tools that type into a session refuse while that session shows a
// prompt waiting for the operator.

package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

const blockApprovalPane = ` Bash command

   rm -rf ./build

 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and don't ask again for rm commands
   3. No, and tell Claude what to do differently (esc)
`

// blockSafetyPane carries no approval menu wording beyond its own footer.
const blockSafetyPane = ` This command was flagged by a safety check. Only a person may approve it.

 ❯ 1. Continue
   2. Stop

 Enter to confirm · Esc to cancel
`

const blockIdlePane = `⏺ Done. The build passes.

╭──────────────────────────────╮
│ >                            │
╰──────────────────────────────╯
  ? for shortcuts
`

type auditSink struct {
	mu     sync.Mutex
	events []session.InputAuditEvent
}

func (a *auditSink) add(ev session.InputAuditEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, ev)
}

func (a *auditSink) all() []session.InputAuditEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]session.InputAuditEvent(nil), a.events...)
}

func newBlockServer(t *testing.T, pane string) (*Server, *session.FakeTmux, *auditSink) {
	t.Helper()
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/true", time.Hour)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	f := mgr.WithFakeTmux()
	now := time.Now()
	sess := &session.Session{
		ID: "ab12", FullID: "testhost-ab12", Hostname: "testhost",
		TmuxSession: "cs-ab12", BackendFamily: "claude-code",
		State: session.StateWaitingInput, CreatedAt: now, UpdatedAt: now,
	}
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	f.Pane[sess.TmuxSession] = pane
	sink := &auditSink{}
	mgr.SetInputAuditHook(sink.add)
	lib, err := session.NewCmdLibrary(filepath.Join(dir, "commands.json"))
	if err != nil {
		t.Fatalf("NewCmdLibrary: %v", err)
	}
	if _, err := lib.Add("block-test-yes", "yes"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	return New("testhost", mgr, &config.MCPConfig{}, dir, Options{CmdLib: lib}), f, sink
}

func blockTyped(f *session.FakeTmux) []session.FakeTmuxCall {
	var out []session.FakeTmuxCall
	for _, c := range f.CallsSnapshot() {
		switch c.Op {
		case "send-keys", "send-keys-settle", "send-keys-literal", "enter", "SendTrustPromptAccept":
			out = append(out, c)
		}
	}
	return out
}

func blockResultText(res *ToolCallResult) string {
	var sb strings.Builder
	for _, c := range res.Content {
		if t, ok := c["text"].(string); ok {
			sb.WriteString(t)
		}
	}
	return sb.String()
}

// sendingTools are the MCP tools that type their caller's text into a named
// session.
var sendingTools = map[string]map[string]any{
	"send_input":         {"session_id": "testhost-ab12", "text": "1"},
	"send_saved_command": {"session_id": "testhost-ab12", "command_name": "block-test-yes"},
}

// CallTool is the path behind POST /api/mcp/call: the channel bridge every
// Claude session uses, federation peers (also through /api/proxy/) and
// docs_apply all arrive on it.
func callBlockTool(t *testing.T, s *Server, tool string, args map[string]any) *ToolCallResult {
	t.Helper()
	res, err := s.CallTool(context.Background(), tool, args)
	if err != nil {
		// Some handlers return a failed daemon call as a Go error.
		return &ToolCallResult{IsError: true, Content: []map[string]any{{"text": err.Error()}}}
	}
	return res
}

func TestMCPSendToolsRefuseWhilePromptWaits(t *testing.T) {
	panes := map[string]string{
		"approval": blockApprovalPane,
		"safety":   blockSafetyPane,
		"yes-no":   "Overwrite config.yaml? [y/N] ",
	}
	for tool, args := range sendingTools {
		for kind, pane := range panes {
			t.Run(tool+"/"+kind, func(t *testing.T) {
				s, f, sink := newBlockServer(t, pane)
				res := callBlockTool(t, s, tool, args)
				if calls := blockTyped(f); len(calls) != 0 {
					t.Fatalf("typed into a waiting prompt: %+v", calls)
				}
				if !res.IsError {
					t.Fatalf("want a refusal, got %q", blockResultText(res))
				}
				msg := blockResultText(res)
				for _, want := range []string{"waiting for the operator", "nothing was typed"} {
					if !strings.Contains(msg, want) {
						t.Errorf("refusal %q does not say %q", msg, want)
					}
				}
				ev := sink.all()
				if len(ev) != 1 || ev[0].Action != session.InputAuditRefused || ev[0].Source != "mcp" || ev[0].SessionFullID != "testhost-ab12" {
					t.Fatalf("audit events = %+v", ev)
				}
			})
		}
	}
}

func TestMCPSendToolsRefuseWhenScreenUnreadable(t *testing.T) {
	for tool, args := range sendingTools {
		t.Run(tool, func(t *testing.T) {
			s, f, sink := newBlockServer(t, blockIdlePane)
			f.FailNextOp = map[string]int{"capture-live-tail": 1}
			res := callBlockTool(t, s, tool, args)
			if calls := blockTyped(f); len(calls) != 0 {
				t.Fatalf("typed with an unreadable screen: %+v", calls)
			}
			msg := blockResultText(res)
			if !res.IsError || !strings.Contains(msg, "could not be read") || !strings.Contains(msg, "nothing was typed") {
				t.Fatalf("refusal = %v %q", res.IsError, msg)
			}
			if ev := sink.all(); len(ev) != 1 || ev[0].Action != session.InputAuditRefused {
				t.Fatalf("audit events = %+v", ev)
			}
		})
	}
}

func TestMCPSendToolsUnchangedWithoutPrompt(t *testing.T) {
	for tool, args := range sendingTools {
		t.Run(tool, func(t *testing.T) {
			s, f, sink := newBlockServer(t, blockIdlePane)
			res := callBlockTool(t, s, tool, args)
			if res.IsError {
				t.Fatalf("refused without a prompt: %q", blockResultText(res))
			}
			if len(blockTyped(f)) == 0 {
				t.Fatal("nothing typed")
			}
			if ev := sink.all(); len(ev) != 0 {
				t.Fatalf("audit events = %+v", ev)
			}
		})
	}
}

// A new tool cannot go round either block: this package types only with
// SendToolInput, and only loopbackDo sends a request that can write.
func TestMCPPackageCannotBypassTheBlocks(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	direct := regexp.MustCompile(`\.(SendInput|SendRawKeys|SendAutomatedRawKeys)\(|"send-keys"|\b(httpProxy|http\.DefaultClient|c|client)\.(Do|Post|PostForm)\(|\bhttp\.(Post|PostForm)\(`)
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || name == "loopback_guard.go" {
			continue
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for i, l := range strings.Split(string(b), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(l), "//") && direct.MatchString(l) {
				t.Errorf("%s:%d: type with SendToolInput and send daemon requests with loopbackDo: %s", name, i+1, strings.TrimSpace(l))
			}
		}
	}
}

// A child session's reply to its parent is refused the same way.
func TestMCPReplyToParentStaysRefused(t *testing.T) {
	s, f, sink := newBlockServer(t, blockApprovalPane)
	now := time.Now()
	if err := s.manager.SaveSession(&session.Session{
		ID: "cd34", FullID: "testhost-cd34", Hostname: "testhost", ParentID: "testhost-ab12",
		TmuxSession: "cs-cd34", State: session.StateRunning, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	res := callBlockTool(t, s, "reply_to_parent", map[string]any{"session_id": "testhost-cd34", "text": "yes"})
	if calls := blockTyped(f); len(calls) != 0 || !res.IsError {
		t.Fatalf("reply_to_parent: error=%v typed=%+v", res.IsError, calls)
	}
	if msg := blockResultText(res); !strings.Contains(msg, "waiting for the operator") || !strings.Contains(msg, "nothing was typed") {
		t.Errorf("refusal = %q", msg)
	}
	if ev := sink.all(); len(ev) != 1 || ev[0].Action != session.InputAuditRefused || ev[0].Source != "mcp-reply" {
		t.Fatalf("audit events = %+v", ev)
	}
}

// No MCP tool sends to the REST routes that type into a session, however its
// arguments are spelled: ids are joined into loopback paths, so a crafted one
// would otherwise redirect the call there.
func TestMCPLoopbackNeverReachesTypingRoutes(t *testing.T) {
	var mu sync.Mutex
	var hits []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, r.Method+" "+r.URL.Path)
		mu.Unlock()
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()
	port, _ := strconv.Atoi(srv.URL[strings.LastIndex(srv.URL, ":")+1:])

	crafted := []struct {
		tool string
		args map[string]any
	}{
		{"session_rollback", map[string]any{"id": "testhost-ab12/input?"}},
		{"session_rollback", map[string]any{"id": "testhost-ab12/input#"}},
		{"session_rollback", map[string]any{"id": "testhost-ab12%2Finput?"}},
		{"session_rollback", map[string]any{"id": "peer/testhost-ab12/input?"}},
		{"session_summarize", map[string]any{"session_id": "testhost-ab12/input?"}},
		{"session_guardrail_run", map[string]any{"session_id": "testhost-ab12/input?", "name": "x"}},
		{"session_guardrail_approve", map[string]any{"session_id": "testhost-ab12/input?", "guardrail": "x"}},
		{"plugin_disable", map[string]any{"name": "../sessions/testhost-ab12/input?"}},
		{"plugin_disable", map[string]any{"name": "../sessions/send?"}},
		{"plugin_disable", map[string]any{"name": "../test/message?"}},
		{"plugin_disable", map[string]any{"name": "../proxy/peer/api/sessions/send?"}},
	}
	for _, c := range crafted {
		s, f, sink := newBlockServer(t, blockApprovalPane)
		s.SetWebPort(port)
		mu.Lock()
		hits = nil
		mu.Unlock()
		res := callBlockTool(t, s, c.tool, c.args)
		mu.Lock()
		got := append([]string(nil), hits...)
		mu.Unlock()
		if len(got) != 0 {
			t.Errorf("%s %v reached the daemon: %v", c.tool, c.args, got)
			continue
		}
		if msg := blockResultText(res); !strings.Contains(msg, "nothing was typed") {
			t.Errorf("%s %v: result %q does not say nothing was typed", c.tool, c.args, msg)
		}
		if ev := sink.all(); len(ev) != 1 || ev[0].Action != session.InputAuditRefused {
			t.Errorf("%s %v: audit events = %+v", c.tool, c.args, ev)
		}
		if calls := blockTyped(f); len(calls) != 0 {
			t.Errorf("%s %v typed: %+v", c.tool, c.args, calls)
		}
	}

	// Ordinary ids still reach their own routes.
	s, _, _ := newBlockServer(t, blockIdlePane)
	s.SetWebPort(port)
	mu.Lock()
	hits = nil
	mu.Unlock()
	callBlockTool(t, s, "session_rollback", map[string]any{"id": "testhost-ab12"})
	callBlockTool(t, s, "plugin_disable", map[string]any{"name": "input"})
	mu.Lock()
	defer mu.Unlock()
	want := "POST /api/sessions/testhost-ab12/rollback,POST /api/plugins/input/disable"
	if strings.Join(hits, ",") != want {
		t.Fatalf("ordinary calls = %v, want %s", hits, want)
	}
}
