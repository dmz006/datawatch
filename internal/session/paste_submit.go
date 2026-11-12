package session

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrPromptNotSubmitted is the sentinel returned by
// confirmPromptSubmitted when a pasted task prompt was re-submitted (or
// left as-is) but no assistant activity was observed within the timeout
// window. Callers use errors.Is to detect the condition without string
// matching.
var ErrPromptNotSubmitted = errors.New("prompt not submitted: paste was never accepted as input and no assistant activity followed")

// pasteSleep and pasteNow are the injectable clock used by
// confirmPromptSubmitted. Production uses the real ones; tests swap them
// for immediate fakes so the 30-second polling loop runs without any
// real-time sleep.
var (
	pasteSleep = time.Sleep
	pasteNow   = time.Now
)

// pastedTextMarker is the placeholder claude-code (and other
// bracketed-paste TUIs) render inside the input line for pasted content
// that has not been expanded/submitted: "[Pasted text #N +M lines]".
const pastedTextMarker = "[Pasted text #"

// pastePromptWindow is how far above the marker we still count the marker
// as "on or near the input prompt" (the prompt affordances — ❯ selector
// or the input-box frame — live within a couple of lines of the text).
const pastePromptWindow = 2

// looksLikeAssistantOutput reports whether a stripped pane line carries
// the signatures of the assistant actually responding (tool-use lines,
// thinking spinners, bullet commentary) as opposed to chrome such as
// prompts, footers, or the pasted input itself.
func looksLikeAssistantOutput(line string) bool {
	for _, sig := range []string{
		"⏺",      // claude-code tool-use line marker
		"✻",      // thinking spinner frame
		"✳",      // thinking spinner frame
		"⣷", "⣾", // braille spinner frames
		"• ", // assistant bullet line
		"Done in ",
	} {
		if strings.Contains(line, sig) {
			return true
		}
	}
	return false
}

// promptAffordance reports whether a stripped pane line is part of the
// input-prompt area: the ❯ selector or the input-box frame chars.
func promptAffordance(line string) bool {
	return strings.Contains(line, "❯") ||
		strings.Contains(line, "┌") ||
		strings.Contains(line, "└")
}

// pastePending reports whether the visible pane still shows a
// "[Pasted text #N ...]" marker sitting on (or within a few lines of)
// the input prompt with no assistant output after it — i.e. the pasted
// text is accepted into the TUI's input buffer but has not been
// submitted. ANSI is stripped before matching. Pure: no I/O, no clock.
func pastePending(pane string) bool {
	lines := strings.Split(StripANSI(pane), "\n")

	// The LAST marker wins: older conversations may have markers higher
	// up the transcript; only a still-pending one near the current
	// prompt is a problem.
	last := -1
	for i, l := range lines {
		if strings.Contains(l, pastedTextMarker) {
			last = i
		}
	}
	if last < 0 {
		return false
	}

	// On or near the input prompt: a prompt affordance must sit within
	// a couple of lines above the marker or on its line.
	lo := last - pastePromptWindow
	if lo < 0 {
		lo = 0
	}
	nearPrompt := false
	for j := lo; j <= last && j < len(lines); j++ {
		if promptAffordance(lines[j]) {
			nearPrompt = true
			break
		}
	}
	if !nearPrompt {
		return false
	}

	// No assistant output after the marker. The first clearly-assistant
	// line means the prompt was already submitted and work has begun.
	for j := last + 1; j < len(lines); j++ {
		if looksLikeAssistantOutput(lines[j]) {
			return false
		}
	}
	return true
}

// confirmPromptSubmitted verifies that a task prompt pasted into a
// claude-code session's TUI was actually submitted. It takes the tmux
// layer (TmuxAPI, so tests can use FakeTmux), the tmux session name, an
// injected assistant-activity check (session state moved to running, or
// the session log grew past the paste point), a timeout and a poll
// interval.
//
// Behaviour:
//   - Paste not pending (marker already gone, or assistant output
//     already on screen): return nil and send NO keys at all.
//   - Paste pending: send Enter exactly ONCE, then poll (every `poll`,
//     bounded by `timeout`) until assistantActive() is true.
//   - If the timeout elapses with no assistant activity, return an error
//     wrapping ErrPromptNotSubmitted.
//
// The clock is injectable (pasteSleep/pasteNow) so tests run with the
// full 30-second budget in zero real time.
func confirmPromptSubmitted(tmux TmuxAPI, tmuxSession string, assistantActive func() bool, timeout, poll time.Duration) error {
	pane, err := tmux.CapturePaneVisible(tmuxSession)
	if err != nil {
		return fmt.Errorf("confirm prompt submitted: capture pane %s: %w", tmuxSession, err)
	}
	if !pastePending(pane) {
		// Already submitted (or nothing pending): do not send Enter at
		// all — a stray Enter in an idle TUI is exactly the class of bug
		// this function exists to prevent.
		return nil
	}
	// Pending: submit with exactly one Enter. SendKeys with an empty
	// payload strips no text and issues only the final Enter keypress
	// (after the -l/Enter settle), and FakeTmux records it as a single
	// send-keys call.
	if err := tmux.SendKeys(tmuxSession, ""); err != nil {
		return fmt.Errorf("confirm prompt submitted: send Enter to %s: %w", tmuxSession, err)
	}
	deadline := pasteNow().Add(timeout)
	for {
		if assistantActive != nil && assistantActive() {
			return nil
		}
		// Do not sleep past the deadline: stop as soon as the next poll
		// would land at or after it.
		if !pasteNow().Add(poll).Before(deadline) {
			return fmt.Errorf("%w (session %s, waited %s)", ErrPromptNotSubmitted, tmuxSession, timeout)
		}
		pasteSleep(poll)
	}
}
