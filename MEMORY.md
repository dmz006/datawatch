# Memory (2026-11-24)

## Task
PWA test for the BL312 S3 header server dropdown: new
`internal/server/web/app-header-server-dropdown.test.js` (13 tests, all pass,
node --test, no daemon/tmux).

## How
- vm-sandbox harness as the existing tests: `buildSandbox({ byId: { serverDropdown: <select catcher>, view: <innerHTML catcher> } })`, `loadScript(app.js)`, then `sandbox._i18n.bundle = JSON.parse(locales/en.json)` so `t()` yields real labels ("All servers", "This server").
- wrap `sandbox.selectServer` with a call-recording wrapper; neutralise `connect`, `loadServers`, `loadServersList`, `_ensureLocalHostname`, `_checkFederatedConnection`, `_loadAllServersSessions`, `_probePickerReachability`, `showToast` (record toasts).
- drive `onServerDropdownChange(value)` (the single generic onchange) and assert one `selectServer` call: peer → name, `"all"` → "all", `""` → null (local).
- "choice remembered on next load (same browser)" = in-memory per page session: call `_initServerDropdown()` (the navigate() re-init path) and assert the same selection re-renders with no additional selectServer call.
- hostile-name option parsing: regex `/<option value="([^"]*)"/g` + attribute-unescape; names travel escHtml-escaped, so `say "hi" & back\slash` round-trips as one option.
- removal proof: assert neither `class="server-picker-bar"` nor `class="sessions-topbar-servers"` exists in app.js/index.html source, rendered `_serverPickerBar()` output, or `renderSessionsView()` output with a peer active.
- accessibility: single native `<select>` with `aria-label="Server"` and `onchange="onServerDropdownChange(this.value)"`; simulated keyboard gesture = set `.value` then call the handler with that value.

## Verification
`env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-header-server-dropdown.test.js`
→ 13/13 pass, exit 0 (ran twice, stable).
