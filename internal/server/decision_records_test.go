// decision_records_test.go — the decision record routes under
// /api/autonomous/decision-records. The real question store, decision record
// store and decision record service stand behind the handler, as in the
// daemon, over temp files.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

const decisionRecordsPathBase = "/api/autonomous/decision-records"

type decisionRecordsRig struct {
	s         *Server
	questions *autonomous.QuestionStore
	records   *autonomous.DecisionRecordStore
	svc       *autonomous.DecisionRecords
	dcfg      *config.AutonomousDecisionsConfig
	qcfg      *config.AutonomousQuestionsConfig
}

func newDecisionRecordsRig(t *testing.T) *decisionRecordsRig {
	t.Helper()
	s, qs := newQuestionsRESTServer(t)
	rs, err := autonomous.NewDecisionRecordStore(filepath.Join(t.TempDir(), "decision_records.jsonl"))
	if err != nil {
		t.Fatalf("NewDecisionRecordStore: %v", err)
	}
	r := &decisionRecordsRig{s: s, questions: qs, records: rs,
		dcfg: &config.AutonomousDecisionsConfig{},
		qcfg: &config.AutonomousQuestionsConfig{Enabled: true},
	}
	r.svc = autonomous.NewDecisionRecords(rs, qs,
		func() config.AutonomousDecisionsConfig { return *r.dcfg },
		func() config.AutonomousQuestionsConfig { return *r.qcfg },
		nil)
	r.svc.ChainQuestionHook()
	s.SetDecisionRecords(r.svc)
	return r
}

// clearRecordBody is a record that passes the clarity check.
func clearRecordBody() map[string]any {
	return map[string]any{
		"label":     "decision",
		"title":     "Which storage should the service use?",
		"situation": "The service keeps its state in memory and loses it on every restart, so the operator has to re-enter the same settings each time it comes back.",
		"deciding":  "where the settings are kept",
		"options": []map[string]any{
			{"id": "sqlite", "label": "SQLite", "tradeoff": "one more dependency, survives restarts"},
			{"id": "files", "label": "Plain files", "tradeoff": "no dependency, easy to corrupt by hand"},
		},
		"recommendation": map[string]any{"option_id": "sqlite", "reason": "it survives restarts and needs no hand editing"},
		"default":        "hold",
		"links":          map[string]any{"automaton": "auto-1"},
	}
}

// drCall sends one request to the handler. body is marshalled unless it is a
// []byte or string, which are sent as they are.
func drCall(t *testing.T, s *Server, method, path string, body any, caps ...string) *httptest.ResponseRecorder {
	t.Helper()
	var rd *bytes.Reader
	switch b := body.(type) {
	case nil:
		rd = bytes.NewReader(nil)
	case []byte:
		rd = bytes.NewReader(b)
	case string:
		rd = bytes.NewReader([]byte(b))
	default:
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rd = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, rd)
	if len(caps) > 0 {
		req = asPeer(req, caps...)
	}
	rec := httptest.NewRecorder()
	s.handleDecisionRecords(rec, req)
	return rec
}

func drJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("response is not a JSON object: %v\n%s", err, rec.Body.String())
	}
	return m
}

// createClearRecord creates a clear record over REST and returns its id and
// question id.
func (r *decisionRecordsRig) createClearRecord(t *testing.T, mutate func(map[string]any)) (id, questionID string) {
	t.Helper()
	body := clearRecordBody()
	if mutate != nil {
		mutate(body)
	}
	rec := drCall(t, r.s, http.MethodPost, decisionRecordsPathBase, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: want 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	m := drJSON(t, rec)
	record := m["record"].(map[string]any)
	return record["id"].(string), m["question_id"].(string)
}

func TestDecisionRecordRoutes_CreateReturns201WithRecordAndQuestion(t *testing.T) {
	r := newDecisionRecordsRig(t)
	rec := drCall(t, r.s, http.MethodPost, decisionRecordsPathBase, clearRecordBody())
	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	m := drJSON(t, rec)
	record, _ := m["record"].(map[string]any)
	id, _ := record["id"].(string)
	if !strings.HasPrefix(id, "dr-") {
		t.Fatalf("record id = %q, want a dr- id", id)
	}
	if record["state"] != "open" || record["source"] != "rest" || record["question_state"] != "open" {
		t.Fatalf("record = %v, want open, source rest, question open", record)
	}
	qid, _ := m["question_id"].(string)
	if qid == "" || record["question_id"] != qid {
		t.Fatalf("question_id = %q, record question_id = %v", qid, record["question_id"])
	}
	if w, ok := m["warnings"].([]any); !ok || len(w) != 0 {
		t.Fatalf("warnings = %#v, want an empty array for a clear record", m["warnings"])
	}
	q, ok := r.questions.Get(qid)
	if !ok || q.DecisionRecordID != id || q.State != autonomous.QuestionOpen {
		t.Fatalf("question %+v, want open and linked to %s", q, id)
	}
	if !strings.Contains(q.Text, "Which storage should the service use?") {
		t.Fatalf("question text %q does not carry the title", q.Text)
	}
}

func TestDecisionRecordRoutes_CreateWarnModeReportsClarityFindings(t *testing.T) {
	r := newDecisionRecordsRig(t) // the default clarity mode is warn
	rec := drCall(t, r.s, http.MethodPost, decisionRecordsPathBase, map[string]any{
		"title": "Pick one", "situation": "Short.", "deciding": "which",
		"options": []map[string]any{{"id": "a", "label": "A"}, {"id": "b", "label": "B"}},
		"default": "hold",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201 in warn mode, got %d body=%s", rec.Code, rec.Body.String())
	}
	warnings, _ := drJSON(t, rec)["warnings"].([]any)
	if len(warnings) == 0 {
		t.Fatalf("want clarity warnings for an unclear record, got none: %s", rec.Body.String())
	}
	for _, w := range warnings {
		f := w.(map[string]any)
		if f["code"] == "" || f["message"] == "" {
			t.Fatalf("warning %v lacks a code or message", f)
		}
	}
}

func TestDecisionRecordRoutes_CreateRefuseModeReturns422AndStoresNothing(t *testing.T) {
	r := newDecisionRecordsRig(t)
	mode := "refuse"
	r.dcfg.ClarityMode = &mode
	rec := drCall(t, r.s, http.MethodPost, decisionRecordsPathBase, map[string]any{
		"title": "Pick one", "situation": "Short.", "deciding": "which",
		"options": []map[string]any{{"id": "a", "label": "A"}, {"id": "b", "label": "B"}},
		"default": "hold",
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d body=%s", rec.Code, rec.Body.String())
	}
	m := drJSON(t, rec)
	if msg, _ := m["error"].(string); msg == "" {
		t.Fatalf("422 body has no error: %s", rec.Body.String())
	}
	if f, ok := m["findings"].([]any); !ok || len(f) == 0 {
		t.Fatalf("422 body has no findings: %s", rec.Body.String())
	}
	if n := len(r.records.List(autonomous.DecisionRecordFilter{})); n != 0 {
		t.Fatalf("%d records stored after a refusal, want 0", n)
	}
	if n := len(r.questions.List(autonomous.QuestionFilter{})); n != 0 {
		t.Fatalf("%d questions raised after a refusal, want 0", n)
	}
}

func TestDecisionRecordRoutes_CreateInvalidRecordIsBadRequestNamingTheProblems(t *testing.T) {
	r := newDecisionRecordsRig(t)
	rec := drCall(t, r.s, http.MethodPost, decisionRecordsPathBase, map[string]any{
		"situation": "Something is happening.", "deciding": "what to do",
		"options": []map[string]any{{"id": "a", "label": "Only one"}},
		"default": "nowhere",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	msg, _ := drJSON(t, rec)["error"].(string)
	for _, want := range []string{"title is required", "at least 2 options", `default "nowhere"`} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not name the problem %q", msg, want)
		}
	}
	if n := len(r.records.List(autonomous.DecisionRecordFilter{})); n != 0 {
		t.Fatalf("%d records stored after an invalid body, want 0", n)
	}
}

func TestDecisionRecordRoutes_CreateRefusesBodiesThatAreNotJustContent(t *testing.T) {
	r := newDecisionRecordsRig(t)
	withField := func(k string, v any) map[string]any {
		b := clearRecordBody()
		b[k] = v
		return b
	}
	cases := []struct {
		name string
		body any
	}{
		{"unknown field", withField("colour", "blue")},
		{"state", withField("state", "answered")},
		{"question id", withField("question_id", "q-123")},
		{"source", withField("source", "file")},
		{"unknown field inside an option", map[string]any{
			"title": "T", "situation": "S", "deciding": "D", "default": "hold",
			"options": []map[string]any{{"id": "a", "label": "A", "cost": 1}, {"id": "b", "label": "B"}},
		}},
		{"not JSON", "title=nothing"},
		{"empty body", ""},
		{"trailing junk", `{"title":"x"} {"title":"y"}`},
	}
	for _, c := range cases {
		rec := drCall(t, r.s, http.MethodPost, decisionRecordsPathBase, c.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d body=%s", c.name, rec.Code, rec.Body.String())
		}
	}
	if n := len(r.records.List(autonomous.DecisionRecordFilter{})); n != 0 {
		t.Fatalf("%d records stored after refused bodies, want 0", n)
	}
}

func TestDecisionRecordRoutes_CreateWithASwitchOffIs503NamingTheSwitch(t *testing.T) {
	r := newDecisionRecordsRig(t)
	off := false
	r.dcfg.Enabled = &off
	rec := drCall(t, r.s, http.MethodPost, decisionRecordsPathBase, clearRecordBody())
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d body=%s", rec.Code, rec.Body.String())
	}
	if msg, _ := drJSON(t, rec)["error"].(string); !strings.Contains(msg, "autonomous.decisions.enabled") {
		t.Fatalf("503 message %q does not name autonomous.decisions.enabled", msg)
	}

	r.dcfg.Enabled = nil
	r.qcfg.Enabled = false
	rec = drCall(t, r.s, http.MethodPost, decisionRecordsPathBase, clearRecordBody())
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d body=%s", rec.Code, rec.Body.String())
	}
	if msg, _ := drJSON(t, rec)["error"].(string); !strings.Contains(msg, "autonomous.questions.enabled") {
		t.Fatalf("503 message %q does not name autonomous.questions.enabled", msg)
	}
	if n := len(r.records.List(autonomous.DecisionRecordFilter{})); n != 0 {
		t.Fatalf("%d records stored while a switch was off, want 0", n)
	}
}

func TestDecisionRecordRoutes_NotWiredIs503OnEveryRoute(t *testing.T) {
	s, _ := newQuestionsRESTServer(t) // no decision record service
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, decisionRecordsPathBase},
		{http.MethodGet, decisionRecordsPathBase},
		{http.MethodGet, decisionRecordsPathBase + "/dr-1"},
		{http.MethodPut, decisionRecordsPathBase + "/dr-1"},
		{http.MethodPost, decisionRecordsPathBase + "/dr-1/withdraw"},
	} {
		rec := drCall(t, s, c.method, c.path, map[string]any{})
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: want 503, got %d", c.method, c.path, rec.Code)
		}
	}
}

func TestDecisionRecordRoutes_ListFiltersAndNewestFirst(t *testing.T) {
	r := newDecisionRecordsRig(t)
	firstID, _ := r.createClearRecord(t, func(b map[string]any) { b["links"] = map[string]any{"automaton": "auto-1"} })
	secondID, secondQ := r.createClearRecord(t, func(b map[string]any) { b["links"] = map[string]any{"automaton": "auto-2"} })
	thirdID, _ := r.createClearRecord(t, func(b map[string]any) { b["links"] = map[string]any{"automaton": "auto-1"} })

	// second is answered, third is withdrawn, first stays open.
	if rec := doAnswer(t, r.s, secondQ, map[string]any{"chosen_option_id": "files", "quote": "plain files"}); rec.Code != http.StatusCreated {
		t.Fatalf("answer: %d %s", rec.Code, rec.Body.String())
	}
	if rec := drCall(t, r.s, http.MethodPost, decisionRecordsPathBase+"/"+thirdID+"/withdraw", nil); rec.Code != http.StatusOK {
		t.Fatalf("withdraw: %d %s", rec.Code, rec.Body.String())
	}

	ids := func(query string) []string {
		t.Helper()
		rec := drCall(t, r.s, http.MethodGet, decisionRecordsPathBase+query, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("list %q: want 200, got %d body=%s", query, rec.Code, rec.Body.String())
		}
		rows, ok := drJSON(t, rec)["decision_records"].([]any)
		if !ok {
			t.Fatalf("list %q: decision_records is not an array: %s", query, rec.Body.String())
		}
		out := []string{}
		for _, row := range rows {
			out = append(out, row.(map[string]any)["id"].(string))
		}
		return out
	}
	eq := func(name string, got []string, want ...string) {
		t.Helper()
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: got %v, want %v", name, got, want)
		}
	}
	eq("all, newest first", ids(""), thirdID, secondID, firstID)
	eq("open", ids("?state=open"), firstID)
	eq("answered", ids("?state=answered"), secondID)
	eq("withdrawn", ids("?state=withdrawn"), thirdID)
	eq("prd_id", ids("?prd_id=auto-1"), thirdID, firstID)
	eq("prd_id and state", ids("?prd_id=auto-1&state=open"), firstID)
	eq("prd_id with no match", ids("?prd_id=auto-none"))
	eq("limit", ids("?limit=2"), thirdID, secondID)
	eq("limit above the maximum is capped, not refused", ids("?limit=5000"), thirdID, secondID, firstID)

	for _, q := range []string{"?state=expired", "?state=all", "?state=bogus", "?limit=0", "?limit=-3", "?limit=abc", "?limit=1.5"} {
		if rec := drCall(t, r.s, http.MethodGet, decisionRecordsPathBase+q, nil); rec.Code != http.StatusBadRequest {
			t.Errorf("list %q: want 400, got %d body=%s", q, rec.Code, rec.Body.String())
		}
	}
}

func TestDecisionRecordRoutes_ListAnsweredViewCarriesTheAnswerVerbatim(t *testing.T) {
	r := newDecisionRecordsRig(t)
	id, qid := r.createClearRecord(t, nil)
	const quote = "  SQLite, été \"quoted\"\n"
	if rec := doAnswer(t, r.s, qid, map[string]any{
		"chosen_option_id": "sqlite", "quote": quote, "actor": "dmz", "surface": "pwa",
	}); rec.Code != http.StatusCreated {
		t.Fatalf("answer: %d %s", rec.Code, rec.Body.String())
	}

	rec := drCall(t, r.s, http.MethodGet, decisionRecordsPathBase, nil)
	rows := drJSON(t, rec)["decision_records"].([]any)
	if len(rows) != 1 {
		t.Fatalf("%d rows, want 1: %s", len(rows), rec.Body.String())
	}
	row := rows[0].(map[string]any)
	if row["id"] != id || row["state"] != "answered" || row["question_state"] != "answered" {
		t.Fatalf("row = %v, want answered record with an answered question", row)
	}
	ans, _ := row["answer"].(map[string]any)
	if ans == nil {
		t.Fatalf("answered row has no answer object: %v", row)
	}
	if ans["quote"] != quote || ans["actor"] != "dmz" || ans["surface"] != "pwa" || ans["chosen_option_id"] != "sqlite" {
		t.Fatalf("answer = %v, want the verbatim quote, actor dmz, surface pwa, option sqlite", ans)
	}
	if at, _ := ans["answered_at"].(string); at == "" {
		t.Fatalf("answer has no answered_at: %v", ans)
	}
	if _, has := row["doc_change"]; has {
		t.Fatalf("doc_change present although none was set: %v", row)
	}
	if _, has := row["follow_up"]; has {
		t.Fatalf("follow_up present although none was set: %v", row)
	}
}

func TestDecisionRecordRoutes_ListCarriesTheFolderStatus(t *testing.T) {
	r := newDecisionRecordsRig(t)

	// No folder: off, empty path, never null lists.
	folder := drJSON(t, drCall(t, r.s, http.MethodGet, decisionRecordsPathBase, nil))["folder"].(map[string]any)
	if folder["enabled"] != false || folder["path"] != "" || folder["files_seen"] != float64(0) {
		t.Fatalf("folder with none configured = %v", folder)
	}
	if errs, ok := folder["errors"].([]any); !ok || len(errs) != 0 {
		t.Fatalf("folder errors = %#v, want an empty array", folder["errors"])
	}
	if v, ok := folder["last_scan"]; !ok || v != nil {
		t.Fatalf("folder last_scan = %#v (present %v), want null before the first scan", v, ok)
	}

	// A folder with one file the daemon cannot read: the problem is reported.
	dir := t.TempDir()
	r.dcfg.Folder = dir
	if err := os.WriteFile(filepath.Join(dir, "broken.md"), []byte("no front matter in this file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.svc.ScanFolder()
	folder = drJSON(t, drCall(t, r.s, http.MethodGet, decisionRecordsPathBase, nil))["folder"].(map[string]any)
	if folder["enabled"] != true || folder["path"] != dir {
		t.Fatalf("folder = %v, want enabled on %s", folder, dir)
	}
	if folder["files_seen"] != float64(1) {
		t.Fatalf("files_seen = %v, want 1", folder["files_seen"])
	}
	errs, _ := folder["errors"].([]any)
	if len(errs) != 1 || errs[0].(map[string]any)["file"] != "broken.md" || errs[0].(map[string]any)["message"] == "" {
		t.Fatalf("folder errors = %v, want one for broken.md with a message", errs)
	}
	if at, _ := folder["last_scan"].(string); at == "" {
		t.Fatalf("folder last_scan = %v, want a time after a scan", folder["last_scan"])
	}
}

func TestDecisionRecordRoutes_GetOneAndUnknownIs404(t *testing.T) {
	r := newDecisionRecordsRig(t)
	id, qid := r.createClearRecord(t, nil)

	rec := drCall(t, r.s, http.MethodGet, decisionRecordsPathBase+"/"+id, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	m := drJSON(t, rec)
	record := m["record"].(map[string]any)
	if record["id"] != id || record["question_id"] != qid || record["question_state"] != "open" || m["question_id"] != qid {
		t.Fatalf("get body = %v", m)
	}
	if record["title"] != "Which storage should the service use?" || record["default"] != "hold" {
		t.Fatalf("record content = %v", record)
	}

	for _, p := range []string{"/dr-unknown", "/dr-unknown/withdraw"} {
		method := http.MethodGet
		if strings.HasSuffix(p, "withdraw") {
			method = http.MethodPost
		}
		if rec := drCall(t, r.s, method, decisionRecordsPathBase+p, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: want 404, got %d body=%s", method, p, rec.Code, rec.Body.String())
		}
	}
	if rec := drCall(t, r.s, http.MethodPut, decisionRecordsPathBase+"/dr-unknown", clearRecordBody()); rec.Code != http.StatusNotFound {
		t.Errorf("put unknown: want 404, got %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := drCall(t, r.s, http.MethodGet, decisionRecordsPathBase+"/"+id+"/other", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown sub path: want 404, got %d", rec.Code)
	}
}

func TestDecisionRecordRoutes_UpdateOpenRecordChangesItsQuestion(t *testing.T) {
	r := newDecisionRecordsRig(t)
	id, qid := r.createClearRecord(t, nil)

	body := clearRecordBody()
	body["title"] = "Where should the service keep its settings?"
	rec := drCall(t, r.s, http.MethodPut, decisionRecordsPathBase+"/"+id, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	m := drJSON(t, rec)
	if m["question_id"] != qid {
		t.Fatalf("question_id = %v, want the same question %s", m["question_id"], qid)
	}
	if got := m["record"].(map[string]any)["title"]; got != "Where should the service keep its settings?" {
		t.Fatalf("record title = %v after the update", got)
	}
	if _, ok := m["warnings"].([]any); !ok {
		t.Fatalf("update body has no warnings array: %s", rec.Body.String())
	}
	q, _ := r.questions.Get(qid)
	if q.State != autonomous.QuestionOpen || !strings.Contains(q.Text, "Where should the service keep its settings?") || strings.Contains(q.Text, "Which storage") {
		t.Fatalf("question after the update = %+v, want the new title in its text and still open", q)
	}
	if n := len(r.questions.List(autonomous.QuestionFilter{})); n != 1 {
		t.Fatalf("%d questions after the update, want the same single one", n)
	}

	// An invalid replacement is a 400 and leaves the question alone.
	bad := clearRecordBody()
	bad["title"] = ""
	if rec := drCall(t, r.s, http.MethodPut, decisionRecordsPathBase+"/"+id, bad); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid update: want 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if q2, _ := r.questions.Get(qid); q2.Text != q.Text {
		t.Fatalf("question text changed by a refused update: %q", q2.Text)
	}
}

func TestDecisionRecordRoutes_UpdateAnsweredOrWithdrawnRecordIs409(t *testing.T) {
	r := newDecisionRecordsRig(t)
	answeredID, answeredQ := r.createClearRecord(t, nil)
	withdrawnID, _ := r.createClearRecord(t, nil)
	if rec := doAnswer(t, r.s, answeredQ, map[string]any{"chosen_option_id": "sqlite"}); rec.Code != http.StatusCreated {
		t.Fatalf("answer: %d %s", rec.Code, rec.Body.String())
	}
	if rec := drCall(t, r.s, http.MethodPost, decisionRecordsPathBase+"/"+withdrawnID+"/withdraw", nil); rec.Code != http.StatusOK {
		t.Fatalf("withdraw: %d %s", rec.Code, rec.Body.String())
	}

	body := clearRecordBody()
	body["title"] = "A different title altogether?"
	for name, id := range map[string]string{"answered": answeredID, "withdrawn": withdrawnID} {
		rec := drCall(t, r.s, http.MethodPut, decisionRecordsPathBase+"/"+id, body)
		if rec.Code != http.StatusConflict {
			t.Fatalf("%s: want 409, got %d body=%s", name, rec.Code, rec.Body.String())
		}
		msg, _ := drJSON(t, rec)["error"].(string)
		if !strings.Contains(msg, name) {
			t.Errorf("%s: 409 message %q does not say the record is %s", name, msg, name)
		}
	}
	got, _ := r.records.Get(answeredID)
	if got.Title != "Which storage should the service use?" {
		t.Fatalf("answered record title changed to %q", got.Title)
	}
}

func TestDecisionRecordRoutes_WithdrawClosesTheRecordAndItsQuestion(t *testing.T) {
	r := newDecisionRecordsRig(t)
	id, qid := r.createClearRecord(t, nil)

	rec := drCall(t, r.s, http.MethodPost, decisionRecordsPathBase+"/"+id+"/withdraw", map[string]any{"reason": "no longer needed"})
	if rec.Code != http.StatusOK {
		t.Fatalf("withdraw: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	record := drJSON(t, rec)["record"].(map[string]any)
	if record["state"] != "withdrawn" || record["question_state"] != "withdrawn" {
		t.Fatalf("record after the withdraw = %v, want withdrawn with a withdrawn question", record)
	}
	q, _ := r.questions.Get(qid)
	if q.State != autonomous.QuestionWithdrawn {
		t.Fatalf("question state %s, want withdrawn", q.State)
	}

	// Withdrawing again changes nothing and still answers 200; an empty body
	// is fine.
	if rec := drCall(t, r.s, http.MethodPost, decisionRecordsPathBase+"/"+id+"/withdraw", ""); rec.Code != http.StatusOK {
		t.Fatalf("second withdraw: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	// An answered record cannot be withdrawn.
	id2, q2 := r.createClearRecord(t, nil)
	if rec := doAnswer(t, r.s, q2, map[string]any{"chosen_option_id": "files"}); rec.Code != http.StatusCreated {
		t.Fatalf("answer: %d %s", rec.Code, rec.Body.String())
	}
	if rec := drCall(t, r.s, http.MethodPost, decisionRecordsPathBase+"/"+id2+"/withdraw", nil); rec.Code != http.StatusConflict {
		t.Fatalf("withdraw answered: want 409, got %d body=%s", rec.Code, rec.Body.String())
	}

	// A body with a field the route does not know is refused.
	id3, _ := r.createClearRecord(t, nil)
	if rec := drCall(t, r.s, http.MethodPost, decisionRecordsPathBase+"/"+id3+"/withdraw", map[string]any{"colour": "red"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("withdraw with an unknown field: want 400, got %d", rec.Code)
	}
}

func TestDecisionRecordRoutes_MethodNotAllowed(t *testing.T) {
	r := newDecisionRecordsRig(t)
	id, _ := r.createClearRecord(t, nil)
	cases := []struct {
		method, path, allow string
	}{
		{http.MethodPut, decisionRecordsPathBase, "GET, POST"},
		{http.MethodDelete, decisionRecordsPathBase, "GET, POST"},
		{http.MethodPost, decisionRecordsPathBase + "/" + id, "GET, PUT"},
		{http.MethodDelete, decisionRecordsPathBase + "/" + id, "GET, PUT"},
		{http.MethodGet, decisionRecordsPathBase + "/" + id + "/withdraw", "POST"},
		{http.MethodPut, decisionRecordsPathBase + "/" + id + "/withdraw", "POST"},
	}
	for _, c := range cases {
		rec := drCall(t, r.s, c.method, c.path, clearRecordBody())
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: want 405, got %d", c.method, c.path, rec.Code)
			continue
		}
		if got := rec.Header().Get("Allow"); got != c.allow {
			t.Errorf("%s %s: Allow = %q, want %q", c.method, c.path, got, c.allow)
		}
	}
}

func TestDecisionRecordRoutes_BodyOverTheCapIs413(t *testing.T) {
	r := newDecisionRecordsRig(t)
	id, _ := r.createClearRecord(t, nil)
	big := clearRecordBody()
	big["situation"] = strings.Repeat("a long situation ", 6000) // ~100 KiB

	for _, c := range []struct{ method, path string }{
		{http.MethodPost, decisionRecordsPathBase},
		{http.MethodPut, decisionRecordsPathBase + "/" + id},
		{http.MethodPost, decisionRecordsPathBase + "/" + id + "/withdraw"},
	} {
		body := any(big)
		if strings.HasSuffix(c.path, "withdraw") {
			body = map[string]any{"reason": strings.Repeat("r", 70*1024)}
		}
		rec := drCall(t, r.s, c.method, c.path, body)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("%s %s: want 413, got %d body=%.120s", c.method, c.path, rec.Code, rec.Body.String())
		}
	}
	if n := len(r.records.List(autonomous.DecisionRecordFilter{})); n != 1 {
		t.Fatalf("%d records after oversized bodies, want only the first", n)
	}
}

func TestDecisionRecordRoutes_FederationReadAndWriteCapabilities(t *testing.T) {
	r := newDecisionRecordsRig(t)
	id, _ := r.createClearRecord(t, nil)

	// A peer with only the read capability reads but cannot change anything.
	read := []string{"autonomous:read"}
	if rec := drCall(t, r.s, http.MethodGet, decisionRecordsPathBase, nil, read...); rec.Code != http.StatusOK {
		t.Errorf("list with read: want 200, got %d", rec.Code)
	}
	if rec := drCall(t, r.s, http.MethodGet, decisionRecordsPathBase+"/"+id, nil, read...); rec.Code != http.StatusOK {
		t.Errorf("get with read: want 200, got %d", rec.Code)
	}
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, decisionRecordsPathBase},
		{http.MethodPut, decisionRecordsPathBase + "/" + id},
		{http.MethodPost, decisionRecordsPathBase + "/" + id + "/withdraw"},
	} {
		if rec := drCall(t, r.s, c.method, c.path, clearRecordBody(), read...); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s with read only: want 403, got %d", c.method, c.path, rec.Code)
		}
	}

	// A peer with only the write capability changes but cannot read.
	write := []string{"autonomous:write"}
	if rec := drCall(t, r.s, http.MethodGet, decisionRecordsPathBase, nil, write...); rec.Code != http.StatusForbidden {
		t.Errorf("list with write only: want 403, got %d", rec.Code)
	}
	if rec := drCall(t, r.s, http.MethodPost, decisionRecordsPathBase, clearRecordBody(), write...); rec.Code != http.StatusCreated {
		t.Errorf("create with write: want 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := drCall(t, r.s, http.MethodPost, decisionRecordsPathBase+"/"+id+"/withdraw", nil, write...); rec.Code != http.StatusOK {
		t.Errorf("withdraw with write: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDecisionRecordRoutes_RecordIDOnTheDecisionLine(t *testing.T) {
	r := newDecisionRecordsRig(t)
	id, qid := r.createClearRecord(t, nil)
	plain := raiseQ(t, r.s) // a question with no record

	r.s.autonomousMgr = &decisionsSpy{}
	rec := decisionsGET(t, r.s, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("decision line: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	byID := map[string]map[string]any{}
	for _, e := range decisionEntries(t, rec) {
		byID[e["id"].(string)] = e
	}
	linked, ok := byID[qid]
	if !ok {
		t.Fatalf("decision line lacks the record's question %s: %s", qid, rec.Body.String())
	}
	if linked["record_id"] != id {
		t.Fatalf("record_id of the record's decision = %v, want %s", linked["record_id"], id)
	}
	other, ok := byID[plain.ID]
	if !ok {
		t.Fatalf("decision line lacks the plain question %s", plain.ID)
	}
	if v, present := other["record_id"]; !present || v != "" {
		t.Fatalf("record_id of a decision with no record = %#v (present %v), want an empty string", v, present)
	}
}
