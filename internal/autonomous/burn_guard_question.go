// Burn guard questions: the ask_on_change wiring between the burn guard's
// ladder decisions (internal/burnguard) and the question store.
//
// The functions here take a BurnGuardQuestionStore interface so they are testable
// with a fake; *QuestionStore satisfies it, as does the server's QuestionsAPI.
// They never end or hold a session; they only raise and withdraw questions.
package autonomous

import (
	"errors"
	"log"
	"strings"

	"github.com/dmz006/datawatch/internal/burnguard"
)

const (
	// BurnGuardQuestionAsker is the asked_by of a guard question: it is also how a
	// guard question is found again. It matches the asker the server's burn guard
	// notifier has raised with since the start of the feature.
	BurnGuardQuestionAsker = "burn-guard"
	// burnGuardQuestionActor is the actor of a withdrawal the operator did not make.
	burnGuardQuestionActor = "burn-guard"
	// BurnGuardQuestionLabel is the question's label.
	BurnGuardQuestionLabel = "decision"

	// BurnGuardReasonSuperseded is the reason the current open guard question
	// is withdrawn when a newer rung change raises one.
	BurnGuardReasonSuperseded = "superseded by a newer burn guard rung change"
	// BurnGuardReasonAskOff is the reason an open guard question is withdrawn
	// when a rung change happens with ask_on_change off.
	BurnGuardReasonAskOff = "ask_on_change is off, so the burn guard alerts only"
	// BurnGuardReasonStaleAtStart is the reason an open guard question is
	// withdrawn at start when the guard's state no longer requires it.
	BurnGuardReasonStaleAtStart = "at start the burn guard no longer applies the rung this question refers to"
)

// BurnGuardQuestionStore is the surface of the question store the wiring
// uses: *QuestionStore and the server's QuestionsAPI both satisfy it.
type BurnGuardQuestionStore interface {
	Raise(q Question) (Question, error)
	Withdraw(id, actor, reason string) (Question, error)
	List(f QuestionFilter) []Question
}

// BurnGuardQuestion builds the guard question for one rung change: the
// ladder's sentence, a note about running sessions if given, and the two
// ways to answer it. Raising it is ApplyBurnGuardAskOnChange's job.
func BurnGuardQuestion(text string) Question {
	return Question{
		Label:     BurnGuardQuestionLabel,
		AskedBy:   BurnGuardQuestionAsker,
		AskerKind: QuestionAskerSystem,
		Context:   QuestionContextNone,
		Risk:      QuestionRiskLow,
		Text:      strings.TrimSpace(text),
		Options: []QuestionOption{
			{ID: "keep", Label: "Leave it as is",
				Tradeoffs: "The burn guard stays at this rung and the setting keeps its changed value."},
			{ID: "release", Label: "Release",
				Tradeoffs: "This release only records your choice and performs nothing, so the guard keeps the rung until it itself releases it."},
		},
		Default: "keep",
	}
}

// burnGuardOpenQuestions returns the open guard questions, newest first; a
// nil store answers to none.
func burnGuardOpenQuestions(store BurnGuardQuestionStore) []Question {
	if store == nil {
		return nil
	}
	return store.List(QuestionFilter{State: QuestionOpen, Asker: BurnGuardQuestionAsker, Limit: 100})
}

// withdrawBurnGuardQuestions withdraws the named guard questions with the
// reason and returns the ones actually withdrawn. A question that another
// actor has already answered or withdrawn is left alone and not counted.
func withdrawBurnGuardQuestions(store BurnGuardQuestionStore, qs []Question, reason string) []Question {
	var out []Question
	for _, q := range qs {
		if _, err := store.Withdraw(q.ID, burnGuardQuestionActor, reason); err != nil {
			if err == ErrQuestionNotFound || err == ErrQuestionNotOpen {
				continue // already answered or withdrawn by somebody else
			}
			log.Printf("burn guard question %s: withdraw: %v", q.ID, err)
			continue
		}
		out = append(out, q)
	}
	return out
}

// WithdrawOpenBurnGuardQuestions withdraws every open guard question with the
// reason and returns the ones actually withdrawn.
func WithdrawOpenBurnGuardQuestions(store BurnGuardQuestionStore, reason string) []Question {
	if store == nil {
		return nil
	}
	return withdrawBurnGuardQuestions(store, burnGuardOpenQuestions(store), reason)
}

// RaiseBurnGuardQuestion raises the guard question for one rung change: it
// first withdraws every open guard question (reason: superseded), so the
// guard never leaves more than one open.
func RaiseBurnGuardQuestion(store BurnGuardQuestionStore, text string) (Question, error) {
	if store == nil {
		return Question{}, errors.New("burn guard question: no question store wired")
	}
	WithdrawOpenBurnGuardQuestions(store, BurnGuardReasonSuperseded)
	q := BurnGuardQuestion(text)
	raised, err := store.Raise(q)
	if err != nil {
		return Question{}, err
	}
	return raised, nil
}

// ApplyBurnGuardAskOnChange applies the ask_on_change behaviour of one ladder
// decision to the store:
//   - ask_on_change on and the ladder wrote a QuestionText: withdraw the
//     guard's open question (superseded), then raise the new one, so at most
//     one stays open;
//   - ask_on_change off: raise nothing and withdraw the guard's open question
//     if any, so turning the setting off clears the last one on the next
//     change;
//   - a hold (no change) or a change that left no QuestionText (a no-op rung,
//     a disabled release): do nothing, and the current open question stays
//     open for the operator.
//
// The guard's own decision log stays the single source of truth; this only
// raises and withdraws questions.
func ApplyBurnGuardAskOnChange(store BurnGuardQuestionStore, askOnChange bool, d burnguard.Decision, running string) {
	if store == nil {
		return
	}
	if askOnChange && strings.TrimSpace(d.QuestionText) != "" {
		if _, err := RaiseBurnGuardQuestion(store, d.QuestionText+running); err != nil {
			log.Printf("burn guard question: raise: %v", err)
		}
		return
	}
	if !askOnChange && d.Changed {
		WithdrawOpenBurnGuardQuestions(store, BurnGuardReasonAskOff)
	}
}

// PruneBurnGuardQuestionsAtStart runs at daemon start: the guard has just
// loaded its persisted ladder state, and any open guard question a previous
// run raised that the state no longer requires is withdrawn. When the ladder
// applies no rung (it is at rung 0) every open guard question is stale; when
// it applies a rung the newest open question is the current one and is kept,
// while any older open one is withdrawn, so the guard stays at most one open
// question. Returns the questions actually withdrawn.
func PruneBurnGuardQuestionsAtStart(store BurnGuardQuestionStore, state burnguard.LadderState) []Question {
	open := burnGuardOpenQuestions(store)
	if len(open) == 0 {
		return nil
	}
	if len(state.Applied) == 0 {
		return withdrawBurnGuardQuestions(store, open, BurnGuardReasonStaleAtStart)
	}
	return withdrawBurnGuardQuestions(store, open[1:], BurnGuardReasonStaleAtStart)
}
