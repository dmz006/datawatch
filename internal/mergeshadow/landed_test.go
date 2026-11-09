package mergeshadow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// newLandedRepo creates a throwaway repository with a commit on the default
// branch (main) holding the named files with the given contents, and
// returns the repo path.
func newLandedRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	must := func(args ...string) {
		t.Helper()
		if _, err := runGit(context.Background(), dir, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	must("init")
	must("symbolic-ref", "HEAD", "refs/heads/main")
	for p, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	must("add", "-A")
	must("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "initial")
	return dir
}

var landedPipeline = config.MergePipelineConfig{
	VersionFiles: []string{"cmd/app/main.go", "version.go"},
}

func TestReadLanded_PresentValues(t *testing.T) {
	repo := newLandedRepo(t, map[string]string{
		"CHANGELOG.md":         "# Changelog\n\n## [9.6.0] - 2026-11-01\n\n- **Merge plans** read back the release.\n\n## [9.5.0] - 2026-10-01\n\n- Old release.\n",
		"cmd/app/main.go":      "package main\n\nvar Version = \"9.6.0\"\n",
		"version.go":           "package app\n\nvar Version = \"9.6.0\"\n",
		"README.md":            "# datawatch\n\n## Current release\n\n**Current release: 9.6.0 (2026-11-01).** Change from Automaton da660001: 3 files in 2 packages.\n\n**Current release: 9.5.0 (2026-10-01).** Older line.\n",
		"docs/plans/README.md": "# Registry\n\n| # | Description | Status | BL |\n|---|---|---|---|\n| 188 | Unrelated row | done | - |\n| 189 | Merge read-back (PRD `da660001`) | done, 9.6.0 | - |\n",
	})
	out, err := ReadLanded(context.Background(), repo, "main", LandedQuery{
		Project:     "app",
		AutomatonID: "da660001",
		Version:     "9.6.0",
		Pipeline:    landedPipeline,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.ChangelogHeading.Found || out.ChangelogHeading.Value != "## [9.6.0] - 2026-11-01" {
		t.Errorf("changelog heading: %+v", out.ChangelogHeading)
	}
	for _, vf := range []string{"cmd/app/main.go", "version.go"} {
		if v := out.Versions[vf]; !v.Found || v.Value != "9.6.0" {
			t.Errorf("version file %s: %+v", vf, v)
		}
	}
	wantReadme := "**Current release: 9.6.0 (2026-11-01).** Change from Automaton da660001: 3 files in 2 packages."
	if !out.ReadmeLine.Found || out.ReadmeLine.Value != wantReadme {
		t.Errorf("readme line: %+v", out.ReadmeLine)
	}
	wantRow := "| 189 | Merge read-back (PRD `da660001`) | done, 9.6.0 | - |"
	if !out.RegistryRow.Found || out.RegistryRow.Value != wantRow {
		t.Errorf("registry row: %+v", out.RegistryRow)
	}
}

func TestReadLanded_AbsentValues(t *testing.T) {
	// None of the four values is present: the changelog has no "## "
	// heading, the README has no release line, the registry names a
	// different Automaton, and neither version file is on the ref.
	repo := newLandedRepo(t, map[string]string{
		"CHANGELOG.md":         "# Changelog\n\nStill unreleased.\n",
		"README.md":            "# datawatch\n",
		"docs/plans/README.md": "# Registry\n\n| # | Description | Status | BL |\n|---|---|---|---|\n| 1 | Other Automaton abcd0001 | done | - |\n",
	})
	out, err := ReadLanded(context.Background(), repo, "main", LandedQuery{
		Project:     "app",
		AutomatonID: "da660001",
		Version:     "9.6.0",
		Pipeline:    landedPipeline,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.ChangelogHeading.Found || out.ChangelogHeading.Value != "" {
		t.Errorf("changelog heading should be absent: %+v", out.ChangelogHeading)
	}
	if len(out.Versions) != 2 {
		t.Fatalf("versions map should hold one entry per configured file: %+v", out.Versions)
	}
	for vf, v := range out.Versions {
		if v.Found || v.Value != "" {
			t.Errorf("version file %s should be absent: %+v", vf, v)
		}
	}
	if out.ReadmeLine.Found || out.ReadmeLine.Value != "" {
		t.Errorf("readme line should be absent: %+v", out.ReadmeLine)
	}
	if out.RegistryRow.Found || out.RegistryRow.Value != "" {
		t.Errorf("registry row should be absent: %+v", out.RegistryRow)
	}
}

func TestReadLanded_IgnoresWorkingTree(t *testing.T) {
	repo := newLandedRepo(t, map[string]string{
		"CHANGELOG.md":    "# Changelog\n\n## [9.6.0] - 2026-11-01\n\n- Shipped.\n",
		"cmd/app/main.go": "var Version = \"9.6.0\"\n",
		"version.go":      "var Version = \"9.6.0\"\n",
		"README.md":       "# datawatch\n\n**Current release: 9.6.0 (2026-11-01).** Change from Automaton da660001: 1 file in 1 package.\n",
	})
	// Dirty the working tree after the commit: none of it may be read.
	if err := os.WriteFile(filepath.Join(repo, "CHANGELOG.md"), []byte("## [0.0.1] - 1970-01-01\n\nUncommitted noise.\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "version.go"), []byte("var Version = \"0.0.1\"\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	out, err := ReadLanded(context.Background(), repo, "main", LandedQuery{
		Project:     "app",
		AutomatonID: "da660001",
		Version:     "9.6.0",
		Pipeline:    landedPipeline,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.ChangelogHeading.Value != "## [9.6.0] - 2026-11-01" {
		t.Errorf("heading must come from the ref, got %+v", out.ChangelogHeading)
	}
	if v := out.Versions["version.go"]; v.Value != "9.6.0" {
		t.Errorf("version must come from the ref, got %+v", v)
	}
	if !strings.Contains(out.ReadmeLine.Value, "9.6.0") {
		t.Errorf("readme line must come from the ref, got %+v", out.ReadmeLine)
	}
}

func TestReadLanded_BadArguments(t *testing.T) {
	repo := newLandedRepo(t, map[string]string{"CHANGELOG.md": "## [1.0.0] - 2026-01-01\n"})
	q := LandedQuery{AutomatonID: "da660001"}
	if _, err := ReadLanded(context.Background(), "", "main", q); err == nil {
		t.Error("empty repo path must fail")
	}
	if _, err := ReadLanded(context.Background(), repo, "", q); err == nil {
		t.Error("empty ref must fail")
	}
	if _, err := ReadLanded(context.Background(), repo, "no-such-branch", q); err == nil {
		t.Error("an unresolvable ref must fail")
	}
}
