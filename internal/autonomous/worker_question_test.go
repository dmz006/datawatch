// worker_question_test.go — raising a recognised worker question into the
// question store: the record built from the event, dedupe on session id and
// line hash, the refusal audit (hash and reason, never the line), the same
// notifications and decision-log entry a REST raise gets, and the persistence
// of the new fields with replay of records that lack them.
package autonomous

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/session"
)

const wqTestSession = "host-ab12"

// wqLineHash is the hash a scanner would compute for line.
func wqLineHash(line string) string {
	sum := sha256.Sum256([]byte(line))
	return hex.EncodeToString(sum[:])
}

// wqSignal builds a valid two-option signal whose hash is that of line.
func wqSignal(line string) *session.WorkerQuestionSignal {
	return &session.WorkerQuestionSignal{
		Question: "Which store should hold the cache?",
		Options: []session.WorkerQuestionOption{
			{ID: "mem", Label: "In memory", Tradeoffs: "lost on restart"},
			{ID: "disk", Label: "On disk"},
		},
		Default:    "hold",
		Label:      "decision",
		Risk:       "normal",
		LineSHA256: wqLineHash(line),
	}
}

func wqEvent(line string) session.WorkerQuestionEvent {
	return session.WorkerQuestionEvent{
		SessionFullID: wqTestSession,
		Signal:        wqSignal(line),
		LineSHA256:    wqLineHash(line),
	}
}

// wqFakes records what the raiser hands to its audit sink, notifier and log.
type wqFakes struct {
	mu       sync.Mutex
	audits   []string
	details  []map[string]any
	notified []Question
	logs     []string
}

func (f *wqFakes) deps(store *QuestionStore) WorkerQuestionDeps {
	return WorkerQuestionDeps{
		Store: store,
		Audit: func(action string, details map[string]any) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.audits = append(f.audits, action)
			f.details = append(f.details, details)
		},
		Notify: func(q Question) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.notified = append(f.notified, q)
		},
		Logf: func(format string, args ...any) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.logs = append(f.logs, fmt.Sprintf(format, args...))
		},
	}
}

func wqStore(t *testing.T) (*QuestionStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "questions.jsonl")
	s, err := NewQuestionStore(path)
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	return s, path
}

func TestWorkerQuestionRaisesRecordFromEvent(t *testing.T) {
	store, _ := wqStore(t)
	f := &wqFakes{}
	var storeAudits []string
	store.SetAuditFn(func(action string, _ map[string]any) { storeAudits = append(storeAudits, action) })

	line := "DATAWATCH_QUESTION: full"
	ev := wqEvent(line)
	ev.PRDID, ev.TaskID = "prd1", "task1"
	ev.Signal.Recommendation = &session.WorkerQuestionRecommendation{OptionID: "disk", Rationale: "survives restarts"}
	ev.Signal.Default = "mem"
	ev.Signal.Blocked = true
	ev.Signal.Label = "design review"
	ev.Signal.Risk = "low"

	q, raised := RaiseWorkerQuestion(f.deps(store), ev)
	if !raised {
		t.Fatal("RaiseWorkerQuestion raised nothing")
	}
	got, ok := store.Get(q.ID)
	if !ok {
		t.Fatalf("question %s not in the store", q.ID)
	}
	if got.AskedBy != "worker-ab12" || got.AskerKind != QuestionAskerSession {
		t.Errorf("asker = %q/%q, want worker-ab12/session", got.AskedBy, got.AskerKind)
	}
	if got.SessionID != wqTestSession || got.PRDID != "prd1" || got.TaskID != "task1" {
		t.Errorf("links = %q %q %q", got.SessionID, got.PRDID, got.TaskID)
	}
	if got.Context != QuestionContextTask {
		t.Errorf("context = %q, want task", got.Context)
	}
	if got.Text != ev.Signal.Question || len(got.Options) != 2 || got.Options[0].Tradeoffs != "lost on restart" {
		t.Errorf("text/options not carried: %+v", got)
	}
	if got.Recommendation == nil || got.Recommendation.OptionID != "disk" || got.Recommendation.Rationale != "survives restarts" {
		t.Errorf("recommendation = %+v", got.Recommendation)
	}
	if got.Default != "mem" || !got.Blocking || got.Label != "design review" || got.Risk != "low" {
		t.Errorf("default/blocking/label/risk = %q %v %q %q", got.Default, got.Blocking, got.Label, got.Risk)
	}
	if got.Priority != QuestionPriorityNormal || got.ReturnPath {
		t.Errorf("priority/return_path = %q/%v, want normal/false", got.Priority, got.ReturnPath)
	}
	if got.Source != QuestionSourceMarker || got.SourceLineSHA256 != wqLineHash(line) {
		t.Errorf("source = %q hash %q", got.Source, got.SourceLineSHA256)
	}
	if got.State != QuestionOpen {
		t.Errorf("state = %q, want open", got.State)
	}
	if len(f.notified) != 1 || f.notified[0].ID != q.ID {
		t.Errorf("notify calls = %+v, want one for %s", f.notified, q.ID)
	}
	// The store wrote its own question_raised entry, as for a REST raise.
	if len(storeAudits) != 1 || storeAudits[0] != audit.ActionQuestionRaised {
		t.Errorf("store audits = %v, want [question_raised]", storeAudits)
	}
	if len(f.audits) != 0 {
		t.Errorf("a raise wrote refusal audits: %v", f.audits)
	}
}

func TestWorkerQuestionContextAndDefaults(t *testing.T) {
	cases := []struct {
		name        string
		prd, task   string
		blank       bool
		wantContext string
	}{
		{name: "task id sets context task", prd: "p1", task: "t1", wantContext: QuestionContextTask},
		{name: "no task and no prd is context none", wantContext: QuestionContextNone},
		{name: "prd without task is context prd", prd: "p1", wantContext: QuestionContextPrd},
		{name: "absent default means hold", blank: true, wantContext: QuestionContextNone},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, _ := wqStore(t)
			ev := wqEvent(fmt.Sprintf("line %d", i))
			ev.PRDID, ev.TaskID = tc.prd, tc.task
			if tc.blank {
				ev.Signal.Default = ""
			}
			q, raised := RaiseWorkerQuestion((&wqFakes{}).deps(store), ev)
			if !raised {
				t.Fatal("not raised")
			}
			if q.Context != tc.wantContext {
				t.Errorf("context = %q, want %q", q.Context, tc.wantContext)
			}
			if q.Default != "hold" {
				t.Errorf("default = %q, want hold", q.Default)
			}
		})
	}
}

func TestWorkerQuestionInvalidDefaultIsRefused(t *testing.T) {
	store, _ := wqStore(t)
	f := &wqFakes{}
	ev := wqEvent("line with bad default")
	ev.Signal.Default = "nope"

	if _, raised := RaiseWorkerQuestion(f.deps(store), ev); raised {
		t.Fatal("a default that is neither an option nor hold was raised")
	}
	if n := len(store.List(QuestionFilter{})); n != 0 {
		t.Errorf("store holds %d questions, want 0", n)
	}
	if len(f.audits) != 1 || f.audits[0] != audit.ActionQuestionMarkerRefused {
		t.Fatalf("audits = %v, want one question_marker_refused", f.audits)
	}
	if f.details[0]["reason"] != "invalid_default" {
		t.Errorf("reason = %v, want invalid_default", f.details[0]["reason"])
	}
	if len(f.notified) != 0 {
		t.Errorf("a refusal notified: %+v", f.notified)
	}
}

func TestWorkerQuestionRefusalAuditsHashNeverTheLine(t *testing.T) {
	store, path := wqStore(t)
	f := &wqFakes{}
	line := `DATAWATCH_QUESTION: {"question":"secret-token-xyz","options":[{"id":"a","label":"A"}]}`
	ev := session.WorkerQuestionEvent{
		SessionFullID: wqTestSession,
		RefusalReason: "too_few_options",
		LineSHA256:    wqLineHash(line),
	}

	if _, raised := RaiseWorkerQuestion(f.deps(store), ev); raised {
		t.Fatal("a refusal was raised")
	}
	if len(f.audits) != 1 || f.audits[0] != audit.ActionQuestionMarkerRefused || audit.ActionQuestionMarkerRefused != "question_marker_refused" {
		t.Fatalf("audits = %v, want [question_marker_refused]", f.audits)
	}
	d := f.details[0]
	if d["session_id"] != wqTestSession || d["reason"] != "too_few_options" || d["line_sha256"] != wqLineHash(line) {
		t.Errorf("details = %+v", d)
	}
	if len(d) != 3 {
		t.Errorf("details carry %d keys, want session_id, reason and line_sha256 only: %+v", len(d), d)
	}
	for _, v := range d {
		if s, _ := v.(string); strings.Contains(s, "secret-token-xyz") {
			t.Errorf("audit detail carries the line text: %q", s)
		}
	}
	if len(f.logs) != 1 {
		t.Fatalf("logged %d lines, want 1: %v", len(f.logs), f.logs)
	}
	if strings.Contains(f.logs[0], "secret-token-xyz") || !strings.Contains(f.logs[0], wqTestSession) ||
		!strings.Contains(f.logs[0], "too_few_options") {
		t.Errorf("log line = %q", f.logs[0])
	}
	if len(f.notified) != 0 || len(store.List(QuestionFilter{})) != 0 {
		t.Error("a refusal raised or notified")
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("a refusal wrote to the question store file")
	}
}

func TestWorkerQuestionDedupesOpenQuestionBySessionAndLine(t *testing.T) {
	store, _ := wqStore(t)
	f := &wqFakes{}
	deps := f.deps(store)

	first, raised := RaiseWorkerQuestion(deps, wqEvent("same line"))
	if !raised {
		t.Fatal("first raise did nothing")
	}
	again, raised := RaiseWorkerQuestion(deps, wqEvent("same line"))
	if raised {
		t.Fatal("the same line raised a second question while the first is open")
	}
	if again.ID != first.ID {
		t.Errorf("duplicate returned %q, want the open question %q", again.ID, first.ID)
	}
	if n := len(store.List(QuestionFilter{})); n != 1 {
		t.Errorf("store holds %d questions, want 1", n)
	}
	if len(f.notified) != 1 {
		t.Errorf("notified %d times, want 1", len(f.notified))
	}

	// Another line, or the same line from another session, is a new question.
	if _, raised := RaiseWorkerQuestion(deps, wqEvent("other line")); !raised {
		t.Error("a different line was deduped")
	}
	other := wqEvent("same line")
	other.SessionFullID = "host-cd34"
	if _, raised := RaiseWorkerQuestion(deps, other); !raised {
		t.Error("the same line from another session was deduped")
	}

	// Once the question is no longer open, the line raises again.
	if _, err := store.Withdraw(first.ID, "operator", "test"); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	if _, raised := RaiseWorkerQuestion(deps, wqEvent("same line")); !raised {
		t.Error("the line stayed deduped after its question was withdrawn")
	}
}

func TestWorkerQuestionDedupeHoldsUnderConcurrency(t *testing.T) {
	store, _ := wqStore(t)
	f := &wqFakes{}
	deps := f.deps(store)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			RaiseWorkerQuestion(deps, wqEvent("racing line"))
		}()
	}
	wg.Wait()
	if n := len(store.List(QuestionFilter{})); n != 1 {
		t.Errorf("store holds %d questions after a racing raise, want 1", n)
	}
	if len(f.notified) != 1 {
		t.Errorf("notified %d times, want 1", len(f.notified))
	}
}

func TestWorkerQuestionGainsOperatorDecisionEntry(t *testing.T) {
	m, store, prdID := newOperatorDecisionRig(t)
	f := &wqFakes{}
	ev := wqEvent("decision log line")
	ev.PRDID, ev.TaskID = prdID, "task1"

	q, raised := RaiseWorkerQuestion(f.deps(store), ev)
	if !raised {
		t.Fatal("not raised")
	}
	got := operatorDecisions(t, m, prdID)
	if len(got) != 1 {
		t.Fatalf("decision log has %d operator entries, want 1: %+v", len(got), got)
	}
	d := got[0]
	if d.Kind != "operator_decision" || d.DecisionState != "open" || d.QuestionID != q.ID {
		t.Errorf("entry = %+v, want operator_decision/open for question %s", d, q.ID)
	}
	if d.Actor != "worker-ab12" {
		t.Errorf("actor = %q, want worker-ab12", d.Actor)
	}

	// A duplicate adds no second entry.
	RaiseWorkerQuestion(f.deps(store), ev)
	if n := len(operatorDecisions(t, m, prdID)); n != 1 {
		t.Errorf("a duplicate left %d decision entries, want 1", n)
	}
}

func TestWorkerQuestionFieldsPersistAndOldRecordsReplay(t *testing.T) {
	store, path := wqStore(t)
	ev := wqEvent("persisted line")
	ev.Signal.Blocked = true
	q, raised := RaiseWorkerQuestion((&wqFakes{}).deps(store), ev)
	if !raised {
		t.Fatal("not raised")
	}

	// A record written before these fields existed sits in the same file.
	old := `{"type":"question_raised","question":{"id":"q-old","text":"Old?","options":[{"id":"a","label":"A"},{"id":"b","label":"B"}],"default":"a","context":"none","return_path":false,"state":"open"}}` + "\n"
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(old); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	reopened, err := NewQuestionStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, ok := reopened.Get(q.ID)
	if !ok || !got.Blocking || got.Source != QuestionSourceMarker || got.SourceLineSHA256 != wqLineHash("persisted line") {
		t.Errorf("replayed question = %+v", got)
	}
	oldQ, ok := reopened.Get("q-old")
	if !ok || oldQ.Blocking || oldQ.Source != "" || oldQ.SourceLineSHA256 != "" {
		t.Errorf("old record = %+v (ok=%v), want it loaded with the new fields empty", oldQ, ok)
	}

	// The dedupe survives a restart: the open question still blocks a re-read.
	if _, raised := RaiseWorkerQuestion((&wqFakes{}).deps(reopened), ev); raised {
		t.Error("a re-read after restart raised a second question")
	}

	// The new fields stay off the wire when unset.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"source":"marker"`) || !strings.Contains(string(raw), `"blocking":true`) {
		t.Errorf("store file lacks the new fields: %s", raw)
	}
}

func TestWorkerQuestionSourceValidation(t *testing.T) {
	good := wqLineHash("x")
	cases := []struct {
		name    string
		mutate  func(q *Question)
		wantErr string
	}{
		{name: "rest source accepted", mutate: func(q *Question) { q.Source = QuestionSourceRest }},
		{name: "empty source accepted", mutate: func(q *Question) {}},
		{name: "unknown source refused", mutate: func(q *Question) { q.Source = "carrier-pigeon" }, wantErr: "source is"},
		{
			name:    "marker without session refused",
			mutate:  func(q *Question) { q.Source, q.SourceLineSHA256 = QuestionSourceMarker, good },
			wantErr: "source marker requires session_id",
		},
		{
			name:    "marker without line hash refused",
			mutate:  func(q *Question) { q.Source, q.SessionID = QuestionSourceMarker, wqTestSession },
			wantErr: "source marker requires source_line_sha256",
		},
		{
			name:    "line text in the hash field refused",
			mutate:  func(q *Question) { q.SourceLineSHA256 = "DATAWATCH_QUESTION: {}" },
			wantErr: "source_line_sha256 must be",
		},
		{
			name:    "uppercase digest refused",
			mutate:  func(q *Question) { q.SourceLineSHA256 = strings.ToUpper(good) },
			wantErr: "source_line_sha256 must be",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := baseQuestion()
			tc.mutate(q)
			err := ValidateNewQuestion(q)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateNewQuestion() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateNewQuestion() = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}
