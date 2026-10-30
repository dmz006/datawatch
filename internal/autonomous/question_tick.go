package autonomous

// The coordinator tick's question pass. A task parked on a blocking worker
// question (await_decision, await_decision.go) is resumed here, never by the
// executor: each tick reads the answered and withdrawn questions through the
// QuestionStore List method — the same read GET /api/questions serves, with a
// state filter — and resumes every parked task whose question has been
// decided.
//
//   - It makes no HTTP call and never reads session output.
//   - An open question leaves its task parked.
//   - Resume = re-admit through awaitReAdmitAs, re-claim the tree lock if the
//     park released it, then prompt the session with one line carrying the
//     decision (BuildDecisionResumeLine).
//   - Each resume runs in its own goroutine behind a per-task in-flight guard,
//     so the ticker never waits on a question, a session or capacity.
//   - The task records the answer it resumed for (Task.DecisionResume), so a
//     later tick never resumes it twice; a superseding answer is recorded as
//     the new current answer without a second resume.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// questionMarkerText is the worker question marker the resume line of a
// withdrawn question points at (the session scanner owns the parser).
const questionMarkerText = "DATAWATCH_QUESTION:"

// DecisionResume is what a task records when the coordinator tick resumed it
// for a worker question.
type DecisionResume struct {
	QuestionID string `json:"question_id"`
	// AnswerID is the answer the resume prompt carried; empty when the
	// question was withdrawn.
	AnswerID  string `json:"answer_id,omitempty"`
	Withdrawn bool   `json:"withdrawn,omitempty"`
	// CurrentAnswerID is the question's newest answer the tick has seen. It
	// differs from AnswerID once an answer superseded the one resumed for.
	CurrentAnswerID string    `json:"current_answer_id,omitempty"`
	ResumedAt       time.Time `json:"resumed_at"`
}

// QuestionLister is the read side of the question store that the REST layer
// and the tick share.
type QuestionLister interface {
	List(filter QuestionFilter) []Question
}

// ReadQuestionsForTick is the tick's one read of the question store: the
// questions in the given state, newest first, up to the store's maximum list
// size. GET /api/questions calls the same List method.
func ReadQuestionsForTick(l QuestionLister, state QuestionState) []Question {
	return l.List(QuestionFilter{State: state, Limit: maxQuestionListLimit})
}

// questionTickState guards the resumes in flight: one per task at a time.
type questionTickState struct {
	mu       sync.Mutex
	inflight map[string]bool
	wg       sync.WaitGroup
}

// begin claims the in-flight slot for a task and counts the resume in the
// wait group; false means a resume for that task is already running.
func (s *questionTickState) begin(taskID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inflight[taskID] {
		return false
	}
	if s.inflight == nil {
		s.inflight = make(map[string]bool)
	}
	s.inflight[taskID] = true
	s.wg.Add(1)
	return true
}

func (s *questionTickState) end(taskID string) {
	s.mu.Lock()
	delete(s.inflight, taskID)
	s.mu.Unlock()
	s.wg.Done()
}

// AwaitDecisionQuestionID returns the question id a task's wait reason names:
// "await_decision: <id>" or "await_decision: <id> — <queue reason>". Empty
// when the reason is not an await-decision one or names no question.
func AwaitDecisionQuestionID(reason string) string {
	if !IsAwaitDecisionWaitReason(reason) {
		return ""
	}
	rest := strings.TrimSpace(strings.TrimSpace(reason)[len(AwaitDecisionWaitReasonPrefix):])
	rest = strings.TrimSpace(strings.TrimPrefix(rest, ":"))
	rest = strings.TrimSpace(strings.TrimPrefix(rest, "—"))
	if i := strings.IndexAny(rest, " \t"); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

// questionPass is the coordinator's hook into the question tick.
func (c *Coordinator) questionPass(ctx context.Context) {
	if c == nil || c.m == nil {
		return
	}
	c.m.QuestionTick(ctx)
}

// QuestionTick runs one pass: it reads the answered and withdrawn questions
// and starts a resume goroutine for each parked task whose question was
// decided. It returns without waiting for any of them.
func (m *Manager) QuestionTick(ctx context.Context) {
	m.mu.Lock()
	store := m.decisionStore
	m.mu.Unlock()
	if store == nil {
		return
	}
	answered := indexQuestions(ReadQuestionsForTick(store, QuestionAnswered))
	withdrawn := indexQuestions(ReadQuestionsForTick(store, QuestionWithdrawn))
	if len(answered) == 0 && len(withdrawn) == 0 {
		return
	}

	for _, p := range m.store.ListPRDs() {
		prd, ok := m.store.SnapshotPRD(p.ID)
		if !ok {
			continue
		}
		for _, st := range prd.Story {
			for _, tk := range st.Tasks {
				m.questionTickTask(ctx, store, tk, answered, withdrawn)
			}
		}
	}
}

func indexQuestions(qs []Question) map[string]Question {
	out := make(map[string]Question, len(qs))
	for _, q := range qs {
		out[q.ID] = q
	}
	return out
}

// questionTickTask handles one task snapshot.
func (m *Manager) questionTickTask(ctx context.Context, store *QuestionStore, tk Task, answered, withdrawn map[string]Question) {
	if isTaskTerminal(tk.Status) {
		return
	}
	// A task already resumed for a question follows the question's newest
	// answer; it is never resumed again for it.
	if dr := tk.DecisionResume; dr != nil && !dr.Withdrawn {
		if _, ok := answered[dr.QuestionID]; ok {
			if cur, ok := store.CurrentAnswer(dr.QuestionID); ok && cur.ID != dr.CurrentAnswerID && cur.ID != dr.AnswerID {
				m.recordCurrentAnswer(tk.ID, dr.QuestionID, cur.ID)
			}
		}
	}

	if tk.Status != TaskWaitingCapacity || !IsAwaitDecisionWaitReason(tk.WaitReason) {
		return
	}
	qid := AwaitDecisionQuestionID(tk.WaitReason)
	if qid == "" {
		return
	}
	if dr := tk.DecisionResume; dr != nil && dr.QuestionID == qid {
		return // already resumed for this question
	}
	if q, ok := answered[qid]; ok {
		ans, ok := store.CurrentAnswer(qid)
		if !ok {
			return
		}
		m.startQuestionResume(ctx, tk, q, &ans)
		return
	}
	if q, ok := withdrawn[qid]; ok {
		m.startQuestionResume(ctx, tk, q, nil)
	}
	// Open (or unknown): the task stays parked.
}

// recordCurrentAnswer notes the newest answer on a task already resumed for
// the question, so status shows it as the new answer. It prompts nothing.
func (m *Manager) recordCurrentAnswer(taskID, questionID, answerID string) {
	m.store.MutateTask(taskID, func(c *Task) {
		if c.DecisionResume == nil || c.DecisionResume.QuestionID != questionID {
			return
		}
		next := *c.DecisionResume
		next.CurrentAnswerID = answerID
		c.DecisionResume = &next
	})
}

// startQuestionResume starts the resume of one parked task in its own
// goroutine unless one is already in flight for it. ans is nil for a
// withdrawn question.
func (m *Manager) startQuestionResume(ctx context.Context, tk Task, q Question, ans *Answer) {
	if !m.questionTick.begin(tk.ID) {
		return
	}
	go func() {
		defer m.questionTick.end(tk.ID)
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[autonomous] %s: question resume panicked: %v", tk.ID, r)
			}
		}()
		m.resumeForQuestion(ctx, tk, q, ans)
	}()
}

// waitQuestionResumes blocks until every resume started so far has finished.
// For tests; the tick itself never waits.
func (m *Manager) waitQuestionResumes() {
	m.questionTick.wg.Wait()
}

// resumeForQuestion resumes one task parked on a decided question: it records
// the resume, re-admits the task, re-claims the tree lock if the park released
// it, and prompts the session with the decision. A failed re-admission undoes
// the record so the next tick tries again.
func (m *Manager) resumeForQuestion(ctx context.Context, snap Task, q Question, ans *Answer) {
	if ctx.Err() != nil {
		return
	}
	prd, ok := m.store.GetPRD(snap.PRDID)
	if !ok {
		return
	}
	t, ok := m.store.GetTask(snap.ID)
	if !ok {
		return
	}

	rec := &DecisionResume{QuestionID: q.ID, Withdrawn: ans == nil, ResumedAt: time.Now().UTC()}
	if ans != nil {
		rec.AnswerID, rec.CurrentAnswerID = ans.ID, ans.ID
	}
	var claimed bool
	var prev *DecisionResume
	m.store.MutateTask(snap.ID, func(c *Task) {
		if c.Status != TaskWaitingCapacity || AwaitDecisionQuestionID(c.WaitReason) != q.ID {
			return
		}
		if c.DecisionResume != nil && c.DecisionResume.QuestionID == q.ID {
			return
		}
		prev = c.DecisionResume
		c.DecisionResume = rec
		claimed = true
	})
	if !claimed {
		return
	}

	box := m.treeLockBoxLocked(snap.ID)
	releasedTree := box != nil && *box == nil

	if err := m.awaitReAdmitAs(ctx, prd, t, AwaitDecisionWaitReasonFor(q.ID), snap.ComputeNode); err != nil {
		if !m.taskCancelled(snap.ID) {
			m.store.MutateTask(snap.ID, func(c *Task) {
				if c.DecisionResume == rec {
					c.DecisionResume = prev
				}
			})
		}
		log.Printf("[autonomous] %s: resume after decision %s not admitted: %v", snap.ID, q.ID, err)
		return
	}
	m.clearWaiting(t)
	if m.taskCancelled(snap.ID) {
		return
	}
	m.reclaimTreeLock(prd, t, releasedTree, box)

	sessionID := snap.SessionID
	if cur, ok := m.store.TaskSnapshot(snap.ID); ok && cur.SessionID != "" {
		sessionID = cur.SessionID
	}
	if fn := m.sessionContinueLineFnLocked(); fn != nil && sessionID != "" {
		if err := fn(sessionID, BuildDecisionResumeLine(q, ans)); err != nil {
			log.Printf("[autonomous] %s: decision resume prompt to session %s failed: %v", snap.ID, sessionID, err)
		}
	}
	m.emitDecisionResumeAudit(prd, snap.ID, rec)
	log.Printf("[autonomous] %s: resumed after decision %s (answer %q, withdrawn=%v); no retry counted", snap.ID, q.ID, rec.AnswerID, rec.Withdrawn)
}

func (m *Manager) emitDecisionResumeAudit(prd *PRD, taskID string, rec *DecisionResume) {
	if m.auditFn == nil {
		return
	}
	m.auditFn("autonomous_decision_resume", map[string]any{
		"prd":         prd.ID,
		"task":        taskID,
		"question_id": rec.QuestionID,
		"answer_id":   rec.AnswerID,
		"withdrawn":   rec.Withdrawn,
	})
}

// BuildDecisionResumeLine builds the one-line prompt that resumes a worker
// after its blocking question was decided. For an answer it carries the
// question id, the chosen option id and label, the operator's quote as a JSON
// string value, the answer id and the actor; for a withdrawn question (ans
// nil) it says so and that no default was applied. Free text is JSON-encoded,
// so a newline in it cannot break the line.
func BuildDecisionResumeLine(q Question, ans *Answer) string {
	if ans == nil {
		return fmt.Sprintf("[automaton decision] question=%s was withdrawn — no default was applied and no decision was made. Continue only with work that does not depend on it; if you are still blocked, raise a new question with the %s marker.",
			q.ID, questionMarkerText)
	}
	var label string
	for _, o := range q.Options {
		if o.ID == ans.ChosenOptionID {
			label = o.Label
			break
		}
	}
	return fmt.Sprintf("[automaton decision] question=%s option=%s label=%s quote=%s answer=%s actor=%s — the operator has decided; continue the task from where you left off using this decision.",
		q.ID, oneLineJSON(ans.ChosenOptionID), oneLineJSON(label), oneLineJSON(ans.Quote), ans.ID, oneLineJSON(ans.Actor))
}

// oneLineJSON encodes s as a JSON string value that holds no line break of
// any kind: Go's encoder escapes CR, LF, U+2028 and U+2029; NEL is escaped
// here.
func oneLineJSON(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	out := strings.TrimSuffix(b.String(), "\n")
	return strings.ReplaceAll(out, "\u0085", `\u0085`)
}
