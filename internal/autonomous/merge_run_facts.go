// What the merge pipeline asks the Manager for: the release one Automaton's
// merge makes, the model that proposes conflict resolutions and whether the
// operator has paused the repository.
package autonomous

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// MergeReleaseFacts is what a merge run needs to know about its Automaton.
type MergeReleaseFacts struct {
	// Title is the first line of the Automaton's title.
	Title string
	// Size is patch, minor, major or none.
	Size string
	// RaisesMajorApproval says the release goes through the major approval
	// mode whatever its size (a breaking change).
	RaisesMajorApproval bool
}

// MergeReleaseFacts resolves the release one Automaton's merge makes, from
// its own release size, then its kind through kindMap, then the built-in
// sizes. false means there is no such Automaton.
func (m *Manager) MergeReleaseFacts(id string, kindMap map[string]string, majorTrigger string) (MergeReleaseFacts, bool) {
	p, ok := m.store.GetPRD(id)
	if !ok {
		return MergeReleaseFacts{}, false
	}
	var f MergeReleaseFacts
	m.readLocked(func() {
		res := ResolveReleaseSize(ReleaseSizeInput{
			ReleaseSize: p.ReleaseSize, BreakingChange: p.BreakingChange, Kind: EffectiveKind(p, nil),
			KindMap: kindMap, MajorTrigger: majorTrigger,
		})
		title, _, _ := strings.Cut(strings.TrimSpace(p.Title), "\n")
		f = MergeReleaseFacts{Title: strings.TrimSpace(title), Size: res.Size, RaisesMajorApproval: res.RaisesMajorApproval}
	})
	return f, true
}

// mergeConflictTimeout bounds the one call to the conflict model.
const mergeConflictTimeout = 30 * time.Minute

type mergeModelWiring struct {
	lookup config.LLMLookup
	call   func(ctx context.Context, entry, prompt string) (string, error)
}

// SetMergeConflictModel wires the model registry and the direct call to one
// named entry that merge conflicts use.
func (m *Manager) SetMergeConflictModel(lookup config.LLMLookup, call func(ctx context.Context, entry, prompt string) (string, error)) {
	m.mergeModel.Store(&mergeModelWiring{lookup: lookup, call: call})
}

// MergeModelLookup is the model registry a merge run checks its conflict
// model against; nil when none is wired.
func (m *Manager) MergeModelLookup() config.LLMLookup {
	if w := m.mergeModel.Load(); w != nil {
		return w.lookup
	}
	return nil
}

// ProposeMergeConflict sends a conflict prompt, unchanged, to the registry
// entry named entry and returns the raw reply. It never goes through the
// planner: another model, or planning text around the prompt, would not be
// the answer the setting names.
func (m *Manager) ProposeMergeConflict(prompt, entry string) (string, error) {
	w := m.mergeModel.Load()
	if w == nil || w.call == nil {
		return "", errors.New("no conflict model call is wired")
	}
	ctx, cancel := context.WithTimeout(context.Background(), mergeConflictTimeout)
	defer cancel()
	out, err := w.call(ctx, entry, prompt)
	if err != nil {
		return "", fmt.Errorf("conflict model %q: %w", entry, err)
	}
	return out, nil
}

// MergePauseMessage reports an operator pause on the repository merges go
// into.
func (m *Manager) MergePauseMessage() (string, bool) {
	b, blocked := m.pauseBlock(m.MergeBaseRepo())
	if !blocked {
		return "", false
	}
	return b.Message(), true
}
