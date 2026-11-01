package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestGitPushDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if got := cfg.Git.PushOnComplete; got != GitPushBranch {
		t.Errorf("default git.push_on_complete = %q, want %q", got, GitPushBranch)
	}
	if got := cfg.Git.PushRemote; got != "origin" {
		t.Errorf("default git.push_remote = %q, want %q", got, "origin")
	}
	if got := cfg.Git.EffectivePushOnComplete(); got != "branch" {
		t.Errorf("EffectivePushOnComplete on defaults = %q, want %q", got, "branch")
	}
	if got := cfg.Git.EffectivePushRemote(); got != "origin" {
		t.Errorf("EffectivePushRemote on defaults = %q, want %q", got, "origin")
	}
}

func TestGitPushYAMLRoundTrip(t *testing.T) {
	in := DefaultConfig()
	in.Git = GitConfig{PushOnComplete: GitPushNever, PushRemote: "upstream"}
	raw, err := yaml.Marshal(in)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	var out Config
	if err := yaml.Unmarshal(raw, &out); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if got := out.Git.PushOnComplete; got != "never" {
		t.Errorf("round-trip push_on_complete = %q, want %q", got, "never")
	}
	if got := out.Git.PushRemote; got != "upstream" {
		t.Errorf("round-trip push_remote = %q, want %q", got, "upstream")
	}
}

func TestGitPushEffectiveNormalization(t *testing.T) {
	onComplete := map[string]string{
		"":               GitPushBranch, // empty falls back to branch
		"never":          GitPushNever,  // known policies pass through
		"branch":         GitPushBranch,
		"default_branch": GitPushDefaultBranch,
		"force-push":     GitPushBranch, // unknown falls back to branch
		"BRANCH":         GitPushBranch, // unknown (case-sensitive) falls back
	}
	for in, want := range onComplete {
		if got := (GitConfig{PushOnComplete: in}).EffectivePushOnComplete(); got != want {
			t.Errorf("EffectivePushOnComplete(%q) = %q, want %q", in, got, want)
		}
	}

	if got := (GitConfig{PushRemote: ""}).EffectivePushRemote(); got != "origin" {
		t.Errorf("EffectivePushRemote(\"\") = %q, want %q", got, "origin")
	}
	if got := (GitConfig{PushRemote: "upstream"}).EffectivePushRemote(); got != "upstream" {
		t.Errorf("EffectivePushRemote(\"upstream\") = %q, want %q", got, "upstream")
	}
}
