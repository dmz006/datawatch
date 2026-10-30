// Tests for the repo CI watch config (RepoCIWatchConfig): every default is
// applied in exactly one place — the Effective* accessors — and a bad value
// is refused on every write path (the config load validator plus the REST
// PUT validator in internal/server).

package autonomous

import (
	"slices"
	"strings"
	"testing"
)

// containsFoldString reports whether sub appears in s, ignoring case (a
// small helper so the table tests above can name the offending field in the
// error text).
func containsFoldString(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

func TestRepoCIWatchConfigDefaults(t *testing.T) {
	var unset RepoCIWatchConfig
	if got := unset.EffectiveMode(); got != RepoCIWatchModeOff {
		t.Errorf("effective mode = %q, want %q", got, RepoCIWatchModeOff)
	}
	if want, got := []string{}, unset.EffectiveRepos(); len(got) != len(want) {
		t.Errorf("effective repos = %v, want empty (unset watches nothing)", got)
	}
	if got := unset.EffectivePollSeconds(); got != RepoCIWatchDefaultPollSeconds {
		t.Errorf("effective poll_seconds = %d, want %d", got, RepoCIWatchDefaultPollSeconds)
	}
	if got := unset.EffectiveAlertMinutes(); got != DefaultAutomationTriageAlertMinutes {
		t.Errorf("effective alert_minutes = %d, want %d (the triage alert default)", got, DefaultAutomationTriageAlertMinutes)
	}
	if got := unset.EffectiveTagCount(); got != RepoCIWatchDefaultTagCount {
		t.Errorf("effective tag_count = %d, want %d", got, RepoCIWatchDefaultTagCount)
	}
	if got := unset.EffectiveHungAfterMinutes(); got != RepoCIWatchDefaultHungAfterMinutes {
		t.Errorf("effective hung_after_minutes = %d, want %d", got, RepoCIWatchDefaultHungAfterMinutes)
	}
	wantWorkflows := []string{
		"CodeQL",
		"dependency-review",
		"owasp-zap",
		"secret-scan",
		"security-scan",
	}
	if !slices.Equal(unset.EffectiveSecurityWorkflows(), wantWorkflows) {
		t.Errorf("effective security_workflows = %v, want %v", unset.EffectiveSecurityWorkflows(), wantWorkflows)
	}
	if got := unset.EffectiveDeleteSuperseded(); got != RepoCIWatchDeleteSupersededReport {
		t.Errorf("effective delete_superseded = %q, want %q", got, RepoCIWatchDeleteSupersededReport)
	}
}

func TestRepoCIWatchExplicitZeroIsMeaningful(t *testing.T) {
	c := RepoCIWatchConfig{}
	zero := 0
	c.AlertMinutes = &zero
	c.TagCount = &zero

	// An explicit 0 must not read its default: 0 alerts (off), 0 tags (none).
	if got := c.EffectiveAlertMinutes(); got != 0 {
		t.Errorf("explicit alert_minutes 0 = %d, want 0 (alert off)", got)
	}
	if got := c.EffectiveTagCount(); got != 0 {
		t.Errorf("explicit tag_count 0 = %d, want 0 (none)", got)
	}

	// A non-zero explicit value stores as such.
	five := 5
	c.TagCount = &five
	if got := c.EffectiveTagCount(); got != 5 {
		t.Errorf("explicit tag_count 5 = %d, want 5", got)
	}
}

func TestRepoCIWatchExplicitReposAndSecurityWorkflows(t *testing.T) {
	c := RepoCIWatchConfig{
		Repos:             []string{"/home/user/repo-a", "/home/user/repo-b"},
		SecurityWorkflows: []string{}, // an empty list is explicit: none
	}
	repos := c.EffectiveRepos()
	if len(repos) != 2 || repos[0] != "/home/user/repo-a" || repos[1] != "/home/user/repo-b" {
		t.Errorf("effective repos = %v, want the two configured paths", repos)
	}
	if got := c.EffectiveSecurityWorkflows(); len(got) != 0 {
		t.Errorf("explicit empty security_workflows = %v, want none", got)
	}
	// The unset (nil) case reads the default list instead.
	var unset RepoCIWatchConfig
	if got := unset.EffectiveSecurityWorkflows(); len(got) != len(DefaultRepoCIWatchSecurityWorkflows) {
		t.Errorf("unset security_workflows = %v, want the %d-entry default", got, len(DefaultRepoCIWatchSecurityWorkflows))
	}
}

func TestRepoCIWatchConfigRejectsBadValues(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*RepoCIWatchConfig)
		message string
	}{
		{
			name:    "negative alert minutes",
			mutate:  func(c *RepoCIWatchConfig) { n := -1; c.AlertMinutes = &n },
			message: "alert_minutes",
		},
		{
			name:    "negative tag count",
			mutate:  func(c *RepoCIWatchConfig) { n := -2; c.TagCount = &n },
			message: "tag_count",
		},
		{
			name:    "poll below minimum",
			mutate:  func(c *RepoCIWatchConfig) { c.PollSeconds = 10 },
			message: "poll_seconds",
		},
		{
			name:    "relative repository path",
			mutate:  func(c *RepoCIWatchConfig) { c.Repos = []string{"src/repo"} },
			message: "absolute path",
		},
		{
			name:    "bare slash repository path",
			mutate:  func(c *RepoCIWatchConfig) { c.Repos = []string{"/"} },
			message: "absolute path",
		},
		{
			name:    "empty workflow name",
			mutate:  func(c *RepoCIWatchConfig) { c.SecurityWorkflows = []string{""} },
			message: "non-empty",
		},
		{
			name:    "bad mode",
			mutate:  func(c *RepoCIWatchConfig) { c.Mode = "loud" },
			message: "mode",
		},
		{
			name:    "bad delete_superseded",
			mutate:  func(c *RepoCIWatchConfig) { c.DeleteSuperseded = "purge" },
			message: "delete_superseded",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := RepoCIWatchConfig{}
			tc.mutate(&c)
			err := ValidateRepoCIWatchConfig(c)
			if err == nil {
				t.Fatalf("ValidateRepoCIWatchConfig accepted %+v", c)
			}
			if !containsFoldString(err.Error(), tc.message) {
				t.Errorf("error %q does not mention %q", err, tc.message)
			}
		})
	}
}

func TestRepoCIWatchConfigToleratesUnsetAndZero(t *testing.T) {
	// Unset means "nothing is configured": every field reads its default,
	// so the whole struct validates clean. (hung_after_minutes 0 is the
	// unset value — it reads the default, so it must validate clean too.)
	c := RepoCIWatchConfig{}
	c.HungAfterMinutes = 0
	if err := ValidateRepoCIWatchConfig(c); err != nil {
		t.Errorf("unset struct should validate clean, got %v", err)
	}
	// No repositories is the normal "watch nothing" state: accepted.
	if err := ValidateRepoCIWatchConfig(RepoCIWatchConfig{Repos: []string{}}); err != nil {
		t.Errorf("empty repos list (nothing watched) should be accepted, got %v", err)
	}
	// A configured-but-valid struct validates clean.
	c = RepoCIWatchConfig{
		Mode:              RepoCIWatchModeShadow,
		Repos:             []string{"/home/user/repo-a"},
		PollSeconds:       RepoCIWatchDefaultPollSeconds,
		SecurityWorkflows: []string{"CodeQL"},
		DeleteSuperseded:  RepoCIWatchDeleteSupersededDelete,
		HungAfterMinutes:  RepoCIWatchDefaultHungAfterMinutes,
	}
	zero := 0
	c.TagCount = &zero
	if err := ValidateRepoCIWatchConfig(c); err != nil {
		t.Errorf("valid struct with tag_count 0 should be accepted, got %v", err)
	}
}

func TestValidateRepoCIWatchModeAndDeleteSuperseded(t *testing.T) {
	for _, ok := range []string{"off", "shadow", "on", " OFF ", "Shadow", "", "on "} {
		if err := ValidateRepoCIWatchMode(ok); err != nil {
			t.Errorf("ValidateRepoCIWatchMode(%q) = %v, want nil (case-insensitive, trimmed; empty is unset/off)", ok, err)
		}
	}
	for _, bad := range []string{"loud", "Shadows", "onoff"} {
		if err := ValidateRepoCIWatchMode(bad); err == nil {
			t.Errorf("ValidateRepoCIWatchMode(%q) = nil, want an error", bad)
		}
	}
	for _, ok := range []string{"report", "delete", " Report", "", "Delete", "delete "} {
		if err := ValidateRepoCIWatchDeleteSuperseded(ok); err != nil {
			t.Errorf("ValidateRepoCIWatchDeleteSuperseded(%q) = %v, want nil (case-insensitive, trimmed; empty is unset/report)", ok, err)
		}
	}
	for _, bad := range []string{"purge", "reporting"} {
		if err := ValidateRepoCIWatchDeleteSuperseded(bad); err == nil {
			t.Errorf("ValidateRepoCIWatchDeleteSuperseded(%q) = nil, want an error", bad)
		}
	}
}

func TestDefaultRepoCIWatchConfigCarriesEveryDefault(t *testing.T) {
	d := DefaultRepoCIWatchConfig()
	if d.Mode != RepoCIWatchModeOff {
		t.Errorf("default mode = %q, want %q", d.Mode, RepoCIWatchModeOff)
	}
	if d.PollSeconds != RepoCIWatchDefaultPollSeconds {
		t.Errorf("default poll_seconds = %d, want %d", d.PollSeconds, RepoCIWatchDefaultPollSeconds)
	}
	if d.AlertMinutes == nil || *d.AlertMinutes != DefaultAutomationTriageAlertMinutes {
		t.Errorf("default alert_minutes = %v, want %d", d.AlertMinutes, DefaultAutomationTriageAlertMinutes)
	}
	if d.TagCount == nil || *d.TagCount != RepoCIWatchDefaultTagCount {
		t.Errorf("default tag_count = %v, want %d", d.TagCount, RepoCIWatchDefaultTagCount)
	}
	if d.HungAfterMinutes != RepoCIWatchDefaultHungAfterMinutes {
		t.Errorf("default hung_after_minutes = %d, want %d", d.HungAfterMinutes, RepoCIWatchDefaultHungAfterMinutes)
	}
	if !slices.Equal(d.SecurityWorkflows, DefaultRepoCIWatchSecurityWorkflows) {
		t.Errorf("default security_workflows = %v, want %v", d.SecurityWorkflows, DefaultRepoCIWatchSecurityWorkflows)
	}
	if d.DeleteSuperseded != RepoCIWatchDeleteSupersededReport {
		t.Errorf("default delete_superseded = %q, want %q", d.DeleteSuperseded, RepoCIWatchDeleteSupersededReport)
	}
	// The returned slice must be a copy, not the package default's backing
	// array: appending to it must not corrupt DefaultRepoCIWatchSecurityWorkflows.
	orig := len(DefaultRepoCIWatchSecurityWorkflows)
	d.SecurityWorkflows = append(d.SecurityWorkflows, "mutated")
	if got := DefaultRepoCIWatchSecurityWorkflows; len(got) != orig || got[orig-1] != "security-scan" {
		t.Errorf("caller append corrupted the default list: %v", got)
	}
}
