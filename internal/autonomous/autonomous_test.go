// BL24+BL25 — autonomous package unit tests.

package autonomous

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- store ---

func TestStore_CreatePRD_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	st, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	prd, err := st.CreatePRD("add login", "/proj", "claude-code", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if prd.ID == "" || prd.Status != PRDDraft {
		t.Fatalf("prd: %+v", prd)
	}
	// Reload and confirm persistence.
	st2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, ok := st2.GetPRD(prd.ID)
	if !ok || got.Spec != "add login" {
		t.Fatalf("not persisted: %+v ok=%v", got, ok)
	}
}

func TestStore_CreatePRD_RejectsEmptySpec(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewStore(dir)
	if _, err := st.CreatePRD("   ", "/proj", "", ""); err == nil {
		t.Fatalf("want error for empty spec")
	}
}

func TestStore_SetStories_AssignsIDs(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewStore(dir)
	prd, _ := st.CreatePRD("spec", "/p", "", "")
	stories := []Story{{
		Title: "S1",
		Tasks: []Task{{Title: "T1", Spec: "do thing"}},
	}}
	if err := st.SetStories(prd.ID, stories); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	got, _ := st.GetPRD(prd.ID)
	if len(got.Story) != 1 || got.Story[0].ID == "" {
		t.Fatalf("story not persisted: %+v", got.Story)
	}
	if len(got.Story[0].Tasks) != 1 || got.Story[0].Tasks[0].ID == "" {
		t.Fatalf("task ID not assigned: %+v", got.Story[0].Tasks)
	}
}

func TestStore_AddLearning(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewStore(dir)
	if err := st.AddLearning(Learning{TaskID: "t1", Text: "use composition"}); err != nil {
		t.Fatalf("AddLearning: %v", err)
	}
	if got := st.ListLearnings(); len(got) != 1 || got[0].Text != "use composition" {
		t.Fatalf("listLearnings: %+v", got)
	}
}

// --- decompose / parser ---

func TestParseDecomposition_Plain(t *testing.T) {
	in := `{"title":"Auth","stories":[{"title":"S","tasks":[{"title":"T","spec":"do"}]}]}`
	title, stories, err := ParseDecomposition(in)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if title != "Auth" || len(stories) != 1 || stories[0].Title != "S" {
		t.Fatalf("got title=%q stories=%+v", title, stories)
	}
}

func TestParseDecomposition_FencesAndComments(t *testing.T) {
	in := "```json\n{\"title\":\"X\",\"stories\":[]} // trailing comment\n```"
	title, _, err := ParseDecomposition(in)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if title != "X" {
		t.Fatalf("title=%q", title)
	}
}

func TestStripLineComment_RespectsURLsInStrings(t *testing.T) {
	got := stripLineComment(`"https://example.com/x" // real comment`)
	if !strings.Contains(got, "https://example.com/x") {
		t.Fatalf("URL stripped wrongly: %q", got)
	}
	if strings.Contains(got, "real comment") {
		t.Fatalf("comment not stripped: %q", got)
	}
}

// --- security scan ---

func TestSecurityScan_FindsDangerousPatterns(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.py")
	_ = os.WriteFile(bad, []byte("import os\nos.system('rm -rf /tmp/x')\n"), 0644)
	findings, err := SecurityScan(dir)
	if err != nil {
		t.Fatalf("SecurityScan: %v", err)
	}
	if len(findings) == 0 {
		t.Fatalf("expected findings on os.system pattern")
	}
}

func TestSecurityScan_CleanFileNoFindings(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "ok.py"),
		[]byte("def main():\n    return 0\n"), 0644)
	got, err := SecurityScan(dir)
	if err != nil {
		t.Fatalf("SecurityScan: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("clean file should produce zero findings: %+v", got)
	}
}

// --- manager ---

func TestManager_DecomposeWiresParsedStoriesIntoStore(t *testing.T) {
	dir := t.TempDir()
	fakeLLM := func(req DecomposeRequest) (string, error) {
		return `{"title":"Feature","stories":[{"title":"S1","tasks":[{"title":"T1","spec":"x"}]}]}`, nil
	}
	m, err := NewManager(dir, DefaultConfig(), fakeLLM)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, _ := m.CreatePRD("build login", "/p", "claude-code", EffortNormal)
	got, err := m.Decompose(prd.ID)
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	// BL191 (v5.2.0) — Decompose now lands in needs_review, not active.
	if got.Title != "Feature" || got.Status != PRDNeedsReview {
		t.Fatalf("post-decompose: %+v", got)
	}
	if len(got.Story) != 1 || got.Story[0].Tasks[0].Spec != "x" {
		t.Fatalf("stories: %+v", got.Story)
	}
}

func TestManager_StatusCounts(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(dir, DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "")
	prd.Status = PRDActive
	_ = m.Store().SavePRD(prd)
	_ = m.Store().SetStories(prd.ID, []Story{{
		Title: "S",
		Tasks: []Task{
			{Title: "a", Status: TaskQueued},
			{Title: "b", Status: TaskInProgress},
		},
	}})
	st := m.Status()
	if st.ActivePRDs != 1 || st.QueuedTasks != 1 || st.RunningTasks != 1 {
		t.Fatalf("status: %+v", st)
	}
}

// --- executor ---

func TestExecutor_RunsTasksInDependencyOrder(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(dir, DefaultConfig(), nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "")
	tasks := []Task{
		{Title: "first", Spec: "1"},
		{Title: "second", Spec: "2"},
	}
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: tasks}})
	prd, _ = m.Store().GetPRD(prd.ID)
	// Make second depend on first.
	prd.Story[0].Tasks[1].DependsOn = []string{prd.Story[0].Tasks[0].ID}
	// BL191 (v5.2.0) — Run requires PRDApproved. Tests skip the human gate
	// by flipping status directly.
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	var ran []string
	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		ran = append(ran, req.Title)
		return SpawnResult{SessionID: "s-" + req.TaskID}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "ok"}, nil
	}
	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(ran) != 2 || ran[0] != "first" || ran[1] != "second" {
		t.Fatalf("dependency order broken: %+v", ran)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.Status != PRDCompleted {
		t.Fatalf("PRD not completed: %s", got.Status)
	}
}

func TestExecutor_RetryOnVerifyFailure(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.AutoFixRetries = 2
	m, _ := NewManager(dir, cfg, nil)
	prd, _ := m.CreatePRD("spec", "/p", "", "")
	_ = m.Store().SetStories(prd.ID, []Story{{
		Title: "S",
		Tasks: []Task{{Title: "T", Spec: "do"}},
	}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	_ = m.Store().SavePRD(prd)

	spawnCount := 0
	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		spawnCount++
		return SpawnResult{SessionID: "s"}, nil
	}
	verifyCount := 0
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		verifyCount++
		// Pass on the third attempt.
		if verifyCount == 3 {
			return VerificationResult{OK: true, Summary: "ok"}, nil
		}
		return VerificationResult{OK: false, Summary: "still broken"}, nil
	}
	if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if spawnCount != 3 || verifyCount != 3 {
		t.Fatalf("retry count off: spawn=%d verify=%d", spawnCount, verifyCount)
	}
}

func TestTopoSort_DetectsCycles(t *testing.T) {
	tasks := []*Task{
		{ID: "a", DependsOn: []string{"b"}},
		{ID: "b", DependsOn: []string{"a"}},
	}
	if _, err := topoSort(tasks); err == nil {
		t.Fatalf("expected cycle error")
	}
}

// --- API adapter ---

func TestAPI_SetConfigUnmarshalsRawMessage(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewManager(dir, DefaultConfig(), nil)
	a := NewAPI(m)
	raw := json.RawMessage(`{"enabled":true,"max_parallel_tasks":7}`)
	if err := a.SetConfig(raw); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	got := m.Config()
	if !got.Enabled || got.MaxParallelTasks != 7 {
		t.Fatalf("config not applied: %+v", got)
	}
}
