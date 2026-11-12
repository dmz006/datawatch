// Parallel task worktrees — PWA side: the settings rows and group, the help
// link entries, the merge-state badge on the task card, and the locale keys in
// every bundle.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const ANCHOR = 'parallel-task-worktrees';
const SETTING_KEYS = [
  'autonomous.parallel.worktree_per_task',
  'autonomous.parallel.merge_conflict_policy',
  'autonomous.parallel.merge_conflict_model',
];
const LOCALE_KEYS = [
  'settings_parallel_task_group', 'settings_parallel_read_only_tasks', 'settings_parallel_worktree_per_task',
  'settings_parallel_merge_conflict_policy', 'settings_parallel_merge_conflict_model',
  'parallel_policy_additive_then_model', 'parallel_policy_block', 'parallel_policy_triage',
  'task_merge_state_pending', 'task_merge_state_merged', 'task_merge_state_conflict',
  'task_merge_state_held', 'task_merge_badge_tip', 'task_merge_badge_tip_commit',
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

function autonomousRows() {
  const sandbox = loadApp();
  return JSON.parse(vm.runInContext(
    "JSON.stringify((GENERAL_CONFIG_FIELDS.find(s => s.id === 'autonomous') || {fields: []}).fields)", sandbox));
}

function docsLink(key) {
  const sandbox = loadApp();
  sandbox._key = key;
  return vm.runInContext('docsLinkForKey(_key)', sandbox);
}

test('the three settings sit in a group right after the supervisor watch rows', () => {
  const rows = autonomousRows();
  const idxOf = (k) => rows.findIndex((r) => r.key === k);
  const last = idxOf('autonomous.automation.supervisor_watch.working_model');
  assert.ok(last >= 0, 'supervisor watch rows missing');
  const group = rows[last + 1];
  assert.equal(group.type, 'group');
  assert.equal(group.labelKey, 'settings_parallel_task_group');
  SETTING_KEYS.forEach((k, i) => assert.equal(idxOf(k), last + 2 + i, k + ' is not next to the supervisor watch rows'));
});

test('each setting has the right control, a locale label and a help link key', () => {
  const rows = autonomousRows();
  const by = new Map(rows.map((r) => [r.key, r]));
  assert.equal(by.get(SETTING_KEYS[0]).type, 'toggle');
  const policy = by.get(SETTING_KEYS[1]);
  assert.equal(policy.type, 'select');
  assert.deepEqual(policy.options, ['additive_then_model', 'block', 'triage']);
  assert.equal(policy.default, 'additive_then_model');
  assert.equal(by.get(SETTING_KEYS[2]).type, 'text');
  const en = bundle('en');
  for (const k of SETTING_KEYS) {
    const r = by.get(k);
    assert.ok(en[r.labelKey], k + ' has no English label');
    assert.equal(r.label, en[r.labelKey], k + ' fallback label differs from the English bundle');
    assert.equal(r.docsKey, k);
  }
  for (const v of Object.values(policy.optionLabelKeys)) assert.ok(en[v], 'option label ' + v);
});

test('the group and every setting have a help link table entry that opens the glossary section', () => {
  for (const key of ['settings_parallel_task_group', 'task_merge_state_badge'].concat(SETTING_KEYS)) {
    const target = docsLink(key);
    assert.ok(target && target.endsWith('#' + ANCHOR), key + ' -> ' + target);
  }
  const doc = fs.readFileSync(path.join(__dirname, '..', '..', '..', 'docs', 'datawatch-definitions.md'), 'utf8');
  const slugs = doc.split('\n').filter((l) => /^#{2,4} /.test(l))
    .map((l) => l.replace(/^#+ /, '').toLowerCase().replace(/[^a-z0-9 -]/g, '').trim().replace(/ /g, '-'));
  assert.ok(slugs.includes(ANCHOR), 'docs/datawatch-definitions.md has no heading for #' + ANCHOR);
});

function badge(task) {
  const sandbox = loadApp();
  sandbox._t = task;
  return vm.runInContext('window.taskMergeBadgeHtml(_t)', sandbox);
}

test('the badge draws for each known merge state and for no other', () => {
  const labels = { pending: 'merge pending', merged: 'merged', conflict: 'merge conflict', held: 'merge held' };
  for (const [state, label] of Object.entries(labels)) {
    const html = badge({ id: 't1', merge_state: state });
    assert.ok(html.includes('prd-task-merge-' + state), state + ': ' + html);
    assert.ok(html.includes('>' + label + '<'), state + ' label: ' + html);
  }
  for (const t of [{}, { merge_state: '' }, { merge_state: 'constructor' }, { merge_state: 'bogus' }, null, undefined]) {
    assert.equal(badge(t), '', JSON.stringify(t));
  }
});

test('a merged task shows the short merge commit in the badge tip, and hostile text is escaped', () => {
  const html = badge({ merge_state: 'merged', merge_commit: '0123456789abcdef' });
  assert.ok(html.includes('commit 0123456'), html);
  assert.ok(!html.includes('0123456789'), 'only the short commit belongs in the tip');
  const hostile = badge({ merge_state: 'merged', merge_commit: '<img src=x onerror=1>' });
  assert.ok(!hostile.includes('<img'), hostile);
});

test('the task card carries the badge', () => {
  const sandbox = loadApp();
  const html = vm.runInContext(
    "renderTask({id:'p1',status:'needs_review'},{id:'s1',title:'S'},{id:'t1',title:'T',spec:'x',merge_state:'conflict'},true)", sandbox);
  assert.match(html, /prd-task-merge-conflict/);
  const none = vm.runInContext(
    "renderTask({id:'p1',status:'needs_review'},{id:'s1',title:'S'},{id:'t2',title:'T',spec:'x'},true)", sandbox);
  assert.doesNotMatch(none, /prd-task-merge-badge/);
});

test('locale coverage: every new key is in every bundle, says Automaton or Automata and has no tracker id', () => {
  const en = bundle('en');
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of LOCALE_KEYS) {
      assert.ok(typeof b[k] === 'string' && b[k].length > 0, lang + ' missing or empty ' + k);
      assert.ok(!/\bPRDs?\b/.test(b[k]), lang + ' ' + k + ' says PRD');
      assert.ok(!/\b(?:BL|GH#?)\d+\b/.test(b[k]), lang + ' ' + k + ' carries a tracker id');
      assert.equal((b[k].match(/%\d\$s/g) || []).join(), (en[k].match(/%\d\$s/g) || []).join(), lang + ' ' + k + ' placeholders differ from English');
    }
  }
});

test('every locale key the badge and rows use is declared by a literal in app.js', () => {
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  for (const k of LOCALE_KEYS) assert.ok(src.includes("'" + k + "'"), k + ' has no literal use in app.js');
});

test('the merge retry interval row and its help link entry exist', () => {
  const row = autonomousRows().find((r) => r.key === 'autonomous.parallel.merge_retry_seconds');
  assert.ok(row, 'merge_retry_seconds row missing');
  assert.equal(row.type, 'number');
  assert.equal(row.min, 0);
  assert.equal(row.step, 1);
  assert.equal(row.default, 60);
  assert.equal(row.docsKey, row.key);
  for (const lang of LANGS) {
    const b = bundle(lang);
    assert.ok(b[row.labelKey], lang + ' missing ' + row.labelKey);
    assert.ok(b[row.helpKey], lang + ' missing ' + row.helpKey);
  }
  const target = docsLink(row.key);
  assert.ok(target && target.endsWith('#' + ANCHOR), String(target));
});
