package autonomous

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/genartifacts"
)

func gwTask(id, title string, kind string, files []string, deps ...string) Task {
	return Task{ID: id, Title: title, Kind: kind, FilesPlanned: files, DependsOn: deps}
}

func TestDropGeneratedTasks_EachKindDropped(t *testing.T) {
	cases := []struct {
		kind  genartifacts.Kind
		title string
		files []string
		key   string
	}{
		{genartifacts.GateReport, "Write the gate report", []string{"docs/plans/reports/x.md"}, "gate_report"},
		{genartifacts.ChangelogDraft, "Add the changelog entry", []string{"CHANGELOG.md"}, "changelog_draft"},
		{genartifacts.ParityDraft, "Draft the parity issue", []string{"docs/plans/parity-issues/a.md"}, "parity_draft"},
		{genartifacts.TrackerRows, "Add testing tracker rows", []string{"docs/testing-tracker.md"}, "tracker_rows"},
		{genartifacts.RegistryRow, "Add the registry row", []string{"docs/plans/README.md"}, "registry_row"},
	}
	for _, c := range cases {
		stories := []Story{{ID: "s1", Tasks: []Task{gwTask("a", "Add code", "", []string{"x.go"}), gwTask("g", c.title, "", c.files)}}}
		kept, dropped := DropGeneratedTasks(stories, config.GeneratedArtefacts{})
		if len(dropped) != 1 || dropped[0].Kind != c.kind || len(kept[0].Tasks) != 1 {
			t.Fatalf("%s: kept=%v dropped=%v", c.kind, kept, dropped)
		}
		if !strings.Contains(dropped[0].Reason, "generated_artefacts."+c.key+"=false") {
			t.Fatalf("reason: %s", dropped[0].Reason)
		}
	}
	// Title-only classification (no files).
	if k, ok := ClassifyGeneratedTask("Write the gate report", "", nil); !ok || k != genartifacts.GateReport {
		t.Fatalf("title-only: %v %v", k, ok)
	}
}

func TestDropGeneratedTasks_CodeTaskWithChangelogKept(t *testing.T) {
	stories := []Story{{ID: "s1", Tasks: []Task{gwTask("a", "Update changelog and code", "", []string{"CHANGELOG.md", "x.go"})}}}
	kept, dropped := DropGeneratedTasks(stories, config.GeneratedArtefacts{})
	if len(dropped) != 0 || len(kept[0].Tasks) != 1 {
		t.Fatal("code task must be kept")
	}
}

func TestDropGeneratedTasks_DisabledKindKept(t *testing.T) {
	off := false
	stories := []Story{{ID: "s1", Tasks: []Task{gwTask("g", "Write the gate report", "", []string{"docs/plans/reports/x.md"})}}}
	kept, dropped := DropGeneratedTasks(stories, config.GeneratedArtefacts{GateReport: &off})
	if len(dropped) != 0 || len(kept[0].Tasks) != 1 {
		t.Fatal("disabled kind must keep task")
	}
}

func TestDropGeneratedTasks_RepairsDepsAndEmptyStory(t *testing.T) {
	stories := []Story{
		{ID: "s1", Tasks: []Task{gwTask("a", "Add code", "", []string{"x.go"})}},
		{ID: "s2", DependsOn: []string{"s1"}, Tasks: []Task{gwTask("g", "Write the gate report", "", []string{"docs/plans/reports/x.md"}, "a")}},
		{ID: "s3", DependsOn: []string{"s2", "g"}, Tasks: []Task{gwTask("b", "More code", "", []string{"y.go"}, "g")}},
	}
	kept, dropped := DropGeneratedTasks(stories, config.GeneratedArtefacts{})
	if len(dropped) != 1 || len(kept) != 2 {
		t.Fatalf("kept=%d dropped=%d", len(kept), len(dropped))
	}
	if got := kept[1].Tasks[0].DependsOn; len(got) != 1 || got[0] != "a" {
		t.Fatalf("task deps: %v", got)
	}
	if got := kept[1].DependsOn; len(got) != 2 || got[0] != "s1" || got[1] != "a" {
		t.Fatalf("story deps: %v", got)
	}
}

func TestDropGeneratedTasks_NoGeneratedUnchanged(t *testing.T) {
	stories := []Story{{ID: "s1", Tasks: []Task{gwTask("a", "Add code", "", []string{"x.go"}, "b"), gwTask("b", "Add more", "", nil)}}}
	kept, dropped := DropGeneratedTasks(stories, config.GeneratedArtefacts{})
	if dropped != nil || len(kept) != 1 || len(kept[0].Tasks) != 2 || kept[0].Tasks[0].DependsOn[0] != "b" {
		t.Fatal("plan must be unchanged")
	}
}

func TestDropGeneratedTasks_GateAndSmokeNeverDropped(t *testing.T) {
	stories := []Story{{ID: "s1", Tasks: []Task{
		gwTask("f", "Write the gate report", TaskKindFinalGate, []string{"docs/plans/reports/x.md"}),
		gwTask("s", "Write the gate report", TaskKindSmoke, nil),
	}}}
	kept, dropped := DropGeneratedTasks(stories, config.GeneratedArtefacts{})
	if len(dropped) != 0 || len(kept[0].Tasks) != 2 {
		t.Fatal("final_gate/smoke must be kept")
	}
}

func TestClassifyGeneratedTask_CodeTasksNamingArtefactsKept(t *testing.T) {
	for _, title := range []string{
		"Add a changelog parser to the CLI",
		"Add tracker rows to the status table",
		"Fix the gate report renderer",
		"Implement the registry row sorter",
	} {
		if k, ok := ClassifyGeneratedTask(title, "", nil); ok {
			t.Fatalf("%q classified as %s", title, k)
		}
	}
	if _, ok := ClassifyGeneratedTask("Fix the scanner", "", []string{"docs/plans/reports/x.md"}); ok {
		t.Fatal("title not naming an artefact must be kept")
	}
	if _, ok := ClassifyGeneratedTask("Write the gate report", "Edit internal/x/render.go too", nil); ok {
		t.Fatal("spec naming source files must be kept")
	}
	if k, ok := ClassifyGeneratedTask("Update CHANGELOG.md for the release", "", []string{"CHANGELOG.md"}); !ok || k != genartifacts.ChangelogDraft {
		t.Fatal("changelog task should classify")
	}
}

func TestGeneratedArtefactGuidance_EnabledKinds(t *testing.T) {
	g := GeneratedArtefactGuidance(config.GeneratedArtefacts{}.Effective())
	for _, k := range generatedKindOrder {
		if !strings.Contains(g, generatedKindTable[k].noun) {
			t.Fatalf("missing %s: %s", k, g)
		}
	}
	if !strings.Contains(g, "at most one combined documentation task") {
		t.Fatal(g)
	}
}

func TestGeneratedArtefactGuidance_KindOffAllowed(t *testing.T) {
	off := false
	g := GeneratedArtefactGuidance(config.GeneratedArtefacts{ChangelogDraft: &off}.Effective())
	if strings.Contains(g, "changelog entry") || !strings.Contains(g, "the gate report") {
		t.Fatal(g)
	}
}

func TestLintCombinedDocs(t *testing.T) {
	three := []Story{{Tasks: []Task{
		gwTask("a", "Write the how-to", "", []string{"docs/howto/a.md"}),
		gwTask("b", "Update operator docs", "", []string{"docs/ops.md"}),
		gwTask("c", "Add definition docs", "", []string{"docs/def.md"}),
		gwTask("d", "Add code", "", []string{"x.go"}),
	}}}
	w := LintCombinedDocs(three)
	if len(w) != 1 || !strings.Contains(w[0], "a (") || !strings.Contains(w[0], "b (") || !strings.Contains(w[0], "c (") || strings.Contains(w[0], "d (") {
		t.Fatalf("%v", w)
	}
	one := []Story{{Tasks: []Task{gwTask("a", "Write the how-to", "", []string{"docs/howto/a.md"}), gwTask("d", "Add code", "", []string{"x.go"})}}}
	if w := LintCombinedDocs(one); len(w) != 0 {
		t.Fatalf("%v", w)
	}
}

func TestBuiltinTemplatesNoGeneratedKind(t *testing.T) {
	for _, tm := range builtinTaskTemplates {
		for _, k := range generatedKindOrder {
			if generatedKindTable[k].title.MatchString(tm.Title) {
				t.Fatalf("%s contains %s task", tm.Title, k)
			}
		}
	}
}
