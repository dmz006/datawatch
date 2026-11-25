// Automaton lifecycle events reach the alert store without a session, so
// the Alerts view must show them on its System alerts tab. The fixture is
// the JSON GET /api/alerts serves for such an alert: no session_id and no
// source.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');
const EN = require('./locales/en.json');

const ALERTS = [
  { id: 'l1', level: 'error', title: 'prd.blocked', body: 'PRD prd-1: running -> blocked', read: false, created_at: '2026-01-01T00:00:02Z' },
  { id: 'l2', level: 'warn', title: 'prd.completed', body: 'PRD prd-2: running -> completed', read: false, created_at: '2026-01-01T00:00:01Z' },
];

function render(tab, sort) {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = EN;
  sandbox.__data = { alerts: ALERTS };
  vm.runInContext(`state.sessions = []; window._alertsTabSel = ${JSON.stringify(tab)};`, sandbox);
  if (sort) vm.runInContext(`_alertsCurrentFilter().sort = ${JSON.stringify(sort)};`, sandbox);
  return vm.runInContext('_alertsBuildHtml({ data: __data, cmds: [], hasMore: false, total: 2 })', sandbox);
}

test('lifecycle alerts are listed on the System alerts tab with kind and text', () => {
  for (const sort of ['session', 'chrono']) {
    const html = render('system', sort);
    for (const want of ['prd.blocked', 'PRD prd-1: running -&gt; blocked', 'prd.completed', 'PRD prd-2: running -&gt; completed']) {
      assert.ok(html.includes(want), `${sort} sort: ${want} is painted`);
    }
    assert.ok(html.includes(`<span class="alert-session-label">${EN.alerts_tab_system_alerts}</span> (2)`),
      'the System alerts tab button counts both');
  }
});

test('lifecycle alerts are not filed under a session tab', () => {
  for (const tab of ['active', 'historical']) {
    assert.ok(!render(tab).includes('prd.blocked'), `${tab} tab does not list them`);
  }
});
