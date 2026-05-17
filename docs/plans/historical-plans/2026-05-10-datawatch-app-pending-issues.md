# datawatch-app — pending mobile-parity issues (deferred until GATE done)

These get filed as ONE consolidated issue (or per-area sub-issues) when the GATE walkthrough completes. Today's batch covers the post-alpha.36 changes — file as **alpha.36-2** parity.

## Bundle 1 — Sessions list (already filed: datawatch-app#123 covers filter UX)

(no additional changes since #123 — confirmed working in GATE)

## Bundle 2 — Alert dock card layout
- Cards: bordered tile per alert with type-colored left rail + matching icon (✓/⚠/✕/ℹ)
- Type chip + time + ×N pill + per-card dismiss (✕)
- Long messages: clamp to 3 lines with `▸ more` chevron — tap message body or chevron to expand inline; per-card expansion state preserved across re-renders
- Auto-clear on success "Connected" (3s self-dismiss; also wipes any prior "Disconnected" entries before settling)
- Mobile equivalent: re-style AlertDock composable with the same card spec; add per-message expand/collapse; auto-dismiss reconnect success

## Bundle 3 — Sessions card layout (small adjustment)
- State badge moved to far right of action buttons with `|` separator before it (drag handle stays at the very right)
- Mobile: match same right-side state placement on session cards

## Bundle 4 — Session detail header
- New "📄 Response" pill button right of the existing "🕐 Timeline" button (opens last-response viewer; previously hidden in the send bar)
- LLM/v7 LLMRef/Compute badges restyled: border + tinted bg + bold; matches session-list badge look (operator: prior pale style was hard to read)
- Mobile: add same Response button to session detail toolbar; restyle backend/LLM/compute chips

## Bundle 5 — Session detail terminal toolbar
- Font controls (`Aa▾`) and Scroll button now right-justified against the tabs (operator: needed visual gap from tab bar)
- Mobile: ensure equivalent right-justification on the per-session toolbar

## Bundle 6 — Session detail send bar
- Removed Response button from send bar (moved to header — see Bundle 4)
- Removed alertPillSlot from send bar (header dock is single source)
- D-pad reordered: `[ESC ␛]` LEFT, `[↑ ↓ ← →]` middle, `[ENTER ⏎]` RIGHT
- Arrow buttons now press-and-hold repeat (operator scrolls a lot through long outputs); ESC + Enter stay one-shot
- Saved-cmds dropdown filters server-seeded duplicates (only user-saved show under "Saved" optgroup) — fixes the dropdown looking duplicated
- Mobile: same d-pad order + repeat-on-hold; same saved-cmds dedup

## Bundle 7 — Session detail Status panel
- Removed duplicate state badge from Status board body (header already shows session state)
- Hooks indicator (●) is now click-to-refetch with explanatory tooltip; "stale" gets a dotted-underline dual click target
- Hook stale threshold raised server-side: 30s → 5min (so long Thinking turns no longer false-fire stale)
- Status board badge eager-loads on session-detail mount — green dot appears in tab without clicking
- "no hooks installed" / "hooks stale" / empty focus card now include a clickable **Docs ↗** link that opens the in-PWA howto viewer
- Mobile: equivalent eager status fetch on session-detail mount; clickable docs links on empty states

## Bundle 8 — Disabled stale-comms running-dot in CSS
- The yellow comms-stale dot inside session state badges was inaccurate; CSS suppressed in alpha.36
- Mobile: disable equivalent indicator (if any) — hooks now drive near-prompt detection per Bundle 7
- Code-strip across both repos tracked in datawatch task #283 (POST v7.0)

## Bundle 9 — Locale × 5 — new keys
- `btn_response`, `btn_view_last_response`
- `status_hooks_alive_tip`, `status_hooks_stale_tip`, `status_hooks_missing_tip` (added inline as fallback strings, will move to bundles when stable)
- `common_docs`, `common_setup` (link labels)
- Stripped internal version refs from existing keys: `profile_ollama_models_ph`, `peer_group_by_node_tip`, `status_no_events_yet`, `status_hooks_missing`, `status_followup_note`
- Mobile: pull updated bundles + add common_docs/common_setup keys for matching link patterns

## Reference
- Server release: v7.0.0-alpha.36 (in-progress; will tag once GATE walkthrough completes)
- Source spec: GATE walkthrough findings 2026-05-10 (#248)
- Filed under epic datawatch-app#94

---

## Followup — broader docs-link pattern (file separately)

Pattern: any "feature not active / not configured" empty state surfaces a clickable **Set up ↗** or **Docs ↗** link to the relevant howto. Apply across Observer, Stats, Status hooks, Federation, Council, Skills, Automata Tools, etc. Tracked in datawatch backlog (filed as task in this session).
