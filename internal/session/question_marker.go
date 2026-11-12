// Worker question marker parser: recognises the DATAWATCH_QUESTION: line a
// worker session prints to ask the operator a design question (the Step 4
// operator decision channel). It mirrors classifyNeedsInput in
// needs_input_echo.go: ANSI and leading indicator runes are stripped, the line
// is split on carriage return, and only a segment that starts with the marker
// counts. Task text and instruction echoes reach the pane verbatim, so a
// marker in the middle of a sentence or followed by a placeholder is dropped.
// The parser only decodes and validates; storing and raising a question is the
// caller's job, and a refused line is never stored or raised.
package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// workerQuestionMarker is the prefix a worker prints before the JSON payload.
const workerQuestionMarker = "DATAWATCH_QUESTION:"

// Caps on a worker question line. They duplicate the caps in
// internal/autonomous/questions.go (questionTextMaxBytes,
// questionOptionsMax, questionOptionLabelMaxBytes, questionTradeoffsMaxBytes,
// questionRationaleMaxBytes and answerQuoteMaxBytes) because session cannot
// import autonomous; keep the two in step. workerQuestionLineMaxBytes caps the
// whole line and matches the answer quote cap there.
const (
	workerQuestionTextMaxBytes        = 8192
	workerQuestionOptionsMax          = 20
	workerQuestionOptionLabelMaxBytes = 200
	workerQuestionTradeoffsMaxBytes   = 1000
	workerQuestionRationaleMaxBytes   = 1000
	workerQuestionLineMaxBytes        = 16384
)

// Defaults applied to a payload that omits a field.
const (
	workerQuestionDefaultHold  = "hold"
	workerQuestionDefaultLabel = "decision"
	workerQuestionRiskLow      = "low"
	workerQuestionRiskNormal   = "normal"
)

// workerQuestionLabels is the accepted label set; it duplicates
// DefaultQuestionLabels in internal/autonomous/questions.go.
var workerQuestionLabels = map[string]bool{
	"decision":      true,
	"design review": true,
	"approval":      true,
	"information":   true,
}

// workerQuestionKind classifies a line against the worker question marker.
type workerQuestionKind int

const (
	// workerQuestionNone: the line carries no marker.
	workerQuestionNone workerQuestionKind = iota
	// workerQuestionInstruction: the line mentions the marker but is not a
	// question, e.g. task text that names it mid-sentence, a placeholder such
	// as "DATAWATCH_QUESTION: <json>", or a shell echo. It is dropped silently.
	workerQuestionInstruction
	// workerQuestionReal: a standalone line that starts with the marker and a
	// brace and carries a valid payload.
	workerQuestionReal
	// workerQuestionRefused: a line that starts with the marker and a brace
	// but fails validation. It is never stored and never raised.
	workerQuestionRefused
)

// Refusal reason categories. A reason is a category, never the line text.
const (
	workerQuestionReasonMalformed          = "malformed"
	workerQuestionReasonTooLong            = "too_long"
	workerQuestionReasonEmptyQuestion      = "empty_question"
	workerQuestionReasonTooFewOptions      = "too_few_options"
	workerQuestionReasonDuplicateOption    = "duplicate_option"
	workerQuestionReasonInvalidLabel       = "invalid_label"
	workerQuestionReasonInvalidRisk        = "invalid_risk"
	workerQuestionReasonInvalidDefault     = "invalid_default"
	workerQuestionReasonInvalidRecommended = "invalid_recommendation"
)

// WorkerQuestionOption is one choice on a worker question.
type WorkerQuestionOption struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Tradeoffs string `json:"tradeoffs,omitempty"`
}

// WorkerQuestionRecommendation is the worker's suggested option and why.
type WorkerQuestionRecommendation struct {
	OptionID  string `json:"option_id"`
	Rationale string `json:"rationale,omitempty"`
}

// WorkerQuestionSignal is the neutral form of a parsed worker question. It
// carries no autonomous types, so the caller maps it onto a question record.
type WorkerQuestionSignal struct {
	Question       string
	Options        []WorkerQuestionOption
	Recommendation *WorkerQuestionRecommendation
	Default        string
	Blocked        bool
	Label          string
	Risk           string
	// LineSHA256 is the hex SHA-256 of the exact line bytes, so a re-read of
	// the same pane line can be told apart from a new question.
	LineSHA256 string
}

// workerQuestionPayload is the wire form of the JSON after the marker. Pointer
// fields tell an absent key from a zero value.
type workerQuestionPayload struct {
	Question       *string                       `json:"question"`
	Options        []WorkerQuestionOption        `json:"options"`
	Recommendation *WorkerQuestionRecommendation `json:"recommendation"`
	Default        *string                       `json:"default"`
	Blocked        bool                          `json:"blocked"`
	Label          *string                       `json:"label"`
	Risk           *string                       `json:"risk"`
}

// classifyWorkerQuestion reports whether line is a worker question. Like the
// needs-input marker, a real question starts a segment (after TUI indicator
// runes such as ●) and is followed by an open brace. For a real question it
// returns the signal; for a refused line it returns the reason category and a
// zero signal.
func classifyWorkerQuestion(line string) (workerQuestionKind, WorkerQuestionSignal, string) {
	if !strings.Contains(line, workerQuestionMarker) {
		return workerQuestionNone, WorkerQuestionSignal{}, ""
	}
	kind := workerQuestionInstruction
	for _, seg := range strings.Split(line, "\r") {
		seg = strings.TrimSpace(StripANSI(strings.TrimSpace(seg)))
		for len(seg) > 0 {
			r, size := utf8.DecodeRuneInString(seg)
			if r <= 127 {
				break
			}
			seg = strings.TrimLeft(seg[size:], " \t")
		}
		if !strings.HasPrefix(seg, workerQuestionMarker) {
			continue
		}
		rest := strings.TrimSpace(seg[len(workerQuestionMarker):])
		if !strings.HasPrefix(rest, "{") {
			continue
		}
		if len(line) > workerQuestionLineMaxBytes {
			return workerQuestionRefused, WorkerQuestionSignal{}, workerQuestionReasonTooLong
		}
		sig, reason := parseWorkerQuestionPayload(rest)
		if reason != "" {
			return workerQuestionRefused, WorkerQuestionSignal{}, reason
		}
		sum := sha256.Sum256([]byte(line))
		sig.LineSHA256 = hex.EncodeToString(sum[:])
		return workerQuestionReal, sig, ""
	}
	return kind, WorkerQuestionSignal{}, ""
}

// parseWorkerQuestionPayload strictly decodes and validates the JSON after the
// marker. It returns a refusal reason category, or "" with the signal.
func parseWorkerQuestionPayload(raw string) (WorkerQuestionSignal, string) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	var p workerQuestionPayload
	if err := dec.Decode(&p); err != nil {
		return WorkerQuestionSignal{}, workerQuestionReasonMalformed
	}
	// Exactly one JSON value: anything after it is malformed.
	if _, err := dec.Token(); err != io.EOF {
		return WorkerQuestionSignal{}, workerQuestionReasonMalformed
	}

	if p.Question == nil || strings.TrimSpace(*p.Question) == "" {
		return WorkerQuestionSignal{}, workerQuestionReasonEmptyQuestion
	}
	if len(*p.Question) > workerQuestionTextMaxBytes {
		return WorkerQuestionSignal{}, workerQuestionReasonTooLong
	}

	if len(p.Options) > workerQuestionOptionsMax {
		return WorkerQuestionSignal{}, workerQuestionReasonTooLong
	}
	if len(p.Options) < 2 {
		return WorkerQuestionSignal{}, workerQuestionReasonTooFewOptions
	}
	ids := make(map[string]bool, len(p.Options))
	for _, o := range p.Options {
		if o.ID == "" || o.Label == "" {
			return WorkerQuestionSignal{}, workerQuestionReasonMalformed
		}
		if ids[o.ID] {
			return WorkerQuestionSignal{}, workerQuestionReasonDuplicateOption
		}
		ids[o.ID] = true
		if len(o.Label) > workerQuestionOptionLabelMaxBytes || len(o.Tradeoffs) > workerQuestionTradeoffsMaxBytes {
			return WorkerQuestionSignal{}, workerQuestionReasonTooLong
		}
	}

	if p.Recommendation != nil {
		if len(p.Recommendation.Rationale) > workerQuestionRationaleMaxBytes {
			return WorkerQuestionSignal{}, workerQuestionReasonTooLong
		}
		// The store rejects a recommendation that names no option, so
		// refuse it here rather than raise a question that cannot be stored.
		if !ids[p.Recommendation.OptionID] {
			return WorkerQuestionSignal{}, workerQuestionReasonInvalidRecommended
		}
	}

	def := workerQuestionDefaultHold
	if p.Default != nil {
		def = *p.Default
	}
	if def != workerQuestionDefaultHold && !ids[def] {
		return WorkerQuestionSignal{}, workerQuestionReasonInvalidDefault
	}

	label := workerQuestionDefaultLabel
	if p.Label != nil {
		label = *p.Label
	}
	if !workerQuestionLabels[label] {
		return WorkerQuestionSignal{}, workerQuestionReasonInvalidLabel
	}

	risk := workerQuestionRiskNormal
	if p.Risk != nil {
		risk = *p.Risk
	}
	if risk != workerQuestionRiskLow && risk != workerQuestionRiskNormal {
		return WorkerQuestionSignal{}, workerQuestionReasonInvalidRisk
	}

	return WorkerQuestionSignal{
		Question:       *p.Question,
		Options:        p.Options,
		Recommendation: p.Recommendation,
		Default:        def,
		Blocked:        p.Blocked,
		Label:          label,
		Risk:           risk,
	}, ""
}

// WorkerQuestionEvent is what the scanner hands the hook for a worker question
// line. Exactly one of Signal and RefusalReason is set: Signal for a valid
// question, RefusalReason (a category, never the line text) for a refused one.
// LineSHA256 is the hex SHA-256 of the exact line in both cases, so a refusal
// can be audited by hash without carrying the line.
type WorkerQuestionEvent struct {
	SessionFullID string
	PRDID         string
	TaskID        string
	Signal        *WorkerQuestionSignal
	RefusalReason string
	LineSHA256    string
	At            time.Time
}

// workerQuestionRoute holds the gate and hook behind their own lock, so a
// setter called after the scanners start never races a read.
type workerQuestionRoute struct {
	mu   sync.RWMutex
	gate func() bool
	hook func(WorkerQuestionEvent)
}

// SetWorkerQuestionGate sets the switch that turns worker question routing on.
// A nil gate, or one that returns false, leaves routing off.
func (m *Manager) SetWorkerQuestionGate(fn func() bool) {
	m.workerQuestion.mu.Lock()
	m.workerQuestion.gate = fn
	m.workerQuestion.mu.Unlock()
}

// SetWorkerQuestionHook sets the receiver for worker question events. It runs
// on the output scanner, so it must return quickly: the daemon side hands the
// event to a goroutine and never waits on a question store here.
func (m *Manager) SetWorkerQuestionHook(fn func(WorkerQuestionEvent)) {
	m.workerQuestion.mu.Lock()
	m.workerQuestion.hook = fn
	m.workerQuestion.mu.Unlock()
}

// routeWorkerQuestion sends line to the worker question hook when it is a real
// or refused worker question, the gate is on, and the session's markers are
// trusted (outside the post-restart grace and the task-echo window, the same
// window the needs-input marker uses). It never changes session state.
func (m *Manager) routeWorkerQuestion(sess *Session, line string) {
	m.workerQuestion.mu.RLock()
	gate, hook := m.workerQuestion.gate, m.workerQuestion.hook
	m.workerQuestion.mu.RUnlock()
	if hook == nil {
		return
	}
	kind, sig, reason := classifyWorkerQuestion(line)
	if kind != workerQuestionReal && kind != workerQuestionRefused {
		return
	}
	if gate == nil || !gate() {
		return
	}
	if !m.markersTrusted(sess) {
		m.debugf("processOutputLine: ignoring worker question marker inside restart-grace/task-echo window for %s", sess.FullID)
		return
	}
	ev := WorkerQuestionEvent{
		SessionFullID: sess.FullID,
		PRDID:         sess.PRDID,
		TaskID:        sess.TaskID,
		At:            m.now(),
	}
	if kind == workerQuestionReal {
		ev.Signal = &sig
		ev.LineSHA256 = sig.LineSHA256
	} else {
		ev.RefusalReason = reason
		sum := sha256.Sum256([]byte(line))
		ev.LineSHA256 = hex.EncodeToString(sum[:])
	}
	hook(ev)
}
