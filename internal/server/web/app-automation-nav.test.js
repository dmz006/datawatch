// Automation tab — navigation and list scaffold.
//
// The Automation sub-tab is the leftmost tab of the Automata view, is the
// default on a first visit, and needs the autonomous subsystem enabled. This
// test covers the nav markup, the sub-tab bar, the default and its switch, the functions the section exposes, the status-aware
// request helper (a 409 must surface its status; a 204 resolves null) and the
// escaping in the card builder.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  // t() falls back to the raw key until a bundle loads; install the English
  // one so the rendered strings are the real user-visible ones.
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

function loadView() {
  const els = {};
  const out = { view: '' };
  els.view = makeStubElement(h => { out.view = h; });
  const sandbox = buildSandbox({ byId: els });
  sandbox.fetch = () => Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve([]), text: () => Promise.resolve('[]') });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return { sb: sandbox, out, els };
}
const tabState = sb => vm.runInContext('_automataState.tab', sb);

test('index.html has no top-level Automation button, only the Automata one', () => {
  const html = fs.readFileSync(path.join(__dirname, 'index.html'), 'utf8');
  assert.ok(!html.includes('navBtnAutomation'), 'the Automation nav button is gone');
  assert.ok(!/data-view="automation"/.test(html));
  assert.ok(html.includes('id="navBtnAutonomous"'), 'the Automata nav button stays');
});

test('app.js has no code path that depends on the automation view', () => {
  const js = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  assert.ok(!/activeView\s*===\s*'automation'/.test(js));
  assert.ok(!js.includes('navBtnAutomation'));
  assert.ok(!/navigate\('automation'\)/.test(js));
});

test('a first visit opens the Automata tab on the Automation sub-tab, which is the leftmost', () => {
  const { sb, out } = loadView();
  assert.equal(vm.runInContext('_AUTOMATA_DEFAULT_TAB', sb), 'automation');
  assert.equal(tabState(sb), 'automation');
  sb.renderAutonomousView();
  const order = [...out.view.matchAll(/class="output-tab[^"]*" data-tab="([a-z]+)"/g)].map(m => m[1]);
  assert.deepEqual(order, ['automation', 'automata', 'templates']);
  assert.match(out.view, /class="output-tab active" data-tab="automation"/);
  assert.match(out.view, /id="automationPanel"/);
  assert.doesNotMatch(out.view, /id="automataPanel"/);
});

test('the last chosen sub-tab is not remembered: the persistence switch is off', () => {
  const { sb } = loadView();
  assert.equal(vm.runInContext('_AUTOMATA_REMEMBER_TAB', sb), false);
  sb.localStorage.setItem('cs_automata_tab', 'templates');
  sb.switchAutomataTab('automata');
  assert.equal(sb.localStorage.getItem('cs_automata_tab'), 'templates', 'switching writes nothing');
  assert.equal(vm.runInContext('_automataInitialTab()', sb), 'automation', 'a stored value is ignored');
});

test('the action buttons follow the selected sub-tab', () => {
  const { sb, out } = loadView();
  const shown = (html, id) => !new RegExp('id="' + id + '"[^>]*style="[^"]*display:none').test(html);
  sb.renderAutonomousView();
  for (const id of ['automationFilterBtn', 'automationHistoryBtn']) assert.ok(shown(out.view, id), id + ' shown on Automation');
  assert.doesNotMatch(out.view, /automationNewBtn/);
  for (const id of ['automataSelectBtn', 'automataFilterBtn', 'automataHistoryBtn', 'automataWatchFilterBtn']) assert.ok(!shown(out.view, id), id + ' hidden on Automation');
  sb.switchAutomataTab('automata');
  for (const id of ['automationFilterBtn', 'automationHistoryBtn']) assert.ok(!shown(out.view, id), id + ' hidden on Automata');
  for (const id of ['automataSelectBtn', 'automataFilterBtn', 'automataHistoryBtn', 'automataWatchFilterBtn']) assert.ok(shown(out.view, id), id + ' shown on Automata');
});

test('the floating action button follows the selected sub-tab', () => {
  const { sb, els } = loadView();
  const fab = els.newSessionFab = makeStubElement();
  const orig = sb.document.getElementById;
  sb.document.getElementById = id => (id === 'newSessionFab' ? fab : orig(id));
  sb.switchAutomataTab('automation');
  assert.equal(fab.title, 'New Automation');
  assert.equal(fab.onclick.toString().includes('openAutomationForm'), true);
  sb.switchAutomataTab('automata');
  assert.equal(fab.onclick, sb.openLaunchAutomatonWizard);
  sb.switchAutomataTab('templates');
  assert.equal(fab.onclick, sb.openTemplateCreateModal);
});

test('renderAutomationView routes to the Automata view with the Automation sub-tab selected', () => {
  const { sb, out } = loadView();
  sb.switchAutomataTab('templates');
  sb.renderAutomationView();
  assert.equal(tabState(sb), 'automation');
  assert.match(out.view, /data-tab="automata"/);
  assert.match(out.view, /id="automationPanel"/);
});

test('a disabled subsystem, or an unreachable config, falls the Automation sub-tab back to Automata', () => {
  const { sb, out } = loadView();
  vm.runInContext('state._autonomousEnabled = false', sb);
  sb.renderAutonomousView();
  assert.equal(tabState(sb), 'automata');
  assert.doesNotMatch(out.view, /data-tab="automation"/);
  assert.match(out.view, /id="automataPanel"/);
  // Enabled again: the sub-tab is selectable.
  vm.runInContext('state._autonomousEnabled = true', sb);
  sb.switchAutomataTab('automation');
  assert.equal(tabState(sb), 'automation');
});

test('the header title follows the sub-tab', () => {
  const { sb } = loadView();
  assert.equal(sb._automataHeaderTitle(), 'Automation');
  sb.switchAutomataTab('automata');
  assert.equal(sb._automataHeaderTitle(), 'Automata');
});

test('the sandbox exposes the Automation view, request helper and card builder', () => {
  const sb = loadApp();
  assert.equal(typeof sb.renderAutomationView, 'function');
  assert.equal(typeof sb.automationRequest, 'function');
  assert.equal(typeof sb.buildAutomationCardHTML, 'function');
});

test('automationRequest rejects with the HTTP status on a 409 and resolves null on a 204', async () => {
  const sb = loadApp();
  sb.fetch = () => Promise.resolve({
    ok: false, status: 409, statusText: 'Conflict',
    text: () => Promise.resolve('version is stale'),
  });
  await assert.rejects(
    () => sb.automationRequest('PUT', '/api/automations/a1', { name: 'x' }),
    err => err.status === 409 && err.message === 'version is stale',
  );

  sb.fetch = () => Promise.resolve({ ok: true, status: 204 });
  assert.equal(await sb.automationRequest('DELETE', '/api/automations/a1'), null);
});

test('automationRequest sends the token, the method and the JSON body', async () => {
  const sb = loadApp();
  sb.localStorage.setItem('cs_token', 'tok');
  let seen;
  sb.fetch = (url, opts) => {
    seen = { url, opts };
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ id: 'a1' }) });
  };
  const out = await sb.automationRequest('POST', '/api/automations', { name: 'n' });
  assert.deepEqual(JSON.parse(JSON.stringify(out)), { id: 'a1' });
  assert.equal(seen.url, '/api/automations');
  assert.equal(seen.opts.method, 'POST');
  assert.equal(seen.opts.headers.Authorization, 'Bearer tok');
  assert.equal(seen.opts.body, JSON.stringify({ name: 'n' }));
});

test('buildAutomationCardHTML escapes the name and the id', () => {
  const sb = loadApp();
  const html = sb.buildAutomationCardHTML({
    id: 'x" onclick="boom()',
    name: `<script>alert(1)</script> it's "quoted"`,
    status: 'active',
  }, {});
  assert.ok(!html.includes('<script>'), 'raw script tag is escaped');
  assert.ok(html.includes('&lt;script&gt;'));
  assert.ok(!html.includes(`it's`), 'single quote is escaped');
  assert.ok(html.includes('&#39;') && html.includes('&quot;quoted&quot;'));
  assert.ok(!/data-automation-id="x" onclick/.test(html), 'id cannot break out of its attribute');
  assert.match(html, /prd-card-status-active/);
  assert.match(html, />Active</);
});

test('the list renders cards, the empty text and the escaped load failure', async () => {
  const sb = loadApp();
  const panel = { innerHTML: '' };
  sb.document.getElementById = id => (id === 'automationPanel' ? panel : { style: {}, classList: { toggle() {} } });

  sb.fetch = () => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ automations: [{ id: 'a1', name: 'Nightly', status: 'draft' }] }) });
  await sb.loadAutomationList();
  assert.match(panel.innerHTML, /data-automation-id="a1"/);
  assert.match(panel.innerHTML, /Nightly/);

  sb.fetch = () => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve([]) });
  await sb.loadAutomationList();
  assert.match(panel.innerHTML, /No Automations yet/);

  sb.fetch = () => Promise.resolve({ ok: false, status: 500, statusText: 'x', text: () => Promise.resolve('<b>down</b>') });
  await sb.loadAutomationList();
  assert.match(panel.innerHTML, /Could not load Automations: &lt;b&gt;down&lt;\/b&gt;/);
});
