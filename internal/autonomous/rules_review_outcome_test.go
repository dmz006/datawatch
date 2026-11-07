package autonomous

import (
	"bytes"
	"log"
	"os"

	"encoding/json"
	"github.com/dmz006/datawatch/internal/audit"
	"strings"
	"testing"
	"time"
)

func TestRulesReviewOutcomeCopiesIssues(t *testing.T) {
	at := time.Date(2026, 11, 4, 1, 2, 3, 0, time.UTC)
	v := GuardrailVerdict{Summary: "blocked", Issues: []string{"a", "b"}}
	o := newRulesReviewOutcome(v, RulesReviewActionFixTaskCreated, "why", 2, 3, 1, at)
	if len(o.Findings) != 2 || o.Findings[0] != "a" || o.Findings[1] != "b" {
		t.Fatalf("findings = %v", o.Findings)
	}
	if o.Action != "fix_task_created" || o.Attempt != 2 || o.RetryBudget != 3 || o.RetriesSpent != 1 || o.Reason != "why" || !o.At.Equal(at) {
		t.Fatalf("fields wrong: %+v", o)
	}
	v.Issues[0] = "changed"
	if o.Findings[0] != "a" {
		t.Fatal("findings alias the verdict slice")
	}
}

func TestRulesReviewOutcomeFallsBackToSummary(t *testing.T) {
	o := newRulesReviewOutcome(GuardrailVerdict{Summary: "only summary"}, RulesReviewActionRetriesSpent, "", 1, 1, 1, time.Time{})
	if len(o.Findings) != 1 || o.Findings[0] != "only summary" {
		t.Fatalf("findings = %v", o.Findings)
	}
}

func TestRulesReviewOutcomeFindingsNeverNil(t *testing.T) {
	o := newRulesReviewOutcome(GuardrailVerdict{}, RulesReviewActionAutoFixDisabled, "", 0, 0, 0, time.Time{})
	if o.Findings == nil {
		t.Fatal("Findings is nil")
	}
	b, _ := json.Marshal(o)
	if !strings.Contains(string(b), `"findings":[]`) {
		t.Fatalf("json = %s", b)
	}
}

func TestRulesReviewOutcomeJSONRoundTrip(t *testing.T) {
	var plain PRD
	b, err := json.Marshal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "rules_review_outcome") {
		t.Fatal("unset field must be omitted")
	}
	var back PRD
	if err := json.Unmarshal(b, &back); err != nil || back.RulesReviewOutcome != nil {
		t.Fatalf("unset round trip: %v %+v", err, back.RulesReviewOutcome)
	}
	p := PRD{RulesReviewOutcome: newRulesReviewOutcome(GuardrailVerdict{Issues: []string{"x"}, Summary: "s"},
		RulesReviewActionFixTaskFailed, "r", 1, 2, 1, time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC))}
	b, err = json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var got PRD
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.RulesReviewOutcome == nil || got.RulesReviewOutcome.Action != "fix_task_failed" || got.RulesReviewOutcome.Findings[0] != "x" {
		t.Fatalf("set round trip: %+v", got.RulesReviewOutcome)
	}
}

func TestRulesReviewOutcomeReviewNowHonoursClock(t *testing.T) {
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	m := &Manager{attemptClock: func() time.Time { return fixed }}
	if !m.reviewNow().Equal(fixed) {
		t.Fatal("reviewNow ignored the fixed clock")
	}
	if (&Manager{}).reviewNow().IsZero() {
		t.Fatal("reviewNow with no clock must use wall time")
	}
}

func TestRulesReviewOutcomeJudgeOutcome(t *testing.T) {
	cases := []struct {
		name string
		v    GuardrailVerdict
		want string
	}{
		{"could not start", GuardrailVerdict{Outcome: "block", Category: "infra", Summary: "task delivery failed: main prompt never appeared within 60s"}, JudgeKindCouldNotStart},
		{"timed out", GuardrailVerdict{Outcome: "block", Category: JudgeTimeoutCategory, Summary: "timed out"}, JudgeKindTimedOut},
		{"found problem", GuardrailVerdict{Outcome: "block", Summary: "bad", Issues: []string{"x"}}, JudgeKindFoundProblem},
		{"policy problem", GuardrailVerdict{Outcome: "block", Category: "policy", Summary: "bad"}, JudgeKindFoundProblem},
		{"scope problem", GuardrailVerdict{Outcome: "block", Category: "scope", Summary: "bad"}, JudgeKindFoundProblem},
		{"unparseable response", GuardrailVerdict{Outcome: "block", Category: "invalid-response"}, ""},
		{"pass", GuardrailVerdict{Outcome: "pass"}, ""},
		{"no judge ran", GuardrailVerdict{}, ""},
	}
	for _, c := range cases {
		o := newRulesReviewOutcome(c.v, RulesReviewActionFixTaskCreated, "", 1, 1, 0, time.Time{})
		if o.JudgeOutcome != c.want {
			t.Errorf("%s: JudgeOutcome = %q, want %q", c.name, o.JudgeOutcome, c.want)
		}
		b, _ := json.Marshal(o)
		has := strings.Contains(string(b), `"judge_outcome"`)
		if has != (c.want != "") {
			t.Errorf("%s: json %s omitempty wrong", c.name, b)
		}
	}
}

func TestRecordJudgeOutcomeKindWording(t *testing.T) {
	cases := []struct {
		name string
		v    GuardrailVerdict
		kind string
		want string
	}{
		{"could not start", GuardrailVerdict{Outcome: "block", Category: "infra", Summary: couldNotStartMsg}, JudgeKindCouldNotStart, "judge could not start"},
		{"timed out", GuardrailVerdict{Outcome: "block", Category: JudgeTimeoutCategory}, JudgeKindTimedOut, "judge timed out"},
		{"found a problem", GuardrailVerdict{Outcome: "block", Summary: "rule skipped"}, JudgeKindFoundProblem, "judge found a problem"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
			var action string
			var details map[string]any
			m.SetAuditFn(func(a string, d map[string]any) { action, details = a, d })
			var buf bytes.Buffer
			log.SetOutput(&buf)
			defer log.SetOutput(os.Stderr)

			m.recordJudgeOutcomeKind("prd1", c.v)

			if got := judgeOutcomeKind(c.v); got != c.kind {
				t.Fatalf("kind = %q, want %q", got, c.kind)
			}
			if !strings.Contains(buf.String(), c.want) {
				t.Fatalf("log %q lacks %q", buf.String(), c.want)
			}
			if action != audit.ActionJudgeOutcome || details["headline"] != c.want || details["kind"] != c.kind {
				t.Fatalf("events entry = %q %v", action, details)
			}
			for _, s := range []string{buf.String(), details["headline"].(string)} {
				if strings.Contains(s, "PRD") {
					t.Fatalf("%q must say Automaton, never PRD", s)
				}
			}
		})
	}
}

func TestRecordJudgeOutcomeKindSilentForPass(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	called := false
	m.SetAuditFn(func(string, map[string]any) { called = true })
	m.recordJudgeOutcomeKind("prd1", GuardrailVerdict{Outcome: "pass"})
	if called {
		t.Fatal("a pass must not write an Events entry")
	}
}
