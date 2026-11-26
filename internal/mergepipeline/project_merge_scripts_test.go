package mergepipeline

// This repository's own merge checks (scripts/merge) and the pipeline record
// meant to run them. Nothing here changes a live record.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	// A model's conflict proposal must pass every check inside the merge
	// step, so that step needs time for them.
	if i := at["merge-branches"]; i == 0 || def.Steps[i-1].Timeout < time.Hour {
		t.Error("merge-branches is too short for the checks a conflict proposal must pass")
	}
	parity, err := os.ReadFile(filepath.Join(root, "scripts", "merge", "parity-gates.sh"))
	if err != nil || !strings.Contains(string(parity), "MERGE_PROPOSAL") {
		t.Errorf("the parity gates would fail every proposal on the missing release bump (%v)", err)
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

// A merge check has an empty HOME. Tests that commit without naming an
// identity, as some here do, must still run, and what fails must be named last.
func TestProjectGoTestCheckRunsWithoutAGitIdentity(t *testing.T) {
	for name, c := range map[string]struct {
		extra string
		fails bool
	}{"passing": {"", false}, "failing": {"\nfunc TestBroken(t *testing.T) { t.Fatal(\"broken on purpose\") }\n", true}} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			runGit(t, dir, "init", "-q", "-b", "main")
			putFile(t, dir, "go.mod", "module sandbox\n\ngo 1.22\n")
			putFile(t, dir, "Makefile", "sync-docs docs-index:\n\t@true\n")
			putFile(t, dir, "a_test.go", `package sandbox

import (
	"os/exec"
	"testing"
)

func TestCommits(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"commit", "-q", "--allow-empty", "-m", "x"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
}
`+c.extra)
			for _, f := range []string{"lib.sh", "go-test.sh"} {
				data, err := os.ReadFile(filepath.Join("..", "..", "scripts", "merge", f))
				if err != nil {
					t.Fatal(err)
				}
				putFile(t, dir, "scripts/merge/"+f, string(data))
			}
			home := t.TempDir()
			cmd := exec.Command("bash", "scripts/merge/go-test.sh")
			cmd.Dir = dir
			cmd.Env = isolatedEnv(os.Environ(), home, t.TempDir(), nil)
			cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GOFLAGS=")
			out, err := cmd.CombinedOutput()
			if (err != nil) != c.fails {
				t.Fatalf("err = %v\n%s", err, out)
			}
			if c.fails {
				last := string(out[max(0, len(out)-400):])
				if !strings.Contains(last, "--- FAIL: TestBroken") || strings.Contains(string(out), "TestCommits") {
					t.Errorf("the failed test must be named at the end, and only it: %s", out)
				}
			}
			if left, _ := os.ReadDir(home); name == "passing" {
				for _, e := range left {
					if e.Name() == ".gitconfig" {
						t.Error("the script wrote a git config into HOME")
					}
				}
			}
		})
	}
}
