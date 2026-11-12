// Pins the Packs sub-tab shell under the Templates tab: it is registered in
// the sub-tab registry, shows the explanation and an Enable button when packs
// are off, links to the skills registry settings, and keeps no pack list.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement, flushAsync } = require('./testutil_browser_stub');

function fresh(config) {
  const els = { automataPanel: makeStubElement() };
  const sandbox = buildSandbox({ byId: els });
  sandbox.URLSearchParams = URLSearchParams;
  sandbox.fetch = (url) => {
    const body = String(url).includes('/api/packs/config') ? config : {};
    return Promise.resolve({ ok: true, status: 200, statusText: '', json: () => Promise.resolve(body), text: () => Promise.resolve(JSON.stringify(body)) });
  };
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app-packs-subtab.js'), sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return { sb: sandbox, panel: els.automataPanel };
}

test('registry has a packs entry', () => {
  const { sb } = fresh({ enabled: false });
  const keys = vm.runInContext('_AUTOMATA_TEMPLATE_SUBTABS.map(s => s.key).join(",")', sb);
  assert.equal(keys, 'automaton,standard,packs');
});

test('disabled state shows explanation, Enable button and registry link', () => {
  const { sb } = fresh({ enabled: false });
  const html = sb.packsSubtabHTML({ status: 'ok', enabled: false, error: '' });
  assert.match(html, /Standard task packs are turned off/);
  assert.match(html, /packs-enable-btn/);
  assert.match(html, /packsSubtabOpenSetting/);
  assert.match(html, /packsSubtabOpenRegistry/);
  assert.match(html, /Skills registry settings/);
});

test('enabled state has no Enable button but keeps the registry link, and no pack list', () => {
  const { sb } = fresh({ enabled: true });
  const html = sb.packsSubtabHTML({ status: 'ok', enabled: true, error: '' });
  assert.doesNotMatch(html, /packs-enable-btn/);
  assert.match(html, /packs-registry-link/);
  assert.doesNotMatch(html, /<table|<li/);
});

test('load reads /api/packs/config and renders into the panel', async () => {
  const { sb, panel } = fresh({ enabled: false });
  vm.runInContext('_automataTmplSubtab = "packs"', sb);
  await sb.packsSubtabLoad();
  await flushAsync();
  assert.match(panel.innerHTML, /packs-enable-btn/);
});

test('every locale carries the strings', () => {
  for (const loc of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', loc + '.json'), 'utf8'));
    for (const k of ['packs_subtab_title', 'packs_subtab_disabled', 'packs_subtab_enabled', 'packs_subtab_enable', 'packs_subtab_registry_link']) {
      assert.ok(b[k], loc + ' missing ' + k);
    }
  }
});

test('script is wired in index.html and sw.js', () => {
  assert.match(fs.readFileSync(path.join(__dirname, 'index.html'), 'utf8'), /\/app-packs-subtab\.js/);
  assert.match(fs.readFileSync(path.join(__dirname, 'sw.js'), 'utf8'), /app-packs-subtab\.js/);
});
