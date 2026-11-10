// merge_shadow_compare_hook.go — the post-ship merge-shadow comparison hook.
//
// After RecordAutomationShipped writes an Automaton's shipped record, the
// hook (startMergeShadowCompare) runs the merge-shadow comparison for that
// Automaton in a goroutine, so the ship path never waits on git:
//
//   - it loads the Automaton's stored merge plan (mergeshadow.Load under the
//     manager's data directory). No stored plan means no work at all: no git
//     read, no comparison, no file written.
//   - it reads what landed on the plan's base ref through the landed reader
//     (mergeshadow.ReadLanded), compares it with the plan (mergeshadow.Compare)
//     and stores the differences as defect candidates
//     (mergeshadow.RecordComparisonCandidates).
//
// Candidate storage is idempotent by content, so a repeated run over
// unchanged data adds no duplicate candidates. The hook never fails the
// ship: every error is logged and dropped.
//
// Known limit: the comparison's parity-issue input has no reader yet, so
// the hook passes the plan's own parity_issue_needed value through and that
// field always matches. The gate input is the supervisor's manual record,
// which the hook does not have, so gates count as not compared.
package autonomous

import (
	"context"
	"errors"
	"log"
	"os"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/mergeshadow"
)

// mergeShadowCompareTimeout bounds one background comparison, so a hung git
// cannot hold its goroutine forever.
const mergeShadowCompareTimeout = 2 * time.Minute

// mergeShadowDefaultRef is the base ref used when a stored plan names none.
const mergeShadowDefaultRef = "main"

// mergeShadowLandedReader reads what landed on a ref. It is a variable so
// tests can substitute a fake for the git-backed reader.
var mergeShadowLandedReader = mergeshadow.ReadLanded

// SetMergePipelineConfig sets the merge pipeline settings the landed read
// uses to find version files and the registry file. The daemon calls it at
// startup with the configured merge_pipeline block.
func (m *Manager) SetMergePipelineConfig(c config.MergePipelineConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mergePipeline = c
}

// mergePipelineConfig returns the merge pipeline settings set by
// SetMergePipelineConfig.
func (m *Manager) mergePipelineConfig() config.MergePipelineConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mergePipeline
}

// startMergeShadowCompare runs the merge-shadow comparison for one Automaton
// in the background. It returns at once.
func (m *Manager) startMergeShadowCompare(automatonID string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), mergeShadowCompareTimeout)
		defer cancel()
		created, err := m.runMergeShadowCompare(ctx, automatonID)
		if err != nil {
			log.Printf("[autonomous] merge shadow compare: automaton %s: %v", automatonID, err)
			return
		}
		if len(created) > 0 {
			log.Printf("[autonomous] merge shadow compare: automaton %s: stored %d defect candidate(s)", automatonID, len(created))
		}
	}()
}

// runMergeShadowCompare is the synchronous core of the hook. It returns the
// candidates this call created: nil when the Automaton has no stored plan,
// and an empty slice when every difference was already stored.
func (m *Manager) runMergeShadowCompare(ctx context.Context, automatonID string) ([]mergeshadow.DefectCandidate, error) {
	if m.dataDir == "" {
		return nil, nil
	}
	plan, err := mergeshadow.Load(m.dataDir, automatonID)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil // no stored plan: nothing to compare
	}
	if err != nil {
		return nil, err
	}

	if m.automations == nil {
		return nil, errors.New("automations store not configured")
	}
	auto, ok := m.automations.Get(automatonID)
	if !ok {
		return nil, errors.New("automaton not found")
	}

	ref := plan.BaseRef
	if ref == "" {
		ref = mergeShadowDefaultRef
	}
	query := mergeshadow.LandedQuery{
		Project:     plan.Project,
		AutomatonID: automatonID,
		Version:     plan.PlannedVersion,
		Pipeline:    m.mergePipelineConfig(),
	}
	landed, err := mergeShadowLandedReader(ctx, auto.ProjectDir, ref, query)
	if err != nil {
		return nil, err
	}

	// Parity-issue presence has no reader yet; see the package note above.
	cmp, err := mergeshadow.Compare(plan, landed, plan.ParityIssueNeeded, nil)
	if err != nil {
		return nil, err
	}
	return mergeshadow.RecordComparisonCandidates(m.dataDir, &cmp)
}
