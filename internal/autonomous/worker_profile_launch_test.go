package autonomous

import (
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/workerprofile"
)

// The provider's values must reach the helper's output, and a later change to
// the provider's values must show up on the next call (no restart needed).
func TestWorkerLaunchInputCarriesProviderValues(t *testing.T) {
	m := &Manager{}
	user := workerprofile.WorkerProfile{Name: "mine"}
	settings := WorkerProfileSettings{
		GlobalName:   "mine",
		RoleDefaults: map[string]string{"worker": "mine"},
		KindDefaults: map[string]string{"docs": "mine"},
		UserProfiles: map[string]workerprofile.WorkerProfile{"mine": user},
	}
	m.SetWorkerProfileSettings(func() WorkerProfileSettings { return settings })

	in := m.workerLaunchInput()
	if in.GlobalName != "mine" {
		t.Errorf("GlobalName = %q, want mine", in.GlobalName)
	}
	if in.RoleDefaults["worker"] != "mine" || in.KindDefaults["docs"] != "mine" {
		t.Errorf("defaults not carried: role=%v kind=%v", in.RoleDefaults, in.KindDefaults)
	}
	if in.Role != "" || in.Kind != "" || in.TaskName != "" || in.AutomatonName != "" || in.ProjectName != "" {
		t.Errorf("caller-filled names must be empty: %+v", in)
	}
	if p, ok := in.Lookup("mine"); !ok || p.Name != "mine" {
		t.Errorf("Lookup(user) = %+v, %v; want user profile", p, ok)
	}
	if _, ok := in.Lookup("no-such-profile"); ok {
		t.Errorf("Lookup(unknown) found a profile")
	}

	// A settings change applies to the next call without re-wiring.
	settings = WorkerProfileSettings{GlobalName: "other"}
	if got := m.workerLaunchInput().GlobalName; got != "other" {
		t.Errorf("after change GlobalName = %q, want other", got)
	}
}

// Built-in profiles are found even when no user profiles are configured.
func TestWorkerLaunchInputFindsBuiltinWithoutProvider(t *testing.T) {
	m := &Manager{}
	in := m.workerLaunchInput()
	if in.GlobalName != "" || in.RoleDefaults != nil || in.KindDefaults != nil {
		t.Errorf("no provider should leave settings empty: %+v", in)
	}
	names := workerprofile.Names()
	if len(names) == 0 {
		t.Skip("no built-in profiles")
	}
	if _, ok := in.Lookup(names[0]); !ok {
		t.Errorf("Lookup(builtin %q) not found", names[0])
	}
}

// newProfileTestPRD creates a fresh manager holding a one-task PRD with the
// given per-task and per-Automaton profile names, and returns the manager,
// the PRD and the task.
func newProfileTestPRD(t *testing.T, taskProfile, prdProfile string) (*Manager, *PRD, *Task) {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "", "pi-prompt", "qwen3", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	prd.WorkerProfile = prdProfile
	if err := m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{
		ID: "tp1", Title: "t", Spec: "do it", WorkerProfile: taskProfile,
	}}}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	return m, prd, &prd.Story[0].Tasks[0]
}

func fallbackDecisions(p *PRD) []Decision {
	var out []Decision
	for _, d := range p.Decisions {
		if d.Kind == "worker_profile_fallback" {
			out = append(out, d)
		}
	}
	return out
}

// A requested name that is not found records the fallback in BOTH places:
// the task field (overwritten, not grown, by a second attempt) and the PRD
// decision log with the kind, actor and a note naming the task.
func TestRecordWorkerProfileDecisionFallbackWritesBothPlaces(t *testing.T) {
	m, prd, task := newProfileTestPRD(t, "no-such-profile", "")

	m.recordWorkerProfileDecision(prd, task)

	prd, _ = m.Store().GetPRD(prd.ID)
	gotTask := prd.Story[0].Tasks[0]
	if !strings.HasPrefix(gotTask.WorkerProfileDecision, "requested=no-such-profile layer=task applied=") {
		t.Fatalf("task field %q, want a fallback text for requested=no-such-profile", gotTask.WorkerProfileDecision)
	}
	if !strings.Contains(gotTask.WorkerProfileDecision, "reason=") {
		t.Errorf("task field missing reason: %q", gotTask.WorkerProfileDecision)
	}
	fbs := fallbackDecisions(prd)
	if len(fbs) != 1 {
		t.Fatalf("got %d worker_profile_fallback decisions, want 1", len(fbs))
	}
	if fbs[0].Actor != "autonomous" {
		t.Errorf("decision actor %q, want autonomous", fbs[0].Actor)
	}
	if !strings.Contains(fbs[0].Note, "tp1") || !strings.Contains(fbs[0].Note, "requested=no-such-profile") {
		t.Errorf("decision note %q, want it naming the task and the fallback", fbs[0].Note)
	}

	// A second attempt overwrites the task field; it does not grow it.
	fresh, _ := m.Store().GetPRD(prd.ID)
	len1 := len(fresh.Story[0].Tasks[0].WorkerProfileDecision)
	m.recordWorkerProfileDecision(fresh, &fresh.Story[0].Tasks[0])
	fresh, _ = m.Store().GetPRD(prd.ID)
	d := fresh.Story[0].Tasks[0].WorkerProfileDecision
	if len(d) != len1 {
		t.Errorf("task field grew across attempts: %q", d)
	}
	if n := len(fallbackDecisions(fresh)); n != 2 {
		t.Errorf("after the second attempt got %d fallback entries, want 2", n)
	}
}

// A requested name that resolves (built-in here) writes neither place.
func TestRecordWorkerProfileDecisionResolvedNameWritesNothing(t *testing.T) {
	m, prd, task := newProfileTestPRD(t, "full", "")

	m.recordWorkerProfileDecision(prd, task)

	prd, _ = m.Store().GetPRD(prd.ID)
	if got := prd.Story[0].Tasks[0].WorkerProfileDecision; got != "" {
		t.Errorf("task field %q, want empty for a resolved name", got)
	}
	if n := len(fallbackDecisions(prd)); n != 0 {
		t.Errorf("got %d worker_profile_fallback decisions for a resolved name, want 0", n)
	}
}

// A launch with no name anywhere (pure role default) carries no requested
// name, so there is no fallback to record.
func TestRecordWorkerProfileDecisionUnnamedWritesNothing(t *testing.T) {
	m, prd, task := newProfileTestPRD(t, "", "")

	m.recordWorkerProfileDecision(prd, task)

	prd, _ = m.Store().GetPRD(prd.ID)
	if got := prd.Story[0].Tasks[0].WorkerProfileDecision; got != "" {
		t.Errorf("task field %q, want empty without a requested name", got)
	}
	if n := len(fallbackDecisions(prd)); n != 0 {
		t.Errorf("got %d worker_profile_fallback decisions without a name, want 0", n)
	}
}

// The Automaton layer request is recorded the same way, with its layer named.
func TestRecordWorkerProfileDecisionPRDLayerFallback(t *testing.T) {
	m, prd, task := newProfileTestPRD(t, "", "no-such-prd-profile")

	m.recordWorkerProfileDecision(prd, task)

	prd, _ = m.Store().GetPRD(prd.ID)
	if got := prd.Story[0].Tasks[0].WorkerProfileDecision; !strings.Contains(got, "requested=no-such-prd-profile") || !strings.Contains(got, "layer=automaton") {
		t.Errorf("task field %q, want the automaton-layer fallback for no-such-prd-profile", got)
	}
	if n := len(fallbackDecisions(prd)); n != 1 {
		t.Errorf("got %d worker_profile_fallback decisions, want 1", n)
	}
}
