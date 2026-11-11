// Branch refresh conflict question. When bringing an Automaton's work branch
// up to date with its base hits conflicts the layer will not resolve, the
// operator is asked what to do through the ordinary question store.
package autonomous

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// BranchRefreshQuestionAsker is the asker name on the conflict question.
const BranchRefreshQuestionAsker = "branch-refresh"

// Option ids of the branch refresh conflict question.
const (
	BranchRefreshOptionResolve = "resolve_by_hand"
	BranchRefreshOptionRetry   = "retry_later"
	BranchRefreshOptionLeave   = "leave_alone"
)

// BranchRefreshQuestionRaiser is the slice of QuestionStore the raise needs.
type BranchRefreshQuestionRaiser interface {
	RaiseUnlessOpenSourceLine(q Question) (Question, bool, error)
}

func branchRefreshSourceLine(prdID string, files []string) string {
	sorted := append([]string(nil), files...)
	sort.Strings(sorted)
	sum := sha256.Sum256([]byte(BranchRefreshQuestionAsker + "\x00" + prdID + "\x00" + strings.Join(sorted, "\x00")))
	return hex.EncodeToString(sum[:])
}

// BuildBranchRefreshQuestion builds the conflict question for an Automaton
// naming the conflicting files. The default is to leave the branch alone.
func BuildBranchRefreshQuestion(prdID, base string, files []string) Question {
	var b strings.Builder
	fmt.Fprintf(&b, "Bringing the branch of Automaton %s up to date with %s hit conflicts in %d file(s):\n", prdID, base, len(files))
	for _, f := range files {
		fmt.Fprintf(&b, "- %s\n", f)
	}
	b.WriteString("The branch was left exactly as it was. What should happen next?")
	return Question{
		Label:     "decision",
		AskedBy:   BranchRefreshQuestionAsker,
		AskerKind: QuestionAskerSystem,
		Context:   QuestionContextPrd,
		PRDID:     prdID,
		Text:      b.String(),
		Options: []QuestionOption{
			{ID: BranchRefreshOptionResolve, Label: "resolve by hand",
				Tradeoffs: "Refreshing stays stopped for this Automaton until you reset it."},
			{ID: BranchRefreshOptionRetry, Label: "retry later",
				Tradeoffs: "Refreshing is allowed again and the next pass tries the merge again."},
			{ID: BranchRefreshOptionLeave, Label: "leave the branch alone",
				Tradeoffs: "The branch is not touched and refreshing stays stopped."},
		},
		Default:          BranchRefreshOptionLeave,
		SourceLineSHA256: branchRefreshSourceLine(prdID, files),
	}
}

// RaiseBranchRefreshQuestion raises the conflict question through the store
// and returns the question and whether it is new (an open question for the
// same Automaton and files is reused).
func RaiseBranchRefreshQuestion(raiser BranchRefreshQuestionRaiser, prdID, base string, files []string) (Question, bool, error) {
	if raiser == nil {
		return Question{}, false, fmt.Errorf("no question store")
	}
	if prdID == "" || len(files) == 0 {
		return Question{}, false, fmt.Errorf("branch refresh question needs an Automaton and conflicting files")
	}
	q, isNew, err := raiser.RaiseUnlessOpenSourceLine(BuildBranchRefreshQuestion(prdID, base, files))
	if err != nil {
		return Question{}, false, fmt.Errorf("raise the branch refresh question: %w", err)
	}
	return q, isNew, nil
}

// ApplyBranchRefreshAnswer applies the chosen option to the Automaton:
// retry later clears Stopped; leave alone and resolve by hand keep it
// stopped (resolve by hand until the operator resets it).
func (m *Manager) ApplyBranchRefreshAnswer(prdID, optionID string) error {
	var stopped bool
	var note string
	switch optionID {
	case BranchRefreshOptionRetry:
		stopped, note = false, "branch refresh: operator chose retry later; refreshing allowed again"
	case BranchRefreshOptionLeave:
		stopped, note = true, "branch refresh: operator chose to leave the branch alone; refreshing stays stopped"
	case BranchRefreshOptionResolve:
		stopped, note = true, "branch refresh: operator will resolve by hand; refreshing stays stopped until reset"
	default:
		return fmt.Errorf("unknown branch refresh option %q", optionID)
	}
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %s not found", prdID)
	}
	st := BranchRefresh{}
	if prd.BranchRefresh != nil {
		st = *prd.BranchRefresh
	}
	st.Stopped = stopped
	prd.BranchRefresh = &st
	prd.Decisions = trimDecisions(append(prd.Decisions, Decision{
		At: time.Now(), Kind: "branch_refresh", Actor: "operator", Note: note,
	}))
	prd.UpdatedAt = time.Now()
	return m.store.SavePRD(prd)
}
