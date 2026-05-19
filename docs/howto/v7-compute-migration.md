---
docs:
  index: true
  topics: [v7, migration, compute, llm]
---
# How-to: v7 Compute Unification — what was migrated

When you first started a v7.0.0-alpha.15 (or later) daemon, it auto-migrated every populated `cfg.<Backend>` block from your v6 `config.yaml` into the new **LLM registry** entries. This page explains what changed, where things moved, and how to verify the migration succeeded.

> **Pre-conditions**: Migration requires the new Compute Nodes registry. See [compute-nodes.md](compute-nodes.md) for the v7 compute model and [llm-registry.md](llm-registry.md) for the unified LLM registry.

## What it is

In v6, each LLM backend had its own top-level config block: `cfg.ollama`, `cfg.openwebui`, `cfg.aider`, `cfg.opencode`, `cfg.opencode_acp`, `cfg.goose`, `cfg.gemini`, `cfg.shell_backend`. Each held a binary path or URL plus per-backend tunables (model, console size, output mode).

In v7, **all of them collapse into one place**: the `LLM registry` (Settings → Compute → LLMs). Each LLM entry is `{name, kind, model, address, compute_nodes, …}`. Sessions and Council debates resolve LLMs by **name**, not by hard-coded backend type.

This eliminates the v6 dual-LLM-concept ambiguity (inference adapters vs session backends) and means every consumer (Council, /api/ask, automata, future agent-spawn) goes through the same dispatcher.

## What was migrated for you

On first v7 startup with this version, the daemon walks your `config.yaml` and creates one LLM registry entry per populated block, named `<backend>-default`:

| v6 block                | v7 LLM entry              | Kind            | Source field    |
|-------------------------|---------------------------|-----------------|-----------------|
| `cfg.ollama`            | `ollama-default`          | `ollama`        | `host` + `model`|
| `cfg.openwebui`         | `openwebui-default`       | `openwebui`     | `url` + `model` |
| `cfg.session.claude_bin`| `claude-code-default`     | `claude-code`   | binary path     |
| `cfg.opencode`          | `opencode-default`        | `opencode`      | binary path     |
| `cfg.opencode_acp`      | `opencode-acp-default`    | `opencode-acp`  | binary path     |
| `cfg.opencode_prompt`   | `opencode-prompt-default` | `opencode-prompt` | binary path |
| `cfg.aider`             | `aider-default`           | `aider`         | binary path     |
| `cfg.goose`             | `goose-default`           | `goose`         | binary path     |
| `cfg.gemini`            | `gemini-default`          | `gemini`        | binary path     |
| `cfg.shell_backend`     | `shell-default`           | `shell`         | script path     |

Migration is **idempotent** — re-runs skip any name that already exists, so editing the LLM entry in the PWA persists across restarts.

The migration is also **additive** — the v6 cfg blocks remain in `config.yaml` so external scripts that read them keep working through the v7.0.x window. Hard removal happens in v7.0.x patches once we confirm no broken consumers.

## How to verify

1. **PWA**: Settings → Compute → **LLMs** card. You should see one entry per backend that was populated in your v6 config. Auto-created entries are tagged `auto`.
2. **CLI**: `datawatch llm list` lists every entry.
3. **REST**: `curl -sk $BASE/api/llms | jq '.llms[].name'`.
4. **Daemon log**: lines like `[inference] auto-migrated legacy cfg → llm/ollama-default` confirm what was created.
5. **Migration status**: `curl -sk $BASE/api/migration/status` returns the canonical list with timestamp.

## What to do next

- **Edit any auto-created entry** to set the right ComputeNode failover list, model, or API key. Use the ✏️ Edit button (Form/YAML toggle) and the 🧪 Test action to verify before saving.
- **Add new LLMs** for hosts the migration didn't know about (remote GPU boxes, cloud Anthropic API, etc.). Use the `+ Add LLM` form on the same card.
- **Dismiss the migration toast** by clicking ✕ on it (or `curl -sk -X DELETE $BASE/api/migration/status` from the CLI). It won't show again.

## Migration didn't pick up my backend

Reasons a backend might not have migrated:

- **Block was empty.** Migration skips entries with no address/binary. Set the value in `config.yaml` and restart, or just add the LLM via the PWA `+ Add LLM` form.
- **Name already existed.** Migration is idempotent. If you previously added a manual LLM with the same name, the auto-migration didn't overwrite it. Edit the existing entry instead.
- **Reading the wrong config file.** `datawatch config show` confirms which file the daemon loaded.

## Why v7 unification

Before v7, datawatch had **two parallel LLM concepts** that operators had to reason about separately: "inference adapters" (used by Council, /api/ask) and "session backends" (used by tmux-spawning sessions). They had different config blocks, different APIs, different mental models — and no single answer to "what models are available?".

v7 collapses these into one model: every LLM is in the registry; every consumer (inference, session, automata) uses the same name to refer to it. The dispatcher routes through ordered ComputeNode failover. The Compute tab in Settings is the single source of truth.

## See also

- [`compute-mode.md`](compute-mode.md) — Compute Nodes + LLMs registry primer.
- [`council-mode.md`](council-mode.md) — Council Mode uses LLM registry names.
- [`chat-and-llm-quickstart.md`](chat-and-llm-quickstart.md) — quick-start for sessions targeting an LLM by name.
