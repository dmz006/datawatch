// Phone layout consistency — every view shares one page container, nothing in
// the phone query is wider than a phone, every filter row wraps and the
// Dashboard header counters never collapse into one narrow column.
//
// Like app-automata-phone-strips.test.js this reads style.css (comments
// stripped) and app.js as source, and like the browser-stub tests it loads the
// real app.js into a vm sandbox to render each view's markup. No network, no
// timers: the stub fetch resolves to a 404, the stub timers never fire, and
// every render below is synchronous.
//
// What is checked:
//   1. Each view root (Sessions, the Automata sub-tabs, Alerts, Observer,
//      Dashboard, Settings, Projects) carries the shared .phone-page class.
//   2. The shared container rule exists in the phone query with a side gutter,
//      width 100%, box-sizing border-box and min-width 0.
//   3. No rule in the phone query declares a fixed width / min-width above
//      360 px (max-width, %, calc(), min()/max()/clamp() and vw/vh are fine).
//   4. Every filter-row container wraps (flex-wrap: wrap) on a phone.
//   5. The Dashboard header counters stay unbreakable units in a wrapping row,
//      never a single narrow column.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

const css = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');
const appSource = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');

const PHONE_QUERY = '@media (max-width: 600px), (pointer: coarse)';
const MAX_PHONE_FIXED_PX = 360;

// ── CSS helpers (same shape as app-automata-phone-strips.test.js) ──────────

// atRuleBodies returns the body (balanced braces) of every block of `source`
// that starts with the given at-rule header.
function atRuleBodies(header, source = css) {
  const bodies = [];
  let from = 0;
  for (;;) {
    const i = source.indexOf(header, from);
    if (i < 0) break;
    const open = source.indexOf('{', i);
    let depth = 0, j = open;
    for (; j < source.length; j++) {
      if (source[j] === '{') depth++;
      else if (source[j] === '}' && --depth === 0) break;
    }
    bodies.push(source.slice(open + 1, j));
    from = j;
  }
  return bodies;
}

// parseRules returns [{ selectors: [...], body }] for every flat rule in
// `scope`. A nested at-rule header is not a selector (the regexp cannot span a
// '{'), so rules inside a nested block are still reported.
function parseRules(scope) {
  const out = [];
  const re = /([^{}]+)\{([^{}]*)\}/g;
  let m;
  while ((m = re.exec(scope))) {
    out.push({ selectors: m[1].split(',').map(s => s.trim()), body: m[2] });
  }
  return out;
}

// rulesFor returns the declaration text of every rule in `scope` whose
// selector list contains exactly `selector`.
function rulesFor(scope, selector) {
  return parseRules(scope).filter(r => r.selectors.includes(selector)).map(r => r.body);
}

// declarations splits a rule body into [{ prop, value }] (lower-cased prop,
// !important removed from the value).
function declarations(body) {
  return body.split(';').map(d => d.trim()).filter(Boolean).map(d => {
    const i = d.indexOf(':');
    return i < 0 ? null : {
      prop: d.slice(0, i).trim().toLowerCase(),
      value: d.slice(i + 1).replace(/!important/i, '').trim(),
    };
  }).filter(Boolean);
}

// outsideAtRules removes every at-rule block (media, supports, keyframes, ...)
// so only the base, un-queried rules remain.
function outsideAtRules(source) {
  let out = '';
  let i = 0;
  while (i < source.length) {
    const at = source.indexOf('@', i);
    if (at < 0) { out += source.slice(i); break; }
    out += source.slice(i, at);
    const open = source.indexOf('{', at);
    const semi = source.indexOf(';', at);
    if (open < 0 || (semi >= 0 && semi < open)) { i = (semi < 0 ? source.length : semi + 1); continue; }
    let depth = 0, j = open;
    for (; j < source.length; j++) {
      if (source[j] === '{') depth++;
      else if (source[j] === '}' && --depth === 0) break;
    }
    i = j + 1;
  }
  return out;
}

// fixedPixelWidths lists every width / min-width declaration in `scope` whose
// whole value is one plain pixel length. max-width, percentages, calc(), min(),
// max(), clamp() and viewport units are not fixed widths, so they never match.
function fixedPixelWidths(scope) {
  const found = [];
  for (const rule of parseRules(scope)) {
    for (const d of declarations(rule.body)) {
      if (d.prop !== 'width' && d.prop !== 'min-width') continue;
      const px = d.value.match(/^(\d+(?:\.\d+)?)px$/i);
      if (px) found.push({ selector: rule.selectors.join(', '), prop: d.prop, px: parseFloat(px[1]) });
    }
  }
  return found;
}

const phoneBodies = atRuleBodies(PHONE_QUERY);
const phoneScope = phoneBodies.join('\n');
const baseScope = outsideAtRules(css);

// has / hasWrap: `flex-wrap: wrap` but not `wrap-reverse` or `nowrap`.
const WRAP = /flex-wrap:\s*wrap\b(?!-)/;
const NOWRAP = /flex-wrap:\s*nowrap/;

// ── Markup helpers ──────────────────────────────────────────────────────────

const DIV_TAG = /<div\b(?:[^>"']|"[^"]*"|'[^']*')*>|<\/div\s*>/g;

// topLevelDivs returns the outermost <div> elements of rendered markup as
// { openTag, classes, inner }. HTML comments are dropped first.
function topLevelDivs(html) {
  const src = html.replace(/<!--[\s\S]*?-->/g, '');
  const out = [];
  let depth = 0, start = -1, openTag = '';
  const push = (end) => {
    const cm = openTag.match(/\sclass="([^"]*)"/);
    out.push({ openTag, classes: cm ? cm[1].split(/\s+/).filter(Boolean) : [], inner: src.slice(start, end) });
  };
  for (const m of src.matchAll(DIV_TAG)) {
    if (m[0][1] === '/') {
      depth--;
      if (depth === 0 && start >= 0) { push(m.index); start = -1; }
    } else {
      if (depth === 0) { openTag = m[0]; start = m.index + m[0].length; }
      depth++;
    }
  }
  if (depth > 0 && start >= 0) push(src.length);
  return out;
}

// contentRoots are the page wrappers of a view: every top-level div except the
// decorative watermark.
function contentRoots(html) {
  return topLevelDivs(html).filter(d => !d.classes.includes('sessions-watermark'));
}

function assertPhonePageRoot(html, label) {
  const roots = contentRoots(html);
  assert.ok(roots.length >= 1, `${label}: renders a root wrapper`);
  for (const r of roots) {
    assert.ok(r.classes.includes('phone-page'), `${label}: root ${r.openTag} carries phone-page`);
  }
  return roots;
}

// ── Rendering helpers ───────────────────────────────────────────────────────

// freshView loads the real app.js into a new sandbox. #view and #automataPanel
// are capturing stub elements: whatever a render writes to their innerHTML is
// what the browser would show.
function freshView() {
  const els = {};
  for (const id of ['view', 'automataPanel']) {
    const el = makeStubElement();
    el.insertAdjacentHTML = () => {};
    els[id] = el;
  }
  const sb = buildSandbox({ byId: els });
  sb.URLSearchParams = URLSearchParams;
  vm.createContext(sb);
  loadScript(path.join(__dirname, 'app.js'), sb);
  return { sb, els, run: (code) => vm.runInContext(code, sb) };
}

// ── 1. Every view root carries the shared phone-page class ─────────────────

// Each entry renders one view (or sub-tab) and returns the markup of its root.
const VIEW_ROOTS = [
  ['Sessions', ({ sb, els }) => { sb.renderSessionsView(); return els.view.innerHTML; }],
  ['Alerts', ({ sb, els }) => { sb.renderAlertsView(); return els.view.innerHTML; }],
  ['Observer', ({ sb, els }) => { sb.renderObserverView(); return els.view.innerHTML; }],
  ['Dashboard', ({ sb, els, run }) => {
    run("state.activeView = 'dashboard'");
    sb.renderDashboardView();
    return els.view.innerHTML;
  }],
  ['Settings', ({ sb, els }) => { sb.renderSettingsView(); return els.view.innerHTML; }],
  // Projects paints its own page into the Automata panel.
  ['Projects (empty list)', ({ sb, els, run }) => {
    run("_projectsState.list = []; _projectsState.error = ''");
    sb._projectsPaint();
    return els.automataPanel.innerHTML;
  }],
  ['Projects (picker, one project)', ({ sb }) => sb._projectsPickerHTML([{ name: 'alpha' }], 'alpha', '')],
  ['Projects (picker, load error)', ({ sb }) => sb._projectsPickerHTML([], '', 'boom')],
  // The Gantt focus page, opened from an Automation and from a project.
  ['Projects (focus page from an Automation)', ({ sb }) => sb._projectsFocusPageHTML({ from: 'automation', id: 'a1', title: 't', loading: true, rows: [] })],
  ['Projects (focus page from a project)', ({ sb }) => sb._projectsFocusPageHTML({ from: 'project', id: 'alpha', title: 't', loading: true, rows: [] })],
  // Automation sub-tab pages that have their own render function.
  ['Automation detail shell', ({ sb }) => sb._automationDetailShell('<p>x</p>')],
  ['Automation form', ({ sb }) => sb._automationFormHTML()],
];

for (const [name, render] of VIEW_ROOTS) {
  test(`${name}: root wrapper carries phone-page`, () => {
    const ctx = freshView();
    assertPhonePageRoot(render(ctx), name);
  });
}

// The Automata view is one root shared by its sub-tabs. Each sub-tab either
// renders a page of its own (Projects) or paints a panel into #automataPanel,
// which must sit inside the phone-page root.
const AUTOMATA_TABS = ['automata', 'projects', 'events', 'templates', 'automation'];

for (const tab of AUTOMATA_TABS) {
  test(`Automata / ${tab}: view root carries phone-page and holds the sub-tab content`, () => {
    const { sb, els, run } = freshView();
    run(`_automataState.tab = ${JSON.stringify(tab)}`);
    sb.renderAutonomousView();
    const roots = assertPhonePageRoot(els.view.innerHTML, `Automata/${tab}`);
    const body = roots.map(r => r.inner).join('\n');
    if (tab === 'automation') {
      assert.match(body, /automation-subtabs/, 'Automation sub-tab strip is inside the root');
    } else {
      assert.match(body, /id="automataPanel"/, `#automataPanel is inside the ${tab} root`);
    }
  });
}

for (const id of ['inbox', 'automations', 'resources', 'progress', 'housekeeping']) {
  test(`Automation / ${id}: sub-tab renders inside the phone-page root`, () => {
    const { sb, els, run } = freshView();
    run(`_automataState.tab = 'automation'; _automationState.subtab = ${JSON.stringify(id)}`);
    sb.renderAutonomousView();
    const roots = assertPhonePageRoot(els.view.innerHTML, `Automation/${id}`);
    const body = roots.map(r => r.inner).join('\n');
    assert.match(body, new RegExp(`data-subtab="${id}"[^>]*>`), `${id} tab button is drawn`);
    assert.match(body, /class="output-tab active"[^>]*data-subtab="/, 'one sub-tab is active');
  });
}

// Sub-tab panels that paint into #automataPanel without a root of their own:
// the panel's host is the phone-page root asserted above, so the panel markup
// itself must not bring a second page-level gutter or width that fights it.
const PANEL_RENDERS = [
  ['Events', ({ sb }) => sb.renderAutomataEventsPanel()],
  ['Templates / automaton', ({ sb }) => sb._automataRenderTemplates()],
  ['Templates / standard tasks', ({ sb }) => sb._automataStandardTasksRender()],
];

for (const [name, render] of PANEL_RENDERS) {
  test(`${name}: panel renders into the Automata panel without its own fixed page width`, () => {
    const ctx = freshView();
    render(ctx);
    const html = ctx.els.automataPanel.innerHTML;
    assert.ok(html.length > 0, `${name} wrote markup`);
    // A panel-level root may add the phone-page class, but never a wide
    // inline width/min-width of its own.
    for (const root of topLevelDivs(html)) {
      const style = (root.openTag.match(/\sstyle="([^"]*)"/) || [, ''])[1];
      for (const d of declarations(style)) {
        if (d.prop !== 'width' && d.prop !== 'min-width') continue;
        const px = d.value.match(/^(\d+(?:\.\d+)?)px$/i);
        assert.ok(!px || parseFloat(px[1]) <= MAX_PHONE_FIXED_PX, `${name}: inline ${d.prop}: ${d.value}`);
      }
    }
  });
}

// Form screens that are not one of the page views listed above. The New
// Session form is opened from the "+" button and is outside the phone-page set.
const NON_PAGE_VIEW_RENDERERS = new Set(['renderNewSessionView']);

test('every page-view root template in app.js that sets #view innerHTML names phone-page', () => {
  // Each template literal written straight into #view that opens a
  // `view-content` wrapper must carry phone-page next to it, so a new view
  // cannot be added without the shared container.
  const wrappers = [...appSource.matchAll(/class="view-content([^"]*)"/g)];
  assert.ok(wrappers.length >= 8, 'found the view-content wrappers');
  const missing = [];
  for (const m of appSource.matchAll(/(?:view|viewEl)\.innerHTML\s*=\s*`\s*(?:<!--[\s\S]*?-->\s*)?<div\s+class="view-content([^"]*)"/g)) {
    if (/\bphone-page\b/.test(m[1])) continue;
    const before = appSource.slice(0, m.index);
    const fn = [...before.matchAll(/function\s+(\w+)\s*\(/g)].pop();
    const name = fn ? fn[1] : '(top level)';
    if (!NON_PAGE_VIEW_RENDERERS.has(name)) missing.push(name);
  }
  assert.deepEqual(missing, []);
});

// ── 2. The shared container rule ────────────────────────────────────────────

test('phone query declares the shared .phone-page container', () => {
  const decls = declarations(rulesFor(phoneScope, '.phone-page').join(';'));
  const get = (prop) => decls.filter(d => d.prop === prop).map(d => d.value);
  assert.ok(decls.length > 0, '.phone-page has a rule in the phone query');
  assert.ok(get('width').includes('100%'), 'width: 100%');
  assert.ok(get('box-sizing').includes('border-box'), 'box-sizing: border-box');
  assert.ok(get('min-width').includes('0'), 'min-width: 0');
  assert.ok(get('padding-left').includes('var(--phone-gutter)'), 'left side gutter');
  assert.ok(get('padding-right').includes('var(--phone-gutter)'), 'right side gutter');
});

test('the phone gutter is a real, phone-sized length', () => {
  const root = declarations(rulesFor(phoneScope, ':root').join(';'));
  const gutter = root.find(d => d.prop === '--phone-gutter');
  assert.ok(gutter, '--phone-gutter is defined in the phone query');
  const px = gutter.value.match(/^(\d+(?:\.\d+)?)px$/);
  assert.ok(px, `gutter ${gutter.value} is a pixel length`);
  assert.ok(parseFloat(px[1]) >= 8 && parseFloat(px[1]) <= 24, `gutter ${gutter.value} is between 8 and 24 px`);
});

test('shared cards fill the container width and may shrink', () => {
  const cards = ['.phone-card', '.phone-page .prd-card', '.phone-page .alert-card', '.phone-page .settings-section', '.phone-page .dboard-card'];
  for (const sel of cards) {
    const decls = declarations(rulesFor(phoneScope, sel).join(';'));
    const has = (prop, value) => decls.some(d => d.prop === prop && d.value === value);
    assert.ok(has('width', '100%'), `${sel} width: 100%`);
    assert.ok(has('min-width', '0'), `${sel} min-width: 0`);
    assert.ok(has('box-sizing', 'border-box'), `${sel} box-sizing: border-box`);
  }
});

test('the container only applies on a phone: no base .phone-page rule', () => {
  assert.deepEqual(rulesFor(baseScope, '.phone-page'), [], 'desktop layout has no .phone-page rule');
  assert.ok(phoneBodies.length > 0);
});

// ── 3. Nothing in the phone query is a fixed width wider than 360 px ───────

test('fixedPixelWidths flags only plain pixel width / min-width values', () => {
  const sample = `
    .a { width: 500px; }
    .b { min-width: 361px !important; }
    .c { max-width: 900px; }
    .d { width: 100%; min-width: 0; }
    .e { width: calc(100% - 500px); }
    .f { min-width: min(500px, 100%); }
    .g { width: 90vw; border-width: 800px; column-width: 700px; }
    .h { width: 360px; }
  `;
  assert.deepEqual(
    fixedPixelWidths(sample).map(f => `${f.selector}:${f.prop}:${f.px}`),
    ['.a:width:500', '.b:min-width:361', '.h:width:360'],
  );
});

test('no rule in the phone query has a fixed width or min-width above 360px', () => {
  const tooWide = fixedPixelWidths(phoneScope).filter(f => f.px > MAX_PHONE_FIXED_PX);
  assert.deepEqual(tooWide.map(f => `${f.selector} { ${f.prop}: ${f.px}px }`), []);
});

test('phone query scan covers every block of the query', () => {
  // The stylesheet repeats the phone query in several places; the scan above
  // must walk all of them, so there is more than one block and each is
  // non-empty.
  assert.ok(phoneBodies.length >= 2, `found ${phoneBodies.length} phone blocks`);
  for (const body of phoneBodies) assert.ok(body.trim().length > 0);
});

// ── 4. Every filter row wraps on a phone ────────────────────────────────────

// The filter-row containers declared in the phone query. Events filters are
// separate: that row is drawn with an inline flex-wrap.
const FILTER_ROWS = [
  '.timeline-card-filters',
  '.automata-filter-bar',
  '.plan-records-controls',
  '.plans-browser-controls',
  '.records-tab-controls',
  '.cost-charts-controls',
  '.list-controls',
  '.trace-filters',
  '.page-controls',
  '.obs-filter-row',
  '.automation-controls',
  '.project-gantt-toolbar',
];

for (const sel of FILTER_ROWS) {
  test(`${sel} wraps inside the phone query`, () => {
    const decls = rulesFor(phoneScope, sel).join('\n');
    assert.ok(decls.length > 0, `${sel} has a rule in the phone query`);
    assert.match(decls, /display:\s*flex/, `${sel} is a flex row`);
    assert.match(decls, WRAP, `${sel} has flex-wrap: wrap`);
    assert.doesNotMatch(decls, NOWRAP, `${sel} does not set nowrap`);
  });

  test(`${sel} children may shrink inside the phone query`, () => {
    const decls = rulesFor(phoneScope, `${sel} > *`).join('\n');
    assert.match(decls, /min-width:\s*0/, `${sel} > * min-width: 0`);
    assert.match(decls, /max-width:\s*100%/, `${sel} > * max-width: 100%`);
  });
}

test('Events filters wrap: the row and its kind chips carry flex-wrap: wrap', () => {
  const { sb } = freshView();
  const html = sb.automataEventsFilterHtml();
  const row = html.match(/<div id="automataEventsFilters"[^>]*>/);
  assert.ok(row, 'the Events filter row is drawn');
  assert.match(row[0], WRAP, 'Events filter row wraps');
  const chips = html.match(/<div class="automata-events-kinds"[^>]*>/);
  assert.ok(chips, 'the Events kind chips are drawn');
  assert.match(chips[0], WRAP, 'Events kind chips wrap');
  // Nothing in the phone query turns either one back into a single line.
  for (const sel of ['#automataEventsFilters', '.automata-events-kinds']) {
    assert.doesNotMatch(rulesFor(phoneScope, sel).join('\n'), NOWRAP, sel);
  }
});

test('Events filter children are not wider than a phone', () => {
  const { sb } = freshView();
  const html = sb.automataEventsFilterHtml();
  for (const m of html.matchAll(/min-width:\s*(\d+(?:\.\d+)?)px/g)) {
    assert.ok(parseFloat(m[1]) <= MAX_PHONE_FIXED_PX, `inline min-width ${m[0]}`);
  }
});

test('every filter-row class that app.js renders is a wrapping row or sits on one', () => {
  // Drift guard: a new `*-controls`, `*-filters`, `filter-bar` or `filter-row`
  // container must be added to FILTER_ROWS (and the phone query) or share its
  // element with one that is.
  const covered = new Set(FILTER_ROWS.map(s => s.slice(1)));
  const uncovered = new Set();
  for (const m of appSource.matchAll(/class="([^"$]*)"/g)) {
    const tokens = m[1].split(/\s+/).filter(Boolean);
    const rowish = tokens.filter(t => /(-controls|-filters|filter-bar|filter-row)$/.test(t));
    if (rowish.length && !tokens.some(t => covered.has(t))) rowish.forEach(t => uncovered.add(t));
  }
  assert.deepEqual([...uncovered].sort(), []);
});

// ── 5. Dashboard header counters ────────────────────────────────────────────

const COUNTERS = ['#dashStatSessions', '#dashStatTasks', '#dashStatGuardrails'];

// barWrapsAsRow: the stat bar is a flex-wrap row or a multi-column grid.
function barWrapsAsRow(scope) {
  const decls = declarations(rulesFor(scope, '.dboard-stat-bar').join(';'));
  const val = (prop) => decls.filter(d => d.prop === prop).map(d => d.value);
  if (val('flex-wrap').some(v => /^wrap$/.test(v))) return true;
  const cols = val('grid-template-columns');
  return val('display').includes('grid') && cols.length > 0 && !cols.every(v => /^(1fr|none|100%)$/.test(v));
}

test('Dashboard stat bar is rendered with the counters inside it', () => {
  const { sb, els, run } = freshView();
  run("state.activeView = 'dashboard'");
  sb.renderDashboardView();
  const html = els.view.innerHTML;
  const [root] = assertPhonePageRoot(html, 'Dashboard');
  const bar = root.inner.match(/<div class="dboard-stat-bar">([\s\S]*?)<\/div>\s*(?:<div|$)/);
  assert.ok(bar, 'the stat bar is the first block of the Dashboard page');
  for (const id of ['dashStatSessions', 'dashStatTasks', 'dashStatGuardrails', 'dashStatBurnRate']) {
    assert.match(bar[1], new RegExp(`id="${id}"`), `${id} sits in the stat bar`);
  }
});

test('Dashboard stat bar wraps as a row on a phone, never one narrow column', () => {
  assert.ok(barWrapsAsRow(phoneScope), 'phone stat bar is a flex-wrap row (or multi-column grid)');
  const decls = declarations(rulesFor(phoneScope, '.dboard-stat-bar').concat(rulesFor(baseScope, '.dboard-stat-bar')).join(';'));
  assert.ok(!decls.some(d => d.prop === 'flex-direction' && /^column/.test(d.value)), 'no column direction');
  assert.ok(!decls.some(d => d.prop === 'flex-wrap' && d.value === 'nowrap'), 'no nowrap');
  assert.ok(!decls.some(d => d.prop === 'display' && d.value === 'block'), 'not a block stack');
});

test('Dashboard header counters keep white-space: nowrap or the bar wraps as a row', () => {
  const counterNowrap = COUNTERS.every(sel =>
    declarations(rulesFor(phoneScope, sel).join(';')).some(d => d.prop === 'white-space' && d.value === 'nowrap'));
  assert.ok(counterNowrap || barWrapsAsRow(phoneScope), 'counters hold or the bar wraps');
});

test('Dashboard header counters are unbreakable, non-shrinking units', () => {
  // The current rule: each counter is one nowrap, non-shrinking unit so a
  // value like "$521.32 today" never splits mid-number.
  const decls = declarations(rulesFor(phoneScope, '#dashStatSessions').join(';'));
  for (const sel of COUNTERS) {
    const d = declarations(rulesFor(phoneScope, sel).join(';'));
    assert.ok(d.some(x => x.prop === 'white-space' && x.value === 'nowrap'), `${sel} white-space: nowrap`);
    assert.ok(d.some(x => x.prop === 'flex' && x.value === '0 0 auto'), `${sel} flex: 0 0 auto`);
  }
  assert.ok(decls.length > 0);
});

test('Dashboard burn-rate summary takes its own full line and wraps between parts', () => {
  const decls = declarations(rulesFor(phoneScope, '#dashStatBurnRate').join(';'));
  assert.ok(decls.some(d => d.prop === 'flex' && d.value === '1 1 100%'), 'burn rate fills its own row');
  assert.ok(decls.some(d => d.prop === 'white-space' && d.value === 'normal'), 'burn rate may wrap');
  const parts = declarations(rulesFor(phoneScope, '#dashStatBurnRate > span').join(';'));
  assert.ok(parts.some(d => d.prop === 'white-space' && d.value === 'nowrap'), 'each burn-rate part stays whole');
});

test('Dashboard stat bar children can shrink and the page cannot scroll sideways', () => {
  const kids = declarations(rulesFor(phoneScope, '.dboard-stat-bar > *').join(';'));
  assert.ok(kids.some(d => d.prop === 'min-width' && d.value === '0'), 'stat bar children min-width: 0');
  const page = declarations(rulesFor(phoneScope, '.phone-page').join(';'));
  assert.ok(page.some(d => d.prop === 'overflow-x' && d.value === 'hidden'), '.phone-page overflow-x: hidden');
});
