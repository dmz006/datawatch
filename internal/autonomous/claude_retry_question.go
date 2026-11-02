package autonomous

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
)

const claudeRetryHoldAsker = "claude-retry-limit"

// raiseClaudeRetryHoldQuestion raises the queue question for a task held by the
// Claude retry limit. It does nothing when no question store is wired or the
// operator question channel is off; the task's WaitReason carries the text then.
// A question already open for the same hold is not raised twice. A failure is
// logged and never changes the hold.
func (m *Manager) raiseClaudeRetryHoldQuestion(prd *PRD, t *Task, limit int) {
	store, enabled := m.operatorQuestionChannel()
	if !enabled {
		return
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("claude-retry-hold\x00%s\x00%s\x00%d", prd.ID, t.ID, t.ClaudeRetries)))
	q := Question{
		Label:     infraHoldQuestionLabel,
		AskedBy:   claudeRetryHoldAsker,
		AskerKind: QuestionAskerSystem,
		Context:   QuestionContextTask,
		PRDID:     prd.ID,
		TaskID:    t.ID,
		Text:      ClaudeRetryHoldQuestion(t, limit),
		Options: []QuestionOption{
			{ID: reviewActionRetry, Label: "Retry the task now",
				Tradeoffs: "Runs the task again; raise usage.burn_guard.retry_limit_claude first or it is held again."},
			{ID: reviewActionCancel, Label: "Cancel the task",
				Tradeoffs: "Cancels the task; tasks that depend on it do not run."},
		},
		Default:          reviewActionCancel,
		Priority:         QuestionPriorityHigh,
		Blocking:         true,
		SourceLineSHA256: hex.EncodeToString(sum[:]),
	}
	if _, _, err := store.RaiseUnlessOpenSourceLine(q); err != nil {
		log.Printf("[autonomous] prd=%s task=%s: raise claude retry hold question: %v", prd.ID, t.ID, err)
	}
}
