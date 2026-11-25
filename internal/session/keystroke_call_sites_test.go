package session

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every place in the daemon that types into a session is listed here with who
// asks for the input. A new call site fails this test until it is reviewed: a
// sender no person asked for must pass an automated source to SendInput (or
// use SendAutomatedRawKeys), or check for an approval prompt itself, so it can
// never answer a permission or safety prompt.
var keystrokeCallSites = map[string]struct {
	n   int
	why string
}{
	"cmd/datawatch/loop_rescue.go":                         {1, "automated: loop-rescue"},
	"cmd/datawatch/main.go":                                {10, "automated: startup auto-accept, filter, questions-answer, automaton x2, channel-task, exit-hook; person: CLI send fallback x2 (tmux) and its pane literal"},
	"cmd/datawatch/post_turn_nudge.go":                     {1, "automated: idle-nudge, task-redelivery"},
	"cmd/datawatch/supervisor_loop_wire.go":                {1, "automated: supervisor-wake"},
	"cmd/datawatch/worker_nudge_hooks.go":                  {1, "automated: idle-nudge, task-redelivery"},
	"internal/autonomous/lifecycle_session_push.go":        {1, "automated: lifecycle-sub"},
	"internal/autonomous/loop_rescue.go":                   {1, "automated: wraps loop_rescue.go"},
	"internal/autonomous/supervisor_wake_subscriptions.go": {1, "automated: wraps supervisor_loop_wire.go"},
	"internal/autonomous/worker_nudge_step.go":             {1, "automated: wraps worker_nudge_hooks.go"},
	"internal/llm/backends/aider/backend.go":               {1, "backend launch/send in its own shell pane"},
	"internal/llm/backends/gemini/backend.go":              {3, "backend launch and output echo in its own shell pane"},
	"internal/llm/backends/goose/backend.go":               {3, "backend launch/send in its own shell pane"},
	"internal/llm/backends/ollama/backend.go":              {2, "backend launch in its own shell pane"},
	"internal/llm/backends/ollama/conversation.go":         {4, "output echo in its own shell pane"},
	"internal/llm/backends/opencode/acpbackend.go":         {2, "backend launch in its own shell pane"},
	"internal/llm/backends/opencode/backend.go":            {3, "backend launch in its own shell pane"},
	"internal/llm/backends/openwebui/backend.go":           {1, "backend launch in its own shell pane"},
	"internal/llm/backends/openwebui/conversation.go":      {8, "output echo in its own shell pane"},
	"internal/llm/backends/pi/backend.go":                  {1, "backend launch in its own shell pane"},
	"internal/llm/backends/shell/backend.go":               {1, "backend launch in its own shell pane"},
	"internal/llm/claudecode/backend.go":                   {2, "launch command typed into the shell before claude runs"},
	"internal/llm/tmuxsend.go":                             {1, "helper for the launch commands"},
	"internal/mcp/bl358_discussion_subscribe.go":           {1, "tool input, refused on a prompt (SendToolInput): discussion-sub"},
	"internal/mcp/server.go":                               {3, "tool input, refused on a prompt (SendToolInput): send_input, send_saved_command, reply_to_parent"},
	"internal/router/router.go":                            {2, "person: chat send and reply"},
	"internal/server/api.go":                               {6, "person: web input, raw terminal keys, scroll keys, /api/sessions/send"},
	"internal/server/rollback.go":                          {1, "person: session input API"},
	"internal/session/filter.go":                           {1, "automated: filter"},
	"internal/session/filter_input_rules.go":               {1, "automated: schedule or filter, via FireScheduled"},
	"internal/session/manager.go":                          {13, "SendInput itself; launch shell setup; guarded: dialog accept/decline, channel-task"},
	"internal/session/paste_submit.go":                     {4, "submit Enter; retries stop at an approval menu"},
	"internal/session/prompt_guard.go":                     {4, "guarded: rate-limit wait, MCP retry, raw keys"},
	"internal/session/tmux.go":                             {8, "tmux layer"},
	"internal/session/tmux_kill_guard.go":                  {1, "launch shell setup"},
	"internal/session/ui_prompt_dismiss.go":                {1, "one Esc only: declines, never approves"},
}

var keystrokeCall = regexp.MustCompile(`(\.(SendInput|SendToolInput|SendRawKeys|SendKeys|SendKeysWithSettle|SendKeysGuarded|SendKeysLiteral|SendEnter|SendTrustPromptAccept|SendTrustDialogAccept)\()|"send-keys"`)

func TestEveryKeystrokeCallSiteIsReviewed(t *testing.T) {
	root := filepath.Join("..", "..")
	got := map[string]int{}
	for _, top := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || d.Name() == "fake_tmux.go" {
				return err
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			for _, l := range strings.Split(string(b), "\n") {
				s := strings.TrimSpace(l)
				if strings.HasPrefix(s, "//") || strings.HasPrefix(s, "func ") {
					continue
				}
				if n := len(keystrokeCall.FindAllString(l, -1)); n > 0 {
					got[filepath.ToSlash(rel)] += n
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for p, n := range got {
		if w := keystrokeCallSites[p]; w.n != n {
			t.Errorf("%s: %d keystroke call sites, reviewed %d: review the new or removed site (see the comment above the table)", p, n, w.n)
		}
	}
	for p, w := range keystrokeCallSites {
		if got[p] == 0 {
			t.Errorf("%s: listed with %d sites but has none", p, w.n)
		}
	}
}

// The sources the daemon's own senders pass are all held back by the guard.
func TestDaemonSendSourcesAreAutomated(t *testing.T) {
	for _, s := range []string{"loop-rescue", "auto-accept-disclaimer", "questions-answer", "automaton", "channel-task", "schedule", "exit-hook", "supervisor-wake", "discussion-sub", "mcp-reply", "lifecycle-sub", "idle-nudge", "task-redelivery", FilterSource("ab12")} {
		if !IsAutomatedInputSource(s) {
			t.Errorf("%s is not treated as automated", s)
		}
	}
}
