// e73d0006 — the Alerts view's help affordance.
//
// The Alerts tab has no help link of its own inside its content: the
// single affordance is the shared "?" help link in the app's top bar,
// which follows the active view. navigate() points it at the per-view
// howto (in-app docs viewer at /diagrams.html#docs/...) and gives the
// Alerts view a tooltip that names it (alerts_help_tip). For Alerts the
// howto is alerts-and-notifications and the content HTML carries no
// "alerts-help-link" of any kind.
//
// Covered here:
//   * navigate('alerts') maps the header "?" link to the
//     alerts-and-notifications howto href;
//   * the link is visible on the Alerts view and its tooltip is the
//     localized alerts_help_tip (English and French);
//   * the rendered Alerts content holds NO "alerts-help-link" (no
//     element, class or href) — the help entry is the header link only,
//     and alerts-help-link markup is absent from app.js/style.css/index.html;
//   * app.js's anchorMap really wires 'alerts' to that howto.

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALE = (code) => JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', code + '.json'), 'utf8'));
const HOWTO_HREF = '/diagrams.html#docs/howto/alerts-and-notifications.md';

const SAMPLE = [
  { id: 'al-hh-1', level: 'warn', title: 'disk filling up', body: 'usage 91% on /data', session_id: 'local-hh', created_at: '2026-01-01T00:00:05Z' },
  { id: 'sys-hh-1', level: 'error', title: 'pipeline failed', body: '', source: 'system', created_at: '2026-01-01T00:00:06Z' },
];

function boot(bundle) {
  const view = makeStubElement();
  const help = makeStubElement();
  const list = makeStubElement();
  const sandbox = buildSandbox({ byId: { view, headerHelpLink: help, alertsList: list } });
  sandbox.fetch = (url) => {
    const u = String(url);
    const body = u.includes('/api/alerts') ? SAMPLE
      : u.includes('/api/sessions') ? { sessions: [{ id: 'local-hh', name: 'Local', state: 'running' }] }
      : u.includes('/api/commands') ? []
      : [];
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) });
  };
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);   // real top-level code runs to completion
  sandbox._i18n.bundle = bundle;
  return { sandbox, view, help, list };
}

test('navigate to Alerts points the header help link at the alerts-and-notifications howto', async () => {
  const EN = LOCALE('en');
  const { sandbox, help, list } = boot(EN);
  sandbox._alertsTabSel = 'active';
  sandbox.navigate('alerts', null);
  await flushAsync();
  assert.equal(String(help.href), HOWTO_HREF, 'the header "?" link href lands on the Alerts howto');
  assert.equal(help.title, EN.alerts_help_tip, 'tooltip names the Alerts howto (localized alerts_help_tip)');
  assert.ok(String(help.style && help.style.display || '').includes('flex'), 'the "?" link is shown on the Alerts view');
  assert.ok(list.innerHTML.length > 0, 'the Alerts content rendered (#alertsList)');
  assert.ok(list.innerHTML.includes('disk filling up'), 'alert card content is present in the rendered view');
});

test('the header help tooltip is localized (French)', async () => {
  const FR = LOCALE('fr');
  const { sandbox, help } = boot(FR);
  sandbox.navigate('alerts', null);
  await flushAsync();
  assert.equal(String(help.href), HOWTO_HREF, 'href is language-independent');
  assert.equal(help.title, FR.alerts_help_tip, 'tooltip follows the bundle language');
});

test('the Alerts content carries no alerts-help-link of its own', async () => {
  const { sandbox, view, list } = boot(LOCALE('en'));
  sandbox._alertsTabSel = 'active';
  sandbox.navigate('alerts', null);
  await flushAsync();
  const painted = String(list.innerHTML);
  assert.ok(painted.includes('disk filling up'), 'sanity — the Alerts content did render');
  assert.ok(!painted.includes('alerts-help-link'),
    'no alerts-help-link element/class/href in the rendered Alerts content');
  assert.ok(!String(view.innerHTML).includes('alerts-help-link'),
    'and none in the #view shell either');
});

test('alerts-help-link markup is gone from app.js/style.css/index.html; anchorMap wires alerts→howto', () => {
  const read = (p) => fs.readFileSync(path.join(__dirname, p), 'utf8');
  for (const f of ['app.js', 'style.css', 'index.html']) {
    assert.ok(!read(f).includes('alerts-help-link'), f + ' does not carry the removed alerts-help-link markup');
  }
  const src = read('app.js');
  const i = src.indexOf('const anchorMap = {');
  assert.ok(i >= 0, 'navigate() still has the per-view anchorMap');
  const anchorSrc = src.slice(i, src.indexOf('};', i) + 2);
  assert.ok(/'alerts':\s*'\/diagrams\.html#docs\/howto\/alerts-and-notifications\.md'/.test(anchorSrc),
    "anchorMap['alerts'] is the alerts-and-notifications howto");
});
