// Taking over task deliveries the last daemon never verified.
//
// The delivery claim is saved before anything is typed, so a daemon that stops
// mid-delivery leaves a claim that proves nothing. At boot the screen and the
// recorded output decide: a task the worker visibly has is marked verified,
// text still sitting unsent in the box is left alone, and only an empty box
// that holds still is typed into again.

package session

import (
	"context"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// claimScreen is what an unverified delivery claim looks like at boot.
type claimScreen int

const (
	claimUnreadable claimScreen = iota // no input box to judge by
	claimDelivered                     // the worker has the task
	claimUnsent                        // the box holds text that was never sent
	claimBoxEmpty                      // a ready, empty box and no sign of the task
)

const (
	// claimEmptyStable is how long an empty box must stay unchanged before
	// the task is typed again, so a turn that has only just been submitted
	// and not yet drawn is never typed over.
	claimEmptyStable = 3 * time.Second
	// claimPoll is how often the screen is read while waiting to decide.
	claimPoll = 500 * time.Millisecond
	// claimScrollbackLines is how much screen history is searched for the task.
	claimScrollbackLines = 500
	// claimShortSnippet is the length below which the start of a task is too
	// common a text to count when found anywhere on screen.
	claimShortSnippet = 8
)

// verifyTaskClaimLocked records that a pending delivery claim reached the
// session. A session with no pending claim is left as it is. Call with m.mu held.
func (m *Manager) verifyTaskClaimLocked(fullID string) {
	sess, ok := m.store.Get(fullID)
	if !ok || !sess.TaskDeliveryUnverified {
		return
	}
	now := m.now()
	sess.TaskDeliveryUnverified = false
	sess.TaskVerifiedAt = &now
	_ = m.store.Save(sess)
}

// taskDeliveryProven reports whether fullID's task is known to have reached
// the session, or is being typed again by this daemon right now. The second
// case keeps boot adoption from ending a worker while its retype runs; a retype
// that fails ends the session on the normal delivery failure path.
func (m *Manager) taskDeliveryProven(fullID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.redelivering[fullID] {
		return true
	}
	if sess, ok := m.store.Get(fullID); ok && sess.TaskDeliveryUnverified {
		return false
	} else if ok && sess.TaskDeliveredAt != nil {
		return true
	}
	_, ok := m.taskDeliveredAt[fullID]
	return ok
}

func (m *Manager) setRedelivering(fullID string, on bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !on {
		delete(m.redelivering, fullID)
		return
	}
	if m.redelivering == nil {
		m.redelivering = make(map[string]bool)
	}
	m.redelivering[fullID] = true
}

// taskOnScreen reports whether screen history shows the start of task, or the
// chip Claude Code draws for a long paste.
func taskOnScreen(screen, task string) bool {
	snip := taskSnippet(task)
	if snip == "" {
		return false
	}
	text := StripANSI(screen)
	if len([]rune(snip)) < claimShortSnippet {
		for _, l := range strings.Split(text, "\n") {
			t := strings.TrimSpace(l)
			for _, mark := range []string{agentPromptMarker, ">"} {
				if rest, ok := strings.CutPrefix(t, mark); ok &&
					strings.ToLower(strings.Join(strings.Fields(rest), " ")) == snip {
					return true
				}
			}
		}
		return false
	}
	flat := strings.ToLower(strings.Join(strings.Fields(text), " "))
	return strings.Contains(flat, snip) || strings.Contains(flat, "[pasted text")
}

// readTaskClaim judges one session's unverified claim from its recorded output
// and its screen. It types nothing. The second result is the visible pane, the
// third says what decided it.
func (m *Manager) readTaskClaim(fullID string) (claimScreen, string, string) {
	sess, ok := m.store.Get(fullID)
	if !ok {
		return claimUnreadable, "", "the session is gone"
	}
	snap := m.store.Snapshot(sess)
	if logShowsAssistantTurn(snap.LogFile) {
		return claimDelivered, "", "the recorded output holds an answer"
	}
	pane, err := m.tmux.CapturePaneVisible(snap.TmuxSession)
	if err != nil || strings.TrimSpace(pane) == "" {
		return claimUnreadable, "", "the terminal could not be read"
	}
	kind, detail := classifyInputBox(pane, snap.Task)
	why := describeDeliveryScreen(kind, detail)
	switch kind {
	case inputBoxBusy:
		return claimDelivered, pane, why
	case inputBoxHoldsText:
		return claimUnsent, pane, why
	}
	history, err := m.tmux.CapturePaneScrollback(snap.TmuxSession, claimScrollbackLines)
	if err != nil {
		// Without the history an empty box proves nothing: do not type.
		return claimUnreadable, pane, "the screen history could not be read"
	}
	if taskOnScreen(history, snap.Task) || taskOnScreen(pane, snap.Task) {
		return claimDelivered, pane, "the task is on screen above the input box"
	}
	if kind == inputBoxEmpty {
		return claimBoxEmpty, pane, why
	}
	return claimUnreadable, pane, why
}

// settleTaskClaim records the outcomes that need no typing and reports whether
// the claim still has to be watched for an empty box.
func (m *Manager) settleTaskClaim(fullID string, kind claimScreen, why string) (stillOpen bool) {
	switch kind {
	case claimDelivered:
		m.mu.Lock()
		m.verifyTaskClaimLocked(fullID)
		m.mu.Unlock()
		m.journalf("[session] task delivery for %s verified at boot: %s\n", fullID, why)
		return false
	case claimUnsent:
		m.journalf("[session] task delivery for %s not verified at boot, nothing typed: %s\n", fullID, why)
		return false
	}
	return true
}

// recoverUnverifiedDeliveries takes over every claim the last daemon left
// unverified on a live daemon-typed session. The cheap verdicts are recorded
// before it returns, so the boot steps that follow see them; a retype runs in
// the background.
func (m *Manager) recoverUnverifiedDeliveries(ctx context.Context) {
	for _, rec := range m.store.List() {
		sess := m.store.Snapshot(rec)
		if sess.Hostname != m.hostname || !sess.DaemonDeliversTask || !sess.TaskDeliveryUnverified ||
			sess.Task == "" || sess.TmuxSession == "" {
			continue
		}
		if sess.State != StateRunning && sess.State != StateWaitingInput {
			continue
		}
		if !m.tmux.SessionExists(sess.TmuxSession) {
			continue
		}
		fullID := sess.FullID
		if m.guardEarlyExit(fullID) {
			continue // the agent is gone: nothing to deliver to
		}
		kind, _, why := m.readTaskClaim(fullID)
		if !m.settleTaskClaim(fullID, kind, why) {
			continue
		}
		m.setRedelivering(fullID, true)
		m.goBG(func() {
			defer m.setRedelivering(fullID, false)
			m.retypeTask(ctx, fullID)
		})
	}
}

// retypeTask waits for an empty box that holds still, then delivers the task
// under the claim already saved on the session. The claim is never released
// first, so a channel bridge ready call cannot type the task a second time.
// When no such box appears within the delivery window, nothing is typed and the
// session is left as it is.
func (m *Manager) retypeTask(ctx context.Context, fullID string) {
	attempts, window := 3, defaultTaskDeliveryWindow
	if m.cfg != nil {
		attempts = m.cfg.Session.EffectiveTaskDeliveryAttempts()
		window = m.cfg.Session.EffectiveTaskDeliveryWindow()
	}
	deadline := m.now().Add(window)
	var lastPane string
	var since time.Time
	for {
		if ctx.Err() != nil {
			return
		}
		sess, ok := m.store.Get(fullID)
		if !ok {
			return
		}
		snap := m.store.Snapshot(sess)
		if snap.State.IsTerminal() || !snap.TaskDeliveryUnverified {
			return
		}
		kind, pane, why := m.readTaskClaim(fullID)
		if !m.settleTaskClaim(fullID, kind, why) {
			return
		}
		now := m.now()
		switch {
		case kind != claimBoxEmpty:
			lastPane = ""
		case pane != lastPane:
			lastPane, since = pane, now
		case now.Sub(since) >= claimEmptyStable:
			m.journalf("[session] task delivery for %s was never verified and the input box is empty: typing the task again\n", fullID)
			input := snap.Task
			if snap.OneShot && strings.HasPrefix(input, "!") {
				input += `; echo "DATAWATCH_COMPLETE: shell task done"`
			}
			m.mu.Lock()
			m.taskDeliveredAt[fullID] = now // arm the task-echo suppression
			m.mu.Unlock()
			if err := m.DeliverOneShotTask(fullID, input, attempts, window, config.TaskDeliveryBackoff); err != nil {
				m.journalf("[session] task delivery for %s failed after the restart: %v\n", fullID, err)
			}
			return
		}
		if !now.Before(deadline) {
			m.journalf("[session] task delivery for %s not verified, nothing typed: no empty input box within %s (%s)\n", fullID, window, why)
			return
		}
		m.sleep(claimPoll)
	}
}
