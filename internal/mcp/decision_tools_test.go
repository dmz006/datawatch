package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/session"
)

const decisionToolQuote = "  go  first, été \"quoted\"\n"

// decisionToolDaemon is a stand-in daemon for the tool to proxy to: it serves
// GET /api/autonomous/decisions from a real question store through the real
// read model, with the route's own query rules, and records the last request.
type decisionToolDaemon struct {
	s                                  *Server
	path, method                       string
	query                              map[string][]string
	answered, withdrawn, expired, open string
}

func newDecisionToolDaemon(t *testing.T) *decisionToolDaemon {
	t.Helper()
	store, err := autonomous.NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	d := &decisionToolDaemon{}
	raise := func(text, prdID string) string {
		got, err := store.Raise(autonomous.Question{
			Text: text, Label: "approval", AskedBy: "supervisor",
			Context: autonomous.QuestionContextPrd, PRDID: prdID, Default: "first",
			Options: []autonomous.QuestionOption{{ID: "first", Label: "First"}, {ID: "second", Label: "Second"}},
		})
		if err != nil {
			t.Fatalf("Raise: %v", err)
		}
		time.Sleep(2 * time.Millisecond)
		return got.ID
	}
	d.answered = raise("Answered one?", "prd-1")
	d.withdrawn = raise("Withdrawn one?", "prd-1")
	d.expired = raise("Expired one?", "prd-2")
	d.open = raise("Open one?", "prd-1")
	if _, err := store.Answer(d.answered, autonomous.Answer{Quote: decisionToolQuote, Actor: "dmz", Surface: "mcp"}); err != nil {
		t.Fatalf("answer: %v", err)
	}
	if _, err := store.Withdraw(d.withdrawn, "dmz", "no longer needed"); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if _, err := store.Expire(d.expired, "timed out"); err != nil {
		t.Fatalf("expire: %v", err)
	}
	prd := &autonomous.PRD{ID: "prd-1", Story: []autonomous.Story{{ID: "s-1", Tasks: []autonomous.Task{
		{ID: "t-1", Spec: "wait for " + d.open, Status: autonomous.TaskWaitingDecision},
		{ID: "t-2", Spec: "needs " + d.open, Status: autonomous.TaskPending},
	}}}}
	lookup := func(id string) (*autonomous.PRD, bool) { return prd, id == prd.ID }

	d.s = capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		d.path, d.method, d.query = r.URL.Path, r.Method, r.URL.Query()
		f := autonomous.QuestionFilter{PRDID: r.URL.Query().Get("prd_id")}
		if v := r.URL.Query().Get("state"); v != "" {
			switch st := autonomous.QuestionState(v); st {
			case autonomous.QuestionOpen, autonomous.QuestionAnswered, autonomous.QuestionWithdrawn, autonomous.QuestionExpired:
				f.State = st
			default:
				http.Error(w, `state must be one of "open", "answered", "withdrawn" or "expired"`, http.StatusBadRequest)
				return
			}
		}
		if v := r.URL.Query().Get("limit"); v != "" {
			f.Limit, _ = strconv.Atoi(v)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"decisions": autonomous.BuildDecisionLine(f, store, lookup)})
	})
	return d
}

func (d *decisionToolDaemon) call(t *testing.T, args map[string]any) (entries []map[string]any, text string, isErr bool) {
	t.Helper()
	res, err := d.s.handleAutonomousDecisions(context.Background(), callReq(args))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	text = automationText(t, res)
	if res.IsError {
		return nil, text, true
	}
	var body struct {
		Decisions []map[string]any `json:"decisions"`
	}
	if err := json.Unmarshal([]byte(text), &body); err != nil || body.Decisions == nil {
		t.Fatalf("decode: %v: %s", err, text)
	}
	return body.Decisions, text, false
}

func decisionToolIDs(entries []map[string]any) []string {
	ids := []string{}
	for _, e := range entries {
		ids = append(ids, e["id"].(string))
	}
	return ids
}

func TestDecisionsToolIsListed(t *testing.T) {
	dir := t.TempDir()
	mgr, err := session.NewManager("testhost", dir, "/bin/echo", 0)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	mgr.WithFakeTmux()
	s := New("testhost", mgr, &config.MCPConfig{Enabled: true}, dir, Options{Version: "test"})
	tools, err := s.ListTools()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tl := range tools {
		found = found || tl.Name == "autonomous_decisions"
	}
	if !found {
		t.Error("tool \"autonomous_decisions\" is not registered")
	}
}

func TestDecisionsMCP(t *testing.T) {
	d := newDecisionToolDaemon(t)

	t.Run("schema and capability", func(t *testing.T) {
		tool := (&Server{}).toolAutonomousDecisions()
		if tool.Name != "autonomous_decisions" {
			t.Fatalf("name = %q", tool.Name)
		}
		if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
			t.Error("autonomous_decisions must be marked read-only")
		}
		if len(tool.InputSchema.Required) != 0 {
			t.Errorf("all parameters are optional, got required %v", tool.InputSchema.Required)
		}
		for _, name := range []string{"prd_id", "state", "limit"} {
			if _, ok := tool.InputSchema.Properties[name]; !ok {
				t.Errorf("lacks the %s parameter", name)
			}
		}
		if got := federation.MCPToolCap["autonomous_decisions"]; got != federation.CapAutonomousRead {
			t.Errorf("autonomous_decisions = %q, want %q", got, federation.CapAutonomousRead)
		}
	})

	t.Run("list proxies GET and returns newest first", func(t *testing.T) {
		got, _, isErr := d.call(t, nil)
		if isErr {
			t.Fatal("tool error")
		}
		if d.method != http.MethodGet || d.path != "/api/autonomous/decisions" {
			t.Errorf("proxied %s %s", d.method, d.path)
		}
		if len(d.query) != 0 {
			t.Errorf("query sent without any argument: %v", d.query)
		}
		if want := []string{d.open, d.expired, d.withdrawn, d.answered}; !reflect.DeepEqual(decisionToolIDs(got), want) {
			t.Errorf("ids = %v; want %v", decisionToolIDs(got), want)
		}
	})

	t.Run("filters pass through", func(t *testing.T) {
		got, _, _ := d.call(t, map[string]any{"prd_id": "prd-1", "state": "open", "limit": 5})
		for key, want := range map[string]string{"prd_id": "prd-1", "state": "open", "limit": "5"} {
			if q := d.query[key]; len(q) != 1 || q[0] != want {
				t.Errorf("query %s = %v, want [%s]", key, q, want)
			}
		}
		if len(got) != 1 || got[0]["id"] != d.open {
			t.Errorf("entries = %v; want only the open decision", got)
		}
		for state, want := range map[string]string{"answered": d.answered, "withdrawn": d.withdrawn, "expired": d.expired} {
			got, _, _ := d.call(t, map[string]any{"state": state})
			if len(got) != 1 || got[0]["id"] != want {
				t.Errorf("state=%s entries = %v; want only %s", state, got, want)
			}
		}
		got, _, _ = d.call(t, map[string]any{"limit": 2})
		if len(got) != 2 {
			t.Errorf("limit=2 entries = %d", len(got))
		}
	})

	t.Run("bad state comes back as a tool error naming the four states", func(t *testing.T) {
		_, text, isErr := d.call(t, map[string]any{"state": "pending"})
		if !isErr {
			t.Fatal("a 400 from the daemon must come back as a tool error")
		}
		for _, w := range []string{"open", "answered", "withdrawn", "expired"} {
			if !strings.Contains(text, w) {
				t.Errorf("refusal does not name %q: %s", w, text)
			}
		}
	})

	t.Run("empty list is an empty array", func(t *testing.T) {
		got, text, _ := d.call(t, map[string]any{"prd_id": "prd-none"})
		if len(got) != 0 || !strings.Contains(text, `"decisions":[]`) {
			t.Errorf("entries = %v, text = %s; want an empty decisions array", got, text)
		}
	})

	t.Run("waiting task is listed", func(t *testing.T) {
		got, _, _ := d.call(t, map[string]any{"state": "open"})
		if len(got) != 1 {
			t.Fatalf("entries = %v", got)
		}
		if want := []any{"t-1", "t-2"}; !reflect.DeepEqual(got[0]["task_ids"], want) {
			t.Errorf("task_ids = %v; want %v", got[0]["task_ids"], want)
		}
		if want := []any{"t-1"}; !reflect.DeepEqual(got[0]["waiting_task_ids"], want) {
			t.Errorf("waiting_task_ids = %v; want %v", got[0]["waiting_task_ids"], want)
		}
	})

	t.Run("answer quote verbatim with its sha256", func(t *testing.T) {
		got, _, _ := d.call(t, map[string]any{"state": "answered"})
		if len(got) != 1 {
			t.Fatalf("entries = %v", got)
		}
		a, ok := got[0]["answer"].(map[string]any)
		if !ok {
			t.Fatalf("answer = %v; want an object", got[0]["answer"])
		}
		if a["quote"] != decisionToolQuote {
			t.Errorf("quote = %q; want it verbatim %q", a["quote"], decisionToolQuote)
		}
		sum := sha256.Sum256([]byte(decisionToolQuote))
		if a["quote_sha256"] != hex.EncodeToString(sum[:]) {
			t.Errorf("quote_sha256 = %v; want %s", a["quote_sha256"], hex.EncodeToString(sum[:]))
		}
		if a["actor"] != "dmz" || a["surface"] != "mcp" {
			t.Errorf("answer = %v", a)
		}
	})
}
