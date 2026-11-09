package mergepipeline

import (
	"context"
	"strings"
	"testing"
	"time"
)

// gateMerger is a Merger whose project is dir and whose pipeline holds the
// given lint and test steps. Each step runs the given command.
func gateMerger(dir, lintCmd, testCmd string) *Merger {
	return &Merger{
		Dir: dir,
		Pipeline: Definition{Steps: []Step{
			{Name: "lint", Kind: KindLint, Command: lintCmd, Timeout: time.Minute},
			{Name: "tests", Kind: KindTest, Command: testCmd, Timeout: time.Minute},
		}},
	}
}

func gateSettings() ConflictSettings {
	return ConflictSettings{TestTimeout: time.Minute}
}

// gateProject is a directory whose Makefile defines a build target that
// succeeds, so the build step runs and passes.
func gateProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "Makefile", "build:\n\ttrue\n")
	return dir
}

// commitBuildTarget commits a Makefile on the checked-out branch of dir whose
// build target succeeds, so the conflict gate's build step has a build path.
func commitBuildTarget(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, dir, "Makefile", "build:\n\ttrue\n")
	gitIn(t, dir, "add", "Makefile")
	gitIn(t, dir, "commit", "-q", "-m", "build target")
}

func TestConflictGate(t *testing.T) {
	t.Run("passing gate", func(t *testing.T) {
		dir := gateProject(t)
		m := gateMerger(dir, "true", "true")
		passed, out := m.runConflictGate(context.Background(), dir, gateSettings())
		if !passed {
			t.Fatalf("gate failed on a passing project: %s", out)
		}
		for _, want := range []string{"build: make build", "lint: 1 step(s) passed", "test: 1 step(s) passed"} {
			if !strings.Contains(out, want) {
				t.Errorf("summary %q lacks %q", out, want)
			}
		}
	})

	t.Run("failing lint step", func(t *testing.T) {
		dir := gateProject(t)
		m := gateMerger(dir, "echo lint-broke; exit 3", "true")
		passed, out := m.runConflictGate(context.Background(), dir, gateSettings())
		if passed {
			t.Fatal("gate passed with a failing lint step")
		}
		if !strings.Contains(out, `lint step "lint" failed (exit 3)`) || !strings.Contains(out, "lint-broke") {
			t.Errorf("output %q does not name the failing lint step", out)
		}
	})

	t.Run("failing test step", func(t *testing.T) {
		dir := gateProject(t)
		m := gateMerger(dir, "true", "echo test-broke; exit 1")
		passed, out := m.runConflictGate(context.Background(), dir, gateSettings())
		if passed {
			t.Fatal("gate passed with a failing test step")
		}
		if !strings.Contains(out, `test step "tests" failed (exit 1)`) || !strings.Contains(out, "test-broke") {
			t.Errorf("output %q does not name the failing test step", out)
		}
	})

	t.Run("no build path fails closed", func(t *testing.T) {
		dir := t.TempDir() // no Makefile, no go.mod
		m := gateMerger(dir, "true", "true")
		passed, out := m.runConflictGate(context.Background(), dir, gateSettings())
		if passed {
			t.Fatal("gate passed with no build path")
		}
		if !strings.Contains(out, "no build path") {
			t.Errorf("output %q does not say there is no build path", out)
		}
	})

	t.Run("go.mod build path", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "go.mod", "module example.com/gate\n\ngo 1.21\n")
		writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
		m := gateMerger(dir, "true", "true")
		passed, out := m.runConflictGate(context.Background(), dir, gateSettings())
		if !passed {
			t.Fatalf("gate failed on a go module that builds: %s", out)
		}
		if !strings.Contains(out, "build: go build ./...") {
			t.Errorf("summary %q does not name the go build", out)
		}
	})

	t.Run("no runnable test step fails closed", func(t *testing.T) {
		dir := gateProject(t)
		m := &Merger{Dir: dir, Pipeline: Definition{Steps: []Step{
			{Name: "lint", Kind: KindLint, Command: "true"},
		}}}
		passed, out := m.runConflictGate(context.Background(), dir, gateSettings())
		if passed {
			t.Fatal("gate passed with no test step")
		}
		if !strings.Contains(out, "no test step") {
			t.Errorf("output %q does not say no test step ran", out)
		}
	})

	t.Run("failing build fails the gate", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "Makefile", "build:\n\techo build-broke; exit 2\n")
		m := gateMerger(dir, "true", "true")
		passed, out := m.runConflictGate(context.Background(), dir, gateSettings())
		if passed || !strings.Contains(out, "build-broke") {
			t.Fatalf("passed=%v output=%q; want a failed build", passed, out)
		}
	})

	t.Run("requires_make_target skips a step that cannot run", func(t *testing.T) {
		dir := gateProject(t)
		m := &Merger{Dir: dir, Pipeline: Definition{Steps: []Step{
			{Name: "tests", Kind: KindTest, Command: "true", Timeout: time.Minute},
			{Name: "extra", Kind: KindLint, Command: "exit 9", RequiresMakeTarget: "absent"},
		}}}
		passed, out := m.runConflictGate(context.Background(), dir, gateSettings())
		if !passed {
			t.Fatalf("gate failed on a step that should be skipped: %s", out)
		}
		if !strings.Contains(out, "lint: 0 step(s) passed") {
			t.Errorf("summary %q should show no lint step ran", out)
		}
	})
}
