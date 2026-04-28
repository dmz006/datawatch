# datawatch v5.26.56 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.55 → v5.26.56
**Patch release** (no binaries — operator directive).
**Closed:** Container Workers config in PWA Settings + interactive Whisper test mic dialog (two operator-asked items).

## What's new

### 1. Settings → General → Container Workers (F10)

Operator-asked: *"Where in the pwa settings is the agent configuration that was enabled for the smoke tests being set up?"*

The `cfg.Agents` knobs (`image_prefix`, `image_tag`, `docker_bin`, `kubectl_bin`, `callback_url`, `bootstrap_token_ttl_seconds`, `worker_bootstrap_deadline_seconds`) had REST + MCP + CLI + comm-channel reach but no Web UI surface. v5.26.56 adds a "Container Workers (F10)" section under Settings → General (between Cluster Profiles and Notifications):

- Each key rendered as a labelled input with inline hint text.
- Save fires `PUT /api/config` with the same dotted-key shape every other channel uses.
- Toast on save reminds the operator that some keys (`image_prefix` / `image_tag` / `*_bin` / `callback_url`) require a daemon restart per `/api/reload`'s response.
- Section is anchored at `gc_agents` (consistent naming with `gc_projectprofiles` / `gc_clusterprofiles`).

Closes the configuration parity gap — every F10 setting is now reachable from every channel.

### 2. Test Whisper now opens a recording dialog

Operator-asked: *"The test whisper i expected it would open a dialog with a mic button to test, not just backend test."*

Previously Settings → Voice → "Test transcription endpoint" fired `POST /api/voice/test` (a 1KB silent-WAV health check). Useful for "is the backend reachable" but not for "does it actually transcribe my voice."

v5.26.56 swaps the click handler to open an interactive modal:

```
┌─ Test transcription ──────────────────────┐
│                                           │
│  Click 🎤 to start recording, click again │
│  to stop. The transcribed text appears    │
│  below.                                   │
│                                           │
│  [🎤]  idle                                │
│                                           │
│  ┌─ transcript ──────────────────────────┐│
│  │ (transcript will appear here)         ││
│  └───────────────────────────────────────┘│
│                                           │
│  [Run silent-WAV health check]  [Done]    │
│                                           │
└───────────────────────────────────────────┘
```

Click the 🎤, speak, click again to stop. The recording goes through the same `/api/voice/transcribe` path the inline mic buttons use; the transcript lands in the dialog with latency + char-count in the status line.

The pre-v5.26.56 silent-WAV health check is still reachable via the "Run silent-WAV health check" button at the bottom of the dialog — same fail-disable behavior, just no longer the default action. The new dialog explicitly does NOT auto-disable Whisper on a bad transcribe; the operator opened the dialog deliberately and can read the result themselves.

### What this required

- Reused the existing `MediaRecorder` + `getUserMedia` plumbing from `startGenericVoiceInput` (the inline mic helper). No new daemon endpoints.
- New element ids: `whisperTestOverlay`, `whisperTestMicBtn`, `whisperTestStatus`, `whisperTestTranscript` — scoped to the modal.

## Configuration parity

The Container Workers section closes the parity gap explicitly. Whisper test is a UX improvement, not a config change.

## Tests

UI-only changes. Smoke unaffected (56/0/1). Go test suite unaffected (465 passing).

## Known follow-ups

- **Wake-up stack L0–L5 smoke probes (#39).** Next service-function audit residual.
- **Stdio-mode MCP tools probe.** Needs an MCP client wrapper.
- PRD-flow phase 3 + phase 4 implementation (designs landed v5.26.53).

## Upgrade path

```bash
git pull
datawatch restart
# Hard-reload the PWA (SW cache datawatch-v5-26-56). Visit
# Settings → General — there's a new "Container Workers (F10)"
# section. Click Settings → Voice → Test transcription endpoint
# — the dialog now lets you record and verify end-to-end.
```
