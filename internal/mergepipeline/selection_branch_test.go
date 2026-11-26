package mergepipeline

import (
	"context"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

type recordSelection struct{}

func (recordSelection) SelectMergeRun(context.Context, []string, string) (autonomous.MergeSelectionEntry, error) {
	return autonomous.MergeSelectionEntry{AutomatonID: "a1", Branch: "automaton/a1", Tip: "tip", Eligible: true}, nil
}

// After selection, git gets the branch name the rule checked, never the text
// of the run request.
func TestSelectedRunUsesTheRecordedBranchName(t *testing.T) {
	asked := []string{" automaton/a1\n"}
	s := &runState{r: &Runner{Selection: recordSelection{}}, req: RunRequest{Branches: asked}}
	if err := s.applySelection(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(s.req.Branches) != 1 || s.req.Branches[0] != "automaton/a1" {
		t.Fatalf("the run would give git %q, want the recorded branch name", s.req.Branches)
	}
	if err := s.checkPin("automaton/a1", "moved"); err == nil {
		t.Fatal("the recorded branch is not held to the selected commit")
	}
	if asked[0] != " automaton/a1\n" {
		t.Fatal("the caller's list was changed")
	}
}
