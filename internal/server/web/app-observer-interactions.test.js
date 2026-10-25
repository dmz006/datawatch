// Observer interactions regression tests — per-system grid (#perSystemGrid,
// BL379) + local system panel (#statsPanel), the mobile-fit reproduction
// findings:
//   * F-timers   — re-renders (8s poller ticks / refresh buttons) used to
//                  fire the /api/stats sub-loaders three times per pass and
//                  stack refresh timers on top of each other.
//   * F-taprow   — tapping a session row must expand it, the expansion must
//                  survive a refresh re-render, and a second tap collapses.
//   * F-dupcards — the synthesized "self" peer (is_self:true) used to render
//                  as a SECOND grid card for the local host.
//
// These pin the post-fix contracts: sharedFetch coalesces only IN-FLIGHT
// duplicate /api/stats requests (exactly one per pass, exactly one new
// request on the next pass — no stack, no stale cache), no loader adds a
// new setInterval on re-render, the grid renders one card per physical
// system, and the tap-row expansion state lives in _expandedSessions so a
// re-render rebuilds the panel with the row still open.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, makeStubElement, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

// The Observer stats panel renders through the shared list loader: the loader
// owns #statsPanel and the stats land in its #statsBody child. This stub gives
// the panel that child, and reads of innerHTML see both, so assertions about
// what the operator sees keep working.
function makeStatsPanel() {
  const body = makeStubElement();
  const panel = makeStubElement();
  return new Proxy(panel, {
    get(target, prop) {
      if (prop === 'innerHTML') return target.innerHTML + body.innerHTML;
      if (prop === 'querySelector') return (sel) => (sel === '#statsBody' ? body : null);
      const v = Reflect.get(target, prop, target);
      return typeof v === 'function' ? v.bind(target) : v;
    },
    set(target, prop, value) { target[prop] = value; return true; },
  });
}

const APP_JS = path.join(__dirname, 'app.js');

const LONG_HOST = 'host-abcdefghij-klmnopqrst-uvwxyz-long-0123456789';
const PEER_OK = 'peer-very-long-hostname-abcdefghijklmnopqrstuvwxyz-01234567';
const PEER_ERROR = 'peer-downbox';
const PEER_SELF_MIRROR = 'mirror-of-self-host';

// One /api/stats body that serves both the local grid card
// (loadSystemStatsGrid reads timestamp/hostname/cpu_load_avg_*/mem_*)
// and the local panel (renderStatsData reads uptime_seconds/session_stats).
const STATS_BODY = {
  timestamp: Math.floor(Date.now() / 1000),
  hostname: LONG_HOST,
  cpu_cores: 8,
  cpu_load_avg_1: 2.2, cpu_load_avg_5: 2.0, cpu_load_avg_15: 1.9,
  mem_used: 4294967296, mem_total: 17179869184,
  gpu_name: 'NVIDIA T4', gpu_util_pct: 42, gpu_temp: 61,
  gpu_mem_used_mb: 2000, gpu_mem_total_mb: 16000,
  uptime_seconds: 7200,
  total_sessions: 1,
  daemon_rss_bytes: 83886080,
  session_stats: [
    {
      session_id: 'sid-1', name: 'alpha', backend: 'anthropic', state: 'active',
      rss_bytes: 20971520, uptime: '10m', pane_pid: 4242, cpu_percent: 5.5,
    },
  ],
};

const PEERS_BODY = {
  peers: [
    { name: PEER_OK, shape: 'flat', last_push_at: new Date().toISOString(), is_self: false },
    { name: PEER_ERROR, shape: 'flat', last_push_at: new Date().toISOString(), is_self: false },
    { name: PEER_SELF_MIRROR, shape: 'flat', last_push_at: new Date().toISOString(), is_self: true },
  ],
};

const PEER_OK_STATS = {
  cpu: { pct: 55, load1: 3.1, load5: 2.8, load15: 2.5 },
  mem: { used_bytes: 12884901888, total_bytes: 34359738368 },
  gpu: [
    { name: '0', util_pct: 70, temp_c: 65, power_w: 60.5,
      mem_used_bytes: 8589934592, mem_total_bytes: 25769803776 },
  ],
};

function fakeResponse(status, body) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: () => Promise.resolve(body),
    text: () => Promise.resolve(typeof body === 'string' ? body : JSON.stringify(body)),
  };
}

// makeEnv loads the real app.js into a vm sandbox. env.hang() puts every
// /api/stats response on a deferred promise (resolvable via env.resume()) so
// a test can fire all three sub-loaders in the same synchronous pass and
// count how many distinct network requests that pass produced.
function makeEnv() {
  const grid = makeStubElement();
  const panel = makeStatsPanel();
  const peerList = makeStubElement();
  const sandbox = buildSandbox();
  let calls = 0;
  let hanging = false;
  let pending = [];
  const env = {
    grid, panel, peerList, sandbox,
    intervals: [],      // ms of every setInterval scheduled
    intervalCount: 0,
    statsCalls: () => calls,
    hang: () => { hanging = true; },
    resume: () => { hanging = false; pending.splice(0).forEach(f => f(fakeResponse(200, STATS_BODY))); pending = []; },
  };

  sandbox.setInterval = (fn, ms) => { env.intervals.push(ms); return ++env.intervalCount; };

  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);

  sandbox.document.getElementById = (id) => {
    if (id === 'perSystemGrid') return grid;
    if (id === 'statsPanel') return panel;
    if (id === 'peerResourceList') return peerList;
    return makeStubElement();
  };

  sandbox.fetch = (url) => {
    const u = String(url);
    if (u === '/api/stats') {
      calls += 1;
      if (hanging) {
        return new Promise((resolve) => { pending.push(resolve); });
      }
      return Promise.resolve(fakeResponse(200, STATS_BODY));
    }
    if (u === '/api/observer/peers') return Promise.resolve(fakeResponse(200, PEERS_BODY));
    if (u.startsWith('/api/observer/peers/')) {
      if (u.includes(encodeURIComponent(PEER_OK))) return Promise.resolve(fakeResponse(200, PEER_OK_STATS));
      return Promise.reject(new Error('connection refused')); // PEER_ERROR stays a card
    }
    return Promise.resolve(fakeResponse(200, {}));
  };

  return env;
}

// The three /api/stats sub-loaders renderObserverView fires back-to-back.
const RENDER_PASS = 'loadSystemStatsGrid(); loadStatsPanel(); loadPeerResourceOverview();';

function count(html, re) {
  const m = html.match(re);
  return m ? m.length : 0;
}

test('sharedFetch exists and is exposed on window', () => {
  const env = makeEnv();
  assert.equal(typeof env.sandbox.sharedFetch, 'function',
    'sharedFetch must be a top-level function (coalescers call it)');
  assert.equal(typeof env.sandbox.window.sharedFetch, 'function',
    'window.sharedFetch must exist so tests and other scripts can find it');
});

test('one in-flight /api/stats per render pass: three sub-loaders issue ONE request, not three', async () => {
  const env = makeEnv();
  env.hang();
  vm.runInContext(RENDER_PASS, env.sandbox);
  assert.equal(env.statsCalls(), 1,
    'loadSystemStatsGrid + loadStatsPanel must share the in-flight /api/stats; got ' + env.statsCalls());
  env.resume();
  await flushAsync();
  // The first pass settled: both render targets got real content.
  assert.match(env.grid.innerHTML, /obs-sys-card/, 'grid should have rendered system cards');
  assert.match(env.panel.innerHTML, /sid-1/, 'stats panel should have rendered the session list');
  // The settle cleared the shared entry, so the NEXT render pass (8s poller
  // tick or refresh button) issues exactly ONE fresh request — not a stack
  // of three, and not zero (no stale-cache bypass).
  const before = env.statsCalls();
  vm.runInContext(RENDER_PASS, env.sandbox);
  assert.equal(env.statsCalls(), before + 1,
    'a settled re-render must issue exactly one new /api/stats request, not a stack');
  await flushAsync();
  assert.match(env.panel.innerHTML, /class="stats-taprow"/, 're-render kept the panel');
  // No leaked in-flight entries.
  const leaked = vm.runInContext('Object.keys(_sharedFetchInflight).length', env.sandbox);
  assert.equal(leaked, 0, 'sharedFetch registry must be empty after settle');
});

test('sub-loader re-renders do not stack refresh timers', async () => {
  const env = makeEnv();
  vm.runInContext(RENDER_PASS, env.sandbox);
  await flushAsync();
  const afterFirst = env.intervals.length;
  for (let i = 0; i < 3; i += 1) {
    vm.runInContext(RENDER_PASS, env.sandbox);
    await flushAsync();
  }
  assert.equal(env.intervals.length, afterFirst,
    're-renders must not schedule new setIntervals (got +' + (env.intervals.length - afterFirst) + ')');
});

test('grid renders one card per physical system: local + 2 peers, the is_self mirror is filtered out', async () => {
  const env = makeEnv();
  vm.runInContext('loadSystemStatsGrid()', env.sandbox);
  await flushAsync();
  const cards = count(env.grid.innerHTML, /class="obs-sys-card"/g);
  assert.equal(cards, 3,
    'local + ' + PEER_OK + ' + ' + PEER_ERROR + ' = 3 cards; got ' + cards + ': ' + env.grid.innerHTML.slice(0, 400));
  assert.match(env.grid.innerHTML, new RegExp(PEER_OK), 'working peer card renders its name');
  assert.match(env.grid.innerHTML, new RegExp(PEER_ERROR), 'errored peer still gets a grey card');
  assert.doesNotMatch(env.grid.innerHTML, new RegExp(PEER_SELF_MIRROR),
    'the synthesized is_self:true self-mirror must not duplicate the local card');
});

test('tap-row: expand shows detail, survives a refresh re-render, second tap collapses', async () => {
  const env = makeEnv();
  vm.runInContext('loadStatsPanel()', env.sandbox);
  await flushAsync();
  const collapsed = env.panel.innerHTML;
  assert.match(collapsed, /class="stats-taprow"/, 'session row must be a stats-taprow');
  assert.match(collapsed, /sid-1/);
  assert.doesNotMatch(collapsed, /Backend: anthropic/, 'row starts collapsed');

  // Exactly what the tap-row onclick does: toggle _expandedSessions, re-render.
  vm.runInContext("_expandedSessions.add('sid-1'); loadStatsPanel();", env.sandbox);
  await flushAsync();
  assert.match(env.panel.innerHTML, /Backend: anthropic/, 'expanded row shows the backend detail');
  assert.match(env.panel.innerHTML, /PID: 4242/, 'expanded row shows the pane PID');

  // Refresh re-render: the panel rebuilds AND the expanded state survives
  // (state lives in the module-level Set, not in the DOM).
  vm.runInContext('loadStatsPanel();', env.sandbox);
  await flushAsync();
  assert.match(env.panel.innerHTML, /sid-1/, 'refresh re-render rebuilt the row');
  assert.match(env.panel.innerHTML, /Backend: anthropic/, 'refresh re-render preserved the expansion');

  // Second tap collapses.
  vm.runInContext("_expandedSessions.delete('sid-1'); loadStatsPanel();", env.sandbox);
  await flushAsync();
  assert.match(env.panel.innerHTML, /sid-1/, 'row still present after collapse');
  assert.doesNotMatch(env.panel.innerHTML, /Backend: anthropic/, 'second tap collapsed the detail');
});
