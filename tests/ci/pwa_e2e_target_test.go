package ci

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// pwaRepoRoot resolves the repository root from the test working directory,
// which is tests/ci for this package.
func pwaRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

// TestPWAMakeTargetFailsLoudly runs `make test-e2e-pwa` against a suite path
// that does not exist. The make target must fail before docker is reached: the
// output names the missing path, and a fake docker on PATH never runs.
func TestPWAMakeTargetFailsLoudly(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make not on PATH")
	}
	root := pwaRepoRoot(t)

	binDir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "docker-ran")
	fakeDocker := "#!/bin/sh\ntouch \"" + marker + "\"\nexit 0\n"
	if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte(fakeDocker), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}

	missing := filepath.Join(t.TempDir(), "no-such-suite")
	cmd := exec.Command("make", "-s", "test-e2e-pwa", "PWA_SUITE_DIR="+missing)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected make to exit non-zero, got err=%v\n%s", err, out)
	}
	if exitErr.ExitCode() == 0 {
		t.Fatalf("expected non-zero exit code, got 0\n%s", out)
	}
	if !strings.Contains(string(out), missing) {
		t.Fatalf("expected output to name missing path %s, got:\n%s", missing, out)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("fake docker ran (marker present at %s); make must stop before docker", marker)
	}
}

// TestPWASuitePathExists asserts the committed PWA story suite is complete and
// that scripts/check-pwa-suite-path.sh accepts it.
func TestPWASuitePathExists(t *testing.T) {
	root := pwaRepoRoot(t)
	suite := filepath.Join(root, "scripts", "test-stories", "pwa")

	for _, name := range []string{"package.json", "package-lock.json"} {
		if _, err := os.Stat(filepath.Join(suite, name)); err != nil {
			t.Fatalf("expected %s in suite dir: %v", name, err)
		}
	}

	stories, err := filepath.Glob(filepath.Join(suite, "TS-*.mjs"))
	if err != nil {
		t.Fatalf("glob TS-*.mjs: %v", err)
	}
	if len(stories) == 0 {
		t.Fatalf("expected at least one TS-*.mjs story in %s", suite)
	}

	script := filepath.Join(root, "scripts", "check-pwa-suite-path.sh")
	out, err := exec.Command("bash", script, suite).CombinedOutput()
	if err != nil {
		t.Fatalf("check-pwa-suite-path.sh %s: expected exit 0, got %v\n%s", suite, err, out)
	}
}

// pwaComposeFile is the subset of docker-compose.test.yml this test reads.
type pwaComposeFile struct {
	Services map[string]struct {
		Volumes    []string `yaml:"volumes"`
		WorkingDir string   `yaml:"working_dir"`
	} `yaml:"services"`
}

// TestPWACompose checks the playwright service in the test compose file: it
// mounts the story suite at /tests/pwa from a host directory that exists and
// holds package.json, uses /tests/pwa as its working dir, and has no test/e2e mount.
func TestPWACompose(t *testing.T) {
	root := pwaRepoRoot(t)
	composePath := filepath.Join(root, "docs", "testing", "docker-compose.test.yml")

	raw, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("read compose file: %v", err)
	}
	var compose pwaComposeFile
	if err := yaml.Unmarshal(raw, &compose); err != nil {
		t.Fatalf("parse compose file: %v", err)
	}

	svc, ok := compose.Services["playwright"]
	if !ok {
		t.Fatalf("compose file has no playwright service")
	}
	if svc.WorkingDir != "/tests/pwa" {
		t.Fatalf("playwright working_dir: want /tests/pwa, got %q", svc.WorkingDir)
	}

	wantHost := filepath.Join(root, "scripts", "test-stories", "pwa")
	var found bool
	for _, vol := range svc.Volumes {
		if strings.Contains(vol, "test/e2e") {
			t.Fatalf("playwright still mounts old test/e2e path: %q", vol)
		}
		parts := strings.Split(vol, ":")
		if len(parts) < 2 || parts[1] != "/tests/pwa" {
			continue
		}
		found = true
		host := filepath.Clean(filepath.Join(root, "docs", "testing", parts[0]))
		if host != wantHost {
			t.Fatalf("playwright /tests/pwa host path: want %s, got %s", wantHost, host)
		}
		if _, err := os.Stat(filepath.Join(host, "package.json")); err != nil {
			t.Fatalf("mounted host dir %s has no package.json: %v", host, err)
		}
	}
	if !found {
		t.Fatalf("playwright service has no volume mounting /tests/pwa, volumes=%v", svc.Volumes)
	}
}
