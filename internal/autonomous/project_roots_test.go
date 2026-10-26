package autonomous

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/gitguard"
	"github.com/dmz006/datawatch/internal/pipeline"
)

// rootsAPI returns an API whose manager allows only the given roots.
func rootsAPI(t *testing.T, roots ...string) (*API, *Manager) {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m.SetProjectRoots(func() []string { return roots })
	return NewAPI(m), m
}

func refusedByRoots(err error) bool {
	return errors.Is(err, gitguard.ErrOutsideAllowedRoots) || errors.Is(err, gitguard.ErrPathEscapes)
}

func TestAPICreatePRDEnforcesProjectRoots(t *testing.T) {
	base := t.TempDir()
	extra := t.TempDir()
	outside := t.TempDir()
	api, m := rootsAPI(t, base, extra)

	// Valid in-base path, and a path inside the configured extra root.
	for _, dir := range []string{filepath.Join(base, "proj"), filepath.Join(extra, "other"), base} {
		if _, err := api.CreatePRD("spec", dir, "", "", ""); err != nil {
			t.Errorf("CreatePRD(%q) refused: %v", dir, err)
		}
	}
	// An empty project_dir is not a request-supplied directory.
	if _, err := api.CreatePRD("spec", "", "", "", ""); err != nil {
		t.Errorf("CreatePRD with no project_dir refused: %v", err)
	}

	link := filepath.Join(base, "escape")
	linkOK := os.Symlink(outside, link) == nil
	dangling := filepath.Join(base, "dangling")
	danglingOK := os.Symlink(filepath.Join(outside, "missing"), dangling) == nil

	cases := map[string]string{
		"outside every root": filepath.Join(outside, "proj"),
		"dot-dot traversal":  filepath.Join(base, "..", filepath.Base(outside)),
		"deep traversal":     base + "/a/../../" + filepath.Base(outside),
		"filesystem root":    "/",
	}
	if linkOK {
		cases["symlink escape"] = link
		cases["symlink escape, deeper"] = filepath.Join(link, "deeper")
	}
	if danglingOK {
		cases["dangling link escape"] = dangling
	}
	before := len(m.Store().ListPRDs())
	for name, dir := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := api.CreatePRD("spec", dir, "", "", "")
			if err == nil || !strings.Contains(err.Error(), "project_dir refused") {
				t.Fatalf("CreatePRD(%q): want a project_dir refusal, got %v", dir, err)
			}
			if name != "filesystem root" && !refusedByRoots(err) {
				t.Fatalf("CreatePRD(%q): want a project-root error, got %v", dir, err)
			}
		})
	}
	if got := len(m.Store().ListPRDs()); got != before {
		t.Fatalf("a refused create stored a PRD (%d -> %d)", before, got)
	}
}

func TestAPICreatePRDNoRootsRefusesEveryDirectory(t *testing.T) {
	api, _ := rootsAPI(t)
	if _, err := api.CreatePRD("spec", t.TempDir(), "", "", ""); !refusedByRoots(err) {
		t.Fatalf("with no roots configured every directory must be refused, got %v", err)
	}
}

func TestAPISetTaskProjectDirEnforcesProjectRoots(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	m, prd := newTaskDirManager(t)
	m.SetProjectRoots(func() []string { return []string{base} })
	api := NewAPI(m)

	if _, err := api.SetTaskProjectDir(prd.ID, "t1", filepath.Join(base, "sub"), "test"); err != nil {
		t.Fatalf("in-base task dir refused: %v", err)
	}
	if _, err := api.SetTaskProjectDir(prd.ID, "t1", filepath.Join(outside, "sub"), "test"); !refusedByRoots(err) {
		t.Fatalf("outside task dir: want a project-root refusal, got %v", err)
	}
	if _, err := api.SetTaskProjectDir(prd.ID, "t1", base+"/../"+filepath.Base(outside), "test"); !refusedByRoots(err) {
		t.Fatalf("traversal task dir: want a project-root refusal, got %v", err)
	}
	link := filepath.Join(base, "escape")
	if err := os.Symlink(outside, link); err == nil {
		if _, err := api.SetTaskProjectDir(prd.ID, "t1", link, "test"); !refusedByRoots(err) {
			t.Fatalf("symlink task dir: want a project-root refusal, got %v", err)
		}
	}
	stored, _ := m.Store().GetPRD(prd.ID)
	if got := stored.Story[0].Tasks[0].ProjectDir; got != filepath.Join(base, "sub") {
		t.Fatalf("a refused override changed the stored dir to %q", got)
	}
	// Clearing the override is always allowed.
	if _, err := api.SetTaskProjectDir(prd.ID, "t1", "", "test"); err != nil {
		t.Fatalf("clearing the override refused: %v", err)
	}
}

func TestAPIInstantiateFromTemplateStoreEnforcesProjectRoots(t *testing.T) {
	api, _ := rootsAPI(t, t.TempDir())
	_, err := api.InstantiateFromTemplateStore("no-such-template", nil, t.TempDir(), "", "")
	if !refusedByRoots(err) {
		t.Fatalf("want the project-root refusal before the template is read, got %v", err)
	}
}

func TestQualityGateRefusesDirectoryOutsideRoots(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	m.SetProjectRoots(func() []string { return []string{base} })
	cfg := pipeline.QualityGateConfig{Enabled: true, TestCommand: "true"}
	// A run in the root starts (it is judged on its output, not refused).
	if _, reason := m.checkGateBaseline(base, cfg); strings.Contains(reason, "could not be started") {
		t.Fatalf("in-root gate run did not start: %q", reason)
	}
	status, reason := m.checkGateBaseline(outside, cfg)
	if status != "invalid" || !strings.Contains(reason, "could not be started") {
		t.Fatalf("out-of-root gate run: status %q reason %q, want invalid", status, reason)
	}
}
