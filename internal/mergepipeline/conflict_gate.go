package mergepipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// runConflictGate decides whether a model proposal may be committed. It runs in
// dir, the scratch worktree, and passes only when all of these hold:
//
//   - the build runs and succeeds: the Makefile's build target when the
//     Makefile defines one, otherwise `go build ./...` when dir holds a go.mod.
//     With neither there is no build path, and the gate fails closed.
//   - every pipeline step of kind lint that can run here passes.
//   - every pipeline step of kind test that can run here passes, and at least
//     one test step ran. No runnable test step fails closed.
//
// A step can run here when its RequiresMakeTarget, if set, is defined by the
// Makefile. A step whose command is empty (a built-in action) or calls an
// unscoped tmux kill cannot be shown to pass, so it counts as a failure. Each
// step runs under the same timeout rules as runTestSteps: the step's own
// timeout, or set.TestTimeout when it sets none. The build uses
// set.TestTimeout.
//
// The returned text is the tail of the failing step's output, or a summary of
// what ran when the gate passes.
func (m *Merger) runConflictGate(ctx context.Context, dir string, set ConflictSettings) (passed bool, output string) {
	build, berr := m.gateBuild(ctx, dir, set)
	if berr != "" {
		return false, berr
	}

	var summary []string
	summary = append(summary, "build: "+build)

	lint, lerr := m.gateSteps(ctx, dir, set, KindLint)
	if lerr != "" {
		return false, lerr
	}
	summary = append(summary, fmt.Sprintf("lint: %d step(s) passed", lint))

	tests, terr := m.gateSteps(ctx, dir, set, KindTest)
	if terr != "" {
		return false, terr
	}
	if tests == 0 {
		return false, "the pipeline has no test step that can run here"
	}
	summary = append(summary, fmt.Sprintf("test: %d step(s) passed", tests))

	return true, strings.Join(summary, "\n")
}

// gateBuild runs the build in dir. It returns a description of the build that
// ran, or a failure text (second result non-empty) when the build cannot run or
// fails.
func (m *Merger) gateBuild(ctx context.Context, dir string, set ConflictSettings) (desc string, failure string) {
	var name string
	var args []string
	switch {
	case makefileHasTarget(dir, "build"):
		name, args, desc = "make", []string{"build"}, "make build"
	case fileExists(filepath.Join(dir, "go.mod")):
		name, args, desc = "go", []string{"build", "./..."}, "go build ./..."
	default:
		return "", "the project has no build path: its Makefile defines no build target and it has no go.mod"
	}
	tctx, cancel := context.WithTimeout(ctx, set.TestTimeout)
	defer cancel()
	r, err := m.run(tctx, dir, name, args...)
	if err != nil {
		return "", fmt.Sprintf("build %q did not finish: %v\n%s", desc, err, tail(r.Out, 2000))
	}
	if r.Code != 0 {
		return "", fmt.Sprintf("build %q failed (exit %d)\n%s", desc, r.Code, tail(r.Out, 2000))
	}
	return desc, ""
}

// fileExists reports whether path names an existing regular file or directory.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// gateSteps runs every step of the given kind that can run here, in pipeline
// order. It returns how many ran and passed, or a failure text.
func (m *Merger) gateSteps(ctx context.Context, dir string, set ConflictSettings, kind Kind) (int, string) {
	ran := 0
	for _, s := range m.Pipeline.Steps {
		if s.Kind != kind {
			continue
		}
		if s.RequiresMakeTarget != "" && !makefileHasTarget(dir, s.RequiresMakeTarget) {
			continue
		}
		if strings.TrimSpace(s.Command) == "" {
			return 0, fmt.Sprintf("%s step %q is a built-in action and cannot be run on a proposal", kind, s.Name)
		}
		if CallsUnscopedTmuxKill(s.Command) {
			return 0, fmt.Sprintf("%s step %q calls an unscoped tmux kill and was not run", kind, s.Name)
		}
		wd := dir
		if s.Dir != "" {
			if checkDir(s.Dir) != nil {
				return 0, fmt.Sprintf("%s step %q has an unusable dir", kind, s.Name)
			}
			wd = filepath.Join(dir, filepath.FromSlash(s.Dir))
		}
		timeout := s.Timeout
		if timeout <= 0 {
			timeout = set.TestTimeout
		}
		tctx, cancel := context.WithTimeout(ctx, timeout)
		r, err := m.run(tctx, wd, "sh", "-c", s.Command)
		cancel()
		ran++
		if err != nil {
			return 0, fmt.Sprintf("%s step %q did not finish: %v\n%s", kind, s.Name, err, tail(r.Out, 2000))
		}
		if r.Code != 0 {
			return 0, fmt.Sprintf("%s step %q failed (exit %d)\n%s", kind, s.Name, r.Code, tail(r.Out, 2000))
		}
	}
	return ran, ""
}
