// question_notify_test.go — the messaging-group leg of question-raise
// notification (Router.NotifyQuestionRaised): the router's backend
// receives exactly one send, and that text names the question id, lists
// the options as a numbered list, and points the operator at the three
// answer routes (the question answer chat verb, the REST answer
// endpoint, and the Automaton PWA).

package router

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// questionNotifySample is the question the tests raise.
func questionNotifySample() autonomous.Question {
	return autonomous.Question{
		ID:       "q-abc123",
		Label:    "decision",
		Text:     "Cap the retry backoff at five minutes?",
		Options:  []autonomous.QuestionOption{{ID: "yes", Label: "Yes, cap it"}, {ID: "no", Label: "No"}},
		Priority: autonomous.QuestionPriorityHigh,
	}
}

// newQuestionNotifyRouter builds a Router over the capturing backend
// already used by the package's other send-path tests, and returns it
// with a mutex-guarded accessor for the captured texts (in send order).
func newQuestionNotifyRouter(t *testing.T) (*Router, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var got []string
	r := &Router{
		hostname: "dw",
		groupID:  "group-1",
		backend: &captureBackend{
			name: "test",
			capture: func(s string) {
				mu.Lock()
				got = append(got, s)
				mu.Unlock()
			},
		},
	}
	return r, func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := make([]string, len(got))
		copy(out, got)
		return out
	}
}

// waitRouterSends waits for at least want captured sends (the send path
// is async by design — r.send enqueues a goroutine).
func waitRouterSends(t *testing.T, want int, got func() []string) []string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if msgs := got(); len(msgs) >= want {
			return msgs
		}
		time.Sleep(5 * time.Millisecond)
	}
	return got()
}

// TestQuestionRaiseNotify_RouterReceivesExactlyOne — one raise lands as
// exactly one group send (no double-send, no zero-send).
func TestQuestionRaiseNotify_RouterReceivesExactlyOne(t *testing.T) {
	r, got := newQuestionNotifyRouter(t)
	q := questionNotifySample()

	r.NotifyQuestionRaised(q)
	msgs := waitRouterSends(t, 1, got)
	if len(msgs) == 0 {
		t.Fatal("backend captured no send; the raise must reach the group")
	}
	if len(msgs) != 1 {
		t.Fatalf("backend captured %d send(s) for one raise, want exactly one: %q", len(msgs), msgs)
	}
}

// TestQuestionRaiseNotify_TextNamesIDNumberedOptionsAndAnswerRoutes — the
// text carries the question id and label, a high-priority marker, the
// options as a numbered list, and points at all three answer routes: the
// question answer chat verb, the REST endpoint, and the Automaton PWA.
func TestQuestionRaiseNotify_TextNamesIDNumberedOptionsAndAnswerRoutes(t *testing.T) {
	r, got := newQuestionNotifyRouter(t)
	q := questionNotifySample()

	r.NotifyQuestionRaised(q)
	msgs := waitRouterSends(t, 1, got)
	text := msgs[0]

	for _, want := range []string{
		"[dw]",
		q.ID,
		q.Label,
		"high priority",
		q.Text,
		"1. Yes, cap it",
		"2. No",
		"question answer " + q.ID + " <option number> followed by your words",
		"POST /api/questions/" + q.ID + "/answer",
		"Automaton PWA",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text missing %q:\n%s", want, text)
		}
	}
}

// TestQuestionRaiseNotice_NamesQuestionAnswerVerb — the closing line names
// the question answer chat verb (option number followed by words, with a
// bare option number when this is the only open question), and a question
// raised without options still renders without a numbered list.
func TestQuestionRaiseNotice_NamesQuestionAnswerVerb(t *testing.T) {
	r, got := newQuestionNotifyRouter(t)
	q := questionNotifySample()

	r.NotifyQuestionRaised(q)
	text := waitRouterSends(t, 1, got)[0]

	for _, want := range []string{
		"question answer " + q.ID + " <option number> followed by your words",
		"a bare option number when this is the only open question",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text missing %q:\n%s", want, text)
		}
	}

	q.Options = nil
	r.NotifyQuestionRaised(q)
	msgs := waitRouterSends(t, 2, got)
	if len(msgs) < 2 {
		t.Fatalf("second raise did not reach the group; got %d send(s)", len(msgs))
	}
	noOpts := msgs[1]
	if strings.Contains(noOpts, "Options:") {
		t.Fatalf("text lists options for a question without any:\n%s", noOpts)
	}
	if !strings.Contains(noOpts, "question answer "+q.ID) {
		t.Fatalf("text for a question without options does not name the question answer verb:\n%s", noOpts)
	}
}

// TestQuestionRaiseNotify_TextWithoutOptions — a question raised without
// options (open text) still gets the id and the answer routes, and just
// drops the options list.
func TestQuestionRaiseNotify_TextWithoutOptions(t *testing.T) {
	r, got := newQuestionNotifyRouter(t)
	q := questionNotifySample()
	q.Options = nil
	q.Priority = ""

	r.NotifyQuestionRaised(q)
	msgs := waitRouterSends(t, 1, got)
	text := msgs[0]

	if !strings.Contains(text, q.ID) || !strings.Contains(text, q.Text) {
		t.Fatalf("text missing id or question text:\n%s", text)
	}
	if strings.Contains(text, "Options:") {
		t.Fatalf("text lists options for a question without any:\n%s", text)
	}
	if strings.Contains(text, "high priority") {
		t.Fatalf("text marks a non-high question as high:\n%s", text)
	}
}
