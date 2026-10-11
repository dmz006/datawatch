// BL403 Phase 1 — runs scripts/check-plugin-manifests.sh against the
// testdata/manifests/{valid,invalid} fixtures so the shell checker's
// positive and negative cases execute inside `go test ./...`, and asserts
// the Go validator (Manifest.Validate) agrees with it on every fixture.

package plugins

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// repoRoot walks up from the test's working dir to the dir holding go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above test working dir")
		}
		dir = parent
	}
}

// fixtureDirs returns the absolute fixture dirs under testdata/manifests/<group>.
func fixtureDirs(t *testing.T, group string) []string {
	t.Helper()
	base, err := filepath.Abs(filepath.Join("testdata", "manifests", group))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatalf("read %s: %v", base, err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(base, e.Name()))
		}
	}
	sort.Strings(dirs)
	if len(dirs) == 0 {
		t.Fatalf("no fixtures under %s", base)
	}
	return dirs
}

// runCheckScript runs the manifest checker with PLUGIN_MANIFEST_ROOTS=roots
// and an isolated HOME so the operator's ~/.datawatch/plugins is never read.
func runCheckScript(t *testing.T, script, roots string) (string, int) {
	t.Helper()
	cmd := exec.Command("bash", script)
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "HOME=") || strings.HasPrefix(kv, "PLUGIN_MANIFEST_ROOTS=") {
			continue
		}
		env = append(env, kv)
	}
	cmd.Env = append(env, "HOME="+t.TempDir(), "PLUGIN_MANIFEST_ROOTS="+roots)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return string(out), ee.ExitCode()
	}
	t.Fatalf("run %s: %v\n%s", script, err, out)
	return "", -1
}

func copyFixture(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatalf("copy %s: %v", src, err)
	}
}

func TestCheckScript_Fixtures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("check-plugin-manifests.sh is a bash script")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	script := filepath.Join(repoRoot(t), "scripts", "check-plugin-manifests.sh")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("script missing: %v", err)
	}

	t.Run("valid", func(t *testing.T) {
		validRoot, err := filepath.Abs(filepath.Join("testdata", "manifests", "valid"))
		if err != nil {
			t.Fatal(err)
		}
		out, code := runCheckScript(t, script, validRoot)
		if code != 0 {
			t.Fatalf("valid fixtures: exit %d, want 0\n%s", code, out)
		}
		if !strings.Contains(out, "PASS") {
			t.Errorf("valid fixtures: output missing PASS\n%s", out)
		}
	})

	for _, src := range fixtureDirs(t, "invalid") {
		name := filepath.Base(src)
		t.Run("invalid/"+name, func(t *testing.T) {
			// Isolate the fixture so each defect is checked on its own.
			root := t.TempDir()
			dst := filepath.Join(root, name)
			copyFixture(t, src, dst)
			manifest := filepath.Join(dst, "manifest.yaml")

			out, code := runCheckScript(t, script, root)
			if code == 0 {
				t.Fatalf("exit 0, want non-zero\n%s", out)
			}
			if !strings.Contains(out, "  ✗ "+manifest) {
				t.Errorf("output does not report %s\n%s", manifest, out)
			}
		})
	}
}

// TestCheckScript_GoValidatorParity keeps the shell checker and
// Manifest.Validate in agreement on every fixture.
func TestCheckScript_GoValidatorParity(t *testing.T) {
	load := func(t *testing.T, dir string) Manifest {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(dir, "manifest.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		var m Manifest
		if err := yaml.Unmarshal(b, &m); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		m.Normalize()
		return m
	}
	for _, dir := range fixtureDirs(t, "valid") {
		t.Run("valid/"+filepath.Base(dir), func(t *testing.T) {
			if err := load(t, dir).Validate(); err != nil {
				t.Errorf("Validate: %v, want nil", err)
			}
		})
	}
	for _, dir := range fixtureDirs(t, "invalid") {
		t.Run("invalid/"+filepath.Base(dir), func(t *testing.T) {
			if err := load(t, dir).Validate(); err == nil {
				t.Error("Validate: nil, want error")
			}
		})
	}
}
