// project_policy_test.go — table-driven tests for the per-project policy
// tier: resolution (daemon / project / automaton precedence, source
// labels), validation of unknown values, the PRD adapter, and JSON/YAML
// round-trips.
//
// No daemon defaults are hard-coded: the daemon tier is always a fixture
// with explicitly set, distinctive values, and expectations reference that
// fixture (or the other tier under test).
package autonomous

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/dmz006/datawatch/internal/config"
)

func ppInt(v int) *int           { return &v }
func ppBool(v bool) *bool        { return &v }
func ppStr(v string) *string     { return &v }
func ppFloat(v float64) *float64 { return &v }

// daemonFixture is the daemon tier: every key explicitly set to a
// distinctive value (0/empty would be ambiguous with "unset").
func daemonFixture() config.DaemonPolicySource {
	return config.DaemonPolicySource{
		Autonomous: config.AutonomousConfig{
			PlanCoverageCheck:       "warn",
			PlanCoverageMinRatio:    0.75,
			PlanReviewMode:          "block",
			ScopeLint:               "warn",
			RetryPolicy:             map[string]int{"infra": 2, "content": 1},
			RetryCeilingTotal:       9,
			AutoFixRetries:          3,
			InfraRetryEscalateAfter: 5,
			FailureEscalation: config.FailureEscalationConfig{
				Enabled:     true,
				MaxFailures: 4,
				Steps: []config.FailureEscalationStepConfig{
					{Backend: "claude", Model: "sonnet", Effort: "high", Attempts: 2},
					{Attempts: 1},
				},
			},
			DefaultAutoPR:       true,
			MaxConcurrentTasks:  7,
			PlanTierMaxInflight: map[string]int{"quick": 2, "deep": 1},
			LaneFill:            config.AutonomousLaneFillConfig{GPULanes: 2, ClaudeLanes: 3},
			Automation: config.AutonomousAutomationConfig{
				UncommittedWorkPolicy: "warn",
				UncommittedWorkScope:  "worktree",
			},
			Parallel: config.AutonomousParallelConfig{
				ReadOnlyTasks:   ppBool(false),
				WorktreePerTask: ppBool(false),
			},
		},
		BurnGuard: config.BurnGuardConfig{AskOnChange: true},
	}
}

// projectFixture sets every project-tier key to a value distinct from the
// daemon fixture.
func projectFixture() *config.ProjectPolicy {
	escStep := config.FailureEscalationStepPolicy{Backend: "gpt", Model: "mini", Effort: "low", Attempts: 3}
	return &config.ProjectPolicy{
		PlanCoverageCheck:       ppStr("block"),
		PlanCoverageMinRatio:    ppFloat(0.9),
		PlanReviewMode:          ppStr("off"),
		ScopeLint:               ppStr("enforce"),
		RetryPolicy:             map[string]int{"infra": 9, "policy": 4},
		RetryCeilingTotal:       ppInt(11),
		AutoFixRetries:          ppInt(2),
		InfraRetryEscalateAfter: ppInt(6),
		FailureEscalation: &config.FailureEscalationLevel{
			Enabled:     ppBool(false),
			MaxFailures: ppInt(3),
			Steps:       []config.FailureEscalationStepPolicy{escStep},
		},
		DefaultAutoPR:           ppBool(false),
		MaxConcurrentTasks:      ppInt(12),
		PlanTierMaxInflight:     map[string]int{"quick": 3, "deep": 2},
		LaneFillGPULanes:        ppInt(4),
		LaneFillClaudeLanes:     ppInt(5),
		UncommittedWorkPolicy:   ppStr("off"),
		UncommittedWorkScope:    ppStr("task_files"),
		BurnGuardAskOnChange:    ppBool(false),
		ParallelReadOnlyTasks:   ppBool(true),
		ParallelWorktreePerTask: ppBool(true),
	}
}

func mustValidate(t *testing.T, p *config.ProjectPolicy) {
	t.Helper()
	if err := config.ValidateProjectPolicy(p, "policy"); err != nil {
		t.Fatalf("fixture policy should be valid, got: %v", err)
	}
}

func assertField[T any](t *testing.T, what string, got config.Field[T], want T, wantSource config.PolicySource) {
	t.Helper()
	if !reflect.DeepEqual(got.Value, want) {
		t.Errorf("%s value = %#v, want %#v", what, got.Value, want)
	}
	if got.Source != wantSource {
		t.Errorf("%s source = %q, want %q", what, got.Source, wantSource)
	}
}

// --- resolution -------------------------------------------------------------

func TestResolveProjectPolicy_EmptyPolicyIsDaemon(t *testing.T) {
	d := daemonFixture()
	got := config.ResolveProjectPolicy(d, nil, config.AutomatonOverrides{})

	daemon := d.Autonomous
	assertField(t, "PlanCoverageCheck", got.PlanCoverageCheck, "warn", config.PolicySourceDaemon)
	assertField(t, "PlanCoverageMinRatio", got.PlanCoverageMinRatio, daemon.PlanCoverageMinRatio, config.PolicySourceDaemon)
	assertField(t, "PlanReviewMode", got.PlanReviewMode, "block", config.PolicySourceDaemon)
	assertField(t, "ScopeLint", got.ScopeLint, "warn", config.PolicySourceDaemon)
	assertField(t, "RetryPolicy", got.RetryPolicy, daemon.RetryPolicy, config.PolicySourceDaemon)
	assertField(t, "RetryCeilingTotal", got.RetryCeilingTotal, daemon.RetryCeilingTotal, config.PolicySourceDaemon)
	assertField(t, "AutoFixRetries", got.AutoFixRetries, daemon.AutoFixRetries, config.PolicySourceDaemon)
	assertField(t, "InfraRetryEscalateAfter", got.InfraRetryEscalateAfter, daemon.InfraRetryEscalateAfter, config.PolicySourceDaemon)
	assertField(t, "DefaultAutoPR", got.DefaultAutoPR, daemon.DefaultAutoPR, config.PolicySourceDaemon)
	assertField(t, "MaxConcurrentTasks", got.MaxConcurrentTasks, daemon.MaxConcurrentTasks, config.PolicySourceDaemon)
	assertField(t, "PlanTierMaxInflight", got.PlanTierMaxInflight, daemon.PlanTierMaxInflight, config.PolicySourceDaemon)
	assertField(t, "LaneFillGPULanes", got.LaneFillGPULanes, daemon.LaneFill.GPULanes, config.PolicySourceDaemon)
	assertField(t, "LaneFillClaudeLanes", got.LaneFillClaudeLanes, daemon.LaneFill.ClaudeLanes, config.PolicySourceDaemon)
	assertField(t, "UncommittedWorkPolicy", got.UncommittedWorkPolicy, "warn", config.PolicySourceDaemon)
	assertField(t, "UncommittedWorkScope", got.UncommittedWorkScope, "worktree", config.PolicySourceDaemon)
	assertField(t, "BurnGuardAskOnChange", got.BurnGuardAskOnChange, d.BurnGuard.AskOnChange, config.PolicySourceDaemon)
	assertField(t, "ParallelReadOnlyTasks", got.ParallelReadOnlyTasks, false, config.PolicySourceDaemon)
	assertField(t, "ParallelWorktreePerTask", got.ParallelWorktreePerTask, false, config.PolicySourceDaemon)

	wantEsc := daemon.FailureEscalation
	esc := got.FailureEscalation.Value
	if esc.Enabled != wantEsc.Enabled ||
		esc.MaxFailures != wantEsc.MaxFailures ||
		len(esc.Steps) != len(wantEsc.Steps) ||
		got.FailureEscalation.Source != config.PolicySourceDaemon {
		t.Errorf("FailureEscalation = %+v (source %q), want daemon %+v",
			esc, got.FailureEscalation.Source, wantEsc)
	}
}

func TestResolveProjectPolicy_ProjectOverridesDaemon(t *testing.T) {
	d := daemonFixture()
	p := projectFixture()
	mustValidate(t, p)
	got := config.ResolveProjectPolicy(d, p, config.AutomatonOverrides{})

	assertField(t, "PlanCoverageCheck", got.PlanCoverageCheck, "block", config.PolicySourceProject)
	assertField(t, "PlanCoverageMinRatio", got.PlanCoverageMinRatio, 0.9, config.PolicySourceProject)
	assertField(t, "PlanReviewMode", got.PlanReviewMode, "off", config.PolicySourceProject)
	assertField(t, "ScopeLint", got.ScopeLint, "enforce", config.PolicySourceProject)
	assertField(t, "RetryPolicy", got.RetryPolicy, p.RetryPolicy, config.PolicySourceProject)
	assertField(t, "RetryCeilingTotal", got.RetryCeilingTotal, 11, config.PolicySourceProject)
	assertField(t, "AutoFixRetries", got.AutoFixRetries, 2, config.PolicySourceProject)
	assertField(t, "InfraRetryEscalateAfter", got.InfraRetryEscalateAfter, 6, config.PolicySourceProject)
	assertField(t, "DefaultAutoPR", got.DefaultAutoPR, false, config.PolicySourceProject)
	assertField(t, "MaxConcurrentTasks", got.MaxConcurrentTasks, 12, config.PolicySourceProject)
	assertField(t, "PlanTierMaxInflight", got.PlanTierMaxInflight, p.PlanTierMaxInflight, config.PolicySourceProject)
	assertField(t, "LaneFillGPULanes", got.LaneFillGPULanes, 4, config.PolicySourceProject)
	assertField(t, "LaneFillClaudeLanes", got.LaneFillClaudeLanes, 5, config.PolicySourceProject)
	assertField(t, "UncommittedWorkPolicy", got.UncommittedWorkPolicy, "off", config.PolicySourceProject)
	assertField(t, "UncommittedWorkScope", got.UncommittedWorkScope, "task_files", config.PolicySourceProject)
	assertField(t, "BurnGuardAskOnChange", got.BurnGuardAskOnChange, false, config.PolicySourceProject)
	assertField(t, "ParallelReadOnlyTasks", got.ParallelReadOnlyTasks, true, config.PolicySourceProject)
	assertField(t, "ParallelWorktreePerTask", got.ParallelWorktreePerTask, true, config.PolicySourceProject)

	want := p.FailureEscalation
	esc := got.FailureEscalation.Value
	if esc.Enabled != *want.Enabled ||
		esc.MaxFailures != 3 ||
		len(esc.Steps) != 1 ||
		esc.Steps[0].Backend != "gpt" ||
		got.FailureEscalation.Source != config.PolicySourceProject {
		t.Errorf("FailureEscalation = %+v (source %q), want %+v",
			esc, got.FailureEscalation.Source, want)
	}
}

func TestResolveProjectPolicy_AutomatonOverridesProject(t *testing.T) {
	d := daemonFixture()
	p := projectFixture()
	mustValidate(t, p)
	auto := config.AutomatonOverrides{
		RetryPolicy:        map[string]int{"content": 77},
		RetryCeilingTotal:  ppInt(99),
		MaxConcurrentTasks: ppInt(13),
		FailureEscalation:  &config.FailureEscalationLevel{Enabled: ppBool(true)},
		DefaultAutoPR:      ppBool(true),
	}
	got := config.ResolveProjectPolicy(d, p, auto)

	// Keys the Automaton set: its value wins over project and daemon.
	assertField(t, "RetryPolicy", got.RetryPolicy, map[string]int{"content": 77}, config.PolicySourceAutomaton)
	assertField(t, "RetryCeilingTotal", got.RetryCeilingTotal, 99, config.PolicySourceAutomaton)
	assertField(t, "MaxConcurrentTasks", got.MaxConcurrentTasks, 13, config.PolicySourceAutomaton)
	assertField(t, "DefaultAutoPR", got.DefaultAutoPR, true, config.PolicySourceAutomaton)

	// Per-field escalation merge: Enabled came from the Automaton (set),
	// MaxFailures and Steps stayed at the project's values (the Automaton
	// left them unset).
	esc := got.FailureEscalation.Value
	if esc.Enabled != true ||
		esc.MaxFailures != 3 ||
		len(esc.Steps) != 1 ||
		esc.Steps[0].Backend != "gpt" {
		t.Errorf("FailureEscalation merge = %+v", esc)
	}
	if got.FailureEscalation.Source != config.PolicySourceAutomaton {
		t.Errorf("FailureEscalation source = %q, want %q (highest tier that set anything)",
			got.FailureEscalation.Source, config.PolicySourceAutomaton)
	}

	// Keys the Automaton left unset fall to the project tier.
	assertField(t, "PlanCoverageCheck", got.PlanCoverageCheck, "block", config.PolicySourceProject)
	assertField(t, "AutoFixRetries", got.AutoFixRetries, 2, config.PolicySourceProject)
	assertField(t, "LaneFillGPULanes", got.LaneFillGPULanes, 4, config.PolicySourceProject)
	assertField(t, "ParallelReadOnlyTasks", got.ParallelReadOnlyTasks, true, config.PolicySourceProject)

	// (Every field above a daemon-tier assertion already demonstrates the
	// "no higher tier set it" fall-through against the project/daemon chain.)
}

// --- validation --------------------------------------------------------------

func TestValidateProjectPolicy_RejectsUnknownValues(t *testing.T) {
	cases := []struct {
		name   string
		policy *config.ProjectPolicy
		why    string
	}{
		{"empty policy is valid", &config.ProjectPolicy{}, ""},
		{"unknown retry category", &config.ProjectPolicy{RetryPolicy: map[string]int{"infra": 1, "bugfix": 2}}, "retry_policy"},
		{"negative retry budget", &config.ProjectPolicy{RetryPolicy: map[string]int{"infra": -1}}, "retry_policy"},
		{"auto fix retries below zero", &config.ProjectPolicy{AutoFixRetries: ppInt(-3)}, "auto_fix_retries"},
		{"ceiling below zero", &config.ProjectPolicy{RetryCeilingTotal: ppInt(-1)}, "retry_ceiling_total"},
		{"concurrency below one", &config.ProjectPolicy{MaxConcurrentTasks: ppInt(0)}, "max_concurrent_tasks"},
		{"escalate-after negative", &config.ProjectPolicy{InfraRetryEscalateAfter: ppInt(-1)}, "infra_retry_escalate_after"},
		{"ratio out of range", &config.ProjectPolicy{PlanCoverageMinRatio: ppFloat(1.5)}, "plan_coverage_min_ratio"},
		{"bad plan coverage check", &config.ProjectPolicy{PlanCoverageCheck: ppStr("maybe")}, "plan_coverage_check"},
		{"bad plan review mode", &config.ProjectPolicy{PlanReviewMode: ppStr("loud")}, "plan_review_mode"},
		{"bad scope lint", &config.ProjectPolicy{ScopeLint: ppStr("nope")}, "scope_lint"},
		{"bad uncommitted policy", &config.ProjectPolicy{UncommittedWorkPolicy: ppStr("yell")}, "uncommitted_work_policy"},
		{"bad uncommitted scope", &config.ProjectPolicy{UncommittedWorkScope: ppStr("everything")}, "uncommitted_work_scope"},
		{"negative escalation attempts", &config.ProjectPolicy{
			FailureEscalation: &config.FailureEscalationLevel{Steps: []config.FailureEscalationStepPolicy{{Attempts: -2}}},
		}, "failure_escalation"},
		{"bad lane fill", &config.ProjectPolicy{LaneFillGPULanes: ppInt(-1)}, "lane_fill_gpu_lanes"},
		{
			name:   "fully valid policy",
			policy: projectFixture(),
			why:    "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := config.ValidateProjectPolicy(tc.policy, "projects.acme.policy")
			if tc.why == "" {
				if err != nil {
					t.Fatalf("expected valid, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected a validation error naming %q, got nil", tc.why)
			}
			if !strings.Contains(err.Error(), tc.why) {
				t.Errorf("error %q does not name the offending key %q", err, tc.why)
			}
			if !strings.Contains(err.Error(), "projects.acme.policy") {
				t.Errorf("error %q does not carry the config key path", err)
			}
		})
	}
}

// --- PRD adapter -------------------------------------------------------------

func TestPRDPolicyOverrides(t *testing.T) {
	step := FailureEscalationStep{Backend: "gpt", Model: "big", Effort: "high", Attempts: 4}
	prd := &PRD{
		RetryPolicy:        map[string]int{"infra": 3, "content": 2},
		RetryCeilingTotal:  21,
		MaxConcurrentTasks: 17,
		FailureEscalation: &FailureEscalationPolicy{
			Enabled:     ppBool(true),
			MaxFailures: 8,
			Steps:       []FailureEscalationStep{step},
		},
		Git: GitWorkflow{AutoPR: true},
	}
	ov := prd.PolicyOverrides()

	if want := map[string]int{"infra": 3, "content": 2}; !mapiq(ov.RetryPolicy, want) {
		t.Errorf("RetryPolicy = %v, want %v", ov.RetryPolicy, want)
	}
	if ov.RetryCeilingTotal == nil || *ov.RetryCeilingTotal != 21 {
		t.Errorf("RetryCeilingTotal = %v, want 21", ov.RetryCeilingTotal)
	}
	if ov.MaxConcurrentTasks == nil || *ov.MaxConcurrentTasks != 17 {
		t.Errorf("MaxConcurrentTasks = %v, want 17", ov.MaxConcurrentTasks)
	}
	if ov.DefaultAutoPR == nil || *ov.DefaultAutoPR != true {
		t.Errorf("DefaultAutoPR = %v, want true", ov.DefaultAutoPR)
	}
	esc := ov.FailureEscalation
	if esc == nil || esc.Enabled == nil || !*esc.Enabled || esc.MaxFailures == nil || *esc.MaxFailures != 8 || len(esc.Steps) != 1 || esc.Steps[0].Attempts != 4 {
		t.Errorf("FailureEscalation = %+v", esc)
	}

	// Zero-valued PRD: every tier slot stays unset so lower tiers supply it.
	empty := (&PRD{}).PolicyOverrides()
	if len(empty.RetryPolicy) != 0 || empty.RetryCeilingTotal != nil ||
		empty.MaxConcurrentTasks != nil || empty.FailureEscalation != nil ||
		empty.DefaultAutoPR != nil {
		t.Errorf("empty PRD overrides = %+v, want all unset", empty)
	}

	// A nil PRD must be safe.
	if got := (*PRD)(nil).PolicyOverrides(); len(got.RetryPolicy) != 0 || got.MaxConcurrentTasks != nil {
		t.Errorf("nil PRD overrides = %+v, want all unset", got)
	}
}

// The adapter output feeds the automaton tier of the resolver: its values
// must actually win.
func TestPRDPolicyOverrides_WinInResolution(t *testing.T) {
	d := daemonFixture()
	p := projectFixture()
	mustValidate(t, p)
	prd := &PRD{MaxConcurrentTasks: 25, Git: GitWorkflow{AutoPR: true}}
	got := config.ResolveProjectPolicy(d, p, prd.PolicyOverrides())
	assertField(t, "MaxConcurrentTasks", got.MaxConcurrentTasks, 25, config.PolicySourceAutomaton)
	// The project fixture sets default_auto_pr = false; the Automaton's
	// explicit auto_pr: true wins over it.
	assertField(t, "DefaultAutoPR", got.DefaultAutoPR, true, config.PolicySourceAutomaton)
	// Keys the Automaton leaves unset stay at the lower tier.
	assertField(t, "AutoFixRetries", got.AutoFixRetries, 2, config.PolicySourceProject)
}

func mapiq(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// --- JSON / YAML round-trips ---------------------------------------------------

func TestProjectPolicyRoundTrips(t *testing.T) {
	p := projectFixture()
	// Give the map fields one empty step and one pointer-heavy case.
	if p.FailureEscalation != nil {
		p.FailureEscalation.Steps = append(p.FailureEscalation.Steps, config.FailureEscalationStepPolicy{Attempts: 2})
	}

	t.Run("json", func(t *testing.T) {
		b, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var back config.ProjectPolicy
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if !reflect.DeepEqual(back, *p) {
			t.Errorf("json round-trip = %+v, want %+v", back, *p)
		}
	})

	t.Run("yaml", func(t *testing.T) {
		b, err := yaml.Marshal(p)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var back config.ProjectPolicy
		if err := yaml.Unmarshal(b, &back); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if !reflect.DeepEqual(back, *p) {
			t.Errorf("yaml round-trip = %+v, want %+v", back, *p)
		}
	})

	t.Run("project entry json", func(t *testing.T) {
		entry := config.ProjectConfigEntry{Dir: "/srv/acme", Policy: p}
		b, err := json.Marshal(entry)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var back config.ProjectConfigEntry
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if back.Dir != entry.Dir || !reflect.DeepEqual(back.Policy, p) {
			t.Errorf("entry json round-trip = %+v", back)
		}
	})

	t.Run("project entry yaml", func(t *testing.T) {
		entry := config.ProjectConfigEntry{Dir: "/srv/acme", Policy: p}
		b, err := yaml.Marshal(entry)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var back config.ProjectConfigEntry
		if err := yaml.Unmarshal(b, &back); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if back.Dir != entry.Dir || !reflect.DeepEqual(back.Policy, p) {
			t.Errorf("entry yaml round-trip = %+v", back)
		}
	})

	// Absent policy key round-trips to nil (a bare project).
	var bare config.ProjectConfigEntry
	if err := yaml.Unmarshal([]byte("dir: /srv/plain\n"), &bare); err != nil {
		t.Fatalf("unmarshal bare: %v", err)
	}
	if bare.Policy != nil {
		t.Errorf("bare project has Policy = %+v, want nil", bare.Policy)
	}
}

// resolveTaskConcurrency is the executor's single decision point for the
// task-session concurrency limit: Automaton (PRD) > project policy > daemon.
func TestResolveTaskConcurrency_TierPrecedence(t *testing.T) {
	m := &Manager{cfg: Config{MaxConcurrentTasks: 7}}

	if got := m.resolveTaskConcurrency(&PRD{}); got != 7 {
		t.Errorf("no tiers set: got %d, want daemon 7", got)
	}
	if got := m.resolveTaskConcurrency(&PRD{MaxConcurrentTasks: 3}); got != 3 {
		t.Errorf("PRD tier: got %d, want 3", got)
	}

	// Project-policy tier (injected hook) wins over the daemon default...
	m.projectPolicyConcurrency = func(*PRD) (int, bool) { return 5, true }
	if got := m.resolveTaskConcurrency(&PRD{}); got != 5 {
		t.Errorf("project tier: got %d, want 5", got)
	}
	// ...but stays below the PRD tier.
	if got := m.resolveTaskConcurrency(&PRD{MaxConcurrentTasks: 3}); got != 3 {
		t.Errorf("PRD over project: got %d, want 3", got)
	}

	// A hook that reports "no project policy" defers to the daemon default.
	m.projectPolicyConcurrency = func(*PRD) (int, bool) { return 0, false }
	if got := m.resolveTaskConcurrency(&PRD{}); got != 7 {
		t.Errorf("unset hook: got %d, want daemon 7", got)
	}
	m.projectPolicyConcurrency = nil
	if got := m.resolveTaskConcurrency(&PRD{}); got != 7 {
		t.Errorf("nil hook: got %d, want daemon 7", got)
	}
}
