// Parallel execution metrics — PWA side: the card on the Automaton view, its
// help link and the locale keys in every bundle.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const KEYS = [
  'parallel_metrics_card_title', 'parallel_metrics_help_link', 'parallel_metrics_total_tasks',
  'parallel_metrics_read_only_tasks', 'parallel_metrics_speedup', 'parallel_metrics_speedup_none',
];

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
}

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = bundle('en');
  return sandbox;
}

function card(prd) {
  const sandbox = loadApp();
  sandbox._p = prd;
  return vm.runInContext('window.parallelMetricsCardHtml(_p)', sandbox);
}

test('the card shows total tasks, read-only tasks and the speedup', () => {
  const html = card({ total_tasks: 4, read_only_tasks: 2, speedup: 2 });
  assert.match(html, /parallel-metrics-card/);
  assert.ok(html.includes('Tasks: 4'), html);
  assert.ok(html.includes('Read-only tasks: 2'), html);
  assert.ok(html.includes('Speedup: 2.00x'), html);
});

test('the card carries a help link to the glossary section', () => {
  const html = card({ total_tasks: 1 });
  assert.ok(html.includes('#parallel-execution-metrics'), html);
  const sandbox = loadApp();
  assert.ok(vm.runInContext("docsLinkForKey('parallel_metrics_card_title')", sandbox).endsWith('#parallel-execution-metrics'));
  const doc = fs.readFileSync(path.join(__dirname, '..', '..', '..', 'docs', 'datawatch-definitions.md'), 'utf8');
  assert.ok(/^### Parallel execution metrics$/m.test(doc), 'glossary heading missing');
});

test('an unmeasured speedup reads as not measured and zero tasks draw nothing', () => {
  assert.ok(card({ total_tasks: 3, read_only_tasks: 0, speedup: 0 }).includes('not measured'));
  for (const p of [{}, { total_tasks: 0 }, { total_tasks: 'x' }, null, undefined]) {
    assert.equal(card(p), '', JSON.stringify(p));
  }
});

test('locale coverage: every key is in every bundle with the same placeholders', () => {
  const en = bundle('en');
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of KEYS) {
      assert.ok(typeof b[k] === 'string' && b[k].length > 0, lang + ' missing ' + k);
      assert.ok(!/\b(?:BL|GH#?)\d+\b/.test(b[k]), lang + ' ' + k + ' carries a tracker id');
      assert.equal((b[k].match(/%\d\$s/g) || []).join(), (en[k].match(/%\d\$s/g) || []).join(), lang + ' ' + k);
    }
  }
});
