// Guard tests for the generated ignore file: the security-failure coordinator
// and its workers never write .trivyignore, and the repository check
// (scripts/check-no-direct-ignore-writes.sh) catches a hand edit of it.

package autonomous

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const guardRegistryFixture = `entries:
  - id: cve/CVE-2031-0042
    kind: container
    package: "examplelib"
    version: "1.4.2"
    severity: critical
    images:
      - agent-base
    first_added: "2026-01-01"
    added: "2026-01-02"
    expires: "2026-12-31"
    validated_by: "some-operator"
    reason: "no fix on the stable release yet"
    impact:
      traced: false
      analysis: "the vulnerable path is not reachable from our code"
`

const guardRegistryFixtureChanged = guardRegistryFixture + `  - id: cve/CVE-2031-0077
    kind: container
    package: "otherlib"
    version: "2.0.1"
    severity: high
    images:
      - agent-base
    first_added: "2026-02-01"
    added: "2026-02-02"
    expires: "2026-12-31"
    validated_by: "some-operator"
    reason: "fix is not yet in the stable release"
    impact:
      traced: false
      analysis: "only reachable through an unused code path"
`

const guardReviewFixture = `# Security review

## Container image scanning

<!-- BEGIN GENERATED: security/accepted-risks.yml container entries (scripts/gen_trivyignore.py) -->
<!-- END GENERATED -->

## Other
`

func TestSecurityFailureNoDirectIgnoreWrite(t *testing.T) {
	t.Run("workers never mention the ignore file", func(t *testing.T) {
		files, err := filepath.Glob("security_failure_*.go")
		if err != nil {
			t.Fatalf("glob: %v", err)
		}
		if len(files) == 0 {
			t.Fatal("found no security_failure_*.go files to inspect")
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			body, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("read %s: %v", f, err)
			}
			for _, needle := range []string{".trivyignore", "gen_trivyignore"} {
				if bytes.Contains(body, []byte(needle)) {
					t.Errorf("%s mentions %q; the coordinator and its workers must not write the generated ignore file", f, needle)
				}
			}
		}
	})

	t.Run("check catches a hand edit and accepts a regenerated file", func(t *testing.T) {
		if _, err := exec.LookPath("bash"); err != nil {
			t.Skip("bash not found")
		}
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git not found")
		}
		if err := exec.Command("python3", "-c", "import yaml").Run(); err != nil {
			t.Skip("python3 with PyYAML is required to run the generator: " + err.Error())
		}

		repo := t.TempDir()
		write := func(rel, body string, mode os.FileMode) {
			t.Helper()
			p := filepath.Join(repo, rel)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatalf("mkdir for %s: %v", rel, err)
			}
			if err := os.WriteFile(p, []byte(body), mode); err != nil {
				t.Fatalf("write %s: %v", rel, err)
			}
		}
		copyFromRepo := func(rel string) {
			t.Helper()
			body, err := os.ReadFile(filepath.Join("..", "..", rel))
			if err != nil {
				t.Fatalf("read %s: %v", rel, err)
			}
			write(rel, string(body), 0o755)
		}
		run := func(name string, args ...string) (string, error) {
			t.Helper()
			cmd := exec.Command(name, args...)
			cmd.Dir = repo
			cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
			out, err := cmd.CombinedOutput()
			return string(out), err
		}
		mustRun := func(name string, args ...string) {
			t.Helper()
			if out, err := run(name, args...); err != nil {
				t.Fatalf("%s %v: %v\n%s", name, args, err, out)
			}
		}

		copyFromRepo("scripts/check-no-direct-ignore-writes.sh")
		copyFromRepo("scripts/gen_trivyignore.py")
		write("security/accepted-risks.yml", guardRegistryFixture, 0o644)
		write("docs/security-review.md", guardReviewFixture, 0o644)
		write(".trivyignore", "", 0o644)

		mustRun("git", "init", "-q")
		mustRun("python3", "scripts/gen_trivyignore.py")
		mustRun("git", "add", "-A")
		mustRun("git", "-c", "user.name=guard", "-c", "user.email=guard@example.invalid", "commit", "-q", "-m", "fixture")

		check := func() (string, error) { return run("bash", "scripts/check-no-direct-ignore-writes.sh") }

		if out, err := check(); err != nil {
			t.Fatalf("clean check failed: %v\n%s", err, out)
		}

		ignore := filepath.Join(repo, ".trivyignore")
		orig, err := os.ReadFile(ignore)
		if err != nil {
			t.Fatalf("read .trivyignore: %v", err)
		}
		if err := os.WriteFile(ignore, append(orig, []byte("CVE-2099-0001\n")...), 0o644); err != nil {
			t.Fatalf("plant hand edit: %v", err)
		}
		out, err := check()
		if err == nil {
			t.Fatalf("check passed after a hand edit of .trivyignore:\n%s", out)
		}
		if !strings.Contains(out, ".trivyignore") {
			t.Errorf("failure output does not name .trivyignore:\n%s", out)
		}

		// Registry-only change, then regenerate: the check passes again.
		if err := os.WriteFile(ignore, orig, 0o644); err != nil {
			t.Fatalf("restore .trivyignore: %v", err)
		}
		write("security/accepted-risks.yml", guardRegistryFixtureChanged, 0o644)
		mustRun("python3", "scripts/gen_trivyignore.py")
		if out, err := check(); err != nil {
			t.Fatalf("check failed after a registry change and regeneration: %v\n%s", err, out)
		}
	})
}
