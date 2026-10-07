// BL317 — Dashboard "all servers" mode. The audit (2026-10-07) found
// Dashboard showed the server picker's "All" chip but had no backing
// aggregation (silently kept showing local-only data). Per operator
// decision the same day: build real aggregation for Dashboard
// (/api/cost/aggregated + the existing /api/autonomous/prds/aggregated),
// but NOT for Observer (too big a redesign, conflicts with its own
// pre-existing "observer peers" concept) -- Observer instead just drops
// the "All" chip from its picker. This covers both halves.
//
// Incidentally found and fixed in the same pass: _dash._costToday's
// consumers all read `c.total_cost_usd`, but /api/cost's real field is
// `total_usd` -- the Dashboard cost display has always rendered
// $0/hidden, in every mode, not just "all servers". _dashFetchCost's
// summing logic is what's tested here; the display-side field-name fix
// itself has no dedicated test (no render-logic change, just a typo fix
// reading the now-correct field already covered by these tests).

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  for (const fn of ['_dashFetchPRDs', '_dashFetchCost', '_serverPickerBar', '_injectServerPickerBar']) {
    assert.equal(vm.runInContext(`typeof ${fn}`, sandbox), 'function', `${fn} is not defined -- did it get renamed?`);
  }
  return sandbox;
}

function setState(sandbox, patch) {
  sandbox.__patch = patch;
  vm.runInContext('Object.assign(state, __patch)', sandbox);
  delete sandbox.__patch;
}

function mockFetchJSON(sandbox, byUrl) {
  sandbox.fetch = (url) => {
    const body = byUrl[url];
    if (body === undefined) return Promise.resolve({ ok: false, status: 404, text: () => Promise.resolve('not found') });
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) });
  };
}

test('_dashFetchPRDs hits the plain endpoint and unwraps {prds:[...]} when not in all-servers mode', async () => {
  const sandbox = loadAppJS();
  mockFetchJSON(sandbox, {
    '/api/autonomous/prds': { prds: [{ id: 'p1', status: 'running' }, { id: 'p2', status: 'completed' }] },
  });
  await vm.runInContext('_dashFetchPRDs()', sandbox);
  const prds = vm.runInContext('_dash._prds', sandbox);
  assert.deepEqual(prds.map(p => p.id), ['p1'], 'only the running/blocked/planning PRD should survive the filter');
});

test('_dashFetchPRDs hits the aggregated endpoint and accepts a bare array in all-servers mode', async () => {
  const sandbox = loadAppJS();
  setState(sandbox, { activeServer: 'all' });
  mockFetchJSON(sandbox, {
    '/api/autonomous/prds/aggregated': [
      { id: 'p1', status: 'running', server: 'local' },
      { id: 'p2', status: 'blocked', server: 'pi-node' },
      { id: 'p3', status: 'completed', server: 'pi-node' },
    ],
  });
  await vm.runInContext('_dashFetchPRDs()', sandbox);
  const prds = vm.runInContext('_dash._prds', sandbox);
  assert.deepEqual(prds.map(p => p.id).sort(), ['p1', 'p2']);
});

test('_dashFetchCost passes the single-server CostSummary through unchanged when not in all-servers mode', async () => {
  const sandbox = loadAppJS();
  mockFetchJSON(sandbox, { '/api/cost': { sessions: 3, total_tokens_in: 100, total_tokens_out: 50, total_usd: 1.23 } });
  await vm.runInContext('_dashFetchCost()', sandbox);
  const cost = vm.runInContext('_dash._costToday', sandbox);
  assert.equal(cost.total_usd, 1.23);
  assert.equal(cost.sessions, 3);
});

test('_dashFetchCost sums every server\'s CostSummary in all-servers mode', async () => {
  const sandbox = loadAppJS();
  setState(sandbox, { activeServer: 'all' });
  mockFetchJSON(sandbox, {
    '/api/cost/aggregated': [
      { server: 'local', sessions: 2, total_tokens_in: 10, total_tokens_out: 5, total_usd: 0.50 },
      { server: 'pi-node', sessions: 1, total_tokens_in: 20, total_tokens_out: 8, total_usd: 0.75 },
    ],
  });
  await vm.runInContext('_dashFetchCost()', sandbox);
  const cost = vm.runInContext('_dash._costToday', sandbox);
  assert.equal(cost.sessions, 3);
  assert.equal(cost.total_tokens_in, 30);
  assert.equal(cost.total_tokens_out, 13);
  assert.ok(Math.abs(cost.total_usd - 1.25) < 1e-9, `expected ~1.25, got ${cost.total_usd}`);
});

test('_dashFetchCost in all-servers mode tolerates a non-array response (e.g. a 403 body) without throwing', async () => {
  const sandbox = loadAppJS();
  setState(sandbox, { activeServer: 'all' });
  mockFetchJSON(sandbox, { '/api/cost/aggregated': { error: 'forbidden' } });
  await vm.runInContext('_dashFetchCost()', sandbox);
  const cost = vm.runInContext('_dash._costToday', sandbox);
  assert.equal(cost.total_usd, 0);
  assert.equal(cost.sessions, 0);
});

test('_serverPickerBar includes the "All" chip by default', () => {
  const sandbox = loadAppJS();
  setState(sandbox, { servers: { servers: [{ name: 'pi-node', enabled: true }] } });
  const html = vm.runInContext('_serverPickerBar()', sandbox);
  assert.ok(/selectServer\(&quot;all&quot;\)/.test(html), 'expected a selectServer("all") chip');
});

test('_serverPickerBar({hideAll:true}) omits the "All" chip (Observer)', () => {
  const sandbox = loadAppJS();
  setState(sandbox, { servers: { servers: [{ name: 'pi-node', enabled: true }] } });
  sandbox.__opts = { hideAll: true };
  const html = vm.runInContext('_serverPickerBar(__opts)', sandbox);
  delete sandbox.__opts;
  assert.ok(!html.includes('>' + 'All' + '<') && !/selectServer\(&quot;all&quot;\)/.test(html), 'expected no selectServer("all") chip when hideAll is set');
  assert.ok(html.includes('pi-node'), 'other chips should still render normally');
});

test('_serverPickerBar still returns empty string with hideAll when there are no configured servers at all', () => {
  const sandbox = loadAppJS();
  sandbox.__opts = { hideAll: true };
  const html = vm.runInContext('_serverPickerBar(__opts)', sandbox);
  delete sandbox.__opts;
  assert.equal(html, '');
});

// Operator-reported (2026-10-07): a real federated peer named "Apple
// Testing Sandbox" made every chip's onclick throw "Unexpected end of
// input" on click -- JSON.stringify(c.name) wraps the name in raw `"`
// characters that collided with the onclick attribute's own `"`
// delimiter, truncating the attribute value at the first embedded quote
// and leaving an incomplete `selectServer(` as the actual handler. This
// broke every named chip unconditionally (not just names with unusual
// characters -- ANY JSON.stringify-quoted value collides), including the
// "All" chip itself. Fixed by escHtml()-wrapping the whole onclick
// expression, same pattern loadServersList()'s testServerEntry button
// already used correctly.
test('a server name does not truncate its chip\'s onclick attribute (regression)', () => {
  const sandbox = loadAppJS();
  setState(sandbox, { servers: { servers: [{ name: 'Apple Testing Sandbox', enabled: true }] } });
  const html = vm.runInContext('_serverPickerBar()', sandbox);

  // The old bug produced a raw, unescaped `"` immediately after
  // `selectServer(` -- which prematurely closes the onclick="..."
  // attribute value one character in. Assert that specific collision is
  // gone: no bare `"` appears between `onclick="` and the matching chip's
  // closing `"`, only its escaped form.
  assert.ok(!/onclick="selectServer\("/.test(html), 'onclick attribute value must not contain a raw, unescaped double quote');
  assert.ok(/onclick="selectServer\(&quot;Apple Testing Sandbox&quot;\)"/.test(html), 'expected the escaped form of selectServer("Apple Testing Sandbox")');

  // Simulate what the browser's HTML attribute parser actually does for
  // EVERY chip: extract each onclick="..." value and decode entities,
  // confirming each is a complete, valid JS call -- not truncated
  // mid-string -- and that our server's specific chip is among them.
  const matches = [...html.matchAll(/onclick="([^"]*)"/g)];
  assert.ok(matches.length >= 2, 'expected at least the Local and Apple Testing Sandbox chips');
  const decode = s => s.replace(/&quot;/g, '"').replace(/&#39;/g, "'").replace(/&amp;/g, '&');
  const decoded = matches.map(m => decode(m[1]));
  assert.ok(decoded.includes('selectServer("Apple Testing Sandbox")'), `expected one chip to decode to the full call, got: ${decoded.join(' | ')}`);
});
