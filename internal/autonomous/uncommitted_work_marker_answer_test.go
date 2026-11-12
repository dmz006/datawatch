package autonomous

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// markerHoldRig is an Automaton with a worktree hold on the given files and a
// real question store holding the hold's question.
type markerHoldRig struct {
	m     *Manager
	prd   *PRD
	store *QuestionStore
	qid   string
}

func newMarkerHoldRig(t *testing.T, withStore bool, files ...string) *markerHoldRig {
	t.Helper()
	m, _ := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	r := &markerHoldRig{m: m, prd: prd}
	if withStore {
		s, err := NewQuestionStore(filepath.Join(t.TempDir(), "questions.jsonl"))
		if err != nil {
			t.Fatalf("NewQuestionStore: %v", err)
		}
		m.SetDecisionGate(s, func() config.AutonomousQuestionsConfig { return config.AutonomousQuestionsConfig{} })
		r.store = s
	}
	uf := make([]UncommittedFile, len(files))
	for i, f := range files {
		uf[i] = UncommittedFile{Path: f}
	}
	r.qid = m.raiseUncommittedQuestion(prd, nil, UncommittedHoldKindWorktree, uf, "files were left uncommitted")
	m.store.WithLock(func() {
		prd.UncommittedWork = &UncommittedWorkHold{
			Kind: UncommittedHoldKindWorktree, Files: files, QuestionID: r.qid, HeldAt: time.Now(),
		}
		prd.Status = PRDBlocked
	})
	if err := m.store.SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return r
}

func TestMarkerSweepAnswersMarkerOnlyHold(t *testing.T) {
	r := newMarkerHoldRig(t, true, "DATAWATCH_COMPLETE")
	if got := r.m.sweepMarkerOnlyUncommittedQuestions(); got != 1 {
		t.Fatalf("answered = %d, want 1", got)
	}
	q, _ := r.store.Get(r.qid)
	if q.State != QuestionAnswered {
		t.Fatalf("question state = %s, want answered", q.State)
	}
	a, ok := r.store.CurrentAnswer(r.qid)
	if !ok || a.ChosenOptionID != UncommittedOptionIgnore || a.Actor != "autonomous" {
		t.Fatalf("answer = %+v ok=%v", a, ok)
	}
	prd, _ := r.m.Store().GetPRD(r.prd.ID)
	if prd.UncommittedWork != nil {
		t.Fatalf("hold not cleared: %+v", prd.UncommittedWork)
	}
	var found bool
	for _, d := range prd.Decisions {
		if strings.HasPrefix(d.Note, "ignored: the question listed only the daemon marker file(s) DATAWATCH_COMPLETE") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no ignore decision line: %+v", prd.Decisions)
	}
	if len(decisionsOfKind(prd, uncommittedResolvedDecisionKind)) < 2 {
		t.Fatalf("want the resolved decision from the answer path too: %+v", prd.Decisions)
	}
	if prd.Status != PRDRunning {
		t.Fatalf("status = %s, want running", prd.Status)
	}
}

func TestMarkerSweepLeavesRealFilesAlone(t *testing.T) {
	r := newMarkerHoldRig(t, true, "DATAWATCH_COMPLETE", "internal/real.go")
	before := len(r.prd.Decisions)
	if got := r.m.sweepMarkerOnlyUncommittedQuestions(); got != 0 {
		t.Fatalf("answered = %d, want 0", got)
	}
	q, _ := r.store.Get(r.qid)
	if q.State != QuestionOpen {
		t.Fatalf("question state = %s, want open", q.State)
	}
	for _, o := range q.Options {
		if o.ID == UncommittedOptionIgnore {
			t.Fatal("ignore option added to a question that lists a real file")
		}
	}
	prd, _ := r.m.Store().GetPRD(r.prd.ID)
	if prd.UncommittedWork == nil || len(prd.Decisions) != before || prd.Status != PRDBlocked {
		t.Fatalf("hold touched: hold=%+v decisions=%d status=%s", prd.UncommittedWork, len(prd.Decisions), prd.Status)
	}
}

func TestMarkerSweepTwiceAnswersOnce(t *testing.T) {
	r := newMarkerHoldRig(t, true, "DATAWATCH_COMPLETE")
	first := r.m.sweepMarkerOnlyUncommittedQuestions()
	second := r.m.sweepMarkerOnlyUncommittedQuestions()
	if first != 1 || second != 0 {
		t.Fatalf("first=%d second=%d, want 1 and 0", first, second)
	}
	if n := r.store.AnswerCount(r.qid); n != 1 {
		t.Fatalf("answer count = %d, want 1", n)
	}
	prd, _ := r.m.Store().GetPRD(r.prd.ID)
	if n := len(decisionsOfKind(prd, uncommittedResolvedDecisionKind)); n != 2 {
		t.Fatalf("resolved decisions = %d, want 2 (answer path + sweep line)", n)
	}
}

func TestMarkerSweepWithoutStoreDoesNothing(t *testing.T) {
	r := newMarkerHoldRig(t, false, "DATAWATCH_COMPLETE")
	if got := r.m.sweepMarkerOnlyUncommittedQuestions(); got != 0 {
		t.Fatalf("answered = %d, want 0", got)
	}
	prd, _ := r.m.Store().GetPRD(r.prd.ID)
	if prd.UncommittedWork == nil {
		t.Fatal("hold cleared without a store")
	}
}

func TestMarkerSweepSkipsNestedMarkerName(t *testing.T) {
	r := newMarkerHoldRig(t, true, "sub/DATAWATCH_COMPLETE")
	if got := r.m.sweepMarkerOnlyUncommittedQuestions(); got != 0 {
		t.Fatalf("answered = %d, want 0 for a file in a subfolder", got)
	}
}
