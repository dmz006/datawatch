package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestHeldQuestionConfigAbsentModeReadsShadow(t *testing.T) {
	var q AutonomousQuestionsConfig
	if got := q.EffectiveHeldMode(); got != HeldModeShadow {
		t.Fatalf("absent mode = %q; want shadow", got)
	}
	if got := DefaultConfig().Autonomous.Questions.EffectiveHeldMode(); got != HeldModeShadow {
		t.Fatalf("default config mode = %q; want shadow", got)
	}
}

func TestHeldQuestionConfigExplicitModesReadAsSet(t *testing.T) {
	for _, mode := range []string{HeldModeOff, HeldModeShadow, HeldModeOn} {
		q := AutonomousQuestionsConfig{HeldMode: mode}
		if got := q.EffectiveHeldMode(); got != mode {
			t.Errorf("explicit mode %q = %q", mode, got)
		}
		if err := ValidateHeldMode(mode); err != nil {
			t.Errorf("ValidateHeldMode(%q) = %v; want nil", mode, err)
		}
	}
}

func TestHeldQuestionConfigUnknownModeRefused(t *testing.T) {
	if err := ValidateHeldMode("enforce"); err == nil {
		t.Fatal(`ValidateHeldMode("enforce") accepted`)
	} else if !strings.Contains(err.Error(), "held_mode") || !strings.Contains(err.Error(), "enforce") {
		t.Errorf("error = %v; want it to name the key and the bad value", err)
	}

	p := filepath.Join(t.TempDir(), "config.yaml")
	body := "autonomous:\n  questions:\n    held_mode: enforce\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal(`Load accepted held_mode "enforce"`)
	} else if !strings.Contains(err.Error(), "held_mode") || !strings.Contains(err.Error(), "enforce") {
		t.Errorf("error = %v; want it to name the key and the bad value", err)
	}

	// Every valid mode (and empty, which reads shadow) loads.
	for _, mode := range []string{"", "off", "shadow", "on"} {
		val := "shadow"
		if mode != "" {
			val = mode
		}
		if err := os.WriteFile(p, []byte("autonomous:\n  questions:\n    held_mode: "+mode+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(p)
		if err != nil {
			t.Fatalf("Load with mode %q: %v", mode, err)
		}
		if got := cfg.Autonomous.Questions.EffectiveHeldMode(); got != val {
			t.Errorf("mode %q read %q; want %q", mode, got, val)
		}
	}
}

func TestHeldQuestionConfigAbsentStatesCoverAllFive(t *testing.T) {
	var q AutonomousQuestionsConfig
	states := q.EffectiveHeldStates()
	if len(states) != len(AllHeldStates) {
		t.Fatalf("absent states = %v; want all five", states)
	}
	for _, want := range []string{
		HeldStateBlocked,
		HeldStateReviewHeld,
		HeldStateRevisionRequested,
		HeldStateAwaitingDecision,
		HeldStatePendingInsert,
	} {
		if !q.HeldStateCovered(want) {
			t.Errorf("absent list must cover %q", want)
		}
	}
	if q.HeldStateCovered("needs_review") {
		t.Error("absent list must not cover an unknown state")
	}
}

func TestHeldQuestionConfigExplicitEmptyStatesCoverNone(t *testing.T) {
	q := AutonomousQuestionsConfig{HeldStates: []string{}}
	if states := q.EffectiveHeldStates(); len(states) != 0 {
		t.Fatalf("explicit empty list = %v; want none", states)
	}
	for _, want := range AllHeldStates {
		if q.HeldStateCovered(want) {
			t.Errorf("explicit empty list must not cover %q", want)
		}
	}
	if err := ValidateHeldStates(q.HeldStates); err != nil {
		t.Errorf("explicit empty list = %v; want nil", err)
	}
}

func TestHeldQuestionConfigExplicitStatesCoverListedOnly(t *testing.T) {
	q := AutonomousQuestionsConfig{HeldStates: []string{HeldStateBlocked, HeldStateReviewHeld}}
	if got := q.EffectiveHeldStates(); len(got) != 2 || got[0] != HeldStateBlocked || got[1] != HeldStateReviewHeld {
		t.Fatalf("explicit list = %v; want the two ids", got)
	}
	if !q.HeldStateCovered(HeldStateBlocked) || !q.HeldStateCovered(HeldStateReviewHeld) {
		t.Error("listed states must be covered")
	}
	if q.HeldStateCovered(HeldStatePendingInsert) {
		t.Error("unlisted state must not be covered")
	}
}

func TestHeldQuestionConfigUnknownStateIDRefused(t *testing.T) {
	err := ValidateHeldStates([]string{HeldStateBlocked, "bogus"})
	if err == nil {
		t.Fatal(`ValidateHeldStates accepted "bogus"`)
	}
	msg := err.Error()
	if !strings.Contains(msg, `bogus`) {
		t.Errorf("error = %v; want it to name the bad id", err)
	}
	for _, id := range AllHeldStates {
		if !strings.Contains(msg, id) {
			t.Errorf("error = %v; want it to name the valid id %q", err, id)
		}
	}

	p := filepath.Join(t.TempDir(), "config.yaml")
	body := "autonomous:\n  questions:\n    held_states: [blocked, bogus]\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal(`Load accepted held_states with "bogus"`)
	} else if !strings.Contains(err.Error(), "held_states") {
		t.Errorf("error = %v; want it to name the key", err)
	}

	// A fully valid explicit list loads and reads exactly those ids.
	valid := "autonomous:\n  questions:\n    held_states: [review_held, pending_insert]\n"
	if err := os.WriteFile(p, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	q := cfg.Autonomous.Questions
	if got := q.EffectiveHeldStates(); len(got) != 2 || got[0] != HeldStateReviewHeld || got[1] != HeldStatePendingInsert {
		t.Fatalf("loaded states = %v; want the two ids", got)
	}
}

func TestHeldQuestionConfigYAMLRoundTripKeepsExplicitEmpty(t *testing.T) {
	in := AutonomousQuestionsConfig{HeldMode: HeldModeOn, HeldStates: []string{}}
	data, err := yaml.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "held_mode: ") {
		t.Fatalf("yaml must carry held_mode:\n%s", data)
	}
	// No omitempty on held_states: the explicit empty list must be
	// emitted (as an empty list or null) rather than elided, otherwise
	// the "none" would read "all five" after a round trip.
	if !strings.Contains(s, "held_states:") {
		t.Fatalf("yaml must carry held_states (explicit empty list):\n%s", data)
	}
	var out AutonomousQuestionsConfig
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.HeldStates == nil {
		t.Fatal("explicit empty list read nil after round trip; want none, not all five")
	}
	if got := out.HeldStates; len(got) != 0 {
		t.Errorf("explicit empty list = %v; want none", got)
	}
	if out.HeldStateCovered(AllHeldStates[0]) {
		t.Error("explicit empty list must cover no state")
	}
	if out.HeldMode != HeldModeOn || out.EffectiveHeldMode() != HeldModeOn {
		t.Errorf("held_mode = %q / effective %q; want on", out.HeldMode, out.EffectiveHeldMode())
	}

	// Key absent from the config file reads nil and covers all five.
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte("autonomous:\n  questions:\n    held_mode: on\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	q := cfg.Autonomous.Questions
	if q.HeldStates != nil {
		t.Errorf("absent key = %v; want nil (all five)", q.HeldStates)
	}
	if got := q.EffectiveHeldStates(); len(got) != len(AllHeldStates) {
		t.Errorf("absent key reads %v; want all five", got)
	}

	// An explicit empty list in the config file reads none and stays
	// none across a full marshal/unmarshal cycle: once cleared by the
	// operator, a daemon restart must not silently restore all five.
	if err := os.WriteFile(p, []byte("autonomous:\n  questions:\n    held_states: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(p)
	if err != nil {
		t.Fatal(err)
	}
	q = cfg.Autonomous.Questions
	if q.HeldStates == nil {
		t.Fatal("explicit empty list read nil; want none, not all five")
	}
	if got := q.EffectiveHeldStates(); len(got) != 0 {
		t.Errorf("explicit empty list reads %v; want none", got)
	}
	for _, id := range AllHeldStates {
		if q.HeldStateCovered(id) {
			t.Errorf("explicit empty list must not cover %q", id)
		}
	}
}
