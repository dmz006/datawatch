package main

import (
	"slices"
	"testing"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// The boot bridge carries the repo CI watch block into the Manager, keeping
// the security workflow list's nil (default) and empty (none) apart, and
// copies the pointer knobs by value.
func TestRepoCIWatchFromYAMLBoot(t *testing.T) {
	alert, tags := 15, 0
	in := config.RepoCIWatchConfig{
		Mode:              "shadow",
		Repos:             []string{"/srv/repo"},
		PollSeconds:       120,
		AlertMinutes:      &alert,
		TagCount:          &tags,
		HungAfterMinutes:  45,
		SecurityWorkflows: []string{"CodeQL"},
		DeleteSuperseded:  "report",
	}
	out := repoCIWatchFromYAML(in)
	if out.Mode != "shadow" || out.PollSeconds != 120 || out.HungAfterMinutes != 45 || out.DeleteSuperseded != "report" {
		t.Fatalf("scalar fields not carried: %+v", out)
	}
	if !slices.Equal(out.Repos, []string{"/srv/repo"}) {
		t.Fatalf("repos = %v", out.Repos)
	}
	if out.AlertMinutes == nil || *out.AlertMinutes != 15 || out.TagCount == nil || *out.TagCount != 0 {
		t.Fatalf("pointer knobs not carried: alert=%v tags=%v", out.AlertMinutes, out.TagCount)
	}
	if out.AlertMinutes == in.AlertMinutes || out.TagCount == in.TagCount {
		t.Fatal("pointer knobs must be copied, not aliased")
	}
	if !slices.Equal(out.SecurityWorkflows, []string{"CodeQL"}) {
		t.Fatalf("security workflows = %v", out.SecurityWorkflows)
	}
	in.SecurityWorkflows[0] = "changed"
	if out.SecurityWorkflows[0] != "CodeQL" {
		t.Fatal("security workflows must be copied, not aliased")
	}

	// Unset: nil stays nil, so the default applies.
	unset := repoCIWatchFromYAML(config.RepoCIWatchConfig{})
	if unset.SecurityWorkflows != nil || unset.Repos != nil || unset.AlertMinutes != nil || unset.TagCount != nil {
		t.Fatalf("unset block must stay nil: %+v", unset)
	}
	if got := unset.EffectiveSecurityWorkflows(); !slices.Equal(got, autonomouspkg.DefaultRepoCIWatchSecurityWorkflows) {
		t.Fatalf("unset security workflows read %v, want the default", got)
	}

	// Explicit none: a non-nil empty list stays non-nil empty.
	none := repoCIWatchFromYAML(config.RepoCIWatchConfig{SecurityWorkflows: []string{}})
	if none.SecurityWorkflows == nil || len(none.SecurityWorkflows) != 0 {
		t.Fatalf("explicit empty list must stay non-nil empty, got %#v", none.SecurityWorkflows)
	}
}
