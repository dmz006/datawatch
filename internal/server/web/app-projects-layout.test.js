// Projects page layout — Gantt size cap, section order, collapsible section
// cards with per-section memory, and the absence of hide/reorder controls.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP = path.join(__dirname, 'app.js');
const DAY = 86400000;
const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];

// memStorage is a localStorage stand-in that keeps values between calls.
function memStorage() {
  const m = new Map();
  return {
    getItem(k) { return m.has(k) ? m.get(k) : null; },
    setItem(k, v) { m.set(k, String(v)); },
    removeItem(k) { m.delete(k); },
    keys() { return Array.from(m.keys()); },
  };
}
const throwingStorage = {
  getItem() { throw new Error('blocked'); },
  setItem() { throw new Error('blocked'); },
  removeItem() { throw new Error('blocked'); },
};

function load({ storage, showEmpty } = {}) {
  const out = { view: '' };
  const view = makeStubElement(h => { out.view = h; });
  view.insertAdjacentHTML = (pos, h) => { out.view = h + out.view; };
  const sandbox = buildSandbox({ byId: { automataPanel: view, view } });
  if (storage) sandbox.localStorage = storage;
  vm.createContext(sandbox);
  loadScript(APP, sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  vm.runInContext("state.activeView = 'autonomous'; _automataState.tab = 'projects'", sandbox);
  if (showEmpty !== undefined) vm.runInContext(`_projectsShowEmpty = function () { return ${showEmpty}; }`, sandbox);
  return { sb: sandbox, out, view };
}

const FULL = { name: 'acme', dir: '/work/acme', owner: 'dana', weekly_share: 40 };
const ROLLUP = {
  arcs: [{ automation_id: 'auto-a', priority: 1, state: 'active', tasks: { done: 1, total: 2 }, automata: [
    { id: 'prd-1', title: 'Login', status: 'running', session_ids: ['sess-1'] }] }],
  unassigned: { automata: [{ id: 'loose-1', title: 'Loose', session_ids: ['s-9'] }] },
};
const ROLLUP_NO_UNASSIGNED = { arcs: ROLLUP.arcs, unassigned: { automata: [] } };

function iso(ms) { return new Date(ms).toISOString(); }
function hist() {
  const now = Date.now();
  return {
    name: 'acme', next: '', error: '',
    items: [{ at: iso(now - DAY), kind: 'automaton_completed', automaton_id: 'prd-1' }],
    gantt: [
      { kind: 'automaton', automaton_id: 'prd-1', automation_id: 'auto-a', title: 'Login', state: 'completed', start: iso(now - 5 * DAY), end: iso(now - 3 * DAY) },
      { kind: 'automaton', automaton_id: 'prd-2', automation_id: 'auto-a', title: 'Cart', state: 'running', start: iso(now - 2 * DAY), end: null },
    ],
  };
}
function page(sb, roll = ROLLUP) { return sb._projectPageHTML(FULL, roll, '', hist()); }

// seedGantt gives the selected project a Gantt state with two arc rows, as if
// the timeline's first page had arrived.
function seedGantt(sb) {
  const now = Date.now();
  const rows = [
    { kind: 'arc', id: 'auto-a', title: 'Login', state: 'done', first: iso(now - 5 * DAY), last: iso(now - 3 * DAY), running: false, has_children: true, children_count: 1 },
    { kind: 'arc', id: 'auto-b', title: 'Cart', state: 'active', first: iso(now - 2 * DAY), last: null, running: true, has_children: true, children_count: 1 },
  ];
  vm.runInContext(`_projectsState.selected = 'acme'; _projectsState.gantt = { name: 'acme', rows: ${JSON.stringify(rows)}, next: '', error: '', loading: false, kids: {}, preset: 'all', view: null, expanded: {} }`, sb);
}

function chartOf(html) {
  const i = html.indexOf('<div class="project-gantt-scroll">');
  assert.ok(i >= 0, 'the Gantt chart is drawn');
  const j = html.indexOf('id="projectSection-', i);
  return html.slice(i, j < 0 ? undefined : j);
}

// ---- size cap ----

test('the Gantt chart is capped by a class rule and carries no viewport-relative unit', () => {
  const { sb } = load();
  seedGantt(sb);
  const chart = chartOf(page(sb));
  assert.ok((chart.match(/class="project-gantt-bar/g) || []).length >= 2, 'at least two bars');
  assert.match(chart, /^<div class="project-gantt-scroll">/);
  const css = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8');
  const rule = /\.project-gantt-chart\s*\{([^}]*)\}/.exec(css);
  assert.ok(rule, 'a .project-gantt-chart rule exists in style.css');
  const mw = /max-width:\s*(\d+)px/.exec(rule[1]);
  assert.ok(mw && Number(mw[1]) === 720, 'the rule sets the 720px max-width');
  assert.ok(!/\b\d*\.?\d+\s*(vw|vh|vmin|vmax)\b/.test(rule[1]), 'the rule has no viewport-relative unit');
  assert.ok(!/\b\d*\.?\d+\s*(vw|vh|vmin|vmax)\b/.test(chart), 'the chart markup has no viewport-relative unit');
  const section = page(sb).match(/id="projectGantt"[\s\S]*?project-gantt-chart/)[0];
  assert.ok(!/\b\d*\.?\d+\s*(vw|vh|vmin|vmax)\b/.test(section), 'the Gantt block has no viewport-relative unit');
});

test('Gantt text sizes are fixed pixels in style.css and the labels are sticky', () => {
  const css = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8');
  for (const cls of ['project-gantt-label', 'project-gantt-tick', 'project-gantt-now', 'project-gantt-planned']) {
    const rule = new RegExp('\\.' + cls + '\\s*\\{([^}]*)\\}').exec(css);
    assert.ok(rule, cls + ' has a rule');
    const fs1 = /font-size:\s*([^;]+);/.exec(rule[1]);
    assert.ok(fs1 && /^\d+(\.\d+)?px$/.test(fs1[1].trim()), cls + ' font-size is plain px');
  }
  assert.match(css, /\.project-gantt-label\s*\{[^}]*position:\s*sticky;\s*left:\s*0/);
});

// ---- order ----

test('the Gantt block comes before Unassigned Automata, and Unassigned before History', () => {
  const { sb } = load({ showEmpty: true });
  for (const roll of [ROLLUP_NO_UNASSIGNED, ROLLUP]) {
    const html = page(sb, roll);
    const g = html.indexOf('id="projectGantt"');
    const u = html.indexOf('Unassigned Automata');
    const h = html.indexOf('id="projectHistory"');
    assert.ok(g > -1 && u > -1 && h > -1, 'all three blocks drawn');
    assert.ok(g < u, 'Gantt before Unassigned');
    assert.ok(u < h, 'Unassigned before History');
  }
  assert.match(page(sb, ROLLUP), /data-automaton="loose-1"/, 'the unassigned Automaton is drawn');
});

// ---- collapse memory ----

const SECTION_IDS = ['arcs', 'gantt', 'unassigned', 'history', 'events'];

test('every drawn section sits in a card with a toggle', () => {
  const { sb } = load({ showEmpty: true });
  const html = page(sb, ROLLUP_NO_UNASSIGNED);
  const ids = Array.from(html.matchAll(/data-section="([^"]+)"/g)).map(m => m[1]);
  for (const id of ['arcs', 'gantt', 'unassigned', 'history']) assert.ok(ids.includes(id), id + ' is a card');
  for (const id of new Set(ids)) {
    const card = new RegExp(`<div class="project-section-card[^"]*" id="projectSection-${id}" data-section="${id}"`);
    assert.match(html, card, id + ' card wrapper');
    assert.match(html, new RegExp(`projectsToggleSection\\('${id}'\\)`), id + ' has a toggle handler');
  }
  assert.equal((html.match(/class="project-section-toggle"/g) || []).length, (html.match(/class="project-section-card/g) || []).length, 'one toggle per card');
  assert.ok(!/ hidden>/.test(html) && !/ collapsed"/.test(html), 'nothing starts collapsed');
});

// fakeDom wires document.querySelectorAll to the cards found in html, enough for
// projectsToggleSection to flip one in place.
function wireCards(sb, html) {
  const cards = {};
  for (const m of html.matchAll(/class="project-section-card( collapsed)?" id="[^"]*" data-section="([^"]+)" data-title="([^"]*)"/g)) {
    const classes = new Set(['project-section-card']);
    if (m[1]) classes.add('collapsed');
    const attrs = { 'data-title': m[3] };
    const btn = { attrs: {}, setAttribute(k, v) { this.attrs[k] = v; }, querySelector() { return { textContent: '' }; } };
    const body = { hidden: !!m[1] };
    cards[m[2]] = {
      getAttribute(k) { return k in attrs ? attrs[k] : null; },
      classList: { contains: c => classes.has(c), toggle: (c, on) => { if (on) classes.add(c); else classes.delete(c); } },
      querySelector(sel) { return sel === '.project-section-toggle' ? btn : body; },
      btn, body,
    };
  }
  sb.document.querySelectorAll = sel => {
    const m = /data-section="([^"]+)"/.exec(sel);
    return m && cards[m[1]] ? [cards[m[1]]] : [];
  };
  return cards;
}

test('collapsing stores a per-section value; a fresh render draws only that section collapsed; expanding clears it', () => {
  const storage = memStorage();
  const a = load({ storage, showEmpty: true });
  const cards = wireCards(a.sb, page(a.sb));
  a.sb.projectsToggleSection('gantt');
  assert.equal(cards.gantt.body.hidden, true, 'body hidden in place');
  assert.deepEqual(storage.keys(), ['cs_projects_section_collapsed_gantt'], 'one per-section key');
  assert.equal(storage.getItem('cs_projects_section_collapsed_gantt'), '1');

  const b = load({ storage, showEmpty: true });
  const html = page(b.sb);
  const card = id => new RegExp(`<div class="project-section-card( collapsed)?" id="projectSection-${id}"`).exec(html);
  assert.equal(card('gantt')[1], ' collapsed', 'gantt drawn collapsed');
  for (const id of ['arcs', 'unassigned', 'history']) assert.equal(card(id)[1], undefined, id + ' stays expanded');
  const ganttCard = html.slice(html.indexOf('id="projectSection-gantt"'), html.indexOf('id="projectSection-unassigned"'));
  assert.match(ganttCard, /aria-expanded="false"/);
  assert.match(ganttCard, /project-section-body" hidden>/);
  assert.match(ganttCard, /aria-label="Expand Gantt/);

  const cards2 = wireCards(b.sb, html);
  b.sb.projectsToggleSection('gantt');
  assert.equal(cards2.gantt.body.hidden, false, 'expanded in place');
  assert.notEqual(storage.getItem('cs_projects_section_collapsed_gantt'), '1', 'expanding clears the stored collapse');
  const c = load({ storage, showEmpty: true });
  assert.doesNotMatch(page(c.sb), /project-section-card collapsed/, 'fresh render has nothing collapsed');
});

test('a localStorage that throws still renders every section expanded without an error', () => {
  // app.js reads storage once at load, so the store starts blocking afterwards.
  const { sb } = load({ showEmpty: true });
  sb.localStorage = throwingStorage;
  let html;
  assert.doesNotThrow(() => { html = page(sb, ROLLUP_NO_UNASSIGNED); });
  assert.ok(html.includes('id="projectGantt"') && html.includes('id="projectHistory"'));
  assert.doesNotMatch(html, /project-section-card collapsed/);
  assert.doesNotMatch(html, /project-section-body" hidden/);
  const cards = wireCards(sb, html);
  assert.doesNotThrow(() => sb.projectsToggleSection('gantt'));
  assert.ok(cards.gantt, 'card present');
});

test('the Projects view paints the cards through the real render path', async () => {
  const storage = memStorage();
  storage.setItem('cs_projects_section_collapsed_history', '1');
  const { sb, out } = load({ storage, showEmpty: true });
  sb.fetch = (url) => {
    const u = String(url);
    const ok = body => Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(body), text: () => Promise.resolve(JSON.stringify(body)) });
    if (u.includes('/history')) return ok(hist());
    if (u.includes('/rollup')) return ok(ROLLUP);
    if (u.includes('/api/projects')) return ok([FULL]);
    return ok([]);
  };
  sb.location.hash = '#projects/acme';
  vm.runInContext("_projectsState.selected = 'acme'", sb);
  sb.renderProjectsView();
  await flushAsync();
  assert.match(out.view, /id="projectSection-gantt"/);
  assert.match(out.view, /class="project-section-card collapsed" id="projectSection-history"/);
  assert.ok(out.view.indexOf('id="projectGantt"') < out.view.indexOf('Unassigned Automata'));
});

// ---- no hide / reorder controls, no sub-tab ----

test('no control hides or moves a card and no sub-tab was added', () => {
  const { sb } = load({ showEmpty: true });
  const html = page(sb);
  assert.doesNotMatch(html, /draggable/i);
  assert.doesNotMatch(html, /ondrag|ondrop/i);
  assert.doesNotMatch(html, /(hide|move|reorder|sort)[A-Za-z]*Section|projects(Hide|Move|Reorder)/);
  assert.doesNotMatch(html, /role="tab"|role="tablist"|project-subtab|projects-subtab/);
  assert.doesNotMatch(html, />\s*(Hide|Move up|Move down|Reorder|Remove)\s*</);
  for (const m of html.matchAll(/class="project-section-toggle"[^>]*onclick="([^"]*)"/g)) {
    assert.match(m[1], /^projectsToggleSection\('[a-z]+'\)$/, 'a toggle only collapses or expands');
  }
  const ids = new Set(Array.from(html.matchAll(/data-section="([^"]+)"/g)).map(m => m[1]));
  for (const id of ids) assert.ok([...SECTION_IDS, 'progress', 'stream', 'policy', 'merge'].includes(id), id + ' is a known section');
});

// ---- strings ----

test('the toggle strings exist in all five locales with the same placeholders and translated values', () => {
  const keys = ['projects_section_collapse', 'projects_section_expand'];
  const bundles = {};
  for (const l of LOCALES) bundles[l] = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'));
  const ph = s => (String(s).match(/%\d+\$s/g) || []).sort().join(',');
  const SAME_AS_ENGLISH_OK = new Set([]);
  for (const k of keys) {
    const en = bundles.en[k];
    assert.equal(typeof en, 'string', k + ' in en');
    assert.ok(ph(en).includes('%1$s'), k + ' takes the section title');
    for (const l of LOCALES.filter(x => x !== 'en')) {
      const v = bundles[l][k];
      assert.equal(typeof v, 'string', `${k} in ${l}`);
      assert.equal(ph(v), ph(en), `${k} placeholders in ${l}`);
      if (!SAME_AS_ENGLISH_OK.has(k + ':' + l)) assert.notEqual(v, en, `${k} in ${l} is translated`);
    }
  }
});
