---
docs:
  index: true
  topics: [docs, mcp, search, apply, automation]
exec_params:
  - {name: query, required: true, description: "Free-text question or keyword to search the docs corpus"}
  - {name: howto_id, required: false, description: "Howto path (e.g. howto/secrets-manager.md) for plan/execute mode"}
exec_steps:
  - tool: docs_search
    description: Find docs chunks ranked by hybrid (vector → BM25 fallback) score
    args:
      q: "{{params.query}}"
      limit: 10
    read_only: true
  - tool: docs_list_howtos
    description: List every available howto with provenance flag (authored vs LLM-translatable)
    args: {}
    read_only: true
---
# How-to: Docs-as-MCP-Interface (the operator's AI-driven docs surface)

Datawatch ships every doc, howto, and plan as a search + execute surface
the AI agents can drive directly. Search returns ranked chunks; execute
turns a curated howto into a deterministic MCP-call sequence the
operator approves once and the agent runs.

## What it is

Four MCP tools, each mirrored across REST + CLI + comm + PWA:

| Tool | Purpose |
|---|---|
| `docs_search` | Hybrid search (vector primary + BM25 fallback) over core + trusted skill + trusted plugin sources. Trust filtering applied. |
| `docs_read` | Read a specific chunk by `path` + `anchor`. The `path` parameter must use the full relative path including subdirectory prefix, e.g. `"path": "howto/daemon-operations.md"` — **not** just the filename or a bare howto name. |
| `docs_list_howtos` | List every howto in the index. `has_exec_steps:true` means the howto carries an authored MCP-call sequence; `false` means LLM-translation is required at apply time. |
| `docs_apply` | Plan-then-execute. `mode=plan` returns the resolved exec_steps + an `approval_token`. `mode=execute` consumes the token and runs each step via the in-process MCP dispatcher. `risk_gate=true` pauses before each mutating step and issues a continuation token. |

> **MCP `docs_read` path format**: Always pass the `path` parameter as a relative path with directory prefix. Example MCP call: `{"tool": "docs_read", "params": {"path": "howto/daemon-operations.md"}}`. Using `{"howto": "daemon-operations.md"}` or omitting the `howto/` prefix will not resolve correctly.

## Two happy paths

### Search + read

```sh
# Operator can use the same query through any surface.
datawatch docs search "rotate a secret"           # CLI
peers docs_search rotate a secret                 # comm verb
curl https://localhost:8443/api/docs/search?q=rotate+a+secret   # REST
# PWA: Settings → General → Docs Search card
```

### Plan-then-execute

```sh
# 1. Plan — returns exec_steps + an approval_token.
datawatch docs apply howto/secrets-manager.md \
    --param name=GITHUB_TOKEN --param value=ghp_...
# → {howto_id, steps: [...], approval_token: "f7a2…", risk_gate: false}

# 2. Execute — consumes the token. With risk_gate=true the executor
#    pauses before each mutating step and issues a continuation token.
datawatch docs apply howto/secrets-manager.md \
    --mode execute --approval-token f7a2… --risk-gate
```

## Trust model (operator-controlled)

Skills and plugins are isolated per-source (`skill:<name>` / `plugin:<name>`).
On first sight, the docsindex runtime adds an entry to the pending-trust
queue (`~/.datawatch/docs-trust-pending.json`); operator accepts via:

```sh
datawatch docs trust accept skill:test-first
# or via REST: POST /api/docs/trust {source:"skill:test-first", granted_by:"operator"}
# or via PWA: Settings → General → Docs Search → pending list → Trust
```

Once trusted, the source's docs land in the BM25 (and vector, if Ollama
is configured) index and surface through `docs_search`/`list_howtos`.

## Provenance

Every plan response includes a `provenance` field per step:

- **`authored`** — the howto carries hand-written `exec_steps:` front-matter.
- **`llm_translated`** — the howto has no front-matter; the configured
  LLM (Ollama default, OpenWebUI fallback) translated the prose into a
  step sequence at apply time. LLM-translated plans force `risk_gate=true`.

22 of 24 howtos under `docs/howto/` are curated `authored`. The remaining
two (`channel-state-engine`, `README`) are LLM-only by design.

## Surfaces

| Surface | Search | Read | List howtos | Apply |
|---|---|---|---|---|
| REST | `GET /api/docs/search` | `GET /api/docs/read` | `GET /api/docs/list-howtos` | `POST /api/docs/apply` |
| MCP | `docs_search` | `docs_read` | `docs_list_howtos` | `docs_apply` |
| CLI | `datawatch docs search` | `datawatch docs read` | `datawatch docs list-howtos` | `datawatch docs apply` |
| Comm | `docs search …` | `docs read …` | `docs howtos` | `docs apply …` |
| PWA | Settings → General → Docs Search card | inline modal | tab below search | dialog with token + risk-gate toggle |

## Hard constraints

- **No GPU required.** Vector layer falls back to BM25 when Ollama is
  unreachable; LLM-translation falls back to "no exec_steps" when no
  LLM backend is configured.
- **All trust opt-in.** Core docs are trusted by default; skills and
  plugins must be explicitly trusted before their docs index.
- **CI lints block drift.** Every release runs `check-curated-howtos.sh`,
  `check-howto-coverage.sh`, `check-plugin-manifests.sh` to catch the
  three classes of silent breakage caught by the docs audit.

## Linked references

- See also: [`secrets-manager.md`](secrets-manager.md) — example of a curated authored howto.
- See also: [`skills-sync.md`](skills-sync.md) — how skills + their SKILL.md docs land in the index.
