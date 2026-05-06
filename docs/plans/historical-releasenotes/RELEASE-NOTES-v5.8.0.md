# datawatch v5.8.0 — release notes

**Date:** 2026-04-26
**Spans:** v5.7.0 → v5.8.0
**Closed:** BL201 (voice/whisper backend inheritance)

## What's new

### BL201 — Voice/whisper backend inheritance

Operator directive: voice transcription should reuse the
already-configured ollama / openwebui endpoint + key from the LLM
config block instead of asking the operator to enter them twice.

Three pieces:

1. **`internal/transcribe/factory.go`** — new `ollama` backend
   case, routing through the OpenAI-compat client (the bare ollama
   daemon doesn't ship audio; the operator points the resolved
   endpoint at whichever host actually serves the audio API —
   typically OpenWebUI fronting ollama).

2. **`cmd/datawatch/main.go inheritWhisperEndpoint`** — when the
   operator selects `whisper.backend = openwebui` and leaves
   `whisper.endpoint` + `whisper.api_key` blank, fall back to
   `cfg.OpenWebUI.URL` + `cfg.OpenWebUI.APIKey`. Same for
   `whisper.backend = ollama` (uses `cfg.Ollama.Host` with `/v1`
   appended). Explicit values always win — the inherit step is a
   pure fallback.

3. **PWA — Settings → General → Voice Input** already hides
   `whisper.endpoint` + `whisper.api_key` (since v5.2.0 the field
   list only exposes `backend / model / language / venv_path`),
   so the daemon-side inheritance is now the single source of
   truth for those values. No PWA change in v5.8.0.

8 new unit tests cover the inheritance matrix:

- openwebui blank → OpenWebUI.URL/APIKey inherited
- openwebui explicit → explicit wins (no clobber)
- ollama blank → Ollama.Host + `/v1`
- ollama with trailing slash → trimmed before `/v1` append
- whisper backend → no-op (endpoint/key untouched)
- openai backend → no-op (no top-level OpenAI LLM config)
- backend string with surrounding whitespace + mixed case → still inherits
- factory ollama + openwebui both route to `*OpenAICompatTranscriber`

## Known follow-ups (still open)

- **BL180 Phase 2** — eBPF kprobes (resume) + cross-host federation correlation
- **BL191 Q4 / Q6** — recursive child-PRDs through BL117 + guardrails-at-all-levels
- **BL190** — PWA screenshot rebuild for the now-13-doc howto suite

## Upgrade path

```bash
datawatch update         # check + install
datawatch restart        # apply the new binary; preserves tmux sessions

# verify the inheritance picks up your existing OpenWebUI / Ollama config:
datawatch config set whisper.enabled true
datawatch config set whisper.backend openwebui   # or ollama
datawatch reload
curl -sk -X POST -H "Authorization: Bearer $(cat ~/.datawatch/token)" \
  https://localhost:8443/api/voice/test
```

If the test endpoint returns `{"ok": true, …}` the inheritance is
working; if it returns `{"ok": false, "error": …}` the underlying
LLM-config endpoint is unreachable — fix the LLM-side config,
not the whisper-side.
