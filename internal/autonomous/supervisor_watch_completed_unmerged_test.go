package autonomous

import (
	"strings"
	"testing"
	"time"
)

func TestSupervisorWatchCompletedUnmergedText(t *testing.T) {
	txt, err := SupervisorWatchText(SupervisorWatchCompletedUnmerged, "auto-1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(txt, "\n") || strings.Contains(txt, "PRD") || !strings.Contains(txt, "auto-1") {
		t.Fatalf("bad text %q", txt)
	}
}

func TestSupervisorWatchCompletedUnmergedPassRaisesFindingReadOnly(t *testing.T) {
	m := swNewManager(t)
	m.mu.Lock()
	m.cfg.Automation.SupervisorWatch.Mode = SupervisorWatchModeShadow
	m.mu.Unlock()
	git := &fakeCUGit{merged: map[string]bool{"b-ok": true}}
	m.SetCompletionLandingGit(git)

	prds := []*PRD{
		cuPRD("cu-open", "b-open", PRDCompleted),
		cuPRD("cu-ok", "b-ok", PRDCompleted),
	}
	got := m.supervisorWatchRawFindings(time.Now(), m.Config().Automation.SupervisorWatch, prds)
	var found []SupervisorWatchFinding
	for _, f := range got {
		if f.Kind == SupervisorWatchCompletedUnmerged {
			found = append(found, f)
		}
	}
	if len(found) != 1 || found[0].Subject != "cu-open" || found[0].Text == "" {
		t.Fatalf("CompletedUnmerged findings = %+v, want one for cu-open", found)
	}
	if !supervisorWatchAutomatonScoped(SupervisorWatchCompletedUnmerged) {
		t.Error("completed_unmerged must be Automaton scoped")
	}
	// read only: the fake only ever saw BranchMerged questions, and the
	// records were not touched.
	if prds[0].Status != PRDCompleted {
		t.Errorf("status changed to %s", prds[0].Status)
	}
}
