package autonomous

import (
	"context"
	"time"
)

// FinishedWorkPass is the outcome of one sweep pass.
type FinishedWorkPass struct {
	Mode      string                `json:"mode"`
	Examined  int                   `json:"examined"`
	GitChecks int                   `json:"git_checks"`
	Capped    bool                  `json:"capped"`
	Found     int                   `json:"found"`
	Younger   int                   `json:"younger"`
	Asked     int                   `json:"asked"`
	Reasked   int                   `json:"reasked"`
	Digest    bool                  `json:"digest_sent"`
	Findings  []FinishedWorkFinding `json:"findings"`
}

// FinishedWorkAuditResult is the outcome of one audit.
type FinishedWorkAuditResult struct {
	Mode             string                `json:"mode"`
	At               string                `json:"at"`
	Scanned          int                   `json:"scanned"`
	ByCause          map[string]int        `json:"by_cause"`
	Findings         []FinishedWorkFinding `json:"findings"`
	DecisionRecordID string                `json:"decision_record_id,omitempty"`
}

func (m *Manager) RunFinishedWorkSweep(ctx context.Context, now time.Time) FinishedWorkPass {
	return FinishedWorkPass{}
}

func (m *Manager) RunFinishedWorkAudit(ctx context.Context, now time.Time) (FinishedWorkAuditResult, error) {
	return FinishedWorkAuditResult{}, nil
}

func (m *Manager) ArchiveDecided(id, actor, decision, reason string) (*PRD, error) {
	return m.Archive(id, actor)
}

func (a *API) CancelDecided(id, decision, reason string) error { return a.Cancel(id) }

func (l *RefusalLedger) Blocked(automaton, step string, updated, now time.Time, hold time.Duration) bool {
	return false
}

// Finished-work causes, option ids and timings (stubs for the red run).
const (
	finishedWorkAsker         = "finished-work"
	FinishedWorkNotMerged     = "finished_not_merged"
	FinishedWorkCodeDone      = "code_done_rest_pending"
	FinishedWorkReask         = "reask_due"
	FinishedWorkBranchGone    = "branch_gone_unlanded"
	FinishedWorkStuck         = "stuck_no_activity"
	FinishedWorkOptionLand    = "land_by_hand"
	FinishedWorkOptionLeave   = "leave_alone"
	FinishedWorkOptionDrop    = "drop"
	FinishedWorkReaskDays     = 14
	FinishedWorkLandReaskDays = 3
	FinishedWorkSweepLimit    = 100
)

type FinishedWorkDigest struct {
	Waiting []FinishedWorkFinding `json:"waiting"`
}

func (m *Manager) FinishedWorkDigest(now time.Time) FinishedWorkDigest { return FinishedWorkDigest{} }
