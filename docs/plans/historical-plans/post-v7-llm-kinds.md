# Post-v7 — additional LLM-API Kind support

**Status:** roadmap (not in v7.x).
**Owner:** TBD.
**Created:** 2026-05-09 (alpha.23 design conversation, operator Q1).

## Context

`ComputeNode.Kind` in v7.0.0-alpha.23+ is the LLM-API protocol the daemon speaks at the Node's `address`. The current supported set (selectable in the PWA Add/Edit form) is:

| Kind | API | Adapter |
|---|---|---|
| `ollama` | Ollama native HTTP | `internal/inference/adapters/ollama.go` |
| `openai-compat` | OpenAI-compatible `/v1/chat/completions` | `internal/inference/adapters/openwebui.go` (covers OpenWebUI, vLLM, LMStudio, llama.cpp server, OpenAI itself) |

The operator decided 2026-05-09 (Q1) that the dropdown should expose **only** what the daemon directly supports. Roadmap entries below stay hidden from the form until the corresponding adapter ships.

## Roadmap candidates

Each entry needs: an adapter implementation in `internal/inference/adapters/`, a `Kind` constant in `internal/compute/node.go`'s `SupportedKinds`, plus PWA Add-form support.

| Kind candidate | API surface | Notes |
|---|---|---|
| `claude-api` | Anthropic Messages API (`https://api.anthropic.com/v1/messages`) | Auth via `${secret:anthropic-key}`. Different request shape than OpenAI-compat — needs its own adapter. |
| `gemini-api` | Google Generative AI (`https://generativelanguage.googleapis.com`) | Distinct request shape; needs its own adapter. |
| `opencode-api` | (TBD — operator-flagged 2026-05-09) | Need spec / endpoint contract. |
| `tabbyml` | TabbyML HTTP API | Local code-completion server; different prompt model than chat. |
| `mlx-server` | MLX server (Apple silicon) | OpenAI-compat dialect but worth distinguishing for hardware-routing decisions. |
| `cortex` | Cortex.cpp HTTP | OpenAI-compat dialect. |
| `llamafile` | Mozilla llamafile | OpenAI-compat dialect. |

## Migration plan when adding a Kind

1. New adapter in `internal/inference/adapters/<kind>.go` implementing `Adapter` interface.
2. New `Kind<NewKind>` constant in `internal/compute/node.go`; append to `SupportedKinds`.
3. Register adapter in `cmd/datawatch/main.go` `disp.RegisterAdapter(&adapters.<NewKind>{})`.
4. PWA `loadComputeNodesPanel`: append to the Add-form `<select id="computeNewKind">`.
5. PWA Kind-aware Hardware section auto-detect: extend the SaaS-pattern regex for any new SaaS endpoints (api.gemini.google.com, etc.).
6. Smoke section: add a per-Kind probe at `/api/compute/nodes` create flow.
7. Locale × 5 hint update.
8. datawatch-app issue under epic #94.

## Why these are deferred

- Each adapter takes substantive testing (auth flows, retry semantics, streaming differences).
- OpenAI-compat covers most SaaS today (Together, Groq, Mistral, Anyscale, OpenRouter — all OpenAI-compat); narrow per-provider adapters are pure ergonomics until protocol divergence forces them.
- Operator preference (Q1): "only expose what we have direct support for now, stub for roadmap especially gemini and opencode api and other oss api."

## Related

- `docs/plans/post-v7-routing.md` — separate dimension (HOW we get to a Node, vs WHAT it speaks).
