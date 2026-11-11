package autonomous

import (
	"sort"
	"testing"
)

func TestEventKindRegistryCoversEveryKnownKind(t *testing.T) {
	listed := map[string]bool{}
	for _, e := range ListEventKinds() {
		listed[e.Kind] = true
	}
	want := []string{
		KindTaskSessionStarted, KindTaskSessionCleared, KindScopeWarning,
		KindGuardrailWarn, KindPRDUpdated, KindQuestionAnswered,
		KindQuestionWithdrawn, KindAutomatonStuckRunning, KindAutomatonMergeReady,
		KindAutomatonTasksDone, KindCapacityWaitTooLong, KindBurnGuardRungChanged,
		KindJudgeVerdictBlock, KindJudgeVerdictWarn, KindJudgeVerdictPass, KindFinalGateFailed,
	}
	for _, s := range []PRDStatus{
		PRDDraft, PRDPlanning, PRDNeedsReview, PRDApproved, PRDRevisionsAsked,
		PRDActive, PRDRunning, PRDCompleted, PRDRejected, PRDCancelled,
		PRDArchived, PRDBlocked, PRDFailed, PRDPaused,
	} {
		want = append(want, PRDStatusKind(s))
	}
	for _, s := range []TaskStatus{
		TaskPending, TaskQueued, TaskInProgress, TaskRunningTests,
		TaskVerifying, TaskCompleted, TaskFailed, TaskBlocked,
		TaskCancelled, TaskWaitingCapacity, TaskAwaitingApproval, TaskReviewNeeded,
	} {
		want = append(want, TaskStatusKind(s))
	}
	for _, k := range want {
		if !IsKnownEventKind(k) {
			t.Errorf("kind %q is no longer known", k)
		}
		if !listed[k] {
			t.Errorf("kind %q missing from the registry", k)
		}
	}
	if len(KnownEventKinds) != len(want) {
		t.Errorf("accepted set has %d kinds, want %d", len(KnownEventKinds), len(want))
	}
	if !listed[KindTaskLooping] {
		t.Errorf("registry must list %q", KindTaskLooping)
	}
	if IsKnownEventKind(KindTaskLooping) {
		t.Errorf("%q must stay outside the accepted set", KindTaskLooping)
	}
}

func TestEventKindRegistryEntriesComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range ListEventKinds() {
		if e.Kind == "" {
			t.Errorf("entry with empty kind: %+v", e)
		}
		if seen[e.Kind] {
			t.Errorf("duplicate kind %q", e.Kind)
		}
		seen[e.Kind] = true
		if e.Description == "" {
			t.Errorf("kind %q has no description", e.Kind)
		}
		if !validEventGroup(e.Group) {
			t.Errorf("kind %q has invalid group %q", e.Kind, e.Group)
		}
	}
}

func TestEventKindRegistrySorted(t *testing.T) {
	got := ListEventKinds()
	if !sort.SliceIsSorted(got, func(i, j int) bool {
		if got[i].Group != got[j].Group {
			return got[i].Group < got[j].Group
		}
		return got[i].Kind < got[j].Kind
	}) {
		t.Errorf("registry is not sorted by group then kind")
	}
	got[0].Kind = "mutated"
	if ListEventKinds()[0].Kind == "mutated" {
		t.Errorf("ListEventKinds must return a copy")
	}
}
