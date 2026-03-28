# LLM Backends

datawatch supports multiple AI coding backends. The active backend is selected via
`session.llm_backend` in `~/.datawatch/config.yaml`. Each backend runs inside a tmux
session so you can attach and interact with it directly.

---

## Selecting a Backend

```yaml
# ~/.datawatch/config.yaml
session:
  llm_backend: claude-code   # change to: aider, goose, gemini, opencode, ollama, openwebui, shell
```

Restart `datawatch` after changing the backend.

---

## Backend Comparison

| Backend | Name | Interactive input | External service | Notes |
|---|---|---|---|---|
| Claude Code | `claude-code` | Yes | Anthropic API | Default; best for full agentic coding |
| aider | `aider` | No | Configurable (OpenAI, Anthropic, etc.) | Non-interactive batch mode |
| goose | `goose` | No | Configurable | Block's open-source agent |
| Gemini CLI | `gemini` | No | Google AI | Google's official CLI |
| opencode | `opencode` | No | Configurable | SST's TUI-based agent |
| opencode ACP | `opencode-acp` | Yes | Configurable | opencode via HTTP/SSE API; channel replies |
| Ollama | `ollama` | No | Local (Ollama server) | Fully local, no API key |
| OpenWebUI | `openwebui` | No | OpenWebUI instance | OpenAI-compatible API |
| Shell | `shell` | No | Custom | Bring your own script |

---

## Command and Filter Support

datawatch's saved command library and output filter engine work with any LLM backend.

### Saved Commands

Named commands from the library (`datawatch cmd add <name> <text>`) can be:
- Sent via any messaging backend: `send <id>: approve` (where `approve` is a saved command)
- Scheduled: `datawatch session schedule add <id> approve`
- Applied automatically via output filters

Seed useful defaults with `datawatch seed`:

| Name | Value | Use case |
|---|---|---|
| `approve` | `yes` | Approve a permission or prompt |
| `reject` | `no` | Decline a prompt |
| `enter` | `\n` | Send a blank Enter |
| `continue` | `continue` | Resume a paused session |
| `skip` | `skip` | Skip current step |
| `abort` | `Ctrl-C` | Interrupt the running session |

### Output Filters

Output filters (`datawatch cmd filter add`) watch session output for regex patterns and trigger actions automatically. Useful for hands-free operation:

| Filter trigger | Suggested action | Example pattern |
|---|---|---|
| Permission dialog | `alert` | `Do you want to proceed\?` |
| Rate limit hit | `alert` | `rate.limit` |
| Task complete | `alert` | `(All done|DATAWATCH_COMPLETE)` |
| Recurring prompt | `schedule approve` | `Overwrite existing file\?` |

Filters are configured per backend but apply to all sessions regardless of which LLM runs them.

### Interactive Input Support

Only `claude-code` supports interactive input from datawatch (the `send <id>: <msg>` command and quick-input buttons). All other backends run non-interactively and exit when the task completes. For non-interactive backends:

- The session moves to `complete` or `failed` when the process exits
- Output filters and scheduled commands still fire normally
- You can still view output with `status <id>` and `tail <id> [n]`

---

## claude-code (default)

**Name:** `claude-code`

Anthropic's official Claude Code CLI. This is the default backend. It supports full
interactive input — datawatch can detect when claude-code is waiting for your response
and route replies from your messaging app or MCP client directly to it.

### Prerequisites

- `claude` CLI installed and authenticated (`claude auth login`)
- Anthropic API access

### Installation

```bash
npm install -g @anthropic-ai/claude-code
# or follow https://docs.anthropic.com/en/docs/claude-code
```

### Configuration

```yaml
session:
  llm_backend: claude-code
  claude_code_bin: claude          # path to the claude binary; default: "claude" (claude-code only)
  skip_permissions: false          # pass --dangerously-skip-permissions (claude-code only)
  channel_enabled: true            # enable MCP channel server (claude-code only)
```

### How it runs

```bash
claude --print --add-dir <project_dir> '<task>'
```

The `--add-dir` flag limits claude-code's file access to the project directory. Output
is piped into a tmux session and logged to `~/.datawatch/logs/<hostname>-<id>.log`.

### Interactive input

When claude-code outputs a prompt ending with `?`, `[y/N]`, `>`, or similar patterns,
datawatch marks the session as `waiting_input` and notifies you via your messaging
backend. Reply with `send <id>: <your answer>` to route input back to the session.

### Channel mode (MCP channel)

claude-code supports an optional MCP channel (`channel_enabled: true`) that adds a
second, structured communication path alongside the tmux console. Tool output and
Claude's replies flow over the channel as clean text; folder-trust and consent prompts
still require console interaction.

See [docs/claude-channel.md](claude-channel.md) for a full breakdown of what is
handled over the channel vs the console, one-time prompt handling, and setup.

### Session resume

claude-code stores conversation history keyed by a session ID. To resume a previous
conversation, set the **Resume session ID** field in the New Session form (web UI) or
pass it via API. The session is launched with `--resume <ID>`.

The web UI **Restart** button automatically pre-fills this field from the finished
session's `llm_session_id` so the resumed session continues the prior conversation.

---

## aider

**Name:** `aider`

[aider](https://aider.chat) is a terminal-based AI pair programmer supporting many
LLM providers (OpenAI, Anthropic, local models via Ollama, and more).

### Prerequisites

- aider installed: `pip install aider-chat`
- At least one LLM API key set (e.g. `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`)

### Installation

```bash
pip install aider-chat

# Verify
aider --version
```

### Configuration

```yaml
session:
  llm_backend: aider

aider:
  enabled: true
  binary: aider       # path to aider binary; default: "aider"
```

### How it runs

```bash
cd <project_dir> && aider --yes --message '<task>'
```

Runs in `--yes` mode (non-interactive, auto-accepts changes). Output is captured in
the tmux session.

### Notes

- aider reads its own config from `~/.aider.conf.yml` or the project directory
- Set your LLM provider in aider's config or via environment variables
- aider does not support interactive input via datawatch (use the tmux session directly)
- aider supports git: it will automatically commit changes after each task

---

## goose

**Name:** `goose`

[goose](https://github.com/block/goose) is Block's open-source AI agent supporting
OpenAI, Anthropic, and other providers.

### Prerequisites

- goose installed (follow [goose installation docs](https://github.com/block/goose#installation))
- LLM provider configured (e.g. `OPENAI_API_KEY`)

### Installation

```bash
# macOS/Linux via script
curl -fsSL https://github.com/block/goose/releases/latest/download/install.sh | bash

# Verify
goose version
```

### Configuration

```yaml
session:
  llm_backend: goose

goose:
  enabled: true
  binary: goose       # path to goose binary; default: "goose"
```

### How it runs

```bash
cd <project_dir> && goose run --text '<task>'
```

### Notes

- goose has its own config (`~/.config/goose/config.yaml`) for provider and model selection
- Interactive input is not supported via datawatch
- goose creates its own session context per run

---

## Gemini CLI

**Name:** `gemini`

Google's [Gemini CLI](https://github.com/google-gemini/gemini-cli) for running Gemini
models from the command line.

### Prerequisites

- Gemini CLI installed
- `GEMINI_API_KEY` environment variable set (or auth via `gemini auth`)

### Installation

```bash
npm install -g @google/gemini-cli
# or follow https://github.com/google-gemini/gemini-cli

# Verify
gemini --version
```

### Configuration

```yaml
session:
  llm_backend: gemini

gemini:
  enabled: true
  binary: gemini      # path to gemini binary; default: "gemini"
```

### How it runs

```bash
cd <project_dir> && gemini -p '<task>'
```

### Notes

- Authenticate with `gemini auth` before first use
- The `-p` flag passes the task as a prompt for non-interactive execution
- Interactive input is not supported via datawatch

---

## opencode

**Name:** `opencode`

[opencode](https://github.com/sst/opencode) is SST's terminal AI coding agent with
support for multiple providers.

### Prerequisites

- opencode installed
- Provider API key configured

### Installation

```bash
npm install -g opencode-ai
# or follow https://github.com/sst/opencode

# Verify
opencode --version
```

### Configuration

```yaml
session:
  llm_backend: opencode

opencode:
  enabled: true
  binary: opencode    # path to opencode binary; default: "opencode"
```

### How it runs

```bash
cd <project_dir> && opencode -p '<task>'
```

### Session resume

opencode stores conversation history by session ID. To resume a prior conversation,
set the **Resume session ID** field in the New Session form. The session is launched
with `-s <ID>`. The **Restart** button pre-fills this automatically.

### Notes

- Uses the `-p`/`--print` flag for non-interactive execution
- Interactive input is not supported via datawatch

---

## opencode (ACP mode)

**Name:** `opencode-acp`

ACP mode starts opencode as an HTTP server (`opencode serve`) and communicates with it
via its REST + SSE API. This gives richer bidirectional interaction than the `-p` flag mode.

### Configuration

```yaml
session:
  llm_backend: opencode-acp

opencode:
  enabled: true
  binary: opencode
```

### How it runs

```bash
cd <project_dir> && opencode serve --port <random-free-port>
```

datawatch then:
1. Waits for the server to become ready (`GET /session`)
2. Creates a session (`POST /session`)
3. Sends the task (`POST /session/<id>/message`)
4. Streams events from `GET /event` (SSE)

### Channel replies

ACP replies (from `message.part.updated` SSE events) are broadcast as `channel_reply`
WebSocket messages to the web UI, which renders them as amber-highlighted lines in the
session detail output area — the same visual treatment as claude MCP channel replies.
This means you can distinguish the AI's responses from raw tool/process output.

### Interactive input

Follow-up messages sent via `send <id>: <message>` (or the web UI input bar) are
routed through the opencode HTTP API (`POST /session/<id>/message`) instead of
`tmux send-keys`, giving clean message delivery without terminal interference.

### Session resume

Set **Resume session ID** in the New Session form to reuse an existing opencode session.

### ACP vs tmux: what goes where

| Interaction | tmux | ACP (HTTP/SSE) |
|---|---|---|
| Initial task delivery | No | **Yes — POST /session/<id>/message** |
| Follow-up messages (`send <id>: …`) | No | **Yes — POST /session/<id>/message** |
| AI text replies | In tmux TUI | **Also here (channel_reply, amber)** |
| Tool execution output | In tmux TUI | Via SSE events → log |
| Session output log | tmux pane | SSE events written to log file |
| Folder trust / first-run prompts | N/A (no claude CLI) | N/A |
| Rate-limit / complete detection | Log pattern | Log pattern |

> ACP mode uses **no tmux input paths** — all user messages and AI replies flow
> through the HTTP API. The tmux window exists so you can attach and watch live,
> but all datawatch interaction bypasses it.

### Notes

- Does not require tmux interaction for input — ACP uses the HTTP API
- Output filter engine watches log lines written from SSE events

---

## Ollama (local models)

**Name:** `ollama`

[Ollama](https://ollama.ai) runs large language models locally. No API key required —
models run entirely on your hardware.

### Prerequisites

- Ollama installed and running (`ollama serve`)
- At least one model pulled (e.g. `ollama pull llama3`)

### Installation

```bash
# Linux
curl -fsSL https://ollama.ai/install.sh | sh

# macOS
brew install ollama

# Start Ollama server
ollama serve

# Pull a model
ollama pull llama3
ollama pull codellama
ollama pull deepseek-coder

# Verify
ollama list
```

### Configuration

```yaml
session:
  llm_backend: ollama

ollama:
  enabled: true
  model: llama3                    # model name; default: "llama3"
  host: http://localhost:11434     # Ollama server URL; default: "http://localhost:11434"
```

### How it runs

```bash
cd <project_dir> && ollama run <model> '<task>'
```

### Notes

- Ollama runs locally — no internet connection required after model download
- Model quality varies; `codellama`, `deepseek-coder`, or `qwen2.5-coder` are good for coding tasks
- **Remote Ollama**: set `host` to your server URL (e.g. `http://192.168.1.100:11434`). The `ollama` binary does not need to be installed on the datawatch host — availability is checked via `GET /api/tags` on the remote server, not by running `ollama --version`
- Interactive input is not supported via datawatch

### Recommended models

| Model | Size | Best for |
|---|---|---|
| `llama3` | 4.7 GB | General tasks |
| `codellama` | 3.8 GB | Code generation |
| `deepseek-coder` | 776 MB | Code-focused, lightweight |
| `qwen2.5-coder` | 4.7 GB | Strong coding performance |

---

## OpenWebUI

**Name:** `openwebui`

Calls an [OpenWebUI](https://github.com/open-webui/open-webui) instance via its
OpenAI-compatible API. OpenWebUI is a self-hosted UI for local and remote LLMs.

### Prerequisites

- OpenWebUI running (locally or on your network)
- API key if auth is enabled on your OpenWebUI instance

### Configuration

```yaml
session:
  llm_backend: openwebui

openwebui:
  enabled: true
  url: http://localhost:3000      # OpenWebUI base URL; default: "http://localhost:3000"
  model: llama3                   # model name as shown in OpenWebUI; default: "llama3"
  api_key: ""                     # API key if OpenWebUI auth is enabled
```

### How it runs

The backend sends a chat completion request to `<url>/api/chat/completions` using curl
and streams the response into the tmux session.

### Notes

- Requires OpenWebUI v0.3+ with the OpenAI-compatible API enabled
- Supports any model available in your OpenWebUI instance (local Ollama models, OpenAI, Anthropic, etc.)
- Interactive input is not supported via datawatch
- The API key can also be set via the `OPENWEBUI_API_KEY` environment variable

---

## Shell (custom script)

**Name:** `shell`

Run any program or script as the AI backend. Your script receives the task and project
directory as arguments and is responsible for running the AI and producing output.

### Configuration

```yaml
session:
  llm_backend: shell

shell_backend:
  enabled: true
  script_path: ~/scripts/my-ai.sh   # path to your script
```

### Script interface

Your script is called with:

```bash
/path/to/your/script '<task>' '<project_dir>'
```

- `$1` — task description (the text sent in `new: <task>`)
- `$2` — absolute path to the project directory

Output is captured in the tmux session. When your script exits, the session is marked
complete.

### Example script

```bash
#!/usr/bin/env bash
# my-ai.sh — custom AI backend example
set -euo pipefail

TASK="$1"
PROJECT_DIR="$2"

cd "$PROJECT_DIR"

# Call any AI tool you like
my-custom-ai-tool --task "$TASK" --dir "$PROJECT_DIR"
```

### Notes

- The script must be executable: `chmod +x ~/scripts/my-ai.sh`
- Use this to integrate with any AI tool not directly supported by datawatch
- Interactive input is not supported; the script runs to completion
- The completion marker `DATAWATCH_COMPLETE: shell done` is emitted automatically after the script exits

---

## Adding a New LLM Backend

To add a new backend:

1. Create `internal/llm/backends/<name>/backend.go` implementing the `llm.Backend` interface:

```go
type Backend interface {
    Name() string
    Launch(ctx context.Context, task, tmuxSession, projectDir, logFile string) error
    SupportsInteractiveInput() bool
    Version() string
}
```

2. Register it in `internal/llm/registry.go`
3. Add config fields to `internal/config/config.go`
4. Wire it up in `cmd/datawatch/main.go`
5. **Document it in this file** (`docs/llm-backends.md`) with full config and usage details
6. Update `docs/backends.md` summary table
7. Update `CHANGELOG.md` under `[Unreleased]`
