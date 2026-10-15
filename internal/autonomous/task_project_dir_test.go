// BL447 — per-task ProjectDir override: resolver, scope helpers, setter.

package autonomous

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func newTaskDirManager(t *testing.T) (*Manager, *PRD) {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("test spec", "/srv/proj", "ollama", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	prd.Story = []Story{{ID: "s1", Title: "story", Tasks: []Task{{ID: "t1", Title: "task"}}}}
	prd.Status = PRDNeedsReview
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	return m, prd
}

func TestResolveTaskProjectDir(t *testing.T) {
	prd := &PRD{ProjectDir: "/srv/proj"}
	cases := []struct {
		name string
		prd  *PRD
		task *Task
		want string
	}{
		{"nil task", prd, nil, "/srv/proj"},
		{"empty override", prd, &Task{}, "/srv/proj"},
		{"set override", prd, &Task{ProjectDir: "/srv/other"}, "/srv/other"},
		{"override cleaned", prd, &Task{ProjectDir: "/srv/other/./sub/../x/"}, "/srv/other/x"},
		{"nil prd nil task", nil, nil, ""},
		{"nil prd with override", nil, &Task{ProjectDir: "/a/b"}, "/a/b"},
	}
	m := &Manager{}
	for _, c := range cases {
		if got := ResolveTaskProjectDir(c.prd, c.task); got != c.want {
			t.Errorf("%s: ResolveTaskProjectDir = %q; want %q", c.name, got, c.want)
		}
		if got := m.resolveTaskProjectDir(c.prd, c.task); got != c.want {
			t.Errorf("%s: resolveTaskProjectDir = %q; want %q", c.name, got, c.want)
		}
	}
}

func TestTaskScope_NoOverrideMatchesPRD(t *testing.T) {
	prds := []*PRD{
		{ProjectDir: "/srv/proj"},
		{ProjectDir: "/srv/proj", ReadDirs: []string{"/srv/ref", "/srv/proj/sub"}},
		{ProjectDir: "/srv/proj", WriteDirs: []string{"/srv/w1", "/srv/w2"}, ReadDirs: []string{"/srv/ref"}},
		{},
	}
	for i, prd := range prds {
		for _, task := range []*Task{nil, {}, {ProjectDir: prd.ProjectDir}} {
			if got, want := EffectiveTaskWriteDirs(prd, task), EffectiveWriteDirs(prd); !reflect.DeepEqual(got, want) {
				t.Errorf("prd %d: write dirs %v; want %v", i, got, want)
			}
			if got, want := EffectiveTaskReadDirs(prd, task), EffectiveReadDirs(prd); !reflect.DeepEqual(got, want) {
				t.Errorf("prd %d: read dirs %v; want %v", i, got, want)
			}
			if got, want := TaskScopeBlock(prd, task), ScopeBlock(prd); got != want {
				t.Errorf("prd %d: scope block\n%s\nwant\n%s", i, got, want)
			}
		}
	}
}

func TestScopeBlock_Unchanged(t *testing.T) {
	prd := &PRD{ProjectDir: "/srv/proj", ReadDirs: []string{"/srv/ref", "/srv/proj/sub"}}
	want := "SCOPE (hard boundary):\n" +
		"- Project directory: /srv/proj\n" +
		"- You may WRITE only under: /srv/proj\n" +
		"- You may READ (never modify) under: /srv/ref\n" +
		"- Do not read, write or list anything outside these directories.\n" +
		"- All output files, notes and checkpoints go under the project directory, using paths relative to it.\n"
	if got := ScopeBlock(prd); got != want {
		t.Fatalf("ScopeBlock changed:\n%s", got)
	}
}

func TestTaskScope_WithOverride(t *testing.T) {
	prd := &PRD{ProjectDir: "/srv/proj", ReadDirs: []string{"/srv/ref"}}
	task := &Task{ProjectDir: "/srv/proj/sub/"}
	if got := EffectiveTaskWriteDirs(prd, task); !reflect.DeepEqual(got, []string{"/srv/proj/sub"}) {
		t.Errorf("write dirs = %v", got)
	}
	if got := EffectiveTaskReadDirs(prd, task); !reflect.DeepEqual(got, []string{"/srv/ref", "/srv/proj/sub"}) {
		t.Errorf("read dirs = %v", got)
	}
	block := TaskScopeBlock(prd, task)
	for _, want := range []string{
		"- Project directory: /srv/proj/sub\n",
		"- You may WRITE only under: /srv/proj/sub\n",
		"- You may READ (never modify) under: /srv/ref\n",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("block missing %q:\n%s", want, block)
		}
	}

	// Declared WriteDirs win over the task override for write scope.
	prd.WriteDirs = []string{"/srv/w"}
	task.ProjectDir = "/srv/w/a"
	if got := EffectiveTaskWriteDirs(prd, task); !reflect.DeepEqual(got, []string{"/srv/w"}) {
		t.Errorf("write dirs with WriteDirs = %v", got)
	}
	block = TaskScopeBlock(prd, task)
	if !strings.Contains(block, "- Project directory: /srv/w/a\n") || !strings.Contains(block, "- You may WRITE only under: /srv/w\n") {
		t.Errorf("block with WriteDirs:\n%s", block)
	}
}

func TestValidateTaskProjectDir(t *testing.T) {
	prd := &PRD{ProjectDir: "/srv/proj"}
	for _, ok := range []string{"/srv/proj/sub", "/home/u/x", "/srv/other"} {
		if err := ValidateTaskProjectDir(prd, ok); err != nil {
			t.Errorf("%q: unexpected error %v", ok, err)
		}
	}
	for _, bad := range []string{"relative/dir", "", "/", "/etc", "/home", "/tmp/"} {
		if err := ValidateTaskProjectDir(prd, bad); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
	prd.WriteDirs = []string{"/srv/w"}
	if err := ValidateTaskProjectDir(prd, "/srv/w/a"); err != nil {
		t.Errorf("inside write dirs: %v", err)
	}
	if err := ValidateTaskProjectDir(prd, "/srv/proj"); err == nil {
		t.Error("outside write dirs: expected error")
	}
}

func TestSetTaskProjectDir(t *testing.T) {
	m, prd := newTaskDirManager(t)

	out, err := m.SetTaskProjectDir(prd.ID, "t1", "/srv/proj/sub/", "tester")
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if got := out.Story[0].Tasks[0].ProjectDir; got != "/srv/proj/sub" {
		t.Fatalf("ProjectDir = %q", got)
	}
	stored, _ := m.Store().GetPRD(prd.ID)
	if got := stored.Story[0].Tasks[0].ProjectDir; got != "/srv/proj/sub" {
		t.Fatalf("stored ProjectDir = %q", got)
	}
	last := stored.Decisions[len(stored.Decisions)-1]
	if last.Kind != "set_task_project_dir" || last.Actor != "tester" || !strings.Contains(last.Note, "project_dir=/srv/proj/sub") {
		t.Fatalf("decision = %+v", last)
	}

	if _, err := m.SetTaskProjectDir(prd.ID, "t1", "", "tester"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	stored, _ = m.Store().GetPRD(prd.ID)
	if got := stored.Story[0].Tasks[0].ProjectDir; got != "" {
		t.Fatalf("cleared ProjectDir = %q", got)
	}
	if got := ResolveTaskProjectDir(stored, &stored.Story[0].Tasks[0]); got != "/srv/proj" {
		t.Fatalf("resolved after clear = %q", got)
	}

	if _, err := m.SetTaskProjectDir(prd.ID, "t1", "rel/dir", "tester"); err == nil {
		t.Fatal("invalid path: expected error")
	}
	if _, err := m.SetTaskProjectDir(prd.ID, "nope", "/srv/x", "tester"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unknown task: err = %v", err)
	}
	if _, err := m.SetTaskProjectDir("missing", "t1", "/srv/x", "tester"); err == nil {
		t.Fatal("unknown prd: expected error")
	}

	stored.Status = PRDRunning
	if err := m.Store().SavePRD(stored); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetTaskProjectDir(prd.ID, "t1", "/srv/x", "tester"); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("locked: err = %v", err)
	}
}

func TestAPISetTaskProjectDir(t *testing.T) {
	m, prd := newTaskDirManager(t)
	a := &API{M: m}
	if _, err := a.SetTaskProjectDir(prd.ID, "t1", "/srv/a", "api"); err != nil {
		t.Fatalf("api set: %v", err)
	}
	stored, _ := m.Store().GetPRD(prd.ID)
	if stored.Story[0].Tasks[0].ProjectDir != "/srv/a" {
		t.Fatalf("api did not persist: %q", stored.Story[0].Tasks[0].ProjectDir)
	}
}

func TestTaskProjectDir_JSON(t *testing.T) {
	b, err := json.Marshal(Task{ID: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "project_dir") {
		t.Fatalf("empty ProjectDir not omitted: %s", b)
	}
	b, err = json.Marshal(Task{ID: "t1", ProjectDir: "/srv/x"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"project_dir":"/srv/x"`) {
		t.Fatalf("missing project_dir: %s", b)
	}
	var back Task
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.ProjectDir != "/srv/x" {
		t.Fatalf("round-trip = %q", back.ProjectDir)
	}
}
