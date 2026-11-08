// Append-only, persistent store for decision records. It is written the same
// way QuestionStore is: one JSON-lines file is the whole store, every change
// is one new line, the file is never truncated or rewritten, and the current
// view is rebuilt by replay on open. A half-written last line (a crash in the
// middle of a write) is skipped on open and repaired by the next append. The
// store holds no network or tmux logic; it only keeps records.
package autonomous

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Decision record list limits: the default when the caller gives none and the
// most the store returns.
const (
	defaultDecisionListLimit = 100
	maxDecisionListLimit     = 1000
)

// Line types, one per JSON line of the store file.
const (
	decisionLineCreated      = "decision_created"
	decisionLineUpdated      = "decision_updated"
	decisionLineWithdrawn    = "decision_withdrawn"
	decisionLineQuestionLink = "decision_question_linked"
	decisionLineAnswered     = "decision_answered"
	decisionLineDocChange    = "decision_doc_change"
	decisionLineFollowUp     = "decision_follow_up"
	decisionLineCrashResidue = "crash_residue"
)

// decisionStoreLine is one line of the store file.
type decisionStoreLine struct {
	Type       string             `json:"type"`
	ID         string             `json:"id,omitempty"`
	Record     *DecisionRecord    `json:"record,omitempty"`
	QuestionID string             `json:"question_id,omitempty"`
	Answer     *DecisionAnswer    `json:"answer,omitempty"`
	DocChange  *DecisionDocChange `json:"doc_change,omitempty"`
	FollowUp   *DecisionFollowUp  `json:"follow_up,omitempty"`
	Actor      string             `json:"actor,omitempty"`
	Reason     string             `json:"reason,omitempty"`
	At         *time.Time         `json:"at,omitempty"`
	// DiscardedBytes is set on a crash_residue line: the byte length of the
	// abandoned fragment on the line before it.
	DiscardedBytes int `json:"discarded_bytes,omitempty"`
}

// DecisionRecordFilter narrows List. Every field is optional. AutomatonID
// matches the record's automaton link exactly; Limit defaults to 100 and is
// capped at 1000.
type DecisionRecordFilter struct {
	State       DecisionRecordState
	AutomatonID string
	Limit       int
}

// DecisionRecordStore holds the decision records, backed by one append-only
// JSON-lines file. Every method is safe for concurrent use.
type DecisionRecordStore struct {
	mu   sync.Mutex
	path string

	now   func() time.Time
	newID func() string

	records    map[string]*DecisionRecord // id -> current view
	order      []string                   // ids in creation order, oldest first
	bySource   map[string]string          // source + ref -> id
	byQuestion map[string]string          // question id -> id

	// writeFn writes one chunk to the open append file; a test replaces it
	// to simulate a write that fails part way.
	writeFn func(f *os.File, b []byte) (int, error)

	// objectTagDeriver, when wired (Manager.SetDecisionRecords does),
	// replaces a record's SystemTags with the derived list on Create and
	// Update (see ObjectTagDeriver). A nil deriver leaves SystemTags
	// untouched.
	objectTagDeriver ObjectTagDeriver
}

// NewDecisionRecordStore opens (or starts, when the file is missing) the
// store at path and replays its lines. An unparsable line is an error that
// names the line number, except an unterminated final line, which is skipped.
func NewDecisionRecordStore(path string) (*DecisionRecordStore, error) {
	s := &DecisionRecordStore{
		path:       path,
		now:        time.Now,
		newID:      NewDecisionRecordID,
		writeFn:    func(f *os.File, b []byte) (int, error) { return f.Write(b) },
		records:    make(map[string]*DecisionRecord),
		bySource:   make(map[string]string),
		byQuestion: make(map[string]string),
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("read decision record store %s: %w", path, err)
	}
	if err := s.replay(data); err != nil {
		return nil, err
	}
	return s, nil
}

// replay rebuilds the view from the file bytes. Called only from
// NewDecisionRecordStore, before the store is shared.
func (s *DecisionRecordStore) replay(data []byte) error {
	terminated := len(data) > 0 && data[len(data)-1] == '\n'
	lines := strings.Split(string(data), "\n")
	if terminated {
		lines = lines[:len(lines)-1]
	}
	for i, text := range lines {
		if text == "" {
			continue
		}
		line, err := parseDecisionLine(text)
		if err != nil {
			if i == len(lines)-1 && !terminated {
				// A crash mid-write left an unterminated final line: skip it;
				// the next append repairs the framing.
				continue
			}
			if i+1 < len(lines) {
				if next, nerr := parseDecisionLine(lines[i+1]); nerr == nil &&
					next.Type == decisionLineCrashResidue && next.DiscardedBytes == len(text) {
					continue
				}
			}
			return fmt.Errorf("decision record store %s: line %d is not a valid record: %w", s.path, i+1, err)
		}
		s.applyLine(line)
	}
	return nil
}

// parseDecisionLine parses and shape-checks one file line.
func parseDecisionLine(text string) (decisionStoreLine, error) {
	var l decisionStoreLine
	if err := json.Unmarshal([]byte(text), &l); err != nil {
		return l, err
	}
	switch l.Type {
	case decisionLineCreated, decisionLineUpdated:
		if l.Record == nil || l.Record.ID == "" {
			return l, fmt.Errorf("%s line is missing the record", l.Type)
		}
	case decisionLineWithdrawn:
		if l.ID == "" {
			return l, errors.New("decision_withdrawn line is missing id")
		}
	case decisionLineQuestionLink:
		if l.ID == "" || l.QuestionID == "" {
			return l, errors.New("decision_question_linked line is missing id or question_id")
		}
	case decisionLineAnswered:
		if l.ID == "" || l.Answer == nil || l.Answer.AnswerID == "" {
			return l, errors.New("decision_answered line is missing id or answer")
		}
	case decisionLineDocChange:
		if l.ID == "" || l.DocChange == nil {
			return l, errors.New("decision_doc_change line is missing id or doc_change")
		}
	case decisionLineFollowUp:
		if l.ID == "" || l.FollowUp == nil {
			return l, errors.New("decision_follow_up line is missing id or follow_up")
		}
	case decisionLineCrashResidue:
		if l.DiscardedBytes <= 0 {
			return l, errors.New("crash_residue line is missing discarded_bytes")
		}
	default:
		return l, errors.New("unknown record type")
	}
	return l, nil
}

func decisionSourceKey(source, ref string) string { return source + "\x00" + ref }

// applyLine folds one line into the view. It is the only place the view
// changes, for replay and for live writes alike, so the two cannot differ.
func (s *DecisionRecordStore) applyLine(l decisionStoreLine) {
	switch l.Type {
	case decisionLineCreated:
		r := l.Record.clone()
		s.records[r.ID] = &r
		s.order = append(s.order, r.ID)
		if r.Source != "" && r.SourceRef != "" {
			s.bySource[decisionSourceKey(r.Source, r.SourceRef)] = r.ID
		}
		if r.QuestionID != "" {
			s.byQuestion[r.QuestionID] = r.ID
		}
		return
	}
	r := s.records[l.ID]
	if l.Type == decisionLineUpdated {
		r = s.records[l.Record.ID]
	}
	if r == nil {
		return
	}
	switch l.Type {
	case decisionLineUpdated:
		c := l.Record.clone()
		r.Label, r.Title, r.Situation, r.Deciding = c.Label, c.Title, c.Situation, c.Deciding
		r.Options, r.Recommendation, r.Default, r.Links = c.Options, c.Recommendation, c.Default, c.Links
		r.ContentSHA256 = c.ContentSHA256
	case decisionLineWithdrawn:
		r.State = DecisionRecordWithdrawn
	case decisionLineQuestionLink:
		r.QuestionID = l.QuestionID
		s.byQuestion[l.QuestionID] = r.ID
	case decisionLineAnswered:
		a := *l.Answer
		a.AnsweredAt = cloneTime(l.Answer.AnsweredAt)
		r.Answer = &a
		r.State = DecisionRecordAnswered
	case decisionLineDocChange:
		d := *l.DocChange
		d.At = cloneTime(l.DocChange.At)
		r.DocChange = &d
	case decisionLineFollowUp:
		f := *l.FollowUp
		f.At = cloneTime(l.FollowUp.At)
		r.FollowUp = &f
	default:
		return
	}
	r.UpdatedAt = cloneTime(l.At)
}

// commitLocked appends the line and, only when that succeeded, folds it into
// the view. The caller holds s.mu.
func (s *DecisionRecordStore) commitLocked(l decisionStoreLine) error {
	if err := s.appendLocked(l); err != nil {
		return err
	}
	s.applyLine(l)
	return nil
}

// appendLocked appends one line and syncs it. The file is only ever
// extended: when a crash left it without a trailing newline, the append first
// closes that line, and if the leftover is an unparsable fragment it also
// writes a crash_residue marker so replay skips exactly that fragment.
func (s *DecisionRecordStore) appendLocked(l decisionStoreLine) error {
	data, err := json.Marshal(l)
	if err != nil {
		return fmt.Errorf("encode decision record store line: %w", err)
	}
	prefix, err := s.tailRepair()
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open decision record store %s for append: %w", s.path, err)
	}
	// One write for the repair and the line, so a failure leaves at most one
	// more unterminated tail, which the next append repairs the same way.
	chunk := append(prefix, data...)
	chunk = append(chunk, '\n')
	if _, err := s.writeFn(f, chunk); err != nil {
		_ = f.Close()
		return fmt.Errorf("append decision record store line: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync decision record store %s: %w", s.path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close decision record store %s: %w", s.path, err)
	}
	return nil
}

// tailRepair returns the bytes the next append must write before its own
// line: nothing when the file is missing, empty or ends with a newline;
// otherwise a newline, plus a crash_residue marker when the unterminated tail
// is not a valid line.
func (s *DecisionRecordStore) tailRepair() ([]byte, error) {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read decision record store %s: %w", s.path, err)
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("stat decision record store %s: %w", s.path, err)
	}
	if fi.Size() == 0 {
		_ = f.Close()
		return nil, nil
	}
	last := make([]byte, 1)
	if _, err := f.ReadAt(last, fi.Size()-1); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("read decision record store %s: %w", s.path, err)
	}
	_ = f.Close()
	if last[0] == '\n' {
		return nil, nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("read decision record store %s: %w", s.path, err)
	}
	tail := data[strings.LastIndexByte(string(data), '\n')+1:]
	prefix := []byte{'\n'}
	if _, perr := parseDecisionLine(string(tail)); perr != nil {
		marker, merr := json.Marshal(decisionStoreLine{Type: decisionLineCrashResidue, DiscardedBytes: len(tail)})
		if merr != nil {
			return nil, fmt.Errorf("encode decision record store line: %w", merr)
		}
		prefix = append(prefix, marker...)
		prefix = append(prefix, '\n')
	}
	return prefix, nil
}

// contentOf copies only the content fields of in into a fresh record, so
// bookkeeping the caller sent (id, state, hash, answer) is never trusted.
func contentOf(in DecisionRecord) DecisionRecord {
	c := DecisionRecord{
		Label:     in.Label,
		Title:     in.Title,
		Situation: in.Situation,
		Deciding:  in.Deciding,
		Default:   in.Default,
		Links:     in.Links,
	}
	if in.Options != nil {
		c.Options = append([]DecisionOption(nil), in.Options...)
	}
	if in.Recommendation != nil {
		rec := *in.Recommendation
		c.Recommendation = &rec
	}
	return c
}

// Create validates the record, assigns its id, timestamps and content hash,
// sets the state to open, appends it and stores it. Only the content fields,
// source and source_ref are taken from the input. A second record with the
// same non-empty source and source_ref is refused with
// ErrDecisionRecordDuplicate (whatever state the first one is in).
func (s *DecisionRecordStore) Create(in DecisionRecord) (DecisionRecord, error) {
	r := contentOf(in)
	r.Source, r.SourceRef = in.Source, in.SourceRef
	if err := ValidateNewDecisionRecord(&r); err != nil {
		return DecisionRecord{}, err
	}
	if d := s.objectTagDeriver; d != nil {
		// Object system tags: the full derived list replaces the stored
		// one (no-op when equal) before the write is committed.
		r.SystemTags = replaceSystemTags(r.SystemTags, d.ObjectTagsForDecisionRecord(&r))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Source != "" && r.SourceRef != "" {
		if id, ok := s.bySource[decisionSourceKey(r.Source, r.SourceRef)]; ok {
			return DecisionRecord{}, fmt.Errorf("%w: %s %q is held by %s", ErrDecisionRecordDuplicate, r.Source, r.SourceRef, id)
		}
	}
	id := ""
	for try := 0; try < 8; try++ {
		if cand := s.newID(); cand != "" && s.records[cand] == nil {
			id = cand
			break
		}
	}
	if id == "" {
		return DecisionRecord{}, errors.New("could not draw an unused decision record id")
	}
	now := s.now()
	r.ID = id
	r.State = DecisionRecordOpen
	r.ContentSHA256 = r.ContentHash()
	r.CreatedAt, r.UpdatedAt = &now, &now
	if err := s.commitLocked(decisionStoreLine{Type: decisionLineCreated, Record: &r, At: &now}); err != nil {
		return DecisionRecord{}, err
	}
	return s.records[id].clone(), nil
}

// Update replaces the content fields of an open record with those of in,
// refreshing the hash and updated_at. Source, source_ref and the question id
// are not touched. An answered or withdrawn record is refused with
// ErrDecisionRecordClosed.
func (s *DecisionRecordStore) Update(id string, in DecisionRecord) (DecisionRecord, error) {
	c := contentOf(in)
	if err := ValidateNewDecisionRecord(&c); err != nil {
		return DecisionRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.openRecordLocked(id)
	if err != nil {
		return DecisionRecord{}, err
	}
	c.ID = cur.ID
	c.ContentSHA256 = c.ContentHash()
	if d := s.objectTagDeriver; d != nil {
		// Re-derive on every update: the full derived list replaces the
		// stored one (no-op when equal) before the write is committed.
		c.SystemTags = replaceSystemTags(c.SystemTags, d.ObjectTagsForDecisionRecord(&c))
	}
	now := s.now()
	if err := s.commitLocked(decisionStoreLine{Type: decisionLineUpdated, Record: &c, At: &now}); err != nil {
		return DecisionRecord{}, err
	}
	return s.records[id].clone(), nil
}

// SetObjectTagDeriver wires object-derived system tags into Create and
// Update: the record's SystemTags are replaced with the derived list
// before the write (see ObjectTagDeriver and replaceSystemTags).
func (s *DecisionRecordStore) SetObjectTagDeriver(d ObjectTagDeriver) {
	s.objectTagDeriver = d
}

// openRecordLocked returns the record when it exists and is open.
func (s *DecisionRecordStore) openRecordLocked(id string) (*DecisionRecord, error) {
	cur, ok := s.records[id]
	if !ok {
		return nil, fmt.Errorf("decision record %s: %w", id, ErrDecisionRecordNotFound)
	}
	if cur.State != DecisionRecordOpen {
		return nil, fmt.Errorf("decision record %s is %s: %w", id, cur.State, ErrDecisionRecordClosed)
	}
	return cur, nil
}

// Withdraw marks an open record withdrawn. Withdrawing a record that is
// already withdrawn changes nothing; an answered record is refused with
// ErrDecisionRecordClosed. Actor and reason are kept on the stored line.
func (s *DecisionRecordStore) Withdraw(id, actor, reason string) (DecisionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.records[id]
	if !ok {
		return DecisionRecord{}, fmt.Errorf("decision record %s: %w", id, ErrDecisionRecordNotFound)
	}
	if cur.State == DecisionRecordWithdrawn {
		return cur.clone(), nil
	}
	if _, err := s.openRecordLocked(id); err != nil {
		return DecisionRecord{}, err
	}
	now := s.now()
	if err := s.commitLocked(decisionStoreLine{Type: decisionLineWithdrawn, ID: id, Actor: actor, Reason: reason, At: &now}); err != nil {
		return DecisionRecord{}, err
	}
	return s.records[id].clone(), nil
}

// SetQuestionID links the record to its queue question. It is set once:
// the same id again changes nothing, a different id is refused with
// ErrDecisionQuestionIDSet, and so is an id another record already holds.
func (s *DecisionRecordStore) SetQuestionID(id, questionID string) (DecisionRecord, error) {
	if strings.TrimSpace(questionID) == "" {
		return DecisionRecord{}, fmt.Errorf("%w: question id is empty", ErrInvalidDecisionRecord)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.records[id]
	if !ok {
		return DecisionRecord{}, fmt.Errorf("decision record %s: %w", id, ErrDecisionRecordNotFound)
	}
	if cur.QuestionID == questionID {
		return cur.clone(), nil
	}
	if cur.QuestionID != "" {
		return DecisionRecord{}, fmt.Errorf("%w: %s already has question %s", ErrDecisionQuestionIDSet, id, cur.QuestionID)
	}
	if other, held := s.byQuestion[questionID]; held {
		return DecisionRecord{}, fmt.Errorf("%w: question %s belongs to %s", ErrDecisionQuestionIDSet, questionID, other)
	}
	now := s.now()
	if err := s.commitLocked(decisionStoreLine{Type: decisionLineQuestionLink, ID: id, QuestionID: questionID, At: &now}); err != nil {
		return DecisionRecord{}, err
	}
	return s.records[id].clone(), nil
}

// RecordAnswer stores the answer and moves the record to answered. The same
// answer id again changes nothing. A different answer id on an answered
// record replaces the stored answer (a changed mind supersedes, as it does in
// the question queue; the earlier answer stays in the file). A withdrawn
// record is refused with ErrDecisionRecordClosed.
func (s *DecisionRecordStore) RecordAnswer(id string, a DecisionAnswer) (DecisionRecord, error) {
	switch {
	case strings.TrimSpace(a.AnswerID) == "":
		return DecisionRecord{}, fmt.Errorf("%w: answer_id is required", ErrInvalidDecisionRecord)
	case a.ChosenOptionID == "" && strings.TrimSpace(a.Quote) == "":
		return DecisionRecord{}, fmt.Errorf("%w: an answer needs a chosen option or a quote", ErrInvalidDecisionRecord)
	case !utf8.ValidString(a.Quote):
		return DecisionRecord{}, fmt.Errorf("%w: it cannot be stored exactly as received", ErrQuoteNotUTF8)
	case len(a.Quote) > answerQuoteMaxBytes:
		return DecisionRecord{}, fmt.Errorf("%w: answer quote is %d bytes; the cap is %d", ErrInvalidDecisionRecord, len(a.Quote), answerQuoteMaxBytes)
	case len(a.ChosenOptionID) > decisionOptionIDMaxBytes || len(a.Actor) > decisionShortFieldMax || len(a.Surface) > decisionShortFieldMax:
		return DecisionRecord{}, fmt.Errorf("%w: an answer field is over its size cap", ErrInvalidDecisionRecord)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.records[id]
	if !ok {
		return DecisionRecord{}, fmt.Errorf("decision record %s: %w", id, ErrDecisionRecordNotFound)
	}
	if cur.State == DecisionRecordWithdrawn {
		return DecisionRecord{}, fmt.Errorf("decision record %s is %s: %w", id, cur.State, ErrDecisionRecordClosed)
	}
	if cur.Answer != nil && cur.Answer.AnswerID == a.AnswerID {
		return cur.clone(), nil
	}
	now := s.now()
	if a.AnsweredAt == nil {
		a.AnsweredAt = &now
	}
	if err := s.commitLocked(decisionStoreLine{Type: decisionLineAnswered, ID: id, Answer: &a, At: &now}); err != nil {
		return DecisionRecord{}, err
	}
	return s.records[id].clone(), nil
}

// RecordDocChange stores the outcome of the document change that followed
// the answer, replacing an earlier one. Status must be one of the DecisionDoc
// constants.
func (s *DecisionRecordStore) RecordDocChange(id string, d DecisionDocChange) (DecisionRecord, error) {
	switch d.Status {
	case DecisionDocReported, DecisionDocApplied, DecisionDocUnchanged, DecisionDocNoMatch, DecisionDocAmbiguous, DecisionDocError:
	default:
		return DecisionRecord{}, fmt.Errorf("%w: doc_change status is %q; it must be one of %s, %s, %s, %s, %s or %s", ErrInvalidDecisionRecord, d.Status,
			DecisionDocReported, DecisionDocApplied, DecisionDocUnchanged, DecisionDocNoMatch, DecisionDocAmbiguous, DecisionDocError)
	}
	if d.Path != "" {
		if err := checkDecisionDocPath(d.Path); err != nil {
			return DecisionRecord{}, fmt.Errorf("%w: doc_change %v", ErrInvalidDecisionRecord, err)
		}
	}
	if len(d.Mode) > decisionShortFieldMax || len(d.Path) > decisionLinkMaxBytes ||
		len(d.Before) > decisionDocFieldMaxBytes || len(d.After) > decisionDocFieldMaxBytes || len(d.Diff) > decisionDocFieldMaxBytes {
		return DecisionRecord{}, fmt.Errorf("%w: a doc_change field is over its size cap", ErrInvalidDecisionRecord)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[id]; !ok {
		return DecisionRecord{}, fmt.Errorf("decision record %s: %w", id, ErrDecisionRecordNotFound)
	}
	now := s.now()
	if d.At == nil {
		d.At = &now
	}
	if err := s.commitLocked(decisionStoreLine{Type: decisionLineDocChange, ID: id, DocChange: &d, At: &now}); err != nil {
		return DecisionRecord{}, err
	}
	return s.records[id].clone(), nil
}

// RecordFollowUp stores the outcome of the configured follow-up action,
// replacing an earlier one.
func (s *DecisionRecordStore) RecordFollowUp(id string, f DecisionFollowUp) (DecisionRecord, error) {
	if len(f.Kind) > decisionShortFieldMax || len(f.Target) > decisionShortFieldMax ||
		len(f.Status) > decisionShortFieldMax || len(f.Result) > decisionDocFieldMaxBytes {
		return DecisionRecord{}, fmt.Errorf("%w: a follow_up field is over its size cap", ErrInvalidDecisionRecord)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[id]; !ok {
		return DecisionRecord{}, fmt.Errorf("decision record %s: %w", id, ErrDecisionRecordNotFound)
	}
	now := s.now()
	if f.At == nil {
		f.At = &now
	}
	if err := s.commitLocked(decisionStoreLine{Type: decisionLineFollowUp, ID: id, FollowUp: &f, At: &now}); err != nil {
		return DecisionRecord{}, err
	}
	return s.records[id].clone(), nil
}

// Get returns the current view of the record with id.
func (s *DecisionRecordStore) Get(id string) (DecisionRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[id]
	if !ok {
		return DecisionRecord{}, false
	}
	return r.clone(), true
}

// GetByQuestionID returns the record linked to the queue question.
func (s *DecisionRecordStore) GetByQuestionID(questionID string) (DecisionRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.byQuestion[questionID]
	if !ok || questionID == "" {
		return DecisionRecord{}, false
	}
	return s.records[id].clone(), true
}

// GetBySource returns the record that came from the given source and
// source_ref. An empty source or source_ref never matches.
func (s *DecisionRecordStore) GetBySource(source, sourceRef string) (DecisionRecord, bool) {
	if source == "" || sourceRef == "" {
		return DecisionRecord{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.bySource[decisionSourceKey(source, sourceRef)]
	if !ok {
		return DecisionRecord{}, false
	}
	return s.records[id].clone(), true
}

// List returns the matching records, newest first, capped at the filter's
// limit (default 100, maximum 1000).
func (s *DecisionRecordStore) List(f DecisionRecordFilter) []DecisionRecord {
	limit := f.Limit
	if limit <= 0 {
		limit = defaultDecisionListLimit
	}
	if limit > maxDecisionListLimit {
		limit = maxDecisionListLimit
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// A small constant capacity: append grows it, so no caller-supplied limit
	// ever sizes the allocation.
	out := make([]DecisionRecord, 0, 16)
	for i := len(s.order) - 1; i >= 0 && len(out) < limit; i-- {
		r := s.records[s.order[i]]
		if f.State != "" && r.State != f.State {
			continue
		}
		if f.AutomatonID != "" && r.Links.Automaton != f.AutomatonID {
			continue
		}
		out = append(out, r.clone())
	}
	return out
}
