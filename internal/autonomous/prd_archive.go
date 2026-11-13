// Archive of finished Automata.
//
// A finished Automaton (completed, shipped or cancelled and older than the
// retention) is still read in full at every start. The archive moves the bulky
// parts of its record into one archive file and leaves a small live record:
//
//	<dir>/archive/<name>.json   the parts moved out (same name rule as records)
//
// What moves: the decision log, every task's session history (history notes,
// session decisions, attempts, nudge, delivery and verifier events, infra
// retries, start-failure tail, runner evidence), the error text of a task, the
// raw judge text of every verdict and verification, and the generated memory
// report. What stays: identity, status, timestamps, counts, shipped state,
// tags, links, stories and tasks with their ids, titles, statuses and short
// fields, plus the `archive` marker that names the archive file.
//
// Nothing is deleted. The live record plus the archive file hold everything
// the original held, and RestoreFromArchive puts it back byte for byte. The
// marker also carries the judge session ids that were in the decision log, so
// the summary index and cost roll-up read the same ids as before.
//
// Every file is written to a temp file, fsynced and renamed. The archive file
// is written and verified before the live record is rewritten, so a crash
// leaves each record at its old or new generation and always readable.

package autonomous

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	archiveDirName = "archive"
	// archiveFormatVersion is the version of the archive file layout.
	archiveFormatVersion = 1
	// archiveErrorFloor is the length above which a task's error text moves
	// to the archive; shorter errors stay on the live record.
	archiveErrorFloor = 1 << 10
)

// ErrNotArchived is returned when an Automaton has no archive.
var ErrNotArchived = errors.New("automaton is not archived")

// ArchiveRef is the marker a live record carries once its bulky parts moved.
type ArchiveRef struct {
	Archived bool `json:"archived"`
	// File is the archive file, relative to the store directory.
	File string `json:"file"`
	// SHA256 is the hash of the archive file's bytes.
	SHA256     string    `json:"sha256"`
	Bytes      int64     `json:"bytes"`
	ArchivedAt time.Time `json:"archived_at"`
	// DecisionCount is how many decision-log entries moved, and
	// DecisionCostUSD the cost they carried, so list rows read the same
	// count and cost as before.
	DecisionCount   int     `json:"decision_count,omitempty"`
	DecisionCostUSD float64 `json:"decision_cost_usd,omitempty"`
	// JudgeSessionIDs are the judge session ids of the moved decision log, in
	// order, so the summary index and cost roll-up read the same ids.
	JudgeSessionIDs []string `json:"judge_session_ids,omitempty"`
}

// archivedTaskParts is what moved out of one task.
type archivedTaskParts struct {
	StoryIndex int    `json:"story_index"`
	TaskIndex  int    `json:"task_index"`
	TaskID     string `json:"task_id"`

	Error                  string                  `json:"error,omitempty"`
	VerificationRaw        string                  `json:"verification_raw_judge,omitempty"`
	VerdictRaw             []string                `json:"verdict_raw_judge,omitempty"`
	History                []TaskHistoryEntry      `json:"history,omitempty"`
	SessionDecisions       []SessionDecision       `json:"session_decisions,omitempty"`
	Attempts               []TaskAttempt           `json:"attempts,omitempty"`
	NudgeEvents            []NudgeEvent            `json:"nudge_events,omitempty"`
	DeliveryFailureEvents  []DeliveryFailureEvent  `json:"delivery_failure_events,omitempty"`
	VerifierIncidentEvents []VerifierIncidentEvent `json:"verifier_incident_events,omitempty"`
	InfraRetries           []InfraRetryRecord      `json:"infra_retries,omitempty"`
	StartFailureTail       []string                `json:"start_failure_tail,omitempty"`
	RunnerEvidence         *RunnerEvidence         `json:"runner_evidence,omitempty"`
}

func (t archivedTaskParts) empty() bool {
	return t.Error == "" && t.VerificationRaw == "" && len(t.VerdictRaw) == 0 &&
		len(t.History) == 0 && len(t.SessionDecisions) == 0 && len(t.Attempts) == 0 &&
		len(t.NudgeEvents) == 0 && len(t.DeliveryFailureEvents) == 0 &&
		len(t.VerifierIncidentEvents) == 0 && len(t.InfraRetries) == 0 &&
		len(t.StartFailureTail) == 0 && t.RunnerEvidence == nil
}

// archivedStoryParts is the raw judge text of one story's verdicts.
type archivedStoryParts struct {
	StoryIndex int      `json:"story_index"`
	VerdictRaw []string `json:"verdict_raw_judge"`
}

// PRDArchive is the content of one archive file.
type PRDArchive struct {
	Version      int                  `json:"version"`
	ID           string               `json:"id"`
	Decisions    []Decision           `json:"decisions,omitempty"`
	MemoryReport string               `json:"memory_report,omitempty"`
	VerdictRaw   []string             `json:"verdict_raw_judge,omitempty"`
	Stories      []archivedStoryParts `json:"stories,omitempty"`
	Tasks        []archivedTaskParts  `json:"tasks,omitempty"`
}

// takeRawJudge blanks RawJudge on every verdict and returns the texts by
// index, or nil when none had any.
func takeRawJudge(vs []GuardrailVerdict) []string {
	found := false
	for i := range vs {
		if vs[i].RawJudge != "" {
			found = true
		}
	}
	if !found {
		return nil
	}
	raws := make([]string, len(vs))
	for i := range vs {
		raws[i] = vs[i].RawJudge
		vs[i].RawJudge = ""
	}
	return raws
}

func putRawJudge(vs []GuardrailVerdict, raws []string) error {
	if len(raws) == 0 {
		return nil
	}
	if len(raws) != len(vs) {
		return fmt.Errorf("archive holds %d raw judge texts for %d verdicts", len(raws), len(vs))
	}
	for i := range vs {
		if raws[i] != "" {
			vs[i].RawJudge = raws[i]
		}
	}
	return nil
}

// extractArchiveParts moves the bulky parts out of p, in place, and returns
// them. p keeps its identity, status, counts and short fields.
func extractArchiveParts(p *PRD) *PRDArchive {
	a := &PRDArchive{Version: archiveFormatVersion, ID: p.ID}
	a.Decisions = p.Decisions
	p.Decisions = nil
	a.MemoryReport = p.MemoryReport
	p.MemoryReport = ""
	a.VerdictRaw = takeRawJudge(p.Verdicts)
	for si := range p.Story {
		st := &p.Story[si]
		if raws := takeRawJudge(st.Verdicts); raws != nil {
			a.Stories = append(a.Stories, archivedStoryParts{StoryIndex: si, VerdictRaw: raws})
		}
		for ti := range st.Tasks {
			t := &st.Tasks[ti]
			part := archivedTaskParts{StoryIndex: si, TaskIndex: ti, TaskID: t.ID}
			if len(t.Error) > archiveErrorFloor {
				part.Error, t.Error = t.Error, ""
			}
			if t.Verification != nil && t.Verification.RawJudge != "" {
				part.VerificationRaw, t.Verification.RawJudge = t.Verification.RawJudge, ""
			}
			part.VerdictRaw = takeRawJudge(t.Verdicts)
			part.History, t.History = t.History, nil
			part.SessionDecisions, t.SessionDecisions = t.SessionDecisions, nil
			part.Attempts, t.Attempts = t.Attempts, nil
			part.NudgeEvents, t.NudgeEvents = t.NudgeEvents, nil
			part.DeliveryFailureEvents, t.DeliveryFailureEvents = t.DeliveryFailureEvents, nil
			part.VerifierIncidentEvents, t.VerifierIncidentEvents = t.VerifierIncidentEvents, nil
			part.InfraRetries, t.InfraRetries = t.InfraRetries, nil
			part.StartFailureTail, t.StartFailureTail = t.StartFailureTail, nil
			part.RunnerEvidence, t.RunnerEvidence = t.RunnerEvidence, nil
			if !part.empty() {
				a.Tasks = append(a.Tasks, part)
			}
		}
	}
	return a
}

// restoreArchiveParts puts the parts back into p, in place. It refuses an
// archive that does not fit the record, and changes nothing in that case.
func restoreArchiveParts(p *PRD, a *PRDArchive) error {
	if a == nil {
		return errors.New("archive is empty")
	}
	if a.ID != p.ID {
		return fmt.Errorf("archive belongs to %q, not %q", a.ID, p.ID)
	}
	if a.Version != archiveFormatVersion {
		return fmt.Errorf("archive version %d is not supported", a.Version)
	}
	// Check the fit first so a failure leaves p untouched.
	if len(a.VerdictRaw) != 0 && len(a.VerdictRaw) != len(p.Verdicts) {
		return fmt.Errorf("archive holds %d raw judge texts for %d verdicts", len(a.VerdictRaw), len(p.Verdicts))
	}
	for _, s := range a.Stories {
		if s.StoryIndex < 0 || s.StoryIndex >= len(p.Story) || len(s.VerdictRaw) != len(p.Story[s.StoryIndex].Verdicts) {
			return fmt.Errorf("archive story %d does not fit the record", s.StoryIndex)
		}
	}
	for _, part := range a.Tasks {
		if part.StoryIndex < 0 || part.StoryIndex >= len(p.Story) ||
			part.TaskIndex < 0 || part.TaskIndex >= len(p.Story[part.StoryIndex].Tasks) {
			return fmt.Errorf("archive task %s does not fit the record", part.TaskID)
		}
		t := &p.Story[part.StoryIndex].Tasks[part.TaskIndex]
		if t.ID != part.TaskID {
			return fmt.Errorf("archive task %s sits where the record has %s", part.TaskID, t.ID)
		}
		if len(part.VerdictRaw) != 0 && len(part.VerdictRaw) != len(t.Verdicts) {
			return fmt.Errorf("archive task %s holds %d raw judge texts for %d verdicts", part.TaskID, len(part.VerdictRaw), len(t.Verdicts))
		}
		if part.VerificationRaw != "" && t.Verification == nil {
			return fmt.Errorf("archive task %s holds a verification text but the record has no verification", part.TaskID)
		}
	}
	// Archived entries are older than anything the live record gained since,
	// so they go first; text fields keep the live value when it has one.
	p.Decisions = append(append([]Decision(nil), a.Decisions...), p.Decisions...)
	if a.MemoryReport != "" && p.MemoryReport == "" {
		p.MemoryReport = a.MemoryReport
	}
	_ = putRawJudge(p.Verdicts, a.VerdictRaw)
	for _, s := range a.Stories {
		_ = putRawJudge(p.Story[s.StoryIndex].Verdicts, s.VerdictRaw)
	}
	for _, part := range a.Tasks {
		t := &p.Story[part.StoryIndex].Tasks[part.TaskIndex]
		if part.Error != "" && t.Error == "" {
			t.Error = part.Error
		}
		if part.VerificationRaw != "" && t.Verification.RawJudge == "" {
			t.Verification.RawJudge = part.VerificationRaw
		}
		_ = putRawJudge(t.Verdicts, part.VerdictRaw)
		if len(part.History) > 0 {
			t.History = append(append([]TaskHistoryEntry(nil), part.History...), t.History...)
		}
		if len(part.SessionDecisions) > 0 {
			t.SessionDecisions = append(append([]SessionDecision(nil), part.SessionDecisions...), t.SessionDecisions...)
		}
		if len(part.Attempts) > 0 {
			t.Attempts = append(append([]TaskAttempt(nil), part.Attempts...), t.Attempts...)
		}
		if len(part.NudgeEvents) > 0 {
			t.NudgeEvents = append(append([]NudgeEvent(nil), part.NudgeEvents...), t.NudgeEvents...)
		}
		if len(part.DeliveryFailureEvents) > 0 {
			t.DeliveryFailureEvents = append(append([]DeliveryFailureEvent(nil), part.DeliveryFailureEvents...), t.DeliveryFailureEvents...)
		}
		if len(part.VerifierIncidentEvents) > 0 {
			t.VerifierIncidentEvents = append(append([]VerifierIncidentEvent(nil), part.VerifierIncidentEvents...), t.VerifierIncidentEvents...)
		}
		if len(part.InfraRetries) > 0 {
			t.InfraRetries = append(append([]InfraRetryRecord(nil), part.InfraRetries...), t.InfraRetries...)
		}
		if len(part.StartFailureTail) > 0 {
			t.StartFailureTail = append(append([]string(nil), part.StartFailureTail...), t.StartFailureTail...)
		}
		if part.RunnerEvidence != nil && t.RunnerEvidence == nil {
			t.RunnerEvidence = part.RunnerEvidence
		}
	}
	return nil
}

// archiveJudgeIDs lists the judge session ids of a decision log, in order and
// without repeats.
func archiveJudgeIDs(ds []Decision) []string {
	var set idSet
	for i := range ds {
		set.add(ds[i].JudgeSessionID)
	}
	return set.ids
}

// archiveDecisionCost sums the cost of a decision log in log order.
func archiveDecisionCost(ds []Decision) float64 {
	var c float64
	for i := range ds {
		c += ds[i].CostUSD
	}
	return c
}

// archivedDecisionCount is how many decision-log entries moved to the archive.
func (p *PRD) archivedDecisionCount() int {
	if p.Archive == nil {
		return 0
	}
	return p.Archive.DecisionCount
}

func (s *Store) archiveDir() string { return filepath.Join(s.dir, archiveDirName) }

func (s *Store) archiveFilePath(id string) string {
	return filepath.Join(s.archiveDir(), recordName(id)+".json")
}

func archiveFileRel(id string) string { return archiveDirName + "/" + recordName(id) + ".json" }

// archiveFileHash is the hex SHA-256 of data.
func archiveFileHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// recordSize is the bytes a record occupies on disk: the record and its side
// files.
func (s *Store) recordSize(p *PRD) int64 {
	rec, sides, _, err := s.encodePRDRecord(p)
	if err != nil {
		return 0
	}
	n := int64(len(rec))
	for _, b := range sides {
		n += int64(len(b))
	}
	return n
}

// archiveSplit builds the archive of p on a copy: the archive bytes, the marker
// and the slimmed copy. p is not changed. It proves the split loses nothing by
// merging the parts back into a copy and comparing it with p.
func archiveSplit(p *PRD, now time.Time) (data []byte, ref *ArchiveRef, slim *PRD, err error) {
	if p.Archive != nil {
		return nil, nil, nil, fmt.Errorf("automaton %s is already archived", p.ID)
	}
	want, err := json.Marshal(p)
	if err != nil {
		return nil, nil, nil, err
	}
	slim = deepCopyPRD(p)
	parts := extractArchiveParts(slim)
	data, err = json.Marshal(parts)
	if err != nil {
		return nil, nil, nil, err
	}
	ref = &ArchiveRef{
		Archived:        true,
		File:            archiveFileRel(p.ID),
		SHA256:          archiveFileHash(data),
		Bytes:           int64(len(data)),
		ArchivedAt:      now.UTC(),
		DecisionCount:   len(parts.Decisions),
		DecisionCostUSD: archiveDecisionCost(parts.Decisions),
		JudgeSessionIDs: archiveJudgeIDs(parts.Decisions),
	}
	slim.Archive = ref
	// Merge back into a copy of the slim record, from the decoded bytes, and
	// compare with the original.
	var decoded PRDArchive
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, nil, nil, fmt.Errorf("decode archive: %w", err)
	}
	back := deepCopyPRD(slim)
	back.Archive = nil
	if err := restoreArchiveParts(back, &decoded); err != nil {
		return nil, nil, nil, err
	}
	got, err := json.Marshal(back)
	if err != nil {
		return nil, nil, nil, err
	}
	if !bytes.Equal(got, want) {
		return nil, nil, nil, fmt.Errorf("automaton %s does not survive the archive split", p.ID)
	}
	return data, ref, slim, nil
}

// readArchiveFile reads and checks the archive file a marker names.
func (s *Store) readArchiveFile(id string, ref *ArchiveRef) (*PRDArchive, []byte, error) {
	data, err := os.ReadFile(s.archiveFilePath(id))
	if err != nil {
		return nil, nil, fmt.Errorf("read archive of %s: %w", id, err)
	}
	if got := archiveFileHash(data); got != ref.SHA256 {
		return nil, nil, fmt.Errorf("archive of %s does not match its marker (sha256 %s, want %s)", id, got, ref.SHA256)
	}
	var a PRDArchive
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, nil, fmt.Errorf("decode archive of %s: %w", id, err)
	}
	return &a, data, nil
}

// ReadArchive loads the archive of one Automaton. It reads the file only when
// asked; nothing else in the store touches it.
func (s *Store) ReadArchive(id string) (*PRDArchive, error) {
	s.mu.Lock()
	p, ok := s.prds[id]
	var ref *ArchiveRef
	if ok && p.Archive != nil {
		c := *p.Archive
		ref = &c
	}
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("automaton %s not found", id)
	}
	if ref == nil {
		return nil, ErrNotArchived
	}
	a, _, err := s.readArchiveFile(id, ref)
	return a, err
}

// SetArchiveSessionCheck wires the live-session check the archive refuses on.
func (s *Store) SetArchiveSessionCheck(fn func(sessionID string) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.archiveSessionOpen = fn
}

func taskStatusRunning(st TaskStatus) bool {
	switch st {
	case TaskInProgress, TaskRunningTests, TaskVerifying, TaskQueued,
		TaskWaitingCapacity, TaskWaitingDecision, TaskAwaitingApproval, TaskReviewNeeded:
		return true
	}
	return false
}

// archiveEligibility says whether p may be archived now. When it may not, it
// gives the plain reason and whether the cause is use (a running task, an open
// session, an unmerged branch) rather than "not finished" or "too new".
// Caller holds s.mu.
func (s *Store) archiveEligibility(p *PRD, now time.Time, retention time.Duration) (ok bool, why string, inUse bool) {
	if p.Archive != nil {
		return false, "already archived", false
	}
	if p.IsTemplate {
		return false, "template", false
	}
	shipped := strings.TrimSpace(p.MergeCommit) != ""
	finished := p.Status == PRDCompleted || p.Status == PRDCancelled || shipped
	if !finished {
		return false, "not finished (status " + string(p.Status) + ")", false
	}
	if p.UpdatedAt.IsZero() || now.Sub(p.UpdatedAt) < retention {
		return false, "newer than the retention", false
	}
	if p.Status == PRDRunning || p.Status == PRDActive || p.Status == PRDPaused || p.Status == PRDPlanning {
		return false, "Automaton is still running", true
	}
	for si := range p.Story {
		for ti := range p.Story[si].Tasks {
			t := &p.Story[si].Tasks[ti]
			if taskStatusRunning(t.Status) {
				return false, "task " + t.ID + " is " + string(t.Status), true
			}
		}
	}
	if s.archiveSessionOpen != nil {
		if p.PlannerSessionID != "" && s.archiveSessionOpen(p.PlannerSessionID) {
			return false, "planner session " + p.PlannerSessionID + " is open", true
		}
		for si := range p.Story {
			for ti := range p.Story[si].Tasks {
				t := &p.Story[si].Tasks[ti]
				for _, id := range []string{t.SessionID, t.VerifierSessionID} {
					if id != "" && s.archiveSessionOpen(id) {
						return false, "session " + id + " is open", true
					}
				}
			}
		}
	}
	if p.Git.Branch != "" && !shipped && p.Status != PRDCancelled {
		return false, "branch " + p.Git.Branch + " is not merged and the record still needs it", true
	}
	if !s.rec.active {
		return false, "record layout is not active", true
	}
	return true, "", false
}

// ArchiveResult is the outcome of archiving one Automaton.
type ArchiveResult struct {
	ID          string `json:"id"`
	Title       string `json:"title,omitempty"`
	Status      string `json:"status,omitempty"`
	BytesBefore int64  `json:"bytes_before"`
	BytesAfter  int64  `json:"bytes_after"`
	// ArchiveBytes is the size of the archive file written (or that would be).
	ArchiveBytes int64 `json:"archive_bytes"`
}

// stages named for the test hook that simulates an interrupt.
const (
	archiveStageFileWritten  = "archive-file-written"
	archiveStageRecordWriten = "record-rewritten"
)

// ArchiveAutomaton archives one Automaton now. It refuses (and changes
// nothing) when the Automaton is not eligible. The archive file is written and
// verified before the live record is rewritten.
func (s *Store) ArchiveAutomaton(id string, now time.Time, retention time.Duration) (ArchiveResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.archiveAutomatonLocked(id, now, retention)
}

func (s *Store) archiveAutomatonLocked(id string, now time.Time, retention time.Duration) (ArchiveResult, error) {
	p, ok := s.prds[id]
	if !ok {
		return ArchiveResult{}, fmt.Errorf("automaton %s not found", id)
	}
	if okAr, why, _ := s.archiveEligibility(p, now, retention); !okAr {
		return ArchiveResult{}, fmt.Errorf("automaton %s is not archived: %s", id, why)
	}
	res := ArchiveResult{ID: id, Title: p.Title, Status: string(p.Status), BytesBefore: s.recordSize(p)}
	data, ref, slim, err := archiveSplit(p, now)
	if err != nil {
		return res, err
	}
	res.ArchiveBytes = int64(len(data))
	res.BytesAfter = s.recordSize(slim)

	if err := os.MkdirAll(s.archiveDir(), 0o755); err != nil {
		return res, err
	}
	if err := atomicWriteFile(s.archiveFilePath(id), data); err != nil {
		return res, err
	}
	// Read the file back: the marker must match what is on disk.
	if _, _, err := s.readArchiveFile(id, ref); err != nil {
		return res, err
	}
	if h := s.archiveHook; h != nil {
		if err := h(archiveStageFileWritten); err != nil {
			return res, err
		}
	}
	// Rewrite the live record from the slimmed copy. The in-memory record is
	// changed in place so every pointer into its stories and tasks stays valid.
	saved := deepCopyPRD(p)
	extractArchiveParts(p)
	p.Archive = ref
	if err := s.writePRDRecord(p); err != nil {
		// Put the memory back; the old record on disk is untouched.
		*p = *saved
		s.readoptPointersLocked(p)
		return res, fmt.Errorf("rewrite record of %s: %w", id, err)
	}
	s.refreshSummaryKeepRevLocked(id)
	if h := s.archiveHook; h != nil {
		if err := h(archiveStageRecordWriten); err != nil {
			return res, err
		}
	}
	return res, nil
}

// readoptPointersLocked points the story and task maps at p's current slices.
func (s *Store) readoptPointersLocked(p *PRD) {
	for i := range p.Story {
		st := &p.Story[i]
		s.stories[st.ID] = st
		for j := range st.Tasks {
			s.tasks[st.Tasks[j].ID] = &st.Tasks[j]
		}
	}
}

// refreshSummaryKeepRevLocked rebuilds one summary entry without bumping the
// revision: archiving does not change what the index says.
func (s *Store) refreshSummaryKeepRevLocked(id string) {
	if s.sumIdx == nil {
		s.sumIdx = map[string]SummaryEntry{}
	}
	if p, ok := s.prds[id]; ok && p != nil {
		s.sumIdx[id] = buildSummaryEntry(p)
	}
}

// RestoreFromArchive puts the archived parts back into the live record and
// removes the archive file once the record is written.
func (s *Store) RestoreFromArchive(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.prds[id]
	if !ok {
		return fmt.Errorf("automaton %s not found", id)
	}
	if p.Archive == nil {
		return ErrNotArchived
	}
	a, _, err := s.readArchiveFile(id, p.Archive)
	if err != nil {
		return err
	}
	saved := deepCopyPRD(p)
	if err := restoreArchiveParts(p, a); err != nil {
		return err
	}
	p.Archive = nil
	if err := s.writePRDRecord(p); err != nil {
		*p = *saved
		s.readoptPointersLocked(p)
		return fmt.Errorf("rewrite record of %s: %w", id, err)
	}
	s.refreshSummaryKeepRevLocked(id)
	// The record is whole again. The archive file is set aside under a new
	// name, never deleted.
	path := s.archiveFilePath(id)
	if err := os.Rename(path, uniqueName(path+".restored")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
