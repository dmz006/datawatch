//go:build unix

package mergepipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// A project's makefile that links outside the project stops a step that needs
// a make target: the step fails, it is not skipped, and the link is not
// followed.

// A link out of the project, present or dangling, fails a step that needs a
// make target, for each name make reads.
func TestMakefileLinkOutFailsRequiredStep(t *testing.T) {
	for _, name := range []string{"GNUmakefile", "makefile", "Makefile"} {
		t.Run(name, func(t *testing.T) {
			proj, _ := linkOut(t, name, "test:\n\ttrue\n", false)
			s := &runState{r: &Runner{Dir: proj}}
			step := Step{Name: "tests", Kind: KindTest, Command: "true", RequiresMakeTarget: "test"}
			res, detail, _ := s.execute(context.Background(), step)
			if res != autonomous.MergeStepFailed {
				t.Fatalf("result %q, want failed; detail %q", res, detail)
			}
			if !strings.Contains(detail, name) || !strings.Contains(detail, "links outside the project") {
				t.Errorf("reason %q does not name %s and the outside link", detail, name)
			}
		})
	}
}

func TestDanglingMakefileLinkOutFailsRequiredStep(t *testing.T) {
	proj := t.TempDir()
	if err := os.Symlink(filepath.Join("..", "other", "Makefile"), filepath.Join(proj, "Makefile")); err != nil {
		t.Fatal(err)
	}
	s := &runState{r: &Runner{Dir: proj}}
	step := Step{Name: "tests", Kind: KindTest, Command: "true", RequiresMakeTarget: "test"}
	res, detail, _ := s.execute(context.Background(), step)
	if res != autonomous.MergeStepFailed || !strings.Contains(detail, "links outside the project") {
		t.Fatalf("result %q, detail %q; want a failed step naming the outside link", res, detail)
	}
}

// The conflict gate does not pass when a required step's make target would come
// through a link out of the project.
func TestMakefileLinkOutFailsConflictGate(t *testing.T) {
	proj, _ := linkOut(t, "Makefile", "build:\n\ttrue\ntest:\n\ttrue\n", false)
	writeFile(t, proj, "x.go", "package proj\n")
	writeFile(t, proj, "go.mod", "module example.invalid/proj\n\ngo 1.21\n")
	m := &Merger{Dir: proj, Pipeline: Definition{Steps: []Step{
		{Name: "tests", Kind: KindTest, Command: "true", RequiresMakeTarget: "test", Timeout: time.Minute},
	}}}
	passed, out := m.runConflictGate(context.Background(), proj, gateSettings())
	if passed || !strings.Contains(out, "Makefile") || !strings.Contains(out, "links outside the project") {
		t.Fatalf("passed=%v output=%q; want a failed gate naming the outside link", passed, out)
	}
}

// A normal makefile that lacks the target still skips the step.
func TestMissingMakeTargetStillSkipsStep(t *testing.T) {
	proj := t.TempDir()
	writeFile(t, proj, "Makefile", "build:\n\ttrue\n")
	s := &runState{r: &Runner{Dir: proj}}
	step := Step{Name: "tests", Kind: KindTest, Command: "true", RequiresMakeTarget: "test"}
	res, detail, _ := s.execute(context.Background(), step)
	if res != autonomous.MergeStepSkipped || !strings.Contains(detail, "test target") {
		t.Fatalf("result %q, detail %q; want the step skipped", res, detail)
	}
}

// A makefile link that stays inside the project still supplies its target, so
// the gate runs the step.
func TestInsideMakefileLinkStillRunsGateStep(t *testing.T) {
	proj := t.TempDir()
	if err := os.MkdirAll(filepath.Join(proj, "real"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(proj, "real"), "Makefile", "build:\n\ttrue\ntest:\n\ttrue\n")
	if err := os.Symlink(filepath.Join("real", "Makefile"), filepath.Join(proj, "Makefile")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, proj, "x.go", "package proj\n")
	writeFile(t, proj, "go.mod", "module example.invalid/proj\n\ngo 1.21\n")
	m := &Merger{Dir: proj, Pipeline: Definition{Steps: []Step{
		{Name: "tests", Kind: KindTest, Command: "true", RequiresMakeTarget: "test", Timeout: time.Minute},
	}}}
	passed, out := m.runConflictGate(context.Background(), proj, gateSettings())
	if !passed || !strings.Contains(out, "test: 1 step(s) passed") {
		t.Fatalf("passed=%v output=%q; want the inside link's step to run and pass", passed, out)
	}
}
