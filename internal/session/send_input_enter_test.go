package session

// Fake-tmux tests for the web-input delivery sequence: paste the text,
// wait (bounded) for the pasted placeholder, press Enter, and re-press Enter
// a bounded number of times only while the input is still unsent.
//
// Everything runs on FakeTmux with the injectable pasteSleep/pasteNow clock
// (useTestClock), so no test sleeps for real. sieIsolateTmux additionally
// points tmux at a stub that records any invocation and fails, so a test
// that reached a real tmux server would fail instead of touching one. No
// daemon is started.

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/llm/backends/ollama"
	"github.com/dmz006/datawatch/internal/llm/backends/opencode"
	"github.com/dmz006/datawatch/internal/llm/backends/openwebui"
)

// sieIsolateTmux makes any real tmux contact impossible and detectable: TMUX
// is cleared, TMUX_TMPDIR points at an empty temp dir (so a real tmux binary
// could never find the operator's socket), and PATH holds only a stub `tmux`
// that logs its argv and exits 1. The returned func fails the test if the
// stub was ever invoked.
func sieIsolateTmux(t *testing.T) (assertUntouched func()) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("tmux stub uses /bin/sh")
	}
	dir := t.TempDir()
	logFile := filepath.Join(dir, "tmux-invoked.log")
	stub := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + logFile + "\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_TMPDIR", t.TempDir())
	t.Setenv("PATH", dir)
	return func() {
		t.Helper()
		if b, err := os.ReadFile(logFile); err == nil && len(b) > 0 {
			t.Fatalf("a test reached the tmux binary: %s", b)
		}
	}
}

// sieOps returns the delivery-relevant calls in order: pushes, pane
// captures and Enter presses.
func sieOps(f *FakeTmux) []string {
	var out []string
	for _, c := range f.CallsSnapshot() {
		switch c.Op {
		case "send-keys-settle", "send-keys", "send-keys-literal", "capture-visible", "enter":
			out = append(out, c.Op)
		}
	}
	return out
}

func sieWantOps(t *testing.T, f *FakeTmux, want []string) {
	t.Helper()
	if got := sieOps(f); !reflect.DeepEqual(got, want) {
		t.Fatalf("call sequence = %v\n want %v", got, want)
	}
}

func sieIndexOf(ops []string, op string, last bool) int {
	idx := -1
	for i, o := range ops {
		if o == op {
			idx = i
			if !last {
				return i
			}
		}
	}
	return idx
}

const (
	siePlaceholder = "──────\n❯ [Pasted text #1 +40 lines]\n──────"
	sieWorking     = "⏺ working on it\n✻ Thinking"
)

// sieLongMessage is multi-line, so delivery verifies after the Enter.
func sieLongMessage() string {
	return strings.Repeat("a line of the pasted task\n", 40) + "final words of the task"
}

// (a) paste then Enter accepted first time.
func TestSendInputEnter_AcceptedFirstTime(t *testing.T) {
	assertUntouched := sieIsolateTmux(t)
	tc := useTestClock(t)
	f := newSubmitFake("s")
	// Capture 1 (before Enter): placeholder shown. Capture 2 (after the one
	// Enter): placeholder gone, assistant working.
	f.PaneScript["s"] = []string{siePlaceholder, sieWorking}

	if err := f.SendKeysWithSettle("s", sieLongMessage(), 0); err != nil {
		t.Fatal(err)
	}
	if got := enterCount(f); got != 1 {
		t.Fatalf("Enter count = %d, want exactly 1", got)
	}
	if got := f.Count("send-keys-settle"); got != 1 {
		t.Fatalf("submits = %d, want exactly 1", got)
	}
	if got := f.Count("send-keys"); got != 0 {
		t.Fatalf("bare send-keys count = %d, want 0", got)
	}
	sieWantOps(t, f, []string{"send-keys-settle", "capture-visible", "enter", "capture-visible"})
	if tc.slept != pasteRetrySettle {
		t.Errorf("slept %v, want one retry settle (%v)", tc.slept, pasteRetrySettle)
	}
	assertUntouched()
}

// (b) Enter lost once: the placeholder survives the first Enter.
func TestSendInputEnter_LostOnce(t *testing.T) {
	assertUntouched := sieIsolateTmux(t)
	tc := useTestClock(t)
	f := newSubmitFake("s")
	// Capture 1: placeholder. Capture 2 (after Enter 1): placeholder still
	// there, so Enter 2 goes out. Capture 3 (after Enter 2): cleared.
	f.PaneScript["s"] = []string{siePlaceholder, siePlaceholder, sieWorking}

	if err := f.SendKeysWithSettle("s", sieLongMessage(), 0); err != nil {
		t.Fatal(err)
	}
	if got := enterCount(f); got != 2 {
		t.Fatalf("Enter count = %d, want exactly 2", got)
	}
	if got := f.Count("send-keys-settle"); got != 1 {
		t.Fatalf("submits = %d, want exactly 1", got)
	}
	sieWantOps(t, f, []string{
		"send-keys-settle", "capture-visible", "enter", "capture-visible", "enter", "capture-visible",
	})
	if want := 2 * pasteRetrySettle; tc.slept != want {
		t.Errorf("slept %v, want %v", tc.slept, want)
	}
	assertUntouched()
}

// (c) Enter lost on every attempt: the retry stops at the fixed maximum.
func TestSendInputEnter_LostEveryAttempt_StopsAtMaximum(t *testing.T) {
	assertUntouched := sieIsolateTmux(t)
	tc := useTestClock(t)
	f := newSubmitFake("s")
	f.PaneScript["s"] = []string{siePlaceholder} // the last entry repeats forever

	done := make(chan error, 1)
	go func() { done <- f.SendKeysWithSettle("s", sieLongMessage(), 0) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("delivery returned %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("delivery did not return: the retry loop is unbounded")
	}

	if pasteMaxEnterRetries < 1 || pasteMaxEnterRetries > 5 {
		t.Fatalf("pasteMaxEnterRetries = %d, want a small fixed bound (1..5)", pasteMaxEnterRetries)
	}
	if got, want := enterCount(f), 1+pasteMaxEnterRetries; got != want {
		t.Fatalf("Enter count = %d, want the fixed maximum %d", got, want)
	}
	want := []string{"send-keys-settle", "capture-visible", "enter"}
	for i := 0; i < pasteMaxEnterRetries; i++ {
		want = append(want, "capture-visible", "enter")
	}
	sieWantOps(t, f, want)
	if got, wantSlept := tc.slept, time.Duration(pasteMaxEnterRetries)*pasteRetrySettle; got != wantSlept {
		t.Errorf("slept %v, want %v (one settle per retry, nothing unbounded)", got, wantSlept)
	}
	assertUntouched()
}

// sieChunkTmux is FakeTmux with the real TmuxManager.SendKeysWithSettle
// chunking shape: the text goes out as send-keys -l chunks of at most
// tmuxSendChunk bytes, then the shared submitPastedInput runs. It exists so
// the chunk-then-Enter ordering can be asserted without a tmux binary and
// without the real inter-chunk sleeps.
type sieChunkTmux struct{ *FakeTmux }

func (c sieChunkTmux) SendKeysWithSettle(session, keys string, _ time.Duration) error {
	keys = trimTrailingNewlines(keys)
	sent := keys
	for len(keys) > 0 {
		n := len(keys)
		if n > tmuxSendChunk {
			n = tmuxSendChunk
		}
		if err := c.SendKeysLiteral(session, keys[:n]); err != nil {
			return err
		}
		keys = keys[n:]
	}
	return submitPastedInput(c.FakeTmux, session, sent)
}

// (d) long multi-line message with several attachment paths, split over
// more than one chunk: the Enter comes after the last literal chunk.
func TestSendInputEnter_MultiChunk_EnterAfterLastChunk(t *testing.T) {
	assertUntouched := sieIsolateTmux(t)
	useTestClock(t)
	f := newSubmitFake("s")
	f.PaneScript["s"] = []string{siePlaceholder, sieWorking}

	paths := []string{
		"/home/dmz/.datawatch/attachments/a1b2/screenshot-1.png",
		"/home/dmz/.datawatch/attachments/a1b2/trace-log.txt",
		"/home/dmz/.datawatch/attachments/a1b2/design-notes.pdf",
	}
	var b strings.Builder
	b.WriteString("Please review the attachments and the notes below.\n")
	for i := 0; b.Len() < 2*tmuxSendChunk+500; i++ {
		fmt.Fprintf(&b, "note %04d: some context for the reviewer that makes this long\n", i)
		if i%100 == 50 {
			fmt.Fprintf(&b, "attachment: %s\n", paths[(i/100)%len(paths)])
		}
	}
	for _, p := range paths {
		fmt.Fprintf(&b, "attachment: %s\n", p)
	}
	b.WriteString("closing line after the attachments")
	msg := b.String()
	wantChunks := (len(msg) + tmuxSendChunk - 1) / tmuxSendChunk
	if wantChunks < 2 {
		t.Fatalf("test message is only %d chunk(s), want > 1", wantChunks)
	}

	if err := (sieChunkTmux{f}).SendKeysWithSettle("s", msg+"\n\n", 0); err != nil {
		t.Fatal(err)
	}

	ops := sieOps(f)
	if got := f.Count("send-keys-literal"); got != wantChunks {
		t.Fatalf("literal chunks = %d, want %d", got, wantChunks)
	}
	var joined strings.Builder
	for _, c := range f.CallsSnapshot() {
		if c.Op != "send-keys-literal" {
			continue
		}
		if len(c.Args[0]) > tmuxSendChunk {
			t.Errorf("chunk of %d bytes exceeds tmuxSendChunk %d", len(c.Args[0]), tmuxSendChunk)
		}
		joined.WriteString(c.Args[0])
	}
	if joined.String() != msg {
		t.Fatalf("reassembled chunks differ from the message (%d vs %d bytes)", joined.Len(), len(msg))
	}
	for _, p := range paths {
		if !strings.Contains(joined.String(), p) {
			t.Errorf("attachment path %s missing from the pushed text", p)
		}
	}
	lastLiteral, firstEnter, firstCapture := sieIndexOf(ops, "send-keys-literal", true), sieIndexOf(ops, "enter", false), sieIndexOf(ops, "capture-visible", false)
	if firstEnter < 0 || firstEnter < lastLiteral {
		t.Fatalf("Enter (index %d) must come after the last literal chunk (index %d): %v", firstEnter, lastLiteral, ops)
	}
	if firstCapture < lastLiteral {
		t.Errorf("placeholder wait (index %d) began before the last chunk (index %d)", firstCapture, lastLiteral)
	}
	if got := enterCount(f); got != 1 {
		t.Errorf("Enter count = %d, want 1", got)
	}
	assertUntouched()
}

// (e) short one-line message with no placeholder: one Enter, no retry.
func TestSendInputEnter_ShortOneLine_NoRetry(t *testing.T) {
	assertUntouched := sieIsolateTmux(t)
	tc := useTestClock(t)
	f := newSubmitFake("s")
	f.PaneScript["s"] = []string{"──────\n❯ hello world\n──────"}

	if err := f.SendKeysWithSettle("s", "hello world\n", 0); err != nil {
		t.Fatal(err)
	}
	if got := enterCount(f); got != 1 {
		t.Fatalf("Enter count = %d, want 1", got)
	}
	sieWantOps(t, f, []string{"send-keys-settle", "capture-visible", "enter"})
	if tc.sleeps != 0 {
		t.Errorf("slept %d times (%v), want 0 on the fast path", tc.sleeps, tc.slept)
	}
	assertUntouched()
}

// (f) the placeholder never appears within the bounded wait: the Enter is
// still sent once and the function returns.
func TestSendInputEnter_PlaceholderNeverAppears(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  string
		// verifyPasses is how many post-Enter verification captures run:
		// the multi-line message is re-checked once (the pane is clean, so
		// no retry follows); the short line is not re-checked at all.
		verifyPasses int
	}{
		{"short one-line", "hello world", 0},
		{"multi-line", "first line\nsecond line of text", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertUntouched := sieIsolateTmux(t)
			clk := useTestClock(t)
			f := newSubmitFake("s")
			f.PaneScript["s"] = []string{"──────\n❯ \n──────"} // never shows the text

			done := make(chan error, 1)
			go func() { done <- f.SendKeysWithSettle("s", tc.msg, 0) }()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("delivery returned %v, want nil", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("delivery did not return: the placeholder wait is unbounded")
			}

			if got := enterCount(f); got != 1 {
				t.Fatalf("Enter count = %d, want exactly 1", got)
			}
			maxPolls := int(pasteVisibleTimeout/pasteVisiblePoll) + 1
			if got := f.Count("capture-visible"); got < 2 || got > maxPolls+tc.verifyPasses {
				t.Errorf("captures = %d, want a bounded poll (2..%d)", got, maxPolls+tc.verifyPasses)
			}
			wantSlept := pasteVisibleTimeout - pasteVisiblePoll // no sleep past the deadline
			if tc.verifyPasses > 0 {
				wantSlept += pasteRetrySettle
			}
			if clk.slept != wantSlept {
				t.Errorf("slept %v, want %v", clk.slept, wantSlept)
			}
			ops := sieOps(f)
			if last := ops[len(ops)-1-tc.verifyPasses]; last != "enter" {
				t.Errorf("the Enter must follow the bounded wait: %v", ops)
			}
			assertUntouched()
		})
	}
}

// sieManager builds a Manager on FakeTmux with the delivery flow on, and a
// waiting session in the given backend family.
func sieManager(t *testing.T, id, family, outputMode string) (*Manager, *FakeTmux, string) {
	t.Helper()
	mgr, f := newTestManagerWithFake(t)
	f.SubmitFlow = true
	mgr.SetScheduleSettleMs(200)
	tmuxName := "cs-" + id
	if err := mgr.SaveSession(&Session{
		ID: id, FullID: "testhost-" + id, TmuxSession: tmuxName,
		State: StateWaitingInput, BackendFamily: family, OutputMode: outputMode,
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	return mgr, f, tmuxName
}

// The same paste → Enter → verify sequence runs for every tmux-backed
// family through Manager.SendInput.
func TestSendInputEnter_ManagerSameSequenceForTmuxFamilies(t *testing.T) {
	scenarios := []struct {
		name      string
		script    []string
		wantEnter int
		wantOps   []string
	}{
		{
			"accepted first time",
			[]string{siePlaceholder, sieWorking},
			1,
			[]string{"send-keys-settle", "capture-visible", "enter", "capture-visible"},
		},
		{
			"lost once",
			[]string{siePlaceholder, siePlaceholder, sieWorking},
			2,
			[]string{"send-keys-settle", "capture-visible", "enter", "capture-visible", "enter", "capture-visible"},
		},
	}
	for _, family := range []string{"claude-code", "opencode", "pi"} {
		for _, sc := range scenarios {
			t.Run(family+"/"+sc.name, func(t *testing.T) {
				assertUntouched := sieIsolateTmux(t)
				useTestClock(t)
				id := strings.ReplaceAll(family, "-", "") + fmt.Sprint(len(sc.script))
				mgr, f, tmuxName := sieManager(t, id, family, "")
				f.PaneScript[tmuxName] = sc.script

				if err := mgr.SendInput("testhost-"+id, sieLongMessage(), "web"); err != nil {
					t.Fatalf("SendInput: %v", err)
				}
				if got := enterCount(f); got != sc.wantEnter {
					t.Fatalf("Enter count = %d, want %d", got, sc.wantEnter)
				}
				if got := f.Count("send-keys-settle"); got != 1 {
					t.Fatalf("submits = %d, want exactly 1", got)
				}
				if got := f.Count("send-keys"); got != 0 {
					t.Fatalf("bare send-keys count = %d, want 0", got)
				}
				sieWantOps(t, f, sc.wantOps)
				for _, c := range f.CallsSnapshot() {
					if c.Session != tmuxName {
						t.Errorf("call %s went to %q, want %q", c.Op, c.Session, tmuxName)
					}
				}
				sess, ok := mgr.store.Get("testhost-" + id)
				if !ok {
					t.Fatal("session vanished")
				}
				if sess.InputCount != 1 || sess.State != StateRunning {
					t.Errorf("InputCount=%d State=%s, want 1 / running", sess.InputCount, sess.State)
				}
				assertUntouched()
			})
		}
	}
}

// The ACP, OpenWebUI and Ollama chat routes keep their own gate in
// SendInput: they decide from their own conversation state, ahead of any
// tmux call. With no live conversation the route reports "not handled" and
// input falls back to the shared tmux delivery, which presses one Enter for
// a short message. An active conversation cannot be created from this
// package (the state is private to each backend and needs a live server), so
// this proves the gate and fallback, not a live HTTP route.
func TestSendInputEnter_ManagerChatRoutesUntouched(t *testing.T) {
	cases := []struct {
		name, family, outputMode string
		routeHandled             func(tmuxSession string) bool
	}{
		{"opencode-acp", "opencode-acp", "", func(s string) bool { return opencode.SendMessageACP(s, "probe") }},
		{"openwebui", "openwebui", "chat", func(s string) bool { return openwebui.SendMessageOWUI(s, "probe") }},
		{"ollama chat", "ollama", "chat", func(s string) bool { return ollama.SendMessageOllama(s, "probe") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertUntouched := sieIsolateTmux(t)
			useTestClock(t)
			id := strings.ReplaceAll(tc.name, " ", "") + "rt"
			id = strings.ReplaceAll(id, "-", "")
			mgr, f, tmuxName := sieManager(t, id, tc.family, tc.outputMode)
			f.PaneScript[tmuxName] = []string{"──────\n❯ hello there\n──────"}

			var chat []string
			mgr.SetOnChatMessage(func(_, role, content string, _ bool) { chat = append(chat, role+":"+content) })

			if tc.routeHandled(tmuxName) {
				t.Fatalf("route claims a conversation for %s that was never started", tmuxName)
			}
			if err := mgr.SendInput("testhost-"+id, "hello there", "web"); err != nil {
				t.Fatalf("SendInput: %v", err)
			}

			// Chat-mode sessions still emit the user message exactly once, ahead of delivery.
			if tc.outputMode == "chat" {
				if want := []string{"user:hello there"}; !reflect.DeepEqual(chat, want) {
					t.Errorf("chat messages = %v, want %v", chat, want)
				}
			} else if len(chat) != 0 {
				t.Errorf("non-chat session emitted chat messages: %v", chat)
			}
			// Fallback delivery: one submit, one Enter, short-message fast path.
			sieWantOps(t, f, []string{"send-keys-settle", "capture-visible", "enter"})
			assertUntouched()
		})
	}
}
