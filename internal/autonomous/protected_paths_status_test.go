package autonomous

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// protectedStatusRig is a Manager with three Automata: "Alpha Automaton" with
// two held tasks and an authorised list, "Beta Automaton" with one held task,
// and "Gamma Automaton" with only an authorised list.
type protectedStatusRig struct {
	m                   *Manager
	alpha, beta, gamma  *PRD
	alphaT1, alphaT2    string
	betaT1              string
	sinceOne, sinceTwo  time.Time
	sortedIDs           []string
	sortedAlphaTaskIDs  []string
	sortedAuthorizedIDs []string
}

func newProtectedStatusRig(t *testing.T) *protectedStatusRig {
	t.Helper()
	m, _ := newFailureEscalationFixture(t, t.TempDir())
	mk := func(title string, tasks ...string) *PRD {
		prd, err := m.CreatePRD("test spec", t.TempDir(), "opencode", "", EffortNormal)
		if err != nil {
			t.Fatalf("CreatePRD: %v", err)
		}
		var ts []Task
		for _, name := range tasks {
			ts = append(ts, Task{Title: name, Spec: "do " + name})
		}
		if err := m.Store().SetStories(prd.ID, []Story{{Title: "Story", Tasks: ts}}); err != nil {
			t.Fatalf("SetStories: %v", err)
		}
		prd, _ = m.Store().GetPRD(prd.ID)
		prd.Title = title
		if err := m.Store().SavePRD(prd); err != nil {
			t.Fatalf("SavePRD: %v", err)
		}
		return prd
	}
	r := &protectedStatusRig{m: m}
	r.alpha = mk("Alpha Automaton", "Task one", "Task two")
	r.beta = mk("Beta Automaton", "Task three")
	r.gamma = mk("Gamma Automaton", "Task four")
	r.alphaT1 = r.alpha.Story[0].Tasks[0].ID
	r.alphaT2 = r.alpha.Story[0].Tasks[1].ID
	r.betaT1 = r.beta.Story[0].Tasks[0].ID
	r.sinceOne = time.Date(2026, 11, 5, 9, 30, 0, 0, time.UTC)
	r.sinceTwo = r.sinceOne.Add(time.Hour)

	m.Store().WithLock(func() {
		r.alpha.Story[0].Tasks[0].ProtectedPathHold = &ProtectedPathHold{
			Paths:      []string{"docs/b.md", "AGENT.md"},
			Action:     config.ProtectedPathsActionHold,
			Reason:     "the task commit changed protected paths",
			QuestionID: "q-1",
			BaseCommit: "abc123",
			Since:      r.sinceOne,
		}
		r.alpha.Story[0].Tasks[1].ProtectedPathHold = &ProtectedPathHold{
			Paths:              []string{".github/workflows/ci.yml"},
			Action:             config.ProtectedPathsActionHold,
			Since:              r.sinceTwo,
			AwaitingSpecChange: true,
		}
		r.beta.Story[0].Tasks[0].ProtectedPathHold = &ProtectedPathHold{
			Paths:  []string{"CODEOWNERS"},
			Action: config.ProtectedPathsActionHold,
			Since:  r.sinceTwo,
		}
	})
	for _, p := range []*PRD{r.alpha, r.beta} {
		if err := m.Store().SavePRD(p); err != nil {
			t.Fatalf("SavePRD: %v", err)
		}
	}
	for _, task := range []*Task{&r.alpha.Story[0].Tasks[0], &r.alpha.Story[0].Tasks[1], &r.beta.Story[0].Tasks[0]} {
		if err := m.Store().SaveTask(task); err != nil {
			t.Fatalf("SaveTask: %v", err)
		}
	}
	if _, err := m.SetAuthorizedProtectedPaths(r.alpha.ID, []string{"docs/plans/", "AGENT.md"}, "operator"); err != nil {
		t.Fatalf("SetAuthorizedProtectedPaths: %v", err)
	}
	if _, err := m.SetAuthorizedProtectedPaths(r.gamma.ID, []string{"CODEOWNERS"}, "operator"); err != nil {
		t.Fatalf("SetAuthorizedProtectedPaths: %v", err)
	}

	r.sortedIDs = []string{r.alpha.ID, r.beta.ID, r.gamma.ID}
	sort.Strings(r.sortedIDs)
	r.sortedAuthorizedIDs = []string{r.alpha.ID, r.gamma.ID}
	sort.Strings(r.sortedAuthorizedIDs)
	r.sortedAlphaTaskIDs = []string{r.alphaT1, r.alphaT2}
	sort.Strings(r.sortedAlphaTaskIDs)
	return r
}

func TestProtectedPathsStatusHeldEntryPerHeldTask(t *testing.T) {
	r := newProtectedStatusRig(t)
	st := r.m.ProtectedPathsStatus("")
	if len(st.Held) != 3 {
		t.Fatalf("held = %d entries; want 3 (one per held task): %+v", len(st.Held), st.Held)
	}
	var one *ProtectedPathsHeldEntry
	var two *ProtectedPathsHeldEntry
	var three *ProtectedPathsHeldEntry
	for i := range st.Held {
		switch st.Held[i].TaskID {
		case r.alphaT1:
			one = &st.Held[i]
		case r.alphaT2:
			two = &st.Held[i]
		case r.betaT1:
			three = &st.Held[i]
		}
	}
	if one == nil || two == nil || three == nil {
		t.Fatalf("missing held entries: %+v", st.Held)
	}
	if one.AutomatonID != r.alpha.ID || one.AutomatonTitle != "Alpha Automaton" || one.TaskTitle != "Task one" {
		t.Errorf("entry one identity = %+v", *one)
	}
	if !reflect.DeepEqual(one.Paths, []string{"AGENT.md", "docs/b.md"}) {
		t.Errorf("entry one paths = %v; want them sorted", one.Paths)
	}
	if one.Action != "hold" || one.Reason != "the task commit changed protected paths" || one.QuestionID != "q-1" ||
		one.BaseCommit != "abc123" || !one.Since.Equal(r.sinceOne) || one.AwaitingSpecChange {
		t.Errorf("entry one details = %+v", *one)
	}
	if two.AwaitingSpecChange != true || two.TaskTitle != "Task two" || len(two.Paths) != 1 || two.Paths[0] != ".github/workflows/ci.yml" {
		t.Errorf("entry two = %+v", *two)
	}
	if three.AutomatonID != r.beta.ID || three.AutomatonTitle != "Beta Automaton" || three.Paths[0] != "CODEOWNERS" || three.QuestionID != "" {
		t.Errorf("entry three = %+v", *three)
	}

	// The JSON keys are the ones the route documents.
	b, err := json.Marshal(*one)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{"automaton_id", "automaton_title", "task_id", "task_title", "paths", "action",
		"reason", "question_id", "since", "base_commit", "awaiting_spec_change"} {
		if !strings.Contains(string(b), `"`+key+`"`) {
			t.Errorf("held entry json lacks %q: %s", key, b)
		}
	}

	// A task with no hold is not listed, and a cleared hold drops out.
	r.m.Store().WithLock(func() { r.alpha.Story[0].Tasks[1].ProtectedPathHold = nil })
	if err := r.m.Store().SaveTask(&r.alpha.Story[0].Tasks[1]); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}
	if got := len(r.m.ProtectedPathsStatus("").Held); got != 2 {
		t.Errorf("after clearing one hold: %d entries; want 2", got)
	}
}

func TestProtectedPathsStatusAuthorizedEntries(t *testing.T) {
	r := newProtectedStatusRig(t)
	st := r.m.ProtectedPathsStatus("")
	if len(st.Authorized) != 2 {
		t.Fatalf("authorized = %d entries; want 2 (Alpha and Gamma): %+v", len(st.Authorized), st.Authorized)
	}
	for _, e := range st.Authorized {
		switch e.AutomatonID {
		case r.alpha.ID:
			if e.AutomatonTitle != "Alpha Automaton" || !reflect.DeepEqual(e.Paths, []string{"AGENT.md", "docs/plans/"}) {
				t.Errorf("alpha authorised entry = %+v", e)
			}
		case r.gamma.ID:
			if e.AutomatonTitle != "Gamma Automaton" || !reflect.DeepEqual(e.Paths, []string{"CODEOWNERS"}) {
				t.Errorf("gamma authorised entry = %+v", e)
			}
		default:
			t.Errorf("unexpected authorised entry %+v (Beta authorises nothing)", e)
		}
	}
	b, err := json.Marshal(st.Authorized[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{"automaton_id", "automaton_title", "paths"} {
		if !strings.Contains(string(b), `"`+key+`"`) {
			t.Errorf("authorised entry json lacks %q: %s", key, b)
		}
	}

	// Clearing the list drops the entry.
	if _, err := r.m.SetAuthorizedProtectedPaths(r.alpha.ID, nil, "operator"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	st = r.m.ProtectedPathsStatus("")
	if len(st.Authorized) != 1 || st.Authorized[0].AutomatonID != r.gamma.ID {
		t.Errorf("after clearing Alpha: %+v", st.Authorized)
	}
}

func TestProtectedPathsStatusFilter(t *testing.T) {
	r := newProtectedStatusRig(t)

	alpha := r.m.ProtectedPathsStatus(r.alpha.ID)
	if len(alpha.Held) != 2 || len(alpha.Authorized) != 1 {
		t.Fatalf("Alpha read: held=%d authorized=%d; want 2 and 1", len(alpha.Held), len(alpha.Authorized))
	}
	for _, h := range alpha.Held {
		if h.AutomatonID != r.alpha.ID {
			t.Errorf("Alpha read lists a task of %s", h.AutomatonID)
		}
	}
	if alpha.Authorized[0].AutomatonID != r.alpha.ID {
		t.Errorf("Alpha read lists an authorisation of %s", alpha.Authorized[0].AutomatonID)
	}

	beta := r.m.ProtectedPathsStatus(r.beta.ID)
	if len(beta.Held) != 1 || len(beta.Authorized) != 0 {
		t.Errorf("Beta read: held=%d authorized=%d; want 1 and 0", len(beta.Held), len(beta.Authorized))
	}
	gamma := r.m.ProtectedPathsStatus(r.gamma.ID)
	if len(gamma.Held) != 0 || len(gamma.Authorized) != 1 {
		t.Errorf("Gamma read: held=%d authorized=%d; want 0 and 1", len(gamma.Held), len(gamma.Authorized))
	}

	// The settings do not depend on the filter.
	if alpha.Action != r.m.ProtectedPathsStatus("").Action || !reflect.DeepEqual(alpha.Paths, r.m.ProtectedPathsStatus("").Paths) {
		t.Errorf("the filter changed the settings: %+v", alpha)
	}

	// An id that matches nothing reads empty lists, never an error and never null.
	none := r.m.ProtectedPathsStatus("no-such-automaton")
	if none.Held == nil || len(none.Held) != 0 || none.Authorized == nil || len(none.Authorized) != 0 || none.Paths == nil {
		t.Errorf("unknown id read = %#v; want empty lists", none)
	}
}

func TestProtectedPathsStatusDeterministicOrder(t *testing.T) {
	r := newProtectedStatusRig(t)
	first := r.m.ProtectedPathsStatus("")

	// Held: Automaton id, then task id.
	var wantHeld []string
	for _, id := range r.sortedIDs {
		switch id {
		case r.alpha.ID:
			for _, tid := range r.sortedAlphaTaskIDs {
				wantHeld = append(wantHeld, id+"/"+tid)
			}
		case r.beta.ID:
			wantHeld = append(wantHeld, id+"/"+r.betaT1)
		}
	}
	var gotHeld []string
	for _, h := range first.Held {
		gotHeld = append(gotHeld, h.AutomatonID+"/"+h.TaskID)
	}
	if !reflect.DeepEqual(gotHeld, wantHeld) {
		t.Errorf("held order = %v; want %v", gotHeld, wantHeld)
	}
	var gotAuth []string
	for _, a := range first.Authorized {
		gotAuth = append(gotAuth, a.AutomatonID)
	}
	if !reflect.DeepEqual(gotAuth, r.sortedAuthorizedIDs) {
		t.Errorf("authorised order = %v; want %v", gotAuth, r.sortedAuthorizedIDs)
	}

	// Repeated reads are byte-identical.
	want, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for i := 0; i < 20; i++ {
		got, err := json.Marshal(r.m.ProtectedPathsStatus(""))
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if string(got) != string(want) {
			t.Fatalf("read %d differs:\n%s\nvs\n%s", i, got, want)
		}
	}
}

func TestProtectedPathsStatusNoGitCall(t *testing.T) {
	r := newProtectedStatusRig(t)
	// Every git call goes through the manager's runner; one that fails the
	// test shows the read never reaches git.
	r.m.driftGit = func(_ context.Context, dir string, args ...string) (string, error) {
		t.Errorf("a status read ran git in %q: %v", dir, args)
		return "", nil
	}
	for _, id := range []string{"", r.alpha.ID, r.beta.ID, "no-such-automaton"} {
		r.m.ProtectedPathsStatus(id)
	}
}

func TestProtectedPathsStatusSettingsAreEffective(t *testing.T) {
	r := newProtectedStatusRig(t)

	// Unset settings read their defaults: hold, base_commit and the default
	// list built on the rules file.
	cfg := r.m.Config()
	cfg.Automation.ProtectedPaths = nil
	cfg.Automation.ProtectedPathsAction = ""
	cfg.Automation.RulesReviewSource = ""
	cfg.RulesFile = ""
	r.m.SetConfig(cfg)
	st := r.m.ProtectedPathsStatus("")
	if st.Action != config.ProtectedPathsActionHold || st.RulesSource != config.RulesReviewSourceBaseCommit {
		t.Errorf("defaults = %q/%q; want hold/base_commit", st.Action, st.RulesSource)
	}
	if want := config.DefaultProtectedPaths("AGENT.md"); !reflect.DeepEqual(st.Paths, want) {
		t.Errorf("default paths = %v; want %v", st.Paths, want)
	}

	// A custom rules file name shows up in the default list.
	cfg.RulesFile = "RULES.md"
	r.m.SetConfig(cfg)
	if got := r.m.ProtectedPathsStatus("").Paths; len(got) == 0 || got[0] != "RULES.md" {
		t.Errorf("paths with rules file RULES.md = %v; want RULES.md first", got)
	}

	// Explicit settings follow the live config; an unknown word reads the default.
	cfg.Automation.ProtectedPaths = []string{"./secrets/", "deploy/*.yaml"}
	cfg.Automation.ProtectedPathsAction = config.ProtectedPathsActionBlock
	cfg.Automation.RulesReviewSource = config.RulesReviewSourceWorktree
	r.m.SetConfig(cfg)
	st = r.m.ProtectedPathsStatus("")
	if st.Action != "block" || st.RulesSource != "worktree" {
		t.Errorf("explicit = %q/%q; want block/worktree", st.Action, st.RulesSource)
	}
	if !reflect.DeepEqual(st.Paths, []string{"secrets/", "deploy/*.yaml"}) {
		t.Errorf("explicit paths = %v", st.Paths)
	}
	cfg.Automation.ProtectedPathsAction = "nonsense"
	r.m.SetConfig(cfg)
	if got := r.m.ProtectedPathsStatus("").Action; got != "hold" {
		t.Errorf("unknown action reads %q; want hold", got)
	}
}

func TestProtectedPathsStatusEmptyListsNotNull(t *testing.T) {
	m, _ := newFailureEscalationFixture(t, t.TempDir())
	st := m.ProtectedPathsStatus("")
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{`"held":[]`, `"authorized":[]`, `"action":`, `"rules_source":`, `"paths":[`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("json lacks %s: %s", want, b)
		}
	}
	if strings.Contains(string(b), "null") {
		t.Errorf("json carries null: %s", b)
	}

	var nilMgr *Manager
	nb, err := json.Marshal(nilMgr.ProtectedPathsStatus(""))
	if err != nil {
		t.Fatalf("marshal nil manager read: %v", err)
	}
	if strings.Contains(string(nb), "null") {
		t.Errorf("nil manager read carries null: %s", nb)
	}
}
