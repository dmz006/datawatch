'use strict';
// The header scope applied to the Automata pages' sub-tabs.
//
// The scope is what the Automata pages are filtered to: All, one project, or
// one plan (a project arc keyed by its automation id). The Projects pages
// already honored it (see app-projects-scope.test.js and
// app-header-scope.test.js for the model itself). This file pins the sub-tab
// behavior layered on top of that model:
//   (a) the Automations sub-tab: _automationInScope keeps only the
//       Automations of the scoped project, or of the scoped plan;
//   (b) the Automations sub-tab list filter: automationListVisible drops
//       out-of-scope Automations from the visible set;
//   (c) the Automata list: _automataBaseList keeps only the Automata of the
//       scoped project or plan;
//   (d) the Templates tab: global, and left alone by the scope;
//   (e) the Events sub-tab: the scope's project is sent as project=, a plan
//       narrows a row to the plan's Automation or its Automata, and a
//       project-only scope leaves the rows to the server-side filter;
//   (f) the Inbox: the header scope wins over the in-page project picker, and
//       a plan narrows an item to the plan's Automations or its Automation;
//   (g) the notes: filtered sub-tabs show the standard banner (with a Clear
//       button) and the inherently global sub-tabs (Resources, Progress,
//       Housekeeping) and the Templates tab show their one-line note;
//   (h) locales: header_scope_global_note exists in all five bundles.
//
// No daemon, no tmux: the real app.js runs in a vm sandbox.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-automata-subtab-scope.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const WEB = __dirname;
const APP_JS = path.join(WEB, 'app.js');
const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];

function loadAppSandbox() {
  const sandbox = buildSandbox({ byId: {} });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  return sandbox;
}

// setScope resets the plan-member cache, so seed it again after a plan scope
// when the assertions read the plan's member Automata. This avoids the real
// /api/automations/<plan> fetch (a late reply would be dropped by the token
// guard anyway; we want a deterministic ready set).
function seedPlanMembers(sandbox, prdIds) {
  vm.runInContext(
    `_scopePlanCache = { key: 'acme\\nau-1', ids: new Set(${JSON.stringify(prdIds)}), status: 'ready' };`,
    sandbox
  );
}

function setScope(sandbox, project, plan) {
  vm.runInContext(`setScope(${JSON.stringify(project)}, ${JSON.stringify(plan)}, { silent: true });`, sandbox);
}

// A sandbox function returns a foreign array; Array.from copies it into a Node
// array so deepEqual compares values, not cross-realm prototypes.
function idsOf(arr) { return Array.from(arr).map(x => x.id); }

test('(a) the Automations sub-tab keeps the scoped project, or the scoped plan', () => {
  const s = loadAppSandbox();
  const acme = { id: 'au-1', project: 'acme' };
  const zeta = { id: 'au-9', project: 'zeta' };
  const other = { id: 'au-2', project: 'acme' };
  const st = { statusFilter: new Set(), search: '', historyOn: false };

  assert.equal(s._automationInScope(acme), true, 'All scope: every Automation is in scope');
  assert.equal(s._automationInScope(zeta), true, 'All scope: every Automation is in scope');

  setScope(s, 'acme', '');
  assert.equal(s._automationInScope(acme), true, 'project scope: the scoped project matches');
  assert.equal(s._automationInScope(other), true, 'project scope: the scoped project matches');
  assert.equal(s._automationInScope(zeta), false, 'project scope: another project is out');

  setScope(s, 'acme', 'au-1');
  assert.equal(s._automationInScope(acme), true, 'plan scope: the plan itself matches');
  assert.equal(s._automationInScope(other), false, 'plan scope: a sibling of the project is out');
  assert.equal(s._automationInScope(zeta), false, 'plan scope: another project is out');
});

test('(b) the Automations sub-tab list filter drops out-of-scope Automations', () => {
  const s = loadAppSandbox();
  const acme = { id: 'au-1', project: 'acme', status: 'running', name: 'A' };
  const zeta = { id: 'au-9', project: 'zeta', status: 'running', name: 'Z' };
  vm.runInContext(`_shippedState.enabled = false;`, s);
  const st = { search: '', historyOn: false };

  setScope(s, 'acme', '');
  assert.deepEqual(idsOf(s.automationListVisible([acme, zeta], st)), ['au-1'], 'the scoped project only');

  setScope(s, '', '');
  assert.deepEqual(idsOf(s.automationListVisible([acme, zeta], st)), ['au-1', 'au-9'], 'All shows both');
});

test('(c) the Automata list keeps the scoped project, or the scoped plan', () => {
  const s = loadAppSandbox();
  vm.runInContext(`state.activeView = 'autonomous'; _automataState.tab = 'automata';`, s);
  const p1 = { id: 'p1', project: 'acme', status: 'running', is_template: false, title: 'P1' };
  const p2 = { id: 'p2', project: 'acme', automation_id: 'au-1', status: 'running', is_template: false, title: 'P2' };
  const pOther = { id: 'p3', project: 'acme', automation_id: 'au-2', status: 'running', is_template: false, title: 'P3' };
  const pZeta = { id: 'p4', project: 'zeta', automation_id: 'au-9', status: 'running', is_template: false, title: 'P4' };
  vm.runInContext(`_automataState.allPrds = ${JSON.stringify([p1, p2, pOther, pZeta])};`, s);

  // All scope: every active Automation shows.
  setScope(s, '', '');
  assert.deepEqual(idsOf(s._automataBaseList()).sort(), ['p1', 'p2', 'p3', 'p4'], 'All shows every active Automaton');

  // Project scope: only the scoped project's Automata show.
  setScope(s, 'acme', '');
  assert.deepEqual(idsOf(s._automataBaseList()).sort(), ['p1', 'p2', 'p3'], 'project scope keeps only the scoped project');

  // Plan scope: only the plan's Automation and its member Automata show.
  setScope(s, 'acme', 'au-1');
  seedPlanMembers(s, ['p1', 'p2']);
  assert.deepEqual(idsOf(s._automataBaseList()).sort(), ['p1', 'p2'], 'plan scope keeps the plan and its members only');
});

test('(d) the Templates tab is global and is never scope-filtered', () => {
  const s = loadAppSandbox();
  vm.runInContext(`state.activeView = 'autonomous'; _automataState.tab = 'templates';`, s);
  const t1 = { id: 't1', project: 'acme', status: 'running', is_template: true, title: 'T1' };
  const t2 = { id: 't2', project: 'zeta', status: 'running', is_template: true, title: 'T2' };
  vm.runInContext(`_automataState.allPrds = ${JSON.stringify([t1, t2])};`, s);

  setScope(s, 'acme', '');
  assert.deepEqual(idsOf(s._automataBaseList()).sort(), ['t1', 't2'], 'Templates are shown for every project');

  setScope(s, 'acme', 'au-1');
  assert.deepEqual(idsOf(s._automataBaseList()).sort(), ['t1', 't2'], 'Templates are shown for every plan');
});

test('(e) the Events load sends the scope project and a plan narrows a row', () => {
  const s = loadAppSandbox();
  vm.runInContext(`state.activeView = 'autonomous';`, s);

  // No scope: the operator's own dropdown choice is used.
  setScope(s, '', '');
  vm.runInContext(`_automataEventsState.project = 'operatorproj';`, s);
  assert.equal(s._eventsEffectiveProject(), 'operatorproj', 'no scope: the operator dropdown wins');
  assert.equal(s._eventsScopeProject(), '', 'no scope: nothing sent as project=');
  assert.equal(s._eventsPlanScopeActive(), false, 'no scope: no plan narrowing');
  assert.equal(s._eventsRowInScope({}), true, 'no scope: every row is in scope');

  // Project scope: the scope project wins over the operator dropdown; no row
  // narrowing (the server enforces project=).
  setScope(s, 'acme', '');
  assert.equal(s._eventsEffectiveProject(), 'acme', 'project scope wins over the operator dropdown');
  assert.equal(s._eventsScopeProject(), 'acme', 'project scope is sent as project=');
  assert.equal(s._eventsPlanScopeActive(), false, 'project scope: no plan narrowing');
  assert.equal(s._eventsRowInScope({ automation: 'whatever' }), true, 'project scope leaves rows to the server');
  assert.equal(s._eventsRowInScope({}), true, 'project scope leaves rows to the server');

  // Plan scope: only the plan's Automation or its member Automata match.
  setScope(s, 'acme', 'au-1');
  seedPlanMembers(s, ['p1', 'p2']);
  assert.equal(s._eventsPlanScopeActive(), true, 'plan scope narrows rows');
  assert.equal(s._eventsRowInScope({ automation: 'au-1' }), true, 'the plan itself');
  assert.equal(s._eventsRowInScope({ automaton: 'p1' }), true, 'a member Automaton');
  assert.equal(s._eventsRowInScope({ automaton: 'p2' }), true, 'a member Automaton');
  assert.equal(s._eventsRowInScope({ automation: 'au-2' }), false, 'a sibling plan is out');
  assert.equal(s._eventsRowInScope({}), false, 'an unnamed row is out under a plan');
  assert.equal(s._eventsRowInScope(null), false, 'a missing row is out under a plan');
});

test('(f) the Inbox honors the header scope first, then a plan narrows an item', () => {
  const s = loadAppSandbox();
  vm.runInContext(`state.activeView = 'autonomous'; _projectsState.selected = 'zeta'; _automataState.allPrds = [];`, s);

  // No header scope on the Inbox (not the Projects page): the Inbox is in
  // overview mode, so the in-page picker does not cut it.
  vm.runInContext(`_automataState.tab = 'automata';`, s);
  setScope(s, '', '');
  assert.equal(s._inboxScope().kind, 'overview', 'no scope, Inbox context: overview');

  // No header scope on the Projects page: the in-page picker applies.
  vm.runInContext(`_automataState.tab = 'projects';`, s);
  setScope(s, '', '');
  vm.runInContext(`_projectsState.list = [{ name: 'zeta', dir: '/w/zeta' }]; _projectsState.selected = 'zeta';`, s);
  assert.equal(s._inboxScope().kind, 'project', 'no scope, Projects page: the in-page picker applies');
  assert.equal(s._inboxScope().project.name, 'zeta');

  // A header scope wins over the in-page picker, on either tab.
  vm.runInContext(`_automataState.tab = 'projects';`, s);
  setScope(s, 'acme', '');
  assert.equal(s._inboxScope().kind, 'project', 'header scope wins');
  assert.equal(s._inboxScope().project.name, 'acme', 'not the in-page picker');
  vm.runInContext(`_automataState.tab = 'automata';`, s);
  assert.equal(s._inboxScope().project.name, 'acme', 'header scope wins on the Inbox too');

  // Plan narrowing: the item names a plan member or the plan itself.
  setScope(s, 'acme', 'au-1');
  seedPlanMembers(s, ['p1', 'p2']);
  assert.equal(s._inboxItemInPlan({ prd_id: 'p1' }), true, 'a member Automaton');
  assert.equal(s._inboxItemInPlan({ automation: 'au-1' }), true, 'the plan itself');
  assert.equal(s._inboxItemInPlan({ prd_id: 'p9' }), false, 'another Automaton is out');
  assert.equal(s._inboxItemInPlan(null), false, 'a missing item is out under a plan');

  // Project-only scope: no plan, so every item passes the plan check.
  setScope(s, 'acme', '');
  assert.equal(s._inboxItemInPlan({ prd_id: 'anything' }), true, 'project scope leaves items to the project check');
  assert.equal(s._inboxItemInPlan(null), true, 'project scope leaves items to the project check');

  // No scope at all: the plan check is a no-op.
  setScope(s, '', '');
  assert.equal(s._inboxItemInPlan({ prd_id: 'anything' }), true, 'no scope: every item passes');
});

test('(g) filtered sub-tabs show the banner; global sub-tabs and Templates show a note', () => {
  const s = loadAppSandbox();
  vm.runInContext(`state.activeView = 'autonomous'; _automataState.tab = 'automata';`, s);

  // No scope: no note anywhere.
  setScope(s, '', '');
  assert.equal(s._automataScopeNoteHtml(), '', 'no scope: no banner');
  assert.equal(s._automataGlobalScopeNote(), '', 'no scope: no global note');

  // Active scope: the standard banner (with a Clear button) for filtered tabs,
  // and the one-line global note for the inherently global tabs.
  setScope(s, 'acme', '');
  const banner = s._automataScopeNoteHtml();
  assert.match(banner, /scope-banner/, 'the banner is present');
  assert.match(banner, /header_scope_banner/, 'it carries the scope text');
  assert.match(banner, /clearScope\(\)/, 'it carries a Clear button');
  const gnote = s._automataGlobalScopeNote();
  assert.match(gnote, /scope-banner/, 'the global note is present');
  assert.match(gnote, /header_scope_global_note/, 'it carries the global note text');

  // The Templates tab gets the templates note instead of the standard banner.
  vm.runInContext(`_automataState.tab = 'templates';`, s);
  assert.match(s._automataScopeNoteHtml(), /header_scope_templates_note/, 'the Templates tab notes its global view');
  vm.runInContext(`_automataState.tab = 'automata';`, s);

  // The sub-tab bodies wire the right note.
  const subtab = id => vm.runInContext(`_AUTOMATION_SUBTABS.find(x => x.id === ${JSON.stringify(id)})`, s);
  assert.match(subtab('inbox').bodyHTML(), /header_scope_banner/, 'Inbox shows the banner');
  assert.match(subtab('automations').bodyHTML(), /header_scope_banner/, 'Automations shows the banner');
  assert.match(subtab('resources').bodyHTML(), /header_scope_global_note/, 'Resources shows the global note');
  assert.match(subtab('progress').bodyHTML(), /header_scope_global_note/, 'Progress shows the global note');
  assert.match(subtab('housekeeping').bodyHTML(), /header_scope_global_note/, 'Housekeeping shows the global note');

  // And none of it appears with the scope off.
  setScope(s, '', '');
  assert.doesNotMatch(subtab('resources').bodyHTML(), /scope-banner/, 'global note hidden with no scope');
  assert.doesNotMatch(subtab('inbox').bodyHTML(), /scope-banner/, 'banner hidden with no scope');
});

test('(h) header_scope_global_note is present in all five locale bundles', () => {
  for (const loc of LOCALES) {
    const file = path.join(WEB, 'locales', `${loc}.json`);
    const raw = fs.readFileSync(file, 'utf8');
    const bundle = JSON.parse(raw);
    assert.ok(bundle.header_scope_global_note, `${loc}: header_scope_global_note is defined`);
    assert.equal(typeof bundle.header_scope_global_note, 'string', `${loc}: header_scope_global_note is a string`);
    assert.ok(bundle.header_scope_global_note.trim().length > 0, `${loc}: header_scope_global_note is non-empty`);
    // Both scope notes should be available together.
    assert.ok(bundle.header_scope_templates_note, `${loc}: header_scope_templates_note is defined`);
  }
});
