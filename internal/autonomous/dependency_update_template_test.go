package autonomous

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestDependencyUpdateTemplateParsesWithSixOrderedTasks(t *testing.T) {
	d, err := parseDependencyUpdateData()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Find outdated", "Update minor", "Add one task per major", "Fix breaking", "Run the full checks", "Write the results"}
	if len(d.Tasks) != len(want) {
		t.Fatalf("tasks = %d, want %d", len(d.Tasks), len(want))
	}
	for i, w := range want {
		if !strings.HasPrefix(d.Tasks[i].Title, w) {
			t.Errorf("task %d title %q, want prefix %q", i+1, d.Tasks[i].Title, w)
		}
	}
	if d.ID != "dependency_update" || d.Title != "Weekly dependency update" {
		t.Fatalf("id/title = %q/%q", d.ID, d.Title)
	}
}

func TestDependencyUpdateTemplateSeedsOnceAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	ts := newTemplateStore(dir)
	got, ok := ts.Get(TemplateDependencyUpdate)
	if !ok || !got.IsBuiltin {
		t.Fatalf("not seeded as built-in: %+v", got)
	}
	n := len(ts.List())
	created := got.CreatedAt
	ts.seedBuiltinTemplates()
	ts2 := newTemplateStore(dir)
	if len(ts2.List()) != n {
		t.Fatalf("count changed %d -> %d", n, len(ts2.List()))
	}
	g2, _ := ts2.Get(TemplateDependencyUpdate)
	if !g2.CreatedAt.Equal(created) {
		t.Fatal("reseed replaced the stored template")
	}
}

func TestDependencyUpdateTemplateKeepsOperatorEdit(t *testing.T) {
	dir := t.TempDir()
	ts := newTemplateStore(dir)
	g, _ := ts.Get(TemplateDependencyUpdate)
	g.Spec = "operator edited"
	g.Title = "My title"
	_ = ts.persist()
	ts2 := newTemplateStore(dir)
	g2, _ := ts2.Get(TemplateDependencyUpdate)
	if g2.Spec != "operator edited" || g2.Title != "My title" {
		t.Fatalf("operator edit lost: %+v", g2)
	}
}

func TestDependencyUpdateTemplateInstantiates(t *testing.T) {
	ts := newTemplateStore(t.TempDir())
	spec, title, err := ts.Instantiate(TemplateDependencyUpdate, map[string]string{"max_majors": "5"})
	if err != nil {
		t.Fatal(err)
	}
	if title != "Weekly dependency update" || !strings.Contains(spec, "at most 5 major") {
		t.Fatalf("bad instantiate: %q", spec)
	}
}

func TestDependencyUpdateTemplatePassesDefaultLintPack(t *testing.T) {
	tmpl, err := DependencyUpdateTemplate()
	if err != nil {
		t.Fatal(err)
	}
	d, _ := parseDependencyUpdateData()
	story := Story{ID: "s1", PRDID: "p1", Status: StoryPending}
	for i, task := range d.Tasks {
		story.Tasks = append(story.Tasks, Task{
			ID: "t" + string(rune('1'+i)), StoryID: "s1", PRDID: "p1",
			Title: task.Title, Spec: strings.ReplaceAll(task.Spec, "{{max_majors}}", "3"),
			Status: TaskQueued,
		})
	}
	findings := LintTasks([]Story{story}, DefaultDecomposeLintRules())
	for _, f := range findings {
		if f.Severity == config.DecomposeLintSeverityBlock {
			t.Errorf("block finding on %s: rule %s: %s (matched %q)", f.Task, f.RuleID, f.Message, f.Matched)
		}
	}
	for _, bad := range []string{"PRD", "BL4", "gh "} {
		if strings.Contains(tmpl.Spec, bad) {
			t.Errorf("template text contains %q", bad)
		}
	}
	if !strings.Contains(tmpl.Spec, "no bypass") {
		t.Error("protected paths wording missing")
	}
}
