// The merge conflict model's call: one named registry entry, called directly.
// It does not go through the planner, which would wrap the prompt in planning
// text and could answer with the planning model instead of the named one.

package main

import (
	"context"
	"errors"
	"fmt"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
)

type mergeEntryCall = func(ctx context.Context, backend, model, prompt string) (string, error)

// mergeConflictCaller sends a conflict prompt to one named registry entry: a
// session-style entry as a one-shot session on that entry and its model,
// any other through direct. An entry that is unknown or disabled is refused;
// nothing else ever answers in its place.
func mergeConflictCaller(reg *inference.Registry, direct func(ctx context.Context, entry, prompt string) (string, error), session mergeEntryCall) func(context.Context, string, string) (string, error) {
	return func(ctx context.Context, entry, prompt string) (string, error) {
		llm, err := reg.Get(entry)
		if err != nil {
			return "", fmt.Errorf("model entry %q: %w", entry, err)
		}
		if llm.Disabled {
			return "", fmt.Errorf("model entry %q is disabled", entry)
		}
		if inference.IsSessionBackendKind(llm.Kind) {
			return session(ctx, entry, llm.Model, prompt)
		}
		return direct(ctx, entry, prompt)
	}
}

// wireMergeConflictModel gives merge runs the model registry and the call to
// the named conflict model. Without a registry the model policy stops and
// asks.
func wireMergeConflictModel(amgr *autonomouspkg.Manager, cfg *config.Config, reg *inference.Registry, disp *inference.Dispatcher) {
	if amgr == nil || cfg == nil || reg == nil {
		return
	}
	direct := func(ctx context.Context, entry, prompt string) (string, error) {
		if disp == nil {
			return "", errors.New("no model dispatcher is available")
		}
		resp, err := disp.Call(ctx, entry, inference.Request{Prompt: prompt, Consumer: "ask"})
		return resp.Text, err
	}
	amgr.SetMergeConflictModel(reg, mergeConflictCaller(reg, direct, supervisorLoopSessionFn(cfg)))
}
