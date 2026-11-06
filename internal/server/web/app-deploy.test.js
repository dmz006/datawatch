// Deploy card — PWA side.
//
// window.deployCardHtml(view, opts) turns the decoded GET /api/autonomous/deploy
// body into the Automation Overview card: the settings in effect, the phase and
// last outcome, the deferred reason and a Run now button that is disabled when
// no build, install or restart command is set. The Autonomous settings group
// carries the seven autonomous.deploy.* fields, and every deploy_ and
// settings_deploy_ locale key is present in all five bundles.
//
// Run: node --test internal/server/web/app-deploy.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const HOSTILE = '<img src=x onerror=alert(1)>';
const PREFIXES = ['deploy_', 'settings_deploy_'];

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
}

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  return sandbox;
}

function render(view, opts) {
  const sandbox = loadApp();
  sandbox._view = view;
  sandbox._opts = opts;
  return vm.runInContext('window.deployCardHtml(_view, _opts)', sandbox);
}

function settings(over) {
  return {
    build_command: '', install_command: '', binary_path: '', restart_command: '', health_check: '',
    drain_timeout: '10m0s', health_timeout: '2m0s', ...over,
  };
}

const RUN_BUTTON = /<button[^>]*class="[^"]*deploy-run[^"]*"[^>]*>/;

test('a card with no commands disables the run button and says why', () => {
  const html = render({ settings: settings(), running: false });
  assert.match(html, RUN_BUTTON);
  assert.ok(/deploy-run[^>]*disabled/.test(html.match(RUN_BUTTON)[0]), html);
  assert.ok(html.includes('Set a build, install or restart command'), html);
  assert.ok(html.includes('No deploy has been recorded yet.'), html);
  assert.ok(html.includes('(not set)'), html);
});

test('a card with a command enables the run button', () => {
  const html = render({ settings: settings({ build_command: 'make build' }), running: false });
  assert.ok(!/disabled/.test(html.match(RUN_BUTTON)[0]), html);
  assert.ok(html.includes('make build'), html);
});

test('a deploy that is running disables the run button', () => {
  const html = render({ settings: settings({ restart_command: 'systemctl restart x' }), running: true });
  assert.ok(/disabled/.test(html.match(RUN_BUTTON)[0]), html);
  assert.ok(html.includes('A merge or deploy is running now.'), html);
});

test('a deferred deploy shows the reason', () => {
  const html = render({
    settings: settings({ build_command: 'make build' }), phase: 'deferred',
    deferred: { reason: 'final-gate tasks are still running', at: '2026-01-01T00:00:00Z' }, running: false,
  });
  assert.ok(html.includes('Phase: deferred'), html);
  assert.ok(html.includes('Deferred: final-gate tasks are still running'), html);
});

test('a failed deploy with a rollback shows the phase and the outcome', () => {
  const html = render({
    settings: settings({ install_command: 'cp a b', binary_path: '/usr/bin/x' }),
    phase: 'rolled_back', outcome: 'rolled_back', running: false,
  });
  assert.ok(html.includes('Phase: rolled_back'), html);
  assert.ok(html.includes('Last outcome: rolled_back'), html);
  assert.ok(html.includes('/usr/bin/x'), html);
});

test('a finished deploy shows done and the ok outcome', () => {
  const html = render({ settings: settings({ build_command: 'make' }), phase: 'done', outcome: 'ok', running: false });
  assert.ok(html.includes('Phase: done'), html);
  assert.ok(html.includes('Last outcome: ok'), html);
});

test('the last run result is shown and every value is escaped', () => {
  const html = render({ settings: settings({ build_command: HOSTILE }), phase: HOSTILE, running: false }, { result: HOSTILE });
  assert.ok(!html.includes(HOSTILE), html);
  assert.ok(html.includes('&lt;img'), html);
});

test('a missing view renders the placeholder', () => {
  const html = render(null);
  assert.ok(html.includes('The deploy settings have not loaded yet.'), html);
  assert.ok(!RUN_BUTTON.test(html), html);
});

test('the Autonomous settings group lists the seven deploy fields', () => {
  const sandbox = loadApp();
  const fields = JSON.parse(vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.filter(f => (f.key || '').startsWith('autonomous.deploy.')))",
    sandbox));
  assert.deepEqual(fields.map(f => f.key).sort(), [
    'autonomous.deploy.binary_path', 'autonomous.deploy.build_command', 'autonomous.deploy.drain_timeout',
    'autonomous.deploy.health_check', 'autonomous.deploy.health_timeout', 'autonomous.deploy.install_command',
    'autonomous.deploy.restart_command',
  ]);
  const en = bundle('en');
  for (const f of fields) {
    assert.ok(en[f.labelKey], f.labelKey);
    assert.ok(en[f.hintKey], f.hintKey);
  }
});

test('every deploy key is present in all five bundles', () => {
  const en = bundle('en');
  const keys = Object.keys(en).filter(k => PREFIXES.some(p => k.startsWith(p)));
  assert.ok(keys.length >= 30, 'found ' + keys.length);
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of keys) {
      assert.ok(typeof b[k] === 'string' && b[k] !== '', `${lang} is missing ${k}`);
    }
  }
});

test('every deploy key the app uses is in the English bundle', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const en = bundle('en');
  const used = new Set();
  for (const m of src.matchAll(/['"]((?:settings_)?deploy_[a-z0-9_]+)['"]/g)) used.add(m[1]);
  assert.ok(used.size > 20, 'found ' + used.size);
  for (const k of used) assert.ok(en[k], k);
});
