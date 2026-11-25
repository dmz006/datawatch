'use strict';
// Header scope icon button and its modal, driven end to end.
//
// The scope button sits in the shared header, shows only on the Automata view
// and opens the scope modal. This file runs the real app.js in a vm sandbox
// (no daemon, no tmux) and pins what an operator and a screen reader meet:
//   (a) the button: one element, a real button with an accessible name from
//       header_scope_button_label, visible only on the Automata view;
//   (b) opening: click opens #scopeModal as a labelled modal dialog, focus moves
//       inside and aria-expanded turns true;
//   (c) closing: Escape, backdrop, Close and a row choice all close it and
//       return focus to the button; no keydown listener outlives a cycle;
//   (d) the rows: All, each project and, for a chosen project, All plans and
//       each plan; every choice calls setScope with the right project and plan;
//   (e) the badge: shown only while a scope is active, cleared by All;
//   (f) memory: the choice is kept under dw_scope and restored on a fresh
//       sandbox; a removed project or plan falls back and the fallback is
//       saved; blocked storage never throws.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-header-scope-button.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, flushAsync } = require('./testutil_browser_stub');
const { installFakeDom } = require('./testutil_fake_dom');

const WEB = __dirname;
const EN = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
const INDEX_HTML = fs.readFileSync(path.join(WEB, 'index.html'), 'utf8');

const PROJECTS = [
  { name: 'acme', dir: '/work/acme', arcs: [{ automation_id: 'au-1' }, { automation_id: 'au-2' }] },
  { name: 'zeta', dir: '/work/zeta', arcs: [] },
  { name: 'solo', dir: '/work/solo', arcs: [{ automation_id: 'au-7' }] },
];
const AUTOMATIONS = [
  { id: 'au-1', name: 'Launch plan' },
  { id: 'au-2', name: 'Cleanup plan' },
  { id: 'au-7', name: 'Solo plan' },
];

function json(body) {
  return Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(body), text: () => Promise.resolve(JSON.stringify(body)) });
}

// load builds a sandbox on the Automata view. store is a Map shared between two
// loads to model "a fresh page on the same device"; projects is the list the
// daemon returns.
function load({ store = null, projects = PROJECTS, view = 'autonomous', tab = 'automata' } = {}) {
  const sandbox = buildSandbox({});
  const dom = installFakeDom(sandbox, { statics: ['headerScopeBtn', 'headerScopeBadge', 'view', 'automataPanel', 'nav', 'backBtn'] });
  ['view', 'automataPanel'].forEach(id => { dom.el(id).insertAdjacentHTML = () => {}; });
  sandbox.fetch = (url) => {
    const u = String(url);
    if (u.endsWith('/api/projects')) return json(projects);
    if (u.endsWith('/api/automations')) return json(AUTOMATIONS);
    return json([]);
  };
  if (store) {
    sandbox.localStorage = {
      getItem(k) { return store.has(k) ? store.get(k) : null; },
      setItem(k, v) { store.set(k, String(v)); },
      removeItem(k) { store.delete(k); },
    };
  }
  sandbox.history = { pushState() {}, replaceState() {} };
  vm.createContext(sandbox);
  loadScript(path.join(WEB, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(JSON.stringify(EN));
  vm.runInContext(`state.activeView = ${JSON.stringify(view)}; _automataState.tab = ${JSON.stringify(tab)};`, sandbox);
  sandbox.renderAutonomousView = () => {};
  sandbox.__dom = dom;
  return sandbox;
}

const run = (sb, code) => vm.runInContext(code, sb);
const btn = sb => sb.__dom.el('headerScopeBtn');
const badge = sb => sb.__dom.el('headerScopeBadge');
const modal = sb => sb.__dom.byId('scopeModal');
const rows = sb => { const b = sb.__dom.byId('scopeModalBody'); return b ? b.querySelectorAll('button') : []; };
const scopeOf = sb => JSON.parse(JSON.stringify(sb.getScope()));
const stored = sb => { const v = sb.localStorage.getItem('dw_scope'); return v === null ? null : JSON.parse(v); };

function rowBy(sb, attr, val) {
  const hit = rows(sb).find(r => r.getAttribute(attr) === val);
  assert.ok(hit, `no row with ${attr}=${JSON.stringify(val)}`);
  return hit;
}
const allRow = sb => rows(sb).find(r => r.getAttribute('data-scope-all') !== null);
const choose = (sb, row) => run(sb, row.getAttribute('onclick'));
// click presses the header button the way the page does: through its onclick.
function click(sb) { run(sb, INDEX_HTML.match(/id="headerScopeBtn"[^>]*onclick="([^"]+)"/)[1]); }
async function open(sb) { click(sb); await flushAsync(); return modal(sb); }
// press fires the document keydown handlers the modal installed.
const press = (sb, key) => sb.__dom.fire('keydown', { key, preventDefault() {} });
const keydowns = sb => sb.__dom.listenerCount('keydown');

// withoutHtmlComments returns the markup with every comment cut out. A comment
// that is never closed stays.
function withoutHtmlComments(html) {
  let out = '';
  let from = 0;
  for (;;) {
    const open = html.indexOf('<!--', from);
    const close = open < 0 ? -1 : html.indexOf('-->', open + 4);
    if (close < 0) return out + html.slice(from);
    out += html.slice(from, open);
    from = close + 3;
  }
}

// spyOnSetScope records every setScope call a row handler makes through the
// global the inline onclick resolves.
function spyOnSetScope(sb) {
  const calls = [];
  const real = sb.setScope;
  sb.setScope = function (project, plan, opts) { calls.push([project, plan]); return real.apply(this, arguments); };
  return calls;
}

// ── (a) the button ─────────────────────────────────────────────────────────

test('(a) #headerScopeBtn exists once, as a real button with an accessible name', () => {
  const src = withoutHtmlComments(INDEX_HTML);
  assert.equal((src.match(/id="headerScopeBtn"/g) || []).length, 1, 'exactly one in index.html');
  const tag = src.match(/<button\b[^>]*id="headerScopeBtn"[^>]*>/)[0];
  assert.match(tag, /type="button"/);
  assert.match(tag, /aria-label="[^"]+"/);
  assert.match(tag, /aria-haspopup="dialog"/);
  assert.match(tag, /aria-expanded="false"/);
  // The button opens the scope modal through openScopeModal, and that global exists.
  assert.match(tag, /onclick="openScopeModal\(\)"/, 'the button is wired to openScopeModal()');
  const sb = load();
  assert.equal(typeof sb.openScopeModal, 'function', 'openScopeModal is a global function in app.js');
  sb.renderScopeButton();
  const expected = EN.header_scope_button_label.replace('%1$s', EN.header_scope_all);
  assert.equal(btn(sb).getAttribute('aria-label'), expected, 'the name is header_scope_button_label with the scope label');
  assert.equal(btn(sb).getAttribute('title'), expected);
  assert.match(btn(sb).getAttribute('aria-label'), /All/);
});

test('(a) the accessible name follows the scope: project, then project / plan, then All again', async () => {
  const sb = load();
  run(sb, `_projectsState.list = ${JSON.stringify(PROJECTS)}; _projectsState.error = '';`);
  sb.setScope('acme', '', { silent: true });
  assert.equal(btn(sb).getAttribute('aria-label'), 'Scope: acme');
  sb.setScope('acme', 'au-2', { silent: true });
  assert.equal(btn(sb).getAttribute('aria-label'), 'Scope: acme / au-2');
  sb.clearScope();
  assert.equal(btn(sb).getAttribute('aria-label'), 'Scope: All');
});

test('(a) the button is visible only on the Automata view', () => {
  const sb = load({ view: 'sessions' });
  const seen = {};
  ['sessions', 'autonomous', 'alerts', 'sessions'].forEach(view => {
    sb.navigate(view, null, true);
    seen[view] = btn(sb).style.display;
  });
  assert.equal(seen.autonomous, 'inline-flex', 'shown on Automata');
  assert.equal(seen.sessions, 'none', 'hidden on Sessions');
  assert.equal(seen.alerts, 'none', 'hidden on Alerts');
  assert.match(INDEX_HTML, /id="headerScopeBtn"[^>]*style="display:none"/, 'hidden before the first navigation');
});

test('(a) the remembered scope stays while the button is hidden', () => {
  const store = new Map();
  const sb = load({ store, view: 'sessions' });
  sb.setScope('acme', 'au-1', { silent: true });
  sb.navigate('autonomous', null, true);
  sb.navigate('alerts', null, true);
  assert.equal(btn(sb).style.display, 'none');
  assert.deepEqual(scopeOf(sb), { project: 'acme', plan: 'au-1' });
  assert.deepEqual(stored(sb), { project: 'acme', plan: 'au-1' });
  sb.navigate('autonomous', null, true);
  assert.equal(btn(sb).style.display, 'inline-flex');
  assert.equal(badge(sb).style.display, '', 'the badge is back with the button');
});

// ── (b) opening ────────────────────────────────────────────────────────────

test('(b) a click opens #scopeModal as a labelled modal dialog with focus inside and aria-expanded true', async () => {
  const sb = load();
  assert.equal(modal(sb), null, 'closed to begin with');
  const m = await open(sb);
  assert.ok(m, '#scopeModal is in the page');
  const dlg = m.querySelector('[role="dialog"]');
  assert.ok(dlg, 'role=dialog');
  assert.equal(dlg.getAttribute('aria-modal'), 'true');
  const labelId = dlg.getAttribute('aria-labelledby');
  assert.equal(labelId, 'scopeModalTitle');
  const label = sb.__dom.byId(labelId);
  assert.ok(label, 'the label element exists');
  assert.equal(label.textContent, EN.header_scope_modal_title);
  assert.ok(m.contains(sb.__dom.active()), 'focus is inside the dialog');
  assert.notEqual(sb.__dom.active(), btn(sb));
  assert.equal(btn(sb).getAttribute('aria-expanded'), 'true');
  assert.equal(keydowns(sb), 1, 'one keydown listener while it is open');
});

test('(b) opening twice never stacks a second dialog', async () => {
  const sb = load();
  await open(sb);
  await open(sb);
  assert.equal(sb.__dom.body.children.filter(c => c.id === 'scopeModal').length, 1, 'one overlay in the page');
  assert.equal(keydowns(sb), 1);
});

// ── (c) closing ────────────────────────────────────────────────────────────

test('(c) Escape closes it, focus returns to the button and aria-expanded resets', async () => {
  const sb = load();
  await open(sb);
  press(sb, 'Escape');
  assert.equal(modal(sb), null);
  assert.equal(sb.__dom.active(), btn(sb), 'focus is back on #headerScopeBtn');
  assert.equal(btn(sb).getAttribute('aria-expanded'), 'false');
  assert.equal(keydowns(sb), 0);
});

test('(c) a backdrop click closes it and returns focus', async () => {
  const sb = load();
  const m = await open(sb);
  m.dispatch('click', { target: m });
  assert.equal(modal(sb), null);
  assert.equal(sb.__dom.active(), btn(sb));
  assert.equal(btn(sb).getAttribute('aria-expanded'), 'false');
});

test('(c) a click inside the dialog does not close it, the Close button does', async () => {
  const sb = load();
  const m = await open(sb);
  const dlg = m.querySelector('[role="dialog"]');
  m.dispatch('click', { target: dlg });
  assert.ok(modal(sb), 'a click on the dialog itself leaves it open');
  const close = m.querySelector('.header-modal-close');
  assert.ok(close, 'there is a Close button');
  run(sb, close.getAttribute('onclick'));
  assert.equal(modal(sb), null);
  assert.equal(sb.__dom.active(), btn(sb));
});

test('(c) a row choice closes it and returns focus to the button', async () => {
  const sb = load();
  await open(sb);
  choose(sb, rowBy(sb, 'data-scope-project', 'zeta'));
  assert.equal(modal(sb), null);
  assert.equal(sb.__dom.active(), btn(sb));
});

test('(c) other keys leave it open; Tab stays inside the dialog', async () => {
  const sb = load();
  const m = await open(sb);
  press(sb, 'a');
  press(sb, 'Enter');
  assert.ok(modal(sb));
  const items = m.querySelectorAll('button');
  items[items.length - 1].focus();
  press(sb, 'Tab');
  assert.ok(m.contains(sb.__dom.active()), 'Tab from the last control wraps inside');
});

test('(c) no keydown listener leaks across repeated open/close cycles by any closer', async () => {
  const sb = load();
  const closers = {
    escape: () => press(sb, 'Escape'),
    backdrop: () => { const m = modal(sb); m.dispatch('click', { target: m }); },
    close: () => run(sb, modal(sb).querySelector('.header-modal-close').getAttribute('onclick')),
    row: () => choose(sb, allRow(sb)),
    api: () => run(sb, 'closeScopeModal()'),
  };
  for (let round = 0; round < 3; round++) {
    for (const name of Object.keys(closers)) {
      await open(sb);
      assert.equal(keydowns(sb), 1, `${name}: one listener while open (round ${round})`);
      closers[name]();
      assert.equal(modal(sb), null, `${name} closed it`);
      assert.equal(keydowns(sb), 0, `${name} left no listener behind (round ${round})`);
      assert.equal(sb.__dom.active(), btn(sb), `${name} returned focus`);
    }
  }
});

test('(c) closing a modal that is not open is harmless', () => {
  const sb = load();
  assert.doesNotThrow(() => run(sb, 'closeScopeModal(); closeScopeModal();'));
  press(sb, 'Escape');
  assert.equal(keydowns(sb), 0);
});

// ── (d) the rows ───────────────────────────────────────────────────────────

test('(d) the modal lists All and each project, and no plans before a project is chosen', async () => {
  const sb = load();
  await open(sb);
  assert.ok(allRow(sb), 'an All row');
  assert.equal(allRow(sb).textContent, EN.header_scope_all);
  const names = rows(sb).filter(r => r.getAttribute('data-scope-project') !== null).map(r => r.getAttribute('data-scope-project'));
  assert.deepEqual(names, ['acme', 'zeta', 'solo']);
  assert.equal(rows(sb).filter(r => r.getAttribute('data-scope-plan') !== null).length, 0, 'no plan rows yet');
  assert.equal(rows(sb).filter(r => r.getAttribute('data-scope-all-plans') !== null).length, 0);
  assert.ok(rows(sb).every(r => r.tagName === 'BUTTON'), 'every row is a real button');
});

test('(d) a chosen project shows All plans and one row per plan, with the Automation names', async () => {
  const sb = load();
  await open(sb);
  await flushAsync();
  choose(sb, rowBy(sb, 'data-scope-project', 'acme'));
  assert.ok(modal(sb), 'still open after picking a project that has plans');
  assert.ok(rowBy(sb, 'data-scope-all-plans', '1'), 'All plans');
  assert.equal(rowBy(sb, 'data-scope-all-plans', '1').textContent, EN.header_scope_all_plans);
  const plans = rows(sb).filter(r => r.getAttribute('data-scope-plan') !== null);
  assert.deepEqual(plans.map(r => r.getAttribute('data-scope-plan')), ['au-1', 'au-2']);
  assert.deepEqual(plans.map(r => r.textContent), ['Launch plan', 'Cleanup plan']);
  assert.equal(rowBy(sb, 'data-scope-project', 'acme').getAttribute('aria-expanded'), 'true');
  assert.deepEqual(scopeOf(sb), { project: '', plan: '' }, 'picking a project to browse applies nothing yet');
});

test('(d) another project replaces the plan list with its own plans', async () => {
  const sb = load();
  await open(sb);
  choose(sb, rowBy(sb, 'data-scope-project', 'acme'));
  choose(sb, rowBy(sb, 'data-scope-project', 'solo'));
  const plans = rows(sb).filter(r => r.getAttribute('data-scope-plan') !== null).map(r => r.getAttribute('data-scope-plan'));
  assert.deepEqual(plans, ['au-7']);
});

test('(d) choosing All clears the project and the plan', async () => {
  const sb = load();
  sb.setScope('acme', 'au-1', { silent: true });
  await open(sb);
  assert.equal(allRow(sb).getAttribute('aria-current'), null, 'All is not current under a scope');
  choose(sb, allRow(sb));
  assert.deepEqual(scopeOf(sb), { project: '', plan: '' });
  assert.equal(modal(sb), null);
});

test('(d) choosing rows calls setScope with the right project and plan', async () => {
  const sb = load();
  const calls = spyOnSetScope(sb);
  await open(sb);
  choose(sb, rowBy(sb, 'data-scope-project', 'zeta'));
  assert.deepEqual(calls[calls.length - 1], ['zeta', ''], 'a project with no plans applies at once');
  assert.deepEqual(scopeOf(sb), { project: 'zeta', plan: '' });

  await open(sb);
  choose(sb, rowBy(sb, 'data-scope-project', 'acme'));
  choose(sb, rowBy(sb, 'data-scope-all-plans', '1'));
  assert.deepEqual(calls[calls.length - 1], ['acme', ''], 'All plans applies the project');
  assert.deepEqual(scopeOf(sb), { project: 'acme', plan: '' });

  await open(sb);
  choose(sb, rowBy(sb, 'data-scope-project', 'acme'));
  choose(sb, rowBy(sb, 'data-scope-plan', 'au-2'));
  assert.deepEqual(calls[calls.length - 1], ['acme', 'au-2'], 'a plan row applies project and plan');
  assert.deepEqual(scopeOf(sb), { project: 'acme', plan: 'au-2' });

  await open(sb);
  choose(sb, rowBy(sb, 'data-scope-project', 'solo'));
  choose(sb, rowBy(sb, 'data-scope-plan', 'au-7'));
  assert.deepEqual(calls[calls.length - 1], ['solo', 'au-7']);
  assert.equal(calls.length, 4, 'one setScope per applied choice, none for browsing');

  await open(sb);
  choose(sb, allRow(sb));
  assert.deepEqual(scopeOf(sb), { project: '', plan: '' }, 'All clears it');
  assert.equal(modal(sb), null, 'every applied choice closed the modal');
});

test('(d) the current choice is marked with aria-current and focus lands on it', async () => {
  const sb = load();
  run(sb, `_projectsState.list = ${JSON.stringify(PROJECTS)}; _projectsState.error = '';`);
  sb.setScope('acme', 'au-2', { silent: true });
  await open(sb);
  assert.equal(rowBy(sb, 'data-scope-project', 'acme').getAttribute('aria-current'), 'true');
  assert.equal(rowBy(sb, 'data-scope-plan', 'au-2').getAttribute('aria-current'), 'true');
  assert.equal(rowBy(sb, 'data-scope-plan', 'au-1').getAttribute('aria-current'), null);
  assert.equal(sb.__dom.active().getAttribute('aria-current'), 'true');
});

// ── (e) the badge ──────────────────────────────────────────────────────────

test('(e) the badge shows only while a scope is active and clears on All', async () => {
  const sb = load();
  sb.renderScopeButton();
  assert.equal(badge(sb).style.display, 'none', 'hidden under All');
  assert.equal(badge(sb).innerHTML, '');
  await open(sb);
  choose(sb, rowBy(sb, 'data-scope-project', 'zeta'));
  assert.equal(badge(sb).style.display, '', 'shown once a project is chosen');
  assert.match(badge(sb).innerHTML, /header-scope-badge-text/);
  assert.ok(badge(sb).innerHTML.includes(EN.header_scope_badge_label), 'the badge text is localized');
  await open(sb);
  choose(sb, rowBy(sb, 'data-scope-project', 'acme'));
  choose(sb, rowBy(sb, 'data-scope-plan', 'au-1'));
  assert.equal(badge(sb).style.display, '', 'still shown for a plan');
  await open(sb);
  choose(sb, allRow(sb));
  assert.equal(badge(sb).style.display, 'none', 'All hides it');
  assert.equal(badge(sb).innerHTML, '');
  assert.equal(btn(sb).getAttribute('aria-label'), 'Scope: All');
});

test('(e) the badge follows clearScope() from the banner Clear button too', () => {
  const sb = load();
  run(sb, `_projectsState.list = ${JSON.stringify(PROJECTS)}; _projectsState.error = '';`);
  sb.setScope('acme', '', { silent: true });
  assert.equal(badge(sb).style.display, '');
  assert.match(sb._scopeBannerHTML(), /onclick="clearScope\(\)"/);
  run(sb, 'clearScope()');
  assert.equal(badge(sb).style.display, 'none');
});

// ── (f) memory ─────────────────────────────────────────────────────────────

test('(f) the choice is remembered under dw_scope and restored on a fresh sandbox', async () => {
  const store = new Map();
  const sb = load({ store });
  await open(sb);
  choose(sb, rowBy(sb, 'data-scope-project', 'acme'));
  choose(sb, rowBy(sb, 'data-scope-plan', 'au-2'));
  assert.deepEqual([...store.keys()].filter(k => k.indexOf('scope') >= 0), ['dw_scope'], 'one key');
  assert.deepEqual(stored(sb), { project: 'acme', plan: 'au-2' });

  const fresh = load({ store });
  assert.deepEqual(scopeOf(fresh), { project: 'acme', plan: 'au-2' }, 'restored before any list arrives');
  fresh.renderScopeButton();
  assert.equal(btn(fresh).getAttribute('aria-label'), 'Scope: acme / au-2');
  assert.equal(badge(fresh).style.display, '', 'the badge is back');
  await open(fresh);
  assert.equal(rowBy(fresh, 'data-scope-project', 'acme').getAttribute('aria-current'), 'true', 'and the modal marks it');

  const second = load({ store });
  second.clearScope();
  assert.deepEqual(stored(second), { project: '', plan: '' }, 'All is saved too');
  assert.deepEqual(scopeOf(load({ store })), { project: '', plan: '' });
});

test('(f) a removed project falls back to All and the fallback is saved', async () => {
  const store = new Map([['dw_scope', JSON.stringify({ project: 'gone', plan: '' })]]);
  const sb = load({ store });
  assert.equal(scopeOf(sb).project, 'gone', 'restored optimistically');
  await open(sb);
  await flushAsync();
  assert.deepEqual(scopeOf(sb), { project: '', plan: '' });
  assert.deepEqual(stored(sb), { project: '', plan: '' }, 're-saved');
  assert.equal(btn(sb).getAttribute('aria-label'), 'Scope: All');
  assert.equal(badge(sb).style.display, 'none');
  assert.deepEqual(scopeOf(load({ store })), { project: '', plan: '' }, 'a later page starts on All');
});

test('(f) a removed plan falls back to its project and the fallback is saved', async () => {
  const store = new Map([['dw_scope', JSON.stringify({ project: 'acme', plan: 'au-gone' })]]);
  const sb = load({ store });
  await open(sb);
  await flushAsync();
  assert.deepEqual(scopeOf(sb), { project: 'acme', plan: '' });
  assert.deepEqual(stored(sb), { project: 'acme', plan: '' });
});

test('(f) a project list that fails to load keeps the remembered scope', async () => {
  const store = new Map([['dw_scope', JSON.stringify({ project: 'acme', plan: 'au-1' })]]);
  const sb = load({ store });
  sb.fetch = () => Promise.resolve({ ok: false, status: 500, statusText: 'x', text: () => Promise.resolve('boom'), json: () => Promise.reject(new Error('x')) });
  await open(sb);
  await flushAsync();
  assert.deepEqual(scopeOf(sb), { project: 'acme', plan: 'au-1' }, 'a failed list proves nothing');
  assert.deepEqual(stored(sb), { project: 'acme', plan: 'au-1' });
});

test('(f) unreadable or odd saved text means All', () => {
  ['{not json', 'null', '[]', '"x"', JSON.stringify({ project: 7, plan: 'x' }), JSON.stringify({ project: '', plan: 'x' })].forEach(raw => {
    const sb = load({ store: new Map([['dw_scope', raw]]) });
    assert.deepEqual(scopeOf(sb), { project: '', plan: '' }, raw);
  });
});

test('(f) blocked localStorage never throws and the scope still works in memory', async () => {
  const blocked = {
    getItem() { throw new Error('SecurityError'); },
    setItem() { throw new Error('QuotaExceededError'); },
    removeItem() { throw new Error('SecurityError'); },
  };
  // The scope code's own storage access is what is under test. The project list
  // is already in memory, so the modal does not need apiFetch (which reads its
  // auth token from storage for every request, scope or not).
  const withList = sb => run(sb, `_projectsState.list = ${JSON.stringify(PROJECTS)}; _projectsState.error = '';`);
  const sb = load();
  sb.localStorage = blocked;
  withList(sb);
  assert.doesNotThrow(() => sb.renderScopeButton());
  await assert.doesNotReject(open(sb));
  assert.doesNotThrow(() => choose(sb, rowBy(sb, 'data-scope-project', 'zeta')));
  assert.deepEqual(scopeOf(sb), { project: 'zeta', plan: '' }, 'kept in memory');
  assert.equal(btn(sb).getAttribute('aria-label'), 'Scope: zeta');
  assert.doesNotThrow(() => sb.clearScope());

  // storage whose accessor itself throws (a browser with site data blocked)
  const sb2 = load();
  Object.defineProperty(sb2, 'localStorage', { get() { throw new Error('blocked'); }, configurable: true });
  withList(sb2);
  assert.doesNotThrow(() => sb2.setScope('acme', 'au-1', { silent: true }));
  assert.deepEqual(scopeOf(sb2), { project: 'acme', plan: 'au-1' });
  assert.doesNotThrow(() => sb2.renderScopeButton());
  await assert.doesNotReject(open(sb2));
  assert.doesNotThrow(() => sb2.clearScope());

  // and a fresh page whose storage throws on read starts on All
  const sb3 = load();
  sb3.localStorage = blocked;
  assert.doesNotThrow(() => sb3._scopeBootKick());
  assert.deepEqual(scopeOf(sb3), { project: '', plan: '' });
});
