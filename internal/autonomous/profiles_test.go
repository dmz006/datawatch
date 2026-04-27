// v5.26.19 — F10 project + cluster profile attachment for autonomous
// PRDs. Operator-reported: PRDs should be based on directory or
// profile, with cluster_profile dispatching the worker to /api/agents
// instead of local tmux. SetPRDProfiles tests cover validation,
// running-PRD refusal, and the no-resolver fallback.

package autonomous

import (
	"strings"
	"testing"
)

type fakeResolver struct {
	projects, clusters map[string]bool
}

func (f *fakeResolver) HasProjectProfile(name string) bool { return f.projects[name] }
func (f *fakeResolver) HasClusterProfile(name string) bool { return f.clusters[name] }

func TestSetPRDProfiles_ValidatesProjectName(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	m.SetProfileResolver(&fakeResolver{projects: map[string]bool{"webapp": true}})
	prd, _ := m.Store().CreatePRD("spec", "/p", "claude", EffortNormal)

	err := m.SetPRDProfiles(prd.ID, "ghost", "")
	if err == nil || !strings.Contains(err.Error(), "project profile") {
		t.Fatalf("expected unknown-project error, got: %v", err)
	}
	if err := m.SetPRDProfiles(prd.ID, "webapp", ""); err != nil {
		t.Errorf("known project profile rejected: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.ProjectProfile != "webapp" {
		t.Errorf("project profile not persisted: %q", got.ProjectProfile)
	}
}

func TestSetPRDProfiles_ValidatesClusterName(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	m.SetProfileResolver(&fakeResolver{clusters: map[string]bool{"prod-east": true}})
	prd, _ := m.Store().CreatePRD("spec", "/p", "claude", EffortNormal)

	err := m.SetPRDProfiles(prd.ID, "", "ghost-cluster")
	if err == nil || !strings.Contains(err.Error(), "cluster profile") {
		t.Fatalf("expected unknown-cluster error, got: %v", err)
	}
	if err := m.SetPRDProfiles(prd.ID, "", "prod-east"); err != nil {
		t.Errorf("known cluster profile rejected: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.ClusterProfile != "prod-east" {
		t.Errorf("cluster profile not persisted: %q", got.ClusterProfile)
	}
}

func TestSetPRDProfiles_RefusesRunningPRD(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	m.SetProfileResolver(&fakeResolver{projects: map[string]bool{"webapp": true}})
	prd, _ := m.Store().CreatePRD("spec", "/p", "claude", EffortNormal)
	prd.Status = PRDRunning
	_ = m.Store().SavePRD(prd)

	err := m.SetPRDProfiles(prd.ID, "webapp", "")
	if err == nil || !strings.Contains(err.Error(), "running") {
		t.Fatalf("expected running-PRD refusal, got: %v", err)
	}
}

func TestSetPRDProfiles_NoResolverSkipsValidation(t *testing.T) {
	// When no resolver is wired, profile names go through unchecked.
	// Lets unit tests + transitional setups proceed without the F10
	// stores; runtime will fail at spawn time if the profile is wrong.
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.Store().CreatePRD("spec", "/p", "claude", EffortNormal)

	if err := m.SetPRDProfiles(prd.ID, "any-name", "any-cluster"); err != nil {
		t.Errorf("no-resolver path rejected: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.ProjectProfile != "any-name" || got.ClusterProfile != "any-cluster" {
		t.Errorf("profiles not persisted: %+v", got)
	}
}

func TestSetPRDProfiles_RecordsDecision(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	prd, _ := m.Store().CreatePRD("spec", "/p", "claude", EffortNormal)
	pre := len(prd.Decisions)

	if err := m.SetPRDProfiles(prd.ID, "webapp", "prod-east"); err != nil {
		t.Fatalf("SetPRDProfiles: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if len(got.Decisions) != pre+1 {
		t.Errorf("decisions not appended: was %d, now %d", pre, len(got.Decisions))
	}
	last := got.Decisions[len(got.Decisions)-1]
	if last.Kind != "set_profiles" {
		t.Errorf("decision kind = %q, want set_profiles", last.Kind)
	}
	if !strings.Contains(last.Note, "webapp") || !strings.Contains(last.Note, "prod-east") {
		t.Errorf("decision note missing profile names: %q", last.Note)
	}
}

func TestSetPRDProfiles_MissingPRD(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err := m.SetPRDProfiles("nope", "", ""); err == nil {
		t.Fatal("expected error for missing PRD")
	}
}
