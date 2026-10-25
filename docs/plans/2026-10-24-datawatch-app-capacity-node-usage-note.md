# datawatch-app — capacity card compute-node usage labels — i18n parity note

**Date:** 2026-10-24
**Repo:** `dmz006/datawatch-app` (Android + iPhone/iOS)
**Daemon counterpart (this repo):** the capacity response's per-node `usage` payload
(`internal/server/node_usage.go`), the compute-node rows of the PWA capacity card
(`internal/server/web/app.js`), and the twelve new locale keys listed below.
**Status:** no GitHub issue has been filed. Per the Localization Rule a
datawatch-app issue *is* needed for this key set; it is deliberately NOT filed by
the task that wrote this note (operator decision for this task) and must be filed
before the app ports the node-usage rows. The key-set request is fully specified
below so the issue text can be lifted straight from this note.

## Why this note

The capacity card gained live compute-node usage: each `node:<name>` pool in the
capacity response now carries a `usage` object — the node's loaded models
(name, size in memory, expiry) plus per-GPU utilisation and memory from the node
observer — refreshed on a short TTL (figures are refreshed about every 15 seconds).
The PWA renders one row per node with the model line, the GPU line, and
state chips ("unreachable", "checking", "no GPU data"), and a legend explaining
where the figures come from.

This note is the Localization Rule step for the app repo: the new strings ship
in all five locale bundles here (EN values are the source of truth for the key
names), and the app needs the same keys so the Android and iPhone ports can draw
these rows without English-only UI.

## New keys (English values are the canonical strings)

Context: the **capacity card**, specifically the **compute node rows** (one row per
compute node, each showing the node's loaded model line and GPU line, with
state fallbacks).

| Key | English value | Rendered as |
|---|---|---|
| `capacity_node_model` | `Model` | label of the node's model line |
| `capacity_node_no_model` | `No model loaded` | model line when the node reports no resident model |
| `capacity_node_in_memory` | `{size} in memory` | model size (human-readable `{size}`) for a resident model |
| `capacity_node_expires` | `expires in {time}` | model eviction countdown (`{time}` human-readable) |
| `capacity_node_expires_now` | `expiring now` | model eviction due immediately |
| `capacity_node_never_expires` | `stays loaded` | model with no eviction (resident) |
| `capacity_node_gpu_util` | `GPU {pct}%` | GPU utilisation line (`{pct}` integer percent) |
| `capacity_node_gpu_mem` | `GPU memory {used} of {total}` | GPU memory line (human-readable `{used}` / `{total}`) |
| `capacity_node_unreachable` | `Unreachable` | state chip when the node's model server did not answer |
| `capacity_node_checking` | `Checking...` | state chip while the first fetch is in flight |
| `capacity_node_no_gpu_data` | `No GPU data from the observer` | fallback when the observer has no GPU figures for the node |
| `capacity_node_usage_hint` | `Live figures from the node's model server and the observer, refreshed about every 15 seconds.` | legend under the node rows |

Placeholder contract (identical in every locale): `{size}`, `{time}`, `{pct}`,
`{used}`, `{total}` — substitute the pre-formatted human-readable value from the
`usage` payload fields (`size_bytes`, `expires_at`, `util_pct`,
`mem_used_bytes`, `mem_total_bytes`).

## Parent release

These keys were introduced in the daemon's **next patch above main** (the same
build that ships the per-node `usage` payload and the capacity-card node rows).
No release number is given here by design; the release notes for that build are
the linking point for the datawatch-app issue.

## Request to datawatch-app

- Add the twelve keys above to the Compose Multiplatform string resources (same
  names, same placeholder names) so Android and iPhone/iOS share one pipeline.
- **Ask for DE, ES, FR and JA review** of the translations through the normal
  Compose-Multiplatform pipeline. The daemon repo already ships reviewed
  DE/ES/FR/JA values in `internal/server/web/locales/{de,es,fr,ja}.json`; treat
  those as the review baseline, not as machine output to copy blindly.
- When the app renders the node-usage rows, drive the state chip off the
  `usage.state` value (`ok`, `pending`, `unreachable`, `no_data`) and the
  `models_supported` flag, the same way the PWA does.

## Parity surface

| Surface | Included / excluded | What it does (or why it is out) |
|---|---|---|
| REST | Included | the capacity response body carries the figures: each `node:<name>` pool gains its `usage` field (models, GPU, state, checked time). No new endpoint. |
| MCP | Included | the MCP capacity tool passes the same response body through, so the figures reach MCP clients unchanged; no new tool and no new parameter. |
| CLI | Included | the CLI capacity view prints a text line per node (model line, GPU line, state); the wording is plain text, not locale-keyed here. |
| comm channel | Included | the comm channel prints the same one-line text summary per node as the CLI; no structured payload change. |
| YAML/config | Excluded | **no YAML/config key is added.** Node usage is derived data (server-side probes + observer figures); an operator's existing compute-node declaration is unchanged and nothing new needs configuring. |
| PWA | Included | the capacity card draws the node rows with the twelve keys above from the locale bundles; already wired through `t()` in `app.js`. |
| Android | Pending — excluded this task | the app does not have the node-usage UI yet; it is the work item for the datawatch-app issue this note describes. **That issue is NOT filed by the task that wrote this note**; file it, then port the rows using these keys. |
| iPhone/iOS | Pending — excluded this task | same as Android: covered by the same datawatch-app issue, not yet filed. Shares the Compose Multiplatform resources and the same parity scope. |

## Daemon-side guard

`internal/server/v5280_locales_test.go::TestLocales_CommonNavKeysPresent` now
guards the high-visibility keys `capacity_node_model`,
`capacity_node_unreachable` and `capacity_node_gpu_util` in the `mustHave`
slice, alongside full key-parity checks across all five bundles.
