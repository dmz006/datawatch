// What makes the daemon's merge runs safe to leave unattended: each is built
// in a scratch worktree, sized from its Automaton, checked against the real
// model registry, and asked again just before its push whether merging is
// still allowed.

package server

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/mergepipeline"
)

// The optional Manager surfaces a merge run uses.
type (
	mergeReleaseSource interface {
		MergeReleaseFacts(id string, kindMap map[string]string, majorTrigger string) (autonomous.MergeReleaseFacts, bool)
	}
	mergeLookupSource interface{ MergeModelLookup() config.LLMLookup }
	mergePauseSource  interface{ MergePauseMessage() (string, bool) }
)

// The daemon's Manager and API must carry all three.
var (
	_ mergeReleaseSource = (*autonomous.API)(nil)
	_ mergeLookupSource  = (*autonomous.API)(nil)
	_ mergePauseSource   = (*autonomous.API)(nil)
	_ mergeReleaseSource = (*autonomous.Manager)(nil)
	_ mergeLookupSource  = (*autonomous.Manager)(nil)
	_ mergePauseSource   = (*autonomous.Manager)(nil)
)

// mergeScratchRoot is where merge runs make their scratch worktrees.
func mergeScratchRoot(dataDir string) string {
	return filepath.Join(dataDir, "merge-pipeline", "scratch")
}

// mergeModelLookup is the registry the conflict model is checked against; nil
// when the Manager has none, and the model policy then stops and asks.
func mergeModelLookup(mr mergeRunner) config.LLMLookup {
	if src, ok := mr.(mergeLookupSource); ok {
		return src.MergeModelLookup()
	}
	return nil
}

// makeMergeRunSafe wires the isolation, release facts and pause check onto a
// runner.
func makeMergeRunSafe(r *mergepipeline.Runner, mr mergeRunner, cfg config.MergePipelineConfig, project, dataDir string) {
	r.Isolate = true
	r.ScratchRoot = mergeScratchRoot(dataDir)
	if src, ok := mr.(mergeReleaseSource); ok && len(r.VersionFiles) > 0 {
		built := r.ReleaseApproval
		r.ReleaseFor = func(id string) (mergepipeline.ReleaseFacts, bool) {
			f, ok := src.MergeReleaseFacts(id, cfg.EffectiveReleaseSizeMap(project), cfg.EffectiveMajorTrigger(project))
			if !ok {
				return mergepipeline.ReleaseFacts{}, false
			}
			approval := cfg.EffectiveReleaseApproval(project, f.Size)
			if f.RaisesMajorApproval {
				approval = cfg.EffectiveReleaseApproval(project, config.ReleaseSizeMajor)
			}
			// An approval a caller set on the runner after it was built
			// stands: an empty one here leaves it as it is.
			if r.ReleaseApproval != built {
				approval = ""
			}
			return mergepipeline.ReleaseFacts{Size: f.Size, Approval: approval}, true
		}
	}
	if src, ok := mr.(mergePauseSource); ok {
		r.StillAllowed = func(context.Context) error {
			if msg, paused := src.MergePauseMessage(); paused {
				return errors.New(msg)
			}
			return nil
		}
	}
}

// withMergeSwitch also refuses the push once the merge run and deploy run
// switch has been turned off during the run.
func withMergeSwitch(on func() bool) mergeRunnerOption {
	return func(r *mergepipeline.Runner) {
		before := r.StillAllowed
		r.StillAllowed = func(ctx context.Context) error {
			if !on() {
				return fmt.Errorf("merge run switch: %s", strings.TrimSuffix(config.MergeDeployRunRefusal, "."))
			}
			if before != nil {
				return before(ctx)
			}
			return nil
		}
	}
}
