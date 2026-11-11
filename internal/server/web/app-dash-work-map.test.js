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

// ── Network card (dashboard card id 'orbital') — the work map ───────────────
// The Network card replaced the orbital session map. It draws the same
// work-map payload as the 'work-map' card, as a fixed-box column layout:
// projects collapsed by default, a click expands one, a rotate control steps
// through the running sessions on window._dashWorkMapInterval, and a session
// click opens its detail. Covered here: the regression that the old
// constellation is gone, the collapsed / expanded / focused visibility, the
// layout staying inside its box, the overflow count, the colours by compute
// node and lane, the clicks, the rotation (one timer, stops when there is no
// running work), the feature gate and grid drop, the hide-inactive off body,
// and the three new locale keys in all five locales.

const NET_KEYS = ['dash_network_purpose', 'dash_network_next', 'dash_network_none_running'];

// netPayload: two projects. alpha: an Automation, an Automaton, a task and
// three sessions on two compute nodes and two lanes; beta: one session with no
// Automation; s5 is a root session with no project. Running: s1, s2, s4.
function netPayload(over) {
  return Object.assign({
    generated_at: '2026-11-10T12:00:00Z',
    trace: true,
    counts: {},
    nodes: [
      { id: 'project:alpha', kind: 'project', label: 'alpha', status: 'running' },
      { id: 'automation:amA', kind: 'automation', label: 'amA', status: 'running', parent: 'project:alpha' },
      { id: 'autA', kind: 'automaton', label: 'autA', status: 'running', parent: 'automation:amA' },
      { id: 'tA', kind: 'task', label: 'tA', status: 'ok', parent: 'autA' },
      { id: 's1', kind: 'session', label: 'build one', status: 'running', parent: 'autA', node: 'node1', lane: 'gpu', role: 'worker' },
      { id: 's2', kind: 'session', label: 'build two', status: 'running', parent: 'autA', node: 'node2', lane: 'cpu', role: 'worker' },
      { id: 's3', kind: 'session', label: 'done one', status: 'done', parent: 'autA', node: 'node1', lane: 'gpu', role: 'worker' },
      { id: 'project:beta', kind: 'project', label: 'beta', status: 'running' },
      { id: 's4', kind: 'session', label: 'beta work', status: 'running', parent: 'project:beta', node: 'node2', lane: 'cpu', role: 'worker' },
      { id: 's5', kind: 'session', label: 'root one', status: 'waiting', parent: '' },
    ],
    edges: [
      { from: 'project:alpha', to: 'automation:amA' },
      { from: 'automation:amA', to: 'autA' },
      { from: 'autA', to: 'tA' },
      { from: 'tA', to: 's1' },
      { from: 'autA', to: 's1' },
      { from: 'autA', to: 's2' },
      { from: 'autA', to: 's3' },
      { from: 'project:beta', to: 's4' },
    ],
    running: [
      { id: 's1', label: 'build one', state: 'running', node: 'node1', lane: 'gpu' },
      { id: 's2', label: 'build two', state: 'running', node: 'node2', lane: 'cpu' },
      { id: 's4', label: 'beta work', state: 'running', node: 'node2', lane: 'cpu' },
    ],
  }, over || {});
}

// netEnv: the real app.js with the Network card's own elements, the EN bundle,
// a recording setInterval / clearInterval, and a recording navigate.
function netEnv(payload) {
  const byId = {};
  byId['dashNetworkSvg'] = makeStubElement();
  byId['dashNetworkCount'] = makeStubElement();
  byId['dashCardGrid'] = makeStubElement();
  byId['dashWorkMapCard'] = makeStubElement();
  const sandbox = vm.createContext(buildSandbox({ byId }));
  loadScript(APP_JS, sandbox);
  sandbox.__en = EN;
  vm.runInContext('window._i18n = { bundle: __en, fallback: __en };', sandbox);
  sandbox.__timers = [];
  sandbox.__cleared = [];
  sandbox.setInterval = (fn, ms) => { sandbox.__timers.push({ fn, ms }); return 7; };
  sandbox.clearInterval = (id) => { sandbox.__cleared.push(id); };
  sandbox.__navs = [];
  sandbox.navigate = (...a) => { sandbox.__navs.push(a); };
  if (payload !== undefined) setPayload(sandbox, payload);
  return { sandbox, byId };
}

test('regression: the constellation simulation and its element are gone', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.ok(!src.includes('constellationSvg'), 'no constellationSvg reference left');
  assert.ok(!src.includes('_drawConstellation'), '_drawConstellation removed');
  assert.ok(!src.includes('_dashForceStep'), '_dashForceStep removed');
  assert.ok(src.includes("id: 'orbital'"), 'the orbital card id is kept for stored layouts');
});

test('orbital card body: one purpose line that does not wrap, the count, the rotate control and the map box', () => {
  const { sandbox } = netEnv();
  const body = evalIn(sandbox, "DASH_CARD_DEFS.find(d => d.id === 'orbital').body()");
  assert.ok(body.includes('id="dashNetworkWorkMap"'), 'card wrapper');
  assert.ok(body.includes('white-space:nowrap'), 'purpose stays on one line');
  assert.ok(body.includes('Work in progress: projects, Automations, Automata, tasks and sessions.'), 'purpose text from the EN bundle');
  assert.ok(body.includes('id="dashNetworkCount"'), 'position count');
  assert.ok(body.includes('onclick="window._dashNetworkStep(1)"'), 'rotate control');
  assert.ok(body.includes('id="dashNetworkSvg"'), 'map svg');
  assert.ok(body.includes('overflow:hidden'), 'bounded box');
});

test('collapsed: projects and root work only; a project expands to its subtree', () => {
  const { sandbox } = netEnv(netPayload());
  sandbox.__exp = new Set();
  const ids = () => evalIn(sandbox, '_dashNetworkVisible(_dashNetworkIndex(_dash._workMap), __exp, null).map(n => n.id)');
  assert.deepEqual([...ids()].sort(), ['project:alpha', 'project:beta', 's5'], 'collapsed view');
  sandbox.__exp = new Set(['project:alpha']);
  const expanded = ids();
  for (const id of ['automation:amA', 'autA', 'tA', 's1', 's2', 's3']) assert.ok(expanded.includes(id), id + ' under expanded alpha');
  assert.ok(!expanded.includes('s4'), 'beta stays collapsed');
});

test('the rotation focus shows its own project even when it is not expanded', () => {
  const { sandbox } = netEnv(netPayload());
  sandbox.__exp = new Set();
  const ids = evalIn(sandbox, '_dashNetworkVisible(_dashNetworkIndex(_dash._workMap), __exp, "project:alpha").map(n => n.id)');
  assert.ok(ids.includes('s1') && ids.includes('s2'), 'focused project alpha is drawn open');
  assert.ok(!ids.includes('s4'), 'beta is still collapsed');
});

test('draw: one group per visible node, the collapsed mark on projects, the edges between visible nodes', () => {
  // Nothing running, so no rotation focus opens a project: the view is collapsed.
  const { sandbox, byId } = netEnv(netPayload({ running: [] }));
  evalIn(sandbox, '_drawNetworkWorkMap();');
  const html = byId['dashNetworkSvg'].innerHTML;
  assert.equal((html.match(/data-wm-id="/g) || []).length, 3, 'three visible groups when collapsed');
  assert.ok(html.includes('data-wm-id="project:alpha"'));
  assert.ok(html.includes('▸ 1'), 'collapsed alpha shows its child count');
  assert.ok(!html.includes('data-wm-id="s1"'), 'session hidden while collapsed');
  assert.equal(evalIn(sandbox, '_dashNetworkVisible.length'), 3, 'visible helper is a function of three args');
});

test('click a project toggles it open and closed; click a session opens its detail', () => {
  const { sandbox, byId } = netEnv(netPayload());
  evalIn(sandbox, '_drawNetworkWorkMap();');
  evalIn(sandbox, "_dashNetworkClick('project:alpha');");
  assert.ok(evalIn(sandbox, "_dash._netExpanded.has('project:alpha')"), 'expanded after click');
  assert.ok(byId['dashNetworkSvg'].innerHTML.includes('data-wm-id="s1"'), 'session drawn once expanded');
  assert.ok(byId['dashNetworkSvg'].innerHTML.includes('▾'), 'open mark');
  evalIn(sandbox, "_dashNetworkClick('project:alpha');");
  assert.ok(!evalIn(sandbox, "_dash._netExpanded.has('project:alpha')"), 'collapsed again');
  evalIn(sandbox, "_dashNetworkClick('s2');");
  assert.deepEqual(sandbox.__navs.map(a => [a[0], a[1]]), [['session-detail', 's2']], 'session opens its detail');
  evalIn(sandbox, "_dashNetworkClick('does-not-exist');");
  assert.equal(sandbox.__navs.length, 1, 'an unknown id does nothing');
});

test('clicks reach the handler through the delegated attribute on the svg', () => {
  const { sandbox, byId } = netEnv(netPayload());
  evalIn(sandbox, '_drawNetworkWorkMap();');
  const svg = byId['dashNetworkSvg'];
  assert.equal(typeof svg.onclick, 'function', 'one delegated handler on the svg');
  svg.onclick({ target: { closest: () => ({ getAttribute: () => 's1' }) } });
  assert.deepEqual(sandbox.__navs.map(a => a[1]), ['s1'], 'delegated click opens the session');
  svg.onclick({ target: { closest: () => null } });
  assert.equal(sandbox.__navs.length, 1, 'a click off any node does nothing');
});

test('colours: one fill per compute node, a different outline per lane, an error outline stays red', () => {
  const a = evalIn(sandbox0(), "_dashNetworkColors('node1', 'gpu', 'running')");
  const b = evalIn(sandbox0(), "_dashNetworkColors('node1', 'cpu', 'running')");
  const c = evalIn(sandbox0(), "_dashNetworkColors('node2', 'gpu', 'running')");
  assert.equal(a.fill, b.fill, 'same compute node, same fill');
  assert.notEqual(a.stroke, b.stroke, 'different lane, different outline');
  assert.notEqual(a.fill, c.fill, 'different compute node, different fill');
  assert.equal(evalIn(sandbox0(), "_dashNetworkColors('node1', 'gpu', 'error').stroke"), '#ef4444', 'error stays red');
  assert.equal(evalIn(sandbox0(), "_dashNetworkColors('', '', 'waiting').fill"), '#64748b', 'no node is neutral');
});

// sandbox0: a fresh app.js sandbox for the pure helpers (no payload needed).
function sandbox0() { return netEnv().sandbox; }

test('the layout stays inside its box; a crowded column shows a count for the rest', () => {
  const tasks = [];
  for (let i = 0; i < 12; i++) tasks.push({ id: 'tk' + i, kind: 'task', label: 'tk' + i, status: 'ok', parent: 'autA' });
  const { sandbox } = netEnv(netPayload({
    nodes: [
      { id: 'project:alpha', kind: 'project', label: 'alpha', status: 'ok' },
      { id: 'automation:amA', kind: 'automation', label: 'amA', status: 'ok', parent: 'project:alpha' },
      { id: 'autA', kind: 'automaton', label: 'autA', status: 'ok', parent: 'automation:amA' },
    ].concat(tasks),
    edges: [], running: [],
  }));
  sandbox.__exp = new Set(['project:alpha']);
  const lay = evalIn(sandbox, '(function(){ const v = _dashNetworkVisible(_dashNetworkIndex(_dash._workMap), __exp, null); return _dashNetworkLayout(v, 320, 120); })()');
  for (const p of lay.placed) {
    assert.ok(p.x - lay.nodeW / 2 >= 0 && p.x + lay.nodeW / 2 <= 320, 'x inside the box: ' + p.n.id);
    assert.ok(p.y - lay.nodeH / 2 >= 0 && p.y + lay.nodeH / 2 <= 120, 'y inside the box: ' + p.n.id);
  }
  const taskMore = lay.more.find(m => m.count > 0);
  assert.ok(taskMore, 'a column that overflows reports a count');
  assert.equal(lay.placed.filter(p => p.n.kind === 'task').length + taskMore.count, 12, 'every task is either drawn or counted');
});

test('rotation: next steps through the running sessions, the count tracks the focus, the focused path is highlighted', () => {
  const { sandbox, byId } = netEnv(netPayload());
  evalIn(sandbox, '_drawNetworkWorkMap();');
  assert.equal(byId['dashNetworkCount'].innerHTML, '1/3', 'starts on the first running session');
  evalIn(sandbox, '_dashNetworkStep(1);');
  assert.equal(byId['dashNetworkCount'].innerHTML, '2/3');
  evalIn(sandbox, '_dashNetworkStep(1);');
  evalIn(sandbox, '_dashNetworkStep(1);');
  assert.equal(byId['dashNetworkCount'].innerHTML, '1/3', 'wraps around');
  evalIn(sandbox, '_dashNetworkStep(-1);');
  assert.equal(byId['dashNetworkCount'].innerHTML, '3/3', 'steps back');
  // Focus on s4 (beta): beta is drawn open and its node is outlined thick.
  assert.ok(byId['dashNetworkSvg'].innerHTML.includes('data-wm-id="s4"'), 'focused session drawn');
  assert.ok(byId['dashNetworkSvg'].innerHTML.includes('stroke-width="2.5"'), 'focused path highlighted');
});

test('one rotation timer on window._dashWorkMapInterval: started once, stepped by it, cleared when the work ends', () => {
  const { sandbox, byId } = netEnv(netPayload());
  evalIn(sandbox, '_drawNetworkWorkMap();');
  evalIn(sandbox, '_drawNetworkWorkMap();');
  assert.equal(sandbox.__timers.length, 1, 'never stacked: one timer across repeated draws');
  assert.equal(sandbox.__timers[0].ms, 6000, 'a fixed six-second interval');
  assert.equal(evalIn(sandbox, 'window._dashWorkMapInterval'), 7, 'kept on window._dashWorkMapInterval');
  sandbox.__timers[0].fn();
  assert.equal(byId['dashNetworkCount'].innerHTML, '2/3', 'the timer steps the focus');
  setPayload(sandbox, netPayload({ running: [] }));
  evalIn(sandbox, '_drawNetworkWorkMap();');
  assert.deepEqual(sandbox.__cleared, [7], 'cleared once the running work is gone');
  assert.equal(evalIn(sandbox, 'window._dashWorkMapInterval'), null, 'reset to null');
  assert.ok(byId['dashNetworkCount'].innerHTML.includes('nothing running'), 'the empty count reads nothing running');
});

test('the rotation timer clears itself when the Network card leaves the page', () => {
  const { sandbox, byId } = netEnv(netPayload());
  evalIn(sandbox, '_drawNetworkWorkMap();');
  assert.equal(sandbox.__timers.length, 1);
  // Card gone from the page: the next tick finds no svg and stops.
  const card = byId['dashNetworkSvg'];
  sandbox.document.getElementById = (id) => (id === 'dashNetworkSvg' ? null : byId[id] || null);
  sandbox.__timers[0].fn();
  assert.deepEqual(sandbox.__cleared, [7], 'timer cleared without the card');
  assert.ok(card, 'the element was there before');
});

test('the feature gate: orbital is off with no payload or no nodes, on once nodes exist', () => {
  const { sandbox } = netEnv();
  assert.equal(evalIn(sandbox, "DASH_CARD_FEATURES['orbital'].active()"), false, 'no payload');
  setPayload(sandbox, netPayload({ nodes: [], edges: [], running: [] }));
  assert.equal(evalIn(sandbox, "DASH_CARD_FEATURES['orbital'].active()"), false, 'empty map');
  setPayload(sandbox, netPayload());
  assert.equal(evalIn(sandbox, "DASH_CARD_FEATURES['orbital'].active()"), true, 'nodes present');
});

test('the grid drops the Network card while it has no work, and shows it once it has', () => {
  const { sandbox, byId } = netEnv();
  sandbox.__layout = [{ id: 'orbital', cs: 6, rs: 2 }, { id: 'tree', cs: 2 }];
  vm.runInContext('_dashBuildGrid(__layout)', sandbox);
  assert.ok(!byId['dashCardGrid'].innerHTML.includes('dashNetworkWorkMap'), 'hidden with no work');
  setPayload(sandbox, netPayload());
  vm.runInContext('_dashBuildGrid(__layout)', sandbox);
  assert.ok(byId['dashCardGrid'].innerHTML.includes('dashNetworkWorkMap'), 'shown with work');
});

test('with hide_inactive_cards off, the empty Network card shows its one-line reason, not a map', () => {
  const { sandbox, byId } = netEnv();
  vm.runInContext('Object.assign(state, { _dashHideInactiveCards: false });', sandbox);
  sandbox.__layout = [{ id: 'orbital', cs: 6, rs: 2 }];
  vm.runInContext('_dashBuildGrid(__layout)', sandbox);
  const html = byId['dashCardGrid'].innerHTML;
  assert.ok(html.includes('No live sessions or Automaton tasks yet'), 'one-line reason');
  assert.ok(html.includes('Dashboard card reference'), 'link to the card reference');
  assert.ok(!html.includes('dashNetworkSvg'), 'no empty map drawn');
});

test('the Network card strings exist in all five locales, non-empty', () => {
  for (const loc of LOCALES) {
    const bundle = JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, loc + '.json'), 'utf8'));
    for (const key of NET_KEYS) {
      assert.equal(typeof bundle[key], 'string', loc + ' has ' + key);
      assert.ok(bundle[key].length > 0, loc + ' ' + key + ' is non-empty');
    }
  }
});
