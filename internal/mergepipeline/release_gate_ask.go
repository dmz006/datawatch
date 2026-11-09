package mergepipeline

// Release decision question: when a release is ready to ship and the operator
// has to say yes or no, this raises the one question that asks. It does not
// decide whether to ship; the answer comes back through the question store.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// ReleaseQuestionAsker is the asked_by of the question raised for a release
// that is waiting for the operator's go-ahead.
const ReleaseQuestionAsker = "release-approval"

// Release decision choices.
const (
	releaseOptionShip   = "yes"
	releaseOptionHold   = "no"
	releaseQuestionTail = "Ship this release?"
)

// ReleaseCheck is one check the release ran and whether it passed.
type ReleaseCheck struct {
	Name   string
	Passed bool
	Detail string
}

// ReleasePlan is the merge plan a release question describes.
type ReleasePlan struct {
	// Project is the project directory name or identifier the release is for.
	Project string
	// AutomatonID is the Automaton the release belongs to; it links the
	// question to that Automaton.
	AutomatonID string
	// Size is the release size, such as patch, minor or major.
	Size string
	// Branches are the branches the release merges in.
	Branches []string
	// Version is the version the release would get.
	Version string
	// Checks are the results of the checks run before the release.
	Checks []ReleaseCheck
	// Steps are the steps that would run if the release ships, such as
	// tag, push and deploy, in order.
	Steps []string
}

// releaseSourceLine is the stable identity of a release question: the same
// project, Automaton and size always map to the same key, so a re-run finds
// the question that is already open rather than raising a second one.
func releaseSourceLine(plan ReleasePlan) string {
	sum := sha256.Sum256([]byte(ReleaseQuestionAsker + "\x00" + plan.Project + "\x00" + plan.AutomatonID + "\x00" + plan.Size))
	return hex.EncodeToString(sum[:])
}

// releaseQuestionText writes the merge plan in plain words and ends with the
// choice the operator has to make.
func releaseQuestionText(plan ReleasePlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Release plan for %s", plan.Project)
	if plan.AutomatonID != "" {
		fmt.Fprintf(&b, " (Automaton %s)", plan.AutomatonID)
	}
	b.WriteString(".\n")
	if plan.Size != "" {
		fmt.Fprintf(&b, "Size: %s.\n", plan.Size)
	}
	if plan.Version != "" {
		fmt.Fprintf(&b, "This release would get version %s.\n", plan.Version)
	}
	if len(plan.Branches) > 0 {
		fmt.Fprintf(&b, "It merges %d branch(es): %s.\n", len(plan.Branches), strings.Join(plan.Branches, ", "))
	} else {
		b.WriteString("It merges no branches.\n")
	}
	if len(plan.Checks) > 0 {
		b.WriteString("Checks:\n")
		for _, c := range plan.Checks {
			state := "passed"
			if !c.Passed {
				state = "failed"
			}
			fmt.Fprintf(&b, "- %s: %s", c.Name, state)
			if c.Detail != "" {
				fmt.Fprintf(&b, " (%s)", c.Detail)
			}
			b.WriteString("\n")
		}
	} else {
		b.WriteString("No checks were recorded.\n")
	}
	if len(plan.Steps) > 0 {
		fmt.Fprintf(&b, "If shipped, these steps run in order: %s.\n", strings.Join(plan.Steps, ", then "))
	} else {
		b.WriteString("If shipped, no steps are listed.\n")
	}
	b.WriteString(releaseQuestionTail)
	text := b.String()
	if len(text) > 7000 {
		text = text[:7000] + "\n[shortened]\n" + releaseQuestionTail
	}
	return text
}

// RaiseReleaseDecision raises the yes/no question that asks the operator to
// ship the release described by plan. A question already open for the same
// project, Automaton and size is found rather than raised again: questionID is
// then that open question and raised is false. With no raiser wired it returns
// ErrNoQuestionChannel and raises nothing.
func RaiseReleaseDecision(raiser QuestionRaiser, plan ReleasePlan) (questionID string, raised bool, err error) {
	if raiser == nil {
		return "", false, ErrNoQuestionChannel
	}
	q := autonomous.Question{
		Label:     "decision",
		AskedBy:   ReleaseQuestionAsker,
		AskerKind: autonomous.QuestionAskerSystem,
		Context:   autonomous.QuestionContextNone,
		Text:      releaseQuestionText(plan),
		Options: []autonomous.QuestionOption{
			{ID: releaseOptionShip, Label: "yes (ship it)",
				Tradeoffs: "The release is tagged, pushed and deployed as listed above."},
			{ID: releaseOptionHold, Label: "no (do not ship)",
				Tradeoffs: "Nothing is tagged, pushed or deployed; the release waits."},
		},
		Default:          releaseOptionHold,
		SourceLineSHA256: releaseSourceLine(plan),
	}
	if plan.AutomatonID != "" {
		q.Context = autonomous.QuestionContextPrd
		q.PRDID = plan.AutomatonID
	}
	got, isNew, rerr := raiser.RaiseUnlessOpenSourceLine(q)
	if rerr != nil {
		return "", false, fmt.Errorf("raise the release question: %w", rerr)
	}
	return got.ID, isNew, nil
}
