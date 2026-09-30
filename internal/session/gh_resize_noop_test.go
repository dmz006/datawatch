// v8.37.3 — ResizeTmux skips a same-size resize.
//
// Found live: opening a session in the PWA or on Android Auto sends
// resize_term unconditionally, fitting the terminal to whatever THAT
// device's viewport computes — regardless of whether the tmux window is
// already that size. A redundant resize-window call still makes the TUI
// repaint (fresh SIGWINCH), which StartScreenCapture's next poll reads
// as "content changed", firing MarkChannelEvent(EventRunning) and
// flipping a WaitingInput session to Running purely from opening it.

package session

import "testing"

func TestGH_ResizeTmux_SkipsWhenSizeUnchanged(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	_ = mgr.SaveSession(&Session{
		ID: "cc01", FullID: "testhost-cc01", TmuxSession: "cs-cc01",
		State: StateWaitingInput,
	})
	fake.WindowSizes = map[string][2]int{"cs-cc01": {120, 40}}

	mgr.ResizeTmux("testhost-cc01", 120, 40)

	if fake.Count("resize") != 0 {
		t.Errorf("same-size resize should be skipped; got calls: %+v", fake.Calls)
	}
}

func TestGH_ResizeTmux_ResizesWhenSizeDiffers(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	_ = mgr.SaveSession(&Session{
		ID: "cc02", FullID: "testhost-cc02", TmuxSession: "cs-cc02",
		State: StateWaitingInput,
	})
	fake.WindowSizes = map[string][2]int{"cs-cc02": {80, 24}}

	mgr.ResizeTmux("testhost-cc02", 120, 40)

	if fake.Count("resize") != 1 {
		t.Errorf("differing size should trigger exactly one resize; got calls: %+v", fake.Calls)
	}
}

func TestGH_ResizeTmux_ResizesWhenCurrentSizeUnknown(t *testing.T) {
	mgr, fake := newTestManagerWithFake(t)
	_ = mgr.SaveSession(&Session{
		ID: "cc03", FullID: "testhost-cc03", TmuxSession: "cs-cc03",
		State: StateWaitingInput,
	})
	// No WindowSizes entry — WindowSize() errors, ResizeTmux must fall
	// back to resizing rather than silently no-op'ing forever.
	mgr.ResizeTmux("testhost-cc03", 120, 40)

	if fake.Count("resize") != 1 {
		t.Errorf("unknown current size should still trigger a resize; got calls: %+v", fake.Calls)
	}
}
