package mergepipeline

// Release decision answer: once the release question was raised (see
// RaiseReleaseDecision), the operator's answer is read back through the
// question store. ReleaseDecisionAnswer reduces that answer to the one fact
// the gate needs — did the operator say yes? — without deciding anything
// itself.

import (
	"github.com/dmz006/datawatch/internal/autonomous"
)

// The real store must satisfy the interface; this fails at build time if
// either method's signature drifts.
var _ ReleaseAnswerReader = (*autonomous.QuestionStore)(nil)

// ReleaseAnswerReader is the read-only surface of *autonomous.QuestionStore
// that reading a release decision needs. The real store satisfies it; tests
// can fake it without a store file.
type ReleaseAnswerReader interface {
	Get(id string) (autonomous.Question, bool)
	Answers(questionID string) ([]autonomous.AnswerView, bool)
}

// ReleaseDecisionAnswer reports the operator's answer to the release question
// with questionID. yes is true only when the question exists, is in the
// answered state, and its current answer chose the "yes" option. An
// unanswered, withdrawn, missing or "no" question all return yes false; for
// an answered question answered is true whether the answer is "yes" or "no",
// so a held release is always distinguishable from an undecided one. The
// current answer is the newest one that has not been superseded — a changed
// mind supersedes the earlier answer, so only it counts.
func ReleaseDecisionAnswer(reader ReleaseAnswerReader, questionID string) (yes bool, answered bool) {
	if reader == nil || questionID == "" {
		return false, false
	}
	q, ok := reader.Get(questionID)
	if !ok || q.State != autonomous.QuestionAnswered {
		return false, false
	}
	views, ok := reader.Answers(questionID)
	if !ok || len(views) == 0 {
		// The store sets the answered state only when an answer exists, so
		// this is defensive, not a real case.
		return false, false
	}
	// Newest first: the first view that has not been superseded is the
	// current answer.
	for i := len(views) - 1; i >= 0; i-- {
		if views[i].Superseded {
			continue
		}
		return views[i].ChosenOptionID == releaseOptionShip, true
	}
	return false, true
}
