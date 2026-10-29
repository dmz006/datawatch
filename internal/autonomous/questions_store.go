// Append-only, persistent question and answer store. One JSON-lines file is
// the whole store: every change is one new line, and the current view is
// rebuilt by replay on open. The file is append-only — it is never truncated,
// rewritten, renamed or deleted, because answered questions and their
// quotations are decisions of record, kept forever, and answers are never
// edited (a changed mind is a new answer line that supersedes the old one).
// Design: docs/plans/2026-10-25-operator-decision-channel-proposal.md
// (sections 3, "Storage and persistence") and the operator decisions in
// docs/plans/2026-10-14-automation-orchestration-design.md ("Operator
// decision channel", items 2, 13 and 14: no expiry, kept forever, answers
// superseded and never edited).
package autonomous

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/dmz006/datawatch/internal/audit"
)

// ErrQuestionNotFound is returned when an operation names a question id the
// store does not hold.
var ErrQuestionNotFound = errors.New("question not found")

// Question list limits: the default when the caller does not ask for a
// count, and the maximum the store will return.
const (
	defaultQuestionListLimit = 100
	maxQuestionListLimit     = 1000
)

// Record types, one per JSON line in the store file.
const (
	storeLineQuestionRaised    = "question_raised"
	storeLineAnswerRecorded    = "answer_recorded"
	storeLineQuestionWithdrawn = "question_withdrawn"
	storeLineQuestionExpired   = "question_expired"
	// storeLineQuestionEventEmitted marks that the lifecycle event for one
	// answer (its answer_id) or one withdrawal (answer_id empty) of the
	// question was enqueued into the event queue: the daemon writes it
	// after the emit, and replay records it, so a later
	// ReconcileEmittedEvents run cannot emit the same event twice. Older
	// stores (or a crash between the answer and the marker) simply lack
	// the line; that is exactly what the startup reconcile repairs.
	storeLineQuestionEventEmitted = "question_event_emitted"
	// storeLineCrashResidue marks the line just before it as the abandoned
	// fragment of a write that never finished (a crash or a failed partial
	// write). The file is append-only, so the fragment cannot be cut away;
	// instead the next append closes it with a newline and follows it with
	// this marker, and replay then skips exactly that fragment.
	storeLineCrashResidue = "crash_residue"
)

// ErrQuoteNotUTF8 is returned when an answer quote is not valid UTF-8. The
// store keeps quotes exactly as received, and the JSON encoding of a line
// would silently rewrite invalid bytes, so such a quote is refused instead
// of being stored altered.
var ErrQuoteNotUTF8 = errors.New("answer quote is not valid UTF-8")

// questionStoreRecord is one line of the store file. A raised question or a
// recorded answer carries the full record; a withdrawal or an expiry carries
// the question id, actor, reason and the time.
type questionStoreRecord struct {
	Type       string     `json:"type"`
	Question   *Question  `json:"question,omitempty"`
	Answer     *Answer    `json:"answer,omitempty"`
	QuestionID string     `json:"question_id,omitempty"`
	Actor      string     `json:"actor,omitempty"`
	Reason     string     `json:"reason,omitempty"`
	At         *time.Time `json:"at,omitempty"`
	// DiscardedBytes is set on a crash_residue line: the byte length of the
	// abandoned fragment on the line before it.
	DiscardedBytes int `json:"discarded_bytes,omitempty"`
	// AnswerID and EventKind are set on a question_event_emitted line: the
	// answer the question.answered event named (empty for
	// question.withdrawn) and the event kind.
	AnswerID  string `json:"answer_id,omitempty"`
	EventKind string `json:"event_kind,omitempty"`
}

// QuestionStore holds the raised questions and their answers, backed by one
// append-only JSON-lines file. Every method is safe for concurrent use; one
// mutex guards the in-memory view and the file appends.
type QuestionStore struct {
	mu sync.Mutex

	path string

	auditFn       func(action string, details map[string]any)
	now           func() time.Time
	newQuestionID func(time.Time) string
	newAnswerID   func(time.Time) string

	questions map[string]*Question // id -> current view
	answers   map[string]*Answer   // id -> the stored, immutable record
	answerIDs map[string][]string  // question id -> answer ids, oldest first

	// eventEmitted records the question_event_emitted markers: keyed by
	// questionEventKey (question, kind, answer). Replay sets it for stored
	// markers; MarkEventEmitted sets it for ones this instance wrote.
	eventEmitted map[string]bool
	// withdrawMeta remembers the actor and reason each question was
	// withdrawn with, so its question.withdrawn event names them even when
	// it is rebuilt at a later reconcile.
	withdrawMeta map[string]questionWithdrawMeta

	// eventSink, when non-nil, receives the question's lifecycle event on
	// answer and withdrawal (question_events.go); nil emits nothing.
	eventSink QuestionEventSink

	// changeHook, when non-nil, is told of each successful Raise, Answer,
	// Withdraw and Expire after the store lock is released (SetChangeHook).
	changeHook func(QuestionChange)

	// writeFn writes one chunk to the open append file; a test replaces it
	// to simulate a write that fails part way.
	writeFn func(f *os.File, b []byte) (int, error)
}

// questionWithdrawMeta is the (actor, reason) of a question's withdrawal.
type questionWithdrawMeta struct {
	actor  string
	reason string
}

// NewQuestionStore opens (or starts, when the file is missing) the
// append-only store at path, replaying the lines to rebuild the current
// view. A missing file is an empty store. An unparsable line is an error
// that names the line number, except an unterminated final line — a crash
// mid-write — which is skipped; the next append starts with a newline so the
// two never merge.
func NewQuestionStore(path string) (*QuestionStore, error) {
	s := &QuestionStore{
		path:          path,
		now:           time.Now,
		newQuestionID: defaultNewQuestionID,
		newAnswerID:   defaultNewAnswerID,
		writeFn:       func(f *os.File, b []byte) (int, error) { return f.Write(b) },
		questions:     make(map[string]*Question),
		answers:       make(map[string]*Answer),
		answerIDs:     make(map[string][]string),
		eventEmitted:  make(map[string]bool),
		withdrawMeta:  make(map[string]questionWithdrawMeta),
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("read question store %s: %w", path, err)
	}
	if err := s.replay(data); err != nil {
		return nil, err
	}
	return s, nil
}

// SetAuditFn wires the audit callback used after each successful append (the
// same convention as the manager's audit wiring). The callback is invoked
// outside the store lock, with the matching audit action and the Details
// keys documented on the audit constants; a nil fn disables it.
func (s *QuestionStore) SetAuditFn(fn func(action string, details map[string]any)) {
	s.mu.Lock()
	s.auditFn = fn
	s.mu.Unlock()
}

// Question change kinds reported to the change hook.
const (
	QuestionChangeRaised    = "raised"
	QuestionChangeAnswered  = "answered"
	QuestionChangeWithdrawn = "withdrawn"
	QuestionChangeExpired   = "expired"
)

// QuestionChange describes one successful change to a question. Kind is one
// of the QuestionChange constants and Question is the question after the
// change. Answer is the recorded answer for answered and nil otherwise.
// Actor is who made a withdrawal and Reason why it was made or the question
// expired; both are empty for the other kinds.
type QuestionChange struct {
	Kind     string
	Question Question
	Answer   *Answer
	Actor    string
	Reason   string
}

// SetChangeHook wires a callback invoked after each successful Raise, Answer,
// Withdraw and Expire, once the record is durable. It is called outside the
// store lock, so it may call back into the store; a nil fn disables it.
func (s *QuestionStore) SetChangeHook(fn func(QuestionChange)) {
	s.mu.Lock()
	s.changeHook = fn
	s.mu.Unlock()
}

// emitChange invokes the change hook outside the store lock.
func (s *QuestionStore) emitChange(c QuestionChange) {
	s.mu.Lock()
	fn := s.changeHook
	s.mu.Unlock()
	if fn != nil {
		fn(c)
	}
}

// replay rebuilds the in-memory view from the file bytes. It is called only
// from NewQuestionStore, before the store is shared.
func (s *QuestionStore) replay(data []byte) error {
	terminated := len(data) > 0 && data[len(data)-1] == '\n'
	lines := strings.Split(string(data), "\n")
	if terminated {
		lines = lines[:len(lines)-1]
	}
	for i, line := range lines {
		if line == "" {
			continue
		}
		rec, err := parseStoreRecord(line)
		if err != nil {
			if i == len(lines)-1 && !terminated {
				// A crash mid-write left an unterminated final line: skip
				// it, then repair the framing on the next append.
				continue
			}
			// An abandoned fragment that a later append closed off is
			// followed by a crash_residue marker naming its exact length;
			// that and nothing else makes a bad line acceptable.
			if i+1 < len(lines) {
				if next, nerr := parseStoreRecord(lines[i+1]); nerr == nil &&
					next.Type == storeLineCrashResidue && next.DiscardedBytes == len(line) {
					continue
				}
			}
			return fmt.Errorf("question store %s: line %d is not a valid record: %w", s.path, i+1, err)
		}
		s.applyRecord(rec)
	}
	return nil
}

// parseStoreRecord parses and shape-checks one file line.
func parseStoreRecord(line string) (questionStoreRecord, error) {
	var rec questionStoreRecord
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		return rec, err
	}
	switch rec.Type {
	case storeLineQuestionRaised:
		if rec.Question == nil || rec.Question.ID == "" {
			return rec, errors.New("question_raised line is missing the question")
		}
	case storeLineAnswerRecorded:
		if rec.Answer == nil || rec.Answer.ID == "" || rec.Answer.QuestionID == "" {
			return rec, errors.New("answer_recorded line is missing the answer")
		}
	case storeLineQuestionWithdrawn, storeLineQuestionExpired:
		if rec.QuestionID == "" {
			return rec, errors.New("line is missing question_id")
		}
	case storeLineQuestionEventEmitted:
		if rec.QuestionID == "" || rec.EventKind == "" {
			return rec, errors.New("question_event_emitted line is missing question_id or event_kind")
		}
	case storeLineCrashResidue:
		if rec.DiscardedBytes <= 0 {
			return rec, errors.New("crash_residue line is missing discarded_bytes")
		}
	default:
		return rec, errors.New("unknown record type")
	}
	return rec, nil
}

// applyRecord folds one record into the in-memory view.
func (s *QuestionStore) applyRecord(rec questionStoreRecord) {
	switch rec.Type {
	case storeLineQuestionRaised:
		q := *rec.Question
		s.questions[q.ID] = &q
	case storeLineAnswerRecorded:
		a := *rec.Answer
		s.answers[a.ID] = &a
		s.answerIDs[a.QuestionID] = append(s.answerIDs[a.QuestionID], a.ID)
		if q, ok := s.questions[a.QuestionID]; ok {
			q.State = QuestionAnswered
		}
	case storeLineQuestionWithdrawn:
		if q := s.questions[rec.QuestionID]; q != nil {
			q.State = QuestionWithdrawn
		}
		if rec.Actor != "" || rec.Reason != "" {
			meta := s.withdrawMeta[rec.QuestionID]
			meta.actor, meta.reason = rec.Actor, rec.Reason
			s.withdrawMeta[rec.QuestionID] = meta
		}
	case storeLineQuestionExpired:
		if q := s.questions[rec.QuestionID]; q != nil {
			q.State = QuestionExpired
		}
	case storeLineQuestionEventEmitted:
		s.eventEmitted[questionEventKey(rec.QuestionID, rec.EventKind, rec.AnswerID)] = true
	}
}

// appendLocked appends one record as a single new line and syncs it. The
// caller holds s.mu. The file is only ever extended: when a crash or a failed
// partial write left it without a trailing newline, the append first closes
// that line, and if the leftover is an unparsable fragment it also writes a
// crash_residue marker so replay skips exactly that fragment and the store
// still reopens. Nothing is truncated or rewritten.
func (s *QuestionStore) appendLocked(rec questionStoreRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("encode question store record: %w", err)
	}
	prefix, err := s.tailRepair()
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open question store %s for append: %w", s.path, err)
	}
	// One write for the repair and the record, so a failure leaves at most
	// one more unterminated tail, which the next append repairs the same way.
	chunk := append(prefix, data...)
	chunk = append(chunk, '\n')
	if _, err := s.writeFn(f, chunk); err != nil {
		_ = f.Close()
		return fmt.Errorf("append question store record: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync question store %s: %w", s.path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close question store %s: %w", s.path, err)
	}
	return nil
}

// tailRepair returns the bytes the next append must write before its own
// record. Empty when the file is missing, empty, or ends with a newline.
// Otherwise the last line is unterminated: a valid record there only needs
// its newline, an unparsable fragment also needs the crash_residue marker.
func (s *QuestionStore) tailRepair() ([]byte, error) {
	// The common case costs one byte: only an unterminated file is read whole.
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read question store %s: %w", s.path, err)
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("stat question store %s: %w", s.path, err)
	}
	last := make([]byte, 1)
	if fi.Size() == 0 {
		_ = f.Close()
		return nil, nil
	}
	if _, err := f.ReadAt(last, fi.Size()-1); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("read question store %s: %w", s.path, err)
	}
	_ = f.Close()
	if last[0] == '\n' {
		return nil, nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("read question store %s: %w", s.path, err)
	}
	tail := data[strings.LastIndexByte(string(data), '\n')+1:]
	prefix := []byte{'\n'}
	if _, perr := parseStoreRecord(string(tail)); perr != nil {
		marker, merr := json.Marshal(questionStoreRecord{Type: storeLineCrashResidue, DiscardedBytes: len(tail)})
		if merr != nil {
			return nil, fmt.Errorf("encode question store record: %w", merr)
		}
		prefix = append(prefix, marker...)
		prefix = append(prefix, '\n')
	}
	return prefix, nil
}

// emitAudit invokes the audit callback outside the store lock.
func (s *QuestionStore) emitAudit(action string, details map[string]any) {
	s.mu.Lock()
	fn := s.auditFn
	s.mu.Unlock()
	if fn != nil {
		fn(action, details)
	}
}

// Raise validates q (the record rules from ValidateNewQuestion), assigns a
// fresh id and creation time, sets the state to open, appends the record and
// stores it. It never blocks on anything else: the asker keeps working.
// The id and the state are never taken from the input.
func (s *QuestionStore) Raise(q Question) (Question, error) {
	if err := ValidateNewQuestion(&q); err != nil {
		return Question{}, err
	}
	s.mu.Lock()
	now := s.now()
	q.ID = s.newUniqueQuestionID(now)
	q.CreatedAt = &now
	q.State = QuestionOpen
	rec := questionStoreRecord{Type: storeLineQuestionRaised, Question: &q}
	if err := s.appendLocked(rec); err != nil {
		s.mu.Unlock()
		return Question{}, err
	}
	// On a successful append the in-memory view changes; on failure Raise
	// returned above and the view is untouched.
	stored := q
	s.questions[q.ID] = &stored
	s.mu.Unlock()

	// The write comes first; the audit entry follows only on success, with
	// the details documented on the audit constant.
	s.emitAudit(audit.ActionQuestionRaised, map[string]any{
		"question_id":  q.ID,
		"label":        q.Label,
		"asked_by":     q.AskedBy,
		"asker_kind":   string(q.AskerKind),
		"context":      q.Context,
		"risk":         q.Risk,
		"option_count": len(q.Options),
		"default":      q.Default,
		"text":         q.Text,
	})
	s.emitChange(QuestionChange{Kind: QuestionChangeRaised, Question: q})
	return q, nil
}

// Answer records an answer to the question. The quote is scanned for
// secrets first and, on a hit, refused: nothing is stored, and the refusal
// audit entry carries the name of the matched rule only, never any text. An
// allowed answer is stored exactly as received (no trimming, no
// normalisation), with its id, quote hash and time filled in, and it moves
// the question to answered. An answer to an answered question without a
// valid Supersedes is refused by the state rules.
func (s *QuestionStore) Answer(questionID string, a Answer) (Answer, error) {
	if pattern, found := ScanAnswerForSecret(a.Quote); found {
		// Audited without the text: the rule name is the only part of the
		// match that leaves the store.
		s.emitAudit(audit.ActionQuestionAnswerRefused, map[string]any{
			"question_id": questionID,
			"actor":       a.Actor,
			"surface":     a.Surface,
			"reason":      "secret_detected",
			"pattern":     pattern,
		})
		// The error carries the operator-facing refusal message and the
		// sentinel for errors.Is; it never carries the text.
		return Answer{}, fmt.Errorf("%s: %w", SecretRefusalMessage(pattern), ErrSecretInAnswer)
	}

	if !utf8.ValidString(a.Quote) {
		return Answer{}, fmt.Errorf("%w: it cannot be stored exactly as received", ErrQuoteNotUTF8)
	}

	s.mu.Lock()
	q, ok := s.questions[questionID]
	if !ok {
		s.mu.Unlock()
		return Answer{}, fmt.Errorf("question %s: %w", questionID, ErrQuestionNotFound)
	}
	current := ""
	if ids := s.answerIDs[questionID]; len(ids) > 0 {
		current = ids[len(ids)-1]
	}
	if err := CheckAnswerAllowed(q.State, current, a, q.Options); err != nil {
		s.mu.Unlock()
		return Answer{}, err
	}
	now := s.now()
	a.QuestionID = questionID
	a.ID = s.newUniqueAnswerID(now)
	a.CreatedAt = &now
	// QuoteSHA256 is the hash of the bytes in the quote. An option-only
	// answer has no quote, so there are no bytes to hash and the field is left
	// empty (its omitempty tag omits it when absent): the spec defines it as
	// "a hash of the bytes in quote", so no quote means no hash — not the hash
	// of the empty string.
	if a.Quote != "" {
		a.QuoteSHA256 = QuoteSHA256(a.Quote)
	}
	stored := a
	rec := questionStoreRecord{Type: storeLineAnswerRecorded, Answer: &a}
	if err := s.appendLocked(rec); err != nil {
		s.mu.Unlock()
		return Answer{}, err
	}
	// On a successful append the in-memory view changes; on failure the
	// store returned above with the view untouched.
	s.answers[a.ID] = &stored
	s.answerIDs[questionID] = append(s.answerIDs[questionID], a.ID)
	q.State = QuestionAnswered
	changed := *q
	s.mu.Unlock()

	// The write came first; emit exactly one audit entry per answer — the
	// first-answer action, or the supersedes action for a changed mind.
	details := map[string]any{
		"question_id":      questionID,
		"answer_id":        a.ID,
		"chosen_option_id": a.ChosenOptionID,
		"quote":            a.Quote,
		"quote_sha256":     a.QuoteSHA256,
		"actor":            a.Actor,
		"surface":          a.Surface,
	}
	if a.Supersedes != "" {
		details["supersedes"] = a.Supersedes
		s.emitAudit(audit.ActionAnswerSuperseded, details)
	} else {
		s.emitAudit(audit.ActionQuestionAnswered, details)
	}
	// The lifecycle event fires here, after the record is durable, through
	// the same dispatcher and per-subscriber pending queue as every other
	// kind; the marker then proves it went in (exactly once; see
	// question_events.go). A superseding answer fires a new event for itself
	// — the earlier one stays in the record, never edited. With no sink
	// wired (the daemon's switch off) neither happens: no event, no marker.
	ev := BuildQuestionAnsweredEvent(q, &a)
	if s.fireQuestionEvent(ev) {
		// A marker-write failure only defers the event (a later reconcile
		// re-fires it); the answer record stands.
		_ = s.MarkEventEmitted(ev)
	}
	answered := a
	s.emitChange(QuestionChange{Kind: QuestionChangeAnswered, Question: changed, Answer: &answered})
	return a, nil
}

// Withdraw moves an open question to withdrawn and records who withdrew it
// and why. A question that is no longer open is refused.
func (s *QuestionStore) Withdraw(id, actor, reason string) (Question, error) {
	out, err := s.mutateState(id, storeLineQuestionWithdrawn, actor, reason, QuestionWithdrawn)
	if err != nil {
		return Question{}, err
	}
	s.emitAudit(audit.ActionQuestionWithdrawn, map[string]any{
		"question_id": id,
		"actor":       actor,
		"reason":      reason,
	})
	// The lifecycle event, same path as the answer: fire, and mark only if
	// it fired; with no sink (switch off) neither happens.
	ev := BuildQuestionWithdrawnEvent(&out, actor, reason)
	if s.fireQuestionEvent(ev) {
		_ = s.MarkEventEmitted(ev)
	}
	s.emitChange(QuestionChange{Kind: QuestionChangeWithdrawn, Question: out, Actor: actor, Reason: reason})
	return out, nil
}

// Expire moves an open question to expired. Questions never expire: nothing
// in the store, and no timer and no sweeper, ever calls this method. It
// exists only so the expired state and its audit action are real for an
// explicit caller (for example a supervisor deciding the question is being
// skipped). Retention is forever either way: there is no delete.
func (s *QuestionStore) Expire(id, reason string) (Question, error) {
	out, err := s.mutateState(id, storeLineQuestionExpired, "", reason, QuestionExpired)
	if err != nil {
		return Question{}, err
	}
	// The documented details for the expiry action are the question id and
	// the reason only.
	s.emitAudit(audit.ActionQuestionExpired, map[string]any{
		"question_id": id,
		"reason":      reason,
	})
	s.emitChange(QuestionChange{Kind: QuestionChangeExpired, Question: out, Reason: reason})
	return out, nil
}

// mutateState appends a state-change record for an open question and moves
// its state, returning a copy of the question taken while the store lock is
// still held. A question that is not open is refused with ErrQuestionNotOpen,
// and one the store does not hold with ErrQuestionNotFound.
func (s *QuestionStore) mutateState(id, lineType, actor, reason string, nextState QuestionState) (Question, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.questions[id]
	if !ok {
		return Question{}, fmt.Errorf("question %s: %w", id, ErrQuestionNotFound)
	}
	if q.State != QuestionOpen {
		return Question{}, fmt.Errorf("question %s: %w", id, ErrQuestionNotOpen)
	}
	now := s.now()
	rec := questionStoreRecord{
		Type:       lineType,
		QuestionID: id,
		Actor:      actor,
		Reason:     reason,
		At:         &now,
	}
	if err := s.appendLocked(rec); err != nil {
		return Question{}, err
	}
	q.State = nextState
	return *q, nil
}

// Get returns the current view of the question with id.
func (s *QuestionStore) Get(id string) (Question, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.questions[id]
	if !ok {
		return Question{}, false
	}
	return *q, true
}

// QuestionFilter narrows List. Every field is optional; an empty field
// means "no filter on this field". Text is a case-insensitive substring
// searched over the question text, the option labels and tradeoffs, and the
// answer quotes. Limit is the maximum number of results (default 100,
// maximum 1000).
type QuestionFilter struct {
	State   QuestionState
	Context string
	Asker   string // exact match on AskedBy
	Text    string
	Limit   int
}

// List returns the matching questions, newest first, capped at the filter's
// limit (default 100, maximum 1000).
func (s *QuestionStore) List(f QuestionFilter) []Question {
	s.mu.Lock()
	defer s.mu.Unlock()
	lowerText := strings.ToLower(f.Text)
	matched := make([]Question, 0, len(s.questions))
	for _, q := range s.questions {
		if f.State != "" && q.State != f.State {
			continue
		}
		if f.Context != "" && q.Context != f.Context {
			continue
		}
		if f.Asker != "" && q.AskedBy != f.Asker {
			continue
		}
		if lowerText != "" && !strings.Contains(s.textHaystack(q), lowerText) {
			continue
		}
		matched = append(matched, *q)
	}
	sort.Slice(matched, func(i, j int) bool {
		ti, tj := matched[i].CreatedAt, matched[j].CreatedAt
		if ti != nil && tj != nil {
			a, b := *ti, *tj
			if !a.Equal(b) {
				return a.After(b)
			}
		}
		// The ids embed the creation time and are unique, so this breaks
		// ties deterministically, newest first.
		return matched[i].ID > matched[j].ID
	})
	limit := f.Limit
	if limit <= 0 {
		limit = defaultQuestionListLimit
	}
	if limit > maxQuestionListLimit {
		limit = maxQuestionListLimit
	}
	if len(matched) > limit {
		matched = matched[:limit]
	}
	return matched
}

// textHaystack gathers the searchable text of a question and its answers:
// the question text, the option labels and tradeoffs, and every stored
// answer quote (verbatim, as stored).
func (s *QuestionStore) textHaystack(q *Question) string {
	var b strings.Builder
	b.WriteString(q.Text)
	for _, opt := range q.Options {
		b.WriteString(opt.Label)
		b.WriteString(opt.Tradeoffs)
	}
	for _, id := range s.answerIDs[q.ID] {
		if a := s.answers[id]; a != nil {
			b.WriteString(a.Quote)
		}
	}
	return strings.ToLower(b.String())
}

// AnswerView is an answer plus its derived superseded state: an answer is
// superseded when a later answer names it in Supersedes. The flags are
// derived on read — the answer line itself never changes.
type AnswerView struct {
	Answer
	Superseded   bool   `json:"superseded"`
	SupersededBy string `json:"superseded_by,omitempty"`
}

// Answers returns the question's answers, oldest first, with the derived
// Superseded and SupersededBy flags. The bool reports whether the question
// exists.
func (s *QuestionStore) Answers(questionID string) ([]AnswerView, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.questions[questionID]; !ok {
		return nil, false
	}
	ids := s.answerIDs[questionID]
	out := make([]AnswerView, 0, len(ids))
	for i, id := range ids {
		a, ok := s.answers[id]
		if !ok {
			continue
		}
		view := AnswerView{Answer: *a}
		for _, laterID := range ids[i+1:] {
			if la := s.answers[laterID]; la != nil && la.Supersedes == a.ID {
				view.Superseded = true
				view.SupersededBy = laterID
				break
			}
		}
		out = append(out, view)
	}
	return out, true
}

// CurrentAnswer returns the question's most recent answer.
func (s *QuestionStore) CurrentAnswer(questionID string) (Answer, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := s.answerIDs[questionID]
	if len(ids) == 0 {
		return Answer{}, false
	}
	a, ok := s.answers[ids[len(ids)-1]]
	if !ok {
		return Answer{}, false
	}
	return *a, true
}

// AnswerCount returns how many answers the question has (current plus
// superseded; none are ever deleted).
func (s *QuestionStore) AnswerCount(questionID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.answerIDs[questionID])
}

// defaultNewQuestionID builds a question id: 'q-' plus the creation time in
// nanoseconds as zero-padded hex (time-sortable), plus 4 random hex
// characters so concurrent ids differ. Ids are stable and never reused.
func defaultNewQuestionID(now time.Time) string {
	return "q-" + fmt.Sprintf("%016x", uint64(now.UnixNano())) + randomHex(2)
}

// defaultNewAnswerID builds an answer id: 'a-' likewise.
func defaultNewAnswerID(now time.Time) string {
	return "a-" + fmt.Sprintf("%016x", uint64(now.UnixNano())) + randomHex(2)
}

// randomHex returns n random bytes encoded as lowercase hex (2n chars).
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return strings.Repeat("0", n*2)
	}
	return hex.EncodeToString(b)
}

// newUniqueQuestionID returns a question id that no raised question already
// holds, so a raise never overwrites an existing question and an id is never
// reused. The first candidate keeps the documented id format (prefix, then 20
// hex characters); only if that candidate already exists does the store append
// fresh disambiguating entropy until one is free. Each appended byte is a
// fresh random draw, so the loop terminates even if the id generator itself
// is degenerate. Time-sortability is preserved because the collision suffix
// is appended after the time prefix. The caller holds s.mu.
func (s *QuestionStore) newUniqueQuestionID(now time.Time) string {
	id := s.newQuestionID(now)
	for s.questions[id] != nil {
		id += randomHex(1)
	}
	return id
}

// newUniqueAnswerID is the answer-id analogue of newUniqueQuestionID: it
// resolves a collision against the stored answers so no answer id is reused.
// The caller holds s.mu.
func (s *QuestionStore) newUniqueAnswerID(now time.Time) string {
	id := s.newAnswerID(now)
	for s.answers[id] != nil {
		id += randomHex(1)
	}
	return id
}
