// Node-pool usage rows on the capacity card — PWA side.
//
// The daemon attaches an optional `usage` object to `node:<name>` pool
// entries (model server probe + observer GPU figures). window.capacityCardHtml
// renders it as extra rows under the pool's slot bar: loaded models with
// size-in-memory and expiry, GPU util/memory coloured at the 75/100 thresholds,
// and the plain "no data" lines when there are none. A pool without `usage`
// (older daemon bodies), and every non-node pool, renders exactly as before.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-capacity-node-usage.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
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

const NOW = Date.parse('2026-10-22T15:00:00Z');
const GIB = 1073741824;

const POOL = { name: 'node:gpu1', limit: 2, held: 1, external: 0 };

test('a limited node with a loaded model shows name, GiB size and the expiry against the pinned clock', () => {
  const html = render({
    pools: [{ ...POOL, usage: {
      state: 'ok', models_supported: true,
      models: [{ name: 'stitch-moe', size_bytes: 13743895347, expires_at: '2026-10-22T15:01:30Z' }],
      gpu: [{ name: 'NVIDIA H100', util_pct: 40, mem_used_bytes: 7 * GIB, mem_total_bytes: 80 * GIB }],
    } }],
  }, { now: NOW });
  assert.ok(html.includes('Compute node: gpu1'), 'node pool label missing: ' + html);
  assert.ok(html.includes('1 / 2'), 'used / limit slot text missing: ' + html);
  assert.ok(html.includes('Model: stitch-moe'), 'model name missing: ' + html);
  assert.ok(html.includes('12.8 GiB in memory'), 'GiB size line missing: ' + html);
  assert.ok(html.includes('expires in 1m 30s'), 'expiry must use the pinned clock: ' + html);
  assert.ok(!html.includes('stays loaded'), 'a non-expiry model must not show the forever label');
  assert.ok(!html.includes('expiring now'), 'a future expiry must not read as expiring now');
});

test('a model that never expires shows the stays-loaded wording and no expiry text', () => {
  const html = render({
    pools: [{ ...POOL, usage: {
      state: 'ok', models_supported: true,
      models: [{ name: 'evergreen', size_bytes: 8 * GIB, no_expiry: true }],
    } }],
  }, { now: NOW });
  assert.ok(html.includes('Model: evergreen'), 'model name missing: ' + html);
  assert.ok(html.includes('8.0 GiB in memory'), 'GiB size missing: ' + html);
  assert.ok(html.includes('stays loaded'), 'stays-loaded wording missing: ' + html);
  assert.ok(!html.includes('expires in'), 'no expiry time for a never-expiring model: ' + html);
  assert.ok(!html.includes('expiring now'), 'no expiring-now text for a never-expiring model');
});

test('two loaded models render as two separate rows, each with its own size and expiry', () => {
  const html = render({
    pools: [{ ...POOL, usage: {
      state: 'ok', models_supported: true,
      models: [
        { name: 'first', size_bytes: 7 * GIB, no_expiry: true },
        { name: 'second', size_bytes: 14 * GIB, expires_at: '2026-10-22T16:00:00Z' },
      ],
    } }],
  }, { now: NOW });
  const modelRows = html.match(/Model: /g) || [];
  assert.equal(modelRows.length, 2, 'expected exactly two model rows: ' + html);
  assert.ok(html.includes('Model: first — 7.0 GiB in memory · stays loaded'), 'first model row wrong: ' + html);
  assert.ok(html.includes('Model: second — 14.0 GiB in memory · expires in 1h 0m'), 'second model row wrong: ' + html);
});

test('a node with no model loaded shows the no-model line', () => {
  const html = render({
    pools: [{ ...POOL, usage: {
      state: 'ok', models_supported: true, models: [],
      gpu: [{ name: 'GPU-0', util_pct: 10, mem_used_bytes: 2 * GIB, mem_total_bytes: 24 * GIB }],
    } }],
  }, { now: NOW });
  assert.ok(html.includes('No model loaded'), 'no-model line missing: ' + html);
  assert.ok(html.includes('GPU 10%'), 'GPU line still renders alongside: ' + html);
});

test('an unreachable model server shows the chip with the error escaped in its tooltip', () => {
  const error = '<img src=x onerror=alert(1)> timeout after 3 tries';
  const html = render({
    pools: [{ ...POOL, usage: { state: 'unreachable', error } }],
  }, { now: NOW });
  assert.ok(html.includes('Unreachable'), 'unreachable chip text missing: ' + html);
  assert.ok(html.includes('title="&lt;img src=x onerror=alert(1)&gt; timeout after 3 tries"'), 'error must be escaped in the title: ' + html);
  assert.ok(!html.includes('<img src=x'), 'raw error HTML must not leak: ' + html);
});

test('a pending node shows the checking line and nothing fabricated', () => {
  const html = render({
    pools: [{ ...POOL, usage: { state: 'pending' } }],
  }, { now: NOW });
  assert.ok(html.includes('Checking...'), 'checking line missing: ' + html);
  assert.ok(html.includes('cap-node-usage'), 'usage block missing: ' + html);
  assert.ok(!html.includes('No model loaded'), 'a pending node must not claim no model');
  assert.ok(!html.includes('No GPU data from the observer'), 'a pending node must not claim no GPU data');
});

test('GPU util and memory take the right colour below 75 percent', () => {
  const html = render({
    pools: [{ ...POOL, usage: {
      state: 'ok',
      gpu: [{ name: 'NVIDIA H100', util_pct: 40, mem_used_bytes: 7 * GIB, mem_total_bytes: 80 * GIB }],
    } }],
  }, { now: NOW });
  assert.ok(html.includes('NVIDIA H100'), 'GPU name missing: ' + html);
  assert.ok(html.includes('color:var(--success,#22c55e);">GPU 40%'), 'below 75 must use the success colour: ' + html);
  assert.ok(html.includes('GPU memory 7.0 GiB of 80.0 GiB'), 'GPU memory line missing: ' + html);
  assert.ok(!html.includes('color:var(--warning,#f59e0b);">GPU 40%'), 'must not be warning at 40');
  assert.ok(!html.includes('color:var(--error,#ef4444);">GPU 40%'), 'must not be error at 40');
});

test('GPU util and memory take the warning colour at exactly 75 percent', () => {
  const html = render({
    pools: [{ ...POOL, usage: {
      state: 'ok',
      gpu: [{ name: 'NVIDIA H100', util_pct: 75, mem_used_bytes: 58 * GIB, mem_total_bytes: 80 * GIB }],
    } }],
  }, { now: NOW });
  assert.ok(html.includes('color:var(--warning,#f59e0b);">GPU 75%'), '75 must use the warning colour: ' + html);
  assert.ok(html.includes('GPU memory 58.0 GiB of 80.0 GiB'), 'GPU memory line missing: ' + html);
  assert.ok(!html.includes('color:var(--error,#ef4444);">GPU 75%'), 'must not read as error at 75');
  assert.ok(!html.includes('color:var(--success,#22c55e);">GPU 75%'), 'must not read as success at 75');
});

test('GPU util and memory take the error colour at 100 percent, clamping over-100 values', () => {
  const html = render({
    pools: [{ ...POOL, usage: {
      state: 'ok',
      gpu: [{ name: 'NVIDIA H100', util_pct: 103, mem_used_bytes: 80 * GIB, mem_total_bytes: 80 * GIB }],
    } }],
  }, { now: NOW });
  assert.ok(html.includes('color:var(--error,#ef4444);">GPU 100%'), '100 must use the error colour: ' + html);
  assert.ok(!html.includes('GPU 103%'), 'util must clamp at 100: ' + html);
  assert.ok(html.includes('GPU memory 80.0 GiB of 80.0 GiB'), 'GPU memory line missing: ' + html);
});

test('a GPU-less ok node shows the no-GPU-data line', () => {
  const html = render({
    pools: [{ ...POOL, usage: {
      state: 'ok', models_supported: true,
      models: [{ name: 'small', size_bytes: 4 * GIB, no_expiry: true }],
    } }],
  }, { now: NOW });
  assert.ok(html.includes('No GPU data from the observer'), 'no-GPU-data line missing: ' + html);
});

test('a node pool without a usage block renders exactly as before', () => {
  const noField = { pools: [{ name: 'node:gpu1', limit: 2, held: 2, external: 0, holder: 'abcdef1234567890' }] };
  const explicitAbsent = { pools: [{ name: 'node:gpu1', limit: 2, held: 2, external: 0, holder: 'abcdef1234567890', usage: undefined }] };
  const a = render(noField, { now: NOW });
  const b = render(explicitAbsent, { now: NOW });
  assert.equal(a, b, 'usage-present-but-undefined must render identically to no field at all');
  assert.ok(!a.includes('cap-node-usage'), 'no usage block without the field: ' + a);
  assert.ok(a.includes('2 / 2'), 'slot bar still renders: ' + a);
  assert.ok(a.includes('Holders: abcdef12'), 'holders line still renders: ' + a);
  // And a body WITH the block differs from that baseline by containing the block.
  const withUsage = render({ pools: [{ name: 'node:gpu1', limit: 2, held: 2, external: 0,
    holder: 'abcdef1234567890', usage: { state: 'ok', models_supported: true, models: [{ name: 'm', size_bytes: GIB }] } }] }, { now: NOW });
  assert.ok(withUsage.includes('cap-node-usage'), 'usage block must appear when the field is present: ' + withUsage);
});

test('host and llm pools never get usage rows even if the body carries a usage object', () => {
  const hostHtml = render({
    pools: [{ name: 'host', limit: 4, held: 1, external: 0, usage: { state: 'ok', models_supported: true, models: [{ name: 'sneaky', size_bytes: GIB }] } }],
  }, { now: NOW });
  assert.ok(!hostHtml.includes('cap-node-usage'), 'host pool must not get a usage block: ' + hostHtml);
  assert.ok(!hostHtml.includes('sneaky'), 'host pool must not render model rows: ' + hostHtml);
  const llmHtml = render({
    pools: [{ name: 'llm:claude-pro', limit: 4, held: 1, external: 0, usage: { state: 'ok', gpu: [{ name: 'GPU-0', util_pct: 90 }] } }],
  }, { now: NOW });
  assert.ok(!llmHtml.includes('cap-node-usage'), 'llm pool must not get a usage block: ' + llmHtml);
  assert.ok(!llmHtml.includes('GPU 90%'), 'llm pool must not render GPU rows: ' + llmHtml);
});

test('unlimited and idle node rows also carry the usage block', () => {
  const unlimited = render({
    pools: [{ name: 'node:gpu2', limit: 0, held: 1, external: 0,
      usage: { state: 'ok', models_supported: true, models: [{ name: 'wide-open', size_bytes: 6 * GIB, no_expiry: true }] } }],
  }, { now: NOW });
  assert.ok(unlimited.includes('no limit'), 'unlimited marker missing: ' + unlimited);
  assert.ok(unlimited.includes('cap-node-usage'), 'unlimited node must still show usage: ' + unlimited);
  assert.ok(unlimited.includes('Model: wide-open'), 'unlimited node model row missing: ' + unlimited);

  const idle = render({
    pools: [{ name: 'node:gpu3', limit: 0, held: 0, external: 0,
      usage: { state: 'ok', models_supported: true, models: [], gpu: [{ name: 'GPU-0', util_pct: 5 }] } }],
  }, { now: NOW });
  assert.ok(idle.includes('no cap configured'), 'idle marker missing: ' + idle);
  assert.ok(idle.includes('cap-node-usage'), 'idle node must still show usage: ' + idle);
  assert.ok(idle.includes('GPU 5%'), 'idle node GPU row missing: ' + idle);
});

test('HTML in a model name and an error is escaped', () => {
  const evilModel = '<script>alert("nope")</script>';
  const evilError = '<img src=x onerror=1>';
  const html = render({
    pools: [
      { name: 'node:evil', limit: 2, held: 1, external: 0,
        usage: { state: 'ok', models_supported: true, models: [{ name: evilModel, size_bytes: GIB }] } },
      { name: 'node:broken', limit: 2, held: 1, external: 0, usage: { state: 'unreachable', error: evilError } },
    ],
  }, { now: NOW });
  for (const raw of ['<script>', '<img src=x']) {
    assert.ok(!html.includes(raw), 'unescaped ' + raw + ' leaked: ' + html);
  }
  assert.ok(html.includes('Model: &lt;script&gt;alert(&quot;nope&quot;)&lt;/script&gt;'), 'model name must be escaped: ' + html);
  assert.ok(html.includes('title="&lt;img src=x onerror=1&gt;"'), 'error tooltip must be escaped: ' + html);
});

test('the used / limit slot text sits beside the new usage rows', () => {
  const html = render({
    pools: [{ name: 'node:gpu1', limit: 9, held: 3, external: 1,
      usage: { state: 'ok', models_supported: true, models: [{ name: 'm1', size_bytes: 2 * GIB, no_expiry: true }] } }],
  }, { now: NOW });
  assert.ok(html.includes('4 / 9'), 'used / limit slot text must still render: ' + html);
  assert.ok(html.includes('cap-node-usage'), 'usage block missing: ' + html);
});

test('every locale bundle defines all the capacity_node_ keys app.js uses', () => {
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  const keys = [...new Set([...src.matchAll(/'(capacity_node_[a-z_0-9]+)'/g)].map(m => m[1]))];
  for (const k of ['capacity_node_checking', 'capacity_node_unreachable', 'capacity_node_model',
    'capacity_node_in_memory', 'capacity_node_expires', 'capacity_node_never_expires',
    'capacity_node_gpu_util', 'capacity_node_no_gpu_data']) {
    assert.ok(keys.includes(k), k + ' is not used by app.js');
  }
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    for (const k of keys) {
      assert.ok(typeof b[k] === 'string' && b[k].trim(), lang + ' bundle is missing ' + k);
      assert.ok(!/\b(BL|B|F|S)\d+\b/.test(b[k]), lang + ' ' + k + ' contains a tracker id');
    }
  }
  // Every key except the GPU-abbreviation ones must actually be translated
  // ("GPU {pct}%" is identical across languages on purpose).
  for (const lang of ['de', 'es', 'fr', 'ja']) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    for (const k of keys) {
      if (k === 'capacity_node_gpu_util') continue;
      assert.notEqual(b[k], en[k], lang + ' ' + k + ' is an untranslated copy of English');
    }
  }
});
