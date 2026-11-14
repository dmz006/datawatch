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

// Timing for the web-input delivery (submitPastedInput). These are fixed
// internal constants, not operator settings: they only bound how long the
// daemon watches the pane for its own keystrokes, and the per-send settle
// that IS operator-tunable (session.schedule_settle_ms) is applied before
// this runs.
const (
	// pasteVisibleTimeout bounds the wait for the pasted placeholder (or
	// the pasted tail text) to show up near the input prompt.
	pasteVisibleTimeout = 1500 * time.Millisecond
	// pasteVisiblePoll is the pane re-read interval while waiting.
	pasteVisiblePoll = 50 * time.Millisecond
	// pasteRetrySettle is the pause after an Enter before re-reading the
	// pane to see whether the input was accepted.
	pasteRetrySettle = 400 * time.Millisecond
	// pasteMaxEnterRetries is the most extra Enters sent after the first
	// one when the pane still shows the unsent input.
	pasteMaxEnterRetries = 2
	// pasteTailRunes is how much of the last pasted line is used to
	// recognise the text in the pane.
	pasteTailRunes = 24
	// pasteInputRegionLines is how many non-empty bottom lines count as
	// "the input area" when looking for unsent text.
	pasteInputRegionLines = 6
)

// pasteDeliverer is the slice of the tmux layer submitPastedInput needs.
// Both TmuxManager and FakeTmux satisfy it.
type pasteDeliverer interface {
	SendEnter(session string) error
	CapturePaneVisible(session string) (string, error)
}

// pastedTail returns the recognisable end of the pasted text: the last
// non-empty line, trimmed, cut to its final pasteTailRunes runes.
func pastedTail(keys string) string {
	lines := strings.Split(keys, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" {
			continue
		}
		r := []rune(l)
		if len(r) > pasteTailRunes {
			r = r[len(r)-pasteTailRunes:]
		}
		return string(r)
	}
	return ""
}

// inputHoldsText reports whether the pasted tail text is still sitting in
// the input area: it appears on a prompt-affordance line among the last
// few non-empty pane lines, with no assistant output after it.
func inputHoldsText(pane, tail string) bool {
	if tail == "" {
		return false
	}
	lines := strings.Split(StripANSI(pane), "\n")
	var idx []int
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			idx = append(idx, i)
		}
	}
	if len(idx) > pasteInputRegionLines {
		idx = idx[len(idx)-pasteInputRegionLines:]
	}
	for k := len(idx) - 1; k >= 0; k-- {
		l := lines[idx[k]]
		if !strings.Contains(l, tail) {
			continue
		}
		if !promptAffordance(l) && !strings.Contains(l, "│") {
			return false
		}
		for _, j := range idx[k+1:] {
			if looksLikeAssistantOutput(lines[j]) {
				return false
			}
		}
		return true
	}
	return false
}

// submitPastedInput presses Enter for text already pushed literally into
// the pane. It (1) polls the pane, bounded, until the pasted placeholder or
// the pasted tail is visible; (2) sends Enter; (3) for pasted/long/multi-line
// input re-reads the pane after a short settle and, only while the input
// is still unsent (placeholder or text in the input box, no assistant
// activity), sends one more Enter, at most pasteMaxEnterRetries times; and
// (4) never sends another Enter once the placeholder is gone or the
// assistant is active, so a first Enter that worked is exactly one submit.
// Short single-line input without a placeholder keeps the fast path: one
// Enter after the bounded poll, no verification wait. Empty input is a bare
// Enter with no polling. Uses the injectable pasteSleep/pasteNow clock.
func submitPastedInput(tm pasteDeliverer, session, keys string) error {
	tail := pastedTail(keys)
	if tail == "" {
		return tm.SendEnter(session)
	}

	sawPlaceholder := false
	deadline := pasteNow().Add(pasteVisibleTimeout)
	for {
		pane, err := tm.CapturePaneVisible(session)
		if err != nil {
			break // pane unreadable: do not stall, just submit
		}
		if pastePending(pane) {
			sawPlaceholder = true
			break
		}
		if strings.Contains(StripANSI(pane), tail) {
			break
		}
		if !pasteNow().Add(pasteVisiblePoll).Before(deadline) {
			break
		}
		pasteSleep(pasteVisiblePoll)
	}

	if err := tm.SendEnter(session); err != nil {
		return err
	}
	if !sawPlaceholder && !strings.Contains(keys, "\n") && len(keys) <= 200 {
		return nil
	}
	for retry := 0; retry < pasteMaxEnterRetries; retry++ {
		pasteSleep(pasteRetrySettle)
		pane, err := tm.CapturePaneVisible(session)
		if err != nil {
			return nil
		}
		if !pastePending(pane) && !inputHoldsText(pane, tail) {
			return nil
		}
		if err := tm.SendEnter(session); err != nil {
			return err
		}
	}
	return nil
}
