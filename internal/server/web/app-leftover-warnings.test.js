// Automaton detail shows leftover_warnings: the files a run left behind in
// the project folder, as warnings (the daemon's prd-get already returns the
// list). Pins the render helper, its escaping, that it renders nothing when
// there are no warnings, that the detail overview calls it, and the locale
// keys in all five bundles.
//
//   node --test app-leftover-warnings.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALE_LIST = ['en', 'de', 'es', 'fr', 'ja'];

function loadApp() {
  const sandbox = buildSandbox({ localStorage: new Map() });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  return sandbox;
}

function render(prd) {
  const sandbox = loadApp();
  sandbox.__prd = prd;
  return vm.runInContext('typeof renderDetailLeftoverWarnings === "function" ? renderDetailLeftoverWarnings(__prd) : null', sandbox);
}

test('renderDetailLeftoverWarnings lists each warning with a count', () => {
  const html = render({ leftover_warnings: ['leftover file: a.txt', 'leftover file: dir/b.log'] });
  assert.ok(typeof html === 'string', 'renderDetailLeftoverWarnings is not defined');
  assert.ok(html.includes('leftover file: a.txt'), html);
  assert.ok(html.includes('leftover file: dir/b.log'), html);
  assert.ok(html.includes('(2)'), 'count missing: ' + html);
});

test('renderDetailLeftoverWarnings escapes every warning', () => {
  const html = render({ leftover_warnings: ['leftover file: <img src=x onerror=alert(1)>.txt'] });
  assert.ok(typeof html === 'string', 'renderDetailLeftoverWarnings is not defined');
  assert.ok(!html.includes('<img'), 'unescaped markup: ' + html);
  assert.ok(html.includes('&lt;img'), html);
});

test('renderDetailLeftoverWarnings renders nothing without warnings', () => {
  for (const prd of [{}, { leftover_warnings: [] }, { leftover_warnings: null }, null]) {
    assert.equal(render(prd), '', JSON.stringify(prd));
  }
});

test('the Automaton detail overview calls the leftover warnings renderer', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.ok(/\$\{renderDetailLeftoverWarnings\(prd\)\}/.test(src), 'detail overview does not render leftover warnings');
});

test('leftover warning locale keys exist in all five locales without a tracker id', () => {
  for (const l of LOCALE_LIST) {
    const j = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'));
    for (const k of ['prd_leftover_warnings_title', 'prd_leftover_warnings_body']) {
      assert.ok(typeof j[k] === 'string' && j[k].length > 0, l + ': ' + k + ' missing');
      assert.ok(!/\b(BL|F)\d+\b/.test(j[k]), l + ': tracker id in ' + k);
    }
  }
});
