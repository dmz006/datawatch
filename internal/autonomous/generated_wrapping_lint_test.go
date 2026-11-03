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
