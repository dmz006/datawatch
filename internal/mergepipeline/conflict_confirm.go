package mergepipeline

// Operator confirmation of a project's first model-resolved conflicts.
//
// In on mode, a model proposal for a code or document conflict that passes the
// gate is not applied while the project has fewer than ConfirmFirstN applied
// model resolutions. It is held in the pending store, the merge stays aborted,
// and the operator is asked apply_proposal, resolve_by_hand or
// leave_branch_out. A later merge of the same branch, at the same HEAD and
// branch SHAs, applies the held proposal once the operator answers
// apply_proposal. Every model resolution applied in on mode counts, whether it
// was confirmed or not.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// Operator answers to a confirmation question.
const (
	ConfirmApplyProposal  = "apply_proposal"
	ConfirmResolveByHand  = "resolve_by_hand"
	ConfirmLeaveBranchOut = "leave_branch_out"
)

// ConflictCounterStore is the part of the per-project conflict counter a
// Merger uses. *ConflictCounter is one.
type ConflictCounterStore interface {
	Count(project string) (int, error)
	Increment(project string) (int, error)
}

// confirmationReader reads the operator's answer to a question.
// *autonomous.QuestionStore is one.
type confirmationReader interface {
	Get(id string) (autonomous.Question, bool)
	CurrentAnswer(questionID string) (autonomous.Answer, bool)
}

// PendingConfirmation is a model proposal held until the operator confirms it.
// It is keyed by project, branch, HEAD SHA and branch SHA: a moved branch or
// HEAD is a different key, so the held proposal no longer matches.
type PendingConfirmation struct {
	Project    string            `json:"project"`
	Branch     string            `json:"branch"`
	HeadSHA    string            `json:"head_sha"`
	BranchSHA  string            `json:"branch_sha"`
	Files      []string          `json:"files"`
	Hunks      int               `json:"hunks"`
	Texts      map[string]string `json:"texts"`
	QuestionID string            `json:"question_id"`
	CreatedAt  time.Time         `json:"created_at"`
}

// ConfirmStore holds the pending confirmations. With a path it is kept in a
// JSON file written through a temporary file and renamed; without one it lives
// in memory only. It is safe for concurrent use.
type ConfirmStore struct {
	mu    sync.Mutex
	path  string
	items map[string]PendingConfirmation
}

// NewConfirmStore loads (or starts) the store kept at
// <dataDir>/merge-pipeline/conflict-pending.json. A corrupt file is an error,
// never a silent reset.
func NewConfirmStore(dataDir string) (*ConfirmStore, error) {
	s := &ConfirmStore{
		path:  filepath.Join(dataDir, "merge-pipeline", "conflict-pending.json"),
		items: map[string]PendingConfirmation{},
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// NewMemoryConfirmStore returns a store that is not written anywhere.
func NewMemoryConfirmStore() *ConfirmStore {
	return &ConfirmStore{items: map[string]PendingConfirmation{}}
}

// confirmKey is the store key for a proposal made for these four values.
func confirmKey(project, branch, headSHA, branchSHA string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{project, branch, headSHA, branchSHA}, "\x00")))
	return hex.EncodeToString(sum[:])
}

// Get returns the pending confirmation stored under key.
func (s *ConfirmStore) Get(key string) (PendingConfirmation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.items[key]
	return p, ok
}

// Put stores p under key and persists the store.
func (s *ConfirmStore) Put(key string, p PendingConfirmation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, had := s.items[key]
	s.items[key] = p
	if err := s.persist(); err != nil {
		s.restore(key, prev, had)
		return err
	}
	return nil
}

// Remove deletes the pending confirmation stored under key and persists the store.
func (s *ConfirmStore) Remove(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, had := s.items[key]
	if !had {
		return nil
	}
	delete(s.items, key)
	if err := s.persist(); err != nil {
		s.restore(key, prev, had)
		return err
	}
	return nil
}

// Prune drops every pending confirmation for project and branch except the
// one under keep. A branch that moved, or a newer proposal for it, makes the
// older ones stale.
func (s *ConfirmStore) Prune(project, branch, keep string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := map[string]PendingConfirmation{}
	for k, p := range s.items {
		if k != keep && p.Project == project && p.Branch == branch {
			removed[k] = p
			delete(s.items, k)
		}
	}
	if len(removed) == 0 {
		return nil
	}
	if err := s.persist(); err != nil {
		for k, p := range removed {
			s.items[k] = p
		}
		return err
	}
	return nil
}

// restore puts an entry back the way it was before a failed write.
func (s *ConfirmStore) restore(key string, prev PendingConfirmation, had bool) {
	if had {
		s.items[key] = prev
	} else {
		delete(s.items, key)
	}
}

func (s *ConfirmStore) persist() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.items, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *ConfirmStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var m map[string]PendingConfirmation
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("corrupt pending confirmation file %s: %w", s.path, err)
	}
	for k, p := range m {
		s.items[k] = p
	}
	return nil
}

// confirmStore returns the store the pending confirmations live in. A Merger
// with none wired keeps them in memory.
func (m *Merger) confirmStore() *ConfirmStore {
	if m.Pending == nil {
		m.Pending = NewMemoryConfirmStore()
	}
	return m.Pending
}

// projectName is the key the counter and the pending confirmations use.
func (m *Merger) projectName() string {
	if strings.TrimSpace(m.Project) != "" {
		return m.Project
	}
	return filepath.Base(m.Dir)
}

// touchesCodeOrDocument reports whether a conflicted file is a code or a
// document file. Only those are confirmed and counted.
func (m *Merger) touchesCodeOrDocument(files []string) bool {
	for _, f := range files {
		switch ClassifyConflictFile(f, m.VersionFiles) {
		case ClassCode, ClassDocument:
			return true
		}
	}
	return false
}

// confirmationNeeded reports whether a passing proposal for files must wait for
// the operator: the confirm count is above zero, a file is code or document, and
// the project has fewer applied model resolutions than the count. A count that
// cannot be read is taken as zero, so the operator is asked rather than the
// proposal applied unseen.
func (m *Merger) confirmationNeeded(set ConflictSettings, files []string) bool {
	if set.ConfirmFirstN <= 0 || !m.touchesCodeOrDocument(files) {
		return false
	}
	count := 0
	if m.Counter != nil {
		n, err := m.Counter.Count(m.projectName())
		if err != nil {
			log.Printf("[merge-pipeline] read the conflict count for %s: %v", m.projectName(), err)
		} else {
			count = n
		}
	}
	return count < set.ConfirmFirstN
}

// recordApplied counts one model-resolved code or document conflict applied in
// on mode. A failed count is logged; the change is already committed.
func (m *Merger) recordApplied(files []string) {
	if m.Counter == nil || !m.touchesCodeOrDocument(files) {
		return
	}
	if _, err := m.Counter.Increment(m.projectName()); err != nil {
		log.Printf("[merge-pipeline] count an applied conflict for %s: %v", m.projectName(), err)
	}
}

// holdForConfirmation raises the confirmation question for a proposal that
// passed the gate and stores it as pending. The merge is already aborted.
func (m *Merger) holdForConfirmation(req MergeRequest, out Outcome, prop *modelProposal, headSHA, branchSHA string) (Outcome, error) {
	out.Status = StatusAsked
	out.Summary = conflictSummary(out)
	q, err := m.askConfirmation(req, out, headSHA, branchSHA)
	out.QuestionID = q.ID
	if err != nil {
		return out, err
	}
	p := PendingConfirmation{
		Project:    m.projectName(),
		Branch:     req.Branch,
		HeadSHA:    headSHA,
		BranchSHA:  branchSHA,
		Files:      out.Files,
		Hunks:      prop.Hunks,
		Texts:      prop.Texts,
		QuestionID: q.ID,
		CreatedAt:  time.Now().UTC(),
	}
	if err := m.confirmStore().Put(confirmKey(p.Project, p.Branch, headSHA, branchSHA), p); err != nil {
		return out, fmt.Errorf("store the pending confirmation: %w", err)
	}
	return out, nil
}

// askConfirmation raises the operator question for a held proposal. Its source
// line is distinct from the one for an unsettled conflict, so the two never
// dedupe into each other.
func (m *Merger) askConfirmation(req MergeRequest, o Outcome, headSHA, branchSHA string) (autonomous.Question, error) {
	if m.Questions == nil {
		return autonomous.Question{}, ErrNoQuestionChannel
	}
	sum := sha256.Sum256([]byte("merge-confirm\x00" + req.Branch + "\x00" + headSHA + "\x00" + branchSHA))
	text := fmt.Sprintf("The conflict model's proposal for %s passed the project's test step. It is held and not applied until you confirm it, because the project's first model-resolved code or document conflicts need your confirmation.\n\n%s",
		strings.Join(o.Files, ", "), o.Summary)
	if len(text) > 7000 {
		text = text[:7000] + "\n[shortened]"
	}
	q := autonomous.Question{
		Label:     "decision",
		AskedBy:   ConflictQuestionAsker,
		AskerKind: autonomous.QuestionAskerSystem,
		Context:   autonomous.QuestionContextNone,
		Text:      text + "\nApply the proposal?",
		Options: []autonomous.QuestionOption{
			{ID: ConfirmApplyProposal, Label: "Apply the proposal",
				Tradeoffs: "The proposal is committed and the branch is merged; the project's count of model resolutions goes up by one."},
			{ID: ConfirmResolveByHand, Label: "Resolve it by hand",
				Tradeoffs: "You merge the branch yourself and settle the conflict; the pipeline then continues."},
			{ID: ConfirmLeaveBranchOut, Label: "Leave this branch out",
				Tradeoffs: "The branch is not merged in this run."},
		},
		Default:          ConfirmResolveByHand,
		SourceLineSHA256: hex.EncodeToString(sum[:]),
	}
	if req.AutomatonID != "" {
		q.Context = autonomous.QuestionContextPrd
		q.PRDID = req.AutomatonID
	}
	got, _, err := m.Questions.RaiseUnlessOpenSourceLine(q)
	if err != nil {
		return autonomous.Question{}, fmt.Errorf("raise the operator question: %w", err)
	}
	return got, nil
}

// confirmationAnswer returns the option the operator chose for a confirmation
// question, or "" when it is unanswered or cannot be read.
func (m *Merger) confirmationAnswer(questionID string) string {
	if questionID == "" {
		return ""
	}
	r, ok := m.Questions.(confirmationReader)
	if !ok {
		return ""
	}
	a, ok := r.CurrentAnswer(questionID)
	if !ok {
		return ""
	}
	return a.ChosenOptionID
}

// applyConfirmedProposal runs before the merge. It drops pending confirmations
// for this branch that no longer match, then applies the held proposal when the
// operator answered apply_proposal. handled is false when the merge should run
// as usual. An answer of resolve_by_hand or leave_branch_out ends the held
// proposal, so the merge runs as usual and may ask again.
func (m *Merger) applyConfirmedProposal(ctx context.Context, set ConflictSettings, req MergeRequest, headSHA, branchSHA, msg string) (Outcome, bool, error) {
	out := Outcome{Policy: string(set.Policy), Branch: req.Branch}
	store := m.confirmStore()
	project := m.projectName()
	key := confirmKey(project, req.Branch, headSHA, branchSHA)
	if err := store.Prune(project, req.Branch, key); err != nil {
		return out, false, err
	}
	p, ok := store.Get(key)
	if !ok {
		return out, false, nil
	}
	switch m.confirmationAnswer(p.QuestionID) {
	case ConfirmApplyProposal:
	case "":
		return out, false, nil
	default:
		return out, false, store.Remove(key)
	}

	att := m.applyStoredProposal(ctx, req, msg, p)
	att.Mode = set.Mode
	if err := store.Remove(key); err != nil {
		log.Printf("[merge-pipeline] clear the confirmed proposal for %s: %v", req.Branch, err)
	}
	out.Attempt = &att
	out.Files = p.Files
	if att.Result == AttemptAccepted {
		out.Status = StatusResolvedByModel
		out.Summary = fmt.Sprintf("merged %s; the conflict model's proposal for %d hunk(s), confirmed by the operator, passed the project's test step and was committed",
			req.Branch, p.Hunks)
		m.recordApplied(p.Files)
		return out, true, nil
	}
	out.Status = StatusAsked
	out.Summary = conflictSummary(out)
	q, err := m.askOperator(req, out, headSHA, branchSHA)
	out.QuestionID = q.ID
	return out, true, err
}

// applyStoredProposal merges the branch into the project again, on the same
// HEAD and branch the proposal was made for, and writes the stored texts. The
// test step ran on the same tree when the proposal was made, so it is not run
// again. On any failure the merge is aborted and the attempt says why.
func (m *Merger) applyStoredProposal(ctx context.Context, req MergeRequest, msg string, p PendingConfirmation) ModelAttempt {
	fail := func(result, detail string) ModelAttempt {
		m.abortQuietly(m.Dir)
		return ModelAttempt{Result: result, Detail: detail, Hunks: p.Hunks}
	}
	r, err := m.git(ctx, m.Dir, "merge", "--no-ff", "--no-commit", req.Branch)
	if err != nil {
		return fail(AttemptInfrastructure, err.Error())
	}
	if r.Code == 0 {
		return fail(AttemptApplyFailed, "the merge no longer conflicts")
	}
	files, err := m.unmergedFiles(ctx, m.Dir)
	if err != nil {
		return fail(AttemptApplyFailed, err.Error())
	}
	want := make([]string, 0, len(p.Texts))
	for f := range p.Texts {
		want = append(want, f)
	}
	sort.Strings(want)
	sort.Strings(files)
	if strings.Join(files, "\x00") != strings.Join(want, "\x00") {
		return fail(AttemptApplyFailed, "the conflicted files differ from the stored proposal")
	}
	if err := m.commitResolved(ctx, m.Dir, p.Texts, msg); err != nil {
		return fail(AttemptCommitFailed, err.Error())
	}
	return ModelAttempt{Result: AttemptAccepted, Hunks: p.Hunks}
}
