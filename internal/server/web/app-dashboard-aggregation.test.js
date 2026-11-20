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
const { pickerRows } = require('./testutil_fake_dom');

const APP_JS = path.join(__dirname, 'app.js');

function loadAppJS() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  for (const fn of ['_dashFetchPRDs', '_dashFetchCost', 'openServerPickerModal', '_initServerDropdown']) {
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

test('_dashFetchPRDs hits the status-filtered summary list and unwraps {prds:[...]} when not in all-servers mode', async () => {
  const sandbox = loadAppJS();
  mockFetchJSON(sandbox, {
    '/api/autonomous/prds?status=running,blocked,planning&limit=500': { prds: [{ id: 'p1', status: 'running' }, { id: 'p2', status: 'completed' }] },
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

test('picker modal includes the "All" row', () => {
  const sandbox = loadAppJS();
  setState(sandbox, { servers: { servers: [{ name: 'pi-node', enabled: true }] } });
  const { rowFor } = pickerRows(sandbox);
  assert.ok(rowFor('all'), 'expected an "All servers" row');
  assert.ok(rowFor('pi-node'), 'and the configured peer row');
});

// Operator-reported (2026-10-07): a real federated peer named "Apple
// Testing Sandbox" broke every chip's onclick on click -- the name was
// inline-built into each chip's onclick="..." and collided with the
// attribute's own quote delimiter, truncating it and leaving an
// incomplete `selectServer(` as the actual handler. That broke every
// named chip unconditionally (not just names with unusual characters --
// ANY quoted value collides), including the "All" chip itself.
//
// The picker modal keeps the name in an escHtml-escaped data attribute and
// JSON-encodes it inside an escaped onclick, so the handler can never be
// truncated. The regression this pins: the full name must remain intact in
// the row's data attribute, and the row's handler must decode back to a
// complete selectServer(<json name>) call followed by the one close call.
test('a server name does not break the picker row (regression)', () => {
  const sandbox = loadAppJS();
  const name = 'Apple Testing Sandbox "x" & \'y\'';
  setState(sandbox, { servers: { servers: [{ name, enabled: true }] } });
  const { rowFor, html } = pickerRows(sandbox);
  const row = rowFor(name);
  assert.ok(row, 'expected the full, intact name in the row data attribute');
  assert.equal(row.getAttribute('onclick'), `selectServer(${JSON.stringify(name)});closeServerPickerModal();`,
    'the handler decodes to a complete, correctly quoted call');
  // The raw attribute never holds an unescaped quote from the name.
  assert.ok(!html.includes('data-server-name="Apple Testing Sandbox "'), 'the name is escaped in the attribute');
});
