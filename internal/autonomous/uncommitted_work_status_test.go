package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// uncommittedStatusRig is a Manager with two Automata: "Held Automaton" with
// one held task and a worktree hold, and "Clean Automaton" with no hold.
type uncommittedStatusRig struct {
	m         *Manager
	held      *PRD
	clean     *PRD
	heldTask  string
	heldAtMin time.Time
}

func newUncommittedStatusRig(t *testing.T) *uncommittedStatusRig {
	t.Helper()
	m, _ := newFailureEscalationFixture(t, t.TempDir())
	mk := func(title string) *PRD {
		prd, err := m.CreatePRD("test spec", t.TempDir(), "opencode", "", EffortNormal)
		if err != nil {
			t.Fatalf("CreatePRD: %v", err)
		}
		if err := m.Store().SetStories(prd.ID, []Story{
			{Title: "Story", Tasks: []Task{{Title: "Task one", Spec: "a"}, {Title: "Task two", Spec: "b"}}},
		}); err != nil {
			t.Fatalf("SetStories: %v", err)
		}
		prd, _ = m.Store().GetPRD(prd.ID)
		prd.Title = title
		if err := m.Store().SavePRD(prd); err != nil {
			t.Fatalf("SavePRD: %v", err)
		}
		return prd
	}
	r := &uncommittedStatusRig{m: m, held: mk("Held Automaton"), clean: mk("Clean Automaton")}
	r.heldAtMin = time.Date(2026, 10, 30, 20, 51, 0, 0, time.UTC)
	r.heldTask = r.held.Story[0].Tasks[1].ID
	m.Store().WithLock(func() {
		r.held.Story[0].Tasks[1].UncommittedWork = &UncommittedWorkHold{
			Kind:         UncommittedHoldKindTask,
			Policy:       config.UncommittedWorkPolicyHold,
			Files:        []string{"a.go", "docs/b.md"},
			OutsideScope: []string{"docs/b.md"},
			Reason:       UncommittedCauseStillDirty,
			QuestionID:   "q-1",
			HeldAt:       r.heldAtMin,
		}
		r.held.UncommittedWork = &UncommittedWorkHold{
			Kind:       UncommittedHoldKindWorktree,
			Files:      []string{"README.md"},
			Reason:     UncommittedCauseStillDirty,
			QuestionID: "q-2",
			HeldAt:     r.heldAtMin.Add(time.Minute),
		}
	})
	if err := m.Store().SavePRD(r.held); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return r
}

func TestUncommittedStatusListsHeldTasksAndAutomata(t *testing.T) {
	r := newUncommittedStatusRig(t)

	all := r.m.UncommittedWorkStatus("")
	if all.Policy != config.UncommittedWorkPolicyHold || all.Scope != config.UncommittedWorkScopeTaskFiles {
		t.Fatalf("policy/scope = %q/%q; want the defaults hold/task_files", all.Policy, all.Scope)
	}
	if len(all.Held) != 2 {
		t.Fatalf("held = %d entries; want 2 (the worktree hold and the task hold): %+v", len(all.Held), all.Held)
	}
	wt, task := all.Held[0], all.Held[1]
	if wt.Kind != UncommittedHoldKindWorktree || wt.TaskID != "" || wt.AutomatonID != r.held.ID || wt.Name != "Held Automaton" {
		t.Errorf("worktree entry = %+v", wt)
	}
	if len(wt.Files) != 1 || wt.Files[0] != "README.md" || wt.OutsideScope == nil || len(wt.OutsideScope) != 0 || wt.QuestionID != "q-2" {
		t.Errorf("worktree entry files/outside/question = %+v", wt)
	}
	if task.Kind != UncommittedHoldKindTask || task.TaskID != r.heldTask || task.TaskTitle != "Task two" {
		t.Errorf("task entry = %+v", task)
	}
	if len(task.Files) != 2 || len(task.OutsideScope) != 1 || task.OutsideScope[0] != "docs/b.md" ||
		task.Reason != UncommittedCauseStillDirty || task.QuestionID != "q-1" || !task.HeldAt.Equal(r.heldAtMin) {
		t.Errorf("task entry details = %+v", task)
	}

	// One Automaton by id; the other Automaton is not listed.
	one := r.m.UncommittedWorkStatus(r.held.ID)
	if len(one.Held) != 2 {
		t.Errorf("read by id = %d entries; want 2", len(one.Held))
	}
	if none := r.m.UncommittedWorkStatus(r.clean.ID); len(none.Held) != 0 {
		t.Errorf("clean Automaton read = %+v; want nothing held", none.Held)
	}

	// The policy and scope follow the live config.
	cfg := r.m.Config()
	cfg.Automation.UncommittedWorkPolicy = config.UncommittedWorkPolicyWarn
	cfg.Automation.UncommittedWorkScope = config.UncommittedWorkScopeWorktree
	r.m.SetConfig(cfg)
	live := r.m.UncommittedWorkStatus("")
	if live.Policy != "warn" || live.Scope != "worktree" {
		t.Errorf("after a config change policy/scope = %q/%q; want warn/worktree", live.Policy, live.Scope)
	}
}

func TestUncommittedStatusEmptyWhenNothingHeld(t *testing.T) {
	r := newUncommittedStatusRig(t)
	for _, id := range []string{r.clean.ID, "no-such-automaton"} {
		st := r.m.UncommittedWorkStatus(id)
		if st.Held == nil || len(st.Held) != 0 {
			t.Fatalf("read of %q: held = %#v; want an empty list", id, st.Held)
		}
		b, err := json.Marshal(st)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if !strings.Contains(string(b), `"held":[]`) {
			t.Errorf("read of %q: json = %s; want \"held\":[]", id, b)
		}
	}

	var nilMgr *Manager
	if st := nilMgr.UncommittedWorkStatus(""); st.Held == nil || len(st.Held) != 0 {
		t.Errorf("nil manager read = %#v; want an empty list", st.Held)
	}
}

func TestUncommittedStatusRenderText(t *testing.T) {
	r := newUncommittedStatusRig(t)
	st := r.m.UncommittedWorkStatus("")
	text := RenderUncommittedWorkStatus(st)
	for _, want := range []string{
		"policy: hold", "scope: task_files", "2 held",
		"Automaton Held Automaton", "Task two", "whole worktree",
		"a.go, docs/b.md", "outside the task's declared files: docs/b.md",
		"README.md", UncommittedCauseStillDirty, "question: q-1", "question: q-2",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "PRD") {
		t.Errorf("text says PRD:\n%s", text)
	}

	// The CLI and the comm verb render the body they read over REST: the text
	// from a parsed body must equal the text from the model.
	body, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	parsed, err := ParseUncommittedWorkStatus(body)
	if err != nil {
		t.Fatalf("ParseUncommittedWorkStatus: %v", err)
	}
	if got := RenderUncommittedWorkStatus(parsed); got != text {
		t.Errorf("text after a parse round trip differs:\n%s\nvs\n%s", got, text)
	}

	empty := RenderUncommittedWorkStatus(r.m.UncommittedWorkStatus(r.clean.ID))
	if !strings.Contains(empty, "Nothing is held") || strings.Contains(empty, "PRD") {
		t.Errorf("empty text = %q", empty)
	}

	if _, err := ParseUncommittedWorkStatus([]byte("not json")); err == nil {
		t.Error("a body that is not JSON must be an error")
	}
	if got, err := ParseUncommittedWorkStatus([]byte(`{"policy":"hold","scope":"task_files"}`)); err != nil || got.Held == nil {
		t.Errorf("a body with no held key = %#v, %v; want an empty list", got.Held, err)
	}
}
