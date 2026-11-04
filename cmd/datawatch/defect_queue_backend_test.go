package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/server"
)

// newWiredDefectQueue builds the REST server and wires the defect queue the
// way main.go does (wireDefectQueue), over a real autonomous manager.
func newWiredDefectQueue(t *testing.T) (http.Handler, *autonomouspkg.Manager) {
	t.Helper()
	dir := t.TempDir()
	amgr, err := autonomouspkg.NewManager(dir, autonomouspkg.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.DataDir = dir
	hs := server.New(&cfg.Server, cfg, "", dir, nil, "test-host", nil)
	wireDefectQueue(hs, amgr, cfg)
	return hs.Handler(), amgr
}

func defectDo(t *testing.T, h http.Handler, method, path, body string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// The endpoints answered 503 "defect queue unavailable" in a running daemon
// because nothing installed a backend. Wired as main.go wires it, they work.
func TestDefectQueueWiredNotUnavailable(t *testing.T) {
	h, _ := newWiredDefectQueue(t)

	code, _ := defectDo(t, h, http.MethodGet, "/api/autonomous/defects/batches", "")
	if code != http.StatusOK {
		t.Fatalf("GET batches = %d, want 200 (503 means the queue is not wired)", code)
	}
	code, body := defectDo(t, h, http.MethodPost, "/api/autonomous/defects/intake",
		`{"id":"d1","title":"stall on reconnect","project":"proj","area":"web","files":["internal/server/api.go"],"kind":"rule_tweak","priority":2}`)
	if code != http.StatusOK || body["route"] != autonomouspkg.IntakeRouteNewBatch {
		t.Fatalf("intake = %d %v, want 200 new_batch", code, body)
	}
}

func TestDefectQueueBatchLifecycle(t *testing.T) {
	h, amgr := newWiredDefectQueue(t)
	intake := func(id string) map[string]any {
		code, body := defectDo(t, h, http.MethodPost, "/api/autonomous/defects/intake",
			`{"id":"`+id+`","title":"title `+id+`","project":"proj","area":"web","files":["internal/server/api.go"],"kind":"rule_tweak","priority":3}`)
		if code != http.StatusOK {
			t.Fatalf("intake %s = %d %v", id, code, body)
		}
		return body
	}
	first := intake("d1")
	second := intake("d2")
	if second["route"] != autonomouspkg.IntakeRouteOpenBatch || second["batch_id"] != first["batch_id"] {
		t.Fatalf("second intake should join the open batch: first %v second %v", first, second)
	}
	batchID := first["batch_id"].(string)

	prd, ok := amgr.Store().GetPRD(batchID)
	if !ok || len(prd.Story) != 3 { // two defect stories + the shared story
		t.Fatalf("batch automaton stories = %v", prd)
	}
	gate := ""
	for _, st := range prd.Story {
		for _, tk := range st.Tasks {
			if tk.Kind == autonomouspkg.DefectTaskKindGate {
				gate = tk.ID
				if len(tk.DependsOn) != 4 { // fix + test of each defect
					t.Fatalf("gate depends_on = %v, want the 4 core tasks", tk.DependsOn)
				}
			}
		}
	}
	if gate == "" {
		t.Fatal("no shared gate task")
	}

	_, list := defectDo(t, h, http.MethodGet, "/api/autonomous/defects/batches?project=proj", "")
	if list["total"].(float64) != 1 {
		t.Fatalf("batches = %v", list)
	}
	stories := list["batches"].([]any)[0].(map[string]any)["stories"].([]any)
	if len(stories) != 2 {
		t.Fatalf("batch view stories = %v", stories)
	}
	storyID := stories[0].(map[string]any)["story_id"].(string)

	code, split := defectDo(t, h, http.MethodPost, "/api/autonomous/defects/batches/"+batchID+"/split", `{"story_id":"`+storyID+`"}`)
	if code != http.StatusOK || split["automaton_id"] == "" {
		t.Fatalf("split = %d %v", code, split)
	}
	newPRD, ok := amgr.Store().GetPRD(split["automaton_id"].(string))
	if !ok || len(newPRD.Story) != 2 { // the moved story + its shared story
		t.Fatalf("split automaton = %+v", newPRD)
	}
	orig, _ := amgr.Store().GetPRD(batchID)
	if len(orig.Story) != 2 {
		t.Fatalf("original batch keeps %d stories, want 2", len(orig.Story))
	}
	for _, st := range orig.Story {
		for _, tk := range st.Tasks {
			for _, dep := range tk.DependsOn {
				if _, found := amgr.Store().GetTask(dep); !found {
					t.Fatalf("task %s depends on missing task %s", tk.ID, dep)
				}
			}
		}
	}
	code, _ = defectDo(t, h, http.MethodPost, "/api/autonomous/defects/batches/nope/split", `{"story_id":"x"}`)
	if code != http.StatusNotFound {
		t.Fatalf("split of unknown batch = %d, want 404", code)
	}
}

func TestDefectQueueTinyFixLane(t *testing.T) {
	h, amgr := newWiredDefectQueue(t)
	code, body := defectDo(t, h, http.MethodPost, "/api/autonomous/defects/intake",
		`{"id":"t1","title":"flaky assertion","project":"proj","files":["internal/x/x_test.go"],"kind":"test_only"}`)
	if code != http.StatusOK || body["route"] != autonomouspkg.IntakeRouteTinyFix {
		t.Fatalf("intake = %d %v, want tiny_fix", code, body)
	}
	prd, ok := amgr.Store().GetPRD(body["batch_id"].(string))
	if !ok || len(prd.Story) != 1 || len(prd.Story[0].Tasks) != 1 || prd.Story[0].Tasks[0].VerificationScript == "" {
		t.Fatalf("tiny-fix automaton = %+v", prd)
	}
	// tiny fixes are not batches
	_, list := defectDo(t, h, http.MethodGet, "/api/autonomous/defects/batches", "")
	if list["total"].(float64) != 0 {
		t.Fatalf("tiny fix listed as a batch: %v", list)
	}
}
