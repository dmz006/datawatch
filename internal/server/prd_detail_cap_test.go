package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

type prdDetailFake struct {
	fakeOrchAutonomous
	prd *autonomous.PRD
}

func (f *prdDetailFake) GetPRD(id string) (any, bool) {
	if f.prd == nil || f.prd.ID != id {
		return nil, false
	}
	return f.prd, true
}

func (f *prdDetailFake) ListPRDs() []any { return []any{f.prd} }

const capTestSize = 100 << 10

// capTestPRD builds an Automaton with distinct oversized text in every heavy
// field, including multi-byte runes so the cut must respect rune boundaries.
func capTestPRD() *autonomous.PRD {
	big := func(tag string) string {
		return tag + ":" + strings.Repeat("é-payload-", capTestSize/10)
	}
	return &autonomous.PRD{
		ID: "cap00001", Title: "cap", Spec: big("prdspec"), MemoryReport: big("memrep"),
		Story: []autonomous.Story{{
			ID: "s1", PRDID: "cap00001", Description: big("storydesc"),
			Tasks: []autonomous.Task{{
				ID: "t1", StoryID: "s1", PRDID: "cap00001", Status: autonomous.TaskCompleted,
				Spec: big("spec"), Error: big("error"), PriorAttemptHint: big("hint"),
				StartFailureTail: []string{big("tail0"), "short"},
				AttemptRefs:      []string{big("ref0")},
				Verification:     &autonomous.VerificationResult{OK: true, Summary: "ok", RawJudge: big("judge"), Issues: []string{big("issue")}},
			}, {ID: "t2", StoryID: "s1", PRDID: "cap00001", Spec: "small spec"}},
		}},
	}
}

func prdDetailServer(prd *autonomous.PRD, capBytes int) *Server {
	cfg := config.DefaultConfig()
	cfg.Autonomous.DetailTextCapBytes = capBytes
	return &Server{cfg: cfg, autonomousMgr: &prdDetailFake{prd: prd}}
}

func prdDetailGet(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d: %s", path, rr.Code, rr.Body.String())
	}
	return rr
}

func prdDetailDecode(t *testing.T, rr *httptest.ResponseRecorder) *autonomous.PRD {
	t.Helper()
	var p autonomous.PRD
	if err := json.Unmarshal(rr.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return &p
}

func TestPRDDetailCapTruncatesWithMarker(t *testing.T) {
	prd := capTestPRD()
	s := prdDetailServer(prd, 4096)
	rr := prdDetailGet(t, s, "/api/autonomous/prds/cap00001")
	got := prdDetailDecode(t, rr)
	task := got.Story[0].Tasks[0]

	for name, text := range map[string]string{
		"prd spec": got.Spec, "memory report": got.MemoryReport, "story description": got.Story[0].Description,
		"task spec": task.Spec, "task error": task.Error, "hint": task.PriorAttemptHint,
		"tail": task.StartFailureTail[0], "ref": task.AttemptRefs[0],
		"raw judge": task.Verification.RawJudge, "issue": task.Verification.Issues[0],
	} {
		if !strings.Contains(text, "[truncated: kept ") {
			t.Errorf("%s: no truncation marker", name)
		}
		if len(text) > 4096+80 {
			t.Errorf("%s: %d bytes, want about 4096", name, len(text))
		}
		if !strings.HasPrefix(text, strings.SplitN(text, ":", 2)[0]+":é-payload-") {
			t.Errorf("%s: prefix damaged", name)
		}
	}
	if task.StartFailureTail[1] != "short" || got.Story[0].Tasks[1].Spec != "small spec" {
		t.Error("short fields must be untouched")
	}
	if len(task.Truncated) != 7 {
		t.Fatalf("task truncated notes = %d, want 7: %+v", len(task.Truncated), task.Truncated)
	}
	for _, f := range task.Truncated {
		if f.Ref != "/api/autonomous/prds/cap00001/tasks/t1/outputs" || f.Bytes <= f.Kept || f.Kept <= 0 {
			t.Errorf("bad note %+v", f)
		}
	}
	if len(got.Truncated) != 2 || got.Truncated[0].Ref != "/api/autonomous/prds/cap00001/outputs" {
		t.Errorf("prd notes: %+v", got.Truncated)
	}
	if len(got.Story[0].Tasks[1].Truncated) != 0 {
		t.Error("untruncated task must carry no notes")
	}
	if rr.Body.Len() > 200<<10 {
		t.Errorf("capped reply is %d bytes", rr.Body.Len())
	}
}

func TestPRDDetailCapFullAndOutputsReturnEverythingByteForByte(t *testing.T) {
	prd := capTestPRD()
	want := capTestPRD() // pristine twin
	s := prdDetailServer(prd, 4096)

	full := prdDetailDecode(t, prdDetailGet(t, s, "/api/autonomous/prds/cap00001?full=1"))
	ft := full.Story[0].Tasks[0]
	wt := want.Story[0].Tasks[0]
	if full.Spec != want.Spec || full.MemoryReport != want.MemoryReport || full.Story[0].Description != want.Story[0].Description ||
		ft.Spec != wt.Spec || ft.Error != wt.Error || ft.Verification.RawJudge != wt.Verification.RawJudge ||
		ft.StartFailureTail[0] != wt.StartFailureTail[0] || len(ft.Truncated) != 0 || len(full.Truncated) != 0 {
		t.Fatal("full=1 must return the stored text unchanged and unmarked")
	}

	for _, path := range []string{
		"/api/autonomous/prds/cap00001/outputs?task=t1",
		"/api/autonomous/prds/cap00001/tasks/t1/outputs",
	} {
		var view autonomous.OutputsView
		if err := json.Unmarshal(prdDetailGet(t, s, path).Body.Bytes(), &view); err != nil {
			t.Fatal(err)
		}
		if len(view.Tasks) != 1 {
			t.Fatalf("%s: %d tasks", path, len(view.Tasks))
		}
		o := view.Tasks[0]
		if o.Spec != wt.Spec || o.Error != wt.Error || o.PriorAttemptHint != wt.PriorAttemptHint ||
			o.StartFailureTail[0] != wt.StartFailureTail[0] || o.AttemptRefs[0] != wt.AttemptRefs[0] ||
			o.RawJudge != wt.Verification.RawJudge || o.VerifierIssues[0] != wt.Verification.Issues[0] {
			t.Errorf("%s: outputs differ from stored text", path)
		}
	}

	var all autonomous.OutputsView
	if err := json.Unmarshal(prdDetailGet(t, s, "/api/autonomous/prds/cap00001/outputs").Body.Bytes(), &all); err != nil {
		t.Fatal(err)
	}
	if len(all.Tasks) != 2 {
		t.Errorf("PRD-wide outputs: %d tasks, want 2", len(all.Tasks))
	}

	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/cap00001/tasks/nope/outputs", nil))
	if rr.Code != http.StatusNotFound {
		t.Errorf("unknown task: status %d, want 404", rr.Code)
	}
}

func TestPRDDetailCapLeavesStoredDataUnchanged(t *testing.T) {
	prd := capTestPRD()
	before, _ := json.Marshal(prd)
	s := prdDetailServer(prd, 1024)
	for _, q := range []string{"", "?detail=tasks", "?detail=summary", "?full=1", "/outputs"} {
		prdDetailGet(t, s, "/api/autonomous/prds/cap00001"+q)
	}
	after, _ := json.Marshal(prd)
	if string(before) != string(after) {
		t.Fatal("serving the detail views changed the stored PRD")
	}
	if len(prd.Story[0].Tasks[0].Truncated) != 0 || len(prd.Truncated) != 0 {
		t.Fatal("truncation notes leaked into the stored PRD")
	}
}

func TestPRDDetailCapConfigAndOff(t *testing.T) {
	prd := capTestPRD()
	// A negative value turns the cap off.
	off := prdDetailDecode(t, prdDetailGet(t, prdDetailServer(prd, -1), "/api/autonomous/prds/cap00001"))
	if off.Spec != capTestPRD().Spec || len(off.Truncated) != 0 {
		t.Error("cap off: spec must be returned whole")
	}
	// 0 means the 4 KiB default; the 100 KiB fields are cut near it.
	def := prdDetailDecode(t, prdDetailGet(t, prdDetailServer(prd, 0), "/api/autonomous/prds/cap00001"))
	if n := len(def.Spec); n < autonomous.DefaultDetailTextCap || n > autonomous.DefaultDetailTextCap+80 {
		t.Errorf("default cap: spec is %d bytes", n)
	}
	// detail=tasks reads the same cap for the error text.
	var tv autonomous.TasksView
	if err := json.Unmarshal(prdDetailGet(t, prdDetailServer(prd, 2048), "/api/autonomous/prds/cap00001?detail=tasks").Body.Bytes(), &tv); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tv.Tasks[0].Error, "[truncated: kept ") {
		t.Error("detail=tasks error must be capped")
	}
}

func TestPRDDetailCapListSummaryDoesNotReadHeavyFields(t *testing.T) {
	prd := capTestPRD()
	s := prdDetailServer(prd, 4096)
	rr := prdDetailGet(t, s, "/api/autonomous/prds")
	body := rr.Body.String()
	for _, tag := range []string{"prdspec:", "memrep:", "storydesc:", "spec:é", "error:é", "hint:é", "tail0:", "ref0:", "judge:", "issue:"} {
		if strings.Contains(body, tag) {
			t.Errorf("list summary carries heavy text %q", tag)
		}
	}
	if len(body) > 8<<10 {
		t.Errorf("list summary is %d bytes for a %d-byte Automaton", len(body), capTestSize)
	}
}

func TestPRDDetailCapUTF8Boundary(t *testing.T) {
	prd := &autonomous.PRD{ID: "cap00002", Spec: strings.Repeat("é", 5000)}
	for max := 1000; max < 1004; max++ {
		out := autonomous.CapPRDText(prd, max)
		if !strings.Contains(out.Spec, "[truncated") {
			t.Fatal("expected a cut")
		}
		body := out.Spec[:strings.Index(out.Spec, "\n…[truncated")]
		if len(body) > max || strings.ContainsRune(body, '�') || !json.Valid([]byte(`"`+body+`"`)) {
			t.Errorf("max %d: bad cut of %d bytes", max, len(body))
		}
	}
}

func TestPRDDetailCapEditTaskRejectsTruncatedSpec(t *testing.T) {
	prd := capTestPRD()
	s := prdDetailServer(prd, 4096)
	got := prdDetailDecode(t, prdDetailGet(t, s, "/api/autonomous/prds/cap00001"))
	body, _ := json.Marshal(map[string]string{"task_id": "t1", "new_spec": got.Story[0].Tasks[0].Spec})
	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/cap00001/edit_task", strings.NewReader(string(body))))
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "truncation marker") {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
}
