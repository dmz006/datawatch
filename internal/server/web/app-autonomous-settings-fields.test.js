// Pins the Settings > General > Autonomous field list to the real
// per-Automaton concurrency key. autonomous.max_parallel_tasks never had
// any effect and is deprecated; the PWA must expose
// autonomous.max_concurrent_tasks instead and offer no editable control
// for the dead key.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function autonomousKeys() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  const json = vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields.map(f => f.key))",
    sandbox);
  return JSON.parse(json);
}

test('autonomous settings expose max_concurrent_tasks, not max_parallel_tasks', () => {
  const keys = autonomousKeys();
  assert.ok(keys.includes('autonomous.max_concurrent_tasks'), 'autonomous.max_concurrent_tasks missing: ' + keys.join(','));
  assert.ok(!keys.includes('autonomous.max_parallel_tasks'), 'deprecated autonomous.max_parallel_tasks still editable');
});
