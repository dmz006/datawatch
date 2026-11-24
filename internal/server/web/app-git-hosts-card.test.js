// Git hosts monitor card — PWA side. window-level gitHostsCardHtml draws the
// counters from GET /api/git/hosts/stats and hides itself when no host is configured.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
}

function loadApp() {
  const sandbox = buildSandbox({ byId: {} });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  return sandbox;
}

function render(sandbox, value) {
  sandbox._v = value;
  return vm.runInContext('gitHostsCardHtml(_v)', sandbox);
}

test('card is hidden (empty) when no host is configured', () => {
  const sb = loadApp();
  assert.equal(render(sb, { configured: 0, operations: {}, failed: 0 }), '');
  assert.equal(render(sb, null), '');
  assert.equal(render(sb, undefined), '');
});

test('card shows counters and a one-line purpose', () => {
  const sb = loadApp();
  const html = render(sb, {
    configured: 2, window_seconds: 3600, operations: { push: 3, merge: 1 }, failed: 1,
    last_success_at: new Date().toISOString(),
  });
  const en = bundle('en');
  assert.ok(html.includes(en.git_hosts_card_title));
  assert.ok(html.includes(en.git_hosts_card_purpose));
  assert.ok(html.includes(en.git_hosts_configured));
  assert.ok(html.includes(en.git_hosts_op_push));
  assert.ok(html.includes(en.git_hosts_op_merge));
  assert.ok(html.includes(en.git_hosts_failed));
  assert.ok(html.includes(en.git_hosts_last_success));
  assert.ok(!html.includes(en.git_hosts_never));
});

test('zero last success shows never, empty operations shows none yet', () => {
  const sb = loadApp();
  const html = render(sb, { configured: 1, operations: {}, failed: 0, last_success_at: '0001-01-01T00:00:00Z' });
  const en = bundle('en');
  assert.ok(html.includes(en.git_hosts_never));
  assert.ok(html.includes(en.git_hosts_no_ops));
});

test('hostile operation kind is escaped', () => {
  const sb = loadApp();
  const html = render(sb, { configured: 1, operations: { '<img src=x onerror=alert(1)>': 1 }, failed: 0 });
  assert.ok(!html.includes('<img src=x'));
});

test('all five locale files parse with the same git_hosts key set', () => {
  const keysOf = l => Object.keys(bundle(l)).filter(k => k.startsWith('git_hosts_')).sort();
  const base = keysOf('en');
  assert.ok(base.length >= 10);
  for (const l of LANGS) assert.deepEqual(keysOf(l), base, l);
});
