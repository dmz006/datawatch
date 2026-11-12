// Repo CI watch record — the single record one failing (or otherwise
// watched) CI run at a repository's head of main or a tag carries while
// it waits for operator attention.
//
// Unlike the Automaton triage record (PRD.Triage) this record is a
// SEPARATE store entry, not a field on the PRD: a head-of-main or tag
// failure has no owning Automaton to hang a record on, so the records
// live in their own list — loaded at startup, saved on change, keyed by
// the identity of the watched run (see RepoCIWatchStore).
package autonomous

import (
	"fmt"
	"strings"
	"time"
)

// RepoCIWatchStatuses is the record's status vocabulary. A watched
// run starts "pending" while it executes, lands in "failing" when a
// check fails, "hung" when it stops progressing, "rerunning" while an
// automatic rerun is out, "green" when the run finally passes, and
// "superseded" when a newer run for the same ref replaces it.
var RepoCIWatchStatuses = []string{"green", "failing", "pending", "hung", "rerunning", "superseded"}

// RepoCIWatchClassifications is the record's resolution vocabulary:
// what the failure turned out to be. "none" is the unclassified value
// the builder assigns by default.
var RepoCIWatchClassifications = []string{"code_or_test", "infra", "hung", "security", "policy", "judging", "none"}

// RepoCIWatchRerun is one entry in the record's rerun log — one per
// automatic cancel or rerun the watch performed on the run.
type RepoCIWatchRerun struct {
	// At is when the automatic cancel or rerun happened.
	At time.Time `json:"at"`
	// Action is what the watch did: "cancel" or "rerun".
	Action string `json:"action"`
	// RunID is the run ID the action targeted (the rerun's ID for a
	// "rerun", the cancelled run's ID for a "cancel").
	RunID string `json:"run_id"`
}

// RepoCIWatchWouldDo is one shadow-mode entry: what the watch would
// have done to the run had automatic cancels/reruns not been in
// shadow mode.
type RepoCIWatchWouldDo struct {
	// Action is what the watch would have done: "cancel" or "rerun".
	Action string `json:"action"`
	// RunID is the run ID the action would have targeted.
	RunID string `json:"run_id"`
	// Reason is the shadow-mode explanation for the would-be action.
	Reason string `json:"reason,omitempty"`
}

// RepoCIWatchSupersededRun names a run this record superseded, with
// its run URL kept for the operator's reference.
type RepoCIWatchSupersededRun struct {
	RunID  string `json:"run_id"`
	RunURL string `json:"run_url,omitempty"`
}

// RepoCIWatchJudgeVerdict is the judge's verdict for one run, cached
// per run ID so a later refresh of the record does not re-judge.
type RepoCIWatchJudgeVerdict struct {
	RunID   string `json:"run_id"`
	Verdict string `json:"verdict,omitempty"`
}

// RepoCIWatchRecord is the single record for one watched CI run. All
// fields are additive and JSON-optional wherever a value may be
// absent; the record is one line of a standalone JSONL list (see
// RepoCIWatchStore), so a repository with a clean head carries none.
type RepoCIWatchRecord struct {
	// Repo is the repository path (e.g. "dmz006/datawatch").
	Repo string `json:"repo"`
	// RefKind is the kind of ref being watched: "main" or "tag".
	RefKind string `json:"ref_kind"`
	// RefName is the ref's name (the branch name or the tag).
	RefName string `json:"ref_name"`
	// SHA is the commit the run executed against.
	SHA string `json:"sha"`
	// Workflow is the workflow file name the run belongs to.
	Workflow string `json:"workflow"`
	// RunID is the CI run ID the record is keyed on.
	RunID  string `json:"run_id"`
	RunURL string `json:"run_url,omitempty"`
	// Status is the run's watch state (see RepoCIWatchStatuses).
	Status string `json:"status"`
	// Classification is the failure's resolved category (see
	// RepoCIWatchClassifications); "none" until one is assigned.
	Classification string `json:"classification,omitempty"`
	// FirstSeenAt is when the run was first observed under watch; it is
	// preserved across refreshes.
	FirstSeenAt time.Time `json:"first_seen_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	// AlertedAt is nil until a later step marks the record alerted.
	AlertedAt *time.Time `json:"alerted_at,omitempty"`
	// RerunLog has one entry per automatic cancel or rerun performed on
	// the run.
	RerunLog []RepoCIWatchRerun `json:"rerun_log,omitempty"`
	// WouldDo is the shadow-mode log: what the watch would have done,
	// had it not been shadowed.
	WouldDo []RepoCIWatchWouldDo `json:"would_do,omitempty"`
	// SupersededRunIDs is the run's superseded predecessors, each with
	// its run URL.
	SupersededRunIDs []RepoCIWatchSupersededRun `json:"superseded_run_ids,omitempty"`
	// Notes is the append-only annotation list later steps write to —
	// the same shape as TriageNote.
	Notes []TriageNote `json:"notes,omitempty"`
	// JudgeVerdict is the judge's verdict cached for this record's run.
	JudgeVerdict *RepoCIWatchJudgeVerdict `json:"judge_verdict,omitempty"`
	// Security is the security failure report of this run, set once by
	// the security failure pass (security_failure_pass.go). A record that
	// carries one is never assessed again; every refresh keeps it.
	Security *SecurityFailureReport `json:"security,omitempty"`
}

// RepoCIWatchKey composes the composite key identifying one record:
// repository path, ref kind, ref name, commit sha, workflow name and
// run ID. The unit-separator byte cannot appear in any field, so the
// join is lossless and no two distinct runs collide.
func RepoCIWatchKey(repo, refKind, refName, sha, workflow, runID string) string {
	return strings.Join([]string{repo, refKind, refName, sha, workflow, runID}, "\x1f")
}

// RepoCIWatchKeyOf returns the composite key for rec's identity fields.
func (rec *RepoCIWatchRecord) Key() string {
	return RepoCIWatchKey(rec.Repo, rec.RefKind, rec.RefName, rec.SHA, rec.Workflow, rec.RunID)
}

// ValidRepoCIWatchStatus reports whether status is a known record
// status.
func ValidRepoCIWatchStatus(status string) bool {
	return repoCIWatchInList(RepoCIWatchStatuses, status)
}

// ValidRepoCIWatchClassification reports whether c is a known classif-
// ication (or the empty string, which the builder fills with "none").
func ValidRepoCIWatchClassification(c string) bool {
	return c == "" || repoCIWatchInList(RepoCIWatchClassifications, c)
}

// BuildRepoCIWatchRecord is the pure builder: it validates its inputs
// (no store, no I/O) and assembles a fresh record at now, with
// classification defaulted to "none" when left empty and everything
// else left for subsequent lifecycle steps. A refresh of an existing
// record supplies the same inputs with the stored FirstSeenAt; the
// builder only mints the first one.
func BuildRepoCIWatchRecord(repo, refKind, refName, sha, workflow, runID, runURL, status, classification string, now time.Time) (*RepoCIWatchRecord, error) {
	required := map[string]string{
		"repo":     repo,
		"ref_kind": refKind,
		"ref_name": refName,
		"sha":      sha,
		"workflow": workflow,
		"run_id":   runID,
	}
	for name, v := range required {
		if strings.TrimSpace(v) == "" {
			return nil, fmt.Errorf("repo ci watch: %s is required", name)
		}
	}
	if refKind != "main" && refKind != "tag" {
		return nil, fmt.Errorf("repo ci watch: ref kind must be \"main\" or \"tag\", got %q", refKind)
	}
	if !ValidRepoCIWatchStatus(status) {
		return nil, fmt.Errorf("repo ci watch: status must be one of %s, got %q", strings.Join(RepoCIWatchStatuses, ", "), status)
	}
	if !ValidRepoCIWatchClassification(classification) {
		return nil, fmt.Errorf("repo ci watch: classification must be one of %s, got %q", strings.Join(RepoCIWatchClassifications, ", "), classification)
	}
	rec := &RepoCIWatchRecord{
		Repo:        repo,
		RefKind:     refKind,
		RefName:     refName,
		SHA:         sha,
		Workflow:    workflow,
		RunID:       runID,
		RunURL:      runURL,
		Status:      status,
		FirstSeenAt: now,
		UpdatedAt:   now,
	}
	if classification == "" {
		classification = "none"
	}
	rec.Classification = classification
	return rec, nil
}

// repoCIWatchInList reports whether v is one of list's entries.
func repoCIWatchInList(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
