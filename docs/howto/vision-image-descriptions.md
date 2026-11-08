---
docs:
  index: true
  topics: [vision, image description, image attachment, describe timeout, describe_timeout_seconds, fallback, "@path", trailing space, single Enter, signal, telegram, websocket, settings, config, rest, cli, comm, mcp]
exec_params:
  - {name: image_path, required: false, description: "Absolute local path to a local image file (jpeg/png/webp/gif) to describe; leave empty to skip the describe step. Requires vision.enabled: true"}
exec_steps:
  - tool: get_config
    description: Read the daemon config; the vision block reports the effective describe_timeout_seconds (the built-in 5 seconds when the key is unset)
    args: {}
    read_only: true
  - tool: config_set
    description: Set the per-image-describe deadline in seconds (whole number >= 1), for example 15
    args: {key: "vision.describe_timeout_seconds", value: "15"}
  - tool: get_config
    description: Read back the vision block and confirm the new describe_timeout_seconds is in effect
    args: {}
    read_only: true
  - tool: vision_describe
    description: "Describe one local image with the configured vision model; bounded by the describe timeout, on timeout the daemon reports an error rather than waiting (only meaningful when vision.enabled is true and an image is present)"
    args: {image_path: "{{params.image_path}}"}
---
# How-to: Vision image descriptions

Vision lets the daemon understand image and photo attachments. Wherever an image
arrives — a Signal or Telegram message, a `[image:<path>]` tag in text you send
to a session, the `image_paths` of an MCP call, or a Council run — the daemon
sends the image to a vision model you choose and folds a text description into
the content, so text-only models and sessions can work with the picture.

It is off by default. This guide covers how to enable it, what the per-request
**describe timeout** does and what happens when a description times out, and
every surface you can read and set it from. The full integration matrix (comms
router, sessions, MCP, Council, REST, skills) is in
[datawatch-definitions.md](../datawatch-definitions.md) under *Vision System*.

## Base requirements

- A running daemon.
- A vision-capable model reachable over HTTP. With the default `ollama`
  backend: any Ollama model that can see (llava, moondream, minicpm-v, ...).
  With `openai` or `openai_compat`: a chat-completions endpoint with a
  vision-capable model (gpt-4o or similar).
- No auto-detection — the model name is always set explicitly.

## The vision block

```yaml
vision:
  enabled: false            # off by default
  backend: ollama           # "ollama" (default) | "openai" | "openai_compat"
  endpoint: http://localhost:11434
  api_key: ""               # optional for self-hosted, required for upstream
  model: llava              # must be vision-capable
  default_prompt: ""        # empty = "Describe this image concisely."
  max_image_bytes: 0        # 0 = 10 MB
  describe_timeout_seconds: 0   # 0 or negative = 5 s (the built-in default)
```

Enable it by setting `vision.enabled: true`, pointing `endpoint` at your model
server and naming the model in `model`. With `enabled: false` (or no usable
model), an `[image:<path>]` tag is not asked to a model at all: it is converted
to a bare `@<path>` reference, which text-only sessions — Claude Code among
them — pick up and read the file themselves through their own vision pipeline.

## The describe timeout, and what a timeout means

Every image description is one request to the vision model (an Ollama
`/api/generate` or an OpenAI-style `/v1/chat/completions` call). That request
is **bounded**: it runs under a deadline of `vision.describe_timeout_seconds`
seconds, with the built-in default of **5 seconds** when the key is unset (0 or
negative). The model call itself runs in its own worker, so a backend that
ignores its deadline cannot hold the caller past the timeout — it simply stops
being waited on.

What the daemon does with the image depends on the outcome:

| Outcome | What the sender gets |
|---|---|
| Description in time | the text carries `[image: <description> \| path: <path>]` |
| **Timeout, error, or an empty description** | the tag degrades to a bare `@<path>` reference — the message is still delivered, and the session can read the file itself |
| File missing or unreadable, or a path the daemon will not serve | the original `[image:<path>]` tag is left unchanged |

One detail worth knowing — the **trailing-space rule**, which is what keeps a fallback from costing you an extra Enter. Whatever put a message on the bare path (no vision backend, or the timeout, error and empty-description fallbacks), the `@<path>` reference carries exactly one space after the path *when the `[image:<path>]` tag is the last thing in the text*. That space terminates the session's `@<path>` file reference, so the file autocomplete closes and a single Enter sends the message — without it, the first Enter is spent dismissing the open completion instead of sending. The space only appears at the end of the text: with the same tag twice, only the last occurrence takes it, and a tag followed by any text is left exactly as-is. It is never added anywhere else — the successful description form (`[image: <description> | path: <path>]`) carries no trailing space, and the `[image:<path>]` tags left unchanged (unreadable files, paths the daemon will not serve) never got one.

So a slow describe call is never a hard failure of the message: the image
travels as a file reference and the model reads it directly. That is also why
the default is a small 5 seconds — the fallback is cheap, and a longer wait in
the *input path* is what this key is there to prevent.

Two further properties of the bound:

- **The deadline does not block the socket read loop.** When the text arrives
  over a WebSocket (the web app, the terminal), the `[image:<path>]` expansion
  runs on the connection's input worker in send order — not in the socket's
  read loop. A describe that is waiting on a slow model does not freeze the
  terminal or the web view, and a later plain send on the same connection is
  held in order behind the still-expanding image send (per connection, not
  globally per session).
- **The value is read live.** The deadline comes from the current config on
  every describe call, so changing it applies to the very next image — no
  restart, and nothing about an in-flight describe changes when you set it.

## Setting `vision.describe_timeout_seconds`

The key is one whole number: seconds per image describe, minimum 1. Unset
(0) reads the built-in 5-second default everywhere.

Every surface reads and sets the same key.

| Method | Read | Set |
|---|---|---|
| **YAML** | the value in the file (unset = 5 s in effect) | `describe_timeout_seconds: 15` under `vision:` in `~/.datawatch/config.yaml` |
| **Web UI** | Settings → **Vision (Image Descriptions)** → **Describe timeout (seconds, 0 = default 5)** | type the value in the same field and save |
| **REST** | `GET /api/config` → `vision.describe_timeout_seconds` (reports the *effective* value — 5 when unset) | `PUT /api/config` with `{"vision.describe_timeout_seconds": 15}` |
| **MCP** | `get_config` (the `vision` block, effective value in the same way) | `config_set(key="vision.describe_timeout_seconds", value="15")` |
| **Comm channel** | part of the config read | `configure vision.describe_timeout_seconds=15` |
| **CLI** | `datawatch config get vision.describe_timeout_seconds` | `datawatch config set vision.describe_timeout_seconds 15` |

Notes per surface:

- **REST / web app / MCP / comm / CLI write path.** A value below 1, a
  fractional number or a non-numeric value is refused and the stored setting is
  left alone — the next read still shows the previous effective value (5 when
  the key was never set). This is the same rule as
  `results.schedule_history_limit`: a bound can only grow into the valid range,
  it can never become 0.
- **YAML.** The file path is forgiving: an unset, 0 or negative value all mean
  "use the 5-second default"; a positive value is honoured verbatim, down to
  1 second.

```sh
# A heavier model that needs fifteen seconds per description
datawatch config set vision.describe_timeout_seconds 15

# Comm channel
configure vision.describe_timeout_seconds=15

# MCP
config_set(key="vision.describe_timeout_seconds", value="15")

# REST
curl -sk -X PUT -H "Content-Type: application/json" \
  -d '{"vision.describe_timeout_seconds": 15}' \
  https://localhost:8443/api/config
```

## Choosing a value

- **Keep the default (5 s) for small, local models.** A local Ollama vision
  model on decent hardware usually answers well inside it, and when it does
  not, the `@path` fallback delivers the message immediately.
- **Raise it for slow or loaded models** — a big model, a busy node, or an
  upstream API that takes several seconds — so the description usually lands in
  time and the text arrives pre-folded instead of as a file reference. There
  is no upper bound, but remember the wait is per image: five attachments mean
  five bounded describes, one per image.
- **Lower it when the fallback is what you want.** At 1 second the daemon
  almost always hands the image to the session as `@<path>`, which is the
  right trade-off when the receiving model has its own strong vision pipeline.

## Limits

- **The bound is per describe call**, not per message: a message with several
  image tags makes one bounded describe per tag.
- **The fallback is content-preserving, not description-preserving.** A
  timed-out description is never retried automatically; the session gets the
  file reference and reads the image itself.
- **Describing an image is an extra hop to your model server.** It costs one
  model request per image, and with `openai`-style backends it is billed like
  any other chat completion.
- **The deadline does not extend to the rest of the pipeline.** It bounds the
  describe request only; reading the file, or the model's own later reading of
  the `@path` reference, is not part of the bound.
- **The Settings field and the file both read 0 as "default 5".** If you want
  5 seconds explicitly, write 5 — 0 is a stand-in for the default, not a
  distinct value.
- **The trailing space belongs to the fallback, not to you.** A last-tag
  `@<path>` reference ending the text ends in exactly one space on purpose —
  it closes the session's `@<path>` completion so one Enter sends. A consumer
  that compares session input byte for byte will see that space; it is
  deliberate, not stray padding.

## See also

- [datawatch-definitions.md](../datawatch-definitions.md) — *Vision System*:
  the full integration matrix (comms, sessions, MCP, Council, skills) and the
  supported models
- [config-reference.yaml](../config-reference.yaml) — the `vision:` block with
  every key and its default
- [mcp.md](../mcp.md) — `vision_describe`, `image_paths` on
  `start_session` / `send_input`, and `config_set` / `get_config`
- [../operations.md](../operations.md) — *Vision (image description)*:
  enabling the three backends
