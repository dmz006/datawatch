// held_questions.go — GET /api/autonomous/held-questions: the read-only
// report on held-question mode — the mode and the held state ids it covers,
// the open held questions raised in "on" mode (from the question store) and
// the shadow entries the daemon logged on the Automata's triage records
// while in "shadow" mode. GET only: the endpoint never writes anything, and
// acting on a hold is a worker question answer, not this surface.
//
// Gating mirrors the neighboring /api/autonomous endpoints and the operator
// questions switch: 405 on a non-GET, 503 when the autonomous manager or
// the question store is not wired in, the federated peer's read capability
// (fedCap), and 503 with the switch named when the operator questions
// switch is off — held questions live under that switch's subsystem.
package server

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

// heldShadowNoteSource is the TriageNote.Source the daemon's shadow pass
// stamps on the held entries it logs instead of raising (see
// Manager.AppendHeldShadowNote). "on" mode writes nothing to the triage
// record — it raises questions instead — so the shadow list only ever
// carries what shadow mode logged.
const heldShadowNoteSource = "held"

// heldShadowNoteDivider is the " — " the shadow note places between the
// "held:<key>:<state>" marker and the rest of the note text.
const heldShadowNoteDivider = " — "

// heldQuestionOpenEntry is one open held question, as reported. State is
// the held state the question holds in (see heldStateOfOpenQuestion), not
// the question's lifecycle state — the list already carries only open
// ones.
type heldQuestionOpenEntry struct {
	QuestionID        string     `json:"question_id"`
	State             string     `json:"state,omitempty"`
	PRDID             string     `json:"prd_id,omitempty"`
	TaskID            string     `json:"task_id,omitempty"`
	AutomationID      string     `json:"automation_id,omitempty"`
	Title             string     `json:"title,omitempty"`
	Reason            string     `json:"reason,omitempty"`
	RecommendedOption string     `json:"recommended_option,omitempty"`
	Default           string     `json:"default,omitempty"`
	CreatedAt         *time.Time `json:"created_at,omitempty"`
	// SystemTags is the placement-derived tag set the held question
	// carries (the fact of the Automaton its PRDID names); it is what the
	// project / automation / automaton query filters match against.
	SystemTags []string `json:"system_tags,omitempty"`
}

// heldQuestionShadowEntry is one shadow note the daemon logged for a held
// entry it would have raised: where (Automaton id), which held state, the
// option it recommends and when the note landed.
type heldQuestionShadowEntry struct {
	PRDID             string    `json:"prd_id"`
	State             string    `json:"state"`
	RecommendedOption string    `json:"recommended_option,omitempty"`
	At                time.Time `json:"at"`
	// SystemTags is the placement-derived tag set of the Automaton the note
	// belongs to, the same fact its open-question sibling derives; it is
	// what the project / automation / automaton query filters match against.
	SystemTags []string `json:"system_tags,omitempty"`
}

// heldQuestionsResponse is the full report: the mode in effect and the
// held state ids it covers, then the two data views. The lists are always
// arrays (never null) so a consumer can range them unconditionally.
type heldQuestionsResponse struct {
	Mode   string                    `json:"mode"`
	States []string                  `json:"states"`
	Open   []heldQuestionOpenEntry   `json:"open"`
	Shadow []heldQuestionShadowEntry `json:"shadow"`
}

// handleHeldQuestions serves GET /api/autonomous/held-questions. Read-only:
// no method here writes, and the route carries no write surface at all.
func (s *Server) handleHeldQuestions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed: only GET is supported here", http.StatusMethodNotAllowed)
		return
	}
	if s.autonomousMgr == nil {
		http.Error(w, "held questions unavailable: the autonomous manager is not wired in", http.StatusServiceUnavailable)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	if s.cfg != nil && !s.cfg.Autonomous.Questions.Enabled {
		http.Error(w, "operator questions are disabled: set autonomous.questions.enabled to true first (held questions live under the operator-question subsystem switch)", http.StatusServiceUnavailable)
		return
	}
	if s.questionStore == nil {
		http.Error(w, "held questions unavailable: the question store is not wired in", http.StatusServiceUnavailable)
		return
	}

	qcfg := config.AutonomousQuestionsConfig{}
	if s.cfg != nil {
		qcfg = s.cfg.Autonomous.Questions
	}
	mode := qcfg.EffectiveHeldMode()
	states := qcfg.EffectiveHeldStates()

	prdIDFilter := strings.TrimSpace(r.URL.Query().Get("prd_id"))
	stateFilter := strings.TrimSpace(r.URL.Query().Get("state"))
	if stateFilter != "" && !heldStateIDKnown(stateFilter) {
		http.Error(w, fmt.Sprintf("state must be one of the held question state ids: %s (got %q)",
			strings.Join(config.AllHeldStates, ", "), stateFilter), http.StatusBadRequest)
		return
	}
	// The object tag selection — the repeatable tag / tag_id with the
	// tag_match any|all rule and the project / automation / automaton
	// shortcuts — narrows both views against the tags each entry carries;
	// an entry with no axes (no project, no Automation) is absent from any
	// such result.
	tagSel, terr := objectTagSelectionFromRequest(r)
	if terr != nil {
		http.Error(w, terr.Error(), http.StatusBadRequest)
		return
	}

	// One pass over every Automaton feeds both views: the PRD lookup enriches
	// the open entries with the owning Automation name and the Automaton
	// title, and the triage record's notes are the shadow-mode log itself.
	prds := map[string]*autonomous.PRD{}
	shadow := make([]heldQuestionShadowEntry, 0)
	for _, v := range s.autonomousMgr.ListPRDs() {
		if isNilPRD(v) {
			continue
		}
		p, err := toPRD(v)
		if err != nil || p == nil || p.ID == "" {
			continue
		}
		prds[p.ID] = p
		if prdIDFilter != "" && p.ID != prdIDFilter {
			continue
		}
		if p.Triage == nil {
			continue
		}
		for _, n := range p.Triage.Notes {
			if n.Source != heldShadowNoteSource {
				continue
			}
			entry, ok := parseHeldShadowNote(n)
			if !ok || (stateFilter != "" && entry.State != stateFilter) {
				continue
			}
			if !tagSel.MatchesObjectTags(p.SystemTags, nil) {
				continue
			}
			entry.PRDID = p.ID
			entry.SystemTags = p.SystemTags
			shadow = append(shadow, entry)
		}
	}

	// The open held questions: the store's open list (and the Automaton id
	// filter the store understands) narrowed to source "held" and, when a
	// held state was requested, to the held state the question names.
	open := make([]heldQuestionOpenEntry, 0)
	for _, q := range s.questionStore.List(autonomous.QuestionFilter{State: autonomous.QuestionOpen, PRDID: prdIDFilter}) {
		if q.Source != autonomous.QuestionSourceHeld {
			continue
		}
		state := heldStateOfOpenQuestion(&q)
		if stateFilter != "" && state != stateFilter {
			continue
		}
		if !tagSel.MatchesObjectTags(q.SystemTags, nil) {
			continue
		}
		entry := heldQuestionOpenEntry{
			QuestionID: q.ID,
			State:      state,
			PRDID:      q.PRDID,
			TaskID:     q.TaskID,
			Reason:     heldQuestionReason(q.Text),
			Default:    q.Default,
			CreatedAt:  q.CreatedAt,
			SystemTags: q.SystemTags,
		}
		if q.Recommendation != nil {
			entry.RecommendedOption = q.Recommendation.OptionID
		}
		if p, ok := prds[q.PRDID]; ok {
			entry.AutomationID = p.AutomationID
			entry.Title = p.Title
		}
		open = append(open, entry)
	}

	writeJSONOK(w, heldQuestionsResponse{Mode: mode, States: states, Open: open, Shadow: shadow})
}

// heldStateIDKnown reports whether state is one of the five held state ids:
// the vocabulary the state query filter validates against.
func heldStateIDKnown(state string) bool {
	for _, s := range config.AllHeldStates {
		if s == state {
			return true
		}
	}
	return false
}

// heldLineHash mirrors the daemon's held question dedup key hash
// (autonomous.heldQuestionLineHash): the hex SHA-256 of "held:<key>:<state>",
// lowercased. It exists here so the server can read a stored open question
// back to the held state it names without reaching into the daemon's pass.
func heldLineHash(key, state string) string {
	sum := sha256.Sum256([]byte("held:" + key + ":" + state))
	return hex.EncodeToString(sum[:])
}

// heldPendingIDLineRe pulls a pending insert id out of a held question's
// text ("Pending insert id is <id>." — see autonomous.HeldQuestionText).
// It is a key candidate for heldStateOfOpenQuestion, where the pending
// insert's held entry dedups on the pending id, not a task or Automaton id.
var heldPendingIDLineRe = regexp.MustCompile(`Pending insert id is (\S+)\.`)

// heldStateOfOpenQuestion names the held state a stored open held question
// holds in, recovered from its dedup key: the daemon hashes
// "held:<key>:<state>" with the key being the task id when the hold names a
// task, the pending insert id for pending insert holds, and the Automaton
// id otherwise. Matches the question's stored source line hash against each
// candidate key over all five held state ids. Returns "" when the question
// carries no stored hash or none of the candidates match.
func heldStateOfOpenQuestion(q *autonomous.Question) string {
	if q == nil || q.SourceLineSHA256 == "" {
		return ""
	}
	keys := make([]string, 0, 3)
	if q.TaskID != "" {
		keys = append(keys, q.TaskID)
	}
	if q.PRDID != "" {
		keys = append(keys, q.PRDID)
	}
	if m := heldPendingIDLineRe.FindStringSubmatch(q.Text); m != nil {
		keys = append(keys, m[1])
	}
	for _, st := range config.AllHeldStates {
		for _, k := range keys {
			if heldLineHash(k, st) == q.SourceLineSHA256 {
				return st
			}
		}
	}
	return ""
}

// heldQuestionReason pulls the "Why it is held: <reason>." sentence out of
// a held question's text (as composed by the daemon's table) and returns
// the reason verbatim. It is not the question's full text — the table's
// one-sentence rationale lives in the Recommendation, and the state
// description comes first. Empty when the text carries no reason line.
func heldQuestionReason(text string) string {
	const prefix = " Why it is held: "
	i := strings.Index(text, prefix)
	if i < 0 {
		return ""
	}
	rest := text[i+len(prefix):]
	for _, cut := range []string{" Failure category:", " Attempts used so far:", " Available backends:"} {
		if j := strings.Index(rest, cut); j >= 0 {
			rest = rest[:j]
		}
	}
	return strings.TrimSuffix(rest, ".")
}

// heldShadowNoteRe pulls the recommended option id out of a shadow note's
// tail: "…recommended option <id> (<label>)".
var heldShadowNoteRe = regexp.MustCompile(`recommended option (\S+) \(`)

// parseHeldShadowNote reads one shadow-mode note back to an entry. ok is
// false when the note is not the daemon's held shadow shape — anything else
// logged under the source is left out of the report rather than guessed at.
// The note is "held:<key>:<state> — <state> on <who>: …; recommended option
// <id> (<label>)": the marker carries the held state, the tail the option.
func parseHeldShadowNote(n autonomous.TriageNote) (heldQuestionShadowEntry, bool) {
	marker, rest, found := strings.Cut(n.Text, heldShadowNoteDivider)
	if !found || !strings.HasPrefix(marker, "held:") {
		return heldQuestionShadowEntry{}, false
	}
	parts := strings.SplitN(marker, ":", 3)
	if len(parts) != 3 || parts[0] != "held" {
		return heldQuestionShadowEntry{}, false
	}
	state := parts[2]
	if !heldStateIDKnown(state) {
		return heldQuestionShadowEntry{}, false
	}
	entry := heldQuestionShadowEntry{State: state, At: n.At}
	if m := heldShadowNoteRe.FindStringSubmatch(rest); m != nil {
		entry.RecommendedOption = m[1]
	}
	return entry, true
}
