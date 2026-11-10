package server

import (
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func TestAutomataEventRetryCheckSourcesRetryAndHold(t *testing.T) {
	base := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	next := base.Add(5 * time.Minute)
	tasks := []autonomous.InfraRetryTask{
		{
			// Retried once, not held: info, next retry due time is the next action.
			AutomatonID: "auto-1", TaskID: "t-1", RetryCount: 1, MaxTries: 3,
			Retries: []autonomous.InfraRetryEntry{
				{Number: 1, Class: "network", Reason: "dial timeout", At: base, DelaySeconds: 300, NextAt: next},
			},
		},
		{
			// Held after retries ran out, with an open hold question: warn.
			AutomatonID: "auto-2", TaskID: "t-2", RetryCount: 3, MaxTries: 3, Held: true,
			HeldCause: "retries exhausted", QuestionID: "q-9",
			Retries: []autonomous.InfraRetryEntry{
				{Number: 2, Class: "rate_limit", At: base.Add(-time.Hour), NextAt: base.Add(-30 * time.Minute)},
				{Number: 3, Class: "rate_limit", Reason: "429 from model", At: base.Add(-time.Minute)},
			},
		},
		{
			// Held with no retry history and no question: warn, review action.
			AutomatonID: "auto-3", TaskID: "t-3", Held: true, HeldCause: "config",
		},
		{
			// Neither retried nor held: nothing to record.
			AutomatonID: "auto-4", TaskID: "t-4",
		},
	}

	var gotLimit int
	got, err := AutomataEventsFromInfraRetries(func(limit int) []autonomous.InfraRetryTask {
		gotLimit = limit
		return tasks
	}, 25)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotLimit != 25 {
		t.Errorf("source got limit %d, want 25", gotLimit)
	}
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3 (empty task skipped)", len(got))
	}

	retried := got[0]
	if retried.Kind != AutomataEventRetry || retried.Automaton != "auto-1" || retried.Severity != AutomataSeverityInfo {
		t.Errorf("retried event = %+v, want retry info for auto-1", retried)
	}
	if retried.Summary != "Task t-1 of Automaton auto-1 retried after network failure, attempt 1 of 3" {
		t.Errorf("retried summary = %q", retried.Summary)
	}
	if retried.NextAction != "Retry 1 runs at "+next.Format(time.RFC3339)+"." {
		t.Errorf("retried next action = %q", retried.NextAction)
	}
	if retried.Reason != "dial timeout" || !retried.Time.Equal(base) {
		t.Errorf("retried reason/time = %q/%v", retried.Reason, retried.Time)
	}

	held := got[1]
	if held.Severity != AutomataSeverityWarn || held.Automaton != "auto-2" {
		t.Errorf("held event = %+v, want warn for auto-2", held)
	}
	if held.Summary != "Task t-2 of Automaton auto-2 held after 3 of 3 infrastructure retries" {
		t.Errorf("held summary = %q", held.Summary)
	}
	if held.NextAction != "Answer the open question to resume or stop the held task." {
		t.Errorf("held next action = %q", held.NextAction)
	}
	if held.Reason != "retries exhausted" {
		t.Errorf("held reason = %q, want the hold cause", held.Reason)
	}
	if !held.Time.Equal(base.Add(-time.Minute)) {
		t.Errorf("held time = %v, want latest retry %v", held.Time, base.Add(-time.Minute))
	}

	noRetry := got[2]
	if noRetry.Severity != AutomataSeverityWarn || noRetry.Automaton != "auto-3" {
		t.Errorf("no-retry hold = %+v", noRetry)
	}
	if noRetry.Summary != "Task t-3 of Automaton auto-3 held after 0 of 0 infrastructure retries" {
		t.Errorf("no-retry hold summary = %q", noRetry.Summary)
	}
	if noRetry.NextAction != "Review the held task and decide whether to retry it." {
		t.Errorf("no-retry next action = %q", noRetry.NextAction)
	}
	if !noRetry.Time.IsZero() {
		t.Errorf("no-retry time = %v, want zero (nothing recorded)", noRetry.Time)
	}

	for _, e := range got {
		if err := e.Valid(); err != nil {
			t.Errorf("event %q invalid: %v", e.ID, err)
		}
	}
}

func TestAutomataEventRetryCheckSourcesRetryNoNextAndPRDWord(t *testing.T) {
	// A retried entry with no NextAt has no next action; PRD in the reason is
	// rewritten to Automaton so the event stays valid.
	tasks := []autonomous.InfraRetryTask{{
		AutomatonID: "auto-5", TaskID: "t-5", RetryCount: 1, MaxTries: 2,
		Retries: []autonomous.InfraRetryEntry{
			{Number: 1, Class: "network", Reason: "PRD  worker\nlost", At: time.Unix(100, 0).UTC()},
		},
	}}
	got, err := AutomataEventsFromInfraRetries(func(int) []autonomous.InfraRetryTask { return tasks }, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	if got[0].NextAction != "" {
		t.Errorf("next action = %q, want empty when no next retry time", got[0].NextAction)
	}
	if got[0].Reason != "Automaton worker lost" {
		t.Errorf("reason = %q, want PRD replaced and whitespace collapsed", got[0].Reason)
	}
	if err := got[0].Valid(); err != nil {
		t.Errorf("event invalid: %v", err)
	}
}

func TestAutomataEventRetryCheckSourcesRetryErrors(t *testing.T) {
	if _, err := AutomataEventsFromInfraRetries(nil, 10); err == nil {
		t.Error("nil source must be an error, not an empty log")
	}
	calls := 0
	src := func(int) []autonomous.InfraRetryTask { calls++; return nil }
	for _, limit := range []int{0, -3} {
		if _, err := AutomataEventsFromInfraRetries(src, limit); err == nil {
			t.Errorf("limit %d must be an error", limit)
		}
	}
	if calls != 0 {
		t.Errorf("source called %d times for invalid limit, want 0", calls)
	}
	got, err := AutomataEventsFromInfraRetries(src, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("empty source gave %v, want empty non-nil slice", got)
	}
}

func TestAutomataEventRetryCheckSourcesChecks(t *testing.T) {
	at := time.Date(2026, 11, 9, 9, 30, 0, 0, time.UTC)
	findings := []autonomous.StatusFinding{
		{PRDID: "auto-7", At: at, Source: "pre_start_verifier", Text: "Verifier for PRD auto-7 failed: missing go.mod"},
		{PRDID: "auto-8", At: at.Add(-time.Hour), Source: "after_done", Text: "   "},
		{PRDID: "", At: at.Add(-2 * time.Hour), Source: "plan_rules", Text: "Plan rule\nbroke"},
	}
	got, err := AutomataEventsFromChecks(func() []autonomous.StatusFinding { return findings })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3", len(got))
	}

	first := got[0]
	if first.Kind != AutomataEventCheck || first.Automaton != "auto-7" || first.Project != "" || first.Automation != "" {
		t.Errorf("check event = %+v, want check kind for auto-7 only", first)
	}
	if first.Summary != "Verifier for Automaton auto-7 failed: missing go.mod" {
		t.Errorf("summary = %q, want PRD rewritten to Automaton", first.Summary)
	}
	if first.Reason != "pre_start_verifier" || !first.Time.Equal(at) {
		t.Errorf("reason/time = %q/%v", first.Reason, first.Time)
	}

	blank := got[1]
	if blank.Summary != "Check after_done recorded a finding" {
		t.Errorf("blank text summary = %q, want source fallback", blank.Summary)
	}

	unscoped := got[2]
	if unscoped.Automaton != "" || unscoped.Summary != "Plan rule broke" {
		t.Errorf("unscoped event = %+v, want no automaton and one-line text", unscoped)
	}

	for _, e := range got {
		if err := e.Valid(); err != nil {
			t.Errorf("event %q invalid: %v", e.ID, err)
		}
		if strings.Contains(e.Summary, "PRD") {
			t.Errorf("summary %q still names PRD", e.Summary)
		}
	}
}

func TestAutomataEventRetryCheckSourcesChecksNilSource(t *testing.T) {
	if _, err := AutomataEventsFromChecks(nil); err == nil {
		t.Error("nil checks source must be an error, not an empty log")
	}
	got, err := AutomataEventsFromChecks(func() []autonomous.StatusFinding { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("empty findings gave %v, want empty non-nil slice", got)
	}
}
