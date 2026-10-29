package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// decisionsSpy serves one Automaton from GetPRD; every other method is the
// fakeOrchAutonomous stub.
type decisionsSpy struct {
	fakeOrchAutonomous
	prd *autonomous.PRD
}

func (d *decisionsSpy) GetPRD(id string) (any, bool) {
	if d.prd != nil && d.prd.ID == id {
		return d.prd, true
	}
	return nil, false
}

const decisionsQuote = "  go  first, été \"quoted\"\n"

type decisionsFixture struct {
	s                                  *Server
	answered, withdrawn, expired, open string
}

func newDecisionsFixture(t *testing.T) decisionsFixture {
	t.Helper()
	store, err := autonomous.NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	raise := func(text, prdID string) string {
		q := autonomous.Question{
			Text: text, Label: "approval", AskedBy: "supervisor",
			Context: autonomous.QuestionContextPrd, PRDID: prdID, Default: "first",
			Options: []autonomous.QuestionOption{{ID: "first", Label: "First"}, {ID: "second", Label: "Second"}},
		}
		got, err := store.Raise(q)
		if err != nil {
			t.Fatalf("Raise: %v", err)
		}
		time.Sleep(2 * time.Millisecond) // distinct creation times, so the order is newest first
		return got.ID
	}
	f := decisionsFixture{}
	f.answered = raise("Answered one?", "prd-1")
	f.withdrawn = raise("Withdrawn one?", "prd-1")
	f.expired = raise("Expired one?", "prd-2")
	f.open = raise("Open one?", "prd-1")
	if _, err := store.Answer(f.answered, autonomous.Answer{Quote: decisionsQuote, Actor: "dmz", Surface: "pwa"}); err != nil {
		t.Fatalf("answer: %v", err)
	}
	if _, err := store.Withdraw(f.withdrawn, "dmz", "no longer needed"); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if _, err := store.Expire(f.expired, "timed out"); err != nil {
		t.Fatalf("expire: %v", err)
	}
	prd := &autonomous.PRD{ID: "prd-1", Story: []autonomous.Story{{ID: "s-1", Tasks: []autonomous.Task{
		{ID: "t-1", Spec: "wait for " + f.open, Status: autonomous.TaskWaitingDecision},
		{ID: "t-2", Spec: "needs " + f.open + " and " + f.answered, Status: autonomous.TaskPending},
		{ID: "t-3", Spec: "no decision named", Status: autonomous.TaskWaitingDecision},
	}}}}
	f.s = &Server{autonomousMgr: &decisionsSpy{prd: prd}, questionStore: store}
	return f
}

func decisionsGET(t *testing.T, s *Server, query string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleAutonomousDecisions(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/decisions"+query, nil))
	return rec
}

func decisionEntries(t *testing.T, rec *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Decisions []map[string]any `json:"decisions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body.String())
	}
	if body.Decisions == nil {
		t.Fatalf("body has no decisions array: %s", rec.Body.String())
	}
	return body.Decisions
}

func decisionIDs(entries []map[string]any) []string {
	ids := []string{}
	for _, e := range entries {
		ids = append(ids, e["id"].(string))
	}
	return ids
}

func TestDecisionsREST(t *testing.T) {
	f := newDecisionsFixture(t)

	t.Run("list newest first with every state", func(t *testing.T) {
		rec := decisionsGET(t, f.s, "")
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		got := decisionEntries(t, rec)
		if want := []string{f.open, f.expired, f.withdrawn, f.answered}; !reflect.DeepEqual(decisionIDs(got), want) {
			t.Fatalf("ids = %v; want %v", decisionIDs(got), want)
		}
		for _, k := range []string{"id", "question", "label", "state", "asked_by", "asked_at", "prd_id", "task_ids", "waiting_task_ids", "answer"} {
			if _, ok := got[0][k]; !ok {
				t.Errorf("entry lacks %q: %v", k, got[0])
			}
		}
	})

	t.Run("state filter", func(t *testing.T) {
		for state, want := range map[string]string{
			"open": f.open, "answered": f.answered, "withdrawn": f.withdrawn, "expired": f.expired,
		} {
			got := decisionEntries(t, decisionsGET(t, f.s, "?state="+state))
			if len(got) != 1 || got[0]["id"] != want || got[0]["state"] != state {
				t.Errorf("state=%s entries = %v; want only %s", state, got, want)
			}
		}
		got := decisionEntries(t, decisionsGET(t, f.s, "?prd_id=prd-1&state=open"))
		if len(got) != 1 || got[0]["id"] != f.open {
			t.Errorf("prd_id + state entries = %v", got)
		}
	})

	t.Run("prd and limit filters", func(t *testing.T) {
		got := decisionEntries(t, decisionsGET(t, f.s, "?prd_id=prd-2"))
		if len(got) != 1 || got[0]["id"] != f.expired {
			t.Errorf("prd-2 entries = %v", got)
		}
		got = decisionEntries(t, decisionsGET(t, f.s, "?limit=2"))
		if want := []string{f.open, f.expired}; !reflect.DeepEqual(decisionIDs(got), want) {
			t.Errorf("limit=2 ids = %v; want %v", decisionIDs(got), want)
		}
		if n := len(decisionEntries(t, decisionsGET(t, f.s, "?limit=5000"))); n != 4 {
			t.Errorf("limit above the maximum: entries = %d; want it clamped, not refused", n)
		}
	})

	t.Run("bad state is a 400 naming the four states", func(t *testing.T) {
		rec := decisionsGET(t, f.s, "?state=pending")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("want 400, got %d: %s", rec.Code, rec.Body.String())
		}
		for _, w := range []string{"open", "answered", "withdrawn", "expired"} {
			if !strings.Contains(rec.Body.String(), w) {
				t.Errorf("400 body does not name %q: %s", w, rec.Body.String())
			}
		}
		for _, q := range []string{"?limit=0", "?limit=-3", "?limit=many"} {
			if rec := decisionsGET(t, f.s, q); rec.Code != http.StatusBadRequest {
				t.Errorf("%s: want 400, got %d", q, rec.Code)
			}
		}
	})

	t.Run("empty list is an empty array", func(t *testing.T) {
		rec := decisionsGET(t, f.s, "?prd_id=prd-none")
		if got := decisionEntries(t, rec); len(got) != 0 {
			t.Fatalf("entries = %v; want none", got)
		}
		if body := strings.TrimSpace(rec.Body.String()); body != `{"decisions":[]}` {
			t.Errorf("body = %s; want {\"decisions\":[]}", body)
		}
	})

	t.Run("waiting task is listed", func(t *testing.T) {
		got := decisionEntries(t, decisionsGET(t, f.s, "?state=open"))
		if len(got) != 1 {
			t.Fatalf("entries = %v", got)
		}
		if want := []any{"t-1", "t-2"}; !reflect.DeepEqual(got[0]["task_ids"], want) {
			t.Errorf("task_ids = %v; want %v", got[0]["task_ids"], want)
		}
		if want := []any{"t-1"}; !reflect.DeepEqual(got[0]["waiting_task_ids"], want) {
			t.Errorf("waiting_task_ids = %v; want %v", got[0]["waiting_task_ids"], want)
		}
		if got[0]["answer"] != nil {
			t.Errorf("answer = %v; want null for an open decision", got[0]["answer"])
		}
	})

	t.Run("answer quote verbatim with its sha256", func(t *testing.T) {
		got := decisionEntries(t, decisionsGET(t, f.s, "?state=answered"))
		if len(got) != 1 {
			t.Fatalf("entries = %v", got)
		}
		a, ok := got[0]["answer"].(map[string]any)
		if !ok {
			t.Fatalf("answer = %v; want an object", got[0]["answer"])
		}
		if a["quote"] != decisionsQuote {
			t.Errorf("quote = %q; want it verbatim %q", a["quote"], decisionsQuote)
		}
		sum := sha256.Sum256([]byte(decisionsQuote))
		if a["quote_sha256"] != hex.EncodeToString(sum[:]) {
			t.Errorf("quote_sha256 = %v; want %s", a["quote_sha256"], hex.EncodeToString(sum[:]))
		}
		if a["actor"] != "dmz" || a["surface"] != "pwa" || a["id"] == "" || a["answered_at"] == nil {
			t.Errorf("answer = %v", a)
		}
		if len(got[0]["waiting_task_ids"].([]any)) != 0 {
			t.Errorf("waiting_task_ids = %v; want none for an answered decision", got[0]["waiting_task_ids"])
		}
	})

	t.Run("method and wiring", func(t *testing.T) {
		rec := httptest.NewRecorder()
		f.s.handleAutonomousDecisions(rec, httptest.NewRequest(http.MethodPost, "/api/autonomous/decisions", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST: want 405, got %d", rec.Code)
		}
		if rec := decisionsGET(t, &Server{questionStore: f.s.questionStore}, ""); rec.Code != http.StatusServiceUnavailable {
			t.Errorf("no manager: want 503, got %d", rec.Code)
		}
		if rec := decisionsGET(t, &Server{autonomousMgr: f.s.autonomousMgr}, ""); rec.Code != http.StatusServiceUnavailable {
			t.Errorf("no question store: want 503, got %d", rec.Code)
		}
	})

	t.Run("autonomous read capability", func(t *testing.T) {
		deny := httptest.NewRequest(http.MethodGet, "/api/autonomous/decisions", nil)
		deny = deny.WithContext(context.WithValue(deny.Context(), sessionCapsKey, []string{federation.CapHealthRead}))
		rec := httptest.NewRecorder()
		f.s.handleAutonomousDecisions(rec, deny)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), federation.CapAutonomousRead) {
			t.Fatalf("without the read capability: want 403 naming it, got %d %q", rec.Code, rec.Body.String())
		}
		allow := httptest.NewRequest(http.MethodGet, "/api/autonomous/decisions", nil)
		allow = allow.WithContext(context.WithValue(allow.Context(), sessionCapsKey, []string{federation.CapAutonomousRead}))
		rec = httptest.NewRecorder()
		f.s.handleAutonomousDecisions(rec, allow)
		if rec.Code != http.StatusOK {
			t.Fatalf("with the read capability: want 200, got %d", rec.Code)
		}
	})
}
