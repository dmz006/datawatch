package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// fakeDefectBackend is an in-memory DefectQueueBackend.
type fakeDefectBackend struct {
	open       []autonomous.OpenBatch
	views      []DefectBatchView
	created    []string
	added      []string
	tiny       []string
	splitErr   error
	splitCalls [][2]string
}

func (f *fakeDefectBackend) ListOpenBatches(string) ([]autonomous.OpenBatch, error) {
	return f.open, nil
}
func (f *fakeDefectBackend) CreateBatch(d autonomous.QueueDefect) (string, error) {
	f.created = append(f.created, d.ID)
	return "batch-" + d.ID, nil
}
func (f *fakeDefectBackend) AddToBatch(id string, d autonomous.QueueDefect) error {
	f.added = append(f.added, id+":"+d.ID)
	return nil
}
func (f *fakeDefectBackend) CreateTinyFix(d autonomous.QueueDefect, _ autonomous.TinyFixChange) (string, error) {
	f.tiny = append(f.tiny, d.ID)
	return "auto-" + d.ID, nil
}
func (f *fakeDefectBackend) OpenBatchViews(project string) ([]DefectBatchView, error) {
	var out []DefectBatchView
	for _, v := range f.views {
		if project == "" || v.Project == project {
			out = append(out, v)
		}
	}
	return out, nil
}
func (f *fakeDefectBackend) SplitStory(batchID, storyID string) (string, error) {
	f.splitCalls = append(f.splitCalls, [2]string{batchID, storyID})
	if f.splitErr != nil {
		return "", f.splitErr
	}
	return "auto-split-" + storyID, nil
}

func newDefectServer(b *fakeDefectBackend) *Server {
	s := &Server{cfg: config.DefaultConfig()}
	if b != nil {
		s.SetDefectQueue(b, nil)
	}
	return s
}

func defectPost(t *testing.T, h http.HandlerFunc, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	return rec
}

func TestDefectIntake_NewBatchThenOpenBatch(t *testing.T) {
	b := &fakeDefectBackend{}
	s := newDefectServer(b)
	rec := defectPost(t, s.handleDefectIntake, "/api/autonomous/defects/intake",
		`{"id":"d1","title":"crash","project":"p","area":"core","files":["internal/x.go"],"kind":"rule_tweak"}`)
	if rec.Code != 200 {
		t.Fatalf("code %d: %s", rec.Code, rec.Body)
	}
	var dec autonomous.IntakeDecision
	_ = json.Unmarshal(rec.Body.Bytes(), &dec)
	if dec.Route != autonomous.IntakeRouteNewBatch || dec.BatchID != "batch-d1" || dec.Reason == "" {
		t.Errorf("decision = %+v", dec)
	}
	b.open = []autonomous.OpenBatch{{ID: "batch-d1", Project: "p", Area: "core", Status: autonomous.BatchStatusDraft, Size: 1}}
	rec = defectPost(t, s.handleDefectIntake, "/api/autonomous/defects/intake",
		`{"id":"d2","title":"other","project":"p","area":"core","files":["internal/y.go"],"kind":"rule_tweak"}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &dec)
	if dec.Route != autonomous.IntakeRouteOpenBatch || len(b.added) != 1 || b.added[0] != "batch-d1:d2" {
		t.Errorf("decision = %+v added=%v", dec, b.added)
	}
}

func TestDefectIntake_TinyFixRoute(t *testing.T) {
	b := &fakeDefectBackend{}
	s := newDefectServer(b)
	rec := defectPost(t, s.handleDefectIntake, "/x",
		`{"id":"d3","title":"typo","project":"p","files":["docs/a.md"],"kind":"docs_only"}`)
	var dec autonomous.IntakeDecision
	_ = json.Unmarshal(rec.Body.Bytes(), &dec)
	if dec.Route != autonomous.IntakeRouteTinyFix || dec.BatchID != "auto-d3" || len(b.tiny) != 1 {
		t.Errorf("decision = %+v tiny=%v", dec, b.tiny)
	}
}

func TestDefectIntake_RefusalReasonSurfaced(t *testing.T) {
	b := &fakeDefectBackend{}
	s := newDefectServer(b)
	// Allowed by kind but the file is operator-visible: the lane refuses.
	rec := defectPost(t, s.handleDefectIntake, "/x",
		`{"id":"d4","title":"ui","project":"p","area":"web","files":["internal/server/web/app.js"],"kind":"lint_tweak"}`)
	var dec autonomous.IntakeDecision
	_ = json.Unmarshal(rec.Body.Bytes(), &dec)
	if dec.Route == autonomous.IntakeRouteTinyFix {
		t.Fatalf("operator-visible change routed to tiny fix: %+v", dec)
	}
	if len(b.tiny) != 0 {
		t.Errorf("tiny fix created: %v", b.tiny)
	}
}

func TestDefectIntake_Validation(t *testing.T) {
	s := newDefectServer(&fakeDefectBackend{})
	for name, body := range map[string]string{
		"bad json":   `{`,
		"no id":      `{"title":"t","project":"p"}`,
		"no title":   `{"id":"d","project":"p"}`,
		"no project": `{"id":"d","title":"t"}`,
	} {
		if rec := defectPost(t, s.handleDefectIntake, "/x", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: code %d, want 400", name, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	s.handleDefectIntake(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET code %d", rec.Code)
	}
}

func TestDefectIntake_UnavailableAndDisabled(t *testing.T) {
	body := `{"id":"d","title":"t","project":"p"}`
	if rec := defectPost(t, newDefectServer(nil).handleDefectIntake, "/x", body); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no backend: code %d", rec.Code)
	}
	s := newDefectServer(&fakeDefectBackend{})
	off := false
	s.cfg.Autonomous.DefectQueue.Enabled = &off
	if rec := defectPost(t, s.handleDefectIntake, "/x", body); rec.Code != http.StatusConflict {
		t.Errorf("disabled: code %d", rec.Code)
	}
}

func TestDefectBatches_ListAndFilter(t *testing.T) {
	b := &fakeDefectBackend{views: []DefectBatchView{
		{ID: "b1", Project: "p", Status: "draft", Size: 1, Stories: []DefectStoryRef{{StoryID: "story-d1", DefectID: "d1"}}},
		{ID: "b2", Project: "q", Status: "draft"},
	}}
	s := newDefectServer(b)
	rec := httptest.NewRecorder()
	s.handleDefectBatches(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/defects/batches?project=p", nil))
	var out struct {
		Batches []DefectBatchView `json:"batches"`
		Total   int               `json:"total"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != 200 || out.Total != 1 || out.Batches[0].Stories[0].DefectID != "d1" {
		t.Errorf("code %d body %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	s.handleDefectBatches(rec, httptest.NewRequest(http.MethodGet, "/x?project=none", nil))
	if !strings.Contains(rec.Body.String(), `"batches":[]`) {
		t.Errorf("empty list not []: %s", rec.Body)
	}
	rec = httptest.NewRecorder()
	newDefectServer(nil).handleDefectBatches(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no backend: %d", rec.Code)
	}
}

func TestDefectBatchSplit_Success(t *testing.T) {
	b := &fakeDefectBackend{}
	s := newDefectServer(b)
	rec := defectPost(t, s.handleDefectBatchSplit, "/api/autonomous/defects/batches/b1/split", `{"story_id":"story-d2"}`)
	if rec.Code != 200 {
		t.Fatalf("code %d: %s", rec.Code, rec.Body)
	}
	var out map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out["automaton_id"] != "auto-split-story-d2" || b.splitCalls[0] != [2]string{"b1", "story-d2"} {
		t.Errorf("out=%v calls=%v", out, b.splitCalls)
	}
}

func TestDefectBatchSplit_Errors(t *testing.T) {
	b := &fakeDefectBackend{splitErr: errors.Join(ErrDefectNotFound, errors.New("story nope"))}
	s := newDefectServer(b)
	if rec := defectPost(t, s.handleDefectBatchSplit, "/api/autonomous/defects/batches/b1/split", `{"story_id":"nope"}`); rec.Code != http.StatusNotFound {
		t.Errorf("unknown story: %d", rec.Code)
	}
	if rec := defectPost(t, s.handleDefectBatchSplit, "/api/autonomous/defects/batches/b1/split", `{}`); rec.Code != http.StatusBadRequest {
		t.Errorf("no story_id: %d", rec.Code)
	}
	if rec := defectPost(t, s.handleDefectBatchSplit, "/api/autonomous/defects/batches/b1", `{"story_id":"s"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad path: %d", rec.Code)
	}
	b.splitErr = errors.New("boom")
	if rec := defectPost(t, s.handleDefectBatchSplit, "/api/autonomous/defects/batches/b1/split", `{"story_id":"s"}`); rec.Code != http.StatusInternalServerError {
		t.Errorf("backend error: %d", rec.Code)
	}
	if rec := defectPost(t, newDefectServer(nil).handleDefectBatchSplit, "/api/autonomous/defects/batches/b1/split", `{"story_id":"s"}`); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no backend: %d", rec.Code)
	}
}

func TestDefectTinyFixCheck(t *testing.T) {
	s := newDefectServer(nil) // the check needs no backend
	type res struct {
		Eligible bool     `json:"eligible"`
		Reason   string   `json:"reason"`
		Refused  []string `json:"refused_paths"`
	}
	check := func(body string) (int, res) {
		rec := defectPost(t, s.handleDefectTinyFixCheck, "/x", body)
		var r res
		_ = json.Unmarshal(rec.Body.Bytes(), &r)
		return rec.Code, r
	}
	if code, r := check(`{"files":["docs/a.md"],"kind":"docs_only"}`); code != 200 || !r.Eligible || r.Reason == "" {
		t.Errorf("eligible case: %d %+v", code, r)
	}
	if code, r := check(`{"files":["cmd/datawatch/main.go"],"kind":"docs_only"}`); code != 200 || r.Eligible || !strings.Contains(r.Reason, "cmd/datawatch/main.go") {
		t.Errorf("refusal reason not surfaced: %d %+v", code, r)
	}
	if code, r := check(`{"files":["a.md","b.md","c.md","d.md"],"kind":"docs_only"}`); code != 200 || r.Eligible {
		t.Errorf("too many files: %d %+v", code, r)
	}
	for name, body := range map[string]string{
		"no files": `{"kind":"docs_only"}`,
		"no kind":  `{"files":["a.md"]}`,
		"bad json": `{`,
	} {
		if code, _ := check(body); code != http.StatusBadRequest {
			t.Errorf("%s: code %d", name, code)
		}
	}
}
