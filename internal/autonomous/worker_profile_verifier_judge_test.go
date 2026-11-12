package autonomous

import (
	"testing"
)

func TestSessionLaunchInputRoleIsVerifierOrJudge(t *testing.T) {
	m, prd, task := newProfileTestPRD(t, "tname", "pname")
	task.Kind = "implementation"
	for _, role := range []string{"verifier", "judge"} {
		in := m.sessionLaunchInput(role, prd, task)
		if in.Role != role {
			t.Errorf("role = %q, want %q", in.Role, role)
		}
		if in.Kind != "implementation" || in.TaskName != "tname" || in.AutomatonName != "pname" {
			t.Errorf("%s input = %+v", role, in)
		}
	}
}

func TestSessionLaunchInputWithoutTaskLeavesNamesEmpty(t *testing.T) {
	m, _, _ := newProfileTestPRD(t, "", "")
	in := m.sessionLaunchInput("judge", nil, nil)
	if in.Role != "judge" || in.Kind != "" || in.TaskName != "" || in.AutomatonName != "" {
		t.Errorf("input = %+v", in)
	}
}

func TestSessionProfileNothingConfiguredAppliesNone(t *testing.T) {
	m, prd, task := newProfileTestPRD(t, "", "")
	for _, role := range []string{"verifier", "judge"} {
		if _, applied := m.ResolveSessionProfile(role, prd, task); applied {
			t.Errorf("%s: profile applied with nothing configured", role)
		}
		if _, applied := m.ResolveSessionProfile(role, nil, nil); applied {
			t.Errorf("%s: profile applied with no task or PRD", role)
		}
	}
}

func TestSessionProfileConfiguredRoleApplies(t *testing.T) {
	m, prd, task := newProfileTestPRD(t, "", "")
	m.SetWorkerProfileSettings(func() WorkerProfileSettings {
		return WorkerProfileSettings{RoleDefaults: map[string]string{"verifier": "minimal"}}
	})
	p, applied := m.ResolveSessionProfile("verifier", prd, task)
	if !applied || p.Name != "minimal" {
		t.Fatalf("verifier = %q applied=%v, want minimal", p.Name, applied)
	}
	if _, applied := m.ResolveSessionProfile("judge", prd, task); applied {
		t.Error("judge has no configured default; none should apply")
	}
}
