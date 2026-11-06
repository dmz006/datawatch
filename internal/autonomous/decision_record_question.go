package autonomous

import "strings"

// The system asker a question built from a decision record carries.
const decisionQuestionAsker = "datawatch"

// BuildQuestionFromDecision builds the queue question for a decision record.
// It is a pure function of the record: no caller prose goes in, so a decision
// always reads the same way. The text is the title, a blank line, the
// situation, a blank line, then "Deciding: " and what is being decided, each
// part trimmed. The options keep the record order, the recommendation and the
// default come from the record, and the label is the record label (the
// default label when it is empty). The question is asked by the system and is
// not blocking; risk and priority stay empty. DecisionRecordID is the record
// id and Source is QuestionSourceDecisionRecord.
//
// The context follows the links, first match wins: an Automaton gives context
// prd with that Automaton id, else a registry row gives registry_row, else a
// document gives proposal with the document as the proposal path, else the
// context is none. The links that do not fit stay on the record.
func BuildQuestionFromDecision(rec DecisionRecord) Question {
	label := strings.TrimSpace(rec.Label)
	if label == "" {
		label = DecisionDefaultLabel
	}
	q := Question{
		Label:            label,
		AskedBy:          decisionQuestionAsker,
		AskerKind:        QuestionAskerSystem,
		Text:             decisionTrimmedText(rec),
		Default:          rec.Default,
		Blocking:         false,
		Source:           QuestionSourceDecisionRecord,
		DecisionRecordID: rec.ID,
	}

	q.Options = make([]QuestionOption, 0, len(rec.Options))
	for _, opt := range rec.Options {
		q.Options = append(q.Options, QuestionOption{
			ID:        opt.ID,
			Label:     strings.TrimSpace(opt.Label),
			Tradeoffs: strings.TrimSpace(opt.Tradeoff),
		})
	}

	if rec.Recommendation != nil {
		q.Recommendation = &QuestionRecommendation{
			OptionID:  rec.Recommendation.OptionID,
			Rationale: strings.TrimSpace(rec.Recommendation.Reason),
		}
	}

	automaton := strings.TrimSpace(rec.Links.Automaton)
	row := strings.TrimSpace(rec.Links.RegistryRow)
	doc := strings.TrimSpace(rec.Links.Doc)
	switch {
	case automaton != "":
		q.Context = QuestionContextPrd
		q.PRDID = automaton
	case row != "":
		q.Context = QuestionContextRegistryRow
		q.RegistryRowKey = row
	case doc != "":
		q.Context = QuestionContextProposal
		q.ProposalPath = doc
	default:
		q.Context = QuestionContextNone
	}
	return q
}

// decisionTrimmedText lays out the question text from the trimmed title,
// situation and deciding parts.
func decisionTrimmedText(rec DecisionRecord) string {
	return strings.TrimSpace(rec.Title) + "\n\n" +
		strings.TrimSpace(rec.Situation) + "\n\nDeciding: " +
		strings.TrimSpace(rec.Deciding)
}
