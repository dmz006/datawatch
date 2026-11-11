// Dashboard work map card (id 'work-map').
//
// The card renders the GET /api/dashboard/work-map graph as an SVG with one
// column per node kind (project → Automation → Automaton → task → session),
// a "Running now" strip from the server's running list, and a one-line note
// when the trace store did not contribute. It is hidden through its
// DASH_CARD_FEATURES active() check (nodes present) while the map is empty
// or unavailable, and while hidden it shows the one-line reason with a link
// to the dashboard card reference. Covered here: the node and running render,
// label escaping, the empty / unavailable / placement-only states, the active
// gate and the grid drop, the docs link key, that a failed fetch never
// retries from the render path, and the strings in all five locales.

'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { buildSandbox, loadScript, vm, makeStubElement } = require('./testutil_browser_stub.js');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALE_DIR = path.join(__dirname, 'locales');
const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const EN = JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, 'en.json'), 'utf8'));

const NEW_KEYS = [
  'dash_work_map', 'dash_work_map_unavailable', 'dash_work_map_empty', 'dash_work_map_running',
  'dash_work_map_no_trace', 'dash_work_map_counts', 'dash_card_off_work_map_reason',
  'dash_card_off_work_map_link',
];

function makeEnv(opts) {
  const byId = {};
  byId['dashWorkMapCard'] = makeStubElement();
  byId['dashCardGrid'] = makeStubElement();
  const sandbox = vm.createContext(buildSandbox({ byId }));
  loadScript(APP_JS, sandbox);
  sandbox.__en = EN;
  vm.runInContext('window._i18n = { bundle: __en, fallback: __en };', sandbox);
  if (opts && opts.apiFetch) sandbox.apiFetch = opts.apiFetch;
  return { sandbox, byId };
}

function evalIn(sandbox, code) {
  return vm.runInContext(code, sandbox);
}

function setPayload(sandbox, payload) {
  sandbox.__wm = payload;
  vm.runInContext('_dash._workMap = __wm;', sandbox);
}

// samplePayload is a small but complete response: a project → Automation →
// Automaton → task → session chain, plus one running session.
function samplePayload(over) {
  return Object.assign({
    generated_at: '2026-11-10T12:00:00Z',
    trace: true,
    counts: { project: 1, automation: 1, automaton: 1, task: 1, session: 1 },
    nodes: [
      { id: 'project:proj1', kind: 'project', label: 'proj1', status: 'running' },
      { id: 'automation:ama1', kind: 'automation', label: 'ama1', status: 'running', parent: 'project:proj1' },
      { id: 'aut1', kind: 'automaton', label: 'aut1', status: 'running', parent: 'automation:ama1', events: 4, errors: 0 },
      { id: 'taskA', kind: 'task', label: 'taskA', status: 'ok', parent: 'aut1', events: 4, errors: 0 },
      { id: 'sess1', kind: 'session', label: 'build thing', status: 'running', parent: 'aut1', node: 'node1', lane: 'gpu', role: 'worker' },
    ],
    edges: [
      { from: 'project:proj1', to: 'automation:ama1' },
      { from: 'automation:ama1', to: 'aut1' },
      { from: 'aut1', to: 'taskA' },
      { from: 'taskA', to: 'sess1' },
      { from: 'aut1', to: 'sess1' },
    ],
    running: [
      { id: 'sess1', label: 'build thing', state: 'running', node: 'node1', lane: 'gpu', role: 'worker', updated_at: '2026-11-10T11:59:00Z' },
    ],
  }, over || {});
}

test('renders one SVG node per graph node, the edges and the running strip', () => {
  const { sandbox, byId } = makeEnv();
  setPayload(sandbox, samplePayload());
  evalIn(sandbox, '_dashRenderWorkMap();');
  const html = byId['dashWorkMapCard'].innerHTML;
  assert.ok(html.includes('<svg'), 'svg rendered');
  for (const label of ['proj1', 'ama1', 'aut1', 'taskA', 'build thing']) {
    assert.ok(html.includes('>' + label + '</text>'), 'node label ' + label);
  }
  assert.equal((html.match(/<path /g) || []).length, 5, 'one curve per edge');
  assert.ok(html.includes('Running now · 1'), 'running strip header with count');
  assert.ok(html.includes('node1 · gpu · worker'), 'running entry carries its placement');
  assert.ok(html.includes('nodes: 5'), 'node count line');
  assert.ok(!html.includes('placement only'), 'no placement-only note when the trace contributed');
});

test('node status drives the outline colour; unknown statuses fall back to grey', () => {
  const { sandbox } = makeEnv();
  assert.equal(evalIn(sandbox, "_dashWorkMapStatusColor('error')"), '#ef4444');
  assert.equal(evalIn(sandbox, "_dashWorkMapStatusColor('running')"), '#3b82f6');
  assert.equal(evalIn(sandbox, "_dashWorkMapStatusColor('waiting')"), '#f59e0b');
  assert.equal(evalIn(sandbox, "_dashWorkMapStatusColor('rate_limited')"), '#64748b');
});

test('labels are escaped, never injected as markup', () => {
  const { sandbox, byId } = makeEnv();
  setPayload(sandbox, samplePayload({
    nodes: [{ id: 'sessX', kind: 'session', label: '<img src=x onerror=1>', status: 'done' }],
    edges: [],
    running: [],
  }));
  evalIn(sandbox, '_dashRenderWorkMap();');
  const html = byId['dashWorkMapCard'].innerHTML;
  assert.ok(!html.includes('<img'), 'label not injected');
  assert.ok(html.includes('&lt;img'), 'label escaped');
});

test('empty map shows the empty line and no SVG', () => {
  const { sandbox, byId } = makeEnv();
  setPayload(sandbox, samplePayload({ nodes: [], edges: [], running: [] }));
  evalIn(sandbox, '_dashRenderWorkMap();');
  const html = byId['dashWorkMapCard'].innerHTML;
  assert.ok(html.includes('No work mapped yet'), 'empty line');
  assert.ok(!html.includes('<svg'), 'no svg');
});

test('placement-only payload (trace:false) carries the no-trace note', () => {
  const { sandbox, byId } = makeEnv();
  setPayload(sandbox, samplePayload({ trace: false }));
  evalIn(sandbox, '_dashRenderWorkMap();');
  assert.ok(byId['dashWorkMapCard'].innerHTML.includes('placement only (no trace store)'));
});

test('no payload yet shows the unavailable line and does not refetch from the render path', () => {
  let fetches = 0;
  const { sandbox, byId } = makeEnv({
    apiFetch: () => { fetches++; return Promise.reject(new Error('503')); },
  });
  setPayload(sandbox, null);
  evalIn(sandbox, '_dashRenderWorkMap();');
  evalIn(sandbox, '_dashRenderWorkMap();');
  evalIn(sandbox, '_dashRenderWorkMap();');
  assert.ok(byId['dashWorkMapCard'].innerHTML.includes('Work map unavailable'));
  assert.equal(fetches, 0, 'render path never fetches; the 30s tick is the retry');
});

test('a failed fetch keeps the last good payload', async () => {
  const { sandbox } = makeEnv({
    apiFetch: () => Promise.reject(new Error('boom')),
  });
  setPayload(sandbox, samplePayload());
  await evalIn(sandbox, '_dashFetchWorkMap()');
  assert.ok(evalIn(sandbox, '_dash._workMap') !== null, 'last good payload kept');
});

test('DASH_CARD_FEATURES work-map active(): off with no payload or no nodes, on once nodes exist', () => {
  const { sandbox } = makeEnv();
  assert.equal(evalIn(sandbox, "DASH_CARD_FEATURES['work-map'].active()"), false, 'no payload');
  setPayload(sandbox, samplePayload({ nodes: [], edges: [], running: [] }));
  assert.equal(evalIn(sandbox, "DASH_CARD_FEATURES['work-map'].active()"), false, 'empty map');
  setPayload(sandbox, samplePayload());
  assert.equal(evalIn(sandbox, "DASH_CARD_FEATURES['work-map'].active()"), true, 'nodes present');
});

test('grid drops the card while inactive and shows it once nodes are mapped', () => {
  const { sandbox, byId } = makeEnv();
  sandbox.__layout = [{ id: 'work-map', cs: 6, rs: 1 }, { id: 'tree', cs: 2 }];
  vm.runInContext('_dashBuildGrid(__layout)', sandbox);
  assert.ok(!byId['dashCardGrid'].innerHTML.includes('dashWorkMapCard'), 'hidden while inactive');
  setPayload(sandbox, samplePayload());
  vm.runInContext('_dashBuildGrid(__layout)', sandbox);
  assert.ok(byId['dashCardGrid'].innerHTML.includes('dashWorkMapCard'), 'shown once active');
});

test('off body names the reason and links the dashboard card reference', () => {
  const { sandbox } = makeEnv();
  const html = evalIn(sandbox, "_dashCardOffBody(DASH_CARD_FEATURES['work-map'])");
  assert.ok(html.includes('No live sessions or Automaton tasks yet'), 'one-line reason');
  assert.ok(html.includes('Dashboard card reference'), 'link text');
  assert.ok(html.includes('href="/diagrams.html#docs/datawatch-definitions.md%23dashboard"'),
    'docs link resolves to the dashboard section');
});

test('the docs key for the card has a DOCS_LINKS_BY_KEY entry', () => {
  const { sandbox } = makeEnv();
  assert.equal(evalIn(sandbox, "DOCS_LINKS_BY_KEY['dash_card_work_map']"), 'datawatch-definitions.md#dashboard');
});

test('the card definition is registered with its label key and default width', () => {
  const { sandbox } = makeEnv();
  const def = evalIn(sandbox, "DASH_CARD_DEFS.find(d => d.id === 'work-map')");
  assert.ok(def, 'work-map def present');
  assert.equal(def.labelKey, 'dash_work_map');
  assert.equal(def.defaultCs, 6);
});

test('every new work-map string exists in all five locales, non-empty', () => {
  for (const loc of LOCALES) {
    const bundle = JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, loc + '.json'), 'utf8'));
    for (const key of NEW_KEYS) {
      assert.equal(typeof bundle[key], 'string', loc + ' has ' + key);
      assert.ok(bundle[key].length > 0, loc + ' ' + key + ' is non-empty');
    }
  }
});
