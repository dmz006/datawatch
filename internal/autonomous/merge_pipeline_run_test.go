package autonomous

import (
	"testing"
	"time"
)

func TestMergeStatusListsPipelineRunsWhateverTheMode(t *testing.T) {
	th := newTriageHarness(t, 1, 10)
	m := th.h.m
	if got := m.MergeStatus().PipelineRuns; got == nil || len(got) != 0 {
		t.Fatalf("want an empty, non-nil list, got %#v", got)
	}
	end := time.Now().UTC()
	m.RecordMergeRun(MergePipelineRun{ID: "r1", Project: "p", Result: MergeRunOK, End: &end,
		Steps: []MergeStepRun{{Step: "merge-branches", Kind: "ship", Start: end, End: &end, Result: MergeStepOK, Source: "default"}}})
	ms := m.MergeStatus()
	if len(ms.PipelineRuns) != 1 || ms.PipelineRuns[0].Steps[0].Source != "default" || ms.PipelineRuns[0].Steps[0].Step != "merge-branches" {
		t.Fatalf("runs %+v", ms.PipelineRuns)
	}
}

func TestMergeRunRecordsAreBoundedAndNewestFirst(t *testing.T) {
	r := NewMergeRunRecords(t.TempDir())
	for i := 0; i < mergeRunRecordLimit+5; i++ {
		r.RecordMergeRun(MergePipelineRun{ID: time.Duration(i).String()})
	}
	got := r.List(0)
	if len(got) != mergeRunRecordLimit || got[0].ID != time.Duration(mergeRunRecordLimit+4).String() {
		t.Fatalf("len %d first %s", len(got), got[0].ID)
	}
	if len(r.List(3)) != 3 {
		t.Fatal("limit not applied")
	}
}
