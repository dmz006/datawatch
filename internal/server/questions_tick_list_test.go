// questions_tick_list_test.go — the coordinator tick reads worker questions
// through the same QuestionStore List method that GET /api/questions serves,
// so for every state the tick filters on, the HTTP list and the tick's read
// are the same set.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// TestQuestionTick: GET /api/questions and the tick's read return the same
// set of questions for open, answered and withdrawn.
func TestQuestionTick(t *testing.T) {
	s, store := polledServer(t, true)

	openID := polledRaise(t, s)
	answeredID := polledRaise(t, s)
	withdrawnID := polledRaise(t, s)
	polledAnswer(t, s, answeredID, map[string]any{"chosen_option_id": "yes", "actor": "operator", "surface": "rest"})
	rec := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]any{"actor": "operator", "reason": "no longer needed"})
	s.handleQuestions(rec, httptest.NewRequest(http.MethodPost, "/api/questions/"+withdrawnID+"/withdraw", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("withdraw: want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	for state, wantID := range map[autonomous.QuestionState]string{
		autonomous.QuestionOpen:      openID,
		autonomous.QuestionAnswered:  answeredID,
		autonomous.QuestionWithdrawn: withdrawnID,
	} {
		rec := httptest.NewRecorder()
		s.handleQuestions(rec, httptest.NewRequest(http.MethodGet, "/api/questions?state="+string(state)+"&limit=1000", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET state=%s: want 200, got %d: %s", state, rec.Code, rec.Body.String())
		}
		var env struct {
			Questions json.RawMessage `json:"questions"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode GET state=%s: %v", state, err)
		}
		// Each HTTP row is the stored question plus the computed context_ref;
		// decoding into the stored shape compares the stored records only.
		var httpRows []autonomous.Question
		if err := json.Unmarshal(env.Questions, &httpRows); err != nil {
			t.Fatalf("decode GET rows state=%s: %v", state, err)
		}
		viaHTTP, err := json.Marshal(httpRows)
		if err != nil {
			t.Fatalf("marshal GET rows state=%s: %v", state, err)
		}

		tick := autonomous.ReadQuestionsForTick(store, state)
		viaTick, err := json.Marshal(tick)
		if err != nil {
			t.Fatalf("marshal tick read state=%s: %v", state, err)
		}

		if !bytes.Equal(viaHTTP, viaTick) {
			t.Errorf("state=%s: GET /api/questions and the tick read differ\n http: %s\n tick: %s", state, viaHTTP, viaTick)
		}
		if len(tick) != 1 || tick[0].ID != wantID {
			t.Errorf("state=%s: tick read = %+v, want exactly question %s", state, tick, wantID)
		}
	}
}
