// GH#192 Phase 1 — data-only / existing-API items (#8 agent badge, #1
// watched-only alert badge, #3 Observer server info). See
// docs/plans/historical-plans/2026-10-07-gh192-parity-batch.md for the full batch plan.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS(byId) {
  const sandbox = buildSandbox({ byId });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  return sandbox;
}

function setState(sandbox, patch) {
  sandbox.__patch = patch;
  vm.runInContext('Object.assign(state, __patch)', sandbox);
  delete sandbox.__patch;
}

function callWithArg(sandbox, fnName, arg) {
  sandbox.__arg = arg;
  const result = vm.runInContext(`${fnName}(__arg)`, sandbox);
  delete sandbox.__arg;
  return result;
}

// ── #8 — agent badge shows the agent id directly ───────────────────────────

test('sessionCard shows the agent id directly, not just "worker", in the badge text', () => {
  const sandbox = loadAppJS();
  const html = callWithArg(sandbox, 'sessionCard', { id: 's1', full_id: 's1', agent_id: 'worker-7f3a', state: 'running' });
  assert.ok(html.includes('⬡ worker-7f3a'), 'expected the agent id to appear directly in the badge, not just in the tooltip');
  assert.ok(!html.includes('>⬡ worker<'), 'the old generic "worker" text should be gone');
});

test('sessionCard renders no agent badge at all when agent_id is absent', () => {
  const sandbox = loadAppJS();
  const html = callWithArg(sandbox, 'sessionCard', { id: 's1', full_id: 's1', state: 'running' });
  assert.ok(!html.includes('agent-badge'));
});

// ── #1 — watched-only alert badge ───────────────────────────────────────────

function loadAppJSWithBadge() {
  const badge = makeBadgeStub();
  const sandbox = loadAppJS({ alertBadge: badge });
  return { sandbox, badge };
}

function makeBadgeStub() {
  let display = '';
  return {
    get textContent() { return this._text || ''; },
    set textContent(v) { this._text = v; },
    style: { get display() { return display; }, set display(v) { display = v; } },
  };
}

test('updateAlertBadge shows the flat total when the watch filter is off', () => {
  const { sandbox, badge } = loadAppJSWithBadge();
  setState(sandbox, { alertUnread: 5, alertWatchedUnread: 1, sessionWatchFilter: false });
  vm.runInContext('updateAlertBadge()', sandbox);
  assert.equal(badge.textContent, '5');
  assert.equal(badge.style.display, 'inline');
});

test('updateAlertBadge shows only the watched-session count when the watch filter is on', () => {
  const { sandbox, badge } = loadAppJSWithBadge();
  setState(sandbox, { alertUnread: 5, alertWatchedUnread: 1, sessionWatchFilter: true });
  vm.runInContext('updateAlertBadge()', sandbox);
  assert.equal(badge.textContent, '1');
});

test('updateAlertBadge hides the badge when the watched-only count is zero, even if the total is not', () => {
  const { sandbox, badge } = loadAppJSWithBadge();
  setState(sandbox, { alertUnread: 5, alertWatchedUnread: 0, sessionWatchFilter: true });
  vm.runInContext('updateAlertBadge()', sandbox);
  assert.equal(badge.style.display, 'none');
});

test('handleAlert increments alertWatchedUnread only for alerts from a watched session', () => {
  const { sandbox, badge } = loadAppJSWithBadge();
  sandbox.__ws = new Set(['sess-watched']);
  vm.runInContext('state.watchedSessions = __ws', sandbox);
  delete sandbox.__ws;

  callWithArg(sandbox, 'handleAlert', { id: 'a1', level: 'info', title: 't', body: 'b', session_id: 'sess-watched' });
  callWithArg(sandbox, 'handleAlert', { id: 'a2', level: 'info', title: 't', body: 'b', session_id: 'sess-unwatched' });
  callWithArg(sandbox, 'handleAlert', { id: 'a3', level: 'info', title: 't', body: 'b', source: 'system' }); // no session_id

  assert.equal(vm.runInContext('state.alertUnread', sandbox), 3);
  assert.equal(vm.runInContext('state.alertWatchedUnread', sandbox), 1, 'only the watched-session alert should count');
});

test('toggleSessionWatchFilter updates the badge immediately, without waiting for the next alert', () => {
  const { sandbox, badge } = loadAppJSWithBadge();
  setState(sandbox, { alertUnread: 5, alertWatchedUnread: 2, sessionWatchFilter: false });
  vm.runInContext('toggleSessionWatchFilter()', sandbox);
  assert.equal(vm.runInContext('state.sessionWatchFilter', sandbox), true);
  assert.equal(badge.textContent, '2', 'badge should already reflect the watched-only count after toggling');
});

// ── #3 — Observer server info (hostname + daemon version) ─────────────────

test('renderStatsData shows hostname in the Daemon card and daemon_version in the Infrastructure card when present', () => {
  const el = makeStubEl();
  const sandbox = loadAppJS();
  callStatsRender(sandbox, el, {
    timestamp: Date.now(), cpu_load_avg_1: 0.1, cpu_cores: 4, mem_used: 1, mem_total: 2,
    disk_used: 1, disk_total: 2, daemon_rss_bytes: 1, goroutines: 1, open_fds: 1, uptime_seconds: 1,
    hostname: 'johnnyjohnny', daemon_version: '8.70.0',
  });
  assert.ok(el.innerHTML.includes('johnnyjohnny'), 'expected hostname to appear');
  assert.ok(el.innerHTML.includes('8.70.0'), 'expected daemon version to appear');
});

test('renderStatsData omits the hostname/version rows entirely when absent (older daemon, field not yet sent)', () => {
  const el = makeStubEl();
  const sandbox = loadAppJS();
  callStatsRender(sandbox, el, {
    timestamp: Date.now(), cpu_load_avg_1: 0.1, cpu_cores: 4, mem_used: 1, mem_total: 2,
    disk_used: 1, disk_total: 2, daemon_rss_bytes: 1, goroutines: 1, open_fds: 1, uptime_seconds: 1,
  });
  assert.ok(!el.innerHTML.includes('Hostname'));
});

function makeStubEl() {
  const el = makeStubElement();
  el.closest = () => null;
  return el;
}

function callStatsRender(sandbox, el, data) {
  sandbox.scrollTo = () => {}; // renderStatsData restores page scroll position
  sandbox.__el = el;
  sandbox.__data = data;
  vm.runInContext('renderStatsData(__el, __data)', sandbox);
  delete sandbox.__el;
  delete sandbox.__data;
}
