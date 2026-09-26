package server

import (
	"os"
	"strings"
	"testing"
)

// opencode sessions started with a task run `opencode run` (real tmux
// scrollback); only the task-less interactive TUI needs the client-side
// frame history for scroll mode.
func TestPWA_ScrollModeUsesTmuxHistoryForOpencodeRunSessions(t *testing.T) {
	b, err := os.ReadFile("web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	i := strings.Index(src, "function _scrollIsOpenCodeTUI()")
	if i < 0 {
		t.Fatal("_scrollIsOpenCodeTUI not found")
	}
	body := src[i : i+700]
	if !strings.Contains(body, "backend_family") || !strings.Contains(body, "sess.task") {
		t.Fatalf("scroll-mode TUI check must exclude sessions that have a task (opencode run):\n%s", body)
	}
}
