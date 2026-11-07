// OneShot task-delivery budget helpers.
//
// DeliverOneShotTask waits for the worker's main prompt before typing the
// task. That wait used to be a hard-coded 15 s constant baked into the
// manager while the API caller hard-coded its own attempt count and backoff,
// so the two could drift and the combined worst-case delivery time could
// not be reasoned about or tested. These helpers give the budget one home:
// the per-attempt window falls back to a production default when the caller
// does not supply one, the outcome split (ready / timed out / session
// exited) lets one attempt observe that the worker has already died, and
// the Manager.sleep seam (next to the existing Manager.now()/clock seams in
// scan_timing.go) lets a test drive the whole retry budget with virtual
// time instead of burning wall clock.

package session

import (
	"strings"
	"time"
)

// defaultTaskDeliveryWindow is the per-attempt main-prompt window when the
// caller (or config) supplies window <= 0.
const defaultTaskDeliveryWindow = 60 * time.Second

// maxJudgeDeliveryWindow caps the scaled main-prompt window for judge sessions.
const maxJudgeDeliveryWindow = 15 * time.Minute

// judgeSessionNamePrefix is the name prefix of guardrail-judge sessions.
const judgeSessionNamePrefix = "autonomous:guardrail:"

// deliveryWindowForPrompt returns the main-prompt delivery window for a prompt
// of promptLines lines: baseSeconds plus ceil(promptLines/1000) times
// perThousandSeconds, capped at maxJudgeDeliveryWindow. Negative inputs count
// as zero.
func deliveryWindowForPrompt(baseSeconds, perThousandSeconds, promptLines int) time.Duration {
	if baseSeconds < 0 {
		baseSeconds = 0
	}
	if perThousandSeconds < 0 {
		perThousandSeconds = 0
	}
	if promptLines < 0 {
		promptLines = 0
	}
	thousands := (promptLines + 999) / 1000
	d := time.Duration(baseSeconds)*time.Second + time.Duration(thousands)*time.Duration(perThousandSeconds)*time.Second
	if d > maxJudgeDeliveryWindow || d < 0 {
		return maxJudgeDeliveryWindow
	}
	return d
}

// SetJudgeDeliveryWaitPerThousandLines sets the extra main-prompt wait, in
// seconds per thousand prompt lines, that judge sessions get. Zero disables
// the scaling.
func (m *Manager) SetJudgeDeliveryWaitPerThousandLines(seconds int) {
	m.mu.Lock()
	m.judgeDeliveryPerThousand = seconds
	m.mu.Unlock()
}

// scaledDeliveryWindow returns the window for session fullID's prompt: the
// scaled value for judge sessions, window unchanged for everything else.
func (m *Manager) scaledDeliveryWindow(fullID, input string, window time.Duration) time.Duration {
	sess, ok := m.GetSession(fullID)
	if !ok || sess == nil || !strings.HasPrefix(sess.Name, judgeSessionNamePrefix) {
		return window
	}
	m.mu.Lock()
	per := m.judgeDeliveryPerThousand
	m.mu.Unlock()
	lines := strings.Count(input, "\n") + 1
	scaled := deliveryWindowForPrompt(int(window/time.Second), per, lines)
	if scaled < window {
		return window
	}
	return scaled
}

// oneShotPromptOutcome is the result of one awaitOneShotPrompt wait.
type oneShotPromptOutcome int

const (
	oneShotReady         oneShotPromptOutcome = iota + 1 // main prompt visible, awaiting input
	oneShotTimedOut                                      // window elapsed, prompt still not visible
	oneShotSessionExited                                 // session missing, terminal, or its tmux session is gone
)

// sleep is time.Sleep behind one injection point: a nil Manager.sleepFn
// means production behaviour (real sleep), tests supply a fake so the
// DeliverOneShotTask backoff loop can run without wall time.
func (m *Manager) sleep(d time.Duration) {
	if m.sleepFn != nil {
		m.sleepFn(d)
		return
	}
	time.Sleep(d)
}
