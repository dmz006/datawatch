package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
)

// runCapLeaseManager builds a manager with one PRD task whose verifier holds
// the only "host" slot under VerifyHolder(task id).
func runCapLeaseManager(t *testing.T) (*autonomouspkg.Manager, *capacity.Ledger, string, string) {
	t.Helper()
	m, err := autonomouspkg.NewManager(t.TempDir(), autonomouspkg.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")
	if err := m.Store().SetStories(prd.ID, []autonomouspkg.Story{{Title: "S", Tasks: []autonomouspkg.Task{{Title: "T"}}}}); err != nil {
		t.Fatal(err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	taskID := prd.Story[0].Tasks[0].ID
	led := capacity.New(capacity.Options{PollInterval: time.Second})
	led.SetLimit("host", 1)
	m.SetCapacity(led, func(string, string) ([]string, string) { return []string{"host"}, "" })
	req := capacity.Request{Holder: autonomouspkg.VerifyHolder(taskID), PRDID: prd.ID, Pools: []string{"host"}}
	if err := led.Acquire(context.Background(), req, time.Second, nil, nil); err != nil {
		t.Fatal(err)
	}
	return m, led, prd.ID, taskID
}

func capEndWith(elapsed time.Duration, capSeconds int) error {
	return &autonomouspkg.OneShotNoOutputError{
		Msg: "verifier session ended at its run cap", RunCap: true,
		RunCapElapsed: elapsed, RunCapSeconds: capSeconds,
	}
}

// The hook runs once per run-cap end, before the next backend is chosen, and
// releases the lease exactly once.
func TestVerifierRunCapLeaseDecisionReleasesOnceBeforeRetry(t *testing.T) {
	m, led, prdID, taskID := runCapLeaseManager(t)
	var events []string
	run := func(backend, _ string) (string, error) {
		events = append(events, "run:"+backend)
		if backend == "ollama-a" {
			return "", capEndWith(95*time.Second, 90)
		}
		return `{"ok":true}`, nil
	}
	holderHeldAtHook := false
	content, err := runVerifierSessionWithRunCapHook([]string{"ollama-a", "ollama-b"}, "ollama-a", "",
		run, retryModelRegistryDefault,
		func(capEnd *autonomouspkg.OneShotNoOutputError) {
			holderHeldAtHook = led.Held(autonomouspkg.VerifyHolder(taskID))
			events = append(events, "hook")
			verifierRunCapDecision(m, prdID, taskID, capEnd)
		})
	if err != nil || content != `{"ok":true}` {
		t.Fatalf("content=%q err=%v, want the retry's output", content, err)
	}
	if want := "run:ollama-a,hook,run:ollama-b"; strings.Join(events, ",") != want {
		t.Fatalf("events = %v, want %s (release before the retry)", events, want)
	}
	if !holderHeldAtHook {
		t.Fatal("the lease should still be held when the hook starts")
	}
	if led.Held(autonomouspkg.VerifyHolder(taskID)) {
		t.Fatal("the verifier lease must be released by the hook")
	}
}

// The hook is not called for a verifier that ends any other way.
func TestVerifierRunCapLeaseDecisionHookSkippedWithoutCapEnd(t *testing.T) {
	calls := 0
	hook := func(*autonomouspkg.OneShotNoOutputError) { calls++ }
	for _, err := range []error{nil, errors.New("boom"), &autonomouspkg.OneShotNoOutputError{Msg: "watchdog"}} {
		_, _ = runVerifierSessionWithRunCapHook([]string{"a", "b"}, "a", "",
			func(string, string) (string, error) { return "", err }, retryModelRegistryDefault, hook)
	}
	if calls != 0 {
		t.Fatalf("hook called %d times for non-cap ends, want 0", calls)
	}
}

// A second cap end on the retry backend calls the hook again, but the lease is
// already gone, so nothing more is released.
func TestVerifierRunCapLeaseDecisionSecondCapEndReleasesNothing(t *testing.T) {
	m, led, prdID, taskID := runCapLeaseManager(t)
	hookCalls := 0
	_, err := runVerifierSessionWithRunCapHook([]string{"a", "b", "c"}, "a", "",
		func(string, string) (string, error) { return "", capEndWith(10*time.Second, 5) },
		retryModelRegistryDefault,
		func(capEnd *autonomouspkg.OneShotNoOutputError) {
			hookCalls++
			verifierRunCapDecision(m, prdID, taskID, capEnd)
		})
	if !autonomouspkg.IsVerifierRunCapEnd(err) {
		t.Fatalf("err = %v, want the cap end", err)
	}
	if hookCalls != 2 {
		t.Fatalf("hook calls = %d, want 2 (first end and the retry's end)", hookCalls)
	}
	if led.Held(autonomouspkg.VerifyHolder(taskID)) {
		t.Fatal("lease still held")
	}
	prd, _ := m.Store().GetPRD(prdID)
	n := 0
	for _, d := range prd.Decisions {
		if d.Kind == "verifier_run_cap" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("decisions = %d, want one per cap end (2)", n)
	}
}

func TestVerifierRunCapLeaseDecisionNoteHasElapsedSecondsAndCap(t *testing.T) {
	m, _, prdID, taskID := runCapLeaseManager(t)
	verifierRunCapDecision(m, prdID, taskID, capEndWith(2711*time.Second, 2700).(*autonomouspkg.OneShotNoOutputError))
	prd, _ := m.Store().GetPRD(prdID)
	var note string
	for _, d := range prd.Decisions {
		if d.Kind == "verifier_run_cap" {
			note = d.Note
		}
	}
	for _, want := range []string{"task=" + taskID, "elapsed_seconds=2711", "cap_seconds=2700"} {
		if !strings.Contains(note, want) {
			t.Fatalf("note %q missing %q", note, want)
		}
	}
	verifierRunCapDecision(nil, prdID, taskID, capEndWith(time.Second, 1).(*autonomouspkg.OneShotNoOutputError)) // nil manager is a no-op
}

// The wait loop puts the elapsed time and the cap on the error it returns, and
// the infra result built from it carries the verifier_max_run end reason.
func TestVerifierRunCapLeaseDecisionEndReasonIsVerifierMaxRun(t *testing.T) {
	useRunCapFastPoll(t)
	useRunCapFakeClock(t, 10*time.Second)
	projectDir := t.TempDir()
	outputFile := filepath.Join(projectDir, "verify-out.json")
	startRunCapDaemon(t, outputFile, 0)
	cfg := runCapConfig(t, 60)

	_, _, err := runOneShotJSONSessionWithReader(cfg, "prd-1", projectDir, "claude-code", "m", "high",
		"autonomous:verify:task-lease", "verify", outputFile, "verify-lease.json", 30*time.Second, runCapFileReader, "", launchTags{})
	var capEnd *autonomouspkg.OneShotNoOutputError
	if !errors.As(err, &capEnd) || !capEnd.RunCap {
		t.Fatalf("err = %v, want a run-cap end", err)
	}
	if capEnd.RunCapSeconds != 60 || capEnd.RunCapElapsed < 60*time.Second {
		t.Fatalf("cap end carries elapsed=%v cap=%ds, want >=60s and 60", capEnd.RunCapElapsed, capEnd.RunCapSeconds)
	}
	res := verifierSessionInfraResult(err)
	if res.EndReason != "verifier_max_run" || res.Category != "infra" || res.OK {
		t.Fatalf("result = %+v, want an infra result ended by verifier_max_run", res)
	}
	if other := verifierSessionInfraResult(errors.New("boom")); other.EndReason != "" {
		t.Fatalf("a plain session error must carry no end reason, got %q", other.EndReason)
	}
}
