package mcp

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type defectCall struct{ method, path, query, body string }

// defectProxy records each request and answers with the given status/body.
func defectProxy(calls *[]defectCall, status int, reply string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*calls = append(*calls, defectCall{r.Method, r.URL.Path, r.URL.RawQuery, string(b)})
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}
}

func TestDefectMCP_ToolNames(t *testing.T) {
	s := &Server{}
	got := []string{s.toolAutonomousDefectIntake().Name, s.toolAutonomousDefectBatches().Name,
		s.toolAutonomousDefectBatchSplit().Name, s.toolAutonomousDefectTinyFixCheck().Name}
	want := []string{"autonomous_defect_intake", "autonomous_defect_batches", "autonomous_defect_batch_split", "autonomous_defect_tinyfix_check"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tool %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDefectMCP_IntakeProxiesPOST(t *testing.T) {
	var calls []defectCall
	s := capacityTestServer(t, defectProxy(&calls, 200, `{"route":"new_batch","reason":"r","batch_id":"batch-d1"}`))
	res, err := s.handleAutonomousDefectIntake(context.Background(), callReq(map[string]any{
		"id": "d1", "title": "crash", "project": "p", "kind": "docs_only", "files": []any{"docs/a.md"}, "priority": float64(2),
	}))
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	c := calls[0]
	if c.method != http.MethodPost || c.path != "/api/autonomous/defects/intake" {
		t.Errorf("call = %+v", c)
	}
	for _, w := range []string{`"id":"d1"`, `"files":["docs/a.md"]`, `"priority":2`, `"project":"p"`} {
		if !strings.Contains(c.body, w) {
			t.Errorf("body %s missing %s", c.body, w)
		}
	}
	if !strings.Contains(resContentText(t, res), "batch-d1") {
		t.Errorf("result: %s", resContentText(t, res))
	}
}

func TestDefectMCP_IntakeValidationNoProxy(t *testing.T) {
	var calls []defectCall
	s := capacityTestServer(t, defectProxy(&calls, 200, `{}`))
	for _, args := range []map[string]any{
		{"title": "t", "project": "p"},
		{"id": "d", "project": "p"},
		{"id": "d", "title": "t"},
	} {
		res, _ := s.handleAutonomousDefectIntake(context.Background(), callReq(args))
		if !res.IsError {
			t.Errorf("args %v: want tool error", args)
		}
	}
	if len(calls) != 0 {
		t.Errorf("validation failures reached the daemon: %+v", calls)
	}
}

func TestDefectMCP_IntakeRefusalSurfaced(t *testing.T) {
	var calls []defectCall
	reply := `{"route":"new_batch","reason":"tiny-fix refused","tiny_fix_refusal":"not a tiny fix: operator-visible path(s) changed: cmd/x.go"}`
	s := capacityTestServer(t, defectProxy(&calls, 200, reply))
	res, _ := s.handleAutonomousDefectIntake(context.Background(), callReq(map[string]any{
		"id": "d", "title": "t", "project": "p", "kind": "docs_only", "files": []any{"cmd/x.go"},
	}))
	if res.IsError || !strings.Contains(resContentText(t, res), "operator-visible path(s) changed: cmd/x.go") {
		t.Errorf("refusal not surfaced: %s", resContentText(t, res))
	}
}

func TestDefectMCP_IntakeRESTErrorBecomesToolError(t *testing.T) {
	var calls []defectCall
	s := capacityTestServer(t, defectProxy(&calls, 409, "defect queue is disabled for project p"))
	res, _ := s.handleAutonomousDefectIntake(context.Background(), callReq(map[string]any{"id": "d", "title": "t", "project": "p"}))
	if !res.IsError || !strings.Contains(resContentText(t, res), "disabled") {
		t.Errorf("res=%+v", res)
	}
}

func TestDefectMCP_BatchesProxiesGETWithProject(t *testing.T) {
	var calls []defectCall
	s := capacityTestServer(t, defectProxy(&calls, 200, `{"batches":[],"total":0}`))
	if _, err := s.handleAutonomousDefectBatches(context.Background(), callReq(map[string]any{"project": "p"})); err != nil {
		t.Fatal(err)
	}
	if _, err := s.handleAutonomousDefectBatches(context.Background(), callReq(nil)); err != nil {
		t.Fatal(err)
	}
	if calls[0].method != http.MethodGet || calls[0].path != "/api/autonomous/defects/batches" || calls[0].query != "project=p" || calls[1].query != "" {
		t.Errorf("calls = %+v", calls)
	}
}

func TestDefectMCP_SplitOut(t *testing.T) {
	var calls []defectCall
	s := capacityTestServer(t, defectProxy(&calls, 200, `{"automaton_id":"auto-1","batch_id":"b1","story_id":"story-d2"}`))
	res, _ := s.handleAutonomousDefectBatchSplit(context.Background(), callReq(map[string]any{"batch_id": "b1", "story_id": "story-d2"}))
	if res.IsError || !strings.Contains(resContentText(t, res), "auto-1") {
		t.Fatalf("res: %s", resContentText(t, res))
	}
	if calls[0].path != "/api/autonomous/defects/batches/b1/split" || !strings.Contains(calls[0].body, `"story_id":"story-d2"`) {
		t.Errorf("call = %+v", calls[0])
	}
	for _, args := range []map[string]any{{"batch_id": "b1"}, {"story_id": "s"}} {
		if r, _ := s.handleAutonomousDefectBatchSplit(context.Background(), callReq(args)); !r.IsError {
			t.Errorf("args %v: want tool error", args)
		}
	}
	if len(calls) != 1 {
		t.Errorf("invalid calls reached the daemon: %+v", calls)
	}
}

func TestDefectMCP_SplitNotFoundSurfaced(t *testing.T) {
	var calls []defectCall
	s := capacityTestServer(t, defectProxy(&calls, 404, "split failed: not found: story nope"))
	res, _ := s.handleAutonomousDefectBatchSplit(context.Background(), callReq(map[string]any{"batch_id": "b1", "story_id": "nope"}))
	if !res.IsError || !strings.Contains(resContentText(t, res), "404") {
		t.Errorf("res: %s", resContentText(t, res))
	}
}

func TestDefectMCP_TinyFixCheck(t *testing.T) {
	var calls []defectCall
	s := capacityTestServer(t, defectProxy(&calls, 200, `{"eligible":false,"reason":"not a tiny fix: operator-visible path(s) changed: cmd/x.go","refused_paths":["cmd/x.go"]}`))
	res, _ := s.handleAutonomousDefectTinyFixCheck(context.Background(), callReq(map[string]any{
		"files": []any{"cmd/x.go"}, "kind": "docs_only", "project": "p",
	}))
	if res.IsError || !strings.Contains(resContentText(t, res), "cmd/x.go") {
		t.Fatalf("res: %s", resContentText(t, res))
	}
	if calls[0].path != "/api/autonomous/defects/tiny-fix/check" || !strings.Contains(calls[0].body, `"kind":"docs_only"`) {
		t.Errorf("call = %+v", calls[0])
	}
	for _, args := range []map[string]any{{"kind": "docs_only"}, {"files": []any{"a.md"}}} {
		if r, _ := s.handleAutonomousDefectTinyFixCheck(context.Background(), callReq(args)); !r.IsError {
			t.Errorf("args %v: want tool error", args)
		}
	}
	if len(calls) != 1 {
		t.Errorf("invalid calls reached the daemon: %+v", calls)
	}
}
