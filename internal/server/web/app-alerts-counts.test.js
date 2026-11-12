// e73d0004 — regression test for the pure alerts-tab counting helpers
// in app.js: _alertCategory (one categorization used by the heading,
// the category chips and the row badges) and _alertsCounts (the shared
// per-tab alert/session counting). The disagreement being fixed: tab
// buttons counted one thing (sessions) while the "N alerts" heading and
// the category chips counted alerts, with each call site computing its
// total a different way. Now heading and "all" chip both read
// counts.all from _alertsCounts, and the session count lives in
// counts.sessions for the tab buttons.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  assert.equal(typeof sandbox._alertCategory, 'function', '_alertCategory is not defined — did it get renamed or moved inside the view closure?');
  assert.equal(typeof sandbox._alertsCounts, 'function', '_alertsCounts is not defined — did it get renamed or moved inside the view closure?');
  return sandbox;
}

function counts(sandbox, entries, system) {
  sandbox.__entries = entries;
  sandbox.__system = system;
  const result = vm.runInContext('_alertsCounts(__entries, __system)', sandbox);
  delete sandbox.__entries;
  delete sandbox.__system;
  return result;
}

function cat(sandbox, a, sessState) {
  sandbox.__a = a;
  sandbox.__s = sessState;
  const result = vm.runInContext('_alertCategory(__a, __s)', sandbox);
  delete sandbox.__a;
  delete sandbox.__s;
  return result;
}

test('_alertCategory: waiting_input session is a prompt regardless of level', () => {
  const s = loadAppJS();
  assert.equal(cat(s, { level: 'info', title: 'x' }, 'waiting_input'), 'prompt');
  assert.equal(cat(s, { level: 'error', title: 'x' }, 'waiting_input'), 'prompt');
});

test('_alertCategory: prompt-ish titles are prompts on any session state', () => {
  const s = loadAppJS();
  assert.equal(cat(s, { level: 'info', title: 'needs input' }, ''), 'prompt');
  assert.equal(cat(s, { level: 'warn', title: 'Agent is waiting for you' }, ''), 'prompt');
  assert.equal(cat(s, { level: 'info', title: '' }, ''), 'info'); // no prompt wording + level decides
});

test('_alertCategory: plain levels map to their category', () => {
  const s = loadAppJS();
  assert.equal(cat(s, { level: 'error', title: 'boom' }, 'running'), 'error');
  assert.equal(cat(s, { level: 'warn', title: 'slow' }, 'running'), 'warn');
  assert.equal(cat(s, { level: 'info', title: 'ok' }, 'running'), 'info');
});

test('_alertCategory: null alert does not throw', () => {
  const s = loadAppJS();
  assert.equal(cat(s, null, ''), 'info');
});

test('_alertsCounts: session alerts counted per category with sessions counted', () => {
  const s = loadAppJS();
  const entries = [
    { sessID: 'a', sessState: 'running', alerts: [
      { level: 'error', title: 'boom' },
      { level: 'warn', title: 'slow' },
      { level: 'info', title: 'ok' },
    ]},
    { sessID: 'b', sessState: 'waiting_input', alerts: [
      { level: 'info', title: 'idle' }, // prompt by state, not level
    ]},
  ];
  const c = counts(s, entries, []);
  assert.equal(c.all, 4);
  assert.equal(c.prompt, 1);
  assert.equal(c.error, 1);
  assert.equal(c.warn, 1);
  assert.equal(c.info, 1);
  assert.equal(c.sessions, 2);
});

test('_alertsCounts: system tab counts the system list, no sessions', () => {
  const s = loadAppJS();
  const c = counts(s, [], [
    { level: 'error', title: 'pipeline failed' },
    { level: 'warn', title: 'disk 90%' },
    { level: 'warn', title: 'eBPF dropped' },
  ]);
  assert.equal(c.all, 3);
  assert.equal(c.error, 1);
  assert.equal(c.warn, 2);
  assert.equal(c.sessions, 0);
});

test('_alertsCounts: heading total always equals the "all" chip total', () => {
  const s = loadAppJS();
  const entries = [
    { sessID: 'a', sessState: 'running', alerts: [
      { level: 'error', title: 'boom' },
      { level: 'error', title: 'bang' },
      { level: 'info', title: 'ok' },
    ]},
  ];
  const c = counts(s, entries, []);
  assert.equal(c.prompt + c.error + c.warn + c.info, c.all,
    'category buckets must partition the tab total (heading == "all" chip)');
});

test('_alertsCounts: tolerant of null/undefined input', () => {
  const s = loadAppJS();
  const c = counts(s, null, undefined);
  assert.equal(c.all, 0);
  assert.equal(c.sessions, 0);
  const d = counts(s, [ { alerts: undefined }, null ], [null]);
  assert.ok(d.all >= 1); // the null system alert still counts as one alert
});

test('_alertsCounts: empty alert list per session still counts the session', () => {
  const s = loadAppJS();
  const c = counts(s, [{ sessID: 'a', sessState: 'running', alerts: [] }], []);
  assert.equal(c.all, 0);
  assert.equal(c.sessions, 1);
});

// ---------------------------------------------------------------------------
// Render-level consistency (e73d0006): the full _alertsBuildHtml output.
// The pure helpers above prove the counting math; these pin the painted
// markup: on every tab the heading number == the "all" chip number, and
// the per-category chips sum to it; tab buttons carry the SESSION counts
// (active tabs: sessions holding alerts; historical: ended sessions;
// system: the system alert count).
// Fixture: 8 alerts across exactly 6 ACTIVE sessions (one with several,
// one with each of the rest), 1 alert on a failed (historical) session,
// 1 system alert.
// ---------------------------------------------------------------------------

const EN = require('./locales/en.json');

const RSESSIONS = [
  { id: 'r-0', name: 'alpha', state: 'running' },
  { id: 'r-1', name: 'bravo', state: 'running' },
  { id: 'r-2', name: 'charlie', state: 'running' },
  { id: 'r-3', name: 'delta', state: 'running' },
  { id: 'r-4', name: 'echo', state: 'running' },
  { id: 'r-5', name: 'foxtrot', state: 'running' },
  { id: 'r-9', name: 'zulu', state: 'failed' },   // historical
];
const RALERTS = [
  // r-0: three alerts (error + warn + info)
  { id: 'a1', level: 'error', title: 'build failed', session_id: 'r-0', created_at: '2026-01-01T00:00:08Z' },
  { id: 'a2', level: 'warn', title: 'slow step', session_id: 'r-0', created_at: '2026-01-01T00:00:07Z' },
  { id: 'a3', level: 'info', title: 'checkpoint ok', session_id: 'r-0', created_at: '2026-01-01T00:00:06Z' },
  // r-1..r-5: one each → 6 active sessions, 8 alerts total
  { id: 'a4', level: 'error', title: 'crash', session_id: 'r-1', created_at: '2026-01-01T00:00:05Z' },
  { id: 'a5', level: 'warn', title: 'retry storm', session_id: 'r-2', created_at: '2026-01-01T00:00:04Z' },
  { id: 'a6', level: 'error', title: 'timeout', session_id: 'r-3', created_at: '2026-01-01T00:00:03Z' },
  { id: 'a7', level: 'warn', title: 'memory high', session_id: 'r-4', created_at: '2026-01-01T00:00:02Z' },
  { id: 'a8', level: 'info', title: 'all green', session_id: 'r-5', created_at: '2026-01-01T00:00:01Z' },
  // historical: failed session
  { id: 'a9', level: 'error', title: 'late failure', session_id: 'r-9', created_at: '2025-12-31T23:59:00Z' },
  // system
  { id: 'a10', level: 'warn', title: 'pipeline degraded', source: 'system', created_at: '2026-01-01T00:00:00Z' },
];

function renderTab(tab) {
  const s = loadAppJS();
  s._i18n.bundle = EN;
  s.__data = RALERTS;
  s.__ses = RSESSIONS;
  vm.runInContext(`state.sessions = __ses; window._alertsTabSel = ${JSON.stringify(tab)};`, s);
  return vm.runInContext(`_alertsBuildHtml({ data: __data, cmds: [], hasMore: false, total: 10 })`, s);
}

// The five chip buttons in the top bar, left to right: all/prompt/error/
// warn/info. The topBar sits between the heading (🔔 …) and the search box.
function topBarChips(html) {
  const i = html.indexOf('🔔');
  const j = html.indexOf('alertsSearchInput');
  assert.ok(i >= 0 && j > i, 'topBar with heading + search box present');
  const region = html.slice(i, j);
  const chips = [...region.matchAll(/ ×(\d+)</g)].map((m) => Number(m[1]));
  assert.equal(chips.length, 5, 'five category chips: all + four categories');
  return chips;   // [all, prompt, error, warn, info]
}

function headingNumber(html) {
  // Heading is `🔔 ${t(headingKey, headingVars)}`. The three forms are
  // "N alerts in M active sessions" / "N alerts in M historical sessions" /
  // "N system alert(s)" — so the only token common to all is the count right
  // after the bell. (The system form has the word "system" between the number
  // and "alert", a plain \`🔔 N alert\` pattern can't match it.)
  const m = /🔔\s+(\d+)/.exec(html);
  assert.ok(m, 'the 🔔 heading with its alert total is painted');
  return Number(m[1]);
}

function tabCount(html, label) {
  const esc = label.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const m = new RegExp('<span class="alert-session-label">' + esc + '</span>\\s*\\((\\d+)\\)').exec(html);
  assert.ok(m, 'tab button "' + label + ' (N)" is painted');
  return Number(m[1]);
}

test('active tab: heading == "all" chip == sum of categories; tab counts the 6 active sessions', () => {
  const html = renderTab('active');
  const heading = headingNumber(html);
  const [all] = topBarChips(html);
  const chips = topBarChips(html);
  assert.equal(heading, 8, 'heading says 8 alerts — the 8 active-tab alerts');
  assert.equal(all, heading, 'the "all" chip matches the heading');
  assert.deepEqual(chips, [8, 0, 3, 3, 2], 'chips: all=8, prompt=0, error=3, warn=3, info=2');
  assert.equal(chips.slice(1).reduce((a, b) => a + b, 0), all,
    'prompt+error+warn+info sum to the "all" chip (== heading)');
  assert.equal(tabCount(html, EN.alerts_tab_active_sessions), 6, 'tab label counts the 6 active sessions');
});

test('historical tab: 1 alert in 1 session; heading == all chip == chip sum', () => {
  const html = renderTab('historical');
  const heading = headingNumber(html);
  const chips = topBarChips(html);
  assert.equal(heading, 1);
  assert.equal(chips[0], 1, 'all chip');
  assert.equal(chips.slice(1).reduce((a, b) => a + b, 0), heading, 'categories sum to the heading');
  assert.equal(chips[1], 0, 'no prompts');
  assert.equal(chips[2], 1, 'the "late failure" alert is an error');
  assert.equal(tabCount(html, EN.alerts_tab_historical_sessions), 1, 'tab label counts the 1 historical session');
});

test('system tab: 1 system alert; heading == all chip == chip sum', () => {
  const html = renderTab('system');
  const heading = headingNumber(html);
  const chips = topBarChips(html);
  assert.equal(heading, 1);
  assert.equal(chips[0], 1);
  assert.equal(chips.slice(1).reduce((a, b) => a + b, 0), heading);
  assert.equal(chips[3], 1, 'the "pipeline degraded" alert is a warn');
  assert.equal(tabCount(html, EN.alerts_tab_system_alerts), 1, 'tab label counts the 1 system alert');
});

test('active tab heading reads the localized one/many form with both numbers', () => {
  const html = renderTab('active');
  assert.ok(html.includes(EN.alerts_heading_active_many.replace('%1$d', '8').replace('%2$d', '6')),
    'heading = "8 alerts in 6 active sessions" (localized many-form with alert + session counts)');
});

