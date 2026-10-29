// Decision line: the one read model of operator decisions, newest first.
// REST (GET /api/autonomous/decisions) and MCP (autonomous_decisions) both
// serve what BuildDecisionLine returns. It reads the question store, not the
// Automaton decision log, so a decision that was withdrawn or expired shows
// with that state, and it joins each decision to the tasks of its Automaton
// whose spec names it, with the ones currently held on it.
package autonomous

import "time"

// DecisionLineStore is the part of the question store the decision line
// reads. *QuestionStore satisfies it.
type DecisionLineStore interface {
	List(f QuestionFilter) []Question
	CurrentAnswer(questionID string) (Answer, bool)
}

// DecisionLineAnswer is the current answer of an answered decision. The
// quote is verbatim, exactly as stored, with the sha256 recorded with it.
type DecisionLineAnswer struct {
	ID          string     `json:"id"`
	Quote       string     `json:"quote"`
	QuoteSHA256 string     `json:"quote_sha256"`
	Actor       string     `json:"actor"`
	Surface     string     `json:"surface"`
	AnsweredAt  *time.Time `json:"answered_at"`
}

// DecisionLineWait is a task held on a decision, with the reason it shows.
type DecisionLineWait struct {
	TaskID     string `json:"task_id"`
	WaitReason string `json:"wait_reason"`
}

// DecisionLineEntry is one decision on the line.
type DecisionLineEntry struct {
	ID             string              `json:"id"`
	Question       string              `json:"question"`
	Label          string              `json:"label"`
	State          string              `json:"state"`
	AskedBy        string              `json:"asked_by"`
	AskedAt        *time.Time          `json:"asked_at"`
	PRDID          string              `json:"prd_id"`
	TaskIDs        []string            `json:"task_ids"`
	WaitingTaskIDs []string            `json:"waiting_task_ids"`
	WaitingTasks   []DecisionLineWait  `json:"waiting_tasks"`
	Answer         *DecisionLineAnswer `json:"answer"`
}

// BuildDecisionLine returns the decisions matching f, newest first, capped at
// f's limit (default 100, maximum 1000, applied by the store). lookup finds an
// Automaton by id; a nil lookup, or an id it does not know, leaves the task
// lists empty. The answer is null until the decision is answered, and then it
// is the current (latest) answer.
func BuildDecisionLine(f QuestionFilter, store DecisionLineStore, lookup func(prdID string) (*PRD, bool)) []DecisionLineEntry {
	out := []DecisionLineEntry{}
	if store == nil {
		return out
	}
	// One Automaton is read once however many of its decisions are listed.
	type taskRefs struct {
		named   map[string][]string
		waiting map[string][]DecisionLineWait
	}
	cache := map[string]*taskRefs{}
	refsFor := func(prdID string) *taskRefs {
		if r, ok := cache[prdID]; ok {
			return r
		}
		r := &taskRefs{named: map[string][]string{}, waiting: map[string][]DecisionLineWait{}}
		cache[prdID] = r
		if prdID == "" || lookup == nil {
			return r
		}
		prd, ok := lookup(prdID)
		if !ok || prd == nil {
			return r
		}
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				t := &prd.Story[si].Tasks[ti]
				for _, ref := range DecisionRefs(t.Spec) {
					r.named[ref] = append(r.named[ref], t.ID)
					if normTaskStatus(t.Status) == TaskWaitingDecision {
						r.waiting[ref] = append(r.waiting[ref], DecisionLineWait{TaskID: t.ID, WaitReason: t.WaitReason})
					}
				}
			}
		}
		return r
	}

	for _, q := range store.List(f) {
		refs := refsFor(q.PRDID)
		e := DecisionLineEntry{
			ID:             q.ID,
			Question:       q.Text,
			Label:          q.Label,
			State:          string(q.State),
			AskedBy:        q.AskedBy,
			AskedAt:        q.CreatedAt,
			PRDID:          q.PRDID,
			TaskIDs:        append([]string{}, refs.named[q.ID]...),
			WaitingTaskIDs: []string{},
			WaitingTasks:   append([]DecisionLineWait{}, refs.waiting[q.ID]...),
		}
		for _, w := range e.WaitingTasks {
			e.WaitingTaskIDs = append(e.WaitingTaskIDs, w.TaskID)
		}
		if q.State == QuestionAnswered {
			if a, ok := store.CurrentAnswer(q.ID); ok {
				e.Answer = &DecisionLineAnswer{
					ID:          a.ID,
					Quote:       a.Quote,
					QuoteSHA256: a.QuoteSHA256,
					Actor:       a.Actor,
					Surface:     a.Surface,
					AnsweredAt:  a.CreatedAt,
				}
			}
		}
		out = append(out, e)
	}
	return out
}
