// What a task records about the decision line it was resumed with: delivered
// only after the send worked, retried only when nothing was typed, and shown
// for review (never typed again) when that cannot be told.
//
// New record fields are read and written as JSON so these tests describe
// behaviour, not types.

package autonomous

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/session"
)

// errRefusedBeforeTyping is what Manager.SendInput returns when its first look
// at the pane finds a prompt; errPromptAfterTyping is what it returns when the
// prompt appears after the text was typed and only Enter was held back.
var (
	errRefusedBeforeTyping = session.ErrPromptOnScreen
	errPromptAfterTyping   = fmt.Errorf("send input: %w", session.ErrPromptOnScreen)
)

type deliveryFixture struct {
	*tickFixture
	smu    sync.Mutex
	errs   []error // returned in order; after the last, nil
	calls  []string
	to     []string
	alerts []string
}

func newDeliveryFixture(t *testing.T, errs ...error) *deliveryFixture {
	t.Helper()
	f := &deliveryFixture{tickFixture: newTickFixture(t), errs: errs}
	f.m.SetSessionContinueLineFn(func(sessionID, line string) error {
		f.smu.Lock()
		defer f.smu.Unlock()
		f.calls = append(f.calls, line)
		f.to = append(f.to, sessionID)
		if len(f.errs) == 0 {
			return nil
		}
		err := f.errs[0]
		f.errs = f.errs[1:]
		return err
	})
	f.m.SetAlertSink(func(level, title, body string) string {
		f.smu.Lock()
		defer f.smu.Unlock()
		f.alerts = append(f.alerts, level+"|"+title+"|"+body)
		return "a"
	})
	return f
}

func (f *deliveryFixture) sends() int {
	f.smu.Lock()
	defer f.smu.Unlock()
	return len(f.calls)
}

func (f *deliveryFixture) raised() []string {
	f.smu.Lock()
	defer f.smu.Unlock()
	return append([]string(nil), f.alerts...)
}

// record is the task's decision_resume object as the API serves it.
func (f *deliveryFixture) record(t *testing.T) map[string]any {
	t.Helper()
	b, err := json.Marshal(f.task(t))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		R map[string]any `json:"decision_resume"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out.R
}

func (f *deliveryFixture) delivery(t *testing.T) string {
	t.Helper()
	s, _ := f.record(t)["delivery"].(string)
	return s
}

// leave puts the task in the state a daemon that stopped part-way left behind:
// resumed in the store, with the given delivery state and no sender running.
func (f *deliveryFixture) leave(t *testing.T, ans Answer, delivery string, status TaskStatus) {
	t.Helper()
	rec := &DecisionResume{}
	raw := fmt.Sprintf(`{"question_id":%q,"answer_id":%q,"current_answer_id":%q,"resumed_at":"2026-11-25T10:00:00Z","delivery":%q,"delivery_session_id":"worker-sess"}`,
		f.qid, ans.ID, ans.ID, delivery)
	if err := json.Unmarshal([]byte(raw), rec); err != nil {
		t.Fatal(err)
	}
	f.m.store.MutateTask(f.task(t).ID, func(c *Task) {
		c.DecisionResume = rec
		c.Status = status
		if status == TaskInProgress {
			c.WaitReason = ""
		}
	})
}

const answerQuote = "operator words that must not reach an alert"

func (f *deliveryFixture) decide(t *testing.T) Answer {
	t.Helper()
	return f.answer(t, Answer{ChosenOptionID: "first", Quote: answerQuote, Actor: "operator"})
}

func TestAnswerRefusedBeforeTypingIsRetriedAndTypedOnce(t *testing.T) {
	f := newDeliveryFixture(t, errRefusedBeforeTyping, errRefusedBeforeTyping)
	a := &testAudit{}
	f.m.SetAuditFn(a.fn)
	ans := f.decide(t)

	d := wqDelta(f.tick)
	if got := f.delivery(t); got != "not_typed" {
		t.Fatalf("after a refusal: delivery = %q, want not_typed (record %v)", got, f.record(t))
	}
	if d.Resumed != 0 {
		t.Fatalf("a refused line counted as resumed")
	}
	// A repeat refusal says nothing new: it must not make the task look active
	// or add a log line on every pass.
	touched := f.task(t).UpdatedAt
	var logged bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logged)
	f.tick() // still refused
	log.SetOutput(prev)
	if got := f.task(t).UpdatedAt; !got.Equal(touched) {
		t.Errorf("a repeat refusal moved the task's updated time from %s to %s", touched, got)
	}
	if strings.Contains(logged.String(), "decision resume prompt") {
		t.Errorf("a repeat refusal logged again: %s", logged.String())
	}
	f.tick() // the prompt is gone
	f.tick()
	f.tick()
	if n := f.sends(); n != 3 {
		t.Fatalf("%d send attempts, want 2 refused and 1 that worked, then no more", n)
	}
	if got := f.delivery(t); got != "sent" {
		t.Fatalf("delivery = %q, want sent", got)
	}
	if !strings.Contains(f.calls[2], "answer="+ans.ID) {
		t.Fatalf("typed line %q lacks the answer", f.calls[2])
	}
	al := f.raised()
	if len(al) != 1 || !strings.HasPrefix(al[0], "warn|") || !strings.Contains(al[0], f.qid) {
		t.Fatalf("alerts = %q, want one warning naming the question however many refusals there are", al)
	}
	if strings.Contains(strings.Join(al, " "), answerQuote) {
		t.Fatalf("an alert carries the answer text: %q", al)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	resumes := 0
	for _, e := range a.events {
		if e.action == "autonomous_decision_resume" {
			resumes++
		}
	}
	if resumes != 1 {
		t.Fatalf("%d resume audit events, want 1, written when the line was sent", resumes)
	}
}

func TestAnswerUnknownOutcomeIsNeverRetypedAndNeedsReview(t *testing.T) {
	cases := map[string]error{
		"prompt appeared after the text was typed": errPromptAfterTyping,
		"send failed":                   errors.New("send input: exit status 1"),
		"typed but not saved":           errors.New("save session: disk full"),
		"tool-worded refusal is unsure": fmt.Errorf("refused: something else: %w", session.ErrPromptOnScreen),
	}
	for name, sendErr := range cases {
		t.Run(name, func(t *testing.T) {
			f := newDeliveryFixture(t, sendErr)
			f.decide(t)
			f.tick()
			f.tick()
			f.tick()
			if n := f.sends(); n != 1 {
				t.Fatalf("%d sends, want exactly 1: an unsure line is never typed again", n)
			}
			if got := f.delivery(t); got != "review_needed" {
				t.Fatalf("delivery = %q, want review_needed", got)
			}
			al := f.raised()
			if len(al) != 1 || !strings.HasPrefix(al[0], "error|") || !strings.Contains(al[0], "review needed") || !strings.Contains(al[0], f.qid) {
				t.Fatalf("alerts = %q, want one 'review needed' naming the question", al)
			}
			if strings.Contains(al[0], answerQuote) {
				t.Fatalf("the alert carries the answer text: %q", al[0])
			}
		})
	}
}

func TestAnswerInterruptedMidSendNeedsReviewAndIsNotRetyped(t *testing.T) {
	f := newDeliveryFixture(t)
	ans := f.decide(t)
	f.leave(t, ans, "sending", TaskInProgress)
	f.tick()
	f.tick()
	if n := f.sends(); n != 0 {
		t.Fatalf("%d sends after an interrupted send, want 0", n)
	}
	if got := f.delivery(t); got != "review_needed" {
		t.Fatalf("delivery = %q, want review_needed", got)
	}
	if al := f.raised(); len(al) != 1 || !strings.Contains(al[0], "review needed") {
		t.Fatalf("alerts = %q", al)
	}
}

func TestAnswerInterruptedBeforeTypingIsTypedOnce(t *testing.T) {
	for _, status := range []TaskStatus{TaskInProgress, TaskWaitingCapacity} {
		t.Run(string(status), func(t *testing.T) {
			f := newDeliveryFixture(t)
			ans := f.decide(t)
			f.leave(t, ans, "waiting", status)
			f.tick()
			f.tick()
			if n := f.sends(); n != 1 {
				t.Fatalf("%d sends, want 1", n)
			}
			if got := f.delivery(t); got != "sent" {
				t.Fatalf("delivery = %q, want sent", got)
			}
			if got := f.task(t); got.Status != TaskInProgress {
				t.Fatalf("status = %s, want in_progress", got.Status)
			}
		})
	}
}

func TestAnswerIsNotTypedWhenTheSendingMarkCannotBeSaved(t *testing.T) {
	f := newDeliveryFixture(t)
	ans := f.decide(t)
	f.leave(t, ans, "waiting", TaskInProgress)
	dir := f.m.store.recordsDir()
	if err := os.Chmod(dir, 0o500); err != nil { // #nosec G302 -- the test makes saving fail
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) }) // #nosec G302 -- restore the test dir
	if probe := os.WriteFile(filepath.Join(dir, "probe"), []byte("x"), 0o600); probe == nil {
		t.Skip("this user can write a read-only folder")
	}
	f.tick()
	if n := f.sends(); n != 0 {
		t.Fatalf("%d sends while the record could not be saved, want 0", n)
	}
	if err := os.Chmod(dir, 0o755); err != nil { // #nosec G302 -- restore the test dir
		t.Fatal(err)
	}
	f.tick()
	f.tick()
	if n := f.sends(); n != 1 {
		t.Fatalf("%d sends once saving works again, want 1", n)
	}
}

func TestAnswerNeverTypedIsNotTypedIntoAnotherAttempt(t *testing.T) {
	cases := map[string]func(c *Task){
		"a new worker session":   func(c *Task) { c.SessionID = "other-sess" },
		"the task was requeued":  func(c *Task) { c.Status = TaskPending },
		"the task needs review":  func(c *Task) { c.Status = TaskReviewNeeded },
		"the worker was dropped": func(c *Task) { c.SessionID = "" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			f := newDeliveryFixture(t, errRefusedBeforeTyping)
			f.decide(t)
			f.tick()
			f.m.store.MutateTask(f.task(t).ID, change)
			f.tick()
			f.tick()
			if n := f.sends(); n != 1 {
				t.Fatalf("%d sends, want only the refused one", n)
			}
			if got := f.delivery(t); got != "review_needed" {
				t.Fatalf("delivery = %q, want review_needed", got)
			}
			al := f.raised()
			if len(al) != 2 || !strings.Contains(al[1], "review needed") {
				t.Fatalf("alerts = %q, want the waiting warning then one review alert", al)
			}
		})
	}
}

func TestAnswerSupersededWhileWaitingTypesTheNewestOnce(t *testing.T) {
	f := newDeliveryFixture(t, errRefusedBeforeTyping)
	first := f.decide(t)
	f.tick()
	newer := f.answer(t, Answer{ChosenOptionID: "second", Actor: "operator", Supersedes: first.ID})
	f.tick()
	f.tick()
	if n := f.sends(); n != 2 {
		t.Fatalf("%d sends, want the refused one and one with the newest answer", n)
	}
	if !strings.Contains(f.calls[1], "answer="+newer.ID) {
		t.Fatalf("typed %q, want the newest answer %s", f.calls[1], newer.ID)
	}
	if got := f.record(t)["answer_id"]; got != newer.ID {
		t.Fatalf("answer_id = %v, want %s", got, newer.ID)
	}
}

// A record written before delivery was tracked says nothing about the send, so
// nothing is ever typed or raised for it.
func TestAnswerRecordFromBeforeDeliveryTrackingIsLeftAlone(t *testing.T) {
	f := newDeliveryFixture(t)
	ans := f.decide(t)
	f.leave(t, ans, "", TaskInProgress)
	f.tick()
	if f.sends() != 0 || len(f.raised()) != 0 {
		t.Fatalf("an old record was acted on: %d sends, alerts %q", f.sends(), f.raised())
	}
}

func TestHeldDecisionPromptRefusedBeforeTypingIsRetried(t *testing.T) {
	f := newDeliveryFixture(t, errRefusedBeforeTyping)
	ans := f.decide(t)
	q, ok := f.store.Get(f.qid)
	if !ok {
		t.Fatal("question missing")
	}
	cur, _ := f.m.store.GetPRD(f.prd.ID)
	tk := f.task(t)
	if err := f.m.resumeHeldAwaitingTask(context.Background(), cur, &tk, q, &ans); err != nil {
		t.Fatalf("held resume: %v", err)
	}
	if got := f.delivery(t); got != "not_typed" {
		t.Fatalf("delivery = %q, want not_typed", got)
	}
	f.tick()
	f.tick()
	if n := f.sends(); n != 2 {
		t.Fatalf("%d sends, want the refused one and the retry", n)
	}
	if got := f.delivery(t); got != "sent" {
		t.Fatalf("delivery = %q, want sent", got)
	}
}
