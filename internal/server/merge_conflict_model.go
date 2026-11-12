// Merge conflict model: the adapter that lets the merge pipeline ask a named
// LLM registry entry to propose a conflict resolution. It reuses the planning
// model invocation the autonomous merge resolver already uses
// (autonomous.Manager.ProposeMergeConflict); it adds no client of its own.
package server

import (
	"context"

	"github.com/dmz006/datawatch/internal/mergepipeline"
)

// mergeConflictProposer is the optional Manager surface that sends a conflict
// prompt to a named registry model. The merge run route type-asserts for it,
// so a mergeRunner without it simply gets no conflict model.
type mergeConflictProposer interface {
	ProposeMergeConflict(prompt, model string) (string, error)
}

// conflictModelAdapter implements mergepipeline.ConflictModel by sending each
// prompt to one named registry entry through propose.
type conflictModelAdapter struct {
	name    string
	propose func(prompt, model string) (string, error)
}

// Propose implements mergepipeline.ConflictModel. The reply is returned as the
// model sent it; parsing and gating happen in the merge pipeline.
func (a conflictModelAdapter) Propose(ctx context.Context, prompt string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return a.propose(prompt, a.name)
}

// newConflictModel returns the adapter for the registry entry name, or nil
// when name is empty (no model) or the Manager cannot propose.
func newConflictModel(mr mergeRunner, name string) mergepipeline.ConflictModel {
	if name == "" {
		return nil
	}
	p, ok := mr.(mergeConflictProposer)
	if !ok {
		return nil
	}
	return conflictModelAdapter{name: name, propose: p.ProposeMergeConflict}
}
