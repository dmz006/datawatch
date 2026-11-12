// Package ci holds executable tests for repository CI tooling. It is a
// test-only package: it contains no non-test Go source, only test files
// that exercise the bash scripts in scripts/ against throwaway git repos.
package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	baseVersion   = "1.0.0"
	bumpedVersion = "1.0.1"
	zeroSHA       = "00000000000000000000000000000000000000"

	mainFile = "cmd/datawatch/main.go"
	apiFile  = "internal/server/api.go"
	codeFile = "app/core.go"      // non-exempt: does not match EXEMPT_RE
	testFile = "app/core_test.go" // exempt: matches _test\.go$
)

// goModBefore/goModAfter are the Dependabot-style dependency-only change
// pair: go.mod is non-exempt (not in EXEMPT_RE), so a no-bump range that
// touches it fails under the normal rules — exactly the case the
// Dependabot exemption in the script carves out.
const (
	goModBefore = "module datawatch\n\ngo 1.24\n\nrequire example.com/dep v1.0.0\n"
	goModAfter  = "module datawatch\n\ngo 1.24\n\nrequire example.com/dep v1.0.1\n"
)

// goVersionFile renders a minimal Go file carrying `var Version = "..."`
// for one of the two invariants-checked files.
func goVersionFile(pkg, ver string) string {
	return "package " + pkg + "\n\nvar Version = \"" + ver + "\"\n"
}

// repo is a throwaway git repository built under a t.TempDir() directory.
// The script is copied into the repo's scripts/ subdirectory so its
// `cd "$(dirname "$0")/.."` lands on the temp repo, not the real one (it
// is relocatable by design, per its header comment).
type repo struct {
	t       *testing.T
	root    string
	script  string
	initial string // sha of the first commit
}

// newRepo creates a temp git repo with both Version files committed at
// baseVersion, plus one non-exempt and one test-only file. The script is
// copied from the real scripts/ directory (resolved relative to this test
// package's working directory, tests/ci).
func newRepo(t *testing.T) *repo {
	t.Helper()

	root := t.TempDir()

	// resolve script path relative to this package dir (tests/ci) → repo root
	scriptSrc, err := filepath.Abs(filepath.Clean(filepath.Join("..", "..", "scripts", "check-version-bump.sh")))
	if err != nil {
		t.Fatalf("resolving script path: %v", err)
	}
	if _, err := os.Stat(scriptSrc); err != nil {
		t.Fatalf("real script not found at %s: %v", scriptSrc, err)
	}
	data, err := os.ReadFile(scriptSrc)
	if err != nil {
		t.Fatalf("reading script: %v", err)
	}
	scriptTarget := filepath.Join(root, "scripts", "check-version-bump.sh")
	if err := os.MkdirAll(filepath.Dir(scriptTarget), 0o755); err != nil {
		t.Fatalf("mkdir for script: %v", err)
	}
	if err := os.WriteFile(scriptTarget, data, 0o755); err != nil {
		t.Fatalf("copying script into temp repo: %v", err)
	}

	r := &repo{t: t, root: root, script: scriptTarget}
	r.git("init", "-b", "main")
	r.git("config", "user.email", "ci-test@example.com")
	r.git("config", "user.name", "CI Test")
	r.git("config", "commit.gpgsign", "false")

	files := map[string]string{
		mainFile: goVersionFile("main", baseVersion),
		apiFile:  goVersionFile("server", baseVersion),
		codeFile: "package app\n\nfunc Core() int { return 1 }\n",
		testFile: "package app\n\nfunc TestCore(t *testing.T) {}\n",
	}
	r.initial = r.commit("initial", files)
	return r
}

// git runs a git command in the temp repo and fails the test on error.
func (r *repo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.root
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commit writes the given files, stages everything, commits, and returns
// the new HEAD sha.
func (r *repo) commit(msg string, files map[string]string) string {
	r.t.Helper()
	for path, content := range files {
		full := filepath.Join(r.root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			r.t.Fatalf("mkdir for %s: %v", path, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			r.t.Fatalf("write %s: %v", path, err)
		}
	}
	r.git("add", "-A")
	r.git("commit", "-m", msg)
	return r.shaw()
}

func (r *repo) shaw() string {
	r.t.Helper()
	return r.git("rev-parse", "HEAD")
}

// commitAs is commit() with the git author identity overridden via
// GIT_AUTHOR_NAME / GIT_AUTHOR_EMAIL (the committer stays the repo
// default) — used to simulate Dependabot-authored or spoofed-author commits.
func (r *repo) commitAs(msg, author, email string, files map[string]string) string {
	r.t.Helper()
	for path, content := range files {
		full := filepath.Join(r.root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			r.t.Fatalf("mkdir for %s: %v", path, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			r.t.Fatalf("write %s: %v", path, err)
		}
	}
	r.git("add", "-A")
	cmd := exec.Command("git", "commit", "-m", msg)
	cmd.Dir = r.root
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME="+author,
		"GIT_AUTHOR_EMAIL="+email,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git commit (author=%q): %v\n%s", author, err, out)
	}
	return r.shaw()
}

// mergeAs merges branch into the current head with --no-ff so a merge
// commit is always produced, authored by the given name.
func (r *repo) mergeAs(msg, author, branch string) string {
	r.t.Helper()
	cmd := exec.Command("git", "merge", "--no-ff", "-m", msg, branch)
	cmd.Dir = r.root
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME="+author,
		"GIT_AUTHOR_EMAIL="+strings.ToLower(strings.ReplaceAll(author, " ", "-"))+"@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git merge %q (author=%q): %v\n%s", branch, author, err, out)
	}
	return r.shaw()
}

// versionBumpFiles returns the two invariant files at a given version with
// a non-exempt code change, for multi-file commits.
func versionCommitFiles(ver, codeBody string) map[string]string {
	return map[string]string{
		mainFile: goVersionFile("main", ver),
		apiFile:  goVersionFile("server", ver),
		codeFile: "package app\n\nfunc Core() int { return " + codeBody + " }\n",
	}
}

// runScript invokes the copied script inside the temp repo. envExtra keys
// are set; CHECK_VERSION_BASE and GITHUB_* are guaranteed ABSENT unless
// explicitly provided, so no ambient GitHub Actions state leaks in.
func (r *repo) runScript(t *testing.T, args []string, envExtra map[string]string) (string, bool) {
	t.Helper()

	// Start from a known-clean environment: drop the script's env contract
	// variables from the inherited env, keep only PATH/HOME for git+awk.
	inherited := map[string]bool{}
	for _, kv := range os.Environ() {
		k := kv[:strings.IndexByte(kv, '=')]
		switch k {
		case "CHECK_VERSION_BASE", "GITHUB_EVENT_BEFORE", "GITHUB_EVENT_NAME", "GITHUB_BASE_REF", "VERSION_CHECK_MODE":
		default:
			inherited[kv] = true
		}
	}
	env := make([]string, 0, len(inherited)+len(envExtra))
	for kv := range inherited {
		env = append(env, kv)
	}
	for k, v := range envExtra {
		env = append(env, k+"="+v)
	}

	cmd := exec.Command(r.script, args...)
	cmd.Dir = r.root
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

// --- test cases -----------------------------------------------------------

// range pass: non-exempt file changed somewhere in the range and the
// version is bumped inside the range (not in the head commit itself);
// head version strictly greater than base → exit 0.
func TestRangePassBumpInsideRange(t *testing.T) {
	r := newRepo(t)
	base := r.initial

	// commit 2: non-exempt change + bump (inside the range)
	mid := r.commit("feature + bump", versionCommitFiles(bumpedVersion, "2"))
	_ = mid

	// head commit: further non-exempt change, NO bump — head must still pass
	r.commit("more feature work, no bump", map[string]string{
		codeFile: "package app\n\nfunc Core() int { return 3 }\n",
	})
	if r.shaw() == base || r.shaw() == mid {
		t.Fatalf("expected a distinct head commit")
	}

	out, ok := r.runScript(t, []string{base}, nil)
	if !ok {
		t.Fatalf("expected exit 0 (range pass), got non-zero:\n%s", out)
	}
	if !strings.Contains(out, "PASS") {
		t.Fatalf("expected PASS output, got:\n%s", out)
	}
}

// range fail: non-exempt file changed across the range but head version
// equals base version → exit non-zero.
func TestRangeFailNoBump(t *testing.T) {
	r := newRepo(t)
	base := r.initial

	r.commit("feature, no bump", map[string]string{
		codeFile: "package app\n\nfunc Core() int { return 2 }\n",
	})

	out, ok := r.runScript(t, []string{base}, nil)
	if ok {
		t.Fatalf("expected non-zero exit (range fail), got 0:\n%s", out)
	}
	if !strings.Contains(out, "FAIL") {
		t.Fatalf("expected FAIL output, got:\n%s", out)
	}
}

// test-only range pass: only *_test.go changed, version unchanged →
// exemption preserved, exit 0.
func TestRangePassTestOnly(t *testing.T) {
	r := newRepo(t)
	base := r.initial

	r.commit("tests only", map[string]string{
		testFile: "package app\n\nimport \"testing\"\n\nfunc TestCoreMore(t *testing.T) {}\n",
	})

	out, ok := r.runScript(t, []string{base}, nil)
	if !ok {
		t.Fatalf("expected exit 0 (test-only exemption), got non-zero:\n%s", out)
	}
	if !strings.Contains(out, "PASS") {
		t.Fatalf("expected PASS output, got:\n%s", out)
	}
}

// unknown-base fallback: base ref given as the 40-zero sha (GitHub's
// "unknown" marker) through $1, CHECK_VERSION_BASE and GITHUB_EVENT_BEFORE.
// All of those are rejected by the script's valid_ref check; with a local
// `main` branch present (git init -b main, HEAD on main) the default-branch
// fallback resolves merge-base HEAD main = HEAD itself, so the range is
// empty and the script deterministically passes (exit 0) rather than
// crashing. We assert the contractual exit 0.
func TestRangeUnknownBaseFallsBackToDefaultBranch(t *testing.T) {
	r := newRepo(t)

	env := map[string]string{
		"CHECK_VERSION_BASE":  zeroSHA,
		"GITHUB_EVENT_BEFORE": zeroSHA,
		"GITHUB_EVENT_NAME":   "pull_request",
		"GITHUB_BASE_REF":     "main", // no origin/main — remote ref absent, local fallback
	}
	out, ok := r.runScript(t, []string{zeroSHA}, env)
	if !ok {
		t.Fatalf("expected exit 0 (unknown-base fallback must not crash or spurious-fail), got non-zero:\n%s", out)
	}
	// Either "skipping" (base unresolvable) or a PASS via the main
	// fallback is acceptable per the script's documented contract; both
	// are exit 0. We just require no FAIL.
	if strings.Contains(out, "FAIL") {
		t.Fatalf("did not expect FAIL output:\n%s", out)
	}
}

// commit mode (legacy semantics): head commit changed a non-exempt file
// but did NOT bump the version → exit non-zero, even though the "range"
// is only two commits. Preserves the old strict behavior.
func TestCommitModeFailNoBump(t *testing.T) {
	r := newRepo(t)
	base := r.initial

	r.commit("feature, no bump", map[string]string{
		codeFile: "package app\n\nfunc Core() int { return 2 }\n",
	})

	out, ok := r.runScript(t, []string{base}, map[string]string{"VERSION_CHECK_MODE": "commit"})
	if ok {
		t.Fatalf("expected non-zero exit (commit mode, no bump), got 0:\n%s", out)
	}
	if !strings.Contains(out, "FAIL") {
		t.Fatalf("expected FAIL output, got:\n%s", out)
	}
}

// commit mode (legacy semantics, positive case): head commit changed a
// non-exempt file AND bumped the version → exit 0 (any version difference
// passes in commit mode).
func TestCommitModePassBump(t *testing.T) {
	r := newRepo(t)
	base := r.initial

	r.commit("feature + bump", versionCommitFiles(bumpedVersion, "2"))

	out, ok := r.runScript(t, []string{base}, map[string]string{"VERSION_CHECK_MODE": "commit"})
	if !ok {
		t.Fatalf("expected exit 0 (commit mode, bumped), got non-zero:\n%s", out)
	}
	if !strings.Contains(out, "PASS") {
		t.Fatalf("expected PASS output, got:\n%s", out)
	}
}

// base resolution order: CHECK_VERSION_BASE must be consulted before
// GITHUB_EVENT_BEFORE when $1 is absent (contract: $1 →
// CHECK_VERSION_BASE → GITHUB_EVENT_BEFORE → PR refs → default branch).
// Setup where the two candidates give OPPOSITE results:
//
//	commit 1: v1.0.0 (initial)
//	commit 2: v1.0.1 + non-exempt change (the bump)
//	commit 3: non-exempt change, no bump (head stays v1.0.1)
//
// CHECK_VERSION_BASE=commit 1 → 1.0.1 > 1.0.0 → PASS (exit 0)
// GITHUB_EVENT_BEFORE=commit 2 → 1.0.1 == 1.0.1 with non-exempt diff → FAIL
// If the script resolved GITHUB_EVENT_BEFORE first, it would exit non-zero;
// asserting exit 0 proves CHECK_VERSION_BASE wins.
func TestRangeBaseResolutionOrder(t *testing.T) {
	r := newRepo(t)

	r.commit("bump", versionCommitFiles(bumpedVersion, "2"))
	r.commit("more work, no bump", map[string]string{
		codeFile: "package app\n\nfunc Core() int { return 3 }\n",
	})

	env := map[string]string{
		"CHECK_VERSION_BASE":  r.initial,                    // v1.0.0  → range would PASS
		"GITHUB_EVENT_BEFORE": r.git("rev-parse", "HEAD~1"), // v1.0.1 → range would FAIL
	}
	out, ok := r.runScript(t, nil, env)
	if !ok {
		t.Fatalf("expected CHECK_VERSION_BASE to win (exit 0), got non-zero:\n%s", out)
	}
	if !strings.Contains(out, "CHECK_VERSION_BASE") {
		t.Fatalf("expected output to note base resolved via $CHECK_VERSION_BASE, got:\n%s", out)
	}
}

// --- Dependabot exemption (AGENT.md Versioning) ---------------------------

const (
	depName  = "dependabot[bot]"
	depEmail = "49699333+dependabot[bot]@users.noreply.github.com"
)

// (1) range mode: a Dependabot-authored go.mod change with NO version bump
// passes — the exemption carves out dependabot[bot]-authored ranges.
func TestRangePassDependabotNoBump(t *testing.T) {
	r := newRepo(t)
	base := r.initial

	r.commitAs("build(deps): bump example.com/dep from 1.0.0 to 1.0.1", depName, depEmail, map[string]string{
		"go.mod": goModAfter,
	})

	out, ok := r.runScript(t, []string{base}, nil)
	if !ok {
		t.Fatalf("expected exit 0 (Dependabot exemption), got non-zero:\n%s", out)
	}
	if !strings.Contains(out, "PASS") || !strings.Contains(out, "Dependabot") {
		t.Fatalf("expected Dependabot-exempt PASS output, got:\n%s", out)
	}
}

// (2) commit mode: the same Dependabot-authored no-bump change passes.
func TestCommitModePassDependabotNoBump(t *testing.T) {
	r := newRepo(t)
	base := r.initial

	r.commitAs("build(deps): bump example.com/dep from 1.0.0 to 1.0.1", depName, depEmail, map[string]string{
		"go.mod": goModAfter,
	})

	out, ok := r.runScript(t, []string{base}, map[string]string{"VERSION_CHECK_MODE": "commit"})
	if !ok {
		t.Fatalf("expected exit 0 (Dependabot exemption, commit mode), got non-zero:\n%s", out)
	}
	if !strings.Contains(out, "PASS") || !strings.Contains(out, "Dependabot") {
		t.Fatalf("expected Dependabot-exempt PASS output, got:\n%s", out)
	}
}

// (3) a HUMAN-authored go.mod change with no bump still fails — the
// exemption is keyed on the author, not the file.
func TestRangeFailHumanGoModNoBump(t *testing.T) {
	r := newRepo(t)
	base := r.initial

	r.commit("bump dep (human), no version bump", map[string]string{
		"go.mod": goModAfter,
	})

	out, ok := r.runScript(t, []string{base}, nil)
	if ok {
		t.Fatalf("expected non-zero exit (human go.mod change, no bump), got 0:\n%s", out)
	}
	if !strings.Contains(out, "FAIL") {
		t.Fatalf("expected FAIL output, got:\n%s", out)
	}
}

// (4) a MIXED range — a dependabot[bot] commit plus any commit by another
// author — is not exempt and fails without a bump.
func TestRangeFailMixedDependabotAndHuman(t *testing.T) {
	r := newRepo(t)
	base := r.initial

	r.commitAs("build(deps): bump example.com/dep from 1.0.0 to 1.0.1", depName, depEmail, map[string]string{
		"go.mod": goModAfter,
	})
	r.commit("more work, no bump", map[string]string{
		codeFile: "package app\n\nfunc Core() int { return 2 }\n",
	})

	out, ok := r.runScript(t, []string{base}, nil)
	if ok {
		t.Fatalf("expected non-zero exit (mixed authors, no bump), got 0:\n%s", out)
	}
	if !strings.Contains(out, "FAIL") {
		t.Fatalf("expected FAIL output, got:\n%s", out)
	}
}

// (5) an author name that merely CONTAINS the bot name is not the bot —
// whole-name compare must reject dependabot[bot] lookalikes.
func TestRangeFailSpoofedDependabotAuthor(t *testing.T) {
	r := newRepo(t)
	base := r.initial

	r.commitAs("build(deps): bump (spoofed author)", "not-dependabot[bot]", "spoof@example.com", map[string]string{
		"go.mod": goModAfter,
	})

	out, ok := r.runScript(t, []string{base}, nil)
	if ok {
		t.Fatalf("expected non-zero exit (spoofed author), got 0:\n%s", out)
	}
	if !strings.Contains(out, "FAIL") {
		t.Fatalf("expected FAIL output, got:\n%s", out)
	}
}

// (6) a Dependabot commit that a human merges (no-ff merge commit) stays
// exempt: merge commits are ignored, so the only non-merge commit in the
// range is still dependabot[bot].
func TestRangePassDependabotThenHumanMerge(t *testing.T) {
	r := newRepo(t)
	base := r.initial

	r.git("checkout", "-b", "dependabot-bump", base)
	r.commitAs("build(deps): bump example.com/dep from 1.0.0 to 1.0.1", depName, depEmail, map[string]string{
		"go.mod": goModAfter,
	})
	r.git("checkout", "main")
	r.mergeAs("Merge branch 'dependabot-bump'", "CI Test", "dependabot-bump")

	out, ok := r.runScript(t, []string{base}, nil)
	if !ok {
		t.Fatalf("expected exit 0 (Dependabot commit + human merge), got non-zero:\n%s", out)
	}
	if !strings.Contains(out, "PASS") || !strings.Contains(out, "Dependabot") {
		t.Fatalf("expected Dependabot-exempt PASS output, got:\n%s", out)
	}
}
