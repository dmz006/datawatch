'use strict';
// app.js — Plans browser release-plan surface: release badge + sort order,
// archive (reason + confirm) / restore, theme layout (blocks, unplaced,
// add/theme/place), release migration (preview → mark → approve with the
// preview id), row-plan migration (preview → approve), the unattributed
// list/per-row/bulk attach to an Automaton's release plan (server refusal
// surfaces per row), and five-file locale parity for the new keys.
//
// The server contract is exercised through the fetch stub (the same shape
// the real plans_release_api.go endpoints return): attach is a per-plan
// /attach POST with an ids array, migration is a two-step preview then
// approve carrying the preview id, archive is a POST with the reason body.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');

const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const HOSTILE = '<img src=x onerror=alert(1)>';
const ESCAPED = '&lt;img src=x onerror=alert(1)&gt;';

const bundle = lang => JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
const EN = bundle('en');
function en(key, vars) {
  let s = String(EN[key] || key);
  s = s.replace(/%(\d+)\$[sd]/g, (m, n) => (vars && vars[Number(n) - 1] !== undefined ? String(vars[Number(n) - 1]) : m));
  return s;
}

const PLAN_REL = { title: 'Release 2026-W45', id: 'rel-2026-45', uid: 'r1', goal: 'Weekly release', state: 'in_progress', order: 1, plan_kind: 'release', archived: false,
  progress: { done: 1, total: 4, percent: 25 }, points: 1, automation_ids: [], automaton_ids: ['au-1'], finding_count: 0 };
const PLAN_ORD = { title: 'Ship the login flow', id: 'login-flow', uid: 'u1', goal: 'Users sign in with a passkey', state: 'in_progress', order: 2, plan_kind: 'plan', archived: false,
  progress: { done: 1, total: 3, percent: 33 }, points: 2.5, automation_ids: ['chain-1'], automaton_ids: ['a1'], finding_count: 1 };
const PLAN_ARC = { title: 'Release 2026-W01', id: 'rel-2026-01', uid: 'r2', goal: 'Old release', state: 'archived', order: 3, plan_kind: 'release', archived: true,
  progress: { done: 1, total: 1, percent: 100 }, points: 0, automation_ids: [], automaton_ids: [], finding_count: 0 };
const BACKLOG = { title: 'Idea with no Automaton', id: 'idea', uid: 'b1', goal: 'Think about it', state: 'open', order: 1, archived: false,
  progress: { done: 0, total: 0, percent: 0 }, points: 0, automation_ids: [], automaton_ids: [], finding_count: 0 };

const AUTOMATA = [
  { title: 'Core item', id: 'au-1', state: 'active', run: 'next', position: 1, points: 1 },
  { title: 'Perf item', id: 'au-2', state: 'completed', run: 'completed', position: 2, points: 1 },
];
const UATRUE = [
  { title: 'Orphan one', id: 'ua-1', state: 'active', run: 'next', holding_plan: 'legacy-1', suggested_release: 'rel-2026-45', suggested_theme: 'Core' },
  { title: HOSTILE, id: 'ua-2', state: 'blocked', run: 'stalled' },
];
const THEMES = {
  themes: [
    { id: 'th-core', name: 'Core', items: [{ kind: 'automaton', id: 'au-1', title: 'Core item' }, { kind: 'automation', id: 'reltrain', title: 'Release train' }] },
    { id: 'th-perf', name: 'Perf', items: [{ kind: 'automaton', id: 'au-2', title: 'Perf item' }] },
  ],
  unplaced: [{ kind: 'automaton', id: 'au-3', title: 'Floating item' }],
};

// load builds the app with the Projects tab active; plansState is the mutable
// server-side state the /plans, /archive and /restore routes see.
function load(opts = {}) {
  const plansState = (opts.plans || [PLAN_ORD, PLAN_REL, PLAN_ARC]).map(p => Object.assign({}, p));
  const view = makeStubElement(() => {});
  view.insertAdjacentHTML = () => {};
  const list = { innerHTML: '' };
  const fields = {};
  let serverThemes = JSON.parse(JSON.stringify(THEMES));
  const sandbox = buildSandbox({ byId: { automataPanel: view, view, plansBrowserList: list } });
  const realGet = sandbox.document.getElementById.bind(sandbox.document);
  sandbox.document.getElementById = id => (fields[id] || realGet(id));
  sandbox.__calls = [];
  const ok = b => Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(b), text: () => Promise.resolve(JSON.stringify(b)) });
  const callLog = (u, o, tag) => { o = o || {}; sandbox.__calls.push({ url: u, method: o.method || 'GET', body: o.body ? JSON.parse(o.body) : null, tag }); };
  const find = id => plansState.find(p => p.id === id);
  sandbox.fetch = (url, o) => {
    const u = String(url);
    o = o || {};
    if (u.startsWith('/locales/')) return ok(bundle(u.slice('/locales/'.length, -5)));
    const pm = u.match(/^\/api\/plans\/([^/?]+)\/(automata|tasks|themes|attach|archive|restore)/);
    if (pm) {
      const id = pm[1]; const ep = pm[2];
      if (ep === 'automata') { callLog(u, o, 'automata'); return ok({ automata: AUTOMATA, page: { total: AUTOMATA.length } }); }
      if (ep === 'tasks') { callLog(u, o, 'tasks'); return ok({ tasks: [], page: { total: 0 } }); }
      if (ep === 'themes') {
        callLog(u, o, 'themes');
        if ((o.method || 'GET') === 'GET') return ok(THEMES);
        const body = o.body ? JSON.parse(o.body) : { themes: [] };
        const findItem = (kind, id) => {
          for (const th of serverThemes.themes) { const f = (th.items || []).find(m => m && m.id === id); if (f) return f; }
          const f = (serverThemes.unplaced || []).find(m => m && m.id === id);
          return f ? f : { kind, id, title: String(id), state: '' };
        };
        const held = (m) => (body.themes || []).some(t2 => (t2.automata || []).indexOf(m.id) !== -1 || (t2.automations || []).indexOf(m.id) !== -1);
        serverThemes = {
          themes: (body.themes || []).map(t2 => ({
            id: String(t2.id || ''), name: String(t2.name || ''),
            items: [].concat((t2.automata || []).map(id => findItem('automaton', id)), (t2.automations || []).map(id => findItem('automation', id))),
          })),
          unplaced: (serverThemes.unplaced || []).filter(m => !held(m)),
        };
        return ok({ themes: serverThemes.themes, unplaced: serverThemes.unplaced });
      }
      if (ep === 'attach') {
        callLog(u, o, 'attach');
        const r = opts.attach || { results: [] };
        opts.__attachCalls = (opts.__attachCalls || 0) + 1;
        return ok(r);
      }
      if (ep === 'archive') { callLog(u, o, 'archive'); const p = find(id); if (p) p.archived = true; return ok({ archived: true }); }
      if (ep === 'restore') { callLog(u, o, 'restore'); const p = find(id); if (p) p.archived = false; return ok({ archived: false }); }
    }
    if (u.includes('/api/plans/unattributed')) {
      const rows = opts.unattributed || [];
      callLog(u, o, 'unattributed');
      return ok({ page: { total: rows.length, limit: 20, offset: 0 }, automata: rows });
    }
    if (u.includes('/plans?')) {
      callLog(u, o, 'plans');
      const scope = /archived=only/.test(u) ? 'only' : /archived=include/.test(u) ? 'include' : 'exclude';
      const vis = plansState.filter(p => (scope === 'include' ? true : (scope === 'only' ? p.archived : !p.archived)));
      return ok({ project: 'acme', plans: vis, page: { total: vis.length }, backlog: { plans: [BACKLOG], page: { total: 1 } } });
    }
    if (u === '/api/plans/migrate') {
      const body = o.body ? JSON.parse(o.body) : {};
      callLog(u, o, body.apply ? 'migrate-approve' : 'migrate-preview');
      if (body.apply) return ok({ note: 'Rel note' });
      return ok(Object.assign({
        preview_id: 'pv-1',
        note: 'One new release plan, 1 attached, 1 unplaced, 1 conflict, 1 unchanged, 1 skipped.',
        created: [{ kind: 'release', title: 'Release 2026-W46' }],
        attached: [{ kind: 'automaton', id: 'au-x', title: 'Migration item', theme: 'Core' }],
        unplaced: [{ kind: 'automaton', id: 'au-y', title: 'Floating new item' }],
        conflicts: [{ kind: 'automaton', id: 'au-z', title: 'Both hold it', reason: 'two holders' }],
        unchanged: [{ kind: 'automaton', id: 'au-w', title: 'Already there' }],
        skipped: [{ kind: 'automaton', id: 'au-v', title: 'Not eligible' }],
      }, opts.migrateExtra || {}));
    }
    if (u === '/api/plans/migrate/row-plans') {
      const body = o.body ? JSON.parse(o.body) : {};
      callLog(u, o, body.apply ? 'rowplans-approve' : 'rowplans-preview');
      if (body.apply) return ok({ note: 'Rows done' });
      return ok({
        preview_id: 'rpv-1',
        note: '3 rows become themes, 2 backlog items, 1 waiting, 1 conflict, 1 unchanged, 0 skipped.',
        link_rows: [{ row_id: 'pl-42', title: 'Passkey registry row' }],
        backlog_items: [{ row_id: 'pl-43', title: 'Loose row' }],
        waiting: [{ row_id: 'pl-44', title: 'Waiting row' }],
        conflicts: [{ row_id: 'pl-45', title: 'Double-linked row', reason: 'two plans' }],
        unchanged: [{ row_id: 'pl-46', title: 'Done row' }],
        skipped: [],
      });
    }
    if (u.startsWith('/api/projects')) return ok([{ name: 'acme', dir: '/work/acme' }]);
    if (u.includes('/api/plans')) callLog(u, o, 'fallback');
    return ok({});
  };
  sandbox.__timers = [];
  sandbox.setTimeout = fn => { sandbox.__timers.push(fn); return sandbox.__timers.length; };
  sandbox.clearTimeout = id => { if (id) sandbox.__timers[id - 1] = null; };
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  vm.runInContext("state.activeView = 'autonomous'; _automataState.tab = 'projects'", sandbox);
  return { sb: sandbox, list, fields, plansState, en };
}

// openProjects loads the project list, selects acme and loads the plans page.
async function openProjects(world, opts) {
  const sb = world.sb;
  await sb._projectsLoad();
  vm.runInContext("_projectsState.selected = 'acme'", sb);
  await sb._plansBrowserLoadSelected();
  await flushAsync();
}
const pr = (sb, expr) => vm.runInContext(expr, sb);
const listHTML = world => vm.runInContext('plansBrowserListHTML(_plansBrowser)', world.sb);
const calls = world => world.sb.__calls;
const idx = (h, s, from) => h.search(new RegExp(s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'gm')) === -1 ? h.indexOf(s, from !== undefined ? from : 0) : h.indexOf(s, from !== undefined ? from : 0);
const callByTag = (world, tag) => calls(world).filter(c => c.tag === tag);

test('release plans carry the badge, sorted first; ordinary plans do not', async (t) => {
  const w = load({ plans: [PLAN_ORD, PLAN_REL, PLAN_ARC] });
  await openProjects(w);
  const h = listHTML(w);
  const badge = en('plans_release_badge');
  assert.ok(h.indexOf(badge) !== -1, 'badge label present: ' + badge);
  const relAt = h.indexOf('Release 2026-W45');
  const ordAt = h.indexOf('Ship the login flow');
  assert.ok(relAt !== -1 && ordAt !== -1, 'both plans render');
  assert.ok(callByTag(w, 'plans').length >= 1, 'plans fetched');
  const firstBadge = h.indexOf('plans-release-badge');
  assert.ok(firstBadge > -1 && firstBadge < ordAt, 'badge attached to the release plan, before the ordinary one');
  const ordSegment = h.slice(ordAt, h.indexOf('Idea with no Automaton') === -1 ? h.length : h.indexOf('Idea with no Automaton'));
  assert.ok(ordSegment.indexOf('plans-release-badge') === -1, 'no badge on the ordinary plan');
  assert.ok(h.indexOf('Release 2026-W01') === -1, 'archived plan hidden in the default (exclude) list');
});

test('archive asks for a reason, confirm posts it; scope=only shows restore; restore posts', async (t) => {
  const w = load({ plans: [PLAN_ORD, PLAN_REL, PLAN_ARC] });
  await openProjects(w);
  const sb = w.sb;
  let h = listHTML(w);
  assert.ok(h.indexOf('plans-release-archive') !== -1, 'archive button rendered');
  assert.ok(h.indexOf('plans-release-restore') === -1, 'no restore on a live plan');
  await sb.plansReleaseArchiveAsk('login-flow');
  await flushAsync();
  h = listHTML(w);
  assert.ok(h.indexOf('plans-release-archive-ask') !== -1, 'reason input appears');
  assert.ok(h.indexOf('plans-release-archive-confirm') !== -1, 'confirm button');
  assert.ok(h.indexOf('plans-release-archive-cancel') !== -1, 'cancel button');
  await sb.plansReleaseArchiveReason('retired for the record');
  const p = sb.plansReleaseArchiveSubmit('login-flow');
  await flushAsync();
  if (p && p.catch) p.catch(() => {});
  const arc = callByTag(w, 'archive');
  assert.ok(arc.length === 1, 'one archive POST: ' + JSON.stringify(arc));
  assert.equal(arc[0].url, '/api/plans/login-flow/archive');
  assert.deepEqual(arc[0].body, { reason: 'retired for the record' });
  assert.equal(w.plansState.find(x => x.id === 'login-flow').archived, true, 'server state flipped');
  h = listHTML(w);
  assert.ok(h.indexOf('Ship the login flow') === -1, 'archived plan gone from the default (exclude) list');
  await sb.plansReleaseSetArchived('only');
  await flushAsync();
  h = listHTML(w);
  assert.ok(h.indexOf('Ship the login flow') !== -1, 'visible in scope=only');
  assert.ok(h.indexOf('plans-release-restore') !== -1, 'restore button on the archived plan');
  const p2 = sb.plansReleaseRestore('login-flow');
  await flushAsync();
  if (p2 && p2.catch) p2.catch(() => {});
  const res = callByTag(w, 'restore');
  assert.ok(res.length === 1, 'one restore POST: ' + JSON.stringify(res));
  assert.equal(res[0].url, '/api/plans/login-flow/restore');
  assert.deepEqual(res[0].body, {});
  assert.equal(w.plansState.find(x => x.id === 'login-flow').archived, false, 'restored');
});

test('themes render as ordered blocks with an unplaced group; add-theme and place-in-theme PUT the full layout', async (t) => {
  const w = load({ plans: [PLAN_ORD, PLAN_REL, PLAN_ARC] });
  await openProjects(w);
  const sb = w.sb;
  pr(sb, String.raw`_plansLoadDetail('rel-2026-45')`);
  await flushAsync();
  sb._i18n.bundle = bundle('en'); // keep EN for assertions
  let themes = pr(sb, String.raw`plansReleaseThemesHTML(_plansBrowser, 'rel-2026-45')`);
  assert.ok(themes.indexOf('plans-release-themes') !== -1, 'themes container');
  assert.ok(themes.indexOf('plans-release-theme-block') !== -1, 'theme block');
  assert.ok(themes.indexOf('Core') !== -1 && themes.indexOf('Core item') !== -1, 'theme name + member');
  assert.ok(themes.indexOf('Release train') !== -1, 'automation member rendered in its theme');
  assert.ok(themes.indexOf('plans-release-unplaced-block') !== -1, 'unplaced group');
  assert.ok(themes.indexOf('Floating item') !== -1, 'unplaced member rendered');
  // Add a theme: local copy grows by one, and the PUT carries the whole layout.
  await sb.plansReleaseThemeAdd('rel-2026-45');
  await flushAsync();
  const put = callByTag(w, 'themes').filter(c => c.method === 'PUT');
  assert.ok(put.length >= 1, 'theme layout PUT sent');
  const body = put[put.length - 1].body;
  assert.ok(Array.isArray(body.themes) && body.themes.length === 3, 'full layout with the new theme: ' + JSON.stringify(body));
  const names = body.themes.map(x => x.name);
  assert.deepEqual(names.slice(0, 2), ['Core', 'Perf'], 'order preserved');
  assert.equal(names[2], en('plans_release_theme_new', [3]), 'new theme name from the locale');
  assert.deepEqual(body.themes[0].automata, ['au-1'], 'automaton ids per theme');
  assert.deepEqual(body.themes[0].automations, ['reltrain'], 'automation ids per theme');
  themes = pr(sb, String.raw`plansReleaseThemesHTML(_plansBrowser, 'rel-2026-45')`);
  assert.ok(themes.indexOf(en('plans_release_theme_new', [3])) !== -1, 'new theme rendered after the save');
  // Place the unplaced Automaton into an existing theme: the PUT shows it inside Core.
  w.fields['plansReleasePlaceSel_rel-2026-45_0'] = { value: 'Core' };
  await sb.plansReleasePlaceGo('rel-2026-45', 0);
  await flushAsync();
  const put2 = callByTag(w, 'themes').filter(c => c.method === 'PUT');
  assert.ok(put2.length >= 2, 'place PUT sent');
  assert.deepEqual(put2[put2.length - 1].body.themes[0].automata, ['au-1', 'au-3'], 'unplaced item joins Core, order kept');
});

test('unattributed rows: per-row attach posts ids+theme to the chosen plan and renders the server results', async (t) => {
  const w = load({ plans: [PLAN_ORD, PLAN_REL, PLAN_ARC], unattributed: UATRUE,
    attach: { results: [{ id: 'ua-1', title: 'Orphan one', status: 'attached', theme: 'Core' }, { id: 'ua-2', status: 'refused', error: 'held by ' + HOSTILE }] } });
  await openProjects(w);
  const sb = w.sb;
  await (sb._plansBrowserEnsureUnattributed && sb._plansBrowserEnsureUnattributed());
  await flushAsync();
  let sec = pr(sb, 'plansReleaseUnattributedSectionHTML()');
  assert.ok(sec.indexOf('plans-release-unattributed') !== -1, 'section renders');
  assert.ok(sec.indexOf(en('plans_release_unattributed_title')) !== -1, 'section title');
  assert.ok(sec.indexOf('(2)') !== -1, 'count shown');
  assert.ok(sec.indexOf('Orphan one') !== -1, 'row title');
  assert.ok(sec.indexOf('ua-2') !== -1, 'second row');
  assert.ok(sec.indexOf(ESCAPED) !== -1 && sec.indexOf(HOSTILE) === -1, 'hostile title escaped in the list');
  assert.ok(sec.indexOf('plans-release-unattributed-attach-btn') !== -1, 'attach action');
  assert.ok(sec.indexOf('rel-2026-45') !== -1 && sec.indexOf('Core') !== -1, 'suggestion rendered');
  // Open the row form: plan select + theme input + explicit confirm.
  await sb.plansReleaseUnattribAttachToggle(0);
  await flushAsync();
  sec = pr(sb, 'plansReleaseUnattributedSectionHTML()');
  assert.ok(sec.indexOf('plans-release-unattributed-attach') !== -1, 'attach panel');
  assert.ok(sec.indexOf('plansReleaseRowPlan_ua-1') !== -1, 'plan select');
  assert.ok(sec.indexOf('plansReleaseRowTheme_ua-1') !== -1, 'theme input');
  assert.ok(sec.indexOf('rel-2026-45') !== -1, 'plan option for the release plan');
  // EN confirm text contains an apostrophe that escHtml renders as &#39;.
  const confirmTxt = en('plans_release_unattributed_attach_confirm').replace(/'/g, '&#39;');
  assert.ok(sec.indexOf(confirmTxt) !== -1, 'explicit confirm button');
  await sb.plansReleaseUnattribRowPlan('ua-1', 'rel-2026-45');
  w.fields['plansReleaseRowTheme_ua-1'] = { value: 'Core' };
  const p = sb.plansReleaseUnattribAttachGo(0);
  await flushAsync();
  if (p && p.catch) p.catch(() => {});
  const at = callByTag(w, 'attach');
  assert.ok(at.length === 1, 'attach POST: ' + JSON.stringify(at));
  assert.equal(at[0].url, '/api/plans/rel-2026-45/attach');
  assert.ok(Array.isArray(at[0].body.ids) && at[0].body.ids.length === 1 && at[0].body.ids[0] === 'ua-1', 'ids array form: ' + JSON.stringify(at[0].body));
  assert.equal(at[0].body.kind, 'automaton');
  assert.equal(at[0].body.theme, 'Core');
  sec = pr(sb, 'plansReleaseUnattributedSectionHTML()');
  assert.ok(sec.indexOf('plans-release-unattributed-results') !== -1, 'results block');
  assert.ok(sec.indexOf('data-attach-result="ua-1"') !== -1, 'per-row result line');
  assert.ok(sec.indexOf('data-status="attached"') !== -1, 'attached status');
  assert.ok(sec.indexOf('data-status="refused"') !== -1, 'refused status on the hostile row');
  assert.ok(sec.indexOf(en('plans_release_attach_attached')) !== -1, 'attached label');
  assert.ok(sec.indexOf(en('plans_release_attach_refused')) !== -1, 'refused label');
  assert.ok(sec.indexOf(ESCAPED) !== -1, 'server refusal text escaped');
  assert.ok(sec.indexOf(HOSTILE) === -1, 'no raw markup from the refusal text');
});

test('unattributed bulk: select-all + plan + theme posts every row with one ids array; per-row refusals stay visible', async (t) => {
  const w = load({ plans: [PLAN_ORD, PLAN_REL, PLAN_ARC], unattributed: UATRUE,
    attach: { results: [{ id: 'ua-1', title: 'Orphan one', status: 'attached', theme: 'Bulk' }, { id: 'ua-2', status: 'refused', error: 'owned else' }] } });
  await openProjects(w);
  const sb = w.sb;
  await (sb._plansBrowserEnsureUnattributed && sb._plansBrowserEnsureUnattributed());
  await flushAsync();
  await sb.plansReleaseUnattribSelectAll(true);
  await sb.plansReleaseUnattribRowSelect('__bulkPlan', 'rel-2026-45');
  await flushAsync();
  w.fields['plansReleaseBulkTheme'] = { value: 'Bulk theme' };
  const p = sb.plansReleaseUnattribBulkGo();
  await flushAsync();
  if (p && p.catch) p.catch(() => {});
  const at = callByTag(w, 'attach');
  assert.ok(at.length === 1, 'one bulk POST: ' + JSON.stringify(at));
  assert.equal(at[0].url, '/api/plans/rel-2026-45/attach');
  assert.deepEqual(at[0].body.ids.slice().sort(), ['ua-1', 'ua-2'], 'both rows in one ids array: ' + JSON.stringify(at[0].body));
  assert.equal(at[0].body.theme, 'Bulk theme');
  const sec = pr(sb, 'plansReleaseUnattributedSectionHTML()');
  assert.ok(sec.indexOf('data-status="attached"') !== -1, 'attached row listed');
  assert.ok(sec.indexOf('data-status="refused"') !== -1, 'refused row still listed');
  assert.ok(sec.indexOf('owned else') !== -1, 'refusal reason shown per row');
  const sel = pr(sb, '_plansUnattribSel');
  assert.equal(sel.all, false, 'selection cleared after the bulk round');
  assert.equal(Object.keys(sel.ids).length, 0, 'no ids left selected');
});

test('attach without a chosen plan reports the error without a POST', async (t) => {
  const w = load({ plans: [PLAN_ORD, PLAN_REL, PLAN_ARC], unattributed: UATRUE });
  await openProjects(w);
  const sb = w.sb;
  await (sb._plansBrowserEnsureUnattributed && sb._plansBrowserEnsureUnattributed());
  await flushAsync();
  await sb.plansReleaseUnattribAttachToggle(0);
  await flushAsync();
  let p = sb.plansReleaseUnattribAttachGo(0);
  await flushAsync();
  if (p && p.catch) p.catch(() => {});
  assert.equal(callByTag(w, 'attach').length, 0, 'no attach POST without a plan');
  const sec = pr(sb, 'plansReleaseUnattributedSectionHTML()');
  assert.ok(sec.indexOf(en('plans_release_unattributed_plan_required')) !== -1, 'plan-required message shown');
});

test('release migration: preview lists the report, marks ride along, approve carries the preview id', async (t) => {
  const w = load({ plans: [PLAN_ORD, PLAN_REL, PLAN_ARC] });
  await openProjects(w);
  const sb = w.sb;
  const mig = () => pr(sb, 'plansReleaseMigrateHTML()');
  assert.ok(mig().indexOf('plans-release-migrate') !== -1, 'panel renders');
  assert.ok(mig().indexOf(en('plans_release_migrate_title')) !== -1, 'panel title');
  const pv = sb.plansReleaseMigratePreview;
  assert.ok(typeof pv === 'function', 'preview action present');
  pr(sb, String.raw`_plansReleaseMigrate.nextRelease = 'rel-2026-46'`);
  await sb.plansReleaseMigratePreview();
  await flushAsync();
  const prev = callByTag(w, 'migrate-preview');
  assert.ok(prev.length === 1, 'one preview POST: ' + JSON.stringify(prev));
  assert.equal(prev[0].url, '/api/plans/migrate');
  assert.equal(prev[0].body.apply, false, 'preview is non-apply');
  assert.equal(prev[0].body.next_release, 'rel-2026-46');
  assert.equal(prev[0].body.mode, 'release');
  let m = mig();
  assert.ok(m.indexOf('Release 2026-W46') !== -1, 'created item');
  assert.ok(m.indexOf('Migration item') !== -1, 'attached item');
  assert.ok(m.indexOf('Floating new item') !== -1, 'unplaced item');
  assert.ok(m.indexOf('Both hold it') !== -1, 'conflict listed');
  assert.ok(m.indexOf('plans-release-migrate-approve') !== -1, 'explicit approve button');
  assert.ok(m.indexOf(en('plans_release_migrate_hint')) !== -1, 'hint under approve');
  // Mark the attached Automaton for the next release with a theme.
  await sb.plansReleaseMigrateMark(0, true);
  await sb.plansReleaseMigrateTheme(0, 'Core');
  await flushAsync();
  await sb.plansReleaseMigrateApprove();
  await flushAsync();
  const app = callByTag(w, 'migrate-approve');
  assert.ok(app.length === 1, 'one approve POST: ' + JSON.stringify(app));
  assert.equal(app[0].body.apply, true, 'approve is apply');
  assert.equal(app[0].body.preview_id, 'pv-1', 'approve carries the preview id');
  assert.equal(Array.isArray(app[0].body.marks), true, 'marks array');
  assert.equal(app[0].body.marks.length, 1, 'only the marked item: ' + JSON.stringify(app[0].body.marks));
  assert.equal(app[0].body.marks[0].release, 'rel-2026-46');
  assert.equal(app[0].body.marks[0].theme, 'Core');
  m = mig();
  assert.ok(m.indexOf('Rel note') !== -1, 'server note after apply');
  assert.ok(m.indexOf('plans-release-migrate-approve') === -1, 'approve button gone after apply');
});

test('row-plan migration: preview then approve with its own preview id', async (t) => {
  const w = load({ plans: [PLAN_ORD, PLAN_REL, PLAN_ARC] });
  await openProjects(w);
  const sb = w.sb;
  const rp = () => pr(sb, 'plansReleaseRowPlansHTML()');
  assert.ok(rp().indexOf('plans-release-rowplans') !== -1, 'row-plans panel renders');
  assert.ok(rp().indexOf(en('plans_release_rowplans_title')) !== -1, 'title');
  await sb.plansReleaseRowPlansPreview();
  await flushAsync();
  const prev = callByTag(w, 'rowplans-preview');
  assert.ok(prev.length === 1, 'preview POST: ' + JSON.stringify(prev));
  assert.equal(prev[0].url, '/api/plans/migrate/row-plans');
  assert.equal(prev[0].body.apply, false);
  assert.equal(prev[0].body.project, 'acme');
  let h = rp();
  assert.ok(h.indexOf('Passkey registry row') !== -1, 'linked row listed');
  assert.ok(h.indexOf('Double-linked row') !== -1, 'conflict listed');
  assert.ok(h.indexOf('plans-release-rowplans-approve') !== -1, 'approve button');
  await sb.plansReleaseRowPlansApprove();
  await flushAsync();
  const app = callByTag(w, 'rowplans-approve');
  assert.ok(app.length === 1, 'approve POST: ' + JSON.stringify(app));
  assert.equal(app[0].body.apply, true);
  assert.equal(app[0].body.preview_id, 'rpv-1', 'row-plans preview id');
  h = rp();
  assert.ok(h.indexOf('Rows done') !== -1, 'note after apply');
});

test('the five locale files all carry the new plans_release_ keys', async (t) => {
  const files = LANGS.map(bundle);
  for (const key of Object.keys(EN).filter(k => k.startsWith('plans_release_'))) {
    for (let i = 0; i < LANGS.length; i++) {
      const v = files[i][key];
      assert.ok(typeof v === 'string' && v.length > 0, LANGS[i] + ' missing/' + key);
    }
  }
  // de actually translates at least the badge key differently from EN.
  assert.notEqual(bundle('de').plans_release_badge, EN.plans_release_badge, 'de badge should not copy EN');
});
