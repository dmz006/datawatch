// held_questions_test.go — GET /api/autonomous/held-questions: the mode
// payload in every mode (off, shadow, on) with a held-states subset, the
// prd_id and state query filters, read-only refusal, and the operator
// question switch gating it.
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// heldQuestionsMgr serves the fixture's Automata from ListPRDs; every other
// method is the fakeOrchAutonomous stub.
type heldQuestionsMgr struct {
	fakeOrchAutonomous
	prds []*autonomous.PRD
}

func (m *heldQuestionsMgr) ListPRDs() []any {
	out := make([]any, 0, len(m.prds))
	for _, p := range m.prds {
		out = append(out, p)
	}
	return out
}

// heldShadowNote builds one shadow-mode note the way the daemon logs it:
// "held:<key>:<state> — <state> on <who>: …; recommended option <id> (<label>)".
func heldShadowNote(key, state, opt, label string, at time.Time) autonomous.TriageNote {
	return autonomous.TriageNote{
		At:     at,
		Source: "held",
		Text:   "held:" + key + ":" + state + " — " + state + " on task t-1: would raise a held question; recommended option " + opt + " (" + label + ")",
	}
}

type heldQuestionsFixture struct {
	s          *Server
	store      *autonomous.QuestionStore
	now        time.Time
	openQ1     string // review_held, prd-1 / task t-1, open
	openQ2     string // awaiting_decision, prd-2, open
	answerQ    string // blocked, prd-1, answered
	strayOpen  string // non-held open question
	strayClose string // non-held answered question
}

func newHeldQuestionsFixture(t *testing.T, heldMode string) heldQuestionsFixture {
	t.Helper()
	store, err := autonomous.NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)

	raiseHeld := func(state autonomous.HeldState, prdID, taskID, reason string, attempts int) string {
		t.Helper()
		item := &autonomous.HeldItem{
			PRDID: prdID, PRDTitle: "Autom " + prdID, State: state,
			Reason: reason, AttemptsUsed: attempts,
			FailureCategory: autonomous.FailureCategoryContent,
		}
		if taskID != "" {
			item.TaskID = taskID
		}
		q := autonomous.BuildHeldQuestion(item)
		key := prdID
		if taskID != "" {
			key = taskID
		}
		q.SourceLineSHA256 = heldLineHash(key, string(state))
		got, err := store.Raise(q)
		if err != nil {
			t.Fatalf("Raise held %s: %v", state, err)
		}
		time.Sleep(time.Millisecond) // distinct creation times
		return got.ID
	}
	raisePlain := func(text string) string {
		t.Helper()
		q := autonomous.Question{
			Text: text, Label: "approval", AskedBy: "supervisor",
			Context:   autonomous.QuestionContextNone,
			Default:   "no",
			Options:   []autonomous.QuestionOption{{ID: "yes", Label: "Yes"}, {ID: "no", Label: "No"}},
		}
		got, err := store.Raise(q)
		if err != nil {
			t.Fatalf("Raise plain: %v", err)
		}
		time.Sleep(time.Millisecond)
		return got.ID
	}

	fx := heldQuestionsFixture{store: store, now: now}
	fx.openQ1 = raiseHeld(autonomous.HeldReviewHeld, "prd-1", "t-1", "two attempts used", 2)
	fx.openQ2 = raiseHeld(autonomous.HeldAwaitingDecision, "prd-2", "", "waiting for the operator", 0)
	fx.answerQ = raiseHeld(autonomous.HeldBlocked, "prd-1", "", "blocked outside", 3)
	fx.strayOpen = raisePlain("A plain open question that is not held?")
	fx.strayClose = raisePlain("A plain answered one?")
	if _, err := store.Answer(fx.answerQ, autonomous.Answer{Quote: "keep it held", Actor: "dmz", Surface: "pwa"}); err != nil {
		t.Fatalf("answer held: %v", err)
	}
	if _, err := store.Answer(fx.strayClose, autonomous.Answer{Quote: "no", Actor: "dmz", Surface: "pwa"}); err != nil {
		t.Fatalf("answer plain: %v", err)
	}

	prds := []*autonomous.PRD{
		{
			ID: "prd-1", Title: "Autom A", AutomationID: "auto-7",
			Triage: &autonomous.TriageRecord{
				PRDID: "prd-1", PRDTitle: "Autom A", Status: "blocked",
				Notes: []autonomous.TriageNote{
					heldShadowNote("t-1", "review_held", "hold", "Hold — keep it held, take no action yet", now),
					{At: now, Source: "alert", Text: "an unrelated alert note that must not count"},
					heldShadowNote("prd-1", "blocked", "resume", "Release the hold and let the Automaton continue", now),
				},
			},
		},
		{
			ID: "prd-2", Title: "Autom B", AutomationID: "auto-8",
			Triage: &autonomous.TriageRecord{
				PRDID: "prd-2", PRDTitle: "Autom B", Status: "failed",
				Notes: []autonomous.TriageNote{
					heldShadowNote("prd-2", "awaiting_decision", "hold", "Hold — keep it held, take no action yet", now),
				},
			},
		},
	}
	cfg := config.Config{Autonomous: config.AutonomousConfig{Questions: config.AutonomousQuestionsConfig{
		Enabled:  true,
		HeldMode: heldMode,
	}}}
	fx.s = &Server{cfg: &cfg, autonomousMgr: &heldQuestionsMgr{prds: prds}, questionStore: store}
	return fx
}

func heldQuestionsGET(t *testing.T, s *Server, query string) (int, heldQuestionsBody, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleHeldQuestions(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/held-questions"+query, nil))
	body := heldQuestionsBody{}
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v: %s", err, rec.Body.String())
		}
	}
	return rec.Code, body, rec.Body.String()
}

type heldQuestionsBody struct {
	Mode    string                  `json:"mode"`
	States  []string                `json:"states"`
	Open    []map[string]any        `json:"open"`
	Shadow  []map[string]any        `json:"shadow"`
	Question *map[string]any       `json:"question"`
	Message *string                `json:"message"`
	Error   *string                `json:"error"`
}

func heldEntryFor(entries []map[string]any, key, value string) map[string]any {
	for _, e := range entries {
		if e[key] == value {
			return e
		}
	}
	return nil
}

func TestHeldQuestionsREST(t *testing.T) {
	fx := newHeldQuestionsFixture(t, "")

	t.Run("shadow mode payload: mode, all five states, open held questions and shadow notes", func(t *testing.T) {
		code, body, raw := heldQuestionsGET(t, fx.s, "")
		if code != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", code, raw)
		}
		if body.Mode != "shadow" {
			t.Fatalf("want mode shadow for the effective default, got %q", body.Mode)
		}
		wantStates := []string{"blocked", "review_held", "revision_requested", "awaiting_decision", "pending_insert"}
		if !reflect.DeepEqual(body.States, wantStates) {
			t.Fatalf("want all five state ids in order, got %v", body.States)
		}

		// Open: exactly the two open held questions — the answered held one
		// and both non-held questions are out, and every field is named.
		if len(body.Open) != 2 {
			t.Fatalf("want 2 open held questions, got %d: %s", len(body.Open), raw)
		}
		if got := heldEntryFor(body.Open, "question_id", fx.openQ1); got == nil {
			t.Fatalf("open list missing held question %s: %s", fx.openQ1, raw)
		}
		if got := heldEntryFor(body.Open, "question_id", fx.openQ1); got != nil {
			checks := map[string]any{
				"state":              "review_held",
				"prd_id":             "prd-1",
				"task_id":            "t-1",
				"automation_id":      "auto-7",
				"title":              "Autom A",
				"reason":             "two attempts used",
				"recommended_option": "change_spec", // content failure in review_held
				"default":            "hold",
			}
			for k, want := range checks {
				if got[k] != want {
					t.Errorf("open entry %s: want %s=%v, got %v", fx.openQ1, k, want, got[k])
				}
			}
			if got["created_at"] == nil {
				t.Errorf("open entry %s: created_at missing", fx.openQ1)
			}
			if tstr, _ := got["text"].(string); tstr == "" || !strings.Contains(tstr, "Why it is held: two attempts used.") {
				t.Errorf("open entry %s: want text carrying the held question's own text, got %v", fx.openQ1, got["text"])
			}
		}
		if got := heldEntryFor(body.Open, "question_id", fx.openQ2); got == nil {
			t.Fatalf("open list missing held question %s: %s", fx.openQ2, raw)
		}
		if got := heldEntryFor(body.Open, "question_id", fx.openQ2); got != nil {
			if want := "awaiting_decision"; got["state"] != want {
				t.Errorf("open entry %s: want state %s, got %v", fx.openQ2, want, got["state"])
			}
			if got["prd_id"] != "prd-2" || got["automation_id"] != "auto-8" || got["title"] != "Autom B" {
				t.Errorf("open entry %s: Automaton fields wrong: %v", fx.openQ2, got)
			}
			if got["recommended_option"] != "hold" {
				t.Errorf("open entry %s: want recommended_option hold, got %v", fx.openQ2, got["recommended_option"])
			}
		}
		for _, stray := range []string{fx.answerQ, fx.strayOpen, fx.strayClose} {
			if heldEntryFor(body.Open, "question_id", stray) != nil {
				t.Errorf("open list must not contain %s (answered or not held)", stray)
			}
		}

		// Shadow: exactly the three held notes (the alert note is out), each
		// naming its Automaton, held state, option and time.
		if len(body.Shadow) != 3 {
			t.Fatalf("want 3 shadow notes, got %d: %s", len(body.Shadow), raw)
		}
		wantShadows := map[string]map[string]any{
			"review_held":     {"prd_id": "prd-1", "recommended_option": "hold"},
			"blocked":         {"prd_id": "prd-1", "recommended_option": "resume"},
			"awaiting_decision": {"prd_id": "prd-2", "recommended_option": "hold"},
		}
		for _, e := range body.Shadow {
			st, _ := e["state"].(string)
			want, ok := wantShadows[st]
			if !ok {
				t.Fatalf("unexpected shadow state %q: %s", st, raw)
			}
			for k, v := range want {
				if e[k] != v {
					t.Errorf("shadow %s: want %s=%v, got %v", st, k, v, e[k])
				}
			}
			at, ok := e["at"].(string)
			if !ok {
				t.Errorf("shadow %s: at missing", st)
			} else if ts, err := time.Parse(time.RFC3339Nano, at); err != nil {
				t.Errorf("shadow %s: at not RFC3339: %v (%q)", st, err, at)
			} else if diff := ts.Sub(fx.now); diff < -time.Second || diff > time.Second {
				t.Errorf("shadow %s: at %s drifted from the note time", st, at)
			}
		}
	})

	t.Run("mode off reports off and keeps the data lists", func(t *testing.T) {
		fx := newHeldQuestionsFixture(t, "off")
		code, body, raw := heldQuestionsGET(t, fx.s, "")
		if code != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", code, raw)
		}
		if body.Mode != "off" {
			t.Fatalf("want mode off, got %q", body.Mode)
		}
		if len(body.Open) != 2 || len(body.Shadow) != 3 {
			t.Fatalf("off still reports what exists: want 2 open + 3 shadow, got %d + %d", len(body.Open), len(body.Shadow))
		}
	})

	t.Run("mode on with a held-states subset narrows state ids", func(t *testing.T) {
		fx := newHeldQuestionsFixture(t, "")
		fx.s.cfg.Autonomous.Questions.HeldMode = "on"
		fx.s.cfg.Autonomous.Questions.HeldStates = []string{"blocked", "review_held"}
		code, body, raw := heldQuestionsGET(t, fx.s, "")
		if code != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", code, raw)
		}
		if body.Mode != "on" {
			t.Fatalf("want mode on, got %q", body.Mode)
		}
		if want := []string{"blocked", "review_held"}; !reflect.DeepEqual(body.States, want) {
			t.Fatalf("want the effective held-state subset %v, got %v", want, body.States)
		}
	})
}

func TestHeldQuestionsRESTFilters(t *testing.T) {
	fx := newHeldQuestionsFixture(t, "shadow")

	t.Run("prd_id narrows both lists to that Automaton", func(t *testing.T) {
		code, body, raw := heldQuestionsGET(t, fx.s, "?prd_id=prd-2")
		if code != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", code, raw)
		}
		if len(body.Open) != 1 {
			t.Fatalf("prd_id=prd-2 wants exactly one open held question, got %d: %s", len(body.Open), raw)
		}
		if body.Open[0]["question_id"] != fx.openQ2 || body.Open[0]["prd_id"] != "prd-2" {
			t.Fatalf("prd_id=prd-2 open list wrong: %v", body.Open)
		}
		if len(body.Shadow) != 1 || body.Shadow[0]["prd_id"] != "prd-2" {
			t.Fatalf("prd_id=prd-2 shadow list wrong: %v", body.Shadow)
		}
	})

	t.Run("state narrows open and shadow to that held state", func(t *testing.T) {
		code, body, raw := heldQuestionsGET(t, fx.s, "?state=review_held")
		if code != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", code, raw)
		}
		if len(body.Open) != 1 || body.Open[0]["question_id"] != fx.openQ1 {
			t.Fatalf("state=review_held open list wrong: %v", body.Open)
		}
		if len(body.Shadow) != 1 || body.Shadow[0]["state"] != "review_held" {
			t.Fatalf("state=review_held shadow list wrong: %v", body.Shadow)
		}

		code, body, raw = heldQuestionsGET(t, fx.s, "?state=awaiting_decision")
		if code != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", code, raw)
		}
		if len(body.Open) != 1 || body.Open[0]["question_id"] != fx.openQ2 {
			t.Fatalf("state=awaiting_decision open list wrong: %v", body.Open)
		}
		if len(body.Shadow) != 1 || body.Shadow[0]["prd_id"] != "prd-2" {
			t.Fatalf("state=awaiting_decision shadow list wrong: %v", body.Shadow)
		}

		// A held state the fixture holds nothing of: both lists empty, still
		// arrays, never null.
		code, body, raw = heldQuestionsGET(t, fx.s, "?state=pending_insert")
		if code != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", code, raw)
		}
		if body.Open == nil || body.Shadow == nil || len(body.Open) != 0 || len(body.Shadow) != 0 {
			t.Fatalf("state=pending_insert wants empty arrays, got open=%v shadow=%v", body.Open, body.Shadow)
		}
	})

	t.Run("the filters compose", func(t *testing.T) {
		code, body, raw := heldQuestionsGET(t, fx.s, "?prd_id=prd-1&state=review_held")
		if code != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", code, raw)
		}
		if len(body.Open) != 1 || body.Open[0]["question_id"] != fx.openQ1 {
			t.Fatalf("composed open list wrong: %v", body.Open)
		}
		if len(body.Shadow) != 1 || body.Shadow[0]["prd_id"] != "prd-1" || body.Shadow[0]["state"] != "review_held" {
			t.Fatalf("composed shadow list wrong: %v", body.Shadow)
		}
	})

	t.Run("an unknown held state id is refused", func(t *testing.T) {
		rec := httptest.NewRecorder()
		fx.s.handleHeldQuestions(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/held-questions?state=bogus", nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("want 400 for state=bogus, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// TestHeldQuestionsRESTEntryTextCapped pins the report entry's text cap: a
// held question whose table text is long is reported capped and elided, while
// the question record itself keeps the full text.
func TestHeldQuestionsRESTEntryTextCapped(t *testing.T) {
	fx := newHeldQuestionsFixture(t, "on")
	long := strings.Repeat("the spec step is ambiguous", 40) // > 500 runes in the reason, no trailing space to normalize
	q := autonomous.BuildHeldQuestion(&autonomous.HeldItem{
		PRDID: "prd-1", PRDTitle: "Autom A", TaskID: "t-long",
		State: autonomous.HeldBlocked, Reason: long,
		FailureCategory: autonomous.FailureCategoryContent,
	})
	q.SourceLineSHA256 = heldLineHash("t-long", string(autonomous.HeldBlocked))
	raised, err := fx.store.Raise(q)
	if err != nil {
		t.Fatalf("Raise long held: %v", err)
	}

	code, body, raw := heldQuestionsGET(t, fx.s, "")
	if code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", code, raw)
	}
	got := heldEntryFor(body.Open, "question_id", raised.ID)
	if got == nil {
		t.Fatalf("open list missing long held question %s: %s", raised.ID, raw)
	}
	text, _ := got["text"].(string)
	if n := utf8.RuneCountInString(text); n != heldQuestionTextMaxRunes {
		t.Fatalf("capped text wants %d runes, got %d", heldQuestionTextMaxRunes, n)
	}
	if !strings.HasSuffix(text, "…") {
		t.Fatalf("capped text must be elided with an ellipsis, got %q", text[len(text)-16:])
	}
	if !strings.HasPrefix(text, "Automaton \"Autom A\" (prd-1)") {
		t.Fatalf("capped text must keep the question's opening, got %q", text[:64])
	}
	if reason, _ := got["reason"].(string); reason != long {
		t.Errorf("reason keeps the full reason, got %q…", reason[:32])
	}
}

// TestHeldQuestionText pins the entry helper: trim, pass-through at the cap,
// elision past it, and empty in, empty out (the field is then omitted).
func TestHeldQuestionText(t *testing.T) {
	if got := heldQuestionText(""); got != "" {
		t.Fatalf("empty text must stay empty (the entry then omits the field), got %q", got)
	}
	if got := heldQuestionText("   "); got != "" {
		t.Fatalf("whitespace text must read empty, got %q", got)
	}
	short := "Should the daemon hold t_9?"
	if got := heldQuestionText("  " + short + "  "); got != short {
		t.Fatalf("short text must be trimmed and kept, got %q", got)
	}
	exact := strings.Repeat("a", heldQuestionTextMaxRunes)
	if got := heldQuestionText(exact); got != exact {
		t.Fatalf("text at the cap must be kept whole, got %d runes", utf8.RuneCountInString(got))
	}
	over := strings.Repeat("b", heldQuestionTextMaxRunes+1)
	got := heldQuestionText(over)
	if n := utf8.RuneCountInString(got); n != heldQuestionTextMaxRunes {
		t.Fatalf("capped text wants %d runes, got %d", heldQuestionTextMaxRunes, n)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("capped text must be elided with an ellipsis, got %q", got[len(got)-16:])
	}
}

func TestHeldQuestionsRESTGateDisabled(t *testing.T) {
	fx := newHeldQuestionsFixture(t, "on")
	fx.s.cfg.Autonomous.Questions.Enabled = false
	rec := httptest.NewRecorder()
	fx.s.handleHeldQuestions(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/held-questions", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 while the operator question switch is off, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "autonomous.questions.enabled") {
		t.Fatalf("refusal should name the switch: %s", rec.Body.String())
	}
}

func TestHeldQuestionsRESTNoStore(t *testing.T) {
	fx := newHeldQuestionsFixture(t, "on")
	fx.s.questionStore = nil
	rec := httptest.NewRecorder()
	fx.s.handleHeldQuestions(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/held-questions", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 when the question store is not wired in, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHeldQuestionsRESTReadOnly(t *testing.T) {
	fx := newHeldQuestionsFixture(t, "on")

	t.Run("POST is refused", func(t *testing.T) {
		rec := httptest.NewRecorder()
		fx.s.handleHeldQuestions(rec, httptest.NewRequest(http.MethodPost, "/api/autonomous/held-questions", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("want 405 for POST, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("PUT is refused", func(t *testing.T) {
		rec := httptest.NewRecorder()
		fx.s.handleHeldQuestions(rec, httptest.NewRequest(http.MethodPut, "/api/autonomous/held-questions", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("want 405 for PUT, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}
