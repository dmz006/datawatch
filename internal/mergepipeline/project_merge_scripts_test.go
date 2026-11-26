package mergepipeline

// This repository's own merge checks (scripts/merge) and the pipeline record
// meant to run them. Nothing here changes a live record.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectSmokeModeChoice(t *testing.T) {
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "merge", "smoke-mode.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		version, size string
		files         []string
		want          string
	}{
		"a minor, even docs only":  {"1.3.0", "minor", []string{"docs/a.md"}, "full"},
		"a major":                  {"2.0.0", "major", []string{"x.go"}, "full"},
		"X.Y.0 with no size":       {"1.3.0", "", []string{"x.go"}, "full"},
		"a bug fix":                {"1.2.4", "patch", []string{"internal/x.go"}, "targeted"},
		"a script change":          {"1.2.4", "patch", []string{"scripts/y.sh", "docs/a.md"}, "targeted"},
		"docs only":                {"1.2.4", "patch", []string{"docs/a.md", "README.md", "internal/server/web/docs/b.md"}, "skip"},
		"no release, docs only":    {"1.2.3", "none", []string{"docs/howto/c.md"}, "skip"},
		"no release, code changed": {"1.2.3", "none", []string{"x.go"}, "targeted"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			runGit(t, dir, "init", "-q", "-b", "main")
			runGit(t, dir, "config", "user.email", "t@example.invalid")
			runGit(t, dir, "config", "user.name", "Test")
			runGit(t, dir, "config", "commit.gpgsign", "false")
			putFile(t, dir, "cmd/datawatch/main.go", "var Version = \"1.2.3\"\n")
			runGit(t, dir, "add", "-A")
			runGit(t, dir, "commit", "-q", "-m", "base")
			base := runGit(t, dir, "rev-parse", "HEAD")
			for _, f := range c.files {
				putFile(t, dir, f, "changed\n")
			}
			runGit(t, dir, "add", "-A")
			runGit(t, dir, "commit", "-q", "-m", "work")
			// The release bump commit touches code files in every release.
			putFile(t, dir, "cmd/datawatch/main.go", "var Version = \""+c.version+"\"\n")
			putFile(t, dir, "CHANGELOG.md", "x\n")
			runGit(t, dir, "add", "-A")
			runGit(t, dir, "commit", "-q", "--allow-empty", "-m", "chore(release): v"+c.version+" bump for merged branches")
			cmd := exec.Command("bash", script)
			cmd.Dir = dir
			cmd.Env = append(scrubbedEnv(), "MERGE_BASE_COMMIT="+base, "MERGE_RELEASE_VERSION="+c.version, "MERGE_RELEASE_SIZE="+c.size)
			out, err := cmd.Output()
			if err != nil || strings.TrimSpace(string(out)) != c.want {
				t.Errorf("mode = %q (%v), want %q", strings.TrimSpace(string(out)), err, c.want)
			}
		})
	}
	cmd := exec.Command("bash", script)
	cmd.Dir = t.TempDir()
	cmd.Env = scrubbedEnv()
	if err := cmd.Run(); err == nil {
		t.Error("with no merge facts the choice must fail, not skip the smoke test")
	}
}

func TestProjectIntendedPipelineRunsTheFiveChecksOnTheFinalCandidate(t *testing.T) {
	root := filepath.Join("..", "..")
	def, err := LoadFile(root, "scripts/merge/intended-pipeline.yaml", 0)
	if err != nil {
		t.Fatal(err)
	}
	at := map[string]int{}
	for i, s := range def.Steps {
		at[s.Name] = i + 1
		if s.Action == ActionTagRelease {
			t.Error("tags are on hold: the intended record has no tag step")
		}
		if rest, ok := strings.CutPrefix(s.Command, "bash scripts/merge/"); ok {
			if _, err := os.Stat(filepath.Join(root, "scripts", "merge", strings.Fields(rest)[0])); err != nil {
				t.Errorf("step %s: %v", s.Name, err)
			}
		}
	}
	for _, name := range []string{"lint", "build", "go-test", "node-test", "release-smoke", "parity-gates"} {
		if at[name] == 0 || at[name] < at["commit-release-bump"] || at[name] > at["push-release"] {
			t.Errorf("%s must run after the release bump and before the push (at %d)", name, at[name])
		}
	}
	scripts, err := filepath.Glob(filepath.Join(root, "scripts", "merge", "*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range scripts {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "/home/") {
			t.Errorf("%s names a fixed home path", p)
		}
	}
	if _, err := os.Stat(filepath.Join(root, DefaultFileName)); !os.IsNotExist(err) {
		t.Error("a pipeline file at the default path would replace the live record")
	}
}
