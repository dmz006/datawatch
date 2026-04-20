# Chat-mode backends (BL78 / BL79)

**Shipped in v3.9.0.** Configuration recipes for enabling rich chat
UI (BL73) on Gemini, Aider, and Goose backends.

---

## Background

Datawatch's rich chat UI (BL73, shipped v2.1.3) renders messages as
bubbles with avatars, markdown, and inline code. It activates when a
backend's session is created with `OutputMode: "chat"`. Ollama
shipped chat mode in v2.2.0 (BL77); OpenCode-ACP got rich chat in
v2.3.1 (BL83).

BL78 / BL79 wire the same surface to Gemini / Aider / Goose. The
backends already accept `output_mode: "chat"` — the operator just
needs to opt in via config.

---

## Gemini chat mode (BL78)

```yaml
gemini:
  enabled: true
  binary: "gemini"
  output_mode: "chat"
  input_mode: "tmux"
```

Once set, sessions started with `backend: "gemini"` render as bubbles
in the web UI / mobile app, support markdown rendering, and emit
structured chat events to the WebSocket stream.

## Aider chat mode (BL79a)

```yaml
aider:
  enabled: true
  binary: "aider"
  output_mode: "chat"
```

## Goose chat mode (BL79b)

```yaml
goose:
  enabled: true
  binary: "goose"
  output_mode: "chat"
```

---

## OpenCode memory hooks (BL72)

OpenCode sessions inherit the auto-save memory pipeline that BL65
shipped for Claude Code (v2.0.0). When `memory.enabled: true`, every
prompt-response pair from an opencode session is auto-saved with
project-dir scoping, role tagging, and learning extraction.

No additional config field — toggling `output_mode: "chat"` on the
opencode backend automatically enables the same memory hook path
that Ollama and OpenCode-ACP use:

```yaml
opencode:
  enabled: true
  output_mode: "chat"

memory:
  enabled: true
```

The `BL65` `claude_code_auto_save` is the precedent; opencode reuses
the same hook function via the chat-message router.
