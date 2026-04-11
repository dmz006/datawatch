# B2: Claude Code Prompt Detection False Positives

**Date:** 2026-04-11
**Priority:** high
**Effort:** 1-2 days
**Category:** detection / claude-code

---

## Problem

Despite the 3-second prompt debounce (v2.2.4) and 15-second notification cooldown, Claude Code sessions still generate false `waiting_input` transitions during active computation. The `❯` prompt is visible in the tmux pane between tool calls, triggering detection even though Claude is actively processing.

### Symptoms
- Session flips `running → waiting_input → running` repeatedly during long tasks
- User receives multiple "needs input" alerts (throttled to ~4/min by cooldown, but still noisy)
- Web UI session state badge flickers between running and waiting
- Signal/Telegram notifications arrive while Claude is actively coding

### Root cause analysis

Claude Code's TUI layout shows the `❯` prompt in the input area at ALL times — even while tools are running. The anti-spinner logic in `matchPromptInLines()` checks for `⎿  Running…` or spinner patterns above the prompt, but:

1. **Gap between tool calls** — after one tool finishes and before the next starts, there's a 1-3 second window where no spinner/running indicator is visible, but the `❯` prompt is. The debounce window (3s) is barely enough and sometimes fires during this gap.

2. **Thinking pauses** — Claude pauses to "think" between tool calls. During this time, the screen shows the `❯` prompt with no active indicators above it. This looks identical to a genuine idle prompt.

3. **ChannelReady debounce mismatch** — `StartScreenCapture` requires 50 consecutive captures (~10s) for ChannelReady sessions, but `monitorOutput` capture-pane path only uses the 3s debounce. These two paths have different sensitivity.

4. **Active indicators list is incomplete** — `activeIndicators` array checks for "Forming", "Thinking", "Running", "Executing", "processing", "Processing" but Claude's UI shows other states like "Reading", "Writing", "Searching" that aren't in the list.

---

## Investigation plan

### Phase 1: Catalog all Claude Code UI states (0.5 day)

1. **Capture all status indicators** — run a complex Claude Code task and capture the tmux pane at 200ms intervals. Catalog every unique status line that appears above the `❯` prompt:
   - Tool execution: `⎿  Running…`, `⎿  Reading…`, `⎿  Writing…`
   - Planning: `Thinking…`, `Reasoning…`
   - Claude's status bar text variations
   - Any other transient states

2. **Map timing gaps** — measure the duration of gaps between tool calls where no indicator is visible but Claude is still processing. This determines the minimum safe debounce.

### Phase 2: Improve detection accuracy (1 day)

#### Option A: Increase debounce for Claude Code
- Set per-backend debounce: `claude-code` detection config with `prompt_debounce: 10` (10 seconds)
- This is conservative but eliminates most false positives
- Downside: genuine prompts (permission dialogs, trust prompts) take 10s to detect

#### Option B: Expand active indicators
- Add all Claude status verbs to `activeIndicators`: "Reading", "Writing", "Searching", "Analyzing", "Editing", "Creating", "Updating", "Checking"
- Add Claude's progress bar patterns (e.g., `[████░░░░]`)
- Check for the presence of task list items (numbered items like "1. Read file" "2. Edit code")

#### Option C: Track output velocity (recommended)
- Instead of checking screen content, track how recently the **log file** was written to
- If the log file had output within the last N seconds, suppress prompt detection regardless of screen content
- This catches the "gap between tool calls" case — Claude's output continues flowing even when the screen shows the prompt momentarily
- Implementation: use `lastOutputTime` already tracked in `monitorOutput` — if `time.Since(lastOutputTime) < velocityWindow`, don't check for prompts

#### Option D: Combine B + C
- Expand active indicators (catches known patterns)
- AND require output silence for `prompt_debounce` seconds (catches unknown patterns)
- This is the most robust approach

### Phase 3: Reduce notification noise (0.5 day)

1. **Per-session notification dedup** — track the actual message text sent via Signal/Telegram. If the same prompt text was sent within the last N minutes, suppress duplicate.

2. **Backoff on oscillation** — if a session has flipped `running↔waiting_input` more than 3 times in 60 seconds, increase the debounce to 30s for that session until it stabilizes.

3. **Smart notification** — only send "needs input" to remote channels when the prompt is a **permission/trust** prompt (contains "Allow", "Trust", "[y/N]", etc.), not when it's just the `❯` idle prompt. The `❯` prompt often resolves on its own.

---

## Proposed changes

### manager.go — output velocity check
```go
// In the idle timeout check, before checking prompt patterns:
if time.Since(lastOutputTime) < velocityWindow {
    // Recent output — LLM is still active, skip prompt check
    continue
}
```

### manager.go — expanded active indicators
```go
var activeIndicators = []string{
    "Forming", "Thinking", "Running", "Executing",
    "processing", "Processing",
    "Reading", "Writing", "Searching", "Analyzing",
    "Editing", "Creating", "Updating", "Checking",
    "Installing", "Building", "Compiling",
}
```

### config.go — per-backend debounce override
```yaml
# In config.yaml — user can tune per-backend
session:
  claude_code:
    detection:
      prompt_debounce: 8  # Claude needs longer debounce due to inter-tool gaps
```

---

## Files to modify

| File | Changes |
|------|---------|
| `internal/session/manager.go` | Output velocity check, expanded activeIndicators, oscillation backoff |
| `internal/config/config.go` | Per-backend detection override for claude-code |
| `cmd/datawatch/main.go` | Wire per-backend detection config |

## Success criteria

- Zero false `waiting_input` notifications during a 30-minute Claude Code session with active tool use
- Genuine permission prompts detected within 10 seconds
- Max 1 notification per genuine prompt event
