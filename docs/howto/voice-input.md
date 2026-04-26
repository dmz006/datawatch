# How-to: Voice input (Whisper / OpenAI / OpenWebUI / Ollama)

datawatch transcribes voice messages so you can speak into Signal,
Telegram, Slack, etc., or hit the PWA microphone, instead of typing.
There are four supported backends; pick whichever matches what you
already have running.

| Backend | Where it runs | Privacy | One-line summary |
|---------|---------------|---------|------------------|
| `whisper` | Local Python venv | private | Default; needs Python + the `whisper` model files |
| `openai` | Cloud (api.openai.com) | sends audio to OpenAI | Easiest if you already pay for an OpenAI key |
| `openai_compat` | Any OpenAI-compatible HTTPS endpoint | depends | faster-whisper-server, vLLM, custom |
| `openwebui` | Your already-configured OpenWebUI | private | Reuses whatever model OpenWebUI is fronting |
| `ollama` | Your already-configured Ollama | private | Reuses Ollama's transcribe endpoint |

The non-local backends (`openai`, `openai_compat`, `openwebui`,
`ollama`) **inherit endpoint + API key from the matching LLM-backend
config block**. You don't enter them twice.

## 1. Local whisper

Default. Best privacy; needs a Python venv with the `whisper` package.

```bash
# One-time install
python3 -m venv ~/.datawatch/venv
~/.datawatch/venv/bin/pip install -U openai-whisper

# Configure
datawatch config set whisper.enabled true
datawatch config set whisper.backend whisper
datawatch config set whisper.venv_path ~/.datawatch/venv
datawatch config set whisper.model base       # tiny / base / small / medium / large
datawatch config set whisper.language en      # or "auto"
datawatch reload
```

Verify:

```bash
curl -sk -X POST -H "Authorization: Bearer $(cat ~/.datawatch/token)" \
  https://localhost:8443/api/voice/test
#  → {"ok": true, "transcript": "", "latency_ms": 423}
```

PWA: Settings → General → Voice Input → click **Test transcription
endpoint**. A toast confirms success or surfaces the failure reason
(it'll also force-disable `whisper.enabled` on failure so the daemon
doesn't keep trying).

## 2. OpenWebUI (recommended for already-configured operators)

If you've already wired OpenWebUI as an LLM backend
([chat-and-llm-quickstart](chat-and-llm-quickstart.md)), point voice
at the same endpoint:

```bash
datawatch config set whisper.enabled true
datawatch config set whisper.backend openwebui
datawatch config set whisper.model whisper-large-v3   # or whatever your OpenWebUI is serving
datawatch reload
```

The daemon reads `openwebui.endpoint` + `openwebui.api_key` from
your existing LLM block — no duplication.

Test:

```bash
datawatch diagnose | jq '.voice'
```

PWA: Settings → General → Voice Input → backend **openwebui** → click
**Test transcription endpoint**.

## 3. Ollama

Same pattern — Ollama exposes a `POST /api/transcribe` (or you can
front it with a whisper-compatible model):

```bash
datawatch config set whisper.enabled true
datawatch config set whisper.backend ollama
datawatch config set whisper.model whisper:base
datawatch reload
```

Endpoint pulled from `ollama.endpoint` (default
`http://localhost:11434`) automatically.

## 4. OpenAI (cloud)

```bash
datawatch config set whisper.enabled true
datawatch config set whisper.backend openai
datawatch config set whisper.model whisper-1
datawatch reload
```

Pulls the API key from `anthropic.api_key`-equivalent block —
specifically `openai.api_key` if you've configured the OpenAI LLM
backend, otherwise the daemon refuses to start the transcriber.

## 5. openai_compat (any OpenAI-shaped endpoint)

Use this for `faster-whisper-server`, vLLM Whisper, or your own
proxy:

```bash
datawatch config set whisper.enabled true
datawatch config set whisper.backend openai_compat
datawatch config set whisper.model whisper-large-v3
datawatch config set whisper.endpoint http://my-host:9000/v1
datawatch config set whisper.api_key dev-key                # often empty / "anonymous"
datawatch reload
```

This is the only backend that takes `whisper.endpoint` +
`whisper.api_key` directly — the others inherit from their matching
LLM block.

## Wire it into a chat channel

Once a backend is OK, mobile chat channels start auto-transcribing
voice notes:

| Channel | What happens |
|---------|--------------|
| Signal  | Voice note arrives → transcribed → text dispatched as if typed |
| Telegram | Same — voice → text → command pipeline |
| Discord | Recorder snippet → transcript → command |

The PWA voice button (microphone icon next to the session input) goes
through the same pipeline.

## Troubleshooting

| Symptom | Likely cause | Fix |
|---------|--------------|-----|
| `voice transcription not enabled` (503) | `whisper.enabled` is false (often after a failed Test) | re-Test, then re-enable once OK |
| `transcribe: failed to call ...` | endpoint unreachable | `curl -v <endpoint>/v1/audio/transcriptions` from the daemon host |
| `whisper not found` | venv missing or wrong path | `ls $(datawatch config get whisper.venv_path)/bin/whisper` |
| Test returns OK but real voice fails | model size > available RAM (esp. `large`) | drop to `medium` or `base` |
| Cloud transcription is slow (>30 s) | network latency to OpenAI | switch to `openwebui` or local `whisper` |

`datawatch diagnose` returns the per-backend reachability + last-error
in one shot.

## Reachability across channels

| Channel | Action | Command |
|---------|--------|---------|
| CLI | configure | `datawatch config set whisper.{enabled,backend,model,language,venv_path} …` |
| CLI | test | `curl -sk -X POST -H "Authorization: Bearer …" https://localhost:8443/api/voice/test` |
| REST | configure | `PUT /api/config` (same keys as YAML) |
| REST | test | `POST /api/voice/test` |
| REST | use | `POST /api/voice/transcribe` (multipart audio) |
| MCP | (no voice tools yet — chat-channel + REST cover the surface) | — |
| Chat | use | send a voice note to any allowed sender on Signal / Telegram / Discord |
| PWA | configure + test | Settings → General → Voice Input |
| PWA | use | microphone icon next to the session input |

## See also

- [How-to: Setup + install](setup-and-install.md) — get the daemon running
- [How-to: Chat + LLM quickstart](chat-and-llm-quickstart.md) — wire the LLM backend whose endpoint+key you reuse here
- [`docs/api/voice.md`](../api/voice.md) — full REST reference
