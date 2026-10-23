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

import "time"

// defaultTaskDeliveryWindow is the per-attempt main-prompt window when the
// caller (or config) supplies window <= 0.
const defaultTaskDeliveryWindow = 60 * time.Second

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
