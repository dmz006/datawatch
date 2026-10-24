// Capacity and budget card — PWA side.
//
// window.capacityCardHtml(cap, opts) turns the decoded GET /api/capacity body
// into the card markup: every pool with its limit, used count, usage bar and
// holders; every active back-off flag; every budgeted LLM with a spend bar.
// An empty response renders nothing so the card hides itself.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

function render(cap, opts) {
  const sandbox = loadApp();
  sandbox._cap = cap;
  sandbox._opts = opts;
  return vm.runInContext('window.capacityCardHtml(_cap, _opts)', sandbox);
}

test('pools show limit, used count, a usage bar and holders by short id', () => {
  const html = render({
    pools: [
      { name: 'host', limit: 9, held: 2, external: 1, holder: '' },
      { name: 'node:gpu1', limit: 2, held: 2, external: 0, holder: 'abcdef1234567890,12345678ffff', queue_depth: 1 },
      { name: 'llm:claude-pro', limit: 4, held: 1, external: 0, effective_max_inflight: 4, inflight_source: 'explicit' },
    ],
    leases: [{ holder: 'feedbeef99999', pools: ['llm:claude-pro', 'host'] }],
  });
  assert.ok(html.includes('3 / 9'), 'host used/limit missing: ' + html);
  assert.ok(html.includes('2 / 2'), 'node used/limit missing');
  assert.ok(html.includes('width:33%'), 'host usage bar width missing');
  assert.ok(html.includes('width:100%'), 'full node bar missing');
  assert.ok(html.includes('Holders: abcdef12, 12345678'), 'node holders must be short ids: ' + html);
  assert.ok(!html.includes('abcdef1234567890'), 'full holder id must not leak');
  assert.ok(html.includes('Holders: feedbeef'), 'holders must fall back to leases naming the pool');
  assert.ok(html.includes('waiting for node gpu1'), 'queued node note missing');
});

test('a back-off flag with a reset time shows backend, the time and the reason', () => {
  const html = render({ backoff: [{ backend: 'claude-code', until: '2026-10-22T15:00:00Z', reason: 'weekly limit reached', reset_known: true }] });
  assert.ok(html.includes('Usage back-off'), 'back-off title missing');
  assert.ok(html.includes('claude-code'), 'backend missing');
  assert.ok(html.includes('weekly limit reached'), 'reason missing');
  assert.ok(html.includes('Resets:'), 'reset time label missing: ' + html);
  assert.ok(!html.includes('reset time unknown'), 'known reset must not show the unknown label');
});

test('a back-off flag with reset_known false shows the unknown label instead of a time', () => {
  const html = render({ backoff: [{ backend: 'opencode', until: '2026-10-22T15:00:00Z', reason: 'rate limited', reset_known: false }] });
  assert.ok(html.includes('reset time unknown'), 'unknown label missing: ' + html);
  assert.ok(!html.includes('Resets:'), 'must not present the capped time as a reset time');
  assert.ok(html.includes('rate limited'), 'reason missing');
});

const BUDGET = {
  llm: 'claude-pro', ceiling_usd: 10, spend_usd: 3, peer_spend_usd: 1,
  window_end: '2026-10-26T00:00:00Z', rolls_over_at: '2026-10-26T01:00:00Z', full: false,
};

test('a budget under the ceiling shows a spend bar, window end, rollover and no badge', () => {
  const html = render({ budgets: [BUDGET] });
  assert.ok(html.includes('LLM budgets'), 'budget title missing');
  assert.ok(html.includes('$4.00 of $10.00'), 'spend over ceiling missing: ' + html);
  assert.ok(html.includes('width:40%'), 'spend bar width missing');
  assert.ok(html.includes('Window ends'), 'window end missing');
  assert.ok(html.includes('Rolls over'), 'rollover missing');
  assert.ok(!html.includes('budget window full'), 'no badge under the ceiling');
  assert.ok(!html.includes('var(--error'), 'bar must not be red under the ceiling');
});

test('a full budget shows a red bar and the badge', () => {
  const html = render({ budgets: [{ ...BUDGET, spend_usd: 10, peer_spend_usd: 0, full: true }] });
  assert.ok(html.includes('budget window full'), 'badge missing');
  assert.ok(html.includes('width:100%'), 'bar must be full');
  assert.ok(html.includes('background:var(--error'), 'bar must be red');
});

test('the allocation share and its estimate label appear only when the daemon sent an allocation', () => {
  const without = render({ budgets: [BUDGET] });
  assert.ok(!without.includes('Allocation'), 'no allocation without data');
  assert.ok(!without.includes('estimated'), 'no estimate label without data');
  assert.ok(!without.includes('An estimate'), 'no estimate tooltip without data');
  const withAlloc = render({ budgets: [{ ...BUDGET, allocation: { allocation_pct: 42.5, estimated: true } }] });
  assert.ok(/42\.5%[^<]*\(estimated\)/.test(withAlloc), 'allocation must be labelled an estimate: ' + withAlloc);
  assert.ok(withAlloc.includes('estimate derived from the plan tier'), 'estimate tooltip missing');
});

test('an empty response renders nothing so the card hides', () => {
  assert.equal(render({ pools: [], leases: [], waiting: [], backoff: [], budgets: [] }), '');
  assert.equal(render({}), '');
  assert.equal(render(null), '');
  assert.ok(render({}, { showEmpty: true }).includes('No capacity limits'), 'showEmpty placeholder missing');
});

test('HTML-special characters in backend, LLM, pool and reason text are escaped', () => {
  const html = render({
    backoff: [{ backend: '<script>alert(1)</script>', until: '2026-10-22T15:00:00Z', reason: '<b>x</b>', reset_known: true }],
    budgets: [{ llm: '<img src=x onerror=1>', ceiling_usd: 1, spend_usd: 0, peer_spend_usd: 0, full: false }],
    pools: [{ name: 'llm:<i>q</i>', limit: 1, held: 0, external: 0 }],
  });
  for (const raw of ['<script>', '<img', '<b>x', '<i>q']) {
    assert.ok(!html.includes(raw), 'unescaped ' + raw + ' in: ' + html);
  }
  assert.ok(html.includes('&lt;script&gt;'), 'backend must be escaped');
});

test('the poller keeps its 5 second interval on window and hides an empty card', async () => {
  const sandbox = loadApp();
  const slot = { style: { display: 'block' }, innerHTML: 'old' };
  sandbox.document.getElementById = id => (id === 'prdCapacityCard' ? slot : null);
  const calls = [];
  sandbox.apiFetch = url => { calls.push(url); return Promise.resolve({ pools: [], backoff: [], budgets: [], waiting: [] }); };
  const intervals = [];
  sandbox.setInterval = (fn, ms) => { intervals.push(ms); return 7; };
  sandbox.clearInterval = () => {};
  vm.runInContext('window._loadPRDCapacityCard()', sandbox);
  await new Promise(r => setImmediate(r));
  assert.deepEqual(intervals, [5000]);
  assert.equal(vm.runInContext('window._prdCapacityInterval', sandbox), 7);
  assert.equal(calls[0], '/api/capacity', 'the no-argument call must not send a prd_id');
  assert.equal(slot.style.display, 'none');
  assert.equal(slot.innerHTML, '');
});

test('the Automata list body no longer mounts the capacity card over the list', () => {
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  const body = src.slice(src.indexOf('id="automataFilterBar"'), src.indexOf('<div id="automataPanel"'));
  assert.ok(!body.includes('id="prdCapacityCard"'), 'the list body must not mount a capacity card slot');
  assert.ok(!body.includes('id="progressCard"'), 'the list body must not mount a progress card slot');
  assert.ok(!src.includes('window._loadPRDCapacityCard();'), 'the list view must not start the machine-wide poller');
});

test('every new capacity_card_ key exists in all five locale bundles with a real value', () => {
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  const keys = [...new Set([...src.matchAll(/'(capacity_card_[a-z_]+)'/g)].map(m => m[1]))];
  for (const k of ['capacity_card_backoff_title', 'capacity_card_backoff_reset_unknown', 'capacity_card_budget_title',
    'capacity_card_budget_estimate', 'capacity_card_holders', 'capacity_card_empty']) {
    assert.ok(keys.includes(k), k + ' is not used by app.js');
  }
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    for (const k of keys) {
      assert.ok(typeof b[k] === 'string' && b[k].trim(), lang + ' bundle is missing ' + k);
      if (lang !== 'en' && k !== 'capacity_card_title') assert.notEqual(b[k], en[k], lang + ' ' + k + ' is an untranslated copy of English');
      assert.ok(!/\b(BL|B|F|S)\d+\b/.test(b[k]), lang + ' ' + k + ' contains a tracker id');
    }
  }
});
