// Projects tab — the show-empty checkbox in the picker. It is a viewer
// preference: unchecked by default, remembered, and it repaints on change.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript, makeStubElement } = require('./testutil_browser_stub');

function load() {
  const view = makeStubElement(() => {});
  view.insertAdjacentHTML = () => {};
  const sandbox = buildSandbox({ byId: { view } });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

test('the picker has an unchecked show-empty checkbox by default', () => {
  const sb = load();
  const html = sb._projectsPickerHTML([{ name: 'acme' }], '', '');
  assert.ok(html.includes('id="projectsShowEmpty"'), 'checkbox present');
  assert.ok(!html.includes(' checked '), 'unchecked by default');
  assert.ok(html.includes('toggleProjectsShowEmpty(this.checked)'), 'onchange wired');
  assert.ok(html.includes('Show empty lines and sections'), 'label from locale');
});

test('toggleProjectsShowEmpty remembers the choice and repaints', () => {
  const sb = load();
  let painted = 0;
  vm.runInContext('_projectsPaint = function () { globalThis.__painted = (globalThis.__painted || 0) + 1; }', sb);
  sb.toggleProjectsShowEmpty(true);
  assert.equal(vm.runInContext('_projectsShowEmpty()', sb), true);
  assert.equal(vm.runInContext('globalThis.__painted', sb), 1);
  assert.ok(sb._projectsPickerHTML([], '', '').includes(' checked '), 'checked once remembered');
  sb.toggleProjectsShowEmpty(false);
  assert.equal(vm.runInContext('_projectsShowEmpty()', sb), false);
});
