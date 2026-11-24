// Screen-based readiness for typing a task into a claude-code session.
//
// DeliverOneShotTask used to wait for the session state to read waiting_input
// and for a ❯ row to be on screen. The state is the weak half: it is decided
// by pane-prompt patterns that never match the real idle row (Claude Code
// draws "❯", a no-break space, then a placeholder suggestion) and by a quiet
// gap that never elapses while the pane keeps changing, so an idle, ready
// Claude can read running for as long as the wait lasts. And the ❯ check alone
// is too weak the other way: it accepts a box that already holds an unsent
// paste and a pane that is mid-turn.
//
// This file reads the screen itself. A box is ready when no choice dialog is
// open, the agent is not working, and the input row holds nothing but a
// suggestion. The suggestion's wording is never inspected: any text that is
// neither a paste chip nor the start of the task counts as a suggestion.

package session

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// inputBoxKind is what the screen says about the input box.
type inputBoxKind int

const (
	inputBoxNone      inputBoxKind = iota // no input row on screen
	inputBoxEmpty                         // ready: empty, or only a suggestion
	inputBoxHoldsText                     // the box holds a paste chip or the task text
	inputBoxDialog                        // a choice dialog is waiting for an answer
	inputBoxBusy                          // the agent is working on a turn
)

// numberedChoiceRow matches a numbered choice row ("❯ 1. ...") that a dialog
// draws with the same ❯ cursor as the input row. The gap after ❯ may be a
// plain or a no-break space.
var numberedChoiceRow = regexp.MustCompile(`^❯[\s\x{00a0}]*\d+\.\s`)

// inputBoxHeldChips are the markers of text that is in the box but not sent.
var inputBoxHeldChips = []string{"[Pasted text #", "[Image #"}

// inputRowText returns the text after the ❯ of an input row, with plain and
// no-break spaces trimmed.
func inputRowText(row string) string {
	rest := strings.TrimPrefix(strings.TrimSpace(row), "❯")
	return strings.Trim(rest, " \t ")
}

// isBoxSeparator reports whether a trimmed pane line is the horizontal rule
// that frames the input box.
func isBoxSeparator(l string) bool {
	return strings.HasPrefix(l, "──") || strings.HasPrefix(l, "━━")
}

// taskSnippet is the normalized start of a task: the text the box shows when
// the task has been typed but not sent.
func taskSnippet(task string) string {
	for _, l := range strings.Split(task, "\n") {
		if t := strings.Join(strings.Fields(l), " "); t != "" {
			if r := []rune(strings.ToLower(t)); len(r) > 24 {
				return string(r[:24])
			}
			return strings.ToLower(t)
		}
	}
	return ""
}

// classifyInputBox reads one pane capture and says whether the input box is
// ready for a task. The detail names the line that decided a dialog, busy or
// holds-text answer. Pure: no tmux, no clock, no session state.
func classifyInputBox(pane, task string) (inputBoxKind, string) {
	text := StripANSI(pane)
	lower := strings.ToLower(text)
	for _, phrase := range activeChoiceDialogPhrases {
		if strings.Contains(lower, phrase) {
			return inputBoxDialog, phrase
		}
	}
	lines := nonEmptyTail(text, 40)
	boxIdx := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], "❯") {
			boxIdx = i
			break
		}
	}
	if boxIdx < 0 {
		return inputBoxNone, ""
	}
	if numberedChoiceRow.MatchString(lines[boxIdx]) {
		return inputBoxDialog, lines[boxIdx]
	}
	if paneShowsWork(lines) {
		for _, l := range lines {
			if isToolRunningLine(l) || isSpinnerLine(l) {
				return inputBoxBusy, l
			}
		}
		return inputBoxBusy, "esc to interrupt"
	}
	// The box is the ❯ row plus the wrapped rows that follow it, up to the
	// rule that closes it.
	content := inputRowText(lines[boxIdx])
	for _, l := range lines[boxIdx+1:] {
		if isBoxSeparator(l) {
			break
		}
		if strings.HasPrefix(l, "⏵") {
			break
		}
		content += " " + l
	}
	content = strings.Join(strings.Fields(content), " ")
	for _, chip := range inputBoxHeldChips {
		if strings.Contains(content, chip) {
			return inputBoxHoldsText, content
		}
	}
	if snip := taskSnippet(task); snip != "" && strings.Contains(strings.ToLower(content), snip) {
		return inputBoxHoldsText, content
	}
	return inputBoxEmpty, content
}

// describeDeliveryScreen turns a screen classification into the sentence that
// ends a delivery failure reason.
func describeDeliveryScreen(kind inputBoxKind, detail string) string {
	short := func(s string) string {
		if r := []rune(s); len(r) > 80 {
			return string(r[:80]) + "…"
		}
		return s
	}
	switch kind {
	case inputBoxDialog:
		return fmt.Sprintf("a startup dialog was still waiting for an answer (%q)", short(detail))
	case inputBoxBusy:
		return fmt.Sprintf("the agent was working, not at an empty prompt (%q)", short(detail))
	case inputBoxHoldsText:
		return fmt.Sprintf("the input box already holds text, so nothing more was typed (%q)", short(detail))
	case inputBoxEmpty:
		return "the input box was ready"
	}
	return "no input box was on screen"
}

// deliveryScreenStable is how long the pane must stay unchanged before a
// session whose state is only "running" is treated as idle at its prompt.
const deliveryScreenStable = time.Second

// awaitOneShotPrompt polls on ~1 s ticks (m.now()/m.sleep so a test can drive
// it with virtual time) until the screen shows a ready input box, until window
// elapses, or — checked on every poll first — until the session has exited:
// it is missing from the store, in a terminal state, or its tmux session no
// longer exists. The screen decides, not the state name: a session that reads
// waiting_input is ready the moment the box is; a session that reads running
// is ready once the pane has also stood still for deliveryScreenStable, since
// running is also what a working agent reads. A rate-limited or stalled
// session is never typed into. The second result describes the last screen
// seen, for the failure reason.
func (m *Manager) awaitOneShotPrompt(fullID, task string, window time.Duration) (oneShotPromptOutcome, string) {
	deadline := m.now().Add(window)
	why := "no input box was on screen"
	var lastPane string
	var stableSince time.Time
	for {
		sess, ok := m.store.Get(fullID)
		if !ok || sess.State.IsTerminal() || (sess.TmuxSession != "" && !m.tmux.SessionExists(sess.TmuxSession)) {
			return oneShotSessionExited, why
		}
		switch {
		case sess.State != StateWaitingInput && sess.State != StateRunning:
			why = fmt.Sprintf("the session state was %s", sess.State)
		case sess.TmuxSession == "":
			why = "the session has no terminal"
		default:
			pane, err := m.tmux.CapturePaneVisible(sess.TmuxSession)
			if err != nil || strings.TrimSpace(pane) == "" {
				why = "the terminal could not be read"
				break
			}
			now := m.now()
			if pane != lastPane {
				lastPane, stableSince = pane, now
			}
			kind, detail := classifyInputBox(pane, task)
			why = describeDeliveryScreen(kind, detail)
			if kind != inputBoxEmpty {
				break
			}
			if sess.State == StateWaitingInput || now.Sub(stableSince) >= deliveryScreenStable {
				return oneShotReady, why
			}
			why = "the screen was still changing"
		}
		if !m.now().Before(deadline) {
			return oneShotTimedOut, why
		}
		m.sleep(time.Second)
	}
}

// taskDeliveryReadyGrace is how long the daemon's own delivery trigger waits
// for the channel bridge's ready call to start delivery first.
const taskDeliveryReadyGrace = 20 * time.Second

// armTaskDelivery starts delivery of fullID's task without the channel
// bridge: it takes the delivery claim and runs DeliverOneShotTask. If the
// ready call already holds the claim, or the task was delivered, it does
// nothing — the claim is shared, so the task is typed once whichever trigger
// wins. It blocks until delivery ends; callers run it in the background.
func (m *Manager) armTaskDelivery(fullID string, attempts int, window, backoff time.Duration) {
	sess, ok := m.GetSession(fullID)
	if !ok || sess == nil || sess.Task == "" || sess.State.IsTerminal() {
		return
	}
	input := sess.Task
	if sess.OneShot && strings.HasPrefix(input, "!") {
		input += `; echo "DATAWATCH_COMPLETE: shell task done"`
	}
	if !m.ClaimTaskDelivery(sess.FullID) {
		return
	}
	if err := m.DeliverOneShotTask(sess.FullID, input, attempts, window, backoff); err != nil {
		m.debugf("armTaskDelivery: %s: %v", sess.FullID, err)
	}
}

// recordTaskDeliveryFailure keeps the reason a delivery gave up for fullID.
func (m *Manager) recordTaskDeliveryFailure(fullID, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deliveryFailure == nil {
		m.deliveryFailure = make(map[string]string)
	}
	m.deliveryFailure[fullID] = reason
}

// TaskDeliveryFailure returns the reason the last delivery for fullID gave up,
// or "" when none did in this process. Callers that end a launch use it to
// say why the worker never got its task.
func (m *Manager) TaskDeliveryFailure(fullID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.deliveryFailure[fullID]
}

// armTaskDeliveryAfterStart is the daemon's own trigger for a freshly started
// claude-code session whose launch leaves the task off the command line (the
// channel-enabled launch): after taskDeliveryReadyGrace, if the bridge's ready
// call has not claimed delivery, the daemon delivers the task itself. Without
// it a session whose bridge never reports ready — or whose ready call is lost —
// came up at an empty prompt and the task was never typed.
func (m *Manager) armTaskDeliveryAfterStart(ctx context.Context, sess *Session, backendObj interface{}, resumeID string) {
	if sess == nil || sess.BackendFamily != "claude-code" || sess.Task == "" || resumeID != "" {
		return
	}
	if ce, ok := backendObj.(interface{ ChannelEnabled() bool }); !ok || !ce.ChannelEnabled() {
		return
	}
	attempts, window := 3, defaultTaskDeliveryWindow
	if m.cfg != nil {
		attempts = m.cfg.Session.EffectiveTaskDeliveryAttempts()
		window = m.cfg.Session.EffectiveTaskDeliveryWindow()
	}
	fullID := sess.FullID
	m.goBG(func() {
		select {
		case <-time.After(taskDeliveryReadyGrace):
		case <-ctx.Done():
			return
		}
		m.armTaskDelivery(fullID, attempts, window, config.TaskDeliveryBackoff)
	})
}
