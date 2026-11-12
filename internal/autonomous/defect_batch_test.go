package autonomous

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

type fakeDefectSource struct {
	batch config.DefectQueueBatchSettings
}

func (f fakeDefectSource) EffectiveForProject(string) config.DefectQueueSettings {
	return config.DefectQueueSettings{Enabled: true, Batch: f.batch}
}

func fakeBatchCfg(capN int, unrelated bool, rule string) fakeDefectSource {
	return fakeDefectSource{config.DefectQueueBatchSettings{
		SizeCap: capN, GroupingKeys: config.DefaultDefectGroupingKeys(),
		AllowUnrelated: unrelated, PriorityRule: rule,
	}}
}

func TestDefectBatch_Grouping(t *testing.T) {
	ds := []QueueDefect{
		{ID: "d1", Title: "a", Project: "p", Area: "ui"},
		{ID: "d2", Title: "b", Project: "p", Area: "ui"},
		{ID: "d3", Title: "c", Project: "p", Area: "api"},
	}
	got := GroupDefects(ds, fakeBatchCfg(5, false, config.DefectPriorityHighestMember))
	if len(got) != 2 || len(got[0].Defects) != 2 || len(got[1].Defects) != 1 || got[1].Defects[0].ID != "d3" {
		t.Fatalf("same area should group, different area not: %+v", got)
	}
	got = GroupDefects(ds, fakeBatchCfg(5, true, config.DefectPriorityHighestMember))
	if len(got) != 1 || len(got[0].Defects) != 3 {
		t.Fatalf("allow_unrelated should merge all: %+v", got)
	}
	// shared files relate defects of different areas
	ds2 := []QueueDefect{
		{ID: "e1", Project: "p", Area: "x", Files: []string{"a.go"}},
		{ID: "e2", Project: "p", Area: "y", Files: []string{"a.go"}},
	}
	if g := GroupDefects(ds2, fakeBatchCfg(5, false, "")); len(g) != 1 {
		t.Fatalf("shared file should group: %+v", g)
	}
}

func TestDefectBatch_SizeCap(t *testing.T) {
	var ds []QueueDefect
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		ds = append(ds, QueueDefect{ID: id, Project: "p", Area: "ui"})
	}
	got := GroupDefects(ds, fakeBatchCfg(2, false, config.DefectPriorityHighestMember))
	if len(got) != 3 {
		t.Fatalf("want 3 batches, got %d", len(got))
	}
	for _, b := range got {
		if len(b.Defects) > 2 {
			t.Fatalf("batch over cap: %v", batchSortedIDs(b))
		}
	}
}

func TestDefectBatch_PriorityHighestMember(t *testing.T) {
	ds := []QueueDefect{
		{ID: "d1", Project: "p", Area: "ui", Priority: 2},
		{ID: "d2", Project: "p", Area: "ui", Priority: 9},
		{ID: "d3", Project: "p", Area: "ui", Priority: 4},
	}
	got := GroupDefects(ds, fakeBatchCfg(5, false, config.DefectPriorityHighestMember))
	if len(got) != 1 || got[0].Priority != 9 {
		t.Fatalf("want priority 9, got %+v", got)
	}
	got = GroupDefects(ds, fakeBatchCfg(5, false, config.DefectPriorityFirstMember))
	if got[0].Priority != 2 {
		t.Fatalf("first_member want 2, got %d", got[0].Priority)
	}
}

func countKind(ts []Task, kind string) int {
	n := 0
	for _, t := range ts {
		if t.Kind == kind {
			n++
		}
	}
	return n
}

func sampleBatchPlan() *BatchPlan {
	ds := []QueueDefect{
		{ID: "d1", Title: "one", Project: "p", Area: "ui", Priority: 9, Files: []string{"a.go"}},
		{ID: "d2", Title: "two", Project: "p", Area: "ui", Priority: 3},
		{ID: "d3", Title: "three", Project: "p", Area: "ui", Priority: 5},
	}
	b := GroupDefects(ds, fakeBatchCfg(5, false, config.DefectPriorityHighestMember))
	return BuildBatchPlan(b[0])
}

func TestDefectBatch_PlanSharesGateDocChangelogParity(t *testing.T) {
	p := sampleBatchPlan()
	if len(p.Stories) != 3 {
		t.Fatalf("one story per defect, got %d", len(p.Stories))
	}
	for _, s := range p.Stories {
		if s.DefectID == "" || len(s.Story.Tasks) < 1 || len(s.Story.Tasks) > 2 {
			t.Fatalf("bad story %+v", s)
		}
	}
	for _, k := range []string{DefectTaskKindGate, DefectTaskKindDocs, DefectTaskKindChangelog, DefectTaskKindParity} {
		if n := countKind(p.Shared, k); n != 1 {
			t.Fatalf("kind %s count %d, want 1", k, n)
		}
	}
	for _, s := range p.Stories {
		for _, tk := range s.Story.Tasks {
			switch tk.Kind {
			case DefectTaskKindGate, DefectTaskKindDocs, DefectTaskKindChangelog, DefectTaskKindParity:
				t.Fatalf("shared task inside a story: %s", tk.ID)
			}
		}
	}
	if BatchTemplateText() == "" || BatchGroupingHint(config.DefectQueueBatchSettings{SizeCap: 3, GroupingKeys: []string{"shared_files"}}) == "" {
		t.Fatal("template and hint text must not be empty")
	}
}

func TestDefectBatch_SplitOutFailedStory(t *testing.T) {
	p := sampleBatchPlan()
	sharedBefore := len(p.Shared)
	split, err := SplitOutStory(p, "story-d1")
	if err != nil {
		t.Fatal(err)
	}
	if len(split.Stories) != 1 || split.Stories[0].DefectID != "d1" || len(split.Batch.Defects) != 1 {
		t.Fatalf("split must hold only d1: %+v", split)
	}
	for _, k := range []string{DefectTaskKindGate, DefectTaskKindDocs, DefectTaskKindChangelog} {
		if countKind(split.Shared, k) != 1 {
			t.Fatalf("split missing own %s", k)
		}
	}
	if split.Batch.ID == p.Batch.ID {
		t.Fatalf("split batch ID %q collides with original", split.Batch.ID)
	}
	if split.Batch.Priority != 9 {
		t.Fatalf("split priority %d", split.Batch.Priority)
	}
	if len(p.Stories) != 2 || len(p.Batch.Defects) != 2 || len(p.Shared) != sharedBefore {
		t.Fatalf("original must keep the others and shared tasks: %+v", p)
	}
	if p.Batch.Priority != 5 {
		t.Fatalf("original priority not recomputed: %d", p.Batch.Priority)
	}
	live := map[string]bool{}
	for _, s := range p.Stories {
		for _, tk := range s.Story.Tasks {
			live[tk.ID] = true
		}
	}
	for _, tk := range p.Shared {
		live[tk.ID] = true
	}
	for _, tk := range p.Shared {
		for _, dep := range tk.DependsOn {
			if !live[dep] {
				t.Fatalf("shared task %s depends on removed %s", tk.ID, dep)
			}
		}
	}
	if _, err := SplitOutStory(p, "story-missing"); err == nil {
		t.Fatal("expected error for unknown story")
	}
}
