package autonomous

// Request watch: the alert and the operator question raised when the model
// request of a worker or verifier shows no progress. The alert sink and the
// question store are fakes, so no test reaches a node, a session or a service.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/requestwatch"
)

const (
	rwTestEpisodeStart = "2026-10-31T10:00:00Z"
	rwTestSession      = "host-ab12"
	rwTestNode         = "node-a"
	rwTestModel        = "model-x"
)

// requestWatchRig is a Manager with one Automaton holding one running task, an
// alert log, a question store whose change hook feeds the decision log and the
// API as the daemon wires it, and a mutable request watch config.
type requestWatchRig struct {
	m      *Manager
	store  *QuestionStore
	alerts *holdAlertLog
	cfg    config.RequestWatchConfig
	prdID  string
	taskID string
}

func newRequestWatchRig(t *testing.T, withQuestions bool) *requestWatchRig {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("test spec", t.TempDir(), "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	m.store.WithLock(func() {
		prd.Title = "Nightly rebuild"
		prd.Story = []Story{{ID: "s1", PRDID: prd.ID, Tasks: []Task{{
			ID: "t1", StoryID: "s1", PRDID: prd.ID, Title: "Compile the parser",
			Status: TaskInProgress, SessionID: rwTestSession,
		}}}}
	})
	if err := m.store.SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	r := &requestWatchRig{m: m, alerts: &holdAlertLog{}, prdID: prd.ID, taskID: "t1"}
	m.SetAlertSink(r.alerts.sink)
	m.SetRequestWatchConfig(func() config.RequestWatchConfig { return r.cfg })
	if withQuestions {
		qs, err := NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
		if err != nil {
			t.Fatalf("NewQuestionStore: %v", err)
		}
		api := NewAPI(m)
		qs.SetChangeHook(func(c QuestionChange) {
			m.RecordQuestionChange(c)
			api.OnQuestionChange(c)
		})
		m.SetDecisionGate(qs, func() config.AutonomousQuestionsConfig {
			return config.AutonomousQuestionsConfig{Enabled: true}
		})
		r.store = qs
	}
	return r
}

// obs is the observation of the rig's task past the threshold, for an episode
// that began at start.
func (r *requestWatchRig) obs(start string) requestwatch.Observation {
	return requestwatch.Observation{
		SessionID: rwTestSession, AutomatonID: r.prdID, TaskID: r.taskID,
		Role: requestwatch.RoleWorker, Node: rwTestNode, Model: rwTestModel,
		State: requestwatch.StateIdle, LastChange: start,
		NoProgressSeconds: 31 * 60, OverThreshold: true,
	}
}

func (r *requestWatchRig) openQuestions() []Question {
	return r.store.List(QuestionFilter{State: QuestionOpen, Asker: requestWatchQuestionAsker, Limit: 100})
}

func (r *requestWatchRig) taskStatus(t *testing.T) TaskStatus {
	t.Helper()
	prd, ok := r.m.Store().GetPRD(r.prdID)
	if !ok {
		t.Fatalf("Automaton %s not found", r.prdID)
	}
	return prd.Story[0].Tasks[0].Status
}

func optionIDs(q Question) []string {
	var ids []string
	for _, o := range q.Options {
		ids = append(ids, o.ID)
	}
	return ids
}

func TestRequestWatchAlertAndQuestionRaisedOnce(t *testing.T) {
	r := newRequestWatchRig(t, true)
	o := r.obs(rwTestEpisodeStart)

	r.m.RaiseRequestWatchNoProgress(o)
	r.m.RaiseRequestWatchNoProgress(o)

	alerts := alertsTitled(r.alerts, requestWatchAlertTitle)
	if len(alerts) != 1 {
		t.Fatalf("alerts = %d, want 1", len(alerts))
	}
	if alerts[0].level != "warn" {
		t.Errorf("alert level = %q, want warn", alerts[0].level)
	}
	for _, want := range []string{"Nightly rebuild", r.prdID, "Compile the parser", "t1", "worker", rwTestNode, rwTestModel, "31 minutes", "limit 30", "Ways to act"} {
		if !strings.Contains(alerts[0].body, want) {
			t.Errorf("alert body lacks %q:\n%s", want, alerts[0].body)
		}
	}
	qs := r.openQuestions()
	if len(qs) != 1 {
		t.Fatalf("open questions = %d, want 1", len(qs))
	}
	q := qs[0]
	if q.AskedBy != "request-watch" || q.AskerKind != QuestionAskerSystem || q.Context != QuestionContextTask ||
		q.PRDID != r.prdID || q.TaskID != "t1" {
		t.Errorf("question = %+v, want a system request-watch task question for %s/t1", q, r.prdID)
	}
	for _, want := range []string{"model request", "worker", "Compile the parser", "Nightly rebuild", rwTestNode, "no progress for 31 minutes", "limit 30"} {
		if !strings.Contains(q.Text, want) {
			t.Errorf("question text lacks %q: %s", want, q.Text)
		}
	}
}

func TestRequestWatchNewEpisodeRaisesAgain(t *testing.T) {
	r := newRequestWatchRig(t, true)

	r.m.RaiseRequestWatchNoProgress(r.obs(rwTestEpisodeStart))
	// The request recovers: the open question is withdrawn and the episode is
	// forgotten. A later episode begins at a later time.
	r.m.ForgetRequestWatchEpisode(rwTestSession)
	r.m.WithdrawRequestWatchQuestions(r.prdID, "t1", "", "progress resumed")
	r.m.RaiseRequestWatchNoProgress(r.obs("2026-10-31T11:00:00Z"))

	if n := len(alertsTitled(r.alerts, requestWatchAlertTitle)); n != 2 {
		t.Errorf("alerts = %d, want 2 (one per episode)", n)
	}
	if n := len(r.openQuestions()); n != 1 {
		t.Errorf("open questions = %d, want 1 (the new episode's)", n)
	}
}

func TestRequestWatchAnsweredEpisodeNeverRaisesTwice(t *testing.T) {
	r := newRequestWatchRig(t, true)
	o := r.obs(rwTestEpisodeStart)
	r.m.RaiseRequestWatchNoProgress(o)
	q := r.openQuestions()[0]
	if _, err := r.store.Answer(q.ID, Answer{Actor: "operator", Surface: "test", ChosenOptionID: "keep_waiting"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}

	r.m.RaiseRequestWatchNoProgress(o)

	if n := len(r.openQuestions()); n != 0 {
		t.Errorf("open questions after the answer = %d, want 0: the same episode must not ask again", n)
	}
	if n := len(alertsTitled(r.alerts, requestWatchAlertTitle)); n != 1 {
		t.Errorf("alerts = %d, want 1", n)
	}
}

func TestRequestWatchAlertOffSendsNoAlert(t *testing.T) {
	r := newRequestWatchRig(t, true)
	off := false
	r.cfg.RaiseAlert = &off

	r.m.RaiseRequestWatchNoProgress(r.obs(rwTestEpisodeStart))

	if n := len(r.alerts.all()); n != 0 {
		t.Errorf("alerts = %d, want 0 with raise_alert off", n)
	}
	if n := len(r.openQuestions()); n != 1 {
		t.Errorf("open questions = %d, want 1: the question does not depend on the alert", n)
	}
}

func TestRequestWatchRaiseQuestionOffNoQuestionAlertListsOptions(t *testing.T) {
	r := newRequestWatchRig(t, true)
	off := false
	r.cfg.RaiseQuestion = &off

	r.m.RaiseRequestWatchNoProgress(r.obs(rwTestEpisodeStart))

	if n := len(r.openQuestions()); n != 0 {
		t.Errorf("open questions = %d, want 0 with raise_question off", n)
	}
	assertAlertListsOptions(t, r)
}

func TestRequestWatchQuestionChannelDisabledNoQuestionAlertListsOptions(t *testing.T) {
	r := newRequestWatchRig(t, false)

	r.m.RaiseRequestWatchNoProgress(r.obs(rwTestEpisodeStart))

	assertAlertListsOptions(t, r)

	// A store that is wired but switched off asks nothing either.
	qs, err := NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	r.m.SetDecisionGate(qs, func() config.AutonomousQuestionsConfig {
		return config.AutonomousQuestionsConfig{Enabled: false}
	})
	r.m.RaiseRequestWatchNoProgress(r.obs("2026-10-31T11:00:00Z"))
	if got := qs.List(QuestionFilter{Limit: 100}); len(got) != 0 {
		t.Errorf("questions with the channel disabled = %d, want 0", len(got))
	}
}

// assertAlertListsOptions checks the one alert names every option in plain
// words and performs nothing.
func assertAlertListsOptions(t *testing.T, r *requestWatchRig) {
	t.Helper()
	alerts := alertsTitled(r.alerts, requestWatchAlertTitle)
	if len(alerts) != 1 {
		t.Fatalf("alerts = %d, want 1", len(alerts))
	}
	for _, want := range []string{"Keep waiting", "End only the stuck worker", "Retry on a stronger backend", "Unload and reload the model on the node", "performs nothing"} {
		if !strings.Contains(alerts[0].body, want) {
			t.Errorf("alert body lacks %q:\n%s", want, alerts[0].body)
		}
	}
}

func TestRequestWatchOptionsFollowConfig(t *testing.T) {
	// Unset: all four, keep_waiting first.
	r := newRequestWatchRig(t, true)
	r.m.RaiseRequestWatchNoProgress(r.obs(rwTestEpisodeStart))
	got := optionIDs(r.openQuestions()[0])
	want := []string{"keep_waiting", "end_worker", "retry_other_backend", "unload_reload_model"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("unset options = %v, want %v", got, want)
	}

	// Explicit list: keep_waiting plus the listed.
	r2 := newRequestWatchRig(t, true)
	r2.cfg.RemediationOptions = []string{"unload_reload_model", "end_worker"}
	r2.m.RaiseRequestWatchNoProgress(r2.obs(rwTestEpisodeStart))
	got = optionIDs(r2.openQuestions()[0])
	want = []string{"keep_waiting", "unload_reload_model", "end_worker"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("explicit options = %v, want %v", got, want)
	}

	// Explicit empty list: only keep_waiting, which is not enough for a
	// question, so the alert alone reports the situation.
	r3 := newRequestWatchRig(t, true)
	r3.cfg.RemediationOptions = []string{}
	r3.m.RaiseRequestWatchNoProgress(r3.obs(rwTestEpisodeStart))
	if n := len(r3.openQuestions()); n != 0 {
		t.Errorf("open questions with keep_waiting only = %d, want 0", n)
	}
	alerts := alertsTitled(r3.alerts, requestWatchAlertTitle)
	if len(alerts) != 1 || !strings.Contains(alerts[0].body, "Keep waiting") {
		t.Errorf("alerts = %+v, want one that offers keeping on waiting", alerts)
	}
}

func TestRequestWatchOptionIDsMatchConfig(t *testing.T) {
	for _, id := range config.RequestWatchAllOptions() {
		opt, ok := requestWatchOptionFor(id)
		if !ok || opt.ID != id || opt.Label == "" {
			t.Errorf("option %q: %+v ok=%v, want a labelled option", id, opt, ok)
		}
		if id != config.RequestWatchOptionKeepWaiting && !strings.Contains(opt.Tradeoffs, "only records your choice and performs nothing") {
			t.Errorf("option %q trade-off must say this release only records the choice: %q", id, opt.Tradeoffs)
		}
	}
}

func TestRequestWatchKeepWaitingIsDefaultAndNotBlocking(t *testing.T) {
	r := newRequestWatchRig(t, true)
	r.cfg.RemediationOptions = []string{"end_worker"}

	r.m.RaiseRequestWatchNoProgress(r.obs(rwTestEpisodeStart))

	q := r.openQuestions()[0]
	if q.Default != "keep_waiting" {
		t.Errorf("default = %q, want keep_waiting", q.Default)
	}
	if q.Blocking {
		t.Errorf("the question must not be blocking")
	}
	if st := r.taskStatus(t); st != TaskInProgress {
		t.Errorf("task status = %q, want in_progress: raising a question parks nothing", st)
	}
}

func TestRequestWatchAnswerRecordedNothingPerformed(t *testing.T) {
	r := newRequestWatchRig(t, true)
	r.m.RaiseRequestWatchNoProgress(r.obs(rwTestEpisodeStart))
	q := r.openQuestions()[0]
	before := r.taskStatus(t)

	a, err := r.store.Answer(q.ID, Answer{Actor: "operator", Surface: "test", ChosenOptionID: "end_worker"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}

	prd, _ := r.m.Store().GetPRD(r.prdID)
	var recorded []Decision
	for _, d := range prd.Decisions {
		if d.Kind == "request_watch_answer" {
			recorded = append(recorded, d)
		}
	}
	if len(recorded) != 1 {
		t.Fatalf("request_watch_answer decisions = %d, want 1", len(recorded))
	}
	d := recorded[0]
	if d.Actor != "operator" || d.QuestionID != q.ID || d.AnswerID != a.ID {
		t.Errorf("decision = %+v, want operator, question %s, answer %s", d, q.ID, a.ID)
	}
	for _, want := range []string{"end_worker", "End only the stuck worker", "no remediation"} {
		if !strings.Contains(d.Note, want) {
			t.Errorf("decision note lacks %q: %s", want, d.Note)
		}
	}
	if after := r.taskStatus(t); after != before {
		t.Errorf("task status changed from %q to %q: an answer only records the choice", before, after)
	}
	// Recording the same change again adds nothing.
	r.m.recordRequestWatchAnswer(QuestionChange{Kind: QuestionChangeAnswered, Question: q, Answer: &a})
	prd, _ = r.m.Store().GetPRD(r.prdID)
	n := 0
	for _, d := range prd.Decisions {
		if d.Kind == "request_watch_answer" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("request_watch_answer decisions after a repeat = %d, want 1", n)
	}
}

func TestRequestWatchAnswerOfOtherQuestionRecordsNothing(t *testing.T) {
	r := newRequestWatchRig(t, true)
	q := sampleQuestion("Unrelated?")
	q.Context = QuestionContextPrd
	q.PRDID = r.prdID
	q.AskedBy = "someone"
	q.AskerKind = QuestionAskerSupervisor
	got, err := r.store.Raise(q)
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	if _, err := r.store.Answer(got.ID, Answer{Actor: "operator", ChosenOptionID: "first"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	prd, _ := r.m.Store().GetPRD(r.prdID)
	for _, d := range prd.Decisions {
		if d.Kind == "request_watch_answer" {
			t.Errorf("unexpected request_watch_answer decision: %+v", d)
		}
	}
}

func TestRequestWatchRecoveredWithdrawsOpenQuestion(t *testing.T) {
	r := newRequestWatchRig(t, true)
	r.m.RaiseRequestWatchNoProgress(r.obs(rwTestEpisodeStart))
	if n := len(r.openQuestions()); n != 1 {
		t.Fatalf("open questions = %d, want 1", n)
	}

	r.m.WithdrawRequestWatchQuestions(r.prdID, "t1", "", "the model request is making progress again")

	if n := len(r.openQuestions()); n != 0 {
		t.Errorf("open questions after recovery = %d, want 0", n)
	}
	withdrawn := r.store.List(QuestionFilter{State: QuestionWithdrawn, Asker: requestWatchQuestionAsker, Limit: 100})
	if len(withdrawn) != 1 {
		t.Errorf("withdrawn questions = %d, want 1", len(withdrawn))
	}
	if st := r.taskStatus(t); st != TaskInProgress {
		t.Errorf("task status = %q, want in_progress: withdrawing changes no task", st)
	}
}

func TestRequestWatchSessionEndWithdrawsOnlyThatTask(t *testing.T) {
	r := newRequestWatchRig(t, true)
	r.m.RaiseRequestWatchNoProgress(r.obs(rwTestEpisodeStart))
	other := r.obs(rwTestEpisodeStart)
	other.SessionID, other.TaskID = "host-cd34", "t2"
	r.m.RaiseRequestWatchNoProgress(other)
	if n := len(r.openQuestions()); n != 2 {
		t.Fatalf("open questions = %d, want 2", n)
	}

	r.m.WithdrawRequestWatchQuestions(r.prdID, "t1", "autonomous", "the session ended")

	left := r.openQuestions()
	if len(left) != 1 || left[0].TaskID != "t2" {
		t.Errorf("open questions = %+v, want only the one for t2", left)
	}
	// With no store wired a withdrawal does nothing and does not panic.
	bare := newRequestWatchRig(t, false)
	bare.m.WithdrawRequestWatchQuestions(bare.prdID, "t1", "", "ended")
}

func TestRequestWatchAutomatonWithoutTitleFallsBackToID(t *testing.T) {
	r := newRequestWatchRig(t, true)
	prd, _ := r.m.Store().GetPRD(r.prdID)
	r.m.store.WithLock(func() { prd.Title = "" })
	if err := r.m.store.SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	r.m.RaiseRequestWatchNoProgress(r.obs(rwTestEpisodeStart))

	alerts := alertsTitled(r.alerts, requestWatchAlertTitle)
	if len(alerts) != 1 || !strings.Contains(alerts[0].body, "Automaton: "+r.prdID+" ("+r.prdID+")") {
		t.Errorf("alert body does not fall back to the id: %+v", alerts)
	}
	if q := r.openQuestions(); len(q) != 1 || !strings.Contains(q[0].Text, "Automaton "+r.prdID+" ") {
		t.Errorf("question text does not fall back to the id: %+v", q)
	}
}

func TestRequestWatchNoAlertSinkStillAsks(t *testing.T) {
	r := newRequestWatchRig(t, true)
	r.m.SetAlertSink(nil)

	r.m.RaiseRequestWatchNoProgress(r.obs(rwTestEpisodeStart))

	if n := len(r.openQuestions()); n != 1 {
		t.Errorf("open questions = %d, want 1 without an alert sink", n)
	}
}

// A threshold of one minute reads "1 minute", never "1 minutes".
func TestRequestWatchMinutesTextSingular(t *testing.T) {
	cases := map[int]string{0: "0 minutes", 1: "1 minute", 2: "2 minutes", 31: "31 minutes"}
	for n, want := range cases {
		if got := requestWatchMinutesText(n); got != want {
			t.Errorf("requestWatchMinutesText(%d) = %q, want %q", n, got, want)
		}
	}
}
