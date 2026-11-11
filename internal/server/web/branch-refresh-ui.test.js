// Branch refresh UI — PWA side tests.
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const src = fs.readFileSync(path.join(__dirname, 'branch_refresh_ui.js'), 'utf8');
const keys = Array.from(new Set(src.match(/'(?:settings_)?branch_refresh_[a-z_]+'/g) || [])).map(k => k.slice(1, -1));

test('every locale has every branch refresh key', () => {
  assert.ok(keys.length > 10);
  for (const l of LANGS) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'));
    for (const k of keys) assert.ok(b[k], l + ' missing ' + k);
  }
});

test('settings fields cover both config keys', () => {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  vm.runInContext(src, sandbox);
  const f = sandbox.window.branchRefreshSettingsFields();
  assert.deepEqual(Array.from(f, x => x.key), ['autonomous.branch_refresh_enabled', 'autonomous.branch_refresh_behind_limit']);
  assert.equal(f[0].type, 'toggle');
  assert.equal(f[1].type, 'number');
});
