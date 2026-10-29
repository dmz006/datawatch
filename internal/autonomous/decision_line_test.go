package autonomous

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// decisionLineFixture raises four decisions on a store whose clock the test
// drives, one per state, and returns the store and their ids (oldest first):
// answered, withdrawn, expired, open.
func decisionLineFixture(t *testing.T) (store *QuestionStore, answered, withdrawn, expired, open string) {
	t.Helper()
	store = newDecisionGateStore(t)
	clock := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return clock }
	raise := func(text, prdID string) string {
		q := sampleQuestion(text)
		q.Label = "approval"
		q.AskedBy = "supervisor"
		if prdID != "" {
			q.Context = QuestionContextPrd
			q.PRDID = prdID
		}
		id := mustRaise(t, store, q)
		clock = clock.Add(time.Minute)
		return id
	}
	answered = raise("Which way for the answered one?", "prd-1")
	withdrawn = raise("Which way for the withdrawn one?", "prd-1")
	expired = raise("Which way for the expired one?", "prd-2")
	open = raise("Which way for the open one?", "prd-1")

	if _, err := store.Answer(answered, Answer{Quote: "  go  first, été \"quoted\"\n", Actor: "dmz", Surface: "pwa"}); err != nil {
		t.Fatalf("answer: %v", err)
	}
	if _, err := store.Withdraw(withdrawn, "dmz", "no longer needed"); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if _, err := store.Expire(expired, "timed out"); err != nil {
		t.Fatalf("expire: %v", err)
	}
	return store, answered, withdrawn, expired, open
}

func decisionLinePRD(open, answered string) *PRD {
	return &PRD{ID: "prd-1", Story: []Story{{
		ID: "s-1",
		Tasks: []Task{
			{ID: "t-1", Spec: "wait for " + open, Status: TaskWaitingDecision},
			{ID: "t-2", Spec: "also needs " + open + " and " + answered, Status: TaskPending},
			{ID: "t-3", Spec: "uses " + answered, Status: TaskCompleted},
			{ID: "t-4", Spec: "names no decision at all", Status: TaskWaitingDecision},
		},
	}}}
}

func TestDecisionLine(t *testing.T) {
	store, answered, withdrawn, expired, open := decisionLineFixture(t)
	prd := decisionLinePRD(open, answered)
	lookup := func(id string) (*PRD, bool) {
		if id == "prd-1" {
			return prd, true
		}
		return nil, false
	}

	t.Run("newest first with every state", func(t *testing.T) {
		got := BuildDecisionLine(QuestionFilter{}, store, lookup)
		var ids, states []string
		for _, e := range got {
			ids = append(ids, e.ID)
			states = append(states, e.State)
		}
		if want := []string{open, expired, withdrawn, answered}; !reflect.DeepEqual(ids, want) {
			t.Fatalf("ids = %v; want %v", ids, want)
		}
		if want := []string{"open", "expired", "withdrawn", "answered"}; !reflect.DeepEqual(states, want) {
			t.Fatalf("states = %v; want %v", states, want)
		}
	})

	t.Run("entry fields", func(t *testing.T) {
		got := BuildDecisionLine(QuestionFilter{State: QuestionOpen}, store, lookup)
		if len(got) != 1 {
			t.Fatalf("entries = %d; want 1", len(got))
		}
		e := got[0]
		if e.ID != open || e.Question != "Which way for the open one?" || e.Label != "approval" ||
			e.AskedBy != "supervisor" || e.PRDID != "prd-1" || e.AskedAt == nil {
			t.Errorf("entry = %+v", e)
		}
		if e.Answer != nil {
			t.Errorf("answer = %+v; want nil for an open decision", e.Answer)
		}
		if want := []string{"t-1", "t-2"}; !reflect.DeepEqual(e.TaskIDs, want) {
			t.Errorf("task_ids = %v; want %v", e.TaskIDs, want)
		}
		if want := []string{"t-1"}; !reflect.DeepEqual(e.WaitingTaskIDs, want) {
			t.Errorf("waiting_task_ids = %v; want %v", e.WaitingTaskIDs, want)
		}
	})

	t.Run("answer is verbatim with its hash", func(t *testing.T) {
		got := BuildDecisionLine(QuestionFilter{State: QuestionAnswered}, store, lookup)
		if len(got) != 1 || got[0].Answer == nil {
			t.Fatalf("entries = %+v; want one answered entry with an answer", got)
		}
		a := got[0].Answer
		cur, _ := store.CurrentAnswer(answered)
		if a.ID != cur.ID || a.ID == "" {
			t.Errorf("answer id = %q; want %q", a.ID, cur.ID)
		}
		const quote = "  go  first, été \"quoted\"\n"
		if a.Quote != quote {
			t.Errorf("quote = %q; want it verbatim %q", a.Quote, quote)
		}
		sum := sha256.Sum256([]byte(quote))
		if a.QuoteSHA256 != hex.EncodeToString(sum[:]) {
			t.Errorf("quote_sha256 = %q; want the sha256 of the verbatim quote", a.QuoteSHA256)
		}
		if a.Actor != "dmz" || a.Surface != "pwa" || a.AnsweredAt == nil {
			t.Errorf("answer = %+v", a)
		}
		// The answered decision is named by t-2 (still pending) and t-3
		// (completed); neither is waiting on a decision.
		if want := []string{"t-2", "t-3"}; !reflect.DeepEqual(got[0].TaskIDs, want) {
			t.Errorf("task_ids = %v; want %v", got[0].TaskIDs, want)
		}
		if len(got[0].WaitingTaskIDs) != 0 {
			t.Errorf("waiting_task_ids = %v; want none", got[0].WaitingTaskIDs)
		}
	})

	t.Run("a superseding answer is the current one", func(t *testing.T) {
		first, _ := store.CurrentAnswer(answered)
		if _, err := store.Answer(answered, Answer{Quote: "second thoughts", Supersedes: first.ID, Actor: "dmz", Surface: "cli"}); err != nil {
			t.Fatalf("supersede: %v", err)
		}
		got := BuildDecisionLine(QuestionFilter{State: QuestionAnswered}, store, lookup)
		if len(got) != 1 || got[0].Answer == nil || got[0].Answer.Quote != "second thoughts" || got[0].Answer.Surface != "cli" {
			t.Fatalf("answer = %+v; want the superseding answer", got[0].Answer)
		}
	})

	t.Run("prd and state filters", func(t *testing.T) {
		got := BuildDecisionLine(QuestionFilter{PRDID: "prd-2"}, store, lookup)
		if len(got) != 1 || got[0].ID != expired {
			t.Fatalf("prd-2 entries = %+v; want only the expired decision", got)
		}
		if len(got[0].TaskIDs) != 0 || len(got[0].WaitingTaskIDs) != 0 {
			t.Errorf("an Automaton the lookup does not know has task lists %v / %v", got[0].TaskIDs, got[0].WaitingTaskIDs)
		}
		got = BuildDecisionLine(QuestionFilter{PRDID: "prd-1", State: QuestionWithdrawn}, store, lookup)
		if len(got) != 1 || got[0].ID != withdrawn {
			t.Fatalf("prd-1 withdrawn entries = %+v", got)
		}
	})

	t.Run("limit", func(t *testing.T) {
		got := BuildDecisionLine(QuestionFilter{Limit: 2}, store, lookup)
		if len(got) != 2 || got[0].ID != open || got[1].ID != expired {
			t.Fatalf("limit 2 entries = %+v", got)
		}
	})

	t.Run("nothing matches", func(t *testing.T) {
		got := BuildDecisionLine(QuestionFilter{PRDID: "prd-none"}, store, lookup)
		if got == nil || len(got) != 0 {
			t.Fatalf("entries = %#v; want an empty, non-nil slice", got)
		}
		b, _ := json.Marshal(got)
		if string(b) != "[]" {
			t.Errorf("json = %s; want []", b)
		}
	})

	t.Run("no lookup and no store", func(t *testing.T) {
		got := BuildDecisionLine(QuestionFilter{State: QuestionOpen}, store, nil)
		if len(got) != 1 || got[0].TaskIDs == nil || len(got[0].TaskIDs) != 0 {
			t.Fatalf("entries = %+v; want one entry with empty task lists", got)
		}
		if got := BuildDecisionLine(QuestionFilter{}, nil, lookup); len(got) != 0 {
			t.Fatalf("nil store entries = %+v; want none", got)
		}
	})

	t.Run("json shape", func(t *testing.T) {
		b, err := json.Marshal(BuildDecisionLine(QuestionFilter{State: QuestionOpen}, store, lookup)[0])
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		for _, k := range []string{"id", "question", "label", "state", "asked_by", "asked_at", "prd_id", "task_ids", "waiting_task_ids", "answer"} {
			if _, ok := m[k]; !ok {
				t.Errorf("entry lacks %q: %s", k, b)
			}
		}
		if m["answer"] != nil {
			t.Errorf("answer = %v; want null", m["answer"])
		}
	})
}
