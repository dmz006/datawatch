package autonomous

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// fwRig is a manager with a fake git, an open operator question channel and
// an API whose question hook routes answers back to the Automaton.
type fwRig struct {
	h     *coordHarness
	git   *aumGit
	api   *API
	store *QuestionStore
	at    time.Time
}

func newFWRig(t *testing.T, git *aumGit, n int) *fwRig {
	t.Helper()
	h, _, at := aumRig(t, git, n)
	api := NewAPI(h.m)
	store, err := NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	store.SetChangeHook(func(c QuestionChange) { api.OnQuestionChange(c) })
	h.m.SetDecisionGate(store, func() config.AutonomousQuestionsConfig {
		return config.AutonomousQuestionsConfig{Enabled: true}
	})
	return &fwRig{h: h, git: git, api: api, store: store, at: at}
}

// finish makes record i a finished Automaton on branch with all tasks done,
// last touched at updated.
func (r *fwRig) finish(t *testing.T, i int, status PRDStatus, branch string, updated time.Time) string {
	t.Helper()
	p, ok := r.h.m.Store().GetPRD(r.h.prds[i].ID)
	if !ok {
		t.Fatal("prd missing")
	}
	p.Status = status
	p.Title = "Work " + p.ID
	p.Git.Branch = branch
	p.Git.BaseBranch = "main"
	p.Story = []Story{{Tasks: []Task{{ID: "t1", Title: "Build it", Status: TaskCompleted}}}}
	p.Decisions = append(p.Decisions, Decision{At: updated, Kind: "approve", Actor: "dave"})
	if err := r.h.m.Store().SavePRD(p); err != nil {
		t.Fatal(err)
	}
	r.touch(t, p.ID, updated)
	return p.ID
}

// touch sets the record's update time (SavePRD stamps it, so write it after).
func (r *fwRig) touch(t *testing.T, id string, at time.Time) {
	t.Helper()
	p, _ := r.h.m.Store().GetPRD(id)
	r.h.m.Store().WithLock(func() { p.UpdatedAt = at })
}

func (r *fwRig) get(t *testing.T, id string) *PRD {
	t.Helper()
	p, ok := r.h.m.Store().GetPRD(id)
	if !ok {
		t.Fatalf("prd %s missing", id)
	}
	return p
}

func (r *fwRig) openQuestions(id string) []Question {
	return r.store.List(QuestionFilter{State: QuestionOpen, Asker: finishedWorkAsker, PRDID: id, Limit: 100})
}

func answerQ(t *testing.T, r *fwRig, q Question, option string) {
	t.Helper()
	if _, err := r.store.Answer(q.ID, Answer{Actor: "dave", Surface: "test", ChosenOptionID: option, Quote: "answered " + option}); err != nil {
		t.Fatalf("answer: %v", err)
	}
}

// ---- the archive and cancel guard ----

func TestArchiveRefusesFinishedWorkNotOnMain(t *testing.T) {
	git := &aumGit{ahead: map[string]string{"work/a": "3\n"}}
	r := newFWRig(t, git, 1)
	id := r.finish(t, 0, PRDCompleted, "work/a", r.at.Add(-time.Hour))

	_, err := r.h.m.ArchiveGuarded(id, "operator")
	if err == nil {
		t.Fatal("Archive of finished work with 3 commits not on main must be refused without a decision")
	}
	if !strings.Contains(err.Error(), "3 commit") || !strings.Contains(err.Error(), "work/a") {
		t.Errorf("refusal should name the branch and the commit count: %v", err)
	}
	if st := r.get(t, id).Status; st != PRDCompleted {
		t.Errorf("status after refused archive = %s, want completed", st)
	}
}

func TestArchiveWithKeepDecisionRecordsDatedDecisionAndArchives(t *testing.T) {
	git := &aumGit{ahead: map[string]string{"work/a": "3\n"}}
	r := newFWRig(t, git, 1)
	r.h.m.now = func() time.Time { return r.at }
	id := r.finish(t, 0, PRDCompleted, "work/a", r.at.Add(-time.Hour))

	if _, err := r.h.m.ArchiveDecided(id, "operator", "keep", "wanted for the 10.1 branch"); err != nil {
		t.Fatalf("ArchiveDecided keep: %v", err)
	}
	p := r.get(t, id)
	if p.Status != PRDArchived {
		t.Fatalf("status = %s, want archived", p.Status)
	}
	if p.Landing == nil || p.Landing.State != LandingKept || p.Landing.ReaskAt == nil {
		t.Fatalf("landing = %+v, want kept with a re-ask date", p.Landing)
	}
	want := r.at.AddDate(0, 0, FinishedWorkReaskDays)
	if !p.Landing.ReaskAt.Equal(want) {
		t.Errorf("re-ask at %v, want %v", p.Landing.ReaskAt, want)
	}
}

func TestArchiveWithDropDecisionArchives(t *testing.T) {
	git := &aumGit{ahead: map[string]string{"work/a": "2\n"}}
	r := newFWRig(t, git, 1)
	id := r.finish(t, 0, PRDCompleted, "work/a", r.at.Add(-time.Hour))
	if _, err := r.h.m.ArchiveDecided(id, "operator", "drop", "superseded"); err != nil {
		t.Fatalf("ArchiveDecided drop: %v", err)
	}
	p := r.get(t, id)
	if p.Status != PRDArchived || p.Landing == nil || p.Landing.State != LandingDropped {
		t.Errorf("status %s landing %+v, want archived and dropped", p.Status, p.Landing)
	}
}

func TestArchiveDecidedNeedsAKnownDecisionAndAReason(t *testing.T) {
	git := &aumGit{ahead: map[string]string{"work/a": "2\n"}}
	r := newFWRig(t, git, 1)
	id := r.finish(t, 0, PRDCompleted, "work/a", r.at.Add(-time.Hour))
	if _, err := r.h.m.ArchiveDecided(id, "operator", "whatever", "x"); err == nil {
		t.Error("an unknown decision must be refused")
	}
	if _, err := r.h.m.ArchiveDecided(id, "operator", "keep", ""); err == nil {
		t.Error("a decision without a reason must be refused")
	}
	if st := r.get(t, id).Status; st != PRDCompleted {
		t.Errorf("status = %s after refused calls, want completed", st)
	}
}

func TestArchiveAllowedWhenNothingIsUnmerged(t *testing.T) {
	git := &aumGit{ahead: map[string]string{"work/m": "0\n"}, gone: map[string]bool{"work/g": true}}
	r := newFWRig(t, git, 3)
	merged := r.finish(t, 0, PRDCompleted, "work/m", r.at)
	gone := r.finish(t, 1, PRDCompleted, "work/g", r.at)
	none := r.finish(t, 2, PRDCompleted, "", r.at)
	for _, id := range []string{merged, gone, none} {
		if _, err := r.h.m.ArchiveGuarded(id, "operator"); err != nil {
			t.Errorf("archive %s: %v", id, err)
		}
	}
}

func TestArchiveAllowedWhenShippedRecordNamesTheWork(t *testing.T) {
	git := &aumGit{ahead: map[string]string{"work/a": "4\n"}}
	r := newFWRig(t, git, 1)
	id := r.finish(t, 0, PRDCompleted, "work/a", r.at)
	p := r.get(t, id)
	p.MergedInRelease, p.MergeCommit = "9.183.0", "abc1234"
	if err := r.h.m.Store().SavePRD(p); err != nil {
		t.Fatal(err)
	}
	if _, err := r.h.m.ArchiveGuarded(id, "operator"); err != nil {
		t.Errorf("a record with a shipped record is delivered, archive must pass: %v", err)
	}
}

func TestArchiveKeepsRecordWhenGitCannotTell(t *testing.T) {
	git := &aumGit{failOn: map[string]bool{"work/e": true}}
	r := newFWRig(t, git, 1)
	id := r.finish(t, 0, PRDCompleted, "work/e", r.at)
	if _, err := r.h.m.ArchiveGuarded(id, "operator"); err == nil {
		t.Error("an unknown branch state must refuse the archive, never guess")
	}
}

func TestGuardModes(t *testing.T) {
	for _, tc := range []struct {
		mode      string
		wantAllow bool
	}{{"shadow", true}, {"off", true}, {"on", false}, {"", false}} {
		git := &aumGit{ahead: map[string]string{"work/a": "3\n"}}
		r := newFWRig(t, git, 1)
		cfg := r.h.m.Config()
		cfg.FinishedWork = tc.mode
		r.h.m.SetConfig(cfg)
		id := r.finish(t, 0, PRDCompleted, "work/a", r.at)
		_, err := r.h.m.ArchiveGuarded(id, "operator")
		if (err == nil) != tc.wantAllow {
			t.Errorf("mode %q: archive err = %v, want allowed=%v", tc.mode, err, tc.wantAllow)
		}
	}
}

func TestShadowModeStillRecordsTheRefusalInTheLedger(t *testing.T) {
	git := &aumGit{ahead: map[string]string{"work/a": "3\n"}}
	r := newFWRig(t, git, 1)
	cfg := r.h.m.Config()
	cfg.FinishedWork = "shadow"
	r.h.m.SetConfig(cfg)
	id := r.finish(t, 0, PRDCompleted, "work/a", r.at)
	if _, err := r.h.m.ArchiveGuarded(id, "operator"); err != nil {
		t.Fatalf("shadow must allow: %v", err)
	}
	var found bool
	for _, e := range r.h.m.RefusalLedger().List() {
		if e.Automaton == id && e.Step == "archive" {
			found = true
		}
	}
	if !found {
		t.Error("shadow mode must record what it would have refused in the refusal ledger")
	}
}

func TestCancelRefusesCommitsNotOnMainWithoutADecision(t *testing.T) {
	git := &aumGit{ahead: map[string]string{"work/a": "5\n"}}
	r := newFWRig(t, git, 1)
	id := r.finish(t, 0, PRDRunning, "work/a", r.at)
	if err := r.api.Cancel(id); err == nil {
		t.Fatal("Cancel of an Automaton with 5 commits not on main must be refused without a decision")
	}
	if st := r.get(t, id).Status; st != PRDRunning {
		t.Errorf("status after refused cancel = %s, want running", st)
	}
	if err := r.api.CancelDecided(id, "keep", "stopping the run, branch is for review"); err != nil {
		t.Fatalf("CancelDecided: %v", err)
	}
	p := r.get(t, id)
	if p.Status != PRDCancelled || p.Landing == nil || p.Landing.State != LandingKept {
		t.Errorf("status %s landing %+v, want cancelled with a kept landing", p.Status, p.Landing)
	}
}

func TestCancelWithNothingUnmergedStillWorks(t *testing.T) {
	git := &aumGit{ahead: map[string]string{"work/m": "0\n"}}
	r := newFWRig(t, git, 2)
	a := r.finish(t, 0, PRDRunning, "work/m", r.at)
	b := r.finish(t, 1, PRDRunning, "", r.at)
	for _, id := range []string{a, b} {
		if err := r.api.Cancel(id); err != nil {
			t.Errorf("cancel %s: %v", id, err)
		}
	}
}

// ---- the sweep, the digest and the questions ----

func TestSweepAsksAboutFinishedWorkOlderThanADay(t *testing.T) {
	git := &aumGit{ahead: map[string]string{"work/old": "7\n", "work/new": "2\n"}}
	r := newFWRig(t, git, 2)
	old := r.finish(t, 0, PRDCompleted, "work/old", r.at.Add(-30*time.Hour))
	young := r.finish(t, 1, PRDCompleted, "work/new", r.at.Add(-2*time.Hour))

	pass := r.h.m.RunFinishedWorkSweep(context.Background(), r.at)
	if pass.Found != 1 || pass.Asked != 1 || pass.Younger != 1 {
		t.Fatalf("pass = %+v, want 1 found, 1 asked, 1 younger", pass)
	}
	qs := r.openQuestions(old)
	if len(qs) != 1 {
		t.Fatalf("open questions for the old record = %d, want 1", len(qs))
	}
	if !strings.Contains(qs[0].Text, "7 commit") || !strings.Contains(qs[0].Text, "dave") {
		t.Errorf("question must name the work and the owner: %q", qs[0].Text)
	}
	if len(r.openQuestions(young)) != 0 {
		t.Error("work younger than 24 hours must not be asked about yet")
	}
	f := r.h.m.SweepFindings()
	if len(f) != 1 || f[0].Automaton != old || f[0].Owner != "dave" || f[0].QuestionID != qs[0].ID || f[0].Cause != FinishedWorkNotMerged {
		t.Errorf("findings = %+v", f)
	}
	git.assertReadOnly(t)
}

func TestSweepRaisesTheQuestionOnceAcrossPasses(t *testing.T) {
	git := &aumGit{ahead: map[string]string{"work/old": "7\n"}}
	r := newFWRig(t, git, 1)
	id := r.finish(t, 0, PRDCompleted, "work/old", r.at.Add(-30*time.Hour))
	for i := 0; i < 4; i++ {
		r.h.m.RunFinishedWorkSweep(context.Background(), r.at.Add(time.Duration(i)*time.Minute))
	}
	if n := len(r.openQuestions(id)); n != 1 {
		t.Errorf("open questions after 4 passes = %d, want 1", n)
	}
	if n := len(r.h.m.SweepFindings()); n != 1 {
		t.Errorf("findings after 4 passes = %d, want 1 (a finding is updated, not repeated)", n)
	}
}

func TestSweepShadowAsksNothingAndChangesNothing(t *testing.T) {
	git := &aumGit{ahead: map[string]string{"work/old": "7\n"}}
	r := newFWRig(t, git, 1)
	cfg := r.h.m.Config()
	cfg.FinishedWork = "shadow"
	r.h.m.SetConfig(cfg)
	id := r.finish(t, 0, PRDCompleted, "work/old", r.at.Add(-30*time.Hour))
	before := len(r.get(t, id).Decisions)
	pass := r.h.m.RunFinishedWorkSweep(context.Background(), r.at)
	if pass.Found != 1 || pass.Asked != 0 {
		t.Errorf("shadow pass = %+v, want 1 found and 0 asked", pass)
	}
	if len(r.openQuestions(id)) != 0 || len(r.get(t, id).Decisions) != before {
		t.Error("shadow must not ask or write decisions")
	}
	if len(r.h.m.SweepFindings()) != 1 {
		t.Error("shadow must still record the finding")
	}
}

func TestSweepOffDoesNothing(t *testing.T) {
	git := &aumGit{ahead: map[string]string{"work/old": "7\n"}}
	r := newFWRig(t, git, 1)
	cfg := r.h.m.Config()
	cfg.FinishedWork = "off"
	r.h.m.SetConfig(cfg)
	r.finish(t, 0, PRDCompleted, "work/old", r.at.Add(-30*time.Hour))
	pass := r.h.m.RunFinishedWorkSweep(context.Background(), r.at)
	if pass.Examined != 0 || len(git.calls) != 0 {
		t.Errorf("off pass = %+v with %d git calls, want none", pass, len(git.calls))
	}
}

func TestSweepDropsTheFindingWhenTheWorkLands(t *testing.T) {
	git := &aumGit{ahead: map[string]string{"work/old": "7\n"}}
	r := newFWRig(t, git, 1)
	id := r.finish(t, 0, PRDCompleted, "work/old", r.at.Add(-30*time.Hour))
	r.h.m.RunFinishedWorkSweep(context.Background(), r.at)
	git.ahead["work/old"] = "0\n" // merged by hand
	r.h.m.RunFinishedWorkSweep(context.Background(), r.at.Add(time.Hour))
	if n := len(r.h.m.SweepFindings()); n != 0 {
		t.Errorf("findings after the work landed = %d, want 0", n)
	}
	if n := len(r.openQuestions(id)); n != 0 {
		t.Errorf("open questions after the work landed = %d, want 0 (withdrawn)", n)
	}
}

func TestCodeCompleteDocsStalledIsOfferedForLandingNotLeftUnowned(t *testing.T) {
	git := &aumGit{ahead: map[string]string{"work/c": "12\n"}}
	r := newFWRig(t, git, 1)
	id := r.finish(t, 0, PRDRunning, "work/c", r.at.Add(-48*time.Hour))
	p := r.get(t, id)
	p.Story = []Story{{Tasks: []Task{
		{ID: "t1", Title: "Implement the guard", Status: TaskCompleted},
		{ID: "t2", Title: "Tests for the guard", Status: TaskCompleted},
		{ID: "t3", Title: "Docs: how-to and definitions", Status: TaskFailed},
		{ID: "t4", Title: "Final gate", Kind: "final_gate", Status: TaskPending},
	}}}
	if err := r.h.m.Store().SavePRD(p); err != nil {
		t.Fatal(err)
	}
	r.touch(t, id, r.at.Add(-48*time.Hour))

	pass := r.h.m.RunFinishedWorkSweep(context.Background(), r.at)
	if pass.Found != 1 {
		t.Fatalf("pass = %+v, want the running record found", pass)
	}
	f := r.h.m.SweepFindings()
	if len(f) != 1 || f[0].Cause != FinishedWorkCodeDone {
		t.Fatalf("findings = %+v, want cause %s", f, FinishedWorkCodeDone)
	}
	qs := r.openQuestions(id)
	if len(qs) != 1 || !strings.Contains(qs[0].Text, "2 other task") {
		t.Fatalf("questions = %+v, want one that names the 2 remaining tasks", qs)
	}
	// The remaining tasks are left exactly as they were: they stay
	// separately retryable.
	after := r.get(t, id)
	if after.Story[0].Tasks[2].Status != TaskFailed || after.Story[0].Tasks[3].Status != TaskPending || after.Status != PRDRunning {
		t.Error("the sweep must not change task or Automaton status")
	}
}

func TestACodeTaskStillOpenIsNotOfferedForLanding(t *testing.T) {
	git := &aumGit{ahead: map[string]string{"work/c": "12\n"}}
	r := newFWRig(t, git, 1)
	id := r.finish(t, 0, PRDRunning, "work/c", r.at.Add(-48*time.Hour))
	p := r.get(t, id)
	p.Story = []Story{{Tasks: []Task{
		{ID: "t1", Title: "Implement the guard", Status: TaskCompleted},
		{ID: "t2", Title: "Implement the sweep", Status: TaskInProgress},
	}}}
	if err := r.h.m.Store().SavePRD(p); err != nil {
		t.Fatal(err)
	}
	r.touch(t, id, r.at.Add(-48*time.Hour))
	if pass := r.h.m.RunFinishedWorkSweep(context.Background(), r.at); pass.Found != 0 {
		t.Errorf("pass = %+v, a record with a code task still open is not finished work", pass)
	}
}

func TestLeaveAloneAnswerCreatesDatedDecisionAndIsAskedAgainOnThatDate(t *testing.T) {
	git := &aumGit{ahead: map[string]string{"work/old": "7\n"}}
	r := newFWRig(t, git, 1)
	r.h.m.now = func() time.Time { return r.at }
	id := r.finish(t, 0, PRDCompleted, "work/old", r.at.Add(-30*time.Hour))
	r.h.m.RunFinishedWorkSweep(context.Background(), r.at)
	q := mustOpenQ(t, r, id)
	answerQ(t, r, q, FinishedWorkOptionLeave)

	p := r.get(t, id)
	if p.Landing == nil || p.Landing.State != LandingKept || p.Landing.ReaskAt == nil {
		t.Fatalf("landing = %+v, want kept with a re-ask date", p.Landing)
	}
	reask := *p.Landing.ReaskAt
	if !reask.Equal(r.at.AddDate(0, 0, FinishedWorkReaskDays)) {
		t.Errorf("re-ask %v, want %d days out", reask, FinishedWorkReaskDays)
	}
	var d *Decision
	for i := range p.Decisions {
		if p.Decisions[i].Kind == "leave_alone" {
			d = &p.Decisions[i]
		}
	}
	if d == nil || !strings.Contains(d.Note, reask.Format("2006-01-02")) || d.Actor != "dave" && d.Actor != "operator" {
		t.Fatalf("decision = %+v, want a leave_alone decision that names the re-ask date", d)
	}

	// Before the date: nothing is asked. On the date: asked again, once.
	r.h.m.RunFinishedWorkSweep(context.Background(), reask.Add(-time.Hour))
	if len(r.openQuestions(id)) != 0 {
		t.Error("asked again before the re-ask date")
	}
	pass := r.h.m.RunFinishedWorkSweep(context.Background(), reask.Add(time.Minute))
	if pass.Reasked != 1 || len(r.openQuestions(id)) != 1 {
		t.Errorf("pass = %+v, open = %d, want one re-ask on the date", pass, len(r.openQuestions(id)))
	}
	if f := r.h.m.SweepFindings(); len(f) != 1 || f[0].Cause != FinishedWorkReask {
		t.Errorf("findings = %+v, want one %s finding", f, FinishedWorkReask)
	}
}

func TestBranchRefreshLeaveAloneAnswerAlsoGetsADatedDecision(t *testing.T) {
	git := &aumGit{ahead: map[string]string{"work/old": "7\n"}}
	r := newFWRig(t, git, 1)
	r.h.m.now = func() time.Time { return r.at }
	id := r.finish(t, 0, PRDCompleted, "work/old", r.at.Add(-30*time.Hour))
	if err := r.h.m.ApplyBranchRefreshAnswer(id, BranchRefreshOptionLeave); err != nil {
		t.Fatal(err)
	}
	p := r.get(t, id)
	if p.Landing == nil || p.Landing.ReaskAt == nil {
		t.Fatalf("landing = %+v, want a re-ask date after leave the branch alone", p.Landing)
	}
	var found bool
	for _, d := range p.Decisions {
		if d.Kind == "leave_alone" {
			found = true
		}
	}
	if !found {
		t.Error("want a leave_alone decision on the record")
	}
}

func TestDropAndLandByHandAnswers(t *testing.T) {
	git := &aumGit{ahead: map[string]string{"work/a": "3\n", "work/b": "3\n"}}
	r := newFWRig(t, git, 2)
	r.h.m.now = func() time.Time { return r.at }
	a := r.finish(t, 0, PRDCompleted, "work/a", r.at.Add(-30*time.Hour))
	b := r.finish(t, 1, PRDCompleted, "work/b", r.at.Add(-30*time.Hour))
	r.h.m.RunFinishedWorkSweep(context.Background(), r.at)
	answerQ(t, r, mustOpenQ(t, r, a), FinishedWorkOptionDrop)
	answerQ(t, r, mustOpenQ(t, r, b), FinishedWorkOptionLand)
	if pa := r.get(t, a); pa.Landing == nil || pa.Landing.State != LandingDropped {
		t.Errorf("a landing = %+v, want dropped", pa.Landing)
	}
	pb := r.get(t, b)
	if pb.Landing == nil || pb.Landing.State != LandingPending || pb.Landing.ReaskAt == nil {
		t.Fatalf("b landing = %+v, want pending with a short re-ask date", pb.Landing)
	}
	if !pb.Landing.ReaskAt.Equal(r.at.AddDate(0, 0, FinishedWorkLandReaskDays)) {
		t.Errorf("land by hand re-ask = %v", pb.Landing.ReaskAt)
	}
	// A pending landing still cannot be archived.
	if _, err := r.h.m.ArchiveGuarded(b, "operator"); err == nil {
		t.Error("the archive guard must still hold for work someone said they will land")
	}
}

// ---- the digest ----

func TestDailyDigestHasOwnerAndQuestionAndIsSentOncePerDay(t *testing.T) {
	git := &aumGit{ahead: map[string]string{"work/old": "7\n"}}
	r := newFWRig(t, git, 1)
	var alerts []string
	r.h.m.SetAlertSink(func(level, title, body string) string {
		alerts = append(alerts, title+"|"+body)
		return ""
	})
	id := r.finish(t, 0, PRDCompleted, "work/old", r.at.Add(-30*time.Hour))
	r.h.m.RunFinishedWorkSweep(context.Background(), r.at)
	r.h.m.RunFinishedWorkSweep(context.Background(), r.at.Add(2*time.Hour))
	if len(alerts) != 1 {
		t.Fatalf("digest alerts in one day = %d, want 1: %v", len(alerts), alerts)
	}
	if !strings.Contains(alerts[0], id) || !strings.Contains(alerts[0], "dave") {
		t.Errorf("digest must name the Automaton and its owner: %q", alerts[0])
	}
	r.h.m.RunFinishedWorkSweep(context.Background(), r.at.Add(25*time.Hour))
	if len(alerts) != 2 {
		t.Errorf("digest alerts after a day = %d, want 2", len(alerts))
	}
	d := r.h.m.FinishedWorkDigest(r.at.Add(25 * time.Hour))
	if len(d.Waiting) != 1 || d.Waiting[0].Owner != "dave" || d.Waiting[0].QuestionID == "" {
		t.Errorf("digest = %+v", d)
	}
}

// ---- boundedness and timing ----

func TestSweepIsBoundedAndCoversEveryAutomatonAcrossPasses(t *testing.T) {
	const n = 500
	git := &aumGit{ahead: map[string]string{}}
	r := newFWRig(t, git, 1)
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		prd, err := r.h.m.Store().CreatePRD("spec", "/p", "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		branch := "work/" + prd.ID
		git.ahead[branch] = "2\n"
		prd.Status, prd.Git.Branch, prd.Git.BaseBranch = PRDCompleted, branch, "main"
		prd.Story = []Story{{Tasks: []Task{{ID: "t1", Title: "Build", Status: TaskCompleted}}}}
		if err := r.h.m.Store().SavePRD(prd); err != nil {
			t.Fatal(err)
		}
		r.touch(t, prd.ID, r.at.Add(-30*time.Hour))
		ids = append(ids, prd.ID)
	}
	start := time.Now()
	first := r.h.m.RunFinishedWorkSweep(context.Background(), r.at)
	if first.GitChecks > FinishedWorkSweepLimit || !first.Capped {
		t.Fatalf("first pass = %+v, want at most %d git checks and capped", first, FinishedWorkSweepLimit)
	}
	passes := 1
	for len(r.h.m.SweepFindings()) < n && passes < 20 {
		r.h.m.RunFinishedWorkSweep(context.Background(), r.at)
		passes++
	}
	if got := len(r.h.m.SweepFindings()); got != n {
		t.Fatalf("findings after %d passes = %d, want all %d covered", passes, got, n)
	}
	if want := (n + FinishedWorkSweepLimit - 1) / FinishedWorkSweepLimit; passes > want+1 {
		t.Errorf("%d passes to cover %d Automata, want about %d", passes, n, want)
	}
	if el := time.Since(start); el > 20*time.Second {
		t.Errorf("sweeping %d Automata took %v, want well under 20s with an instant git", n, el)
	}
	_ = ids
}

func TestSweepPassWithManyAutomataAndNothingToDoIsCheap(t *testing.T) {
	const n = 500
	git := &aumGit{}
	r := newFWRig(t, git, 1)
	for i := 0; i < n; i++ {
		prd, err := r.h.m.Store().CreatePRD("spec", "/p", "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		prd.Status = PRDArchived // branchless archived records: nothing to check
		if err := r.h.m.Store().SavePRD(prd); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now()
	pass := r.h.m.RunFinishedWorkSweep(context.Background(), r.at)
	if pass.GitChecks != 0 || len(git.calls) != 0 {
		t.Errorf("pass = %+v with %d git calls, want none for branchless records", pass, len(git.calls))
	}
	if el := time.Since(start); el > 3*time.Second {
		t.Errorf("a pass over %d Automata with nothing to do took %v", n, el)
	}
}

// ---- the weekly audit ----

func TestAuditCoversCancelledBranchlessAndStuckAndWritesOneDecisionRecordPerWeek(t *testing.T) {
	git := &aumGit{
		ahead: map[string]string{"work/arch": "9\n", "work/canc": "4\n", "work/run": "6\n"},
		gone:  map[string]bool{"work/lost": true},
	}
	r := newFWRig(t, git, 5)
	r.h.m.now = func() time.Time { return r.at }
	r.finish(t, 0, PRDArchived, "work/arch", r.at.Add(-10*24*time.Hour))
	r.finish(t, 1, PRDCancelled, "work/canc", r.at.Add(-10*24*time.Hour))
	r.finish(t, 2, PRDCompleted, "work/lost", r.at.Add(-10*24*time.Hour))
	stuck := r.finish(t, 3, PRDRunning, "work/run", r.at.Add(-9*24*time.Hour))
	pst := r.get(t, stuck)
	pst.Story = []Story{{Tasks: []Task{{ID: "t1", Title: "Implement", Status: TaskInProgress}}}}
	if err := r.h.m.Store().SavePRD(pst); err != nil {
		t.Fatal(err)
	}
	r.touch(t, stuck, r.at.Add(-9*24*time.Hour))
	r.finish(t, 4, PRDCompleted, "", r.at.Add(-10*24*time.Hour)) // nothing to find

	drs := newTestDecisionRecords(t, r)
	r.h.m.SetDecisionRecords(drs)

	res, err := r.h.m.RunFinishedWorkAudit(context.Background(), r.at)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if res.Scanned != 5 {
		t.Errorf("scanned = %d, want 5", res.Scanned)
	}
	for cause, want := range map[string]int{FinishedWorkNotMerged: 2, FinishedWorkBranchGone: 1, FinishedWorkStuck: 1} {
		if res.ByCause[cause] != want {
			t.Errorf("by cause %s = %d, want %d (all: %v)", cause, res.ByCause[cause], want, res.ByCause)
		}
	}
	if res.DecisionRecordID == "" {
		t.Fatal("audit in mode on must write its result as a decision record")
	}
	again, err := r.h.m.RunFinishedWorkAudit(context.Background(), r.at.Add(time.Hour))
	if err != nil {
		t.Fatalf("second audit: %v", err)
	}
	if again.DecisionRecordID != res.DecisionRecordID {
		t.Errorf("a second audit in the same week made a new record %q, want the same %q", again.DecisionRecordID, res.DecisionRecordID)
	}
	git.assertReadOnly(t)
}

func TestAuditShadowWritesNoDecisionRecordAndNoQuestions(t *testing.T) {
	git := &aumGit{ahead: map[string]string{"work/arch": "9\n"}}
	r := newFWRig(t, git, 1)
	cfg := r.h.m.Config()
	cfg.FinishedWork = "shadow"
	r.h.m.SetConfig(cfg)
	id := r.finish(t, 0, PRDArchived, "work/arch", r.at.Add(-10*24*time.Hour))
	r.h.m.SetDecisionRecords(newTestDecisionRecords(t, r))
	res, err := r.h.m.RunFinishedWorkAudit(context.Background(), r.at)
	if err != nil {
		t.Fatal(err)
	}
	if res.DecisionRecordID != "" || len(r.openQuestions(id)) != 0 {
		t.Errorf("shadow audit wrote a record %q or asked", res.DecisionRecordID)
	}
	if res.ByCause[FinishedWorkNotMerged] != 1 {
		t.Errorf("shadow audit must still find the work: %v", res.ByCause)
	}
}

// newTestDecisionRecords is the decision record service over the rig's
// question store, with both switches on.
func newTestDecisionRecords(t *testing.T, r *fwRig) *DecisionRecords {
	t.Helper()
	rs, err := NewDecisionRecordStore(filepath.Join(t.TempDir(), "decision_records.jsonl"))
	if err != nil {
		t.Fatalf("NewDecisionRecordStore: %v", err)
	}
	return NewDecisionRecords(rs, r.store,
		func() config.AutonomousDecisionsConfig { return config.AutonomousDecisionsConfig{} },
		func() config.AutonomousQuestionsConfig { return config.AutonomousQuestionsConfig{Enabled: true} },
		func() time.Time { return r.at })
}

func mustOpenQ(t *testing.T, r *fwRig, id string) Question {
	t.Helper()
	qs := r.openQuestions(id)
	if len(qs) == 0 {
		t.Fatalf("no open finished-work question for %s", id)
	}
	return qs[0]
}
