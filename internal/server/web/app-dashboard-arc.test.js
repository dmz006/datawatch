// Arc view card (GET /api/dashboard/arc) — the Automation arc as ordered
// stages with a progress bar each, the maintenance queue as its own lane,
// the blocked stage with its reason, the shared Claude and pi lanes (queued
// work by priority) and the plan usage in points and tokens (never money).
// The card is opt-in: it is defined and addable, but not in the default
// layout, and the five console cards are untouched.
//
// Loads the real app.js into the shared Node vm stub browser (see
// testutil_browser_stub.js for the mechanics) and fakes the network.

'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { buildSandbox, loadScript, flushAsync, vm, makeStubElement } = require('./testutil_browser_stub.js');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];

const ARC_PAYLOAD = {
  generated_at: '2026-11-11T10:00:00Z',
  available: true,
  arc: {
    available: true,
    id: 'arc-1',
    name: 'Release 9.120',
    stages: [
      { stage: 'bugs',     total: 3, done: 3, running: 0, blocked: 0, percent: 100 },
      { stage: 'features', total: 5, done: 2, running: 1, blocked: 0, percent: 40 },
      { stage: 'final',    total: 1, done: 0, running: 0, blocked: 1, percent: 0 },
    ],
    maintenance_queue: { total: 4, done: 1, running: 2, blocked: 1 },
    blocked_stage: { stage: 'final', reason: 'Ship the release is blocked: waiting on features' },
  },
  lanes: {
    available: true,
    lanes: [
      { kind: 'claude', in_flight: 1, cap: 3 },
      { kind: 'pi',     in_flight: 2, cap: 2 },
    ],
    queued_by_priority: [
      { lane: 'pi',     priority: 1, count: 4 },
      { lane: 'claude', priority: 1, count: 1 },
      { lane: 'claude', priority: 5, count: 2 },
    ],
  },
  usage: {
    available: true,
    points: { used: 41, allowance: 100, remaining: 59, percent: 41 },
    tokens: { in: 900000, out: 300000, total: 1200000 },
  },
};

// makeEnv boots app.js fresh with the English bundle loaded and one
// capturing stub element for the card body.
function makeEnv(payload, opts = {}) {
  const arcEl = makeStubElement();
  const byId = { dashArcCard: arcEl, dashCardGrid: makeStubElement() };
  const sandbox = vm.createContext(buildSandbox({ byId }));
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  const calls = [];
  sandbox.fetch = (url) => {
    calls.push(url);
    if (opts.fail) return Promise.reject(new Error('offline'));
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(opts.payload || payload) });
  };
  return { sandbox, arcEl, byId, calls };
}

function evalIn(sandbox, code) {
  return vm.runInContext(code, sandbox);
}

// render fetches the fake payload and returns the card HTML.
async function render(payload) {
  const env = makeEnv(payload);
  await flushAsync();
  evalIn(env.sandbox, '_dashFetchArc();');
  await flushAsync(20);
  return Object.assign(env, { html: env.arcEl.innerHTML });
}

test('the arc card is defined with a label key and a body, addable but not in the default layout', () => {
  const { sandbox } = makeEnv(ARC_PAYLOAD);
  const def = evalIn(sandbox, 'DASH_CARD_DEFS.find(d => d.id === "arc-view")');
  assert.ok(def, 'arc-view is in DASH_CARD_DEFS');
  assert.equal(def.labelKey, 'dash_arc_card');
  assert.equal(typeof def.body, 'function');
  assert.ok(def.body().includes('id="dashArcCard"'));
  const layout = evalIn(sandbox, '_dashDefaultLayout().map(c => c.id)');
  assert.ok(!layout.includes('arc-view'), 'opt-in: not in the default layout');
  // the five console cards still lead the board
  assert.equal(Array.from(layout).slice(0, 5).join(','),
    'console-questions,console-held,console-lanes,console-usage,console-ci-release');
  assert.equal(evalIn(sandbox, 'DASH_CONSOLE_IDS.has("arc-view")'), false);
});

test('before the first payload the card shows its loading line, not raw markup', () => {
  const { sandbox } = makeEnv(ARC_PAYLOAD);
  const html = evalIn(sandbox, '_dashArcCardHTML()');
  assert.ok(html.includes('Loading the plan summary'));
  assert.ok(!html.includes('<script'));
});

test('fetch goes to /api/dashboard/arc once; concurrent calls share the request', async () => {
  const env = makeEnv(ARC_PAYLOAD);
  await flushAsync();
  evalIn(env.sandbox, '_dashFetchArc(); _dashFetchArc();');
  await flushAsync(20);
  assert.deepEqual(env.calls.filter(u => u === '/api/dashboard/arc'), ['/api/dashboard/arc']);
  assert.equal(evalIn(env.sandbox, '_dash._arc.arc.id'), 'arc-1');
  assert.equal(evalIn(env.sandbox, '_dash._arcFetching'), false);
});

test('stages render in the order sent with a progress bar and counts each', async () => {
  const { html } = await render(ARC_PAYLOAD);
  assert.ok(html.includes('Release 9.120'));
  const order = ['bugs', 'features', 'final'].map(s => html.indexOf('data-stage="' + s + '"'));
  assert.ok(order.every(i => i >= 0), 'all three stages present');
  assert.ok(order[0] < order[1] && order[1] < order[2], 'bugs, then features, then final');
  assert.ok(html.indexOf('Bugs') < html.indexOf('Features') && html.indexOf('Features') < html.indexOf('Final stage'));
  assert.ok(html.includes('3 of 3 done') && html.includes('2 of 5 done') && html.includes('0 of 1 done'));
  assert.ok(html.includes('1 running · 0 blocked'));
  assert.ok(html.includes('width:40%'), 'features bar is 40% wide');
  assert.ok(html.includes('width:100%'), 'bugs bar is full');
  assert.equal((html.match(/role="progressbar"/g) || []).length >= 3, true);
});

test('a percent outside 0..100 is clamped in the bar width', () => {
  const { sandbox } = makeEnv(ARC_PAYLOAD);
  assert.equal(evalIn(sandbox, '_dashArcPct(250)'), 100);
  assert.equal(evalIn(sandbox, '_dashArcPct(-5)'), 0);
  assert.equal(evalIn(sandbox, '_dashArcPct("x")'), 0);
  assert.equal(evalIn(sandbox, '_dashArcPct(40)'), 40);
});

test('the maintenance queue is its own lane, never a stage row', async () => {
  const { html } = await render(ARC_PAYLOAD);
  assert.ok(html.includes('Maintenance queue'));
  assert.ok(html.includes('4 total · 1 done · 2 running · 1 blocked'));
  assert.ok(!html.includes('data-stage="maintenance"'));
  // the maintenance section sits after the stages section
  assert.ok(html.indexOf('Maintenance queue') > html.indexOf('data-stage="final"'));
});

test('the blocked stage is named with its reason; no blocked stage says so', async () => {
  const blocked = (await render(ARC_PAYLOAD)).html;
  assert.ok(blocked.includes('Stage blocked: Final stage'));
  assert.ok(blocked.includes('Ship the release is blocked: waiting on features'));
  assert.ok(blocked.includes('dash-arc-stage-blocked'), 'blocked stage row is marked');

  const clear = JSON.parse(JSON.stringify(ARC_PAYLOAD));
  clear.arc.blocked_stage = null;
  clear.arc.stages[2].blocked = 0;
  const html = (await render(clear)).html;
  assert.ok(html.includes('No stage is blocked.'));
  assert.ok(!html.includes('dash-arc-stage-blocked'));
});

test('shared lanes show Claude and pi in flight against the cap, queued work by priority', async () => {
  const { html } = await render(ARC_PAYLOAD);
  assert.ok(html.includes('data-lane="claude"') && html.includes('data-lane="pi"'));
  assert.ok(html.includes('Claude lane') && html.includes('pi lane'));
  assert.ok(html.includes('1 of 3 in flight') && html.includes('2 of 2 in flight'));
  assert.ok(html.includes('width:100%'), 'a full pi lane fills its bar');
  assert.ok(html.includes('Queued by priority'));
  // within a lane the higher priority is listed first
  const p5 = html.indexOf('Claude lane · priority 5: 2 queued');
  const p1 = html.indexOf('Claude lane · priority 1: 1 queued');
  assert.ok(p5 >= 0 && p1 >= 0 && p5 < p1, 'priority 5 before priority 1 on the claude lane');
  assert.ok(html.includes('pi lane · priority 1: 4 queued'));
});

test('nothing queued says so', async () => {
  const p = JSON.parse(JSON.stringify(ARC_PAYLOAD));
  p.lanes.queued_by_priority = [];
  const { html } = await render(p);
  assert.ok(html.includes('Nothing is queued.'));
});

test('usage is points and tokens against the plan — never dollars', async () => {
  const { html } = await render(ARC_PAYLOAD);
  assert.ok(html.includes('Plan usage'));
  assert.ok(/41\.0 of 100\.0 points \(41\.0%\)/.test(html), html);
  assert.ok(html.includes('59.0 points remaining'));
  assert.ok(/Tokens: 1,200,000 total \(900,000 in, 300,000 out\)/.test(html), html);
  assert.ok(!/[$€£¥]/.test(html), 'no currency symbol anywhere in the card');
  assert.ok(!/dollar|usd|cost/i.test(html), 'no money wording');
});

test('parts the daemon cannot read show their reason; the rest still renders', async () => {
  const p = JSON.parse(JSON.stringify(ARC_PAYLOAD));
  p.lanes = { available: false, reason: 'neither lane fill nor the capacity ledger is wired in', lanes: [], queued_by_priority: [] };
  p.usage = { available: true, reason: 'the usage calculator is not wired in', tokens: { in: 1, out: 2, total: 3 } };
  const { html } = await render(p);
  assert.ok(html.includes('neither lane fill nor the capacity ledger is wired in'));
  assert.ok(html.includes('the usage calculator is not wired in'));
  assert.ok(html.includes('Tokens: 3 total (1 in, 2 out)'));
  assert.ok(html.includes('data-stage="bugs"'), 'stages unaffected');
});

test('no arc to show: the reason replaces the stage sections; a disabled summary is one line', async () => {
  const none = JSON.parse(JSON.stringify(ARC_PAYLOAD));
  none.arc = { available: false, reason: 'no arc with staged work was found', stages: [] };
  const a = (await render(none)).html;
  assert.ok(a.includes('no arc with staged work was found'));
  assert.ok(!a.includes('data-stage='));

  const off = {
    available: false, reason: 'the dashboard summary is disabled — set dashboard.console.enabled to true',
    arc: { available: false, stages: [] }, lanes: { available: false, lanes: [], queued_by_priority: [] }, usage: { available: false },
  };
  const b = (await render(off)).html;
  assert.ok(b.includes('the dashboard summary is disabled'));
  assert.equal((b.match(/dash-arc-sec/g) || []).length, 0, 'no sections when the summary is off');
});

test('daemon text and ids are escaped, never injected as markup', async () => {
  const p = JSON.parse(JSON.stringify(ARC_PAYLOAD));
  p.arc.name = '<img src=x onerror=alert(1)>';
  p.arc.blocked_stage.reason = '<script>alert(2)</script>';
  p.arc.stages[0].stage = 'bugs"><script>x</script>';
  p.lanes.lanes[0].kind = '<b>claude</b>';
  const { html } = await render(p);
  assert.ok(!html.includes('<script'));
  assert.ok(!html.includes('<img'));
  assert.ok(!html.includes('<b>claude'));
  assert.ok(html.includes('&lt;img'));
});

test('a failed fetch keeps the last payload on screen and clears the in-flight flag', async () => {
  const env = makeEnv(ARC_PAYLOAD);
  await flushAsync();
  evalIn(env.sandbox, '_dashFetchArc();');
  await flushAsync(20);
  const before = env.arcEl.innerHTML;
  assert.ok(before.includes('data-stage="bugs"'));
  env.sandbox.fetch = () => Promise.reject(new Error('offline'));
  evalIn(env.sandbox, '_dashFetchArc();');
  await flushAsync(20);
  assert.equal(evalIn(env.sandbox, '_dash._arcFetching'), false);
  assert.equal(env.arcEl.innerHTML, before);
  assert.equal(evalIn(env.sandbox, '_dash._arc.arc.id'), 'arc-1');
});

test('building a board that carries the arc card primes the fetch', async () => {
  const env = makeEnv(ARC_PAYLOAD);
  await flushAsync();
  evalIn(env.sandbox, '_dashBuildGrid([{ id: "arc-view", cs: 6, rs: 2 }]);');
  await flushAsync(20);
  assert.ok(env.calls.includes('/api/dashboard/arc'));
  assert.ok(env.byId.dashCardGrid.innerHTML.includes('data-card-id="arc-view"'));
});

test('it adds no interval of its own (refresh rides the dashboard loop)', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const start = src.indexOf('// ── Arc view (GET /api/dashboard/arc)');
  const end = src.indexOf('const DASH_CARD_DEFS = [', start);
  assert.ok(start > 0 && end > start);
  assert.ok(!/setInterval/.test(src.slice(start, end)));
});

test('every dash_arc_* key used in app.js has a real value in all five locale bundles', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const keys = new Set();
  let m;
  const re = /['"](dash_arc_[a-z_]+)['"]/g;
  while ((m = re.exec(src)) !== null) { if (!m[1].endsWith('_')) keys.add(m[1]); }
  // prefixed keys built at runtime (stage ids and lane kinds)
  for (const s of ['bugs', 'features', 'final']) keys.add('dash_arc_stage_' + s);
  for (const k of ['claude', 'pi']) keys.add('dash_arc_lane_' + k);
  assert.ok(keys.size >= 25, 'found the arc keys: ' + keys.size);
  const bundles = {};
  for (const loc of LOCALES) {
    bundles[loc] = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', loc + '.json'), 'utf8'));
    for (const k of keys) {
      assert.ok(typeof bundles[loc][k] === 'string' && bundles[loc][k].length > 0, `${loc}/${k} missing or empty`);
    }
  }
  // real translations: every non-English bundle differs from English for
  // most keys (a copy of the English string is the failure this catches).
  for (const loc of LOCALES.filter(l => l !== 'en')) {
    const same = [...keys].filter(k => bundles[loc][k] === bundles.en[k]);
    assert.ok(same.length <= 6, `${loc} copies English for: ${same.join(', ')}`);
  }
  // placeholders survive translation
  for (const loc of LOCALES) {
    for (const k of keys) {
      const ph = s => (s.match(/%\d\$[sd]/g) || []).sort().join(',');
      assert.equal(ph(bundles[loc][k]), ph(bundles.en[k]), `${loc}/${k} placeholders differ from en`);
    }
  }
});

test('no tracker ids in any visible arc string', () => {
  for (const loc of LOCALES) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', loc + '.json'), 'utf8'));
    for (const [k, v] of Object.entries(b)) {
      if (!k.startsWith('dash_arc_')) continue;
      assert.ok(!/\b(BL|GH|SEC|HLLM)-?\d+\b|\bB\d{2,3}\b/.test(v), `${loc}/${k}: ${v}`);
    }
  }
});
