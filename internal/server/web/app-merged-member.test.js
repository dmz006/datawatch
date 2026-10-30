// Merged-member policy and decisions — PWA side.
//
// The Automata settings carry the autonomous.automation.merged_member_policy
// select (completed, manual or block). window.automationMergedMembersHTML(rows)
// draws the rows of GET /api/automations/{id}/merged-members on the Automation
// detail view, window.loadAutomationMergedMembers(id) fills them from the
// route, and the Automation decision log shows a merged_member entry. The
// merged-member locale keys are pinned in all five bundles, as is the docs
// link for the policy key.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const DEFINITIONS = path.join(__dirname, '..', '..', '..', 'docs', 'datawatch-definitions.md');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];
const HOSTILE = '<img src=x onerror=alert(1)>';
const ENTITY_IMG = '&lt;img src=x onerror=alert(1)&gt;';

function bundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
}

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  return sandbox;
}

function row(over) {
  return {
    automation_id: 'a1',
    automation_name: 'Nightly',
    member: 'm1',
    prd_id: 'p1',
    branch: 'automaton/p1',
    default_branch: 'main',
    proving_commit: '0123456789abcdef0123456789abcdef01234567',
    policy: 'completed',
    outcome: 'advanced',
    at: '2026-10-29T10:00:00Z',
    ...over,
  };
}

function view(rows) {
  const sandbox = loadApp();
  sandbox._rows = rows;
  return vm.runInContext('window.automationMergedMembersHTML(_rows)', sandbox);
}

function detail(automation) {
  const sandbox = loadApp();
  sandbox._a = automation;
  return vm.runInContext('buildAutomationDetailHTML(_a, {})', sandbox);
}

// ── the settings field ───────────────────────────────────────────────────────

test('GENERAL_CONFIG_FIELDS: autonomous.automation.merged_member_policy is a completed, manual or block select defaulting to completed', () => {
  const sandbox = loadApp();
  const fields = vm.runInContext('GENERAL_CONFIG_FIELDS', sandbox, { filename: 'app.js' });
  const autonomous = fields.find((sec) => sec.id === 'autonomous');
  const field = autonomous.fields.find((f) => f.key === 'autonomous.automation.merged_member_policy');
  assert.ok(field, 'autonomous.automation.merged_member_policy field is declared');
  assert.equal(field.type, 'select');
  assert.deepEqual(Array.from(field.options), ['completed', 'manual', 'block']);
  assert.equal(field.default, 'completed', 'unset reads as completed');
  assert.equal(field.labelKey, 'settings_automation_merged_member_policy');
  assert.ok(/Automaton/.test(field.label) && !/\bPRDs?\b/.test(field.label), 'fallback label says Automaton, never PRD');
  const keys = autonomous.fields.map((f) => f.key);
  assert.equal(
    keys.indexOf('autonomous.automation.merged_member_policy'),
    keys.indexOf('autonomous.automation.merge_pending_alert_minutes') + 1,
    'the field sits next to the merge-pending fields');
});

test('the merged_member_policy card key links to the merged-member-policy anchor, which the manual defines', () => {
  const sandbox = loadApp();
  const link = vm.runInContext("docsLinkForKey('merged_member_policy')", sandbox);
  assert.equal(link, 'datawatch-definitions.md#merged-member-policy');
  const md = fs.readFileSync(DEFINITIONS, 'utf8');
  assert.ok(/^#{2,4} Merged-member policy\s*$/m.test(md), 'definitions heading for the anchor missing');
  assert.ok(/^\*\*Merged member\*\*/m.test(md), 'glossary entry for the merged member missing');
});

// ── the decision view ────────────────────────────────────────────────────────

test('a decision row shows the member, branch, proving commit, policy and outcome', () => {
  const html = view([row()]);
  assert.ok(html.includes('Automaton merged-member decisions'), 'section title missing: ' + html);
  assert.ok(html.includes('Automaton member') && html.includes('m1'), 'member missing');
  assert.ok(html.includes('automaton/p1'), 'branch missing');
  assert.ok(html.includes('0123456789ab'), 'proving commit missing');
  assert.ok(!html.includes('0123456789abcdef0'), 'the commit is shortened');
  assert.ok(html.includes('Automaton policy') && html.includes('completed'), 'policy missing');
  assert.ok(html.includes('Automaton outcome') && html.includes('Automaton advanced'), 'outcome missing: ' + html);
  assert.ok(html.includes('data-outcome="advanced"'), 'outcome hook missing');
});

test('a blocked decision names its outcome and the newest row comes first', () => {
  const html = view([row({ member: 'first' }), row({ member: 'second', outcome: 'blocked', policy: 'manual' })]);
  assert.ok(html.includes('Automaton blocked'), 'blocked label missing');
  assert.ok(html.indexOf('second') < html.indexOf('first'), 'newest first');
});

test('no rows, a missing list or malformed rows draw nothing and never throw', () => {
  for (const bad of [[], null, undefined, 'x', 7, {}, [null, 'x', 4]]) {
    assert.equal(view(bad), '', 'nothing for ' + JSON.stringify(bad));
  }
});

test('hostile text in every row field is escaped', () => {
  const html = view([row({ member: HOSTILE, branch: HOSTILE, proving_commit: HOSTILE, policy: HOSTILE, outcome: HOSTILE })]);
  assert.ok(html.includes(ENTITY_IMG), 'escaped text missing: ' + html);
  assert.ok(!html.includes('<img'), 'live tag must not survive: ' + html);
});

test('the Automation detail page carries the merged-member slot', () => {
  const html = detail({ id: 'a1', name: 'Nightly', status: 'active', version: 1, members: [] });
  assert.ok(html.includes('id="automationMergedMembers"'), 'slot missing');
});

test('loadAutomationMergedMembers fills the slot from the merged-members route', async () => {
  const sandbox = loadApp();
  const slot = { innerHTML: '' };
  const calls = [];
  sandbox.document.getElementById = (id) => (id === 'automationMergedMembers' ? slot : null);
  sandbox.automationRequest = (method, p) => { calls.push(method + ' ' + p); return Promise.resolve({ merged_members: [row()] }); };
  vm.runInContext("_automationState.detailId = 'a1'", sandbox);
  await vm.runInContext("window.loadAutomationMergedMembers('a1')", sandbox);
  assert.deepEqual(calls, ['GET /api/automations/a1/merged-members']);
  assert.ok(slot.innerHTML.includes('automaton/p1'), 'rows not drawn: ' + slot.innerHTML);
});

test('loadAutomationMergedMembers leaves the slot alone when the read fails or another Automaton is open', async () => {
  const sandbox = loadApp();
  const slot = { innerHTML: 'kept' };
  sandbox.document.getElementById = () => slot;
  sandbox.automationRequest = () => Promise.reject(new Error('boom'));
  vm.runInContext("_automationState.detailId = 'a1'", sandbox);
  await vm.runInContext("window.loadAutomationMergedMembers('a1')", sandbox);
  assert.equal(slot.innerHTML, 'kept', 'a failed read must not clear the block');
  sandbox.automationRequest = () => Promise.resolve({ merged_members: [row()] });
  await vm.runInContext("window.loadAutomationMergedMembers('other')", sandbox);
  assert.equal(slot.innerHTML, 'kept', 'a stale answer must not draw');
});

// ── the decision log ─────────────────────────────────────────────────────────

test('the decision log shows a merged_member entry with its member, branch, commit and outcome', () => {
  const html = detail({
    id: 'a1', name: 'Nightly', status: 'active', version: 1, members: [],
    decisions: [{
      kind: 'merged_member', actor: 'daemon', at: '2026-10-29T10:00:00Z', note: 'raw note',
      merged_member: { member: 'm1', prd_id: 'p1', branch: 'automaton/p1', default_branch: 'main', commit: '0123456789abcdef', policy: 'completed', outcome: 'advanced' },
    }],
  });
  assert.ok(html.includes('data-kind="merged_member"'), 'entry missing');
  assert.ok(html.includes('Automaton member m1 on main, proved merged at 0123456789ab: Automaton advanced'), 'entry text missing: ' + html);
});

test('a merged_member entry without its record falls back to the plain note, and hostile text is escaped', () => {
  const plain = detail({ id: 'a1', name: 'N', status: 'active', version: 1, members: [], decisions: [{ kind: 'merged_member', note: 'plain note' }] });
  assert.ok(plain.includes('plain note'), 'plain note missing');
  const hostile = detail({
    id: 'a1', name: 'N', status: 'active', version: 1, members: [],
    decisions: [{ kind: 'merged_member', merged_member: { member: HOSTILE, default_branch: HOSTILE, commit: HOSTILE, outcome: HOSTILE } }],
  });
  assert.ok(hostile.includes(ENTITY_IMG), 'escaped text missing');
  assert.ok(!hostile.includes('<img'), 'live tag must not survive');
});

// ── locale keys ──────────────────────────────────────────────────────────────

const KEYS = [
  'settings_automation_merged_member_policy',
  'automation_merged_members_title',
  'automation_merged_member_member',
  'automation_merged_member_branch',
  'automation_merged_member_commit',
  'automation_merged_member_policy',
  'automation_merged_member_outcome',
  'automation_merged_member_outcome_advanced',
  'automation_merged_member_outcome_blocked',
  'automation_decision_merged_member',
];

test('every merged-member key exists in every locale bundle, says Automaton or Automata and has no tracker id', () => {
  for (const lang of LANGS) {
    const b = bundle(lang);
    for (const k of KEYS) {
      const v = b[k];
      assert.ok(typeof v === 'string' && v.length > 0, lang + ' missing or empty ' + k);
      assert.ok(/\bAutomat(?:on|a)\b/.test(v), lang + ' ' + k + ' must say Automaton or Automata: ' + v);
      assert.ok(!/\bPRDs?\b/.test(v), lang + ' ' + k + ' must say Automaton or Automata, never PRD');
      assert.ok(!/\b(?:BL|F|B|S)\d{1,6}\b/.test(v), lang + ' ' + k + ' carries a tracker id');
    }
  }
});

test('the non-English bundles carry the English text until translations arrive', () => {
  const en = bundle('en');
  for (const lang of LANGS.filter(l => l !== 'en')) {
    const b = bundle(lang);
    for (const k of KEYS) assert.equal(b[k], en[k], lang + ' ' + k + ' should match the English text for now');
  }
});
