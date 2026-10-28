# LLM Backends

datawatch supports multiple AI coding backends. The active backend is selected via
`session.llm_backend`. Each backend runs inside a tmux session so you can attach and
interact with it directly.

---

## Selecting a Backend

| Method | How |
|--------|-----|
| **YAML** | `session.llm_backend: claude-code` in `~/.datawatch/config.yaml` |
| **CLI** | `datawatch setup llm` (interactive wizard) |
| **Web UI** | Settings tab → General → **Datawatch** card → Default LLM backend dropdown |
| **REST API** | `PUT /api/config` with `{"session.llm_backend": "aider"}` |
| **Comm channel** | `configure session.llm_backend=aider` |

Per-backend settings (binary path, console size, output/input mode) are configurable via:
- **Web UI**: Settings tab → **LLM** tab → click any backend to expand its config popup
- **REST API**: `PUT /api/config` with e.g. `{"aider.binary": "/usr/local/bin/aider"}`
- **Comm channel**: `configure aider.binary=/usr/local/bin/aider`

Available backends: `claude-code`, `aider`, `goose`, `gemini`, `opencode`, `opencode-acp`, `opencode-prompt`, `ollama`, `openwebui`, `shell`

---

## Backend Comparison

> Interactive input here means: while a session is running, the operator
> can reply via `send <id>: <text>` (CLI / chat / MCP) or the PWA input
> bar, and the backend picks the message up. Backends marked **No** run
> a single non-interactive task and exit; you control them via
> scheduled commands and output filters instead. Some backends ship
> **two variants** (regular task launcher *and* an interactive
> chat/conversation mode); the table flags both.

| Backend | Name | Interactive input | External service | Notes |
|---|---|---|---|---|
| Claude Code | `claude-code` | **Yes** | Anthropic API | Default; agentic coding loop. Channel mode (MCP bridge) wires structured tool callbacks + permission relay. |
| Shell | `shell` | **Yes** | Custom command | Bring-your-own; tmux pane stays attached so anything that reads stdin works. |
| opencode-acp | `opencode-acp` | **Yes** | Configurable (anthropic / openai / others via opencode) | opencode driven over the Agent Coding Protocol (HTTP/SSE). Thinking + structured replies render in the chat panel. |
| OpenWebUI (chat) | `openwebui` (chat mode) | **Yes** | OpenWebUI instance | The `InteractiveBackend` variant uses the OpenAI-compatible chat completions API; pure Go, no tmux. Picked when you start a session with the chat input mode. |
| Ollama (chat) | `ollama` (chat mode) | **Yes** | Local Ollama server | `LaunchChat` mode streams replies into the PWA chat panel; tmux not used. Fully local, no API key. |
| aider | `aider` | No | Configurable (OpenAI, Anthropic, etc.) | Single-shot batch task; finishes when the prompt completes. Use schedules + filters for follow-ups. |
| goose | `goose` | No | Configurable | Block's open-source agent. Single-shot. |
| Gemini CLI | `gemini` | No | Google AI | Google's official CLI. Single-shot. |
| opencode | `opencode` (TUI mode) | No | Configurable | SST's TUI agent run inside tmux. The interactive variant is **opencode-acp** (above). |
| OpenWebUI (task) | `openwebui` (default) | No | OpenWebUI instance | Default `Backend` variant runs a single-shot OpenAI-compatible completion. |
| Ollama (task) | `ollama` (default) | No | Local Ollama server | Default `Backend` variant runs a single-shot completion. |

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

Five backends accept interactive input from datawatch (the
`send <id>: <msg>` command, the PWA input bar, and chat / Signal /
Telegram replies):

- **`claude-code`** — full agentic loop, plus channel mode for
  structured tool callbacks.
- **`shell`** — anything you can drive over stdin in a tmux pane.
- **`opencode-acp`** — opencode over the Agent Coding Protocol,
  with thinking blocks rendered in the chat panel.
- **`openwebui` (chat mode)** — `InteractiveBackend` variant using
  the OpenAI-compatible chat completions API.
- **`ollama` (chat mode)** — `LaunchChat` variant streaming replies
  into the PWA chat panel.

The other backends (`aider`, `goose`, `gemini`, `opencode` TUI,
`openwebui` default, `ollama` default) run a single non-interactive
task and exit when complete. For these:

- The session moves to `complete` or `failed` when the process exits
- Output filters and scheduled commands still fire normally
- You can still view output with `status <id>` and `tail <id> [n]`

### One-shot (`one_shot`) sessions

All backends named `*-prompt` (`pi-prompt`, `goose-prompt`, `opencode-prompt`, …)
are one-shot: the `one_shot` flag is recorded on the session at creation, even
when the session is started by hand (PWA, REST, MCP) without a `one_shot` field
in the request. Interactive backends (`claude-code`, `pi`, `aider`, `goose` TUI,
`gemini`, `opencode`, `opencode-acp`, `shell`, chat-mode backends) are **not**
flagged one-shot by default — `one_shot` only appears if a start request
explicitly asks for it. The decision is made once, at session creation; existing
session records are never rewritten retroactively.

The PWA session list and detail views do not read the `one_shot` flag on a
session: input boxes, reply prompts, and waiting-for-input affordances are
offered only while the session is in a running or waiting state, so a finished
prompt-style session renders as any other completed session (state badge,
output viewable, no input affordances).

---

## Model/backend pairing

A model name must be one the chosen backend can serve. datawatch checks each
model against its backend when the model is set, and refuses a mismatched
pair. The refusal names the model and the backend.

### The pairing rule

- A bare Claude alias such as `claude-haiku-5-5` is refused on the `pi` and
  `pi-prompt` backends. pi resolves a bare alias across providers and exits
  with no output. Use a provider-qualified name such as
  `anthropic/claude-haiku-5-5` on pi, or use the `claude-code` backend with a
  bare name.
- A provider-tagged model such as `ollama/qwen3.8:27b` is refused on
  `claude-code`, which takes only bare Claude model names. Use a bare name on
  `claude-code`.

The check runs when a PRD, story, task, or verification or guardrail model is
set. It applies to REST, MCP, and the comm channel alike. A refused set leaves
the record unchanged; REST returns `400` with the mismatch text, and MCP returns
a tool error.

The check runs again before a PRD runs. A PRD that contains a mismatched task is
not started, and the error names each offending task. This means records written
before the check existed are also refused, instead of spawning a session that
exits with no output.

### pi-prompt failure marker

On the `pi-prompt` backend, a run that fails no longer reports success. A
non-zero exit from pi, or a run that produces no output, prints
`DATAWATCH_PI_FAILED` and does not print `DATAWATCH_COMPLETE`. The failure line
names the exit status and the model, for example:

```
DATAWATCH_PI_FAILED: pi exited with status 3 (model ollama/qwen3.8:27b)
DATAWATCH_PI_FAILED: pi produced no output
```

When you see `DATAWATCH_PI_FAILED`, check these two things:

1. **The model name.** Confirm it is provider-qualified (`provider/model`) and
   matches the `pi_prompt.model` setting, or the model set on the PRD, story,
   or task.
2. **The pi provider.** Run `pi` by hand in the project directory with the same
   model. Confirm the provider is configured (`pi auth`, or pi's
   `~/.pi/agent/models.json`) and that its endpoint is reachable. For Ollama,
   check that the server is running and has the model pulled.

Fix the cause, then run the task again.

## Judge and planning model resolution (Automata)

Every guardrail **judge** session and every decompose (planning) session
launched by the autonomous runner must carry a model **you configured**.
datawatch never falls back to `pi`'s — or any other backend's — built-in
default model for these sessions: a judge running on an unpinned model would
evict the model you pinned on the node, and a decompose session would spend
its whole run on a model you may not want or pay for.

### Guardrail judge resolution order

Every guardrail judge — `rules-review`, `ci-watch`, the per-task and
per-story guardrail verdicts (what the verify step judges), and the rejudge
action — resolves its model through a single resolver with a five-tier
precedence chain. The first tier that is set **and** that the judge's
backend can serve wins:

| Tier | Source | What it is |
|---|---|---|
| 1 | PRD `guardrail_model` | the Automaton's own guardrail judge pin |
| 2 | task `guardrail_model` | the override on the specific task being judged |
| 3 | `autonomous.guardrail_model` | the global guardrail judge model |
| 4 | `autonomous.verification_model` | the global verification model |
| 5 | `autonomous.planning_model` | the global planning model |

Tiers 1–2 are explicit pins for the resolved judge backend.
Tiers 3–5 are global defaults configured for the global pi-family backend;
when a judge resolves to the `claude-code` backend they are skipped (they
are pi-style model names that backend would reject), and only the explicit
PRD/task pins may win. Every tier is validated against the judge's resolved
backend with the [pairing rule](#the-pairing-rule) above — a provider-tagged
model is refused on `claude-code`, a bare Claude alias is refused on `pi`
and `pi-prompt` — so a tier that the backend cannot serve is skipped with a
recorded reason, never launched.

### Loud failure when no tier resolves

If no tier yields a model the backend can serve — either nothing is
configured, or every configured tier is unpairable for the judge's backend —
the judge is **not launched at all**. The guardrail record gets a loud,
non-retried infrastructure-category block instead, and the message names
exactly which keys to set:

- the PRD's `guardrail_model` pin, or
- the task's `guardrail_model` override, or
- `autonomous.guardrail_model`, or
- `autonomous.verification_model`, or
- `autonomous.planning_model`.

`pi`'s built-in default model is never used, and the model handed to `pi`
is never empty. The guardrail model setters (`set_prd_guardrail_llm` /
`set_task_guardrail_llm`, on REST, MCP and the comm channel) validate the
model/backend pair — and, when a model is pinned, the full resolution —
before accepting the change, so a pin that would leave the unit with no
resolvable model is refused at set time rather than failing at judge time.

### Decompose (planning) sessions

Decompose sessions follow the same no-built-in-default rule. Their model
resolves as the PRD's `decomposition_model`, then `autonomous.planning_model`
(when it belongs to the resolved planning backend), then the PRD's own
model, then `autonomous.verification_model` — with each tier checked against
the resolved backend. When no tier resolves and the backend is pi-family,
the decompose attempt fails immediately (without consuming infrastructure
retry budget) with a message naming the same set of keys. A decompose
session is therefore never launched on the backend's built-in default.

### Coverage

This rule covers the `rules-review` judgment, the `ci-watch` guardrail, the
per-task and per-story guardrail verdicts, the rejudge action, and the
decompose (planning) session. The verifier's own model is a separate pin
(task → story → PRD → `autonomous.verification_model`), but a guardrail
judge that cannot resolve one of the tiers above always fails loudly
rather than borrowing the backend default.

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

### Session naming

When a datawatch session has a name (set in the New Session form or via API), Claude is
launched with `--name <session-name>`. This tags the Claude conversation for easy
identification in Claude's `/resume` picker.

### Session resume

claude-code stores conversation history keyed by a session ID. To resume a previous
conversation, select it from the **Resume previous session** dropdown in the New Session
form (web UI), or pass it via API. The session is launched with `--resume <ID>`.

The web UI **Restart** button automatically pre-fills the form with the finished
session's details (name, task, directory, backend) and selects it in the resume dropdown.

### Rate limit detection

claude-code is the only backend with captured throttle lines. Every line below
was copied verbatim from the fixtures in `testdata/rate_limit_lines/` (the
directory README records each line's in-tree source):

```
⎿  You've hit your limit · resets 10pm (America/New_York)
5-hour limit reached ∙ resets 2pm
Weekly limit reached
Hit weekly limit
Opus limit reached
Sonnet limit reached
You're out of extra usage · resets 11:50am (America/New_York)
```

Before a line is matched, the detector trims surrounding whitespace and
lowercases it, and strips ANSI cursor escapes it would otherwise misread; all
matches are line-start anchored, so a line that merely quotes one of these
phrases inside ordinary output does not match. The `⎿` gutter glyph plus two
spaces that claude-code prints before secondary lines is tolerated as that
family's own prefix. Table phrases are only checked on lines under 2048
characters; the shared protocol line is not subject to that length gate.

On top of the tool's own lines, the session carries two plain protocol lines
the detector recognizes line-start on every backend: `DATAWATCH_RATE_LIMITED:`
when the session reports a throttle (claude-code also writes `PAUSED.md` with
a progress summary while paused), and `DATAWATCH_COMPLETE:` when it finishes.
A rate-limited claude-code session is held in a paused `rate_limited` state and
resumed on retry, not failed — it still completes normally.

If you set `detection.rate_limit_patterns`, your list replaces these built-in
matches and is applied as before: a flat substring, anywhere on the line, for
all backends.

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
  binary: goose               # path to goose binary; default: "goose"

  # Provider / model injection (BL363 T2) — datawatch injects these as
  # GOOSE_PROVIDER and GOOSE_MODEL environment variables at session launch.
  # When set here, goose does not need its own ~/.config/goose/config.yaml.
  provider: anthropic         # e.g. anthropic, openai, google
  model: claude-sonnet-5    # model name accepted by the provider

  # API key — literal value or ${secret:name} resolved from secrets store.
  # Injected as the provider-specific key env var (ANTHROPIC_API_KEY,
  # OPENAI_API_KEY, GOOGLE_API_KEY, or GOOSE_API_KEY for others).
  # Never logged. Omit if the key is already in the environment.
  api_key_ref: ${secret:goose-api-key}

  # MCP channel bridge (BL363 T3) — when true, datawatch registers itself
  # as a Goose MCP extension via GOOSE_MCP__DATAWATCH__* env vars so Goose
  # can call datawatch tools (memory, sessions, etc.) from within a session.
  channel_enabled: false
```

### How it runs

```bash
# Interactive mode (goose backend)
cd <project_dir> && GOOSE_CLI_THEME=plain GOOSE_PROVIDER=anthropic ... goose session [--name <name>]

# One-shot mode (goose-prompt backend)
cd <project_dir> && GOOSE_CLI_THEME=plain GOOSE_PROVIDER=anthropic ... goose run --text '<task>'
```

### Notes

- Provider, model, and API key can be injected via datawatch config (see above) — goose does **not** need its own `~/.config/goose/config.yaml` when these are set.
- `goose` backend is interactive (full TUI); `goose-prompt` is one-shot and requires a task prompt.
- Session resume is supported via named sessions (`--name`); datawatch tracks the name automatically.
- `GOOSE_CLI_THEME=plain` is always injected to prevent escape-sequence bleed in tmux captures.
- With `channel_enabled: true`, Goose can call datawatch MCP tools (memory recall, session management, etc.) from within the session — the session ID is threaded through `--caller-session-id`.

---

## pi

**Name:** `pi` (interactive) / `pi-prompt` (one-shot)

[pi](https://pi.dev) ([earendil-works/pi](https://github.com/earendil-works/pi)) is a
minimal open-source coding agent harness that calls provider SDKs (Anthropic,
OpenAI, Google, Ollama, and more) directly rather than through a compatibility
shim. It is a lower-risk alternative to opencode for open-model execution.

### Prerequisites

- pi installed: `npm install -g @earendil-works/pi-coding-agent`
- At least one provider configured (e.g. via `pi auth`, or pointed at a local
  Ollama endpoint — see pi's own `~/.pi/agent/models.json`)

### Installation

```bash
npm install -g @earendil-works/pi-coding-agent

# Verify
pi --version
```

### Configuration

```yaml
session:
  llm_backend: pi   # or pi-prompt for one-shot execution

pi:
  enabled: true
  binary: pi                      # path to pi binary; default: "pi"
  model: ollama/qwen3.8:27b       # optional "provider/model"; empty uses pi's own default

pi_prompt:
  enabled: true
  binary: pi                      # defaults to pi.binary when empty
  model: ollama/qwen3.8:27b       # optional, same form as pi.model
```

### How it runs

```bash
# Interactive mode (pi backend) — stays open after the first turn
cd <project_dir> && pi --approve --session-id '<id>' '<task>'

# One-shot mode (pi-prompt backend) — blocks until done, then exits
cd <project_dir> && pi --print --mode text --approve '<task>'
```

### Which node a pi worker uses

A pi or pi-prompt worker runs on the compute node its capacity lease holds.
When the node is known, the daemon points pi at that node for the launch. The
node is the one the entry's pool chose, or the entry's only node (see
[Compute-node pools and slots](howto/automata-capacity.md#compute-node-pools-and-slots)).

- The daemon writes a per-session pi configuration directory. Every entry of
  the source directory is linked into it, so credentials and sessions stay the
  source's own. Only `models.json` is rewritten: the `baseUrl` of the model's
  provider becomes the node's address with `/v1` appended. An address with no
  `http://` or `https://` scheme gets `http://` first.
- The launch command sets `PI_CODING_AGENT_DIR` to that directory for the pi
  process alone. The directory is removed when pi exits, or at once when the
  launch fails.
- The source directory is the daemon's own `PI_CODING_AGENT_DIR` when that is
  set, otherwise `~/.pi/agent`. Every other provider keeps the value the source
  wrote.
- A session keeps the node address it started on, so a resume reuses the same
  host.

A queued request picks its node again on each admission pass and whenever any
node frees an engine. A waiting request therefore moves to a node with a free
engine, least loaded first and then by node priority, rather than staying on the
node it first tried. A change to an LLM entry's `compute_nodes` list reaches
waiting work without a daemon restart. The limit of one engine per node still
applies.

The entry's `compute_nodes` list decides which nodes a local-model worker,
verifier and judge may run on. Each listed node that is known and serves the
model is a candidate. The request goes to the least loaded candidate;
`scheduling_priority` only breaks a tie, then list order. A node that does not
serve the model is skipped, and a one-entry list behaves as before.

The provider is the part of the model name before the first `/`; for
`ollama/qwen3.8:27b` it is `ollama`. The launch fails with an error, and nothing
starts, when the model has no provider prefix, or when the source `models.json`
has no entry for that provider or the entry is not a full provider object. A
bare `baseUrl` is not enough: pi needs the entry's api, key and models. A node
with an empty address gets no override at all.

#### When a worker connects to the wrong host

Check these in order:

1. **The node it holds.** `datawatch capacity` prints the node beside each
   holder when the pool spans two or more nodes. If that node is not the one
   you expect, the problem is placement, not pi. See
   [automata capacity](howto/automata-capacity.md).
2. **The node's address.** `datawatch compute node get <name>` must show the
   address the daemon can reach. An empty address means no override, and pi
   uses its own provider table, which is usually the local host.
3. **The provider entry.** The model must be written `<provider>/<model>`, and
   that provider must be a full entry in the source `models.json`. Look at the
   source the daemon reads: its own `PI_CODING_AGENT_DIR`, or `~/.pi/agent`. A
   daemon started by a service manager may not have the variable you set in an
   interactive shell.
4. **The node answers at the override address.** The provider's `baseUrl` is
   `<address>/v1`. The node must answer there; an Ollama node serves its model
   list at `/v1/models`, so `curl http://<address>/v1/models` should list the
   models it holds.
5. **The directory was not left behind.** The per-session directory is removed
   when pi exits, so check the source table and the node address rather than
   looking for the directory.

### Notes

- `pi` backend is interactive (real TUI, stays open for follow-up input);
  `pi-prompt` is one-shot (`--print`), blocks until pi is genuinely done,
  then exits — same low-risk, no-async-completion-signal shape as `aider`.
- Session resume on the `pi` backend uses pi's own `--session-id`, which
  both creates and resumes a session by an arbitrary caller-supplied ID —
  unlike opencode's own ID format, no translation is needed between
  datawatch's session IDs and pi's.
- A third variant analogous to `opencode-acp` (pi's own `--mode rpc`) is
  planned but not yet implemented — see
  `docs/plans/2026-10-16-pi-opencode-evaluation.md`. Building it now would
  reintroduce the async completion-detection race class that `opencode-acp`
  had to fix when it was added.
- Known upstream caveat: pi issue [#9216](https://github.com/earendil-works/pi/issues/9216)
  describes Ollama + `qwen3.8:27b` stream-termination errors under heavy
  tool use — confirmed on datawatch's own exact model/stack. Live-tested
  tasks (3- and 4-tool-call chains, both one-shot and via a standalone
  smoke test with a real git commit) completed cleanly; a full multi-task
  PRD pilot under sustained heavy tool use has not yet been run.

### Rate limit detection

There is no captured pi throttle line in `testdata/rate_limit_lines/` — no
in-tree source quotes one, so no fixture exists for this family. The only
built-in detection for `pi` and `pi-prompt` is the shared `DATAWATCH_RATE_LIMITED:`
protocol line the session itself prints, matched line-start after trimming.
An operator-configured `detection.rate_limit_patterns` (flat substring, all
backends) is the escape hatch if pi starts printing a specific throttle line.

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

### Model Selection (v8.7.0)

OpenCode supports multiple AI providers. Select the model in the **New Session** form when using an OpenCode backend. Format: `provider/model`.

**Cloud models** (provider API key required):
- `anthropic/claude-sonnet-5-5`, `anthropic/claude-opus-5-5`, `anthropic/claude-haiku-5-5`
- `openai/gpt-4o`, `openai/gpt-4o-mini`, `openai/o3-mini`
- `google/gemini-2.0-flash`, `google/gemini-1.5-pro`

**Local models via Ollama** (`ollama/<model-name>`): any model pulled on the Ollama instance for the LLM's compute node — the model dropdown queries that node's live model list directly, so only models actually present there are offered. With no compute node pinned, it falls back to the daemon's local Ollama host.

The selected model is written to `<projectDir>/opencode.json` at session start. For a cloud model this is just the `model` field:
```json
{ "model": "anthropic/claude-sonnet-5" }
```

For an Ollama model, OpenCode has no built-in Ollama provider, so datawatch also writes a `provider.ollama` block using OpenCode's generic OpenAI-compatible adapter — this is what actually makes the model selectable and usable, not just the `model` field:
```json
{
  "model": "ollama/qwen3.8:27b",
  "provider": {
    "ollama": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "Ollama",
      "options": {
        "baseURL": "http://<compute-node-host>:11434/v1",
        "chunkTimeout": 1200000,
        "headerTimeout": 900000
      },
      "models": { "qwen3.8:27b": {} }
    }
  }
}
```

`chunkTimeout`/`headerTimeout` come from `opencode.ollama_chunk_timeout_sec` / `opencode.ollama_header_timeout_sec` (default 1200s / 900s, seconds → ms) — see [SSE stalls on local models](#sse-stalls-on-local-models) below. Set either to `0` to fall back to opencode's own built-in 300s default.

#### SSE stalls on local models (B89)

Large local models (30B+ params, or "thinking"/reasoning-enabled models like qwen3) can go several minutes with zero streamed output — either loading into VRAM on a cold start (`headerTimeout`) or reasoning silently before the first output token (`chunkTimeout`). OpenCode's own defaults for both are 300s (5 min), which is routinely too short: a stalled request aborts client-side and opencode prints `SSE read timed out` (or `SSE error` / `provider response headers`, depending on which timeout tripped and the opencode/ai-sdk version) into the TUI, then sits idle forever waiting for output that will never arrive because the underlying HTTP request is already dead.

Two layers of defense:

1. **Prevention** — `opencode.ollama_chunk_timeout_sec` / `opencode.ollama_header_timeout_sec` (config-accessible via YAML/REST/MCP/comm/PWA per the Configuration Accessibility Rule) raise opencode's own timeouts past the pauses observed on locally-hosted models, so the request doesn't get killed in the first place.
2. **Detection fallback** — the autonomous PRD executor's built-in stall detectors (`cmd/datawatch/sse_stall.go`): the verifier wait loop scans one-shot opencode session scrollback every ~30s and the automata watchdog every 60s, both through the one shared scan in that file. The scan applies only to the opencode family (decided from the session's backend — claude-code, pi, openwebui, ollama and the rest are never scanned for this and keep their own silence and activity detectors), and counts only a line that IS a known provider error as opencode prints it: a short line beginning with the phrase (optionally after an `Error:` or `AI_APICallError:` prefix), compared case-insensitively, that is new relative to the session's previous scan, and not inside a tool result or code block. On a match the session is ended so the executor's retry path fires (e.g. a genuine network/provider outage), with the matched line, the backend family and the scan source (verifier wait loop or automata watchdog) recorded in the end reason. If opencode ever prints a stall message not on the list, it will not be caught automatically — check `docs/plans/README.md` for the current bug if you hit this.

### Language Server Protocol — LSP (v8.7.0)

LSP gives OpenCode real-time language intelligence: type errors, completions, and diagnostics from the language server installed on your host. This is especially valuable when working with LLMs whose training data predates the current version of a library or language.

**How it works:**
1. Select the programming language in the **New Session** form (Go, TypeScript, Python, Rust, C++)
2. Datawatch writes `<projectDir>/opencode.json` with the server config at session start
3. OpenCode launches the configured LSP binary and uses its output during the session

**Prerequisites:** The language server binary must be installed:
```bash
go install golang.org/x/tools/gopls@latest          # Go
npm install -g typescript-language-server typescript  # TypeScript/JS
pip install pyright                                  # Python
rustup component add rust-analyzer                   # Rust
# clangd ships with clang-tools on most distros      # C/C++
```

**Daemon config** (`~/.datawatch/config.yaml`) — customize server commands:
```yaml
lsp:
  servers:
    go:
      command: ["gopls"]
      extensions: [".go"]
    typescript:
      command: ["typescript-language-server", "--stdio"]
      extensions: [".ts", ".tsx", ".js", ".jsx"]
    python:
      command: ["pyright-langserver", "--stdio"]
      extensions: [".py"]
    rust:
      command: ["rust-analyzer"]
      extensions: [".rs"]
    cpp:
      command: ["clangd"]
      extensions: [".cpp", ".cc", ".c", ".h", ".hpp"]
```

**Note:** Claude Code does not support native LSP. For claude-code sessions, use shell commands for diagnostics: `gopls check ./...`, `tsc --noEmit`, `pyright .`.

### Notes

- Uses the `-p`/`--print` flag for non-interactive execution
- For interactive mode with follow-up input, use `opencode-acp` instead (see below)
- **Rate limit detection** — there is no captured opencode throttle line in
  `testdata/rate_limit_lines/` (this covers `opencode`, `opencode-acp` and
  `opencode-prompt`). The opencode-specific lines recorded in-tree
  (`SSE read timed out`, `SSE error`, …) are network/SSE stall diagnostics,
  not throttle messages, so they are not fixtures. The only built-in
  detection for this family is the shared `DATAWATCH_RATE_LIMITED:` protocol
  line the session prints, matched line-start after trimming, with the same
  quoted-phrase rule: a line that only mentions a throttle phrase in ordinary
  output does not pause the backend.

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
- **Remote Ollama**: set `host` to your server URL (e.g. `http://203.0.113.100:11434`). The `ollama` binary does not need to be installed on the datawatch host — availability is checked via `GET /api/tags` on the remote server, not by running `ollama --version`
- Interactive input is not supported via datawatch

### Recommended models

| Model | Size | Best for |
|---|---|---|
| `llama3` | 4.7 GB | General tasks |
| `codellama` | 3.8 GB | Code generation |
| `deepseek-coder` | 776 MB | Code-focused, lightweight |
| `qwen2.5-coder` | 4.7 GB | Strong coding performance |

### Rate limit detection

There is no captured Ollama throttle line in `testdata/rate_limit_lines/` —
nothing in-tree quotes one, and Ollama serves local models, so it rarely
rate-limits at all. The only built-in detection for `ollama` is the shared
`DATAWATCH_RATE_LIMITED:` protocol line the session prints, matched line-start
after trimming; a line that only quotes a throttle phrase in ordinary output
does not pause the backend. An operator-configured `detection.rate_limit_patterns`
(flat substring, all backends) is the escape hatch.

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

The backend uses a native Go HTTP client to communicate with the OpenAI-compatible
chat completions API. It maintains conversation history in memory, enabling multi-turn
interactive sessions with follow-up messages. Responses are streamed via SSE and
displayed in the tmux session in real time.

### Rich Chat UI

OpenWebUI, Ollama, and OpenCode-ACP default to `output_mode: chat` which activates
the rich chat interface in the web UI:

- **Message bubbles** — rounded, with user (right-aligned, blue) and assistant
  (left-aligned, green) layout
- **Avatars** — U (user), AI (assistant), S (system) with colored backgrounds
- **Timestamps** — HH:MM on every message
- **Markdown rendering** — code blocks with syntax styling, bold, italic, bullet lists
- **Streaming** — animated typing dots during response generation
- **Thinking overlay** — collapsible "Thinking..." section for reasoning models
- **Hover actions** — Copy to clipboard, Remember (save assistant response to memory)
- **Memory command quick bar** — buttons for memories, recall, kg query, research
- **Memory commands in chat** — type `remember:`, `recall:`, `kg query`, `research:`
  directly in the chat input; commands are intercepted and processed locally
- **Prompt debounce** — configurable delay (default 3s) before alerting on detected prompts
- **Notification cooldown** — minimum 15s between repeated alerts per session

Backends that default to chat mode:
| Backend | Default `output_mode` | Conversation Manager |
|---------|----------------------|---------------------|
| OpenWebUI | `chat` | Go HTTP (`/api/chat/completions`) |
| Ollama | `chat` | Go HTTP (`/api/chat`) |
| OpenCode-ACP | `chat` | SSE event stream (`/event`) |
| Claude Code | `terminal` | N/A (TUI) |
| OpenCode | `terminal` | N/A (TUI) |

To override for any backend:
```yaml
ollama:
  output_mode: terminal  # disable chat, use raw terminal view
opencode_acp:
  output_mode: log       # disable chat, use log view
```

Any backend that supports interactive input can use the chat interface.

### Notes

- Requires OpenWebUI v0.3+ with the OpenAI-compatible API enabled
- Supports any model available in your OpenWebUI instance (local Ollama models, OpenAI, Anthropic, etc.)
- **Interactive mode supported** — send follow-up messages via the web UI, CLI (`session send`), or messaging channels. Conversation context is maintained across messages.
- No external dependencies (curl, python3) needed — all API communication is handled in Go
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
