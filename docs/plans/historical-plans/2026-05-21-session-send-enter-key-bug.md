# Bug: `datawatch session send` does not send Enter/newline character

**Date:** 2026-05-21  
**Severity:** Medium  
**Status:** Open  

## Issue

The `datawatch session send <id> <text>` CLI command sends text input to a waiting session but does not automatically append a newline/Enter character. This causes the input to be buffered in the session without being executed.

## Expected Behavior

```bash
datawatch session send 630b "message to send"
```

Should be equivalent to typing the message and pressing Enter, causing the input to be processed.

## Actual Behavior

The text is received by the session but sits in the input buffer. The Enter key must be manually sent separately:

```bash
datawatch session send 630b $'message\n'
```

## Impact

- Commands sent via `datawatch session send` don't execute automatically
- Users must manually send Enter or use bash escaping (`$'...\n'`) as a workaround
- Makes automation/scripting with session send unreliable without extra handling

## Root Cause

The CLI command likely sends raw text via tmux or stdio without appending a newline terminator.

## Proposed Fix

1. **Option A:** Automatically append `\n` to all `datawatch session send` input (preferred - simplest UX)
2. **Option B:** Add a `--send-newline` or `--enter` flag (more explicit but requires user knowledge)
3. **Option C:** Document the workaround clearly in help text and examples

## Affected Code

- CLI: `datawatch session send` command implementation (likely in cmd/session.go or similar)
- May affect: any tmux integration that sends user input directly

## Testing

```bash
# Test 1: Send message without newline (should still execute)
datawatch session send <waiting-session> "test message"

# Test 2: Verify message is processed immediately
datawatch session get <waiting-session>  # output.log should show message was processed
```

## Notes

- Discovered while handing off task to session 630b in datawatch-app v1.0.0 release workflow
- Workaround: Use `$'text\n'` bash syntax or pipe input with echo
- Should be fixed before making session send a standard part of automation workflows
