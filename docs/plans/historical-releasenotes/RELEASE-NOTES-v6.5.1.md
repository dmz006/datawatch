# Release Notes — v6.5.1 (BL247–BL250 + BL253 + BL246 partial)

Released: 2026-05-04
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.5.1
Smoke: 91/0/6

## Summary

Patch release closing 6 open bugs in one cut: **BL248** (rate-limit state override), **BL249** (session reconnect stale view), **BL250** (popup dismiss stale view), **BL247** (Settings tab consolidation — 4 tabs removed), **BL246** (Automata UX first pass — items 2/3/4/7 partial), **BL253** (eBPF setup false-positive, GH#37).

## Fixed

- **BL248** (`internal/session/manager.go`) — `tryTransitionToWaiting()` no longer overrides `StateRateLimited`. A debounced prompt detection ~3 s after rate-limit detection could flip the session state back to `waiting_input`, masking the rate-limit from the operator.
- **BL249** (`internal/server/web/app.js`) — WS reconnect handler now fetches `GET /api/sessions` and calls `updateSession()` for every session. Session detail view reflects live state immediately after a daemon restart without requiring the operator to exit and re-enter.
- **BL250** (`internal/server/web/app.js`) — `dismissNeedsInputBanner()` fetches `GET /api/sessions` after dismiss so banners and buttons update immediately rather than waiting for the next WS event.
- **BL247** (`internal/server/web/app.js`) — Settings tab bar reduced from 11 to 7 tabs by removing standalone `routing`, `orchestrator`, `secrets`, `tailscale` tabs and promoting their content to cards inside Comms, Automata, and General tabs respectively. Pipelines, Autonomous PRD Decomposition, and PRD-DAG Orchestrator cards moved from General to Automata tab. Plugin Framework config card moved to Plugins tab. Stale localStorage values for removed tabs auto-migrated on load.
- **BL246 partial** (`internal/server/web/app.js`, `style.css`, locale bundles) — Automata UX first pass: FAB (⚡) now shown on the Automata list and opens the launch wizard; stale "how-to guide coming in v6.2.0-dev" toast replaced with a link to the shipped howto doc; overflow ⋯ menu anchored right on narrow viewports via CSS media query; workspace label clarified; `automata_wizard_skills` updated across all 5 locale bundles ("coming soon" → "available"). Items 1/5/6 deferred (eventually closed v6.6.0).
- **BL253** (`internal/stats/ebpf.go`, GH#37) — three bugs in `CheckEBPFReady()` fixed: kernel version now parsed from `/proc/version` and enforced ≥ 5.8; `SetCapBPF` adds `cap_sys_resource` so `rlimit.RemoveMemlock()` succeeds at daemon start; `CheckEBPFReady` probes `RemoveMemlock()` and warns if `unprivileged_bpf_disabled` is non-zero.

## See also

CHANGELOG.md `[6.5.1]` entry.
