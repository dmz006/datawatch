package supervisorloop

import (
	"os"
	"path/filepath"
	"testing"
)

// A job name that is not one of the three model jobs never reaches the file
// system: a traversal-shaped name loads nothing from outside the cases folder.
func TestLoadEvalCasesRejectsUnknownJobNames(t *testing.T) {
	root := t.TempDir()
	cases := filepath.Join(root, "cases")
	outside := filepath.Join(root, "secret")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "x.json"), []byte(`{"name":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, job := range []string{"../secret", "..", "a/b", "", "draft_spec/../../secret"} {
		if got, err := LoadEvalCases(job, cases); err == nil || len(got) != 0 {
			t.Fatalf("LoadEvalCases(%q) = %v, %v; want an unknown job error", job, got, err)
		}
	}
}

// The three real jobs still load their own folder.
func TestLoadEvalCasesKnownJobsStillLoad(t *testing.T) {
	cases := t.TempDir()
	for _, job := range []string{"draft_spec", "classify_ci_failure", "pick_backend"} {
		dir := filepath.Join(cases, job)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "one.json"), []byte(`{"name":"one"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := LoadEvalCases(job, cases)
		if err != nil || len(got) != 1 || got[0].Name != "one" {
			t.Fatalf("LoadEvalCases(%q) = %v, %v", job, got, err)
		}
	}
}

// A request's cases set must name an existing subfolder of the default
// folder; nothing else resolves.
func TestResolveCasesDir(t *testing.T) {
	prev, _ := os.Getwd()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, DefaultEvalCasesDir, "mine"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(prev) }()
	if got, err := ResolveCasesDir(""); err != nil || got != DefaultEvalCasesDir {
		t.Fatalf("empty = %q, %v; want the default", got, err)
	}
	if got, err := ResolveCasesDir("mine"); err != nil || got != filepath.Join(DefaultEvalCasesDir, "mine") {
		t.Fatalf("mine = %q, %v", got, err)
	}
	for _, bad := range []string{"../x", "..", "mine/../..", "/etc", "other", "mine/sub"} {
		if got, err := ResolveCasesDir(bad); err == nil {
			t.Fatalf("ResolveCasesDir(%q) = %q; want an error", bad, got)
		}
	}
}
