// Question comm-verb tests: the `question` subverbs (list / show /
// answer) against a fake /api/questions daemon, and the bare-reply
// rule (one open → answered, several open → refused with the list,
// none open or daemon unreachable → fall through).

package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// questionAnswerRecord is what the fake daemon records from a
// POST /api/questions/{id}/answer body.
type questionAnswerRecord struct {
	QuestionID     string `json:"-"`
	ChosenOptionID string `json:"chosen_option_id"`
	Quote          string `json:"quote"`
	Actor          string `json:"actor"`
	Surface        string `json:"surface"`
}

// questionRequest mirrors one loopback call to the fake daemon.
type questionRequest struct {
	method   string
	path     string
	rawquery string
	rawbody  string
}

type questionFixture struct {
	open      []map[string]any
	byID      map[string]map[string]any
	reqs      []questionRequest
	answers   []questionAnswerRecord
	answerErr int // non-zero status returned by /answer
	getErr    int // non-zero status returned by GET by-id (0 = 200)
}

func questionOption(id, label string) map[string]any {
	return map[string]any{"id": id, "label": label}
}

// questionQuestion builds one open question record.
func questionQuestion(id, label, text string, opts ...map[string]any) map[string]any {
	return map[string]any{
		"id":      id,
		"label":   label,
		"text":    text,
		"state":   "open",
		"options": opts,
	}
}

func (f *questionFixture) lastAnswer(id string) *questionAnswerRecord {
	for i := range f.answers {
		if f.answers[i].QuestionID == id {
			return &f.answers[i]
		}
	}
	return nil
}

// fakeQuestionDaemon serves the /api/questions surface from the fixture.
// It returns the daemon port and a close function.
func fakeQuestionDaemon(t *testing.T, f *questionFixture) (int, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawbody, _ := io.ReadAll(r.Body)
		f.reqs = append(f.reqs, questionRequest{
			method:   r.Method,
			path:     r.URL.Path,
			rawquery: r.URL.RawQuery,
			rawbody:  string(rawbody),
		})
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/api/questions" && r.Method == http.MethodGet:
			if r.URL.Query().Get("state") != "open" {
				t.Errorf("list request missing state=open: %q", r.URL.RawQuery)
			}
			open := f.open
			if open == nil {
				open = []map[string]any{}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"count": len(open), "questions": open})

		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/answer"):
			qid := strings.TrimSuffix(r.URL.Path, "/answer")
			qid = strings.TrimPrefix(qid, "/api/questions/")
			if f.answerErr != 0 {
				http.Error(w, "cannot store answer: boom", f.answerErr)
				return
			}
			var rec questionAnswerRecord
			if err := json.Unmarshal(rawbody, &rec); err != nil {
				t.Errorf("answer body: %v", err)
				http.Error(w, "bad answer body", http.StatusBadRequest)
				return
			}
			rec.QuestionID = qid
			f.answers = append(f.answers, rec)
			stored := map[string]any{
				"id":               "ans-1",
				"question_id":      qid,
				"actor":            rec.Actor,
				"surface":          rec.Surface,
				"chosen_option_id": rec.ChosenOptionID,
				"quote":            rec.Quote,
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"answer": stored,
				"question": map[string]any{"id": qid, "state": "answered"}})

		case strings.HasPrefix(r.URL.Path, "/api/questions/"):
			id := strings.TrimPrefix(r.URL.Path, "/api/questions/")
			if f.getErr != 0 {
				http.Error(w, "unknown question", f.getErr)
				return
			}
			q, ok := f.byID[id]
			if !ok {
				http.NotFound(w, r)
				return
			}
			var answer any = nil
			var currentID string
			if rec := f.lastAnswer(id); rec != nil {
				currentID = "ans-1"
				answer = map[string]any{
					"id":               "ans-1",
					"chosen_option_id": rec.ChosenOptionID,
					"quote":            rec.Quote,
					"actor":            rec.Actor,
					"surface":          rec.Surface,
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"question":          q,
				"answer_count":      len(f.answers),
				"current_answer_id": currentID,
				"answer":            answer,
			})

		default:
			http.NotFound(w, r)
		}
	}))
	idx := strings.LastIndex(srv.URL, ":")
	port := 0
	for i := idx + 1; i < len(srv.URL); i++ {
		port = port*10 + int(srv.URL[i]-'0')
	}
	return port, srv.Close
}

func questionRouter(t *testing.T, port int) *Router {
	t.Helper()
	r := &Router{
		hostname: "h",
		groupID:  "grp1",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
	}
	r.SetWebPort(port)
	return r
}

// questionRouterWithManager wires a real (empty) session manager so the
// fall-through default branch runs the same way production does.
func questionRouterWithManager(t *testing.T) *Router {
	t.Helper()
	dir := t.TempDir()
	sm, err := session.NewManager("h", dir, "echo", 30*time.Second)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	allowTestTemp(sm)
	sm.WithFakeTmux()
	t.Cleanup(sm.WaitBackground)
	return &Router{
		hostname: "h",
		groupID:  "grp1",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
		manager:  sm,
	}
}

// ── parse table ───────────────────────────────────────────────────────────

func TestQuestionParse(t *testing.T) {
	lv, err := parseQuestionVerb("list")
	if err != nil || lv.action != "list" {
		t.Fatalf("list: %+v %v", lv, err)
	}
	sv, err := parseQuestionVerb("show q_1")
	if err != nil || sv.action != "show" || sv.id != "q_1" {
		t.Fatalf("show: %+v %v", sv, err)
	}
	av, err := parseQuestionVerb("  answer q_1 2 why  exactly so  ")
	if err != nil || av.action != "answer" || av.id != "q_1" || av.option != "2" {
		t.Fatalf("answer: %+v %v", av, err)
	}
	if av.quote != "why  exactly so" {
		t.Errorf("quote = %q, want the remainder with inner double space kept (only the field separator dropped)", av.quote)
	}
	multiline, err := parseQuestionVerb("answer q_1 opt_b line one\n\nline three")
	if err != nil {
		t.Fatalf("multiline: %v", err)
	}
	if multiline.quote != "line one\n\nline three" {
		t.Errorf("multiline quote = %q", multiline.quote)
	}
	noquote, _ := parseQuestionVerb("answer q_1 2")
	if noquote.quote != "" {
		t.Errorf("no-quote parse: quote = %q, want empty", noquote.quote)
	}
	for _, bad := range []string{"", "  ", "show", "show a b", "answer q_1", "bogus"} {
		if _, err := parseQuestionVerb(bad); err == nil {
			t.Errorf("parse(%q): want an error", bad)
		}
	}
	if got := Parse("question list"); got.Type != CmdQuestion || got.Text != "list" {
		t.Errorf("Parse(question list) = %+v", got)
	}
}

// ── usage error ───────────────────────────────────────────────────────────

func TestQuestionUsageError(t *testing.T) {
	r := &Router{
		hostname: "h",
		groupID:  "grp1",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
	}
	for _, msg := range []string{"question", "question bogus", "question answer q_1"} {
		out := joinReplies(r.HandleTestMessage(msg))
		if !strings.Contains(out, "usage:") {
			t.Errorf("%q: reply = %q, want usage text", msg, out)
		}
	}
}

// ── list ──────────────────────────────────────────────────────────────────

func TestQuestionList(t *testing.T) {
	f := &questionFixture{
		open: []map[string]any{
			questionQuestion("q_1", "Rollback", "Roll back, or keep going?",
				questionOption("opt_a", "Roll back"), questionOption("opt_b", "Keep going")),
			questionQuestion("q_2", "Merge", "Merge now?",
				questionOption("opt_c", "Merge")),
		},
	}
	f.byID = map[string]map[string]any{"q_1": f.open[0], "q_2": f.open[1]}
	port, stop := fakeQuestionDaemon(t, f)
	defer stop()

	out := joinReplies(questionRouter(t, port).HandleTestMessage("question list"))
	if len(f.reqs) != 1 {
		t.Fatalf("daemon requests = %d, want 1 (%v)", len(f.reqs), f.reqs)
	}
	rq := f.reqs[0]
	if rq.method != "GET" || rq.path != "/api/questions" || !strings.Contains(rq.rawquery, "state=open") {
		t.Errorf("list request = %+v", rq)
	}
	for _, want := range []string{
		"1. q_1", "Rollback", "Roll back, or keep going?",
		"2. q_2", "Merge", "Merge now?",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("reply lacks %q:\n%s", want, out)
		}
	}

	// No opens → the empty list is said, not an error.
	f.open = nil
	out = joinReplies(questionRouter(t, port).HandleTestMessage("question list"))
	if !strings.Contains(out, "no open questions") {
		t.Errorf("empty list reply = %q", out)
	}
}

// ── show ──────────────────────────────────────────────────────────────────

func TestQuestionShow(t *testing.T) {
	q := questionQuestion("q_1", "Rollback", "Roll back, or keep going?",
		questionOption("opt_a", "Roll back"), questionOption("opt_b", "Keep going"))
	q["recommendation"] = map[string]any{"option_id": "opt_a", "rationale": "less risk"}
	f := &questionFixture{open: []map[string]any{q}, byID: map[string]map[string]any{"q_1": q}}
	port, stop := fakeQuestionDaemon(t, f)
	defer stop()

	out := joinReplies(questionRouter(t, port).HandleTestMessage("question show q_1"))
	if len(f.reqs) != 1 || f.reqs[0].method != "GET" || f.reqs[0].path != "/api/questions/q_1" {
		t.Fatalf("show request = %v", f.reqs)
	}
	for _, want := range []string{
		"q_1", "Rollback", "Roll back, or keep going?",
		"1. opt_a  Roll back", "2. opt_b  Keep going",
		"recommended: Roll back — less risk",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("reply lacks %q:\n%s", want, out)
		}
	}

	// After an answer, show renders the current answer's quote.
	f.answers = append(f.answers, questionAnswerRecord{
		QuestionID: "q_1", ChosenOptionID: "opt_b", Quote: "keep going; ship it",
	})
	out = joinReplies(questionRouter(t, port).HandleTestMessage("question show q_1"))
	if !strings.Contains(out, "answer (option Keep going):") || !strings.Contains(out, "keep going; ship it") {
		t.Errorf("answer not rendered:\n%s", out)
	}

	// Unknown id → the daemon 404 is surfaced.
	out = joinReplies(questionRouter(t, port).HandleTestMessage("question show q_nope"))
	if !strings.Contains(out, "not found") {
		t.Errorf("unknown id reply = %q", out)
	}
}

// ── answer ────────────────────────────────────────────────────────────────

func TestQuestionAnswerByNumber(t *testing.T) {
	q := questionQuestion("q_1", "Rollback", "Roll back, or keep going?",
		questionOption("opt_a", "Roll back"), questionOption("opt_b", "Keep going"))
	f := &questionFixture{open: []map[string]any{q}, byID: map[string]map[string]any{"q_1": q}}
	port, stop := fakeQuestionDaemon(t, f)
	defer stop()

	out := joinReplies(questionRouter(t, port).HandleTestMessage(
		"question answer q_1 2 because the deploy is broken"))
	if len(f.reqs) != 2 {
		t.Fatalf("daemon requests = %d, want 2 (resolve + POST): %v", len(f.reqs), f.reqs)
	}
	if f.reqs[0].method != "GET" || f.reqs[0].path != "/api/questions/q_1" {
		t.Errorf("resolve request = %+v", f.reqs[0])
	}
	post := f.reqs[1]
	if post.method != "POST" || post.path != "/api/questions/q_1/answer" {
		t.Fatalf("answer request = %+v", post)
	}
	if len(f.answers) != 1 {
		t.Fatalf("answers = %+v", f.answers)
	}
	rec := f.answers[0]
	if rec.ChosenOptionID != "opt_b" {
		t.Errorf("chosen option = %q, want opt_b (number 2)", rec.ChosenOptionID)
	}
	if rec.Quote != "because the deploy is broken" {
		t.Errorf("quote = %q", rec.Quote)
	}
	if rec.Surface != "comm:test" {
		t.Errorf("surface = %q, want comm:test", rec.Surface)
	}
	if rec.Actor != "test:grp1" {
		t.Errorf("actor = %q, want the sender identity test:grp1", rec.Actor)
	}
	if !strings.Contains(out, "answered") || !strings.Contains(out, "opt_b") {
		t.Errorf("reply = %q", out)
	}
}

func TestQuestionAnswerByOptionID(t *testing.T) {
	q := questionQuestion("q_1", "Rollback", "Text?",
		questionOption("opt_a", "Roll back"), questionOption("opt_b", "Keep going"))
	f := &questionFixture{open: []map[string]any{q}, byID: map[string]map[string]any{"q_1": q}}
	port, stop := fakeQuestionDaemon(t, f)
	defer stop()

	out := joinReplies(questionRouter(t, port).HandleTestMessage(
		"question answer q_1 opt_a rolling back now"))
	if len(f.answers) != 1 || f.answers[0].ChosenOptionID != "opt_a" {
		t.Fatalf("answers = %+v", f.answers)
	}
	if f.answers[0].Quote != "rolling back now" {
		t.Errorf("quote = %q", f.answers[0].Quote)
	}
	if !strings.Contains(out, "opt_a") {
		t.Errorf("reply = %q", out)
	}
}

func TestQuestionAnswerQuoteVerbatim(t *testing.T) {
	q := questionQuestion("q_1", "Rollback", "Text?", questionOption("opt_a", "Roll back"))
	f := &questionFixture{open: []map[string]any{q}, byID: map[string]map[string]any{"q_1": q}}
	port, stop := fakeQuestionDaemon(t, f)
	defer stop()

	msg := "question answer q_1 opt_a \nhello   world"
	questionRouter(t, port).HandleTestMessage(msg)
	if len(f.answers) != 1 {
		t.Fatalf("answers = %+v", f.answers)
	}
	// The quote is the text after the option token exactly as typed: the
	// pasted newline becomes the separator and is dropped, the three-inner
	// spaces stay.
	want := "hello   world"
	if f.answers[0].Quote != want {
		t.Errorf("quote = %q, want %q", f.answers[0].Quote, want)
	}
}

func TestQuestionAnswerUnknownOption(t *testing.T) {
	q := questionQuestion("q_1", "Rollback", "Text?",
		questionOption("opt_a", "Roll back"), questionOption("opt_b", "Keep going"))
	f := &questionFixture{open: []map[string]any{q}, byID: map[string]map[string]any{"q_1": q}}
	port, stop := fakeQuestionDaemon(t, f)
	defer stop()

	out := joinReplies(questionRouter(t, port).HandleTestMessage("question answer q_1 nope"))
	if len(f.answers) != 0 {
		t.Fatalf("an answer was recorded: %+v", f.answers)
	}
	if !strings.Contains(out, "opt_a") || !strings.Contains(out, "opt_b") {
		t.Errorf("refusal should name the valid options: %q", out)
	}
}

func TestQuestionAnswerFailed(t *testing.T) {
	q := questionQuestion("q_1", "Rollback", "Text?", questionOption("opt_a", "Roll back"))
	f := &questionFixture{open: []map[string]any{q}, byID: map[string]map[string]any{"q_1": q},
		answerErr: http.StatusConflict}
	port, stop := fakeQuestionDaemon(t, f)
	defer stop()

	out := joinReplies(questionRouter(t, port).HandleTestMessage("question answer q_1 1 quote"))
	if !strings.Contains(out, "409") && !strings.Contains(out, "boom") {
		t.Errorf("reply should carry the failure: %q", out)
	}

	// GET by-id failing surfaces too.
	f.getErr = http.StatusNotFound
	f.answerErr = 0
	out = joinReplies(questionRouter(t, port).HandleTestMessage("question answer q_1 1 quote"))
	if !strings.Contains(out, "404") {
		t.Errorf("reply = %q", out)
	}
}

// ── bare-reply rule ───────────────────────────────────────────────────────

func TestQuestionBareReplyOneOpenAnswers(t *testing.T) {
	q := questionQuestion("q_1", "Rollback", "Roll back, or keep going?",
		questionOption("opt_a", "Roll back"), questionOption("opt_b", "Keep going"))
	f := &questionFixture{open: []map[string]any{q}, byID: map[string]map[string]any{"q_1": q}}
	port, stop := fakeQuestionDaemon(t, f)
	defer stop()
	r := questionRouter(t, port)

	// Number first word: option 1, the rest is the quote.
	joinReplies(r.HandleTestMessage("1 rolling back, the deploy is broken"))
	if len(f.answers) != 1 || f.answers[0].ChosenOptionID != "opt_a" {
		t.Fatalf("answers = %+v, want opt_a", f.answers)
	}
	if f.answers[0].Quote != "rolling back, the deploy is broken" {
		t.Errorf("quote = %q", f.answers[0].Quote)
	}

	// Number first word, no quote.
	joinReplies(r.HandleTestMessage("2"))
	if len(f.answers) != 2 || f.answers[1].ChosenOptionID != "opt_b" || f.answers[1].Quote != "" {
		t.Fatalf("answers = %+v, want opt_b with empty quote", f.answers)
	}

	// The whole message exactly matches an option label.
	joinReplies(r.HandleTestMessage("Keep going"))
	if len(f.answers) != 3 || f.answers[2].ChosenOptionID != "opt_b" || f.answers[2].Quote != "" {
		t.Fatalf("answers = %+v, want opt_b via label", f.answers)
	}
}

func TestQuestionBareReplyMultipleOpenRefused(t *testing.T) {
	q1 := questionQuestion("q_1", "Rollback", "Roll back?",
		questionOption("opt_a", "Roll back"), questionOption("opt_b", "Keep going"))
	q2 := questionQuestion("q_2", "Merge", "Merge now?",
		questionOption("opt_c", "Merge"))
	f := &questionFixture{open: []map[string]any{q1, q2},
		byID: map[string]map[string]any{"q_1": q1, "q_2": q2}}
	port, stop := fakeQuestionDaemon(t, f)
	defer stop()
	r := questionRouter(t, port)

	out := joinReplies(r.HandleTestMessage("2"))
	if len(f.answers) != 0 {
		t.Fatalf("an answer was recorded: %+v", f.answers)
	}
	for _, want := range []string{"question answer", "q_1", "q_2"} {
		if !strings.Contains(out, want) {
			t.Errorf("refusal lacks %q:\n%s", want, out)
		}
	}

	// A label match on one of several open questions is refused too.
	out = joinReplies(r.HandleTestMessage("Roll back"))
	if len(f.answers) != 0 || !strings.Contains(out, "question answer") {
		t.Errorf("label refusal: answers=%+v out=%q", f.answers, out)
	}
}

func TestQuestionBareReplyZeroOpenFallsThrough(t *testing.T) {
	f := &questionFixture{} // no opens
	port, stop := fakeQuestionDaemon(t, f)
	defer stop()
	r := questionRouterWithManager(t)
	r.SetWebPort(port)

	// No open question (or a message that is not an option): handled by
	// the existing plain-text path. With zero sessions waiting that path
	// says nothing is to do — and crucially no answer is recorded.
	out := joinReplies(r.HandleTestMessage("1 hello there"))
	if len(f.answers) != 0 {
		t.Fatalf("an answer was recorded with no open question: %+v", f.answers)
	}
	if len(f.reqs) != 1 || f.reqs[0].path != "/api/questions" {
		t.Errorf("requests = %+v, want only the open-questions read", f.reqs)
	}
	didFallThrough := strings.Contains(out, "Nothing to do")
	if !didFallThrough && out != "" {
		t.Errorf("unexpected reply = %q", out)
	}
}

func TestQuestionBareReplyNoMatchFallsThrough(t *testing.T) {
	q := questionQuestion("q_1", "Rollback", "Roll back?",
		questionOption("opt_a", "Roll back"), questionOption("opt_b", "Keep going"))
	f := &questionFixture{open: []map[string]any{q}, byID: map[string]map[string]any{"q_1": q}}
	port, stop := fakeQuestionDaemon(t, f)
	defer stop()
	r := questionRouterWithManager(t)
	r.SetWebPort(port)

	// One question is open, but this message shapes like none of its
	// options: not consumed — it reaches the existing plain-text handling.
	out := joinReplies(r.HandleTestMessage("hello, not an answer"))
	if len(f.answers) != 0 {
		t.Fatalf("an answer was recorded: %+v", f.answers)
	}
	if strings.Contains(out, "question") {
		t.Errorf("reply must not mention the question: %q", out)
	}
}

func TestQuestionBareReplyDaemonUnreachableFallsThrough(t *testing.T) {
	// webPort set but nothing listens: the open-questions read fails and
	// the message must fall through to the existing handling, not error.
	r := questionRouterWithManager(t)
	// Find a free port with no listener.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	url := srv.URL
	srv.Close()
	idx := strings.LastIndex(url, ":")
	port := 0
	for i := idx + 1; i < len(url); i++ {
		port = port*10 + int(url[i]-'0')
	}
	r.SetWebPort(port)

	out := joinReplies(r.HandleTestMessage("1 rolling back"))
	// The daemon read failed, so this must NOT be a question answer or a
	// question refusal — whatever the plain-text path did (nothing, here
	// with no waiting sessions) is acceptable.
	if strings.Contains(out, "question") || strings.Contains(out, "failed") {
		t.Fatalf("reply = %q, want the existing plain-text handling without a question refusal", out)
	}
}
