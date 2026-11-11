// Dependency update settings in the PWA general config fields:
//
//   - three fields sit in GENERAL_CONFIG_FIELDS next to the repo CI watch group
//   - each has a label key and a help entry that links to the how-to
//
//   node --test app-dependency-update.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const PREFIX = 'autonomous.automation.dependency_update.';
const HOWTO = 'howto/automation-dependency-update.md';

function fieldMap() {
  const sandbox = buildSandbox({ localStorage: new Map() });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  const fields = vm.runInContext('GENERAL_CONFIG_FIELDS', sandbox);
  const section = fields.find(s => s.id === 'autonomous');
  assert.ok(section, 'autonomous section must exist');
  return new Map(section.fields.map(f => [f.key, f]));
}

test('dependency update — the three fields exist with label and help keys', () => {
  const m = fieldMap();
  for (const name of ['enabled', 'schedule', 'max_majors']) {
    const f = m.get(PREFIX + name);
    assert.ok(f, `${PREFIX}${name} must exist`);
    assert.equal(f.labelKey, `settings_dependency_update_${name}`);
    assert.equal(f.helpKey, `settings_dependency_update_${name}_help`);
    assert.equal(f.helpDocs, HOWTO, `${name} help links to the how-to`);
  }
});

test('dependency update — types and defaults', () => {
  const m = fieldMap();
  const en = m.get(PREFIX + 'enabled');
  assert.equal(en.type, 'toggle');
  assert.equal(en.default, true);
  const sch = m.get(PREFIX + 'schedule');
  assert.equal(sch.type, 'text');
  assert.equal(sch.placeholder, 'monday 05:00');
  const mm = m.get(PREFIX + 'max_majors');
  assert.equal(mm.type, 'number');
  assert.equal(mm.min, 0);
  assert.equal(mm.max, 20);
  assert.equal(mm.step, 1);
  assert.equal(mm.default, '5');
});

test('dependency update — fields sit after the repo CI watch group', () => {
  const keys = [...fieldMap().keys()];
  const last = keys.lastIndexOf('autonomous.automation.repo_ci_watch.delete_superseded');
  assert.ok(last >= 0);
  assert.equal(keys.indexOf(PREFIX + 'enabled'), last + 1);
});
