# Splash dwell settings

Date: 2026-11-03
Version: 9.71.0 (from `var Version` in `internal/server/api.go`) — this change does **not** bump it; the CHANGELOG entry sits under the existing `[Unreleased]` heading and no version heading is added.
Parity issue: [parity-issues/splash-dwell-settings.md](parity-issues/splash-dwell-settings.md)
How-to: [../operations.md](../operations.md) → “Splash dwell settings”
Live check: not applicable for this doc task (the live row is open in `docs/testing-tracker.md`)

## Proposal note

The request cited [2026-10-20-tag-browsing-tile-design.md](2026-10-20-tag-browsing-tile-design.md) as the proposal. That is a different design; this plan therefore records the behaviour from the request, not from the cited plan.

## Parity surface

In scope (every surface reads and/or sets the two keys, and the PWA renders the dwell, the early dismiss and the version line):

| Surface | What it does |
|---|---|
| REST | `PUT /api/config` / `GET /api/config` set and read both keys, refusing out-of-range values (whole seconds 0–60) and naming the offending key. |
| MCP | `config_set` / `config_get` with the same validation. |
| CLI | `datawatch config set ui.splash_min_seconds …` and `datawatch config get …` in the same validation path. |
| Comm channel | `configure ui.splash_min_seconds=…` / `configure ui.splash_min_seconds_new_version=…` as in `docs/commands.md`. |
| YAML/config | Both keys persist in the config file, round-trip across a restart unchanged, and apply without a restart. |
| PWA | The splash screen keeps its dwell (defaults 6 s / 9 s, `0` = legacy timing), dismisses early on tap, click or key press once connected, and shows the previous-version line. |

Excluded: Android and iPhone. Their splash lives in the `datawatch-app` repo, not here; the mirror is owed there and tracked by [parity-issues/splash-dwell-settings.md](parity-issues/splash-dwell-settings.md).

## Scope

- `CHANGELOG.md` — the two-settings line under `[Unreleased]`.
- `docs/implementation.md` — `ui.splash_min_seconds` and `ui.splash_min_seconds_new_version` under **Splash dwell settings**.
- `docs/operations.md` → **Splash dwell settings (`ui.splash_min_seconds`, `ui.splash_min_seconds_new_version`)** — what the operator controls: when the splash is shown, how long it stays, the two settings, the early dismiss, `0` = legacy.
- `docs/commands.md` → **Splash dwell settings via `configure`** — the comm and CLI examples, the other surfaces, the read paths.
- `docs/testing-tracker.md` → **Splash dwell settings** — the tested rows (config, REST, comm, MCP, CLI, PWA dwell, PWA settings card) and the open live row.
- `docs/plans/2026-11-03-splash-dwell.md` — this file, and its index line in `docs/plans/README.md`.
- `docs/plans/parity-issues/splash-dwell-settings.md` — the datawatch-app mirror (both settings, dwell/early dismiss, version line, 0 = legacy).

Out of scope, deliberately: no changes to the daemon code, the PWA code, the two
config keys or their defaults (6 / 9), the 24-hour / first-visit / version-change
gating, the 700 ms fade, or any live smoke run. This task documents an already-shipped
behaviour change; the open live-browser row stays open in `docs/testing-tracker.md`,
and the Android/iPhone mirror is owed to the datawatch-app repo via the parity issue.

## Phases and status

| Phase | Contents | Status |
|---|---|---|
| 1 — Operator docs | CHANGELOG line; operations; implementation; commands | **Done** |
| 2 — Test tracker + plan + parity issue | testing-tracker section; this plan doc; `plans/README.md` index line; datawatch-app parity issue | **Done** |
| 3 — Final gate | the live-browser row in `docs/testing-tracker.md` (dwell, early tap, version line against a real daemon); the datawatch-app mirror | **Open** |

## Phase Completion Checklist

| # | Phase requirement | Status |
|---|---|---|
| 1 | **CHANGELOG.md** — line under `[Unreleased]`: the two settings (`ui.splash_min_seconds` default 6, `ui.splash_min_seconds_new_version` default 9, whole seconds 0–60), `0` = legacy timing, the early dismiss (tap/click/keypress once connected), and the new-version version line. No tracker ids. | Done |
| 2 | **docs/operations.md** — new **Splash dwell settings** section: when the splash is shown (first visit, new version, 24 hours), how long it stays (the two keys, defaults 6 / 9, whole seconds 0–60, `0` keeps the legacy timing, takes effect on the next show), the early dismiss, the version line, the surface table, the keys table, and the datawatch-app pointer. | Done |
| 3 | **docs/implementation.md** — **Splash dwell settings** section: `ui.splash_min_seconds` and `ui.splash_min_seconds_new_version` (defaults 6 / 9 seconds, a whole number, range 0–60, `0` = legacy), how it works (config keys, the PWA dwell path, the early-dismiss path, the version line), the surface table, and the tests named. | Done |
| 4 | **docs/commands.md** — **Splash dwell settings via `configure`** subsection between the interstitial-prompt subsection and `setup <service>`: `configure ui.splash_min_seconds=8`, `configure ui.splash_min_seconds_new_version=15`, `datawatch config set …`, `datawatch config get ui.splash_min_seconds`, the YAML and `PUT /api/config` forms, `config_set`, and the note that the change applies without a restart. | Done |
| 5 | **docs/testing-tracker.md** — **Splash dwell settings** rows: settings (defaults, 0 kept, 60 in, 61 and −1 out, YAML round-trip), REST (defaults, `PUT`, refuse naming the key), comm (`configure`), MCP (`config_set`), CLI (`config set`/`get`), the PWA dwell behaviour, the PWA settings card + the five locale bundles; every Tested **Yes** / Pass, Validated **No**; the live-browser row open. | Done |
| 6 | **docs/plans/** — this plan doc with Date, Version (`9.71.0`, stated as not bumped by this change), Parity issue, How-to, Live check, a Proposal note on the cited design, a `## Parity surface` section (REST, MCP, CLI, comm channel, YAML/config, PWA in scope; Android/iPhone excluded via the parity record), Scope and this checklist; one line in `docs/plans/README.md`; the parity issue with both settings, the dwell/early-dismiss behaviour, the version line, `0` = legacy, and the Android/iPhone mirror. | Done |
| 7 | No tracker ids in the operator-facing docs; every new doc file created (this one, the parity issue) is under `docs/plans/` or an existing doc file; nothing else staged. | Done |
