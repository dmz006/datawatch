package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// mergeRepo is a throwaway git repository built under a t.TempDir()
// directory for tests that exercise merge-related CI scripts. Every
// repository it creates is temporary; nothing here touches a real checkout
// or any running service.
type mergeRepo struct {
	t    *testing.T
	root string
}

// newMergeRepo creates a temp git repo on branch main with a Go version
// pair, a changelog, one small Go source file, and the named scripts copied
// from the real scripts/ directory into scripts/NAME (mode 0755). Everything
// is committed as "base".
func newMergeRepo(t *testing.T, scripts ...string) *mergeRepo {
	t.Helper()

	r := &mergeRepo{t: t, root: t.TempDir()}
	r.git("init", "-b", "main")
	r.git("config", "user.email", "ci-test@example.com")
	r.git("config", "user.name", "CI Test")
	r.git("config", "commit.gpgsign", "false")

	r.writeFile("cmd/datawatch/main.go", "package main\n\nvar Version = \"1.0.0\"\n")
	r.writeFile("internal/server/api.go", "package server\n\nvar Version = \"1.0.0\"\n")
	r.writeFile("CHANGELOG.md", strings.Join([]string{
		"# Changelog",
		"",
		"## [Unreleased]",
		"",
		"## v1.0.0 — first release",
		"",
	}, "\n"))
	r.writeFile("app/core.go", "package app\n\nfunc Core() int { return 1 }\n")

	for _, name := range scripts {
		src, err := filepath.Abs(filepath.Join("..", "..", "scripts", name))
		if err != nil {
			t.Fatalf("resolving script %s: %v", name, err)
		}
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("reading script %s: %v", src, err)
		}
		dst := filepath.Join(r.root, "scripts", name)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatalf("mkdir for script %s: %v", name, err)
		}
		if err := os.WriteFile(dst, data, 0o755); err != nil {
			t.Fatalf("copying script %s into temp repo: %v", name, err)
		}
	}

	r.commitAll("base")
	return r
}

// git runs a git command in the temp repo, fails the test on error, and
// returns its trimmed stdout.
func (r *mergeRepo) git(args ...string) string {
	r.t.Helper()
	out, err := r.gitErr(args...)
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(out)
}

// gitErr runs a git command in the temp repo and returns its combined
// output and error without failing the test.
func (r *mergeRepo) gitErr(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.root
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// runScript runs scripts/NAME under bash with the temp repo as the working
// directory and returns its combined stdout and stderr.
func (r *mergeRepo) runScript(name string, args ...string) (string, error) {
	cmdArgs := append([]string{filepath.Join(r.root, "scripts", name)}, args...)
	cmd := exec.Command("bash", cmdArgs...)
	cmd.Dir = r.root
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// writeFile writes content to path, relative to the temp repo root, creating
// any missing parent directories.
func (r *mergeRepo) writeFile(path, content string) {
	r.t.Helper()
	full := filepath.Join(r.root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		r.t.Fatalf("writing %s: %v", path, err)
	}
}

// commitAll stages every change in the temp repo and commits it with msg.
func (r *mergeRepo) commitAll(msg string) {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-m", msg)
}

// head returns the sha of the HEAD commit of the temp repo.
func (r *mergeRepo) head() string {
	r.t.Helper()
	return r.git("rev-parse", "HEAD")
}

// versionAt returns the Version value declared in cmd/datawatch/main.go at
// the given git ref, reading the blob with git show.
func (r *mergeRepo) versionAt(ref string) string {
	r.t.Helper()
	src := r.git("show", ref+":cmd/datawatch/main.go")
	const prefix = `var Version = "`
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) && strings.HasSuffix(line, `"`) {
			return strings.TrimSuffix(strings.TrimPrefix(line, prefix), `"`)
		}
	}
	r.t.Fatalf("no Version declaration in cmd/datawatch/main.go at %s", ref)
	return ""
}
