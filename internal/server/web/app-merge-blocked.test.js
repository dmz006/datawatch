// Blocked merge — PWA side: the line on the task card, the merge-blocked item
// in Triage, the help link entry and the locale keys in every bundle.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const LOCALE_KEYS = [
  'merge_blocked_status', 'merge_blocked_card_title', 'merge_blocked_what', 'merge_blocked_line',
  'merge_blocked_why', 'merge_blocked_how_long', 'merge_blocked_next_action',
  'merge_blocked_reason_unknown', 'merge_blocked_action_unknown', 'merge_blocked_age_now',
  'merge_blocked_age_minutes', 'merge_blocked_age_hours', 'merge_blocked_age_days', 'merge_blocked_help_link',
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

function run(expr, vars) {
  const sandbox = loadApp();
  Object.assign(sandbox, vars || {});
  return vm.runInContext(expr, sandbox);
}

const BLOCK = { kind: 'dirty_worktree', reason: 'the worktree has uncommitted changes', action: 'commit or discard them', since: '2026-01-01T00:00:00Z', attempts: 2 };

test('the line shows the reason and the next action for a blocked task', () => {
  const html = run('taskMergeBlockedLineHtml(_t)', { _t: { id: 't1', merge_block: BLOCK } });
  assert.ok(html.includes('prd-task-merge-blocked'), html);
  assert.ok(html.includes('the worktree has uncommitted changes'), html);
  assert.ok(html.includes('commit or discard them'), html);
  assert.ok(html.includes('docs/datawatch-definitions.md#parallel-task-worktrees'), 'help link: ' + html);
});

test('no line when the merge is not blocked', () => {
  for (const tk of [{}, { merge_block: null }, { merge_block: {} }, { merge_state: 'pending' }, null, undefined]) {
    assert.equal(run('taskMergeBlockedLineHtml(_t)', { _t: tk }), '', JSON.stringify(tk));
  }
});

test('the task card carries the line only for a blocked task', () => {
  const card = (extra) => run(
    "renderTask({id:'p1',status:'needs_review'},{id:'s1',title:'S'},Object.assign({id:'t1',title:'T',spec:'x'}," + JSON.stringify(extra) + "),true)");
  assert.match(card({ merge_block: BLOCK }), /prd-task-merge-blocked/);
  assert.doesNotMatch(card({}), /prd-task-merge-blocked/);
});

test('hostile text is escaped in the line and in the triage item', () => {
  const evil = '<img src=x onerror=1>';
  const line = run('taskMergeBlockedLineHtml(_t)', { _t: { merge_block: { reason: evil, action: evil } } });
  assert.ok(!line.includes('<img'), line);
  const card = run('triageCardHtml(_r)', { _r: { prd_id: 'p1', prd_title: evil, status: 'merge_blocked', merge_blocks: [{ task_id: evil, reason: evil, action: evil }] } });
  assert.ok(!card.includes('<img'), card);
});

test('a merge-blocked triage item says what, why, how long and the action, and is selectable', () => {
  const html = run('triageCardHtml(_r)', { _r: {
    prd_id: 'p1', prd_title: 'Ship it', status: 'merge_blocked',
    merge_blocks: [{ task_id: 't1', task_title: 'Task one', reason: 'files overlap', action: 'resolve the overlap', since: new Date(Date.now() - 3 * 3600 * 1000).toISOString() }],
  } });
  assert.ok(html.includes('Automaton waiting to merge'), html);
  assert.ok(html.includes('files overlap'), html);
  assert.ok(html.includes('3 h'), html);
  assert.ok(html.includes('resolve the overlap'), html);
  assert.match(html, /data-triage-id="p1"/);
  assert.match(html, /tabindex="0"/);
  assert.match(html, /aria-pressed="false"/);
  assert.doesNotMatch(html, /\bPRD\b/);
});

test('selecting the item toggles its state', () => {
  const sandbox = loadApp();
  const classes = new Set();
  const attrs = {};
  sandbox._el = {
    classList: { contains: (c) => classes.has(c), toggle: (c, on) => (on ? classes.add(c) : classes.delete(c)) },
    setAttribute: (k, v) => { attrs[k] = v; }, style: {},
  };
  assert.equal(vm.runInContext('window.triageToggleSelect(_el)', sandbox), true);
  assert.equal(attrs['aria-pressed'], 'true');
  assert.equal(vm.runInContext('window.triageToggleSelect(_el)', sandbox), false);
  assert.equal(attrs['aria-pressed'], 'false');
});

test('failed and blocked triage cards are unchanged', () => {
  const sandbox = buildSandbox({ localStorage: new Map() });
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'automata-triage-model.js'), sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = bundle('en');
  sandbox._r = { prd_id: 'p2', task_id: 't2', status: 'failed', error: 'boom' };
  const html = vm.runInContext('triageCardHtml(_r)', sandbox);
  assert.match(html, /triage-card/);
  assert.match(html, /triage-headline/);
  assert.doesNotMatch(html, /triage-merge-blocked/);
});

test('the help link entry opens the glossary section', () => {
  const target = run('docsLinkForKey("merge_blocked_card_title")');
  assert.ok(target && target.endsWith('#parallel-task-worktrees'), String(target));
});

test('locale coverage: every key is in every bundle, says Automaton never PRD', () => {
  const en = bundle('en');
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of LOCALE_KEYS.concat('merge_blocked_status')) {
      assert.ok(typeof b[k] === 'string' && b[k].length > 0, lang + ' missing ' + k);
      assert.ok(!/\bPRDs?\b/.test(b[k]), lang + ' ' + k + ' says PRD');
      assert.equal((b[k].match(/%\d\$s/g) || []).join(), (en[k].match(/%\d\$s/g) || []).join(), lang + ' ' + k + ' placeholders');
    }
  }
  const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
  for (const k of LOCALE_KEYS) assert.ok(src.includes("'" + k + "'"), k + ' has no literal use in app.js');
});
