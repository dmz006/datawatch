// The Decision records card renders nothing with zero records, renders with
// one, never shows the folder-watch-off text, and the folder setting's help
// link goes to the decision records how-to.
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function load() {
  const sandbox = buildSandbox({ byId: {} });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  return sandbox;
}

const one = { id: 'd1', status: 'open', question: 'Pick one', options: [{ id: 'a', label: 'A' }] };

test('card is empty with zero records, even with folder watch off', () => {
  const sb = load();
  const f = vm.runInContext('window.decisionRecordCardHtml', sb);
  assert.equal(f({ decision_records: [], folder: { enabled: false } }), '');
  assert.equal(f({ decision_records: [] }), '');
});

test('card renders with one record and never says folder watch is off', () => {
  const sb = load();
  const f = vm.runInContext('window.decisionRecordCardHtml', sb);
  const html = f({ decision_records: [one], folder: { enabled: false } });
  assert.match(html, /decision-record-card/);
  assert.doesNotMatch(html, /Folder watch is off/i);
  assert.doesNotMatch(html, /decision-record-folder-off/);
});

test('folder setting help link goes to the how-to', () => {
  const sb = load();
  const f = vm.runInContext('docsLinkForKey', sb);
  assert.equal(f('settings_automata_decisions_folder'), 'howto/automaton-decision-records.md');
  assert.ok(f('settings_automata_decisions_enabled').startsWith('datawatch-definitions.md'));
});
