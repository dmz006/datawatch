package autonomous

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func escBool(v bool) *bool { return &v }

func newEscalationManager(t *testing.T, global FailureEscalationPolicy) *Manager {
	t.Helper()
	cfg := DefaultConfig()
	cfg.FailureEscalation = global
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func TestFailureEscalation_Defaults(t *testing.T) {
	d := defaultFailureEscalationPolicy()
	if d.Enabled == nil || !*d.Enabled || d.MaxFailures != 3 || len(d.Steps) != 0 {
		t.Fatalf("default policy = %+v, want enabled, 3 failures, no steps", d)
	}
	if got := DefaultConfig().FailureEscalation; got.Enabled == nil || !*got.Enabled || got.MaxFailures != 3 {
		t.Fatalf("DefaultConfig().FailureEscalation = %+v", got)
	}

	// An unset global policy (zero value) reads as the defaults, and the
	// manager reports the source as global for every field.
	m := newEscalationManager(t, FailureEscalationPolicy{})
	eff := m.resolveFailureEscalation(&PRD{}, &Task{})
	if !eff.Enabled || eff.MaxFailures != 3 || len(eff.Steps) != 0 {
		t.Fatalf("effective = %+v, want defaults", eff)
	}
	want := EscalationSources{Enabled: "global", MaxFailures: "global", Steps: "global"}
	if eff.Sources != want {
		t.Fatalf("sources = %+v, want %+v", eff.Sources, want)
	}
	// nil prd and nil task are tolerated.
	if got := m.resolveFailureEscalation(nil, nil); got.MaxFailures != 3 || !got.Enabled {
		t.Fatalf("nil args effective = %+v", got)
	}
	// SetConfig normalises the same way.
	m.SetConfig(Config{})
	if p := m.Config().FailureEscalation; p.Enabled == nil || !*p.Enabled || p.MaxFailures != 3 {
		t.Fatalf("SetConfig left policy %+v un-normalised", p)
	}
}

func TestFailureEscalation_GlobalValuesApply(t *testing.T) {
	steps := []FailureEscalationStep{{Backend: "pi-prompt", Model: "qwen3.8:27b", Attempts: 2}, {Backend: "claude-code", Model: "claude-opus-5-5"}}
	m := newEscalationManager(t, FailureEscalationPolicy{Enabled: escBool(false), MaxFailures: 4, Steps: steps})
	eff := m.resolveFailureEscalation(&PRD{}, &Task{})
	if eff.Enabled || eff.MaxFailures != 4 || !reflect.DeepEqual(eff.Steps, steps) {
		t.Fatalf("effective = %+v", eff)
	}
	// The returned steps never alias the manager's config.
	eff.Steps[0].Backend = "changed"
	if m.Config().FailureEscalation.Steps[0].Backend != "pi-prompt" {
		t.Fatal("resolved steps alias the global config")
	}
}

func TestFailureEscalation_OverrideOrderPerField(t *testing.T) {
	globalSteps := []FailureEscalationStep{{Backend: "pi-prompt", Model: "qwen3.8:27b"}}
	prdSteps := []FailureEscalationStep{{Backend: "claude-code", Model: "claude-sonnet-5-5"}}
	taskSteps := []FailureEscalationStep{{Backend: "claude-code", Model: "claude-opus-5-5", Attempts: 2}}
	m := newEscalationManager(t, FailureEscalationPolicy{Enabled: escBool(true), MaxFailures: 3, Steps: globalSteps})

	t.Run("prd beats global", func(t *testing.T) {
		prd := &PRD{FailureEscalation: &FailureEscalationPolicy{Enabled: escBool(false), MaxFailures: 5, Steps: prdSteps}}
		eff := m.resolveFailureEscalation(prd, &Task{})
		if eff.Enabled || eff.MaxFailures != 5 || !reflect.DeepEqual(eff.Steps, prdSteps) {
			t.Fatalf("effective = %+v", eff)
		}
		if eff.Sources != (EscalationSources{Enabled: "prd", MaxFailures: "prd", Steps: "prd"}) {
			t.Fatalf("sources = %+v", eff.Sources)
		}
	})

	t.Run("task beats prd beats global", func(t *testing.T) {
		prd := &PRD{FailureEscalation: &FailureEscalationPolicy{Enabled: escBool(false), MaxFailures: 5, Steps: prdSteps}}
		task := &Task{FailureEscalation: &FailureEscalationPolicy{Enabled: escBool(true), MaxFailures: 6, Steps: taskSteps}}
		eff := m.resolveFailureEscalation(prd, task)
		if !eff.Enabled || eff.MaxFailures != 6 || !reflect.DeepEqual(eff.Steps, taskSteps) {
			t.Fatalf("effective = %+v", eff)
		}
		if eff.Sources != (EscalationSources{Enabled: "task", MaxFailures: "task", Steps: "task"}) {
			t.Fatalf("sources = %+v", eff.Sources)
		}
	})

	t.Run("task sets only max_failures and inherits the prd steps", func(t *testing.T) {
		prd := &PRD{FailureEscalation: &FailureEscalationPolicy{Steps: prdSteps}}
		task := &Task{FailureEscalation: &FailureEscalationPolicy{MaxFailures: 4}}
		eff := m.resolveFailureEscalation(prd, task)
		if eff.MaxFailures != 4 || !reflect.DeepEqual(eff.Steps, prdSteps) || !eff.Enabled {
			t.Fatalf("effective = %+v", eff)
		}
		if eff.Sources != (EscalationSources{Enabled: "global", MaxFailures: "task", Steps: "prd"}) {
			t.Fatalf("sources = %+v", eff.Sources)
		}
	})

	t.Run("enabled resolves independently", func(t *testing.T) {
		prd := &PRD{FailureEscalation: &FailureEscalationPolicy{MaxFailures: 5}}
		task := &Task{FailureEscalation: &FailureEscalationPolicy{Enabled: escBool(false)}}
		eff := m.resolveFailureEscalation(prd, task)
		if eff.Enabled || eff.MaxFailures != 5 || !reflect.DeepEqual(eff.Steps, globalSteps) {
			t.Fatalf("effective = %+v", eff)
		}
		if eff.Sources != (EscalationSources{Enabled: "task", MaxFailures: "prd", Steps: "global"}) {
			t.Fatalf("sources = %+v", eff.Sources)
		}
	})

	t.Run("empty override structs inherit everything", func(t *testing.T) {
		prd := &PRD{FailureEscalation: &FailureEscalationPolicy{}}
		task := &Task{FailureEscalation: &FailureEscalationPolicy{}}
		eff := m.resolveFailureEscalation(prd, task)
		if !eff.Enabled || eff.MaxFailures != 3 || !reflect.DeepEqual(eff.Steps, globalSteps) {
			t.Fatalf("effective = %+v", eff)
		}
		if eff.Sources != (EscalationSources{Enabled: "global", MaxFailures: "global", Steps: "global"}) {
			t.Fatalf("sources = %+v", eff.Sources)
		}
	})
}

func TestFailureEscalation_Validation(t *testing.T) {
	if err := validateFailureEscalationPolicy(nil); err != nil {
		t.Fatalf("nil policy: %v", err)
	}
	ok := &FailureEscalationPolicy{MaxFailures: 4, Steps: []FailureEscalationStep{
		{Backend: "pi-prompt", Model: "ollama/qwen3.8:27b", Attempts: 2},
		{Backend: "claude-code", Model: "claude-opus-5-5", Effort: EffortHigh},
		{},
	}}
	if err := validateFailureEscalationPolicy(ok); err != nil {
		t.Fatalf("valid policy rejected: %v", err)
	}

	tooMany := &FailureEscalationPolicy{Steps: make([]FailureEscalationStep, 9)}
	eight := &FailureEscalationPolicy{Steps: make([]FailureEscalationStep, 8)}
	if err := validateFailureEscalationPolicy(eight); err != nil {
		t.Fatalf("8 steps rejected: %v", err)
	}

	cases := []struct {
		name   string
		policy *FailureEscalationPolicy
		want   []string
	}{
		{"negative max_failures", &FailureEscalationPolicy{MaxFailures: -1}, []string{"max_failures", "-1"}},
		{"too many steps", tooMany, []string{"steps", "9", "at most 8"}},
		{"negative attempts", &FailureEscalationPolicy{Steps: []FailureEscalationStep{{}, {Backend: "claude-code", Attempts: -2}}}, []string{"steps[1].attempts", "-2"}},
		{"bad effort", &FailureEscalationPolicy{Steps: []FailureEscalationStep{{Backend: "claude-code", Effort: "extreme"}}}, []string{"steps[0].effort", "extreme", "low, medium"}},
		{"model without backend", &FailureEscalationPolicy{Steps: []FailureEscalationStep{{Model: "qwen3.8:27b"}}}, []string{"steps[0]", "needs a backend"}},
		{"claude-code with pi model", &FailureEscalationPolicy{Steps: []FailureEscalationStep{{}, {}, {Backend: "claude-code", Model: "anthropic/claude-haiku-5-5"}}}, []string{"steps[2]", "claude-code", "bare Claude model name"}},
		{"pi with bare claude model", &FailureEscalationPolicy{Steps: []FailureEscalationStep{{Backend: "pi-prompt", Model: "claude-haiku-5-5"}}}, []string{"steps[0]", "anthropic/claude-haiku-5-5"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateFailureEscalationPolicy(c.policy)
			if err == nil {
				t.Fatal("want an error")
			}
			for _, w := range c.want {
				if !strings.Contains(err.Error(), w) {
					t.Fatalf("error %q does not mention %q", err, w)
				}
			}
		})
	}

	err := validateFailureEscalationPolicy(&FailureEscalationPolicy{Steps: []FailureEscalationStep{{Backend: "claude-code", Model: "a/b"}}})
	if !errors.Is(err, ErrModelBackendMismatch) {
		t.Fatalf("pair error does not wrap ErrModelBackendMismatch: %v", err)
	}
}

func TestFailureEscalation_ParseAndFormatSteps(t *testing.T) {
	steps, err := ParseFailureEscalationSteps("pi-prompt:qwen3.8:27b@2, claude-code:claude-sonnet-5-5,-@3,-,claude-code:")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []FailureEscalationStep{
		{Backend: "pi-prompt", Model: "qwen3.8:27b", Attempts: 2},
		{Backend: "claude-code", Model: "claude-sonnet-5-5"},
		{Attempts: 3},
		{},
		{Backend: "claude-code"},
	}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("steps = %+v, want %+v", steps, want)
	}

	// Format then parse is an exact round trip.
	text := FormatFailureEscalationSteps(steps)
	if text != "pi-prompt:qwen3.8:27b@2,claude-code:claude-sonnet-5-5,-@3,-,claude-code:" {
		t.Fatalf("format = %q", text)
	}
	again, err := ParseFailureEscalationSteps(text)
	if err != nil || !reflect.DeepEqual(again, steps) {
		t.Fatalf("round trip = %+v, %v", again, err)
	}

	// Empty text is no steps, and effort is not part of the text form.
	if got, err := ParseFailureEscalationSteps("  "); err != nil || got != nil {
		t.Fatalf("empty text = %+v, %v", got, err)
	}
	if got := FormatFailureEscalationSteps([]FailureEscalationStep{{Backend: "claude-code", Model: "claude-opus-5-5", Effort: EffortMax}}); got != "claude-code:claude-opus-5-5" {
		t.Fatalf("effort leaked into text form: %q", got)
	}
	if got := FormatFailureEscalationSteps(nil); got != "" {
		t.Fatalf("format nil = %q", got)
	}
}

func TestFailureEscalation_ParseStepsErrors(t *testing.T) {
	cases := []struct{ text, want string }{
		{"claude-code", "backend:model"},
		{"claude-code:opus@x", "@attempts"},
		{"claude-code:opus@0", "@attempts"},
		{"claude-code:opus@-1", "@attempts"},
		{"-@", "@attempts"},
		{":qwen3.8:27b", "no backend"},
		{"claude-code:opus,,pi-prompt:x", "step 1 is empty"},
		{"claude-code:opus,", "step 1 is empty"},
	}
	for _, c := range cases {
		_, err := ParseFailureEscalationSteps(c.text)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("Parse(%q) error = %v, want mention of %q", c.text, err, c.want)
		}
	}
}

func TestFailureEscalation_JSONRoundTrip(t *testing.T) {
	started := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	task := Task{
		ID:                 "t1",
		FailureEscalation:  &FailureEscalationPolicy{Enabled: escBool(false), MaxFailures: 4, Steps: []FailureEscalationStep{{Backend: "claude-code", Model: "claude-opus-5-5", Effort: EffortHigh, Attempts: 2}}},
		EscalationFailures: 2,
		Status:             TaskReviewNeeded,
		Attempts: []TaskAttempt{{
			Number: 1, Backend: "pi-prompt", Model: "qwen3.8:27b", Effort: EffortMedium,
			StartedAt: started, EndedAt: started.Add(90 * time.Second), DurationSeconds: 90,
			Outcome: "failed", FailureCategory: "content", FailureSummary: "tests failed",
			VerifierSummary: "2 issues", VerifierIssues: []string{"a", "b"},
			ChangedFiles: []string{"x.go"}, SnapshotRef: "snap1", Escalated: true,
		}},
		ReviewSummary: &ReviewSummary{HeldAt: started, FailureCount: 3, TotalSeconds: 270, Text: "attempt 1: failed"},
	}
	b, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"attempts"`, `"failure_escalation"`, `"escalation_failures"`, `"review_summary"`, `"max_failures"`, `"steps"`, `"duration_seconds"`, `"snapshot_ref"`, `"held_at"`, `"review_needed"`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("task JSON missing %s: %s", key, b)
		}
	}
	var back Task
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, task) {
		t.Fatalf("task round trip differs:\n got %+v\nwant %+v", back, task)
	}

	prd := PRD{ID: "p1", FailureEscalation: &FailureEscalationPolicy{MaxFailures: 5}}
	b, err = json.Marshal(prd)
	if err != nil {
		t.Fatal(err)
	}
	var prdBack PRD
	if err := json.Unmarshal(b, &prdBack); err != nil {
		t.Fatal(err)
	}
	if prdBack.FailureEscalation == nil || prdBack.FailureEscalation.MaxFailures != 5 {
		t.Fatalf("prd round trip = %+v", prdBack.FailureEscalation)
	}
}

func TestFailureEscalation_OldStoredRecordsLoad(t *testing.T) {
	var task Task
	if err := json.Unmarshal([]byte(`{"id":"t1","title":"old","status":"failed","retries":1}`), &task); err != nil {
		t.Fatal(err)
	}
	if task.FailureEscalation != nil || task.Attempts != nil || task.EscalationFailures != 0 || task.ReviewSummary != nil {
		t.Fatalf("old task grew escalation state: %+v", task)
	}
	var prd PRD
	if err := json.Unmarshal([]byte(`{"id":"p1","status":"approved"}`), &prd); err != nil {
		t.Fatal(err)
	}
	if prd.FailureEscalation != nil {
		t.Fatalf("old prd grew an override: %+v", prd.FailureEscalation)
	}

	// New fields stay out of the JSON of a record that never used them.
	b, _ := json.Marshal(task)
	for _, key := range []string{"failure_escalation", "attempts", "escalation_failures", "review_summary"} {
		if strings.Contains(string(b), key) {
			t.Errorf("unused field %q written: %s", key, b)
		}
	}
}

func TestFailureEscalation_ReviewNeededIsNotTerminalOrFailure(t *testing.T) {
	if TaskReviewNeeded != "review_needed" {
		t.Fatalf("TaskReviewNeeded = %q", TaskReviewNeeded)
	}
	if !IsKnownEventKind(TaskStatusKind(TaskReviewNeeded)) {
		t.Fatal("review_needed is not a known subscription event kind")
	}
	if !errors.Is(ErrTaskNotInReview, ErrTaskNotInReview) || ErrTaskNotInReview.Error() == "" {
		t.Fatal("ErrTaskNotInReview malformed")
	}
}
