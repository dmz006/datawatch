package main

import (
	"strings"
	"testing"
)

// The concurrency keys are documented in the config-set help text next to
// max_concurrent_tasks; this guards the example and the key name from
// drifting out of the CLI surface.
func TestAutonomousConfigSetHelp_MentionsMaxEnginesPerNode(t *testing.T) {
	help := newAutonomousConfigSetCmd().Long
	if !strings.Contains(help, "max_engines_per_node") {
		t.Fatalf("config-set help lacks max_engines_per_node:\n%s", help)
	}
	if !strings.Contains(help, `config-set '{"max_engines_per_node":2}'`) {
		t.Errorf("config-set help lacks max_engines_per_node example:\n%s", help)
	}
}

func TestAutonomousConfigGetHelp_MentionsMaxEnginesPerNode(t *testing.T) {
	help := newAutonomousConfigGetCmd().Long
	if !strings.Contains(help, "max_concurrent_tasks") || !strings.Contains(help, "max_engines_per_node") {
		t.Fatalf("config-get help lacks concurrency keys:\n%s", help)
	}
}

// B165 — resume_max_age_seconds is a user-facing key with two meaningful
// sentinels (0 = never resume, -1 = no age limit); it must be documented
// in both surfaces' help next to the concurrency keys.
func TestAutonomousConfigSetHelp_MentionsResumeMaxAge(t *testing.T) {
	help := newAutonomousConfigSetCmd().Long
	if !strings.Contains(help, "resume_max_age_seconds") {
		t.Fatalf("config-set help lacks resume_max_age_seconds:\n%s", help)
	}
	if !strings.Contains(help, `config-set '{"resume_max_age_seconds":1800}'`) {
		t.Errorf("config-set help lacks resume_max_age_seconds example:\n%s", help)
	}
	if !strings.Contains(help, "0 = never resume") || !strings.Contains(help, "-1 = no age limit") {
		t.Errorf("config-set help must document both sentinels:\n%s", help)
	}
}

func TestAutonomousConfigGetHelp_MentionsResumeMaxAge(t *testing.T) {
	help := newAutonomousConfigGetCmd().Long
	if !strings.Contains(help, "resume_max_age_seconds") {
		t.Fatalf("config-get help lacks resume_max_age_seconds:\n%s", help)
	}
}
