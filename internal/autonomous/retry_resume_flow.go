package autonomous

// Retry resume: when a task attempt fails verification, the next attempt may
// continue the same worker session instead of starting a new one. The pure
// decision lives in decideRetryResume; this file wires it to the live
// session (liveness, context size, the existing send-input hook) and builds
// the single line that is typed into the session.

import (
	"fmt"
	"log"
	"strings"
)

// retryResumeFraming opens the line sent into a resumed session. The failure
// summary and the retry guidance follow it; the summary text itself comes from
// the existing retry prompt builder (escalationRetryHint).
const retryResumeFraming = "[automaton retry] Your previous attempt at this task failed verification. This session is resumed with its context. The failure summary follows. Fix what it names in the same worktree, then finish with your DATAWATCH_COMPLETE marker."

// retryResumeMaxLineBytes bounds the summary put on the single input line, so
// a long verifier output cannot flood the session's input.
const retryResumeMaxLineBytes = 4000

// retryResumeBackends lists the worker backends whose session takes a
// follow-up prompt and keeps its context across it. Any other backend, or an
// unknown one, starts a fresh session on retry.
var retryResumeBackends = map[string]bool{
	"claude-code": true,
}

// retryResumeBackendSupported reports whether a retry on backend may resume.
func retryResumeBackendSupported(backend string) bool {
	return retryResumeBackends[backend]
}

// SetSessionContextTokensFn wires the context-size measurement the retry
// resume limit reads. It returns the tokens the session has consumed, or 0
// when unknown. Nil leaves the size unknown, which resumes when the other
// checks pass.
func (m *Manager) SetSessionContextTokensFn(fn func(sessionID string) int) {
	m.mu.Lock()
	m.sessionContextTokensFn = fn
	m.mu.Unlock()
}

// previousAttemptBackend returns the backend the task's most recent attempt
// ran on. It falls back to the backend the task would resolve to, so a
// recorded attempt without a backend still gets a decision.
func (m *Manager) previousAttemptBackend(prd *PRD, t *Task) string {
	var recorded string
	m.store.WithLock(func() {
		if n := len(t.Attempts); n > 0 {
			recorded = t.Attempts[n-1].Backend
		}
	})
	if recorded != "" {
		return recorded
	}
	story := findStory(prd, t.StoryID)
	backend, _ := admittedWorker(t, story, prd, m.AutomationForPRD(prd), m.cfgExecutionBackend())
	return backend
}

// cfgExecutionBackend reads the fallback worker backend under the manager lock.
func (m *Manager) cfgExecutionBackend() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.ExecutionBackend
}

// tryRetryResume decides, after a verifier failure, whether the next attempt
// resumes the previous session. When it does, it types the failure summary
// and the next-step instruction into that session through the send-input
// hook and returns true; the caller then waits on the same session. It
// returns false when the session should not be resumed or the send fails, and
// the caller starts a fresh session as it always has.
//
// The reason is the outcome the attempt carries onto its record: the
// decideRetryResume reason, or the fallback reason when a decision to resume
// could not be carried out. A task with no session has nothing to resume, so
// that case reports the same reason decideRetryResume gives for a dead one.
func (m *Manager) tryRetryResume(prd *PRD, t *Task, hint string) (resumed bool, reason string) {
	sessionID := t.SessionID
	if sessionID == "" {
		return false, sessionEndedResumeReason
	}
	m.mu.Lock()
	aliveFn, contextFn := m.sessionAliveFn, m.sessionContextTokensFn
	maxTokens := m.cfg.RetryResumeMaxContextTokens
	m.mu.Unlock()
	sendFn := m.sessionContinueLineFnLocked()

	alive := aliveFn != nil && aliveFn(sessionID)
	contextTokens := 0
	if contextFn != nil {
		contextTokens = contextFn(sessionID)
	}
	backend := m.previousAttemptBackend(prd, t)
	resume, reason := decideRetryResume(retryResumeInput{
		SessionAlive:          alive,
		BackendSupportsResume: retryResumeBackendSupported(backend),
		ContextTokens:         contextTokens,
		MaxContextTokens:      maxTokens,
	})
	if !resume {
		log.Printf("[autonomous] %s/%s retry: session %s not resumed: %s", prd.ID, t.ID, sessionID, reason)
		return false, reason
	}
	if sendFn == nil {
		log.Printf("[autonomous] %s/%s retry: session %s not resumed: no send-input hook; starting a fresh attempt", prd.ID, t.ID, sessionID)
		return false, resumeFallbackNoSendReason
	}
	line := retryResumeLine(m.escalationRetryHint(prd, t, hint))
	if err := sendFn(sessionID, line); err != nil {
		log.Printf("[autonomous] %s/%s retry: send into session %s failed, starting a fresh attempt: %v", prd.ID, t.ID, sessionID, err)
		return false, resumeFallbackSendFailedReason
	}
	log.Printf("[autonomous] %s/%s retry: resumed session %s with the failure summary", prd.ID, t.ID, sessionID)
	return true, reason
}

// retryResumeLine joins the resume framing and the retry prompt text into one
// input line. Newlines become separators so the session's input is not
// submitted early, and the text is capped.
func retryResumeLine(summary string) string {
	body := strings.TrimSpace(summary)
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\n", " | ")
	if len(body) > retryResumeMaxLineBytes {
		body = body[:retryResumeMaxLineBytes] + escalationTruncationMarker
	}
	if body == "" {
		return retryResumeFraming
	}
	return fmt.Sprintf("%s Failure summary: %s", retryResumeFraming, body)
}
