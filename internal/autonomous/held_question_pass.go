// Held-question pass — the daemon's one generic question raised on its own
// initiative when an Automaton holds for a decision: blocked, review-held
// after repeated failures, revision-requested, awaiting an unanswered
// decision, or a pending member insert. The per-state content lives in the
// table (held_question_table.go); this file is only the pass: when to run
// it, what is held at this tick, and how the question is raised, noted in
// shadow mode, deduped and withdrawn.
//
// Modes (autonomous.questions.held_mode, empty reads as shadow):
//
//	off     the pass runs no detection and writes nothing
//	shadow  the pass computes each held Automaton and records one
//	        append-only TriageNote per (item, state) on the Automaton's
//	        record; no question is raised
//	on      the pass raises one general question per held (item, state)
//
// Dedupe is stable across restarts: every raised question carries
// SessionID "held" and SourceLineSHA256 of "held:<item><state>", so
// RaiseUnlessOpenSourceLine admits the first raise and refuses any later
// one with the same key while the question is open.
//
// When the state clears, the open question is withdrawn by the daemon
// with a reason naming the cleared state. A task parked on a decision
// that already exists as an open question is never raised a second
// question: the held item links to the one already open instead.

package autonomous

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// heldQuestionSessionID is the SessionID the pass stamps on every held
// question it raises. It joins with SourceLineSHA256 into the store's
// dedupe key, so held questions never collide with worker questions or
// questions from the REST surface, which use empty or other session ids.
const heldQuestionSessionID = "held"

// heldQuestionActor is the actor recorded on every withdraw the pass
// performs.
const heldQuestionActor = "daemon"

// heldQuestionNoteSource is the TriageNote.Source of a shadow-mode note
// written by the pass.
const heldQuestionNoteSource = "held"

// heldQuestionRef is one entry of the in-memory index of held questions
// the pass is aware of: the dedupe key, the state it was raised for and
// the question id. The index is rebuilt from the store on the first pass
// after a restart; it is only the pass's own bookkeeping, the store stays
// the source of truth.
type heldQuestionRef struct {
	key        string
	state      string
	questionID string
}

// heldQuestionPending is one held item collected at this tick together
// with its dedupe key. linkedQuestionID is set when the held state is
// already covered by an open question (a task parked on a decision that
// already exists as an open question); the pass then records the link
// instead of raising a second question.
type heldQuestionPending struct {
	item             *HeldItem
	hash             string
	linkedQuestionID string
}

// heldQuestionPass runs the held-question pass for the current tick. It
// is nil-safe and gated on the live autonomous.questions config read on
// every tick, the same way the triage pass reads its threshold.
func (c *Coordinator) heldQuestionPass() {
	if c == nil || c.m == nil {
		return
	}
	store, qcfg, ok := c.heldQuestionGate()
	if !ok {
		return
	}
	mode := qcfg.EffectiveHeldMode()
	if mode == config.HeldModeOff {
		return // off: no detection, no writes, nothing withdrawn
	}

	pending := c.collectHeldItems(qcfg, store)

	// First pass after a (re)start: rebuild the in-memory index from the
	// store so the pass keeps its withdraw bookkeeping across restarts.
	c.heldIndexInit(store)

	// Every currently held (item, state) dedupe key.
	active := make(map[string]struct{}, len(pending))
	for _, p := range pending {
		active[p.hash] = struct{}{}
	}

	// Withdraw first: a held question whose held state is no longer
	// present at this tick is withdrawn, naming the cleared state in the
	// reason. Questions the pass did not raise (any other source, or an
	// open question in its own list that is no longer held) are the pass's
	// to remove; an already answered or withdrawn question is left where
	// it is.
	for hash, ref := range c.heldIndexSnapshot() {
		if _, stillHeld := active[hash]; stillHeld {
			continue
		}
		q, open := heldQuestionOpenByID(store, ref.questionID)
		if !open {
			c.heldIndexDrop(hash)
			continue
		}
		if _, err := store.Withdraw(q.ID, heldQuestionActor,
			fmt.Sprintf("held state %s for %s cleared", ref.state, ref.key)); err == nil {
			c.heldIndexDrop(hash)
			c.audit("held_question_withdraw", map[string]any{
				"question_id": q.ID, "state": ref.state, "key": ref.key,
			})
		}
	}

	now := c.now()
	for _, p := range pending {
		if p.linkedQuestionID != "" {
			// The decision being waited on is already an open question:
			// record the link instead of raising a second question for
			// the same hold.
			if mode == config.HeldModeShadow {
				c.heldShadowNote(p, now)
			}
			c.audit("held_question_linked", map[string]any{
				"state": string(p.item.State), "key": heldQuestionKey(p.item),
				"question_id": p.linkedQuestionID,
			})
			continue
		}
		if open, ok := heldQuestionOpenByHash(store, p.hash); ok {
			// Already raised and still open: one open question per
			// (item, state), across restarts.
			c.heldIndexSet(p.hash, heldQuestionRef{
				key: heldQuestionKey(p.item), state: string(p.item.State), questionID: open.ID,
			})
			continue
		}
		switch mode {
		case config.HeldModeShadow:
			c.heldShadowNote(p, now)
		default: // on
			q := BuildHeldQuestion(p.item)
			q.SessionID = heldQuestionSessionID
			q.SourceLineSHA256 = p.hash
			raised, _, err := store.RaiseUnlessOpenSourceLine(q)
			if err != nil {
				c.audit("held_question_raise_refused", map[string]any{
					"state": string(p.item.State), "key": heldQuestionKey(p.item),
					"cause": err.Error(),
				})
				continue
			}
			c.heldIndexSet(p.hash, heldQuestionRef{
				key: heldQuestionKey(p.item), state: string(p.item.State), questionID: raised.ID,
			})
			c.audit("held_question_raised", map[string]any{
				"question_id": raised.ID, "state": string(p.item.State),
				"key": heldQuestionKey(p.item), "prd_id": p.item.PRDID, "task_id": p.item.TaskID,
			})
		}
	}
}

// heldQuestionGate returns the question store and the autonomous.questions
// config the pass runs with, or ok=false when either is unwired (the pass
// is then off: no store to read or write through).
func (c *Coordinator) heldQuestionGate() (*QuestionStore, config.AutonomousQuestionsConfig, bool) {
	m := c.m
	m.mu.Lock()
	store := m.decisionStore
	cfgFn := m.decisionGateCfg
	m.mu.Unlock()
	if store == nil || cfgFn == nil {
		return nil, config.AutonomousQuestionsConfig{}, false
	}
	return store, cfgFn(), true
}

// collectHeldItems walks the store's live state at this tick and returns
// one held item per held (item, state) whose state id is covered by
// autonomous.questions.held_states (an absent list covers all five). Only
// covered states are returned, so an uncovered state is invisible to the
// pass.
func (c *Coordinator) collectHeldItems(qcfg config.AutonomousQuestionsConfig, store *QuestionStore) []heldQuestionPending {
	m := c.m
	var out []heldQuestionPending

	add := func(item *HeldItem, linked string) {
		if item.State == "" || !qcfg.HeldStateCovered(string(item.State)) {
			return
		}
		out = append(out, heldQuestionPending{
			item:             item,
			hash:             heldQuestionLineHash(heldQuestionMarker(item)),
			linkedQuestionID: linked,
		})
	}

	aCtxOf := func(prd *PRD) heldQuestionAutoCtx {
		ctx := heldQuestionAutoCtx{automationID: prd.AutomationID}
		for _, a := range m.automations.List() {
			if a.ID != prd.AutomationID {
				continue
			}
			ctx.automationName = a.Name
			if ctx.automationID == "" {
				ctx.automationID = a.ID
			}
			if b := a.Backend; b != "" {
				ctx.backends = []string{b}
			}
			break
		}
		return ctx
	}

	for _, prd := range m.store.ListPRDs() {
		aCtx := aCtxOf(prd)

		// PRD-level holds.
		if prd.Status == PRDBlocked && prd.BlockedReason != "" {
			add(&HeldItem{
				State:          HeldBlocked,
				PRDID:          prd.ID,
				PRDTitle:       prd.Title,
				AutomationID:   aCtx.automationID,
				AutomationName: aCtx.automationName,
				Reason:         prd.BlockedReason,
			}, "")
		}
		if prd.Status == PRDRevisionsAsked {
			add(&HeldItem{
				State:          HeldRevisionRequested,
				PRDID:          prd.ID,
				PRDTitle:       prd.Title,
				AutomationID:   aCtx.automationID,
				AutomationName: aCtx.automationName,
				Reason:         prd.RejectionReason,
			}, "")
		}

		// Task-level holds.
		for si := range prd.Story {
			story := &prd.Story[si]
			for ti := range story.Tasks {
				t := &story.Tasks[ti]
				if t.Status == TaskReviewNeeded {
					var category string
					if t.FailureReason != nil {
						category = t.FailureReason.Category
					}
					reason := t.Error
					if t.ReviewSummary != nil && t.ReviewSummary.Text != "" {
						reason = t.ReviewSummary.Text
					}
					add(&HeldItem{
						State:             HeldReviewHeld,
						PRDID:             prd.ID,
						PRDTitle:          prd.Title,
						TaskID:            t.ID,
						TaskTitle:         t.Title,
						AutomationID:      aCtx.automationID,
						AutomationName:    aCtx.automationName,
						FailureCategory:   category,
						Reason:            reason,
						AttemptsUsed:      t.EscalationFailures,
						AvailableBackends: aCtx.backends,
					}, "")
					continue
				}
				if t.Status == TaskWaitingDecision ||
					(t.Status == TaskWaitingCapacity && IsAwaitDecisionWaitReason(t.WaitReason)) {
					add(&HeldItem{
						State:          HeldAwaitingDecision,
						PRDID:          prd.ID,
						PRDTitle:       prd.Title,
						TaskID:         t.ID,
						TaskTitle:      t.Title,
						AutomationID:   aCtx.automationID,
						AutomationName: aCtx.automationName,
						Reason:         t.WaitReason,
					}, heldQuestionLinkedDecision(store, t))
				}
			}
		}
	}

	// Automation-level holds: a member insert waiting for operator
	// approval does not belong to any PRD, so it is collected from the
	// Automations directly.
	for _, a := range m.automations.List() {
		for i := range a.PendingInserts {
			pi := &a.PendingInserts[i]
			add(&HeldItem{
				State:          HeldPendingInsert,
				PRDID:          pi.PRDID,
				AutomationID:   a.ID,
				AutomationName: a.Name,
				PendingID:      pi.ID,
				Reason:         pi.Reason,
			}, "")
		}
	}
	return out
}

// heldQuestionAutoCtx is the Automation context a held item is raised in.
type heldQuestionAutoCtx struct {
	automationID   string
	automationName string
	backends       []string
}

// heldQuestionLinkedDecision returns the id of the open question a task is
// parked on, when one exists: the question named by an await_decision wait
// reason, or an open operator decision named by the task's spec (the
// decision gate). Empty when the task's hold is not already an open
// question.
func heldQuestionLinkedDecision(store *QuestionStore, t *Task) string {
	if store == nil {
		return ""
	}
	if id := AwaitDecisionQuestionID(t.WaitReason); id != "" {
		if q, ok := store.Get(id); ok && q.State == QuestionOpen {
			return id
		}
	}
	for _, id := range DecisionRefs(t.Spec) {
		if q, ok := store.Get(id); ok && q.State == QuestionOpen {
			return id
		}
	}
	return ""
}

// heldShadowNote records the one shadow-mode note for a held item: an
// append-only TriageNote on the item's Automaton record naming the state
// and the recommended option, once per (item, state) — the marker in the
// note text is the dedupe key, the same one the open question carries in
// mode on. It writes through the existing SaveTriageRecord path, which
// creates the record when none exists yet and keeps the stored one's
// alert time, first sighting and notes otherwise.
func (c *Coordinator) heldShadowNote(p heldQuestionPending, now time.Time) {
	m := c.m
	item := p.item
	if item.PRDID == "" {
		c.audit("held_question_shadow_note_skipped", map[string]any{
			"state": string(item.State), "key": heldQuestionKey(item), "cause": "no_prd",
		})
		return
	}
	s, ok := m.store.SnapshotPRD(item.PRDID)
	if !ok {
		return
	}
	marker := heldQuestionMarker(item)
	notes := []TriageNote{}
	if s.Triage != nil {
		notes = s.Triage.Notes
	}
	for _, n := range notes {
		if n.Source == heldQuestionNoteSource && strings.HasPrefix(n.Text, marker) {
			c.audit("held_question_shadow_note_skipped", map[string]any{
				"state": string(item.State), "key": heldQuestionKey(item), "cause": "already_recorded",
			})
			return
		}
	}
	fresh := c.triageFresh(s, now)
	if fresh == nil {
		fresh = &TriageRecord{PRDID: s.ID, PRDTitle: s.Title, Status: string(s.Status)}
	}
	// SaveTriageRecord keeps the stored record's append-only notes when
	// one exists, so write the note after the record is in place: the
	// SaveTriageRecord then AppendTriageNote pair is the existing path
	// both shadow passes (triage and security) share.
	if err := m.SaveTriageRecord(item.PRDID, fresh); err != nil {
		c.audit("held_question_shadow_note_refused", map[string]any{
			"state": string(item.State), "key": heldQuestionKey(item), "cause": err.Error(),
		})
		return
	}
	text := heldShadowNoteText(item, p.linkedQuestionID, marker)
	if err := m.AppendTriageNote(item.PRDID, heldQuestionNoteSource, text); err != nil {
		c.audit("held_question_shadow_note_refused", map[string]any{
			"state": string(item.State), "key": heldQuestionKey(item), "cause": err.Error(),
		})
		return
	}
	c.audit("held_question_shadow", map[string]any{
		"state": string(item.State), "key": heldQuestionKey(item), "prd_id": item.PRDID,
		"recommended": heldRecommendText(item),
	})
}

// heldShadowNoteText renders the shadow note: the dedupe marker, the state
// and the recommendation, naming the already-open question when the held
// state is covered by one.
func heldShadowNoteText(item *HeldItem, linkedQuestionID, marker string) string {
	rec := HeldRecommendation(item)
	who := heldQuestionWho(item)
	text := fmt.Sprintf("%s — %s on %s: would raise a held question; recommended option %s (%s)",
		marker, string(item.State), who, rec.OptionID, heldOptionText(rec.OptionID))
	if linkedQuestionID != "" {
		text = fmt.Sprintf("%s; already covered by the open question %s", text, linkedQuestionID)
	}
	return text
}

// heldRecommendText renders the recommendation for audit details.
func heldRecommendText(item *HeldItem) string {
	rec := HeldRecommendation(item)
	return rec.OptionID + " (" + heldOptionText(rec.OptionID) + ")"
}

// heldOptionText is the operator-facing label of a recommendation option
// id; the recommendation option is always one of the pass's own options.
func heldOptionText(optionID string) string {
	if l, ok := heldOptionLabels[optionID]; ok {
		return l
	}
	return optionID
}

// heldQuestionKey is the stable identity of a held item: the task, the
// pending insert, or the Automaton it holds on, in that order.
func heldQuestionKey(item *HeldItem) string {
	switch {
	case item.TaskID != "":
		return item.TaskID
	case item.PendingID != "":
		return item.PendingID
	default:
		return item.PRDID
	}
}

// heldQuestionWho renders the human part of a held item's identity.
func heldQuestionWho(item *HeldItem) string {
	switch {
	case item.TaskTitle != "":
		return "task " + item.TaskTitle
	case item.AutomationName != "":
		return "Automation " + item.AutomationName
	default:
		if item.TaskID != "" {
			return "task " + item.TaskID
		}
		if item.AutomationID != "" {
			return "Automation " + item.AutomationID
		}
		return "its Automaton"
	}
}

// heldQuestionMarker is the preimage of a held question's dedupe key:
// "held:<item><state>". Stable across restarts for the same held
// (item, state).
func heldQuestionMarker(item *HeldItem) string {
	return "held:" + heldQuestionKey(item) + ":" + string(item.State)
}

// heldQuestionLineHash is the dedupe key the store uses for
// RaiseUnlessOpenSourceLine: the SHA-256 of the marker, hex-encoded (the
// questionLineSHA256Len the store accepts).
func heldQuestionLineHash(marker string) string {
	sum := sha256.Sum256([]byte(marker))
	return hex.EncodeToString(sum[:])
}

// heldQuestionOpenByHash returns the open held question raised with the
// given dedupe key, if one is open.
func heldQuestionOpenByHash(store *QuestionStore, hash string) (Question, bool) {
	for _, q := range store.List(QuestionFilter{State: QuestionOpen, Limit: maxQuestionListLimit}) {
		if q.SessionID == heldQuestionSessionID && q.SourceLineSHA256 == hash {
			return q, true
		}
	}
	return Question{}, false
}

// heldQuestionOpenByID returns the held question with the given id when it
// is still open; answered and withdrawn questions are not returned, the
// pass never touches them.
func heldQuestionOpenByID(store *QuestionStore, id string) (Question, bool) {
	q, ok := store.Get(id)
	if ok && q.State == QuestionOpen && q.SessionID == heldQuestionSessionID {
		return q, true
	}
	return Question{}, false
}

// heldIndexInit rebuilds the in-memory index from the store on the first
// pass after a restart: one entry per held question in the store
// (whatever its state) whose key the pass can recover, so the pass does
// not lose its withdraw bookkeeping across restarts.
func (c *Coordinator) heldIndexInit(store *QuestionStore) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.heldIndex != nil {
		return
	}
	c.heldIndex = make(map[string]heldQuestionRef)
	for _, q := range store.List(QuestionFilter{Limit: maxQuestionListLimit}) {
		if q.SessionID != heldQuestionSessionID || q.SourceLineSHA256 == "" {
			continue
		}
		key, ok := heldQuestionKeyFromQuestion(&q)
		if !ok {
			continue
		}
		for _, st := range HeldStates {
			if heldQuestionLineHash("held:"+key+":"+string(st)) == q.SourceLineSHA256 {
				c.heldIndex[q.SourceLineSHA256] = heldQuestionRef{key: key, state: string(st), questionID: q.ID}
				break
			}
		}
	}
}

// heldQuestionKeyFromQuestion recovers the item key from a stored held
// question: the task when one is named, the pending insert named in the
// text of a pending-insert question, the Automaton otherwise. ok=false
// when no key can be recovered.
func heldQuestionKeyFromQuestion(q *Question) (string, bool) {
	if q.TaskID != "" {
		return q.TaskID, true
	}
	if g := heldQuestionPendingInsertIDRe.FindStringSubmatch(q.Text); g != nil && g[1] != "" {
		return g[1], true
	}
	if q.PRDID != "" {
		return q.PRDID, true
	}
	return "", false
}

// heldQuestionPendingInsertIDRe extracts the pending insert id the
// table renders in a pending-insert question's text.
var heldQuestionPendingInsertIDRe = regexp.MustCompile(`Pending insert id is (\S+)\.`)

func (c *Coordinator) heldIndexSnapshot() map[string]heldQuestionRef {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]heldQuestionRef, len(c.heldIndex))
	for h, r := range c.heldIndex {
		out[h] = r
	}
	return out
}

func (c *Coordinator) heldIndexSet(hash string, ref heldQuestionRef) {
	c.mu.Lock()
	c.heldIndex[hash] = ref
	c.mu.Unlock()
}

func (c *Coordinator) heldIndexDrop(hash string) {
	c.mu.Lock()
	delete(c.heldIndex, hash)
	c.mu.Unlock()
}
