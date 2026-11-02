// Operator console cards (GET /api/dashboard/console) — the five default
// dashboard cards: one shared fetch, per-section rendering (available →
// number + line + link; unavailable → reason + settings link), default
// layout starts with them while dashboard.console.enabled is on, a stored
// layout is never rewritten (topped up once), the grid and the Add-Card
// picker drop them entirely when the feature is off, and only in-app "#"
// anchor links are ever rendered.
//
// Loads the real app.js into the shared Node vm stub browser (see
// testutil_browser_stub.js for the mechanics).

'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { buildSandbox, loadScript, flushAsync, vm, makeStubElement } = require('./testutil_browser_stub.js');

const APP_JS = path.join(__dirname, 'app.js');

const CONSOLE_IDS = [
  'console-questions',
  'console-held',
  'console-lanes',
  'console-usage',
  'console-ci-release',
];

const CONSOLE_PAYLOAD = {
  questions_waiting: { available: true,  count: 7, summary: '7 questions in 3 work items', link: '#questions' },
  held_blocked:      { available: true,  count: 2, summary: '1 held, 1 blocked',           link: '#held-questions' },
  lanes:             { available: false, count: 0, summary: '',                            reason: 'Lane fill is off', link: '#settings' },
  usage:             { available: true,  count: 0, summary: '21% of the daily pace line',  link: '#usage' },
  ci_release:        { available: true,  count: 2, summary: '2 repos · one red',           link: '#ci-release' },
};

// makeEnv boots app.js fresh and returns the sandbox plus one capturing
// stub element per console card id (the ids _dashRenderConsole writes to).
function makeEnv() {
  const byId = {};
  for (const id of CONSOLE_IDS) {
    byId['dashConsoleCard_' + id] = makeStubElement();
  }
  byId['dashCardGrid'] = makeStubElement();
  const sandbox = vm.createContext(buildSandbox({ byId }));
  loadScript(APP_JS, sandbox);
  return { sandbox, byId };
}

// evalIn runs code in the sandbox's top-level lexical scope (V8 shares the
// environment across runInContext calls — the same trick the other app-*.
// test.js files in this directory use).
function evalIn(sandbox, code) {
  const { vm } = require('./testutil_browser_stub.js');
  return vm.runInContext(code, sandbox);
}

test('all five console cards are defined, first, with labelKey + linkKey + body', () => {
  const { sandbox } = makeEnv();
  const defs = evalIn(sandbox, 'DASH_CARD_DEFS');
  assert.equal(defs.length >= 5, true);
  const head = defs.slice(0, 5);
  assert.equal(Array.from(head.map(d => d.id)).join(','), CONSOLE_IDS.join(','));
  for (const d of head) {
    assert.ok(d.label, 'label: ' + d.id);
    assert.ok(d.labelKey && d.labelKey.startsWith('dash_console_'), 'labelKey: ' + d.id);
    assert.equal(typeof d.body, 'function', 'body fn: ' + d.id);
  }
  const ids = evalIn(sandbox, '[...DASH_CONSOLE_IDS].sort()');
  assert.equal(ids.join(','), CONSOLE_IDS.slice().sort().join(','));
});

test('default layout: console cards FIRST, then the existing defaults', () => {
  const { sandbox } = makeEnv();
  assert.equal(evalIn(sandbox, '_dashConsoleEnabled()'), true, 'default enabled (unset switch reads on)');
  const layout = evalIn(sandbox, '_dashDefaultLayout().map(c => c.id)');
  assert.equal(layout.slice(0, 5).join(','), CONSOLE_IDS.join(','));
  assert.ok(layout.includes('tree'));       // existing default still present
  assert.ok(layout.includes('ekg'));
  assert.ok(layout.includes('orbital'));
  assert.equal(layout.length, evalIn(sandbox, 'DASH_DEFAULT_LAYOUT.length') + 5);
});

test('stored layout: kept as-is, missing console cards topped up, never rewritten when present', () => {
  const { sandbox } = makeEnv();
  const stored = evalIn(sandbox, 'DASH_DEFAULT_LAYOUT.slice(2).map(c => ({ id: c.id, cs: c.cs }))');
  const merged = evalIn(sandbox, '_dashLayoutWithConsoleCards(' + JSON.stringify(stored) + ').map(c => c.id)');
  assert.equal(merged.slice(0, 5).join(','), CONSOLE_IDS.join(','));
  assert.ok(merged.includes('events'), 'stored cards kept in place');
  // already present → untouched, same length, stored order preserved
  const defl = evalIn(sandbox, '_dashDefaultLayout()');
  const full = evalIn(sandbox, '_dashLayoutWithConsoleCards(_dashDefaultLayout()).map(c => c.id)');
  assert.equal(full.length, defl.length);
});

test('disabled: default layout, stored layout and grid drop all five cards', () => {
  const { sandbox, byId } = makeEnv();
  evalIn(sandbox, 'state._dashConsoleEnabled = false;');
  assert.equal(evalIn(sandbox, '_dashConsoleEnabled()'), false);
  assert.ok(!evalIn(sandbox, '_dashDefaultLayout().map(c => c.id)').some(id => CONSOLE_IDS.includes(id)));
  // stored layout still carrying the cards → grid renders none of them
  const stored = evalIn(sandbox, '_dashDefaultLayout().slice(0, 5).concat([{ id: "tree", cs: 2, rs: 2 }])');
  evalIn(sandbox, '_dashBuildGrid(' + JSON.stringify(stored) + ');');
  for (const id of CONSOLE_IDS) {
    assert.ok(!byId['dashConsoleCard_' + id].innerHTML.includes('data-card-id="' + id + '"'));
  }
  const grid = byId['dashCardGrid'];
  assert.ok(grid.innerHTML.includes('data-card-id="tree"'), 'non-console cards still render');
  assert.ok(!grid.innerHTML.includes('console-'));
});

test('enabled again: cards render from the cached payload; fetch goes to /api/dashboard/console once', async (t) => {
  const { sandbox, byId } = makeEnv();
  const calls = [];
  sandbox.fetch = (url, opts) => {
    calls.push(url);
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(CONSOLE_PAYLOAD) });
  };
  await flushAsync();
  evalIn(sandbox, '_dashFetchConsole();');
  await flushAsync(20);
  assert.deepEqual(calls.filter(u => u === '/api/dashboard/console'), ['/api/dashboard/console']);
  assert.equal(evalIn(sandbox, '_dash._console.questions_waiting.count'), 7);
  assert.equal(evalIn(sandbox, '_dash._consoleFetching'), false);

  // available section: one number, one line, one acting link
  const q = byId['dashConsoleCard_console-questions'].innerHTML;
  assert.ok(q.includes('7') && q.includes('7 questions in 3 work items'));
  assert.ok(q.includes('href="#questions"'));
  // unavailable section: its reason + the settings link the daemon pointed at
  const lanes = byId['dashConsoleCard_console-lanes'].innerHTML;
  assert.ok(lanes.includes('Lane fill is off') && lanes.includes('href="#settings"'));
});

test('loading state and safe-href hardening', () => {
  const { sandbox, byId } = makeEnv();
  // no payload yet → loading line (locale key), not raw markup
  evalIn(sandbox, '_dashRenderConsole();');
  const q = byId['dashConsoleCard_console-questions'].innerHTML;
  assert.ok(!q.includes('<script'));
  // safe hrefs pass anchors through and reject everything else
  const ok = evalIn(sandbox, '[_dashConsoleSafeHref("#questions"), _dashConsoleSafeHref("#settings")].join("|")');
  assert.equal(ok, '#questions|#settings');
  assert.equal(evalIn(sandbox, '_dashConsoleSafeHref("https://x.example")'), '#');
  assert.equal(evalIn(sandbox, '_dashConsoleSafeHref("javascript:alert(1)")'), '#');
  assert.equal(evalIn(sandbox, '_dashConsoleSafeHref(null)'), '#');
});

test('every new t() key has a value in all five locale bundles', () => {
  const keys = new Set();
  const re = /['"](dash_console_[a-z_]+)['"]/g;
  const src = fs.readFileSync(APP_JS, 'utf8');
  let m;
  while ((m = re.exec(src)) !== null) keys.add(m[1]);
  const loading = /['"](dash_console_loading)['"]/;
  assert.ok(loading.test(src));
  const files = ['en', 'de', 'es', 'fr', 'ja'];
  for (const loc of files) {
    const bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', loc + '.json'), 'utf8'));
    for (const k of keys) {
      assert.ok(bundle[k] && String(bundle[k]).length > 0, `${loc}/${k} missing or empty`);
    }
  }
});
