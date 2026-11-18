// Tests for the held-question pass (held_question_pass.go): the five held
// states the pass detects, its off/shadow/on modes, the per-state
// recommendation and default, the note-once dedupe in shadow mode, the
// open-question dedupe in mode on (including across a daemon restart), and
// the withdraw of a raised held question when the held state clears.
// The question store is the real append-only store from
// questions_store_test.go, driven through the coordinator pass the same
// way the idle loop calls it.

package autonomous

import (
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// heldHarness is the held-question pass's test harness: the coordinator
// harness (manager, coordinator, member PRDs, audit/alert sinks) plus the
// real append-only question store. The pass reads the live
// autonomous.questions config on every tick through the harness's mutable
// qcfg.
type heldHarness struct {
	*coordHarness
	qs   *QuestionStore
	rec  *fakeAuditRecorder // store-level audit recorder (question actions)
	qcfg config.AutonomousQuestionsConfig
}

func newHeldHarness(t *testing.T, nPRDs int) *heldHarness {
	t.Helper()
	tm := newCoordHarness(t, nPRDs)
	_, qs, rec, _ := newQuestionStoreTest(t)
	h := &heldHarness{coordHarness: tm, qs: qs, rec: rec}
	tm.m.SetDecisionGate(qs, func() config.AutonomousQuestionsConfig {
		return h.qcfg
	})
	return h
}

// setMode updates the live config the pass reads on the next tick.
func (h *heldHarness) setMode(mode string) {
	h.t.Helper()
	h.qcfg = config.AutonomousQuestionsConfig{HeldMode: mode}
}

// setStates restricts the pass to the named held states.
func (h *heldHarness) setStates(states []string) {
	h.t.Helper()
	h.qcfg.HeldStates = states
}

// restart swaps in a fresh coordinator for the same manager and store:
// the daemon restarted, so the in-memory held index is empty, but the
// append-only store file is untouched.
func (h *heldHarness) restart() {
	h.t.Helper()
	h.c = NewCoordinator(h.m, h.fake)
	h.c.alertFn = func(level, title, body string) string {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.alerts = append(h.alerts, level+"|"+title+"|"+body)
		return ""
	}
	h.c.auditFn = func(action string, d map[string]any) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.audits = append(h.audits, action)
		h.auditD = append(h.auditD, d)
	}
}

func (h *heldHarness) resetAudits() {
	h.mu.Lock()
	h.audits = h.audits[:0]
	h.auditD = h.auditD[:0]
	h.mu.Unlock()
}

// wantMode asserts the mode is what the test set and the pass sees it.
func (h *heldHarness) wantMode(t *testing.T, want string) {
	t.Helper()
	if got := h.qcfg.EffectiveHeldMode(); got != want {
		h.t.Fatalf("effective held mode = %q, want %q", got, want)
	}
}

// ---- state setup ----------------------------------------------------

// setPRD edits and saves PRD i.
func (h *heldHarness) setPRD(i int, mutate func(*PRD)) {
	h.t.Helper()
	p, ok := h.m.store.GetPRD(h.prds[i].ID)
	if !ok {
		h.t.Fatalf("prd %d missing", i)
	}
	mutate(p)
	if err := h.m.store.SavePRD(p); err != nil {
		h.t.Fatal(err)
	}
}

// block flips PRD i into the blocked held state.
func (h *heldHarness) block(i int, reason string) {
	h.setPRD(i, func(p *PRD) {
		p.Status = PRDBlocked
		p.BlockedReason = reason
	})
}

// clearBlocked takes PRD i out of the blocked held state.
func (h *heldHarness) clearBlocked(i int) {
	h.setPRD(i, func(p *PRD) {
		p.Status = PRDApproved
		p.BlockedReason = ""
	})
}

// revisionsAsked flips PRD i into the revision-requested held state.
func (h *heldHarness) revisionsAsked(i int, reason string) {
	h.setPRD(i, func(p *PRD) {
		p.Status = PRDRevisionsAsked
		p.RejectionReason = reason
		p.RevisionsRequested = 1
	})
}

// addTask gives PRD i a single story with one task and returns the task id.
func (h *heldHarness) addTask(i int) string {
	h.t.Helper()
	if err := h.m.Store().SetStories(h.prds[i].ID, []Story{
		{Title: "S", Tasks: []Task{{Title: "T", Spec: "do the work"}}},
	}); err != nil {
		h.t.Fatal(err)
	}
	s, ok := h.m.store.SnapshotPRD(h.prds[i].ID)
	if !ok || len(s.Story) == 0 || len(s.Story[0].Tasks) != 1 {
		h.t.Fatalf("task not stored on prd %d", i)
	}
	return s.Story[0].Tasks[0].ID
}

// saveTask edits and saves the first task of PRD i.
func (h *heldHarness) saveTask(i int, mutate func(*Task)) {
	h.t.Helper()
	p, ok := h.m.store.GetPRD(h.prds[i].ID)
	if !ok {
		h.t.Fatalf("prd %d missing", i)
	}
	mutate(&p.Story[0].Tasks[0])
	if err := h.m.store.SavePRD(p); err != nil {
		h.t.Fatal(err)
	}
}

// addPendingInsert gives automation a one pending member insert.
func (h *heldHarness) addPendingInsert(a *Automation, prdID, id string) {
	h.t.Helper()
	if _, err := h.m.automations.Update(a.ID, 0, func(a *Automation) error {
		a.PendingInserts = []PendingInsert{{
			ID:      id,
			PRDID:   prdID,
			Actor:   "operator",
			Reason:  "operator asked for a review member",
			AddedAt: time.Now(),
			Status:  MemberPending,
		}}
		return nil
	}); err != nil {
		h.t.Fatal(err)
	}
}

// ---- read helpers ---------------------------------------------------

// openHeld returns the open held questions in the store.
func (h *heldHarness) openHeld() []Question {
	h.t.Helper()
	var out []Question
	for _, q := range h.qs.List(QuestionFilter{State: QuestionOpen, Limit: 100}) {
		if q.Source == QuestionSourceHeld {
			out = append(out, q)
		}
	}
	return out
}

// heldNotes returns the pass-written (source held) TriageNotes on PRD i.
func (h *heldHarness) heldNotes(i int) []TriageNote {
	h.t.Helper()
	s, ok := h.m.store.SnapshotPRD(h.prds[i].ID)
	if !ok {
		h.t.Fatalf("prd %d missing", i)
	}
	var out []TriageNote
	if s.Triage == nil {
		return out
	}
	for _, n := range s.Triage.Notes {
		if n.Source == "held" {
			out = append(out, n)
		}
	}
	return out
}

// hasHeldNote reports whether PRD i has a pass-written shadow note whose
// text starts with the given dedupe marker.
func (h *heldHarness) hasHeldNote(i int, marker string) bool {
	for _, n := range h.heldNotes(i) {
		if strings.HasPrefix(n.Text, marker) {
			return true
		}
	}
	return false
}

// countAudits counts the coordinator audit records for one action.
func (h *heldHarness) countAudits(action string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, a := range h.audits {
		if a == action {
			n++
		}
	}
	return n
}

// anyHeldAudit reports whether any held-question audit record exists.
func (h *heldHarness) anyHeldAudit() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, a := range h.audits {
		if strings.HasPrefix(a, "held_question") {
			return true
		}
	}
	return false
}

// wantQuestion asserts the shape every raised held question carries and
// returns it.
func wantQuestion(t *testing.T, q Question, marker string) Question {
	t.Helper()
	if got, want := q.SourceLineSHA256, heldQuestionLineHash(marker); got != want {
		t.Errorf("SourceLineSHA256 = %q, want sha256(%q) = %q", got, marker, want)
	}
	if q.SessionID != "held" {
		t.Errorf("SessionID = %q, want %q", q.SessionID, "held")
	}
	if q.Source != QuestionSourceHeld {
		t.Errorf("Source = %q, want %q", q.Source, QuestionSourceHeld)
	}
	if q.Default != string(HeldOptionHold) {
		t.Errorf("Default = %q, want %q", q.Default, string(HeldOptionHold))
	}
	if !q.Blocking {
		t.Errorf("Blocking = false, want true")
	}
	if len(q.Options) == 0 || q.Options[len(q.Options)-1].ID != string(HeldOptionHold) {
		t.Errorf("options %v, want hold among the options", q.Options)
	}
	if q.Recommendation == nil || q.Recommendation.OptionID == "" {
		t.Errorf("Recommendation missing")
	}
	if q.Text == "" {
		t.Errorf("Text is empty")
	}
	return q
}

// ---- raise: one question per held (item, state) ----------------------

func TestHeldQuestionRaise(t *testing.T) {
	cases := []struct {
		name    string
		prep    func(h *heldHarness) string // prepares the held state; returns the item key
		state   HeldState
		wantRec string // expected recommendation option id (unclassified default)
	}{
		{
			name: "blocked",
			prep: func(h *heldHarness) string {
				h.block(0, "waiting for an operator ruling on the retry budget")
				return h.prds[0].ID
			},
			state:   HeldBlocked,
			wantRec: string(HeldOptionHold),
		},
		{
			name: "review-held",
			prep: func(h *heldHarness) string {
				tid := h.addTask(0)
				h.saveTask(0, func(tk *Task) {
					tk.Status = TaskReviewNeeded
					tk.Error = "out of budget for two attempts"
					tk.FailureReason = &FailureReason{Category: FailureCategoryContent}
					tk.EscalationFailures = 3
					tk.ReviewSummary = &ReviewSummary{
						FailureCount: 3,
						TotalSeconds: 420,
						Text:         "two failed attempts, both out of budget",
					}
				})
				return tid
			},
			state:   HeldReviewHeld,
			wantRec: string(HeldOptionChangeSpec),
		},
		{
			name: "revision-requested",
			prep: func(h *heldHarness) string {
				h.revisionsAsked(0, "the spec misses the auth requirement")
				return h.prds[0].ID
			},
			state:   HeldRevisionRequested,
			wantRec: string(HeldOptionChangeSpec),
		},
		{
			name: "awaiting-decision",
			prep: func(h *heldHarness) string {
				tid := h.addTask(0)
				h.saveTask(0, func(tk *Task) {
					tk.Status = TaskWaitingDecision
					tk.WaitReason = AwaitDecisionWaitReason
				})
				return tid
			},
			state:   HeldAwaitingDecision,
			wantRec: string(HeldOptionHold),
		},
		{
			name: "pending-insert",
			prep: func(h *heldHarness) string {
				a := h.makeAuto(nil, 2)
				h.addPendingInsert(a, h.prds[1].ID, "ins1")
				return "ins1"
			},
			state:   HeldPendingInsert,
			wantRec: string(HeldOptionHold),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHeldHarness(t, 2)
			h.setMode("on")
			h.wantMode(t, "on")
			key := tc.prep(h)

			h.c.heldQuestionPass()

			opens := h.openHeld()
			if len(opens) != 1 {
				h.t.Fatalf("open held questions = %d, want 1: %+v", len(opens), opens)
			}
			if got := opens[0].Recommendation.OptionID; got != tc.wantRec {
				t.Errorf("recommendation = %q, want %q", got, tc.wantRec)
			}
			q := wantQuestion(t, opens[0], "held:"+key+":"+string(tc.state))
			if !strings.Contains(q.Text, key) {
				t.Errorf("question text does not name the held key %q:\n%s", key, q.Text)
			}
			if h.countAudits("held_question_raised") != 1 {
				t.Errorf("held_question_raised audits = %d, want 1", h.countAudits("held_question_raised"))
			}
			if h.countAudits("held_question_shadow") != 0 {
				t.Errorf("mode on wrote shadow notes")
			}
			h.mu.Lock()
			nAlerts := len(h.alerts)
			h.mu.Unlock()
			if nAlerts != 0 {
				t.Errorf("alerts = %d, want 0: the pass does not alert", nAlerts)
			}
		})
	}
}

func TestHeldQuestionRaiseAwaitingDecisionLinksToOpenQuestion(t *testing.T) {
	h := newHeldHarness(t, 1)
	h.setMode("on")

	// The decision the task is parked on is already an open question.
	qid := mustRaise(t, h.qs, sampleQuestion("approve the retry?"))
	tid := h.addTask(0)
	h.saveTask(0, func(tk *Task) {
		tk.Status = TaskWaitingDecision
		tk.WaitReason = AwaitDecisionWaitText(qid)
	})

	h.c.heldQuestionPass()

	if opens := h.openHeld(); len(opens) != 0 {
		h.t.Fatalf("open held questions = %d, want 0: the decision question already exists: %+v",
			len(opens), opens)
	}
	if h.countAudits("held_question_linked") != 1 {
		t.Errorf("held_question_linked audits = %d, want 1", h.countAudits("held_question_linked"))
	}
	if h.countAudits("held_question_raised") != 0 {
		t.Errorf("a second question was raised for the same hold")
	}

	// Once the decision question is no longer open, the link is gone and
	// the next pass raises the held question.
	if _, err := h.qs.Withdraw(qid, "operator", "no longer needed"); err != nil {
		h.t.Fatal(err)
	}
	h.c.heldQuestionPass()

	opens := h.openHeld()
	if len(opens) != 1 {
		h.t.Fatalf("open held questions = %d, want 1 after the decision cleared: %+v",
			len(opens), opens)
	}
	wantQuestion(t, opens[0], "held:"+tid+":"+string(HeldAwaitingDecision))
}

// ---- shadow: the note, and only once per (item, state) ---------------

func TestHeldQuestionShadowWritesNoteAndRaisesNothing(t *testing.T) {
	h := newHeldHarness(t, 1)
	h.setMode("shadow")
	h.block(0, "waiting for an operator ruling")
	marker := "held:" + h.prds[0].ID + ":" + string(HeldBlocked)

	h.c.heldQuestionPass()

	if opens := h.openHeld(); len(opens) != 0 {
		h.t.Fatalf("shadow mode raised questions: %+v", opens)
	}
	notes := h.heldNotes(0)
	if len(notes) != 1 {
		h.t.Fatalf("shadow notes = %d, want 1: %+v", len(notes), notes)
	}
	n := notes[0]
	if n.Source != "held" {
		t.Errorf("note source = %q, want held", n.Source)
	}
	if !strings.HasPrefix(n.Text, marker) {
		t.Errorf("note text does not start with the dedupe marker %q:\n%s", marker, n.Text)
	}
	for _, want := range []string{"blocked", "would raise a held question", string(HeldOptionHold)} {
		if !strings.Contains(n.Text, want) {
			t.Errorf("note text missing %q:\n%s", want, n.Text)
		}
	}
	if h.countAudits("held_question_shadow") != 1 {
		t.Errorf("held_question_shadow audits = %d, want 1", h.countAudits("held_question_shadow"))
	}
	h.mu.Lock()
	nAlerts := len(h.alerts)
	h.mu.Unlock()
	if nAlerts != 0 {
		t.Errorf("shadow alerts = %d, want 0", nAlerts)
	}
}

func TestHeldQuestionShadowNoteOncePerState(t *testing.T) {
	h := newHeldHarness(t, 1)
	h.setMode("shadow")
	h.block(0, "waiting for an operator ruling")

	h.c.heldQuestionPass()
	h.c.heldQuestionPass()
	// A restart does not re-note either: the append-only notes persist
	// and the marker already appears in the record.
	h.restart()
	h.c.heldQuestionPass()

	notes := h.heldNotes(0)
	if len(notes) != 1 {
		h.t.Fatalf("shadow notes after three passes (one on a fresh daemon) = %d, want 1: %+v",
			len(notes), notes)
	}
	if opens := h.openHeld(); len(opens) != 0 {
		h.t.Fatalf("shadow mode raised questions: %+v", opens)
	}
}

// ---- mode: shadow first, then switched to on -------------------------

func TestHeldQuestionModeShadowThenOn(t *testing.T) {
	h := newHeldHarness(t, 1)
	h.setMode("shadow")
	h.block(0, "waiting for an operator ruling")
	marker := "held:" + h.prds[0].ID + ":" + string(HeldBlocked)

	h.c.heldQuestionPass()
	if opens := h.openHeld(); len(opens) != 0 {
		h.t.Fatalf("shadow pass raised questions: %+v", opens)
	}
	if !h.hasHeldNote(0, marker) {
		h.t.Fatalf("shadow pass wrote no note for %q", marker)
	}

	// The operator switches the mode to on: the next pass raises the
	// question, and the existing shadow note is not repeated.
	h.setMode("on")
	h.c.heldQuestionPass()

	opens := h.openHeld()
	if len(opens) != 1 {
		h.t.Fatalf("open held questions = %d, want 1 after mode on: %+v", len(opens), opens)
	}
	wantQuestion(t, opens[0], marker)
	if n := len(h.heldNotes(0)); n != 1 {
		t.Errorf("shadow notes = %d after the mode switch, want still 1", n)
	}
}

// ---- dedupe: one open held question per (item, state) ----------------

func TestHeldQuestionDedupeSecondPass(t *testing.T) {
	h := newHeldHarness(t, 1)
	h.setMode("on")
	h.block(0, "waiting for an operator ruling")
	tid := h.addTask(0)
	h.saveTask(0, func(tk *Task) {
		tk.Status = TaskReviewNeeded
		tk.ReviewSummary = &ReviewSummary{FailureCount: 2, TotalSeconds: 100, Text: "failed twice"}
	})

	h.c.heldQuestionPass()
	if opens := h.openHeld(); len(opens) != 2 {
		h.t.Fatalf("open held questions after first pass = %d, want 2 (blocked prd + review-held task)", len(opens))
	}

	h.c.heldQuestionPass()
	h.c.heldQuestionPass()

	opens := h.openHeld()
	if len(opens) != 2 {
		h.t.Fatalf("open held questions after three passes = %d, want 2: %+v", len(opens), opens)
	}
	if n := h.countAudits("held_question_raised"); n != 2 {
		t.Errorf("held_question_raised audits = %d, want 2 (one per held state)", n)
	}
	if n := h.countAudits("held_question_raise_refused"); n != 0 {
		t.Errorf("held_question_raise_refused = %d, want 0 (dedupe is silent)", n)
	}
	// The two open questions are the two (item, state) pairs, each with
	// its own stable dedupe hash.
	want := map[string]bool{
		"held:" + h.prds[0].ID + ":" + string(HeldBlocked): false,
		"held:" + tid + ":" + string(HeldReviewHeld):       false,
	}
	for _, q := range opens {
		marker := ""
		for m := range want {
			if q.SourceLineSHA256 == heldQuestionLineHash(m) {
				marker = m
			}
		}
		if marker == "" {
			t.Errorf("open question %s does not carry one of the expected dedupe hashes", q.ID)
		} else if want[marker] {
			t.Errorf("duplicate open question for %q", marker)
		} else {
			want[marker] = true
		}
	}
}

func TestHeldQuestionDedupeAcrossRestart(t *testing.T) {
	h := newHeldHarness(t, 1)
	h.setMode("on")
	h.block(0, "waiting for an operator ruling")

	h.c.heldQuestionPass()
	first := h.openHeld()
	if len(first) != 1 {
		h.t.Fatalf("open held questions = %d, want 1", len(first))
	}
	h.resetAudits()

	// The daemon restarts: fresh coordinator, empty held index, same
	// append-only store. The pass must not raise a second open question
	// for the same held (item, state).
	h.restart()
	h.c.heldQuestionPass()
	h.c.heldQuestionPass()

	opens := h.openHeld()
	if len(opens) != 1 {
		h.t.Fatalf("open held questions after restart = %d, want 1: %+v", len(opens), opens)
	}
	if opens[0].ID != first[0].ID {
		t.Errorf("question id = %q, want the original %q", opens[0].ID, first[0].ID)
	}
	if n := h.countAudits("held_question_raised"); n != 0 {
		t.Errorf("held_question_raised after restart = %d, want 0 (already covered)", n)
	}

	// Restart again and check the index rebuild from the store still
	// finds the question by its stable key, so the withdraw bookkeeping
	// survives a third daemon.
	h.restart()
	h.clearBlocked(0)
	h.c.heldQuestionPass()
	q, _ := h.qs.Get(first[0].ID)
	if q.State != QuestionWithdrawn {
		h.t.Fatalf("question state after clear + pass on a fresh daemon = %q, want withdrawn", q.State)
	}
}

func TestHeldQuestionDedupeDifferentStateSameItem(t *testing.T) {
	h := newHeldHarness(t, 2)
	h.setMode("on")
	a := h.makeAuto(nil, 2)
	h.addPendingInsert(a, h.prds[1].ID, "ins1")
	tid := h.addTask(0)
	h.saveTask(0, func(tk *Task) {
		tk.Status = TaskReviewNeeded
		tk.ReviewSummary = &ReviewSummary{FailureCount: 2, TotalSeconds: 30, Text: "failed twice"}
	})

	h.c.heldQuestionPass()

	// Two different held states: the pending insert on one member and
	// the review-held task on another. Both must coexist as open
	// questions.
	opens := h.openHeld()
	if len(opens) != 2 {
		h.t.Fatalf("open held questions = %d, want 2: %+v", len(opens), opens)
	}
	markers := map[string]int{
		"held:ins1:" + string(HeldPendingInsert):     0,
		"held:" + tid + ":" + string(HeldReviewHeld): 0,
	}
	for _, q := range opens {
		for m := range markers {
			if q.SourceLineSHA256 == heldQuestionLineHash(m) {
				markers[m]++
			}
		}
	}
	for m, n := range markers {
		if n != 1 {
			t.Errorf("open questions for %q = %d, want 1", m, n)
		}
	}
}

// ---- withdraw: the held state cleared -------------------------------

func TestHeldQuestionWithdrawWhenStateClears(t *testing.T) {
	h := newHeldHarness(t, 1)
	h.setMode("on")
	h.block(0, "waiting for an operator ruling")
	tid := h.addTask(0)
	h.saveTask(0, func(tk *Task) {
		tk.Status = TaskReviewNeeded
		tk.ReviewSummary = &ReviewSummary{FailureCount: 2, TotalSeconds: 30, Text: "failed twice"}
	})

	h.c.heldQuestionPass()
	opens := h.openHeld()
	if len(opens) != 2 {
		h.t.Fatalf("open held questions = %d, want 2", len(opens))
	}
	var blockedID, reviewID string
	for _, q := range opens {
		if q.SourceLineSHA256 == heldQuestionLineHash("held:"+h.prds[0].ID+":"+string(HeldBlocked)) {
			blockedID = q.ID
		}
		if q.SourceLineSHA256 == heldQuestionLineHash("held:"+tid+":"+string(HeldReviewHeld)) {
			reviewID = q.ID
		}
	}
	if blockedID == "" || reviewID == "" {
		h.t.Fatalf("expected one blocked question and one review question, got %+v", opens)
	}
	h.resetAudits()

	// The review task clears; the block stays.
	h.saveTask(0, func(tk *Task) {
		tk.Status = TaskPending
		tk.ReviewSummary = nil
		tk.FailureReason = nil
		tk.Error = ""
	})
	h.c.heldQuestionPass()

	review, _ := h.qs.Get(reviewID)
	if review.State != QuestionWithdrawn {
		t.Errorf("review question state = %q, want withdrawn", review.State)
	}
	blocked, _ := h.qs.Get(blockedID)
	if blocked.State != QuestionOpen {
		t.Errorf("blocked question state = %q, want still open", blocked.State)
	}
	if n := h.countAudits("held_question_withdraw"); n != 1 {
		t.Errorf("held_question_withdraw audits = %d, want 1", n)
	}

	// The store audit of the withdraw names the daemon actor and the
	// cleared state.
	actions, details := h.rec.snapshot()
	found := false
	for i, act := range actions {
		if act != "question_withdrawn" {
			continue
		}
		d := details[i]
		if d["question_id"] != reviewID {
			continue
		}
		found = true
		if d["actor"] != "daemon" {
			t.Errorf("withdraw audit actor = %v, want daemon", d["actor"])
		}
		reason, _ := d["reason"].(string)
		if !strings.Contains(reason, string(HeldReviewHeld)) {
			t.Errorf("withdraw reason does not name the cleared state: %q", reason)
		}
	}
	if !found {
		t.Errorf("no question_withdrawn store audit for %s: %v", reviewID, actions)
	}

	// The last held state clears: the remaining question is withdrawn.
	h.clearBlocked(0)
	h.c.heldQuestionPass()
	blocked, _ = h.qs.Get(blockedID)
	if blocked.State != QuestionWithdrawn {
		t.Errorf("blocked question state = %q, want withdrawn after it cleared", blocked.State)
	}
}

// A question that is already answered is a decision of record: the pass
// never re-opens it and never touches it when the state clears.
func TestHeldQuestionWithdrawLeavesAnsweredQuestions(t *testing.T) {
	h := newHeldHarness(t, 1)
	h.setMode("on")
	h.block(0, "waiting for an operator ruling")

	h.c.heldQuestionPass()
	opens := h.openHeld()
	if len(opens) != 1 {
		h.t.Fatalf("open held questions = %d, want 1", len(opens))
	}
	ans, err := h.qs.Answer(opens[0].ID, Answer{
		Actor:          operatorActor,
		ChosenOptionID: string(HeldOptionHold),
		Quote:          "approved, proceed",
	})
	if err != nil || ans.QuestionID == "" {
		h.t.Fatalf("Answer: %v %+v", err, ans)
	}

	// The state clears but the question is answered, not open.
	h.clearBlocked(0)
	h.c.heldQuestionPass()

	q, _ := h.qs.Get(opens[0].ID)
	if q.State != QuestionAnswered {
		t.Errorf("answered question state = %q, want answered (the pass does not touch it)", q.State)
	}
	if n := h.countAudits("held_question_withdraw"); n != 0 {
		t.Errorf("held_question_withdraw = %d, want 0: an answered question is a decision of record", n)
	}
}

// ---- off: the pass does nothing -------------------------------------

func TestHeldQuestionOffDoesNothing(t *testing.T) {
	h := newHeldHarness(t, 1)
	h.setMode("off")
	h.block(0, "waiting for an operator ruling")
	tid := h.addTask(0)
	h.saveTask(0, func(tk *Task) {
		tk.Status = TaskReviewNeeded
		tk.ReviewSummary = &ReviewSummary{FailureCount: 2, TotalSeconds: 30, Text: "failed twice"}
	})

	h.c.heldQuestionPass()

	if opens := h.openHeld(); len(opens) != 0 {
		h.t.Fatalf("off mode raised questions: %+v", opens)
	}
	if n := len(h.heldNotes(0)); n != 0 {
		h.t.Fatalf("off mode wrote shadow notes: %+v", h.heldNotes(0))
	}
	if h.anyHeldAudit() {
		t.Errorf("off mode wrote held-question audits")
	}
	h.mu.Lock()
	nAlerts := len(h.alerts)
	h.mu.Unlock()
	if nAlerts != 0 {
		t.Errorf("off mode alerted %d times", nAlerts)
	}
	_ = tid
}

// In off mode the pass neither raises nor withdraws: a held question that
// was raised before the operator switched the mode off (and whose state
// has since cleared) stays where it is.
func TestHeldQuestionOffLeavesOpenQuestionsAlone(t *testing.T) {
	h := newHeldHarness(t, 1)
	h.setMode("on")
	h.block(0, "waiting for an operator ruling")
	h.c.heldQuestionPass()
	opens := h.openHeld()
	if len(opens) != 1 {
		h.t.Fatalf("open held questions = %d, want 1", len(opens))
	}

	// The state clears and the operator switches the mode off.
	h.clearBlocked(0)
	h.setMode("off")
	h.resetAudits()
	h.c.heldQuestionPass()

	q, _ := h.qs.Get(opens[0].ID)
	if q.State != QuestionOpen {
		h.t.Fatalf("question state = %q, want open: off mode does not withdraw", q.State)
	}
	if h.anyHeldAudit() {
		t.Errorf("off mode wrote held-question audits: %+v", h.audits)
	}
}

// ---- held_states: an uncovered state is skipped ----------------------

func TestHeldQuestionStatesFilter(t *testing.T) {
	h := newHeldHarness(t, 1)
	h.setMode("on")
	h.block(0, "waiting for an operator ruling")
	tid := h.addTask(0)
	h.saveTask(0, func(tk *Task) {
		tk.Status = TaskReviewNeeded
		tk.ReviewSummary = &ReviewSummary{FailureCount: 2, TotalSeconds: 30, Text: "failed twice"}
	})

	// Only the review-held state is covered: the blocked state is
	// skipped even though it is present.
	h.setStates([]string{string(HeldReviewHeld)})
	h.c.heldQuestionPass()

	opens := h.openHeld()
	if len(opens) != 1 {
		h.t.Fatalf("open held questions = %d, want 1 (only review_held covered): %+v", len(opens), opens)
	}
	if opens[0].SourceLineSHA256 != heldQuestionLineHash("held:"+tid+":"+string(HeldReviewHeld)) {
		t.Errorf("open question is not the review_held one: %+v", opens[0])
	}

	// Reversing the filter stops covering the review-held state, so its
	// open question is withdrawn on the next pass; the blocked state is
	// now covered and its question is raised.
	h.setStates([]string{string(HeldBlocked)})
	h.c.heldQuestionPass()

	opens = h.openHeld()
	if len(opens) != 1 {
		h.t.Fatalf("open held questions = %d, want 1 (review withdrawn, blocked raised): %+v", len(opens), opens)
	}
	if opens[0].SourceLineSHA256 != heldQuestionLineHash("held:"+h.prds[0].ID+":"+string(HeldBlocked)) {
		t.Errorf("open question is not the blocked one: %+v", opens[0])
	}
	if n := h.countAudits("held_question_withdraw"); n != 1 {
		t.Errorf("held_question_withdraw = %d, want 1 (review question lost coverage)", n)
	}
}

// ---- no held state: no pass records at all ---------------------------

func TestHeldQuestionPassNothingHeld(t *testing.T) {
	h := newHeldHarness(t, 1)
	h.setMode("on")

	h.c.heldQuestionPass()

	if opens := h.openHeld(); len(opens) != 0 {
		h.t.Fatalf("questions raised with nothing held: %+v", opens)
	}
	if h.anyHeldAudit() {
		h.mu.Lock()
		acts := append([]string(nil), h.audits...)
		h.mu.Unlock()
		h.t.Fatalf("audits with nothing held: %v", acts)
	}
}
