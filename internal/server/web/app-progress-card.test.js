// Automata progress card — PWA side.
//
// window.progressCardHtml(state, opts) is a pure function from the loader state
// to markup; window.loadProgressCard fetches the metrics through apiFetch and
// keeps the last good data when a refresh fails.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');

const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const NOW = Date.parse('2026-10-23T12:00:00Z');

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
}

function loadApp(opts) {
  const sandbox = buildSandbox(opts);
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = bundle('en');
  return sandbox;
}

function render(state, opts) {
  const sandbox = loadApp();
  sandbox._state = state;
  sandbox._opts = Object.assign({ now: NOW }, opts || {});
  return vm.runInContext('window.progressCardHtml(_state, _opts)', sandbox);
}

function group(over) {
  return Object.assign({
    prds: 4, done: 2, running: 1, waiting: 1, blocked: 0, tasks: 20, tasks_done: 10, tasks_left: 10,
    work_hours: 12.5, median_prd_wall_hours: 3.5, mean_prd_wall_hours: 4, mean_task_hours: 0.6, median_task_hours: 0.5,
  }, over || {});
}

function metrics(over) {
  return Object.assign({
    generated_at: '2026-10-23T11:58:00Z', scope: 'all', calendar_days: 10, active_days: 7, lanes: 5,
    groups: { feature: group(), bug: group({ prds: 1, done: 1 }), other: group({ prds: 0 }) },
    totals: group({ prds: 5, done: 3 }),
    completed_prds: 3, running_prds: 1,
    throughput: { prds_per_day_3d: 1.5, prds_per_day_24h: 2, prds_last_3d: 4, prds_last_24h: 2, tasks_per_day: 8.2, tasks_last_24h: 9, tasks_per_day_24h: 9 },
    utilization: { worker_hours_per_day: 10, lane_capacity_hours_per_day: 120, ratio: 0.08 },
    lead_time: {},
    inactive: {
      windows: [
        { start: '2026-10-20T01:00:00Z', end: '2026-10-20T09:00:00Z', kind: 'gap', reason: 'no task activity', inferred: true },
        { start: '2026-10-21T01:00:00Z', end: '2026-10-21T05:00:00Z', kind: 'maintenance', reason: 'planned', inferred: false },
      ],
      excluded_hours: 12, share_of_calendar: 0.05, calendar_estimate_days: 3,
    },
    remaining: {
      known: { prds: 3, tasks: 11, undecomposed_prds: 1 },
      estimated: { new_prds: 2, undecomposed_prds: 1, estimated_tasks: 18, tasks_per_prd: { mean: 6.4, median: 5, min: 2, max: 12, samples: 3 } },
      combined: { prds: 5, tasks: 29, label: 'estimate' },
    },
    completion: {
      by_prd_rate_days: 3.3, by_task_rate_days: 3.5, by_task_rate_24h_days: null, by_worker_hours_days: 2.9,
      low_days: 2.9, high_days: 3.5, drivers: ['utilization', 'large_prd_mix', 'undecomposed_backlog', 'sparse_history'],
      method: { prd_rate: 'server sentence', task_rate: 'server sentence' }, calendar_days: 4.1,
    },
    unfiled_rows: [{ number: '7', item: 'SECRET ROW TEXT', status: 'open', kind: 'feature', open: true, unfiled: true }],
    backlog_available: true, not_measured: [],
  }, over || {});
}

test('loading state shows the title and the loading text and no table', () => {
  const html = render({ status: 'loading', data: null, error: '', fetchedAt: 0, trend: null });
  assert.ok(html.includes('Progress'), html);
  assert.ok(html.includes('Loading progress'), html);
  assert.ok(!html.includes('<table'), 'no table while loading');
  assert.ok(!html.includes('progress-card-retry'), 'no retry while loading');
});

test('ok state shows the table rows, the totals and the columns', () => {
  const html = render({ status: 'ok', data: metrics(), error: '', fetchedAt: NOW, trend: { direction: 'converging' } });
  for (const label of ['Automata', 'Done', 'Running', 'Waiting', 'Blocked', 'Tasks', 'Tasks done', 'Tasks left', 'Work time', 'Median per Automaton', 'Mean per task']) {
    assert.ok(html.includes('>' + label + '<'), 'column ' + label + ' missing');
  }
  for (const label of ['Feature', 'Bug', 'Total']) assert.ok(html.includes('>' + label + '<'), 'row ' + label + ' missing');
  assert.ok(!html.includes('>Other<'), 'the other row stays hidden while it is empty');
  assert.ok(html.includes('12.5 h'), 'work time missing');
  assert.ok(html.includes('0.6 h'), 'mean per task missing');
  assert.ok(html.includes('data-progress-status="ok"'));
  assert.ok(!html.includes('progress-card-stale'), 'fresh data is not stale');
});

test('the other row shows once it is non-zero', () => {
  const html = render({ status: 'ok', data: metrics({ groups: { feature: group(), bug: group(), other: group({ prds: 2 }) } }), trend: null });
  assert.ok(html.includes('>Other<'));
});

test('remaining block keeps known and estimated apart and labels the sum an estimate', () => {
  const html = render({ status: 'ok', data: metrics(), trend: null });
  assert.ok(html.includes('Known (these Automata exist)'));
  assert.ok(html.includes('Estimated (not filed or not decomposed yet)'));
  assert.ok(html.includes('New items not filed yet:'));
  assert.ok(html.includes('Automata with no tasks yet:'));
  assert.ok(html.includes('Tasks per Automaton, mean:'));
  assert.ok(html.includes('Tasks per Automaton, median:'));
  assert.ok(/Tasks per Automaton, mean:<\/span> 6\.4</.test(html), 'mean missing');
  assert.ok(/Tasks per Automaton, median:<\/span> 5</.test(html), 'median missing');
  assert.ok(html.includes('Combined estimate (known plus estimated):'));
  assert.ok(html.includes('about 5 Automata and about 29 tasks'));
});

test('completion range lists each method with its sentence and the drivers as localized text', () => {
  const html = render({ status: 'ok', data: metrics(), trend: null });
  assert.ok(html.includes('2.9 d') && html.includes('3.5 d'), 'range ends missing');
  assert.ok(html.includes('an estimate, not a promise'));
  assert.ok(html.includes('Assumes Automata finish at the rate of the last three active days.'));
  assert.ok(html.includes('Assumes each task takes the average worker time'));
  assert.ok(!html.includes('server sentence'), 'the localized sentence wins for a known method');
  assert.ok(html.includes('Workers were idle much of the time, so the rate may rise.'));
  assert.ok(html.includes('A few large Automata pull the average task count up.'));
  assert.ok(!html.includes('large_prd_mix'), 'a driver key must never show raw');
});

test('throughput, utilization, windows with the inferred mark, backlog count and trend', () => {
  const html = render({ status: 'ok', data: metrics(), trend: { direction: 'widening' } });
  assert.ok(html.includes('Last 24 hours:') && html.includes('2 Automata and 9 tasks finished'));
  assert.ok(html.includes('Last 3 days:') && html.includes('4 Automata finished (1.5 per active day), 8.2 tasks per active day'));
  assert.ok(html.includes('10.0 h per day, 8% of 5 lanes'));
  assert.ok(html.includes('2 windows, 12.0 h, 5% of calendar time'));
  assert.equal((html.match(/progress-card-inferred/g) || []).length, 1, 'only the inferred window is marked');
  assert.ok(html.includes('2026-10-20 01:00'));
  assert.ok(html.includes('Rows not filed yet:'));
  assert.ok(html.includes('widening'));
});

test('the unfiled backlog shows a count and no row text', () => {
  const html = render({ status: 'ok', data: metrics(), trend: null });
  assert.ok(/Rows not filed yet:<\/span> 1</.test(html), html);
  assert.ok(!html.includes('SECRET ROW TEXT'), 'row text must not leak');
});

test('nulls render as not measured and never as 0', () => {
  const data = metrics({
    groups: { feature: group({ median_prd_wall_hours: null, mean_task_hours: null }), bug: group(), other: group({ prds: 0 }) },
    completion: { by_prd_rate_days: null, by_task_rate_days: null, by_task_rate_24h_days: null, by_worker_hours_days: null, low_days: null, high_days: null, drivers: [], method: {} },
    remaining: { known: { prds: 0, tasks: 0, undecomposed_prds: 0 }, estimated: { new_prds: 0, undecomposed_prds: 0, estimated_tasks: 0, tasks_per_prd: { mean: 0, median: 0, samples: 0 } }, combined: { prds: 0, tasks: 0 } },
    inactive: { windows: [], excluded_hours: null, share_of_calendar: null },
    utilization: { worker_hours_per_day: null, ratio: null },
    backlog_available: false,
  });
  const html = render({ status: 'ok', data, trend: null });
  const nm = (html.match(/not measured/g) || []).length;
  assert.ok(nm >= 12, 'expected many not-measured cells, got ' + nm);
  assert.ok(!html.includes('0.0 h') && !html.includes('0.0 d'), 'a null must not render as zero: ' + html);
  assert.ok(!/Rows not filed yet:<\/span> 0/.test(html), 'unavailable backlog is not 0');
  assert.ok(/Completion range:<\/span> not measured/.test(html), 'trend null is not measured');
  assert.ok(/Tasks per Automaton, mean:<\/span> not measured/.test(html), 'a mean with no samples is not measured');
});

test('stale after a failed refresh keeps the old data and shows the age', () => {
  const html = render({ status: 'stale', data: metrics(), error: 'timeout (15 s)', fetchedAt: NOW - 125000, trend: null });
  assert.ok(html.includes('progress-card-stale'), html);
  assert.ok(html.includes('data-progress-status="stale"'));
  assert.ok(html.includes('Out of date'));
  assert.ok(html.includes('Last updated 120 s ago'), html);
  assert.ok(html.includes('timeout (15 s)'));
  assert.ok(html.includes('<table'), 'old data stays visible');
  assert.ok(!html.includes('progress-card-retry'), 'retry shows only with no data');
});

test('data generated more than five minutes ago counts as stale even when the fetch worked', () => {
  const html = render({ status: 'ok', data: metrics({ generated_at: '2026-10-23T11:50:00Z' }), error: '', fetchedAt: NOW, trend: null });
  assert.ok(html.includes('progress-card-stale'), html);
  assert.ok(html.includes('Last updated 600 s ago'), html);
  const fresh = render({ status: 'ok', data: metrics({ generated_at: '2026-10-23T11:55:30Z' }), trend: null });
  assert.ok(!fresh.includes('progress-card-stale'), 'under five minutes is fresh');
});

test('failed with no data shows the message and a retry button', () => {
  const html = render({ status: 'failed', data: null, error: 'HTTP 500', fetchedAt: 0, trend: null });
  assert.ok(html.includes('Progress could not be loaded'));
  assert.ok(html.includes('HTTP 500'));
  assert.ok(html.includes('progress-card-retry'));
  assert.ok(html.includes('loadProgressCard(true)'));
  assert.ok(html.includes('Retry'));
  assert.ok(!html.includes('<table'));
});

test('hostile strings are escaped everywhere they can appear', () => {
  const evil = '<img src=x onerror=alert(1)>';
  const data = metrics({
    inactive: { windows: [{ start: evil, end: evil, kind: evil, reason: evil, inferred: true }], excluded_hours: 1, share_of_calendar: 0.1 },
    completion: Object.assign(metrics().completion, { drivers: [evil], method: { [evil]: evil } }),
    generated_at: '2026-10-23T11:59:00Z',
  });
  const html = render({ status: 'stale', data, error: evil, fetchedAt: NOW, trend: { direction: evil } });
  assert.ok(!html.includes('<img'), 'raw tag leaked: ' + html);
  assert.ok(!html.includes('onerror=alert(1)>'), 'raw handler leaked');
  assert.ok(html.includes('&lt;img'), 'escaped form expected');
  const failed = render({ status: 'failed', data: null, error: evil, trend: null });
  assert.ok(!failed.includes('<img'));
});

test('kind chip shows for an explicit kind only and is escaped', () => {
  const sandbox = loadApp();
  const chip = k => vm.runInContext('window.automatonKindChipHtml(' + JSON.stringify({ kind: k }) + ')', sandbox);
  assert.ok(chip('bug').includes('>Bug<'));
  assert.ok(chip('feature').includes('>Feature<'));
  assert.ok(chip('other').includes('>Other<'));
  assert.equal(chip(''), '');
  assert.equal(chip('<b>x</b>'), '');
  assert.equal(vm.runInContext('window.automatonKindChipHtml(null)', sandbox), '');
});

test('loader keeps the last good data when a refresh fails and goes stale', async () => {
  const sandbox = loadApp();
  const calls = [];
  let mode = 'ok';
  sandbox.fetch = (url) => {
    calls.push(String(url));
    if (mode === 'fail') return Promise.resolve({ ok: false, status: 500, statusText: 'boom', text: () => Promise.resolve('boom'), json: () => Promise.resolve({}) });
    const body = String(url).includes('/trend') ? { days: 14, points: [], direction: 'flat' } : metrics();
    return Promise.resolve({ ok: true, status: 200, headers: { get: () => 'application/json' }, json: () => Promise.resolve(body), text: () => Promise.resolve(JSON.stringify(body)) });
  };
  await vm.runInContext('window.loadProgressCard(true)', sandbox);
  await flushAsync();
  const S = vm.runInContext('window._progressCardState', sandbox);
  assert.equal(S.status, 'ok', 'state after first load: ' + JSON.stringify({ s: S.status, e: S.error }));
  assert.ok(calls.some(u => u.includes('/api/autonomous/progress')), calls.join());
  assert.ok(calls.some(u => u.includes('/api/autonomous/progress/trend?days=14')), calls.join());
  assert.equal(S.trend.direction, 'flat');
  mode = 'fail';
  await vm.runInContext('window.loadProgressCard(true)', sandbox);
  await flushAsync();
  assert.equal(S.status, 'stale');
  assert.ok(S.data && S.data.totals, 'last good data kept');
  assert.ok(S.error);
});

test('loader reports failed when the first load fails, and recovers on retry', async () => {
  const sandbox = loadApp();
  let ok = false;
  sandbox.fetch = (url) => {
    if (!ok) return Promise.resolve({ ok: false, status: 503, statusText: 'down', text: () => Promise.resolve('down'), json: () => Promise.resolve({}) });
    const body = String(url).includes('/trend') ? { direction: 'converging' } : metrics();
    return Promise.resolve({ ok: true, status: 200, headers: { get: () => 'application/json' }, json: () => Promise.resolve(body), text: () => Promise.resolve(JSON.stringify(body)) });
  };
  await vm.runInContext('window.loadProgressCard(true)', sandbox);
  await flushAsync();
  const S = vm.runInContext('window._progressCardState', sandbox);
  assert.equal(S.status, 'failed');
  assert.equal(S.data, null);
  ok = true;
  await vm.runInContext('window.loadProgressCard(true)', sandbox);
  await flushAsync();
  assert.equal(S.status, 'ok');
  assert.equal(S.trend.direction, 'converging');
});

test('the loader adds no timer of its own', () => {
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  const a = src.indexOf('// ── Automata progress card');
  const b = src.indexOf('window._loadPRDActiveSessionCard = function');
  assert.ok(a > 0 && b > a);
  const block = src.slice(a, b);
  assert.ok(!/setInterval|setTimeout/.test(block), 'progress card code must not create timers');
});

test('paintProgressCard draws into the slot only while the Automation sub-tab shows', () => {
  const sandbox = loadApp();
  // A slot stand-in the app code can read state from and write into.
  const slot = { style: { display: 'block' }, innerHTML: 'stale markup' };
  sandbox.document.getElementById = id => (id === 'progressCard' ? slot : null);
  // The state machine has nothing loaded yet; either way paint has markup to draw.
  vm.runInContext("window._progressCardState.status = 'ok'; window._progressCardState.data = null;", sandbox);
  const paint = tab => vm.runInContext(`_automataState.tab = '${tab}'; window.paintProgressCard();`, sandbox);

  paint('automation');
  assert.equal(slot.style.display, 'block', 'the slot is visible on the Automation sub-tab');
  assert.match(slot.innerHTML, /progress-card/, 'the slot is drawn on the Automation sub-tab');

  slot.style.display = 'block'; slot.innerHTML = 'stale markup';
  paint('automata');
  assert.equal(slot.style.display, 'none', 'off the Automation sub-tab the slot is hidden');
  assert.equal(slot.innerHTML, 'stale markup', 'and never written when hidden');

  slot.style.display = 'block'; slot.innerHTML = 'stale markup';
  paint('templates');
  assert.equal(slot.style.display, 'none', 'hidden on the Templates sub-tab too');
  assert.equal(slot.innerHTML, 'stale markup', 'and not written either');
});

// ── locale coverage ──
function appKeys() {
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  const keys = new Set();
  for (const m of src.matchAll(/progress_card_[a-z0-9_]+/g)) keys.add(m[0]);
  return keys;
}

test('every progress_card_ key used in app.js exists in all five bundles', () => {
  const used = appKeys();
  // Every key is spelled out as a literal in app.js: a key built at run time
  // would slip past the coverage guard.
  assert.ok(!used.has('progress_card_') && ![...used].some(k => k.endsWith('_')), 'no key may be built from a prefix');
  assert.ok(used.size > 60, 'expected many keys, got ' + used.size);
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of used) assert.ok(typeof b[k] === 'string' && b[k].trim(), lang + ' missing ' + k);
  }
});

test('an API value that is not one of ours never reaches a locale lookup', () => {
  const evil = ['constructor', '__proto__', 'toString'];
  const data = metrics({ completion: Object.assign(metrics().completion, { drivers: evil, method: { constructor: 'x' } }) });
  const html = render({ status: 'ok', data, trend: { direction: 'constructor' } });
  assert.ok(html.includes('<li>constructor</li>'), 'unknown driver shows as escaped text');
  assert.ok(html.includes('>constructor<') || html.includes('constructor</span>'), 'unknown method label shows as text');
  assert.ok(!html.includes('function'), 'no function source leaked');
});

test('no progress_card_ value holds a tracker id or the internal term, and Automata forms are right', () => {
  const forms = { en: ['Automata', 'Automaton'], de: ['Automaten', 'Automat'], es: ['autómatas', 'autómata'], fr: ['automates', 'automate'], ja: ['オートマタ', 'オートマトン'] };
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const [k, v] of Object.entries(b)) {
      if (!k.startsWith('progress_card_')) continue;
      assert.ok(!/\bPRDs?\b/i.test(v), lang + ' ' + k + ' has the internal term: ' + v);
      assert.ok(!/\b(BL|B|F|S)[0-9]+\b/.test(v), lang + ' ' + k + ' has a tracker id: ' + v);
      assert.ok(!/Automation/i.test(v) && !/Automatisierung|automatización|automatisation|オートメーション/.test(v), lang + ' ' + k + ' names the chain object: ' + v);
      if (lang !== 'en') assert.ok(!/Automaton|Automata/.test(v), lang + ' ' + k + ' keeps the English word: ' + v);
    }
    const all = Object.entries(b).filter(([k]) => k.startsWith('progress_card_')).map(([, v]) => v).join('\n');
    assert.ok(all.includes(forms[lang][0]), lang + ' never uses the plural form');
  }
});

test('translations differ from English for the visible labels', () => {
  const en = bundle('en');
  for (const lang of LANGS.filter(l => l !== 'en')) {
    const b = bundle(lang);
    for (const k of ['progress_card_title', 'progress_card_retry', 'progress_card_not_measured', 'progress_card_failed']) {
      assert.notEqual(b[k], en[k], lang + ' ' + k + ' is untranslated');
    }
  }
});
