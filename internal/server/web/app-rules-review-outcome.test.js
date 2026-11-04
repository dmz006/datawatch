// The completion-review outcome on the attention row and the Automaton detail
// block: the action line, the findings (escaped, capped at five with a "+N more"
// line), an item without an outcome rendering as before, and every new key present
// in all five locale bundles. Fixed data only; no browser, no daemon.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const vm = require('vm');
const { buildSandbox, loadScript } = require('./testutil_browser_stub');

const WEB = __dirname;
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const KEYS = [
  'rules_review_action_fix_task_created', 'rules_review_action_fix_task_failed',
  'rules_review_action_retries_spent', 'rules_review_action_auto_fix_disabled',
  'rules_review_action_unknown', 'rules_review_retries_of', 'rules_review_findings_title',
  'rules_review_findings_more', 'rules_review_next_fix_task_failed',
  'rules_review_next_retries_spent', 'rules_review_next_auto_fix_disabled',
  'settings_auto_fix_retries', 'settings_auto_fix_retries_help',
];

function loadApp() {
  const sandbox = buildSandbox({});
  sandbox.setInterval = () => 1;
  vm.createContext(sandbox);
  loadScript(path.join(WEB, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}
const run = (sb, code) => vm.runInContext(code, sb);

function item(extra) {
  return Object.assign({ id: 'p1', title: 'Title p1', status: 'needs_review' }, extra || {});
}
function outcome(extra) {
  return Object.assign({ action: 'retries_spent', findings: ['one', 'two'], summary: 's', attempt: 2, retry_budget: 2, retries_spent: 2, reason: '', at: '2026-11-01T00:00:00Z' }, extra || {});
}
const row = (sb, it) => { sb.__it = it; return run(sb, '_tagPinnedRowHTML(__it, _tagPinnedReason(__it))'); };

test('the attention row names the action and lists the findings', () => {
  const sb = loadApp();
  const html = row(sb, item({ rules_review_outcome: outcome() }));
  assert.match(html, /all auto-fix retries are spent/);
  assert.match(html, /<details class="rules-review-findings"/);
  assert.doesNotMatch(html, /<details[^>]* open/);
  assert.match(html, /<li>one<\/li><li>two<\/li>/);
});

test('findings are HTML-escaped', () => {
  const sb = loadApp();
  const html = row(sb, item({ rules_review_outcome: outcome({ findings: ['<img src=x onerror=alert(1)>'] }) }));
  assert.ok(!html.includes('<img src=x'));
  assert.match(html, /&lt;img src=x onerror=alert\(1\)&gt;/);
});

test('the row shows five findings and a +N more line for the rest', () => {
  const sb = loadApp();
  const f = ['a', 'b', 'c', 'd', 'e', 'f', 'g'];
  const html = row(sb, item({ rules_review_outcome: outcome({ findings: f }) }));
  assert.equal((html.match(/<li>/g) || []).length, 5);
  assert.match(html, /\+2 more/);
  assert.ok(!html.includes('<li>f</li>'));
});

test('an item without an outcome renders as before', () => {
  const sb = loadApp();
  const html = row(sb, item({ blocked_reason: 'waiting on X' }));
  assert.match(html, /waiting on X/);
  assert.doesNotMatch(html, /rules-review/);
});

test('an item with an outcome and a blocked reason still shows the reason', () => {
  const sb = loadApp();
  const html = row(sb, item({ blocked_reason: 'waiting on X', rules_review_outcome: outcome() }));
  assert.match(html, /waiting on X/);
  assert.match(html, /all auto-fix retries are spent/);
});

test('the detail block shows every finding, the retries and the next step', () => {
  const sb = loadApp();
  sb.__o = outcome({ action: 'fix_task_failed', findings: ['a', 'b', 'c', 'd', 'e', 'f'], retries_spent: 1, retry_budget: 3 });
  const html = run(sb, '_rulesReviewOutcomeHTML(__o, { full: true })');
  assert.equal((html.match(/<li>/g) || []).length, 6);
  assert.match(html, /1 of 3/);
  assert.match(html, /add a fix task yourself/);
  assert.doesNotMatch(html, /\+\d+ more/);
});

test('every new key exists in all five locale files', () => {
  for (const lang of LANGS) {
    const b = JSON.parse(fs.readFileSync(path.join(WEB, 'locales', lang + '.json'), 'utf8'));
    for (const k of KEYS) assert.ok(typeof b[k] === 'string' && b[k].length > 0, `${lang} missing ${k}`);
    for (const k of KEYS) assert.doesNotMatch(b[k], /\bPRD\b|Automation\b/, `${lang} ${k} uses a banned word`);
  }
});
