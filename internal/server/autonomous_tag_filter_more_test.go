// REST-level tests for the object tag filter added to the template list,
// the decision-record list and the held-question list (task filter-the-
// template-decision-record, docs/plans/2026-11-06-automata-pages-redesign.md
// section 12): they drive the real Server handlers with httptest so the
// repeatable ?tag= grammar, the tag_match any/all rule, the project /
// automation / automaton shortcuts and the 400 on a bad tag_match are all
// exercised end to end against each of the three lists, and records that
// name no project are verified absent from ?project= filtered results.

package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// tagMoreFake serves the fixture's templates from ListTemplates and the
// fixture's Automata from ListPRDs; every other method is the
// fakeOrchAutonomous stub.
type tagMoreFake struct {
	*fakeOrchAutonomous
	templates []*autonomous.Template
	prds      []*autonomous.PRD
}

func (f *tagMoreFake) ListTemplates() []any {
	out := make([]any, 0, len(f.templates))
	for _, t := range f.templates {
		out = append(out, t)
	}
	return out
}

func (f *tagMoreFake) ListPRDs() []any {
	out := make([]any, 0, len(f.prds))
	for _, p := range f.prds {
		out = append(out, p)
	}
	return out
}

// --- template list -----------------------------------------------------------

func templateTag(id, title string, systemTags []string) *autonomous.Template {
	return &autonomous.Template{
		ID:         id,
		Title:      title,
		Spec:       "spec " + id,
		SystemTags: systemTags,
	}
}

func doTemplates(t *testing.T, s *Server, path string) (int, string) {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleAutonomousTemplates(rr, httptest.NewRequest(http.MethodGet, path, nil))
	return rr.Code, rr.Body.String()
}

func TestObjectTagFilterTemplateList(t *testing.T) {
	dw := templateTag("tpl-dw", "dw template", []string{"project:datawatch", "automation:nightly", "automaton:dwa"})
	none := templateTag("tpl-noproj", "orphan template", nil)
	s := &Server{cfg: config.DefaultConfig(), autonomousMgr: &tagMoreFake{fakeOrchAutonomous: &fakeOrchAutonomous{}, templates: []*autonomous.Template{dw, none}}}

	// No filter returns both, and the wire shape carries system_tags.
	code, body := doTemplates(t, s, "/api/autonomous/templates")
	if code != http.StatusOK || !strings.Contains(body, "tpl-dw") || !strings.Contains(body, "tpl-noproj") {
		t.Fatalf("unfiltered: status %d body %s", code, body)
	}
	if !strings.Contains(body, `system_tags`) {
		t.Fatalf("list must expose system_tags: %s", body)
	}

	// A repeatable comma-separated tag narrows to the matching template.
	code, body = doTemplates(t, s, "/api/autonomous/templates?tag=project:datawatch")
	if code != http.StatusOK || !strings.Contains(body, "tpl-dw") || strings.Contains(body, "tpl-noproj") {
		t.Fatalf("tag=project:datawatch: status %d body %s", code, body)
	}
	code, body = doTemplates(t, s, "/api/autonomous/templates?tag=project:datawatch,does-not-exist")
	if code != http.StatusOK || !strings.Contains(body, "tpl-dw") || strings.Contains(body, "tpl-noproj") {
		t.Fatalf("tag any-match: status %d body %s", code, body)
	}

	// The automation= and automaton= shortcuts match their axis tags.
	for shortcut, want := range map[string]string{
		"automation=nightly": "tpl-dw",
		"automaton=dwa":      "tpl-dw",
	} {
		code, body = doTemplates(t, s, "/api/autonomous/templates?"+shortcut)
		if code != http.StatusOK || !strings.Contains(body, want) || strings.Contains(body, "tpl-noproj") {
			t.Fatalf("%s: status %d body %s", shortcut, code, body)
		}
	}

	// A record naming no project (no system tags) is absent from a
	// project-filtered result.
	code, body = doTemplates(t, s, "/api/autonomous/templates?project=datawatch")
	if code != http.StatusOK || !strings.Contains(body, "tpl-dw") || strings.Contains(body, "tpl-noproj") {
		t.Fatalf("project=datawatch must drop the no-project template: status %d body %s", code, body)
	}

	// A tag neither template carries keeps both out.
	code, body = doTemplates(t, s, "/api/autonomous/templates?tag=stage:implement")
	if code != http.StatusOK || strings.Contains(body, "tpl-dw") || strings.Contains(body, "tpl-noproj") {
		t.Fatalf("tag=stage:implement: status %d body %s", code, body)
	}
}

func TestObjectTagFilterTemplateListAllMatch(t *testing.T) {
	both := templateTag("tpl-both", "both", []string{"project:datawatch", "automation:nightly"})
	one := templateTag("tpl-one", "one", []string{"project:datawatch"})
	s := &Server{cfg: config.DefaultConfig(), autonomousMgr: &tagMoreFake{
		fakeOrchAutonomous: &fakeOrchAutonomous{},
		templates:          []*autonomous.Template{both, one},
	}}

	code, body := doTemplates(t, s, "/api/autonomous/templates?tag=project:datawatch,automation:nightly")
	if code != http.StatusOK || !strings.Contains(body, "tpl-both") || !strings.Contains(body, "tpl-one") {
		t.Fatalf("any-match keeps both: status %d body %s", code, body)
	}

	code, body = doTemplates(t, s, "/api/autonomous/templates?tag=project:datawatch,automation:nightly&tag_match=all")
	if code != http.StatusOK || !strings.Contains(body, "tpl-both") || strings.Contains(body, "tpl-one") {
		t.Fatalf("all-match keeps only the full set: status %d body %s", code, body)
	}
}

func TestObjectTagFilterTemplateListBadTagMatchIs400(t *testing.T) {
	s := &Server{cfg: config.DefaultConfig(), autonomousMgr: &tagMoreFake{
		fakeOrchAutonomous: &fakeOrchAutonomous{},
		templates:          []*autonomous.Template{templateTag("tpl-dw", "dw", []string{"project:datawatch"})},
	}}
	code, body := doTemplates(t, s, "/api/autonomous/templates?tag=x&tag_match=both")
	if code != http.StatusBadRequest || !strings.Contains(body, "tag_match") {
		t.Fatalf("bad tag_match: status %d body %s", code, body)
	}
}

// --- decision record list ------------------------------------------------------

// drTagFake serves the fixture's decision record views from List; the create
// and mutate paths are refused, the list tests never call them.
type drTagFake struct {
	rows []autonomous.DecisionRecordView
}

func (f *drTagFake) List(filter autonomous.DecisionRecordFilter) []autonomous.DecisionRecordView {
	if filter.State != "" && filter.State != "open" {
		return []autonomous.DecisionRecordView{}
	}
	out := make([]autonomous.DecisionRecordView, 0, len(f.rows))
	out = append(out, f.rows...)
	return out
}

func (f *drTagFake) FolderStatus() autonomous.DecisionFolderStatus {
	return autonomous.DecisionFolderStatus{}
}

func (f *drTagFake) Create(autonomous.DecisionRecord, string, string) (autonomous.DecisionRecordView, []autonomous.QualityFinding, error) {
	return autonomous.DecisionRecordView{}, nil, errors.New("not wired in this test")
}
func (f *drTagFake) Update(string, autonomous.DecisionRecord) (autonomous.DecisionRecordView, []autonomous.QualityFinding, error) {
	return autonomous.DecisionRecordView{}, nil, errors.New("not wired in this test")
}
func (f *drTagFake) Withdraw(string, string, string) (autonomous.DecisionRecordView, error) {
	return autonomous.DecisionRecordView{}, errors.New("not wired in this test")
}
func (f *drTagFake) Get(string) (autonomous.DecisionRecordView, bool) {
	return autonomous.DecisionRecordView{}, false
}

// drTag builds a decision record view carrying the given system tags.
func drTag(id string, project string) autonomous.DecisionRecordView {
	var tags []string
	if project != "" {
		tags = []string{"project:" + project, "automaton:" + id}
	}
	return autonomous.DecisionRecordView{
		DecisionRecord: autonomous.DecisionRecord{ID: id, PRDID: "prd-" + id, SystemTags: tags},
	}
}

func doDecisionRecords(t *testing.T, s *Server, path string) (int, string) {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleDecisionRecords(rr, httptest.NewRequest(http.MethodGet, path, nil))
	return rr.Code, rr.Body.String()
}

func TestObjectTagFilterDecisionRecordList(t *testing.T) {
	dw := drTag("dr-dw", "datawatch")
	other := drTag("dr-oth", "other")
	none := drTag("dr-noproj", "") // names no axes at all
	s := &Server{cfg: config.DefaultConfig(), decisionRecords: &drTagFake{rows: []autonomous.DecisionRecordView{dw, other, none}}}

	// No filter returns all three, and the wire shape carries system_tags.
	code, body := doDecisionRecords(t, s, "/api/autonomous/decision-records")
	if code != http.StatusOK || !strings.Contains(body, "dr-dw") || !strings.Contains(body, "dr-oth") || !strings.Contains(body, "dr-noproj") {
		t.Fatalf("unfiltered: status %d body %s", code, body)
	}
	if !strings.Contains(body, `system_tags`) {
		t.Fatalf("list must expose system_tags: %s", body)
	}

	// A tag narrows to the record carrying it.
	code, body = doDecisionRecords(t, s, "/api/autonomous/decision-records?tag=project:datawatch")
	if code != http.StatusOK || !strings.Contains(body, "dr-dw") || strings.Contains(body, "dr-oth") || strings.Contains(body, "dr-noproj") {
		t.Fatalf("tag=project:datawatch: status %d body %s", code, body)
	}

	// The project= shortcut matches the project axis tag and drops both the
	// other project and the record that names no axes (no project).
	code, body = doDecisionRecords(t, s, "/api/autonomous/decision-records?project=datawatch")
	if code != http.StatusOK || !strings.Contains(body, "dr-dw") || strings.Contains(body, "dr-oth") || strings.Contains(body, "dr-noproj") {
		t.Fatalf("project=datawatch must drop the others: status %d body %s", code, body)
	}
	code, body = doDecisionRecords(t, s, "/api/autonomous/decision-records?project=other")
	if code != http.StatusOK || !strings.Contains(body, "dr-oth") || strings.Contains(body, "dr-dw") || strings.Contains(body, "dr-noproj") {
		t.Fatalf("project=other must drop the others: status %d body %s", code, body)
	}

	// The automaton= shortcut matches the automaton axis tag.
	code, body = doDecisionRecords(t, s, "/api/autonomous/decision-records?automaton=dr-oth")
	if code != http.StatusOK || !strings.Contains(body, "dr-oth") || strings.Contains(body, "dr-dw") || strings.Contains(body, "dr-noproj") {
		t.Fatalf("automaton=dr-oth: status %d body %s", code, body)
	}
}

func TestObjectTagFilterDecisionRecordListBadTagMatchIs400(t *testing.T) {
	s := &Server{cfg: config.DefaultConfig(), decisionRecords: &drTagFake{rows: []autonomous.DecisionRecordView{drTag("dr-dw", "datawatch")}}}
	code, body := doDecisionRecords(t, s, "/api/autonomous/decision-records?tag=x&tag_match=nope")
	if code != http.StatusBadRequest || !strings.Contains(body, "tag_match") {
		t.Fatalf("bad tag_match: status %d body %s", code, body)
	}
}

// --- held question list --------------------------------------------------------

// tagHeldQStore serves the fixture's open held questions from List; the rest
// of the QuestionsAPI surface is refused, the list tests never call it.
type tagHeldQStore struct {
	questions []autonomous.Question
}

func (f *tagHeldQStore) List(filter autonomous.QuestionFilter) []autonomous.Question {
	if filter.State != "" && filter.State != autonomous.QuestionOpen {
		return []autonomous.Question{}
	}
	if filter.PRDID != "" {
		out := []autonomous.Question{}
		for _, q := range f.questions {
			if q.PRDID == filter.PRDID {
				out = append(out, q)
			}
		}
		return out
	}
	out := make([]autonomous.Question, 0, len(f.questions))
	out = append(out, f.questions...)
	return out
}

func (f *tagHeldQStore) Raise(autonomous.Question) (autonomous.Question, error) {
	return autonomous.Question{}, errors.New("not wired in this test")
}
func (f *tagHeldQStore) Get(string) (autonomous.Question, bool) {
	return autonomous.Question{}, false
}
func (f *tagHeldQStore) Answer(string, autonomous.Answer) (autonomous.Answer, error) {
	return autonomous.Answer{}, errors.New("not wired in this test")
}
func (f *tagHeldQStore) Withdraw(string, string, string) (autonomous.Question, error) {
	return autonomous.Question{}, errors.New("not wired in this test")
}
func (f *tagHeldQStore) Answers(string) ([]autonomous.AnswerView, bool) {
	return nil, false
}
func (f *tagHeldQStore) CurrentAnswer(string) (autonomous.Answer, bool) {
	return autonomous.Answer{}, false
}
func (f *tagHeldQStore) AnswerCount(string) int { return 0 }

// heldQ builds an open held question carrying the given system tags.
func heldQ(id, prdID string, systemTags []string) autonomous.Question {
	return autonomous.Question{
		ID:         id,
		PRDID:      prdID,
		Source:     autonomous.QuestionSourceHeld,
		Text:       "held: " + id + " would raise a held question",
		SystemTags: systemTags,
	}
}

// heldShadowPRD builds an Automaton (stored PRD) carrying the given system
// tags and one shadow "held" note in the daemon's wire format.
func heldShadowPRD(id, project string) *autonomous.PRD {
	prd := &autonomous.PRD{
		ID:         id,
		Title:      "prd " + id,
		Status:     autonomous.PRDDraft,
		SystemTags: []string{"project:" + project, "automaton:" + id},
	}
	addShadowNote(prd, "t-"+id, "review_held", "opt-a", "a label")
	return prd
}

// addShadowNote appends one shadow-mode note the way the daemon logs it:
// "held:<key>:<state> — <state> on …; recommended option <id> (<label>)".
func addShadowNote(prd *autonomous.PRD, key, state, opt, label string) {
	if prd.Triage == nil {
		prd.Triage = &autonomous.TriageRecord{}
	}
	prd.Triage.Notes = append(prd.Triage.Notes, autonomous.TriageNote{
		Source: "held",
		Text: "held:" + key + ":" + state + " — " + state + " on task t-1: would raise a held question; " +
			"recommended option " + opt + " (" + label + ")",
	})
}

// heldCfg returns the operator-question switch flipped on, the gate the
// held-question handler enforces before anything else.
func heldCfg() *config.Config {
	cfg := config.DefaultConfig()
	cfg.Autonomous.Questions.Enabled = true
	return cfg
}

func doHeldQuestions(t *testing.T, s *Server, path string) (int, string) {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleHeldQuestions(rr, httptest.NewRequest(http.MethodGet, path, nil))
	return rr.Code, rr.Body.String()
}

func TestObjectTagFilterHeldQuestionsOpen(t *testing.T) {
	// q-dw carries the datawatch project; q-noproj asks without naming an
	// Automaton, so it carries no system tags (no project).
	qd := heldQ("q-dw", "prd-dw", []string{"project:datawatch", "automaton:prd-dw"})
	qn := heldQ("q-noproj", "prd-1", nil)
	s := &Server{
		cfg:           heldCfg(),
		autonomousMgr: &tagMoreFake{fakeOrchAutonomous: &fakeOrchAutonomous{}, prds: []*autonomous.PRD{{ID: "prd-dw"}, {ID: "prd-1"}}},
		questionStore: &tagHeldQStore{questions: []autonomous.Question{qd, qn}},
	}

	// No filter returns both, and the open entries carry system_tags.
	code, body := doHeldQuestions(t, s, "/api/autonomous/held-questions")
	if code != http.StatusOK || !strings.Contains(body, "q-dw") || !strings.Contains(body, "q-noproj") {
		t.Fatalf("unfiltered: status %d body %s", code, body)
	}
	if !strings.Contains(body, `system_tags`) {
		t.Fatalf("open entries must expose system_tags: %s", body)
	}

	// A tag narrows to the open question carrying it.
	code, body = doHeldQuestions(t, s, "/api/autonomous/held-questions?tag=project:datawatch")
	if code != http.StatusOK || !strings.Contains(body, "q-dw") || strings.Contains(body, "q-noproj") {
		t.Fatalf("tag=project:datawatch: status %d body %s", code, body)
	}

	// A question with no project is absent from a project-filtered result.
	code, body = doHeldQuestions(t, s, "/api/autonomous/held-questions?project=datawatch")
	if code != http.StatusOK || !strings.Contains(body, "q-dw") || strings.Contains(body, "q-noproj") {
		t.Fatalf("project=datawatch must drop the no-project question: status %d body %s", code, body)
	}

	// A tag neither question carries keeps both out.
	code, body = doHeldQuestions(t, s, "/api/autonomous/held-questions?tag=stage:implement")
	if code != http.StatusOK || strings.Contains(body, "q-dw") || strings.Contains(body, "q-noproj") {
		t.Fatalf("tag=stage:implement: status %d body %s", code, body)
	}
}

func TestObjectTagFilterHeldQuestionsShadowNotes(t *testing.T) {
	dw := heldShadowPRD("prd-dw", "datawatch")
	other := heldShadowPRD("prd-oth", "other")
	s := &Server{
		cfg:           heldCfg(),
		autonomousMgr: &tagMoreFake{fakeOrchAutonomous: &fakeOrchAutonomous{}, prds: []*autonomous.PRD{dw, other}},
		questionStore: &tagHeldQStore{},
	}

	// No filter returns both shadow notes, tagged.
	code, body := doHeldQuestions(t, s, "/api/autonomous/held-questions")
	if code != http.StatusOK || !strings.Contains(body, "prd-dw") || !strings.Contains(body, "prd-oth") {
		t.Fatalf("unfiltered: status %d body %s", code, body)
	}
	if !strings.Contains(body, `system_tags`) {
		t.Fatalf("shadow entries must expose system_tags: %s", body)
	}

	// The project= shortcut keeps only the note whose Automaton carries it.
	code, body = doHeldQuestions(t, s, "/api/autonomous/held-questions?project=datawatch")
	if code != http.StatusOK || !strings.Contains(body, "prd-dw") || strings.Contains(body, "prd-oth") {
		t.Fatalf("project=datawatch: status %d body %s", code, body)
	}
	code, body = doHeldQuestions(t, s, "/api/autonomous/held-questions?project=other")
	if code != http.StatusOK || !strings.Contains(body, "prd-oth") || strings.Contains(body, "prd-dw") {
		t.Fatalf("project=other: status %d body %s", code, body)
	}

	// A tag neither Automaton carries keeps both out.
	code, body = doHeldQuestions(t, s, "/api/autonomous/held-questions?tag=stage:implement")
	if code != http.StatusOK || strings.Contains(body, "prd-dw") || strings.Contains(body, "prd-oth") {
		t.Fatalf("tag=stage:implement: status %d body %s", code, body)
	}
}

func TestObjectTagFilterHeldQuestionsBadTagMatchIs400(t *testing.T) {
	s := &Server{
		cfg:           heldCfg(),
		autonomousMgr: &tagMoreFake{fakeOrchAutonomous: &fakeOrchAutonomous{}},
		questionStore: &tagHeldQStore{questions: []autonomous.Question{heldQ("q1", "prd-dw", []string{"project:datawatch"})}},
	}
	code, body := doHeldQuestions(t, s, "/api/autonomous/held-questions?tag=x&tag_match=xyz")
	if code != http.StatusBadRequest || !strings.Contains(body, "tag_match") {
		t.Fatalf("bad tag_match: status %d body %s", code, body)
	}
}
