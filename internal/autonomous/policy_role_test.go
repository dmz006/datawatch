package autonomous

import (
	"reflect"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestJudgeRoleUnsetResolvesToTheVerifierPolicy(t *testing.T) {
	verifier := config.VerifierPolicyConfig{Mode: "prefer-free", Backends: []string{"ollama-a", "claude-code"}, ClaudeSessionCap: 3}
	for _, unset := range [][]string{nil, {}, {"", "  "}} {
		got := ResolvePolicyForRole(PolicyRoleJudge, verifier, unset)
		if !reflect.DeepEqual(got, verifier) {
			t.Fatalf("judge list %q: policy = %+v, want the verifier policy %+v", unset, got, verifier)
		}
	}
}

func TestJudgeRoleFollowsLaterEditsToTheVerifierPolicy(t *testing.T) {
	load := fakeVerifierLoad{}
	verifier := config.VerifierPolicyConfig{Mode: "fixed", Backends: []string{"ollama-a", "ollama-b"}}
	if got := SelectBackendForRole(PolicyRoleJudge, verifier, nil, nil, load, VerifierSelectFlags{}); got.Backend != "ollama-a" {
		t.Fatalf("before the edit: backend = %q, want ollama-a", got.Backend)
	}
	verifier.Backends = []string{"ollama-b", "ollama-a"}
	if got := SelectBackendForRole(PolicyRoleJudge, verifier, nil, nil, load, VerifierSelectFlags{}); got.Backend != "ollama-b" {
		t.Fatalf("after the edit: backend = %q, want ollama-b", got.Backend)
	}
}

func TestJudgeRoleSetOverridesTheVerifierBackendList(t *testing.T) {
	verifier := config.VerifierPolicyConfig{Mode: "balance", Backends: []string{"ollama-a"}, ClaudeSessionCap: 2}
	got := ResolvePolicyForRole(PolicyRoleJudge, verifier, []string{"claude-code", "ollama-b", "claude-code"})
	if !reflect.DeepEqual(got.Backends, []string{"claude-code", "ollama-b"}) {
		t.Fatalf("judge backends = %v, want the judge list, deduplicated and in order", got.Backends)
	}
	if got.Mode != "balance" || got.ClaudeSessionCap != 2 {
		t.Fatalf("judge policy lost the verifier's mode or cap: %+v", got)
	}
	if v := ResolvePolicyForRole(PolicyRoleVerifier, verifier, []string{"claude-code"}); !reflect.DeepEqual(v, verifier) {
		t.Fatalf("the verifier role must ignore the judge list: %+v", v)
	}
	if !reflect.DeepEqual(verifier.Backends, []string{"ollama-a"}) {
		t.Fatalf("resolving the judge policy changed the verifier policy: %v", verifier.Backends)
	}
}

func TestJudgeRoleSelectionHonoursEachMode(t *testing.T) {
	judge := []string{"ollama-a", "ollama-b", "claude-code"}
	full := func(a, b int) fakeVerifierLoad {
		return fakeVerifierLoad{
			capacity: map[string]int{"ollama-a": 2, "ollama-b": 2, "claude-code": 3},
			inUse:    map[string]int{"ollama-a": a, "ollama-b": b},
		}
	}
	tests := []struct {
		name string
		mode string
		load fakeVerifierLoad
		want string
	}{
		{"fixed takes the first listed backend", "fixed", full(2, 0), "ollama-a"},
		{"prefer-free skips a full backend", "prefer-free", full(2, 0), "ollama-b"},
		{"prefer-free keeps the first when it has room", "prefer-free", full(1, 0), "ollama-a"},
		{"balance picks the lightest backend", "balance", full(1, 0), "ollama-b"},
		{"balance ties go to list order", "balance", full(1, 1), "claude-code"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			verifier := config.VerifierPolicyConfig{Mode: tc.mode, Backends: []string{"unused"}}
			judgePick := SelectBackendForRole(PolicyRoleJudge, verifier, judge, nil, tc.load, VerifierSelectFlags{})
			if judgePick.Backend != tc.want || judgePick.Role != PolicyRoleJudge {
				t.Fatalf("judge pick = %+v, want backend %q for the judge role", judgePick, tc.want)
			}
			verifierPick := SelectBackendForRole(PolicyRoleVerifier, config.VerifierPolicyConfig{Mode: tc.mode, Backends: judge}, nil, nil, tc.load, VerifierSelectFlags{})
			if verifierPick.Backend != judgePick.Backend || verifierPick.Reason != judgePick.Reason {
				t.Fatalf("judge pick %+v differs from the verifier pick %+v on the same list", judgePick, verifierPick)
			}
			if verifierPick.Role != PolicyRoleVerifier {
				t.Fatalf("verifier pick carries role %q", verifierPick.Role)
			}
		})
	}
}

func TestJudgeRoleWithNothingEligibleNamesTheJudge(t *testing.T) {
	verifier := config.VerifierPolicyConfig{Mode: "gpu-only"}
	got := SelectBackendForRole(PolicyRoleJudge, verifier, []string{"claude-code"}, nil, nil, VerifierSelectFlags{})
	if got.Backend != "" || got.Reason != "no eligible judge backend" {
		t.Fatalf("pick = %+v, want no backend and a judge-worded reason", got)
	}
}

func TestJudgePolicyDecisionLogRecordsTheJudgeRole(t *testing.T) {
	m, cap, prdID, ids := escalationFixture(t)
	if err := m.RecordJudgePolicyChoice(prdID, ids[0], "ollama-b", "qwen", "fixed: ollama-b"); err != nil {
		t.Fatal(err)
	}
	prd, _ := m.Store().GetPRD(prdID)
	d := prd.Decisions[len(prd.Decisions)-1]
	if d.Kind != "judge_policy" || d.Backend != "ollama-b" || !contains(d.Note, "role=judge") {
		t.Fatalf("decision = %+v", d)
	}
	if prd.Story[0].Tasks[0].VerifierPolicyChoice != nil {
		t.Fatalf("a judge pick must not be stored as the task's verifier choice")
	}
	e := cap.only(t)
	if e.Action != JudgePolicyAuditAction || e.Details["role"] != "judge" || e.Details["backend"] != "ollama-b" {
		t.Fatalf("audit entry = %+v", e)
	}
	if err := m.RecordJudgePolicyChoice("nope", "", "a", "", "r"); err == nil {
		t.Fatal("want error for unknown prd")
	}
}

func TestVerifierPolicyDecisionLogRecordsTheVerifierRole(t *testing.T) {
	m, cap, prdID, ids := escalationFixture(t)
	if err := m.RecordVerifierPolicyChoice(prdID, ids[0], "ollama-a", "", "fixed: ollama-a"); err != nil {
		t.Fatal(err)
	}
	prd, _ := m.Store().GetPRD(prdID)
	d := prd.Decisions[len(prd.Decisions)-1]
	if d.Kind != "verifier_policy" || !contains(d.Note, "role=verifier") {
		t.Fatalf("decision = %+v", d)
	}
	if e := cap.only(t); e.Details["role"] != "verifier" {
		t.Fatalf("audit entry = %+v", e)
	}
}
