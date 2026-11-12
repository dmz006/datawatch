package autonomous

import (
	"os"
	"path/filepath"
	"testing"
)

func tmuxPlan(spec string) []Story {
	return []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: spec}}}}
}

func TestDecomposeRejectsUnscopedTmux(t *testing.T) {
	cases := []struct {
		name    string
		spec    string
		wantErr bool
	}{
		{"tmpdir only", "run `TMUX_TMPDIR=x tmux kill-server` after", true},
		{"bare kill-session -a", "tmux kill-session -a", true},
		{"throwaway socket", "run `tmux -S \"$(mktemp -d)/s\" kill-server`", false},
		{"scrubbed env", "run `env -u TMUX tmux kill-server`", false},
		{"scrubbed env but tmpdir set", "TMUX_TMPDIR=x env -u TMUX tmux kill-server", true},
		{"no tmux text", "add a unit test for the parser", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateNoUnscopedTmux("", tmuxPlan(c.spec))
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
		})
	}
}

func TestDecomposeRejectsUnscopedTmux_VerificationScript(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "v.sh"), []byte("#!/bin/sh\nTMUX_TMPDIR=x tmux kill-server\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	plan := []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "x", VerificationScript: "v.sh"}}}}
	if err := validateNoUnscopedTmux(dir, plan); err == nil {
		t.Fatal("want error for unscoped kill in verification script")
	}
}

func TestDecomposeRejectsUnscopedTmux_ScopeCategory(t *testing.T) {
	resp := `{"title":"T","stories":[{"title":"S","tasks":[{"title":"Task 1","spec":"TMUX_TMPDIR=x tmux kill-server"}]}]}`
	m, err := NewManager(t.TempDir(), DefaultConfig(), func(DecomposeRequest) (string, error) { return resp, nil })
	if err != nil {
		t.Fatal(err)
	}
	prd, _ := m.CreatePRD("spec", "/p", "", "", "")
	if _, err := m.Decompose(prd.ID); err == nil {
		t.Fatal("want decompose error")
	}
	final, _ := m.Store().GetPRD(prd.ID)
	if final.LastDecomposeFailure == nil || final.LastDecomposeFailure.Category != "scope" {
		t.Fatalf("LastDecomposeFailure = %+v, want category scope", final.LastDecomposeFailure)
	}
}
