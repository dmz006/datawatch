// The switch autonomous.merge_deploy_run_enabled in the web app: the deploy
// card disables Run now and names the setting while the daemon reports it off,
// a refused click shows the daemon's refusal, and Settings carries the toggle.
//
// Run: node --test internal/server/web/app-merge-deploy-run-switch.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const KEY = 'autonomous.merge_deploy_run_enabled';
const RUN_BUTTON = /<button[^>]*class="[^"]*deploy-run[^"]*"[^>]*>/;

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
}

function loadApp(lang) {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = bundle(lang || 'en');
  return sandbox;
}

function render(view, lang) {
  const sandbox = loadApp(lang);
  sandbox._view = view;
  return vm.runInContext('window.deployCardHtml(_view, {})', sandbox);
}

const withCommand = { build_command: 'make build', install_command: '', restart_command: '' };

test('the run switch off disables Run now and names the setting in every locale', () => {
  for (const lang of LANGS) {
    const html = render({ settings: withCommand, running: false, run_enabled: false }, lang);
    assert.match(html.match(RUN_BUTTON)[0], /disabled/, lang);
    assert.match(html, /class="deploy-run-switched-off"/, lang);
    assert.ok(html.includes(KEY), lang + ': ' + html);
  }
});

test('the run switch off is the only hint, even with no command set', () => {
  const html = render({ settings: {}, running: false, run_enabled: false });
  assert.match(html, /class="deploy-run-switched-off"/);
  assert.doesNotMatch(html, /class="deploy-run-hint"/);
});

test('the run switch on, or not reported, leaves the card as it was', () => {
  for (const view of [{ settings: withCommand, run_enabled: true }, { settings: withCommand }]) {
    const html = render(view);
    assert.doesNotMatch(html.match(RUN_BUTTON)[0], /disabled/);
    assert.doesNotMatch(html, /deploy-run-switched-off/);
  }
});

test('Settings carries the toggle with a label, a hint and a help link', () => {
  const sandbox = loadApp();
  const field = JSON.parse(vm.runInContext(
    `JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.find(f => f.key === '${KEY}') || null)`,
    sandbox));
  assert.ok(field, 'no settings field for ' + KEY);
  assert.equal(field.type, 'toggle');
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of [field.labelKey, field.hintKey, 'deploy_run_switched_off_hint']) {
      assert.ok(typeof b[k] === 'string' && b[k] !== '', `${lang} is missing ${k}`);
    }
    assert.ok(b[field.hintKey].includes(KEY) || lang === '', `${lang}: the hint names the setting`);
  }
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  assert.ok(src.includes(`'${KEY}': DOCS_DEFINITIONS + '#autonomous-merge-deploy-run-enabled'`), 'no help link');
});

test('a refused Run now shows the daemon refusal', async () => {
  const sandbox = loadApp();
  const refusal = 'merge run and deploy run are switched off: the setting ' + KEY + ' is false';
  vm.runInContext('apiFetch = () => Promise.reject(new Error(' + JSON.stringify(refusal) + '))', sandbox);
  await vm.runInContext('window.deployRunNow()', sandbox);
  assert.ok(String(sandbox.window._deployResult).includes(refusal), sandbox.window._deployResult);
});
